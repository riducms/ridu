package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	mcpplugin "github.com/riducms/ridu/plugins/mcp"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestMCPListsExplicitToolsAndReadsThroughActorAccessAndRedaction(t *testing.T) {
	publishedPath, err := query.ParsePath("published")
	if err != nil {
		t.Fatal(err)
	}
	authenticatedPublished := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil || ctx.ActorCollection != "users" {
			return ridu.Deny(), nil
		}
		email, _ := ctx.Actor.Values["email"].StringValue()
		if email != "agent@example.test" {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal(publishedPath, query.Boolean(true))), nil
	}
	authenticatedGlobal := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil || ctx.ActorCollection != "users" {
			return ridu.Deny(), nil
		}
		return ridu.Allow(), nil
	}

	application, err := ridu.New(ridu.Config{
		Name: "MCP contracts", Admin: ridu.AdminConfig{User: "users"},
		Plugins: []ridu.Plugin{mcpplugin.New(mcpplugin.Config{
			Collections:  []mcpplugin.Resource{{Slug: "posts"}},
			Globals:      []mcpplugin.Resource{{Slug: "settings"}},
			DefaultLimit: 5,
			MaxLimit:     10,
		})},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true,
				AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}, APIKeys: true},
				Fields: field.Fields{
					field.Text("email").Required().Unique(),
				},
			},
			{
				Slug: "posts", Access: ridu.CollectionAccess{Read: authenticatedPublished},
				Fields: field.Fields{
					field.Text("title"),
					field.Checkbox("published"),
					field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
				},
			},
		},
		Globals: []ridu.Global{{
			Slug: "settings", Access: ridu.GlobalAccess{Read: authenticatedGlobal},
			Fields: field.Fields{
				field.Text("siteName"),
				field.Text("privateNote").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("Visible"), "published": store.Boolean(true), "secret": store.String("redact-me"),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("Hidden"), "published": store.Boolean(false), "secret": store.String("redact-me-too"),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateGlobal(t.Context(), "settings", store.Values{
		"siteName": store.String("Ridu"), "privateNote": store.String("internal"),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Create(t.Context(), "users", store.Values{"email": store.String("agent@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(t.Context(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(t.Context(), "users", "agent@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	key, err := application.CreateAPIKey(t.Context(), session.Token, "MCP", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	deniedUser, err := application.Local().Create(t.Context(), "users", store.Values{"email": store.String("denied@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(t.Context(), "users", deniedUser.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	deniedLogin, err := application.Login(t.Context(), "users", "denied@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	deniedKey, err := application.CreateAPIKey(t.Context(), deniedLogin.Token, "Denied MCP", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	unsupported, err := http.Get(server.URL + "/api/mcp")
	if err != nil {
		t.Fatal(err)
	}
	unsupported.Body.Close()
	if unsupported.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("MCP GET status = %d", unsupported.StatusCode)
	}

	unauthorized, err := http.Post(server.URL+"/api/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized || unauthorized.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("anonymous MCP status = %d, auth = %q", unauthorized.StatusCode, unauthorized.Header.Get("WWW-Authenticate"))
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "ridu-test", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(t.Context(), &mcpsdk.StreamableClientTransport{
		Endpoint:             server.URL + "/api/mcp",
		HTTPClient:           &http.Client{Transport: authorizationTransport{token: key.Key}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 2 || tools.Tools[0].Name != "ridu_find_collection_posts" || tools.Tools[1].Name != "ridu_find_global_settings" {
		t.Fatalf("MCP tools = %#v", tools.Tools)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %s is not marked read-only: %#v", tool.Name, tool.Annotations)
		}
	}

	posts, err := clientSession.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "ridu_find_collection_posts", Arguments: map[string]any{"limit": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if posts.IsError {
		t.Fatalf("posts tool error: %s", toolText(posts))
	}
	var page struct {
		Documents []struct {
			Values map[string]any `json:"values"`
		} `json:"docs"`
		Total int `json:"totalDocs"`
	}
	decodeStructured(t, posts.StructuredContent, &page)
	if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].Values["title"] != "Visible" {
		t.Fatalf("access-filtered MCP page = %#v", page)
	}
	if _, exists := page.Documents[0].Values["secret"]; exists {
		t.Fatalf("MCP page disclosed field-redacted value: %#v", page.Documents[0].Values)
	}

	settings, err := clientSession.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "ridu_find_global_settings", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if settings.IsError {
		t.Fatalf("settings tool error: %s", toolText(settings))
	}
	var global struct {
		Document struct {
			Values map[string]any `json:"values"`
		} `json:"document"`
	}
	decodeStructured(t, settings.StructuredContent, &global)
	if global.Document.Values["siteName"] != "Ridu" {
		t.Fatalf("MCP global = %#v", global)
	}
	if _, exists := global.Document.Values["privateNote"]; exists {
		t.Fatalf("MCP global disclosed field-redacted value: %#v", global.Document.Values)
	}

	tooLarge, err := clientSession.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "ridu_find_collection_posts", Arguments: map[string]any{"limit": 11},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tooLarge.IsError || !strings.Contains(toolText(tooLarge), "limit must not exceed 10") {
		t.Fatalf("oversized limit result = %#v", tooLarge)
	}

	invalidSelect, err := clientSession.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "ridu_find_collection_posts", Arguments: map[string]any{"select": []any{"title["}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !invalidSelect.IsError || !strings.Contains(toolText(invalidSelect), "select[0]") {
		t.Fatalf("invalid select result = %#v", invalidSelect)
	}

	unknown, unknownErr := clientSession.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "ridu_missing_tool", Arguments: map[string]any{}})
	if unknownErr == nil && (unknown == nil || !unknown.IsError) {
		t.Fatalf("unknown tool was accepted: %#v", unknown)
	}

	deniedSession, err := client.Connect(t.Context(), &mcpsdk.StreamableClientTransport{
		Endpoint:             server.URL + "/api/mcp",
		HTTPClient:           &http.Client{Transport: authorizationTransport{token: deniedKey.Key}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deniedSession.Close()
	denied, err := deniedSession.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "ridu_find_collection_posts", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !denied.IsError || !strings.Contains(toolText(denied), "access_denied") {
		t.Fatalf("denied actor result = %#v", denied)
	}
}

func TestMCPRejectsUnknownConfiguredResourcesAtStartup(t *testing.T) {
	_, err := ridu.New(ridu.Config{
		Name: "Invalid MCP", Plugins: []ridu.Plugin{mcpplugin.New(mcpplugin.Config{
			Collections: []mcpplugin.Resource{{Slug: "missing"}},
		})},
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Text("title"),
		}}},
	}, teststore.New())
	if err == nil || !strings.Contains(err.Error(), `unknown collection "missing"`) {
		t.Fatalf("New error = %v", err)
	}
}

type authorizationTransport struct{ token string }

func (transport authorizationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+transport.token)
	return http.DefaultTransport.RoundTrip(cloned)
}

func decodeStructured(t *testing.T, value any, target any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("decode structured output: %v\n%s", err, encoded)
	}
}

func toolText(result *mcpsdk.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if value, ok := content.(*mcpsdk.TextContent); ok {
			text.WriteString(value.Text)
		}
	}
	return text.String()
}
