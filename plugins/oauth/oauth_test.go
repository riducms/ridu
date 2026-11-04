package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	mcpplugin "github.com/riducms/ridu/plugins/mcp"
	"github.com/riducms/ridu/plugins/oauth"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

const redirectURI = "http://127.0.0.1:33418/callback"

type fixture struct {
	t           *testing.T
	application *ridu.App
	server      *httptest.Server
}

func newFixture(t *testing.T, config oauth.Config) *fixture {
	t.Helper()
	authorization := oauth.New(config)
	signedIn := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Allow(), nil
	}
	application := ridu.Config{
		Name: "OAuth contracts", Admin: ridu.AdminConfig{User: "users"},
		Plugins: []ridu.Plugin{authorization, mcpplugin.New(mcpplugin.Config{
			Collections:   []mcpplugin.Resource{{Slug: "posts"}},
			Authorization: authorization,
		})},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true,
				AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
				Fields:     field.Fields{field.Text("email").Required().Unique(), field.Text("name"), field.Checkbox("agent")},
			},
			{Slug: "posts", Access: ridu.CollectionAccess{Read: signedIn}, Fields: field.Fields{field.Text("title")}},
		},
	}
	// A real store: its row locks make the code and refresh token single-use.
	manifest, err := ridu.Resolve(application)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "oauth.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(application, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Hello")}, ridu.MutationOptions{System: true}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	t.Cleanup(server.Close)
	return &fixture{t: t, application: app, server: server}
}

// person creates a user and returns them with a session token.
func (f *fixture) person(email string, values store.Values) (store.Document, string) {
	f.t.Helper()
	values["email"] = store.String(email)
	user, err := f.application.Local().Create(f.t.Context(), "users", values, ridu.MutationOptions{System: true})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.application.SetPassword(f.t.Context(), "users", user.ID, "correct-horse"); err != nil {
		f.t.Fatal(err)
	}
	session, err := f.application.Login(f.t.Context(), "users", email, "correct-horse")
	if err != nil {
		f.t.Fatal(err)
	}
	return user, session.Token
}

func (f *fixture) do(method, path, contentType, body, authorization string) (int, http.Header, map[string]any) {
	f.t.Helper()
	request, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, _ := io.ReadAll(response.Body)
	decoded := map[string]any{}
	_ = json.Unmarshal(encoded, &decoded)
	return response.StatusCode, response.Header, decoded
}

func (f *fixture) json(path, body, authorization string) (int, map[string]any) {
	status, _, decoded := f.do(http.MethodPost, path, "application/json", body, authorization)
	return status, decoded
}

func (f *fixture) form(path string, values url.Values) (int, map[string]any) {
	status, _, decoded := f.do(http.MethodPost, path, "application/x-www-form-urlencoded", values.Encode(), "")
	return status, decoded
}

// register registers a public client the way Claude does and returns its ID.
func (f *fixture) register() string {
	f.t.Helper()
	status, client := f.json("/api/plugins/oauth/register", `{"client_name":"Claude","redirect_uris":["`+redirectURI+`"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`, "")
	id, _ := client["client_id"].(string)
	if status != http.StatusCreated || id == "" {
		f.t.Fatalf("register = %d %v", status, client)
	}
	return id
}

// authorizeQuery is the query string a client sends the browser to the
// consent page with.
func (f *fixture) authorizeQuery(client, challenge string) string {
	return "?" + url.Values{
		"response_type": {"code"}, "client_id": {client}, "redirect_uri": {redirectURI},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"state-1"},
		"resource": {f.server.URL + "/api/mcp"},
	}.Encode()
}

// approve approves a request as session's user, acting as actAs, and returns
// the code from the redirect.
func (f *fixture) approve(session, query, actAs string) string {
	f.t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query, "allow": true, "actAs": actAs})
	status, decision := f.json("/api/plugins/oauth/decision", string(body), "Session "+session)
	redirect, _ := decision["redirect"].(string)
	target, err := url.Parse(redirect)
	if status != http.StatusOK || err != nil || target.Query().Get("state") != "state-1" || target.Query().Get("code") == "" {
		f.t.Fatalf("decision = %d %v", status, decision)
	}
	return target.Query().Get("code")
}

func pkce() (verifier, challenge string) {
	verifier = strings.Repeat("verifier-", 6)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (f *fixture) listTools(accessToken string) error {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "oauth-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(f.t.Context(), &mcpsdk.StreamableClientTransport{
		Endpoint:             f.server.URL + "/api/mcp",
		HTTPClient:           &http.Client{Transport: bearer{token: accessToken}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	_, err = session.ListTools(f.t.Context(), nil)
	return err
}

type bearer struct{ token string }

func (transport bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+transport.token)
	return http.DefaultTransport.RoundTrip(cloned)
}

func TestAnMCPClientConnectsWithOAuthAndStopsWhenRevoked(t *testing.T) {
	f := newFixture(t, oauth.Config{})
	ada, session := f.person("ada@example.test", store.Values{"name": store.String("Ada")})

	// A client without a credential learns where to sign in.
	status, header, _ := f.do(http.MethodPost, "/api/mcp", "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, "")
	metadataURL := f.server.URL + "/.well-known/oauth-protected-resource/api/mcp"
	if status != http.StatusUnauthorized || !strings.Contains(header.Get("WWW-Authenticate"), `resource_metadata="`+metadataURL+`"`) {
		t.Fatalf("anonymous MCP = %d %q", status, header.Get("WWW-Authenticate"))
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource/api/mcp", "/.well-known/oauth-protected-resource"} {
		status, header, resource := f.do(http.MethodGet, path, "", "", "")
		servers, _ := resource["authorization_servers"].([]any)
		if status != http.StatusOK || resource["resource"] != f.server.URL+"/api/mcp" || len(servers) != 1 || servers[0] != f.server.URL || header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s = %d %v", path, status, resource)
		}
	}
	status, _, issuer := f.do(http.MethodGet, "/.well-known/oauth-authorization-server", "", "", "")
	if status != http.StatusOK || issuer["issuer"] != f.server.URL || issuer["authorization_endpoint"] != f.server.URL+"/admin/oauth/authorize" ||
		issuer["token_endpoint"] != f.server.URL+"/api/plugins/oauth/token" || issuer["registration_endpoint"] != f.server.URL+"/api/plugins/oauth/register" {
		t.Fatalf("authorization server metadata = %d %v", status, issuer)
	}

	client := f.register()
	verifier, challenge := pkce()
	query := f.authorizeQuery(client, challenge)

	// The consent page needs a signed-in person, then names the app.
	if status, _ := f.json("/api/plugins/oauth/authorization", `{"query":"`+query+`"}`, ""); status != http.StatusUnauthorized {
		t.Fatalf("anonymous consent check = %d", status)
	}
	body, _ := json.Marshal(map[string]string{"query": query})
	status, request := f.json("/api/plugins/oauth/authorization", string(body), "Session "+session)
	actAs, _ := request["actAs"].([]any)
	if status != http.StatusOK || request["client"].(map[string]any)["name"] != "Claude" || request["redirectHost"] != "127.0.0.1" || len(actAs) != 1 {
		t.Fatalf("consent check = %d %v", status, request)
	}
	code := f.approve(session, query, ada.ID)
	if _, _, page := f.do(http.MethodGet, "/api/collections/oauth-connections", "", "", "Session "+session); len(page["docs"].([]any)) != 0 {
		t.Fatalf("an approval not yet exchanged is listed as a connected app: %v", page)
	}

	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {client}, "code_verifier": {verifier}}
	if status, failure := f.form("/api/plugins/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {client}, "code_verifier": {strings.Repeat("x", 43)}}); status != http.StatusBadRequest || failure["error"] != "invalid_grant" {
		t.Fatalf("wrong verifier = %d %v", status, failure)
	}
	status, tokens := f.form("/api/plugins/oauth/token", exchange)
	access, _ := tokens["access_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	if status != http.StatusOK || !strings.HasPrefix(access, "riduoauth_at_") || refresh == "" || tokens["token_type"] != "Bearer" {
		t.Fatalf("code exchange = %d %v", status, tokens)
	}
	if status, failure := f.form("/api/plugins/oauth/token", exchange); status != http.StatusBadRequest || failure["error"] != "invalid_grant" {
		t.Fatalf("second code exchange = %d %v", status, failure)
	}
	if err := f.listTools(access); err != nil {
		t.Fatalf("MCP with the access token: %v", err)
	}

	// Concurrent refreshes with one refresh token: exactly one wins.
	var wait sync.WaitGroup
	results := make([]map[string]any, 4)
	statuses := make([]int, len(results))
	for index := range results {
		wait.Add(1)
		go func() {
			defer wait.Done()
			statuses[index], results[index] = f.form("/api/plugins/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {client}})
		}()
	}
	wait.Wait()
	won := 0
	for index, result := range results {
		if statuses[index] == http.StatusOK {
			won++
			access, _ = result["access_token"].(string)
		} else if result["error"] != "invalid_grant" {
			t.Fatalf("losing refresh = %d %v", statuses[index], result)
		}
	}
	if won != 1 {
		t.Fatalf("%d concurrent refreshes with one token succeeded, want 1", won)
	}
	if err := f.listTools(access); err != nil {
		t.Fatalf("MCP with the refreshed access token: %v", err)
	}

	// The person sees the connection without its secrets, and deleting it
	// revokes it.
	status, _, page := f.do(http.MethodGet, "/api/collections/oauth-connections", "", "", "Session "+session)
	documents, _ := page["docs"].([]any)
	if status != http.StatusOK || len(documents) != 1 {
		t.Fatalf("connections = %d %v", status, page)
	}
	connection := documents[0].(map[string]any)
	if connection["clientName"] != "Claude" || connection["accessTokenHash"] != nil || connection["codeChallenge"] != nil || connection["redirectUri"] != nil {
		t.Fatalf("connection exposes = %v", connection)
	}
	_, stranger := f.person("eve@example.test", store.Values{})
	if _, _, page := f.do(http.MethodGet, "/api/collections/oauth-connections", "", "", "Session "+stranger); len(page["docs"].([]any)) != 0 {
		t.Fatalf("another user sees connections: %v", page)
	}
	if status, _, _ := f.do(http.MethodGet, "/api/collections/oauth-clients", "", "", "Session "+session); status != http.StatusForbidden {
		t.Fatalf("clients read = %d", status)
	}
	if status, _, _ := f.do(http.MethodDelete, "/api/collections/oauth-connections/"+connection["id"].(string), "", "", "Session "+session); status != http.StatusOK {
		t.Fatalf("revoke = %d", status)
	}
	status, header, _ = f.do(http.MethodPost, "/api/mcp", "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, "Bearer "+access)
	if status != http.StatusUnauthorized || !strings.Contains(header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("MCP after revoking = %d %q", status, header.Get("WWW-Authenticate"))
	}
}

func TestConsentRefusesBadRequestsAndOffersActAsUsers(t *testing.T) {
	f := newFixture(t, oauth.Config{ActAs: func(_ context.Context, local *ridu.LocalAPI, _ store.Document) ([]store.Document, error) {
		page, err := local.List(t.Context(), "users", ridu.ListOptions{System: true, Limit: 10})
		var agents []store.Document
		for _, user := range page.Documents {
			if agent, _ := user.Values["agent"].BooleanValue(); agent {
				agents = append(agents, user)
			}
		}
		return agents, err
	}})
	ada, session := f.person("ada@example.test", store.Values{"name": store.String("Ada")})
	agent, _ := f.person("agent@example.test", store.Values{"name": store.String("Claude agent"), "agent": store.Boolean(true)})
	_, _ = f.person("bob@example.test", store.Values{"name": store.String("Bob")})
	client := f.register()
	verifier, challenge := pkce()
	query := f.authorizeQuery(client, challenge)

	body, _ := json.Marshal(map[string]string{"query": query})
	_, request := f.json("/api/plugins/oauth/authorization", string(body), "Session "+session)
	actAs, _ := request["actAs"].([]any)
	if len(actAs) != 2 || actAs[0].(map[string]any)["id"] != ada.ID || actAs[1].(map[string]any)["label"] != "Claude agent" {
		t.Fatalf("act-as choices = %v", request["actAs"])
	}
	// Bob isn't offered, so the client can't be made to act as him.
	bob, _ := f.application.Local().List(t.Context(), "users", ridu.ListOptions{System: true, Limit: 10})
	for _, user := range bob.Documents {
		if email, _ := user.Values["email"].StringValue(); email == "bob@example.test" {
			payload, _ := json.Marshal(map[string]any{"query": query, "allow": true, "actAs": user.ID})
			if status, _ := f.json("/api/plugins/oauth/decision", string(payload), "Session "+session); status != http.StatusUnprocessableEntity {
				t.Fatalf("acting as an unoffered user = %d", status)
			}
		}
	}
	code := f.approve(session, query, agent.ID)
	status, tokens := f.form("/api/plugins/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {client}, "code_verifier": {verifier}})
	if status != http.StatusOK {
		t.Fatalf("exchange = %d %v", status, tokens)
	}
	identity, err := f.application.AuthenticateExternalIdentity(t.Context(), map[string][]string{"Authorization": {"Bearer " + tokens["access_token"].(string)}})
	if err != nil || identity.Actor.ID != agent.ID {
		t.Fatalf("the token acts as %q, %v; want the agent", identity.Actor.ID, err)
	}
	// RFC 7009 revocation ends the connection too.
	if status, _ := f.form("/api/plugins/oauth/revoke", url.Values{"token": {tokens["refresh_token"].(string)}, "client_id": {client}}); status != http.StatusOK {
		t.Fatalf("revoke = %d", status)
	}
	if _, err := f.application.AuthenticateExternalIdentity(t.Context(), map[string][]string{"Authorization": {"Bearer " + tokens["access_token"].(string)}}); err == nil {
		t.Fatal("the access token works after revocation")
	}

	for name, raw := range map[string]string{
		"an unregistered redirect": strings.Replace(query, url.QueryEscape(redirectURI), url.QueryEscape("https://evil.example/cb"), 1),
		"an unknown client":        strings.Replace(query, client, "missing", 1),
	} {
		payload, _ := json.Marshal(map[string]string{"query": raw})
		status, refusal := f.json("/api/plugins/oauth/authorization", string(payload), "Session "+session)
		if status != http.StatusBadRequest || refusal["redirect"] != nil {
			t.Errorf("%s = %d %v, want a refusal shown only to the person", name, status, refusal)
		}
	}
	payload, _ := json.Marshal(map[string]string{"query": strings.Replace(query, "S256", "plain", 1)})
	status, refusal := f.json("/api/plugins/oauth/authorization", string(payload), "Session "+session)
	if redirect, _ := refusal["redirect"].(string); status != http.StatusOK || refusal["error"] != "invalid_request" || !strings.HasPrefix(redirect, redirectURI+"?") {
		t.Fatalf("plain PKCE = %d %v, want an error redirect to the client", status, refusal)
	}
	for name, metadata := range map[string]string{
		"a non-loopback http redirect": `{"redirect_uris":["http://example.test/cb"]}`,
		"a confidential client":        `{"redirect_uris":["https://example.test/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
	} {
		if status, failure := f.json("/api/plugins/oauth/register", metadata, ""); status != http.StatusBadRequest || failure["error"] == nil {
			t.Errorf("registering %s = %d %v", name, status, failure)
		}
	}
}
