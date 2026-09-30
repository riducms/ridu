package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAdminInitialNormalizesRouteIdentityAndClassifiesRedirects(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts/?z=2&a=two&a=one", nil)
	pathname, search := normalizeAdminRouteIdentity(request)
	if pathname != "/admin/collections/posts" || search != "?a=two&a=one&z=2" {
		t.Fatalf("normalized identity = %q%q", pathname, search)
	}
	encodedRequest := httptest.NewRequest(http.MethodGet, "/admin?tilde=~&star=*&space=hello%20world", nil)
	_, encodedSearch := normalizeAdminRouteIdentity(encodedRequest)
	if encodedSearch != "?space=hello+world&star=*&tilde=%7E" {
		t.Fatalf("browser-compatible search identity = %q", encodedSearch)
	}
	oddRequest := httptest.NewRequest(http.MethodGet, "/admin?q=a;b&bad=%FF", nil)
	_, oddSearch := normalizeAdminRouteIdentity(oddRequest)
	if oddSearch != "?bad=%EF%BF%BD&q=a%3Bb" {
		t.Fatalf("WHATWG search identity = %q", oddSearch)
	}
	for raw, want := range map[string]string{
		"a=1&":                       "?a=1",
		"&a=1&&b=2":                  "?a=1&b=2",
		"bad=%FF%FF":                 "?bad=%EF%BF%BD%EF%BF%BD",
		"incomplete=%E2%82":          "?incomplete=%EF%BF%BD",
		"%F0%90%80%80=a&%EE%80%80=b": "?%F0%90%80%80=a&%EE%80%80=b",
	} {
		candidate := httptest.NewRequest(http.MethodGet, "/admin?"+raw, nil)
		_, got := normalizeAdminRouteIdentity(candidate)
		if got != want {
			t.Fatalf("search identity for %q = %q, want %q", raw, got, want)
		}
	}
	if got := redirectQuery("?redirect=%2Fcollections%2Fposts%3Fpage%3D2%23title"); got != "/admin/collections/posts?page=2#title" {
		t.Fatalf("redirect with fragment = %q", got)
	}
	if got := redirectQuery("?redirect=%2Ffoo%5Cbar"); got != "" {
		t.Fatalf("backslash redirect = %q", got)
	}
	if got := redirectQuery("?redirect=%2FLOGIN%2F"); got != "" {
		t.Fatalf("recursive auth redirect = %q", got)
	}
	runtime := &protocol.AdminPreparedRuntimeV1{
		Manifest:                  schema.Snapshot{Application: schema.Application{Admin: &schema.AdminSettings{}}},
		AdminPreparedNavigationV1: protocol.AdminPreparedNavigationV1{CollectionOperations: map[string]protocol.OperationCapabilities{}},
	}
	login := classifyAdminRoute("/admin/collections/posts", "?page=2", runtime, adminBootstrapMetadata{})
	if login.kind != protocol.AdminPreparedRouteLogin || login.location != "/admin/login?redirect=%2Fcollections%2Fposts%3Fpage%3D2" {
		t.Fatalf("anonymous classification = %#v", login)
	}
	login = classifyAdminRoute("/admin/login", "", runtime, adminBootstrapMetadata{})
	if login.surface != "login" {
		t.Fatalf("login surface = %q", login.surface)
	}
	for _, path := range []string{"LOGIN", "FORGOT-PASSWORD", "RESET-PASSWORD", "REQUEST-VERIFICATION", "VERIFY-EMAIL"} {
		if got := classifyAdminRoute("/admin/"+path, "", runtime, adminBootstrapMetadata{}); got.surface != "login" || got.location != "" {
			t.Fatalf("auth route %q = %#v", path, got)
		}
	}
	closedSetup := classifyAdminRoute("/admin/create-first-user", "", runtime, adminBootstrapMetadata{})
	if closedSetup.location != "/admin/login" {
		t.Fatalf("closed setup redirect = %#v", closedSetup)
	}
	runtime.AuthBootstrap = true
	setup := classifyAdminRoute("/admin/login", "", runtime, adminBootstrapMetadata{})
	if setup.location != "/admin/create-first-user" {
		t.Fatalf("setup redirect = %#v", setup)
	}
	setup = classifyAdminRoute("/admin/create-first-user", "", runtime, adminBootstrapMetadata{})
	if setup.surface != "setup" {
		t.Fatalf("setup surface = %q", setup.surface)
	}
	if got := classifyAdminRoute("/admin/CREATE-FIRST-USER", "", runtime, adminBootstrapMetadata{}); got.surface != "setup" {
		t.Fatalf("uppercase setup = %#v", got)
	}
}

func TestAdminInitialFilterNumbersUseJavaScriptStringFormatting(t *testing.T) {
	field := schema.Field{Type: schema.FieldTypeText}
	for number, want := range map[float64]string{
		1e20: "100000000000000000000",
		1e-5: "0.00001",
	} {
		got, ok := adminFilterValue(field, "equals", number)
		if !ok || got != want {
			t.Fatalf("filter value for %g = %#v, %v; want %q, true", number, got, ok, want)
		}
	}
}

func TestAdminInitialClassifiesExactShapesAndPreservesEncodedDocumentIDs(t *testing.T) {
	runtime := &protocol.AdminPreparedRuntimeV1{Manifest: schema.Snapshot{Application: schema.Application{}}, AdminPreparedNavigationV1: protocol.AdminPreparedNavigationV1{CollectionOperations: map[string]protocol.OperationCapabilities{}}}
	metadata := adminBootstrapMetadata{}
	encoded := classifyAdminRoute("/admin/collections/posts/a%2Fb", "", runtime, metadata)
	if encoded.kind != protocol.AdminPreparedRouteCollectionDocument || len(encoded.segments) != 3 || encoded.segments[2] != "a/b" {
		t.Fatalf("encoded document route = %#v", encoded)
	}
	for _, pathname := range []string{
		"/admin/collections/posts/create/extra",
		"/admin/collections/posts/doc/api/extra",
		"/admin/globals/settings/api/extra",
	} {
		if got := classifyAdminRoute(pathname, "", runtime, metadata); got.kind != protocol.AdminPreparedRouteNotFound {
			t.Fatalf("%s classified as %#v", pathname, got)
		}
	}
	invalidRevision := classifyAdminRoute("/admin/globals/settings/versions/not-a-revision", "", runtime, metadata)
	if invalidRevision.kind != protocol.AdminPreparedRouteGlobalVersions {
		t.Fatalf("invalid revision should open history without exact revision: %#v", invalidRevision)
	}
}

func TestAdminInitialRuntimeUsesValidatedURLLocaleForCapabilities(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"}}
	var capabilityLocales []string
	engine, err := operationengine.New(operationengine.Config{
		Store:        teststore.New(),
		Localization: &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []operationengine.Collection{{
			Schema: collection,
			Access: map[operation.Kind]operationengine.Access{operation.Read: func(ctx operationengine.Context) (operationengine.Decision, error) {
				capabilityLocales = append(capabilityLocales, string(ctx.Locale))
				return operationengine.Decision{Kind: operationengine.Allow}, nil
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{config: Config{
		Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{
			Version:     schema.CurrentVersion,
			Application: schema.Application{Name: "localized", Localization: &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}},
			Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
		}),
		GetPreference: func(context.Context, *AuthIdentity, string) (json.RawMessage, error) {
			return json.RawMessage(`{"locale":"removed"}`), nil
		},
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts?locale=fr", nil)
	runtime, _, _, err := api.prepareAdminRuntime(t.Context(), request, "0123456789abcdef01234567")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ContentLocale != "fr" {
		t.Fatalf("content locale = %q", runtime.ContentLocale)
	}
	for _, locale := range capabilityLocales {
		if locale != "fr" {
			t.Fatalf("capability locale = %q, all = %v", locale, capabilityLocales)
		}
	}
}

func TestAdminInitialSeedabilityHonorsExactReplacedViews(t *testing.T) {
	metadata := adminBootstrapMetadata{
		SeedableCoreSurfaces: []string{"collectionList", "global", "setup"},
		ReplacedCoreViews:    []string{"collectionList:posts", "global:settings", "login:*"},
	}
	for _, test := range []struct {
		route adminRouteClassification
		want  bool
	}{
		{adminRouteClassification{surface: "collectionList", segments: []string{"collections", "posts"}}, false},
		{adminRouteClassification{surface: "collectionList", segments: []string{"collections", "pages"}}, true},
		{adminRouteClassification{surface: "global", segments: []string{"globals", "settings"}}, false},
		{adminRouteClassification{surface: "global", segments: []string{"globals", "branding"}}, true},
		{adminRouteClassification{surface: "login", segments: []string{"login"}}, false},
		{adminRouteClassification{surface: "setup", segments: []string{"create-first-user"}}, true},
		{adminRouteClassification{}, true},
	} {
		if got := seedableSurface(metadata, test.route); got != test.want {
			t.Fatalf("seedableSurface(%#v) = %v, want %v", test.route, got, test.want)
		}
	}
}

func TestAdminInitialRejectsBuildIDThatDoesNotMatchMetadata(t *testing.T) {
	assets := adminInitialAssets(t)
	var metadata adminBootstrapMetadata
	if err := json.Unmarshal(assets["ridu-admin-bootstrap.json"].Data, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.SeedableCoreSurfaces = metadata.SeedableCoreSurfaces[:len(metadata.SeedableCoreSurfaces)-1]
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	assets["ridu-admin-bootstrap.json"] = &fstest.MapFile{Data: encoded}
	api := &API{config: Config{AdminAssets: assets}}
	if _, err := api.adminBootstrapMetadata(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("metadata error = %v", err)
	}
}

func TestAdminHTMLPrefixPlacesBuildIdentityAndPreloadsBeforeTheEntry(t *testing.T) {
	const entry = `<script type="module" src="/admin/assets/entry.js"></script>`
	metadata := adminBootstrapMetadata{
		BuildID: "0123456789abcdef01234567",
		ModuleGroups: map[string][]string{
			"entry":    {"assets/entry.js", "assets/theme.css"},
			"document": {"assets/document.js", "assets/theme.css"},
		},
	}
	for name, index := range map[string]string{
		"head entry": `<html><head><title>Admin</title>` + entry + `</head><body></body></html>`,
		"body entry": `<html><head><title>Admin</title></head><body>` + entry + `</body></html>`,
	} {
		t.Run(name, func(t *testing.T) {
			prefix, suffix, ok := adminHTMLPrefix([]byte(index), metadata, protocol.AdminPreparedRouteStateV1{
				ModuleGroups: []string{"entry", "document"},
			})
			if !ok || !strings.HasPrefix(string(suffix), entry) {
				t.Fatal("HTML did not split before the entry module")
			}
			page := string(prefix) + string(suffix)
			for _, insertion := range []string{
				`<meta name="ridu-admin-build-id" content="` + metadata.BuildID + `" />`,
				`<link rel="modulepreload" href="/admin/assets/entry.js" />`,
				`<link rel="modulepreload" href="/admin/assets/document.js" />`,
				`<link rel="preload" as="style" href="/admin/assets/theme.css" />`,
			} {
				position := strings.Index(page, insertion)
				if position < 0 || position >= strings.Index(page, "</head>") || position >= len(prefix) {
					t.Fatalf("insertion must precede the entry in <head>: %s", insertion)
				}
				if strings.Count(page, insertion) != 1 {
					t.Fatalf("insertion duplicated: %s", insertion)
				}
			}
		})
	}
}

func TestAdminInitialHTMLIsPrivateEscapedAndPreparedWithoutDuplicateRead(t *testing.T) {
	var afterRead int
	collection := schema.Collection{
		ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Fields: []schema.Field{},
	}
	engine, err := operationengine.New(operationengine.Config{
		Store: teststore.New(), Collections: []operationengine.Collection{{
			Schema: collection,
			Hooks: operationengine.Hooks{AfterRead: []operationengine.Hook{func(operationengine.Context) error {
				afterRead++
				return nil
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), operationengine.Request{Operation: operation.Create, Collection: "posts", ImportID: "post-1"}); err != nil {
		t.Fatal(err)
	}
	afterRead = 0
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "hostile </template><script>alert(1)</script>"},
			Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
		}),
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts?z=2&a=two&a=one", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("cache control = %q", response.Header().Get("Cache-Control"))
	}
	for _, vary := range []string{"Accept", "Cookie", "Authorization", "Ridu-Admin-Context"} {
		if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), vary) {
			t.Fatalf("Vary is missing %s: %v", vary, response.Header().Values("Vary"))
		}
	}
	body := response.Body.String()
	if strings.Contains(body, "</template><script>alert") || !strings.Contains(body, `hostile \u003c/template\u003e\u003cscript\u003ealert`) {
		t.Fatalf("embedded state was not safely escaped: %s", body)
	}
	state := decodeAdminInitialTemplate(t, body)
	if state.Outcome != protocol.AdminPreparedRoutePrepared || state.Search != "?a=two&a=one&z=2" || state.Route == nil || state.Route.Data == nil || state.Route.Data.Page.Value == nil {
		t.Fatalf("prepared state = %#v", state)
	}
	if afterRead != 1 {
		t.Fatalf("after-read hook calls = %d, want 1", afterRead)
	}
}

func TestAdminInitialHEADDoesNoRuntimeOrRouteWork(t *testing.T) {
	sessions := 0
	handler := New(Config{
		AdminAssets: adminInitialAssets(t),
		Session: func(_ context.Context, _ string) (AuthSession, error) {
			sessions++
			return AuthSession{}, nil
		},
	})
	request := httptest.NewRequest(http.MethodHead, "/admin/collections/posts", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || sessions != 0 {
		t.Fatalf("HEAD status=%d body=%d sessions=%d", response.Code, response.Body.Len(), sessions)
	}
}

func TestAdminInitialMissingMetadataStillEmbedsPreparedRuntime(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Data: []byte(`<!doctype html><html><head></head><body><div id="app"></div><script type="module" src="/admin/assets/app.js"></script></body></html>`)},
		"assets/app.js": {Data: []byte("export {}")},
	}
	handler := New(Config{AdminAssets: assets, Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "runtime fallback"}, Plugins: []schema.Plugin{}})})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
	state := decodeAdminInitialTemplate(t, response.Body.String())
	if state.Outcome != protocol.AdminPreparedRouteFallback || state.Runtime == nil || state.Runtime.Manifest.Application.Name != "runtime fallback" || state.ContextKey == "" {
		t.Fatalf("metadata fallback = %#v", state)
	}
	navigation := httptest.NewRequest(http.MethodGet, "/admin", nil)
	navigation.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	navigation.Header.Set("Ridu-Admin-Context", "stale-context")
	navigationResponse := httptest.NewRecorder()
	handler.ServeHTTP(navigationResponse, navigation)
	var reload protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(navigationResponse.Body.Bytes(), &reload); err != nil {
		t.Fatal(err)
	}
	if reload.Outcome != protocol.AdminPreparedRouteReload || reload.Runtime != nil || reload.Diagnostic == nil {
		t.Fatalf("metadata fallback context mismatch = %#v", reload)
	}
}

func TestAdminInitialPreferenceFailureFallsBackWithoutPartialRuntime(t *testing.T) {
	users := schema.Collection{
		ID: "users", Slug: "users", Labels: schema.CollectionLabels{Singular: "User", Plural: "Users"},
		Capabilities: schema.Capabilities{Auth: true}, Auth: &schema.AuthSettings{},
	}
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{Schema: users}}})
	if err != nil {
		t.Fatal(err)
	}
	requestErrors := 0
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "preferences", Admin: &schema.AdminSettings{UserCollectionID: "users", UserCollectionSlug: "users"}},
			Collections: []schema.Collection{users}, Plugins: []schema.Plugin{},
		}),
		Session: func(context.Context, string) (AuthSession, error) {
			return AuthSession{ID: "session-1", Collection: "users", User: store.Document{ID: "actor"}}, nil
		},
		GetPreference: func(context.Context, *AuthIdentity, string) (json.RawMessage, error) {
			return nil, errors.New("preference store unavailable")
		},
		RequestError: func(RequestErrorEvent) { requestErrors++ },
	})
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "opaque"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Outcome != protocol.AdminPreparedRouteFallback || state.Runtime != nil || state.Route != nil || state.Diagnostic == nil || state.Diagnostic.Code != "runtime_failed" {
		t.Fatalf("preference failure state = %#v", state)
	}
	if requestErrors != 1 {
		t.Fatalf("request errors = %d, want 1", requestErrors)
	}
}

func TestAdminInitialColdRuntimePanicReturnsHTMLFallback(t *testing.T) {
	requestErrors := 0
	handler := New(Config{
		AdminAssets: adminInitialAssets(t),
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "panic"}, Plugins: []schema.Plugin{},
		}),
		ManifestForRequest: func(context.Context, *AuthIdentity) (schema.Snapshot, error) {
			panic("runtime panic")
		},
		RequestError: func(RequestErrorEvent) { requestErrors++ },
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("cold panic response = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	state := decodeAdminInitialTemplate(t, response.Body.String())
	if state.Outcome != protocol.AdminPreparedRouteFallback || state.Diagnostic == nil || state.Diagnostic.Code != "runtime_failed" {
		t.Fatalf("cold panic fallback = %#v", state)
	}
	if requestErrors != 1 {
		t.Fatalf("request errors = %d, want 1", requestErrors)
	}
}

func TestAdminInitialExplicitSessionAuthorizationPrecedesCookie(t *testing.T) {
	var tokens []string
	api := &API{config: Config{
		Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "identity"}, Plugins: []schema.Plugin{}}),
		Session: func(_ context.Context, token string) (AuthSession, error) {
			tokens = append(tokens, token)
			return AuthSession{ID: token, Collection: "users", User: store.Document{ID: token}}, nil
		},
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("Authorization", "Session header-session")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "cookie-session"})
	identity := api.resolveAdminPreparedIdentity(request)
	if identity.session == nil || identity.session.ID != "header-session" || len(tokens) != 1 || tokens[0] != "header-session" {
		t.Fatalf("resolved identity = %#v, tokens = %v", identity, tokens)
	}
}

func TestAdminInitialSecurityReusesOneResolvedSession(t *testing.T) {
	users := schema.Collection{
		ID: "users", Slug: "users", Labels: schema.CollectionLabels{Singular: "User", Plural: "Users"},
		Capabilities: schema.Capabilities{Auth: true}, Auth: &schema.AuthSettings{APIKeys: true},
	}
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{Schema: users}}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, sessionReads, keyReads := 0, 0, 0
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{
			Version:     schema.CurrentVersion,
			Application: schema.Application{Name: "security", Admin: &schema.AdminSettings{UserCollectionID: "users", UserCollectionSlug: "users"}},
			Collections: []schema.Collection{users}, Plugins: []schema.Plugin{},
		}),
		Session: func(context.Context, string) (AuthSession, error) {
			resolved++
			return AuthSession{
				ID: "session-1", Collection: "users", User: store.Document{ID: "actor"}, ExpiresAt: time.Now().Add(time.Hour),
				PreparedSessions: func(context.Context) ([]AuthSessionInfo, error) {
					sessionReads++
					return []AuthSessionInfo{{ID: "session-1", Current: true}}, nil
				},
				PreparedAPIKeys: func(context.Context) ([]APIKeyInfo, error) {
					keyReads++
					return []APIKeyInfo{{ID: "key-1", Name: "CLI"}}, nil
				},
			}, nil
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/account/security", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "opaque"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if resolved != 1 || sessionReads != 1 || keyReads != 1 || state.Route == nil || state.Route.Security == nil || state.Route.Security.Sessions.Value == nil || state.Route.Security.APIKeys.Value == nil {
		t.Fatalf("resolved=%d sessions=%d keys=%d state=%#v", resolved, sessionReads, keyReads, state)
	}
}

func TestAdminInitialOversizeFallbackDropsContent(t *testing.T) {
	state := protocol.AdminPreparedRouteStateV1{
		Version: protocol.AdminPreparedRouteStateVersion, Outcome: protocol.AdminPreparedRouteRedirect,
		Location: "/admin/login?redirect=%2Fcollections%2Fposts",
		Runtime: &protocol.AdminPreparedRuntimeV1{Preferences: map[string]json.RawMessage{
			"large": json.RawMessage(`"` + strings.Repeat("x", maxAdminPreparedRouteBytes) + `"`),
		}},
	}
	encoded := marshalBoundedAdminState(state)
	if len(encoded) > maxAdminPreparedRouteBytes {
		t.Fatalf("fallback size = %d", len(encoded))
	}
	var decoded protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Outcome != protocol.AdminPreparedRouteFallback || decoded.Runtime != nil || decoded.Route != nil || decoded.Location != "" || decoded.Diagnostic == nil {
		t.Fatalf("oversize fallback = %#v", decoded)
	}
}

func TestAdminInitialDocumentUsesOneSessionAndPreservesRedactionHooksAndAudit(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	secretPath, _ := query.NewPath("secret")
	title := schema.Field{ID: "title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}}
	secret := schema.Field{ID: "secret", Name: "secret", Path: secretPath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}}
	collection := schema.Collection{
		ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Fields: []schema.Field{title, secret}, DocumentLock: &schema.DocumentLockSettings{DurationSeconds: 120},
	}
	afterRead := 0
	engine, err := operationengine.New(operationengine.Config{
		Store: teststore.New(), Collections: []operationengine.Collection{{
			Schema: collection,
			Hooks: operationengine.Hooks{AfterRead: []operationengine.Hook{func(operationengine.Context) error {
				afterRead++
				return nil
			}}},
			Bindings: []operationengine.FieldBinding{{
				ID: "secret", Field: secret,
				Access: operationengine.FieldRules{Read: func(operationengine.Context) (bool, error) { return false, nil }},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), operationengine.Request{
		Operation: operation.Create, Collection: "posts", ImportID: "post-1",
		Data: store.Values{"title": store.String("Visible"), "secret": store.String("hidden")},
	}); err != nil {
		t.Fatal(err)
	}
	afterRead = 0
	sessions := 0
	var audits []AuditEvent
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "document"}, Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{}}),
		Session: func(context.Context, string) (AuthSession, error) {
			sessions++
			return AuthSession{Collection: "users", User: store.Document{ID: "actor"}}, nil
		},
		Audit: func(event AuditEvent) { audits = append(audits, event) },
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts/post-1", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "opaque-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || afterRead != 1 || state.Route == nil || state.Route.Document == nil || state.Route.Document.Document.Value == nil {
		t.Fatalf("sessions=%d afterRead=%d state=%#v", sessions, afterRead, state)
	}
	if len(audits) != 1 || audits[0].Action != "read" || strings.Contains(audits[0].Action, "lock") {
		t.Fatalf("audits = %#v", audits)
	}
	document := decodedDocument(t, *state.Route.Document.Document.Value)
	if document["title"] != "Visible" || document["secret"] != nil {
		t.Fatalf("redacted prepared document = %#v", document)
	}
	initialRoute, _ := json.Marshal(state.Route)
	request.Header.Set("Ridu-Admin-Context", state.ContextKey)
	sessions, afterRead = 0, 0
	audits = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var navigation protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &navigation); err != nil {
		t.Fatal(err)
	}
	navigationRoute, _ := json.Marshal(navigation.Route)
	if navigation.Runtime != nil || navigation.Navigation == nil || string(initialRoute) != string(navigationRoute) {
		t.Fatalf("compact navigation changed the prepared document: %#v", navigation)
	}
	if sessions != 1 || afterRead != 1 || len(audits) != 1 || audits[0].Action != "read" {
		t.Fatalf("navigation lifecycles: sessions=%d afterRead=%d audits=%#v", sessions, afterRead, audits)
	}
	request = httptest.NewRequest(http.MethodGet, "/admin/collections/posts/post-1/api?select=%7B%22id%22%3Atrue%7D&depth=0", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "opaque-session"})
	sessions, afterRead, audits = 0, 0, nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var apiState protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &apiState); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || afterRead != 1 || len(audits) != 1 || apiState.Route == nil || apiState.Route.Document == nil || apiState.Route.Document.Document.Value == nil {
		t.Fatalf("API lifecycle: sessions=%d afterRead=%d audits=%#v state=%s", sessions, afterRead, audits, response.Body.String())
	}
	apiDocument := decodedDocument(t, *apiState.Route.Document.Document.Value)
	if apiDocument["title"] != "Visible" || apiDocument["secret"] != nil || strings.Count(response.Body.String(), `"title":"Visible"`) != 1 {
		t.Fatalf("API view must reuse the authorized document read and ignore arbitrary admin projection: %#v", apiState.Route)
	}
}

func TestAdminInitialVersionsRetainIndependentReadsAndExactRevisionErrors(t *testing.T) {
	path, _ := query.NewPath("title")
	collection := schema.Collection{
		ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{Drafts: true},
		Fields: []schema.Field{{ID: "title", Name: "title", Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}}},
	}
	var reads []operation.Kind
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), EditorCollection: "users", Collections: []operationengine.Collection{{
		Schema: collection,
		Hooks: operationengine.Hooks{BeforeRead: []operationengine.Hook{func(ctx operationengine.Context) error {
			reads = append(reads, ctx.Operation)
			if ctx.ActorCollection != "users" || ctx.Actor == nil || ctx.Actor.ID != "actor" {
				t.Errorf("read identity = %s %#v", ctx.ActorCollection, ctx.Actor)
			}
			return nil
		}}},
	}, adminEditorCollection()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), operationengine.Request{Operation: operation.Create, Collection: "posts", ImportID: "one", Data: store.Values{"title": store.String("Version one")}}); err != nil {
		t.Fatal(err)
	}
	sessions, audits := 0, 0
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "versions"}, Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{}}),
		Session: func(context.Context, string) (AuthSession, error) {
			sessions++
			return AuthSession{Collection: "users", User: store.Document{ID: "actor"}}, nil
		},
		Audit: func(AuditEvent) { audits++ },
		SchedulePublish: func(context.Context, string, string, time.Time, string, int, *AuthIdentity) (store.ScheduledPublication, error) {
			t.Fatal("GET must not schedule")
			return store.ScheduledPublication{}, nil
		},
		CancelScheduledPublication: func(context.Context, string, string, string, *AuthIdentity) error {
			t.Fatal("GET must not cancel")
			return nil
		},
	})
	for _, revision := range []string{"1", "999", "0", "-1", "not-a-revision"} {
		sessions, audits, reads = 0, 0, nil
		request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts/one/versions/"+revision, nil)
		request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "opaque"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var state protocol.AdminPreparedRouteStateV1
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.Outcome != protocol.AdminPreparedRoutePrepared || state.Route == nil || state.Route.Versions == nil {
			t.Fatalf("state = %s", response.Body.String())
		}
		data := state.Route.Versions
		exact := revision == "1" || revision == "999"
		wantReads := 2
		if exact {
			wantReads++
		}
		if sessions != 1 || audits != 1 || len(reads) != wantReads || reads[0] != operation.ReadVersions || reads[1] != operation.Read || (exact && reads[2] != operation.ReadVersions) {
			t.Fatalf("lifecycles: sessions=%d audits=%d reads=%v", sessions, audits, reads)
		}
		if data.History.Value == nil || len(*data.History.Value) != 1 || data.Document.Document.Value == nil || data.Document.Access.Value == nil || (exact != (data.Detail != nil)) {
			t.Fatalf("incomplete semantic versions: %#v", data)
		}
		if revision == "1" && (data.Detail.Value == nil || data.Detail.Value.Revision != 1) {
			t.Fatalf("exact version = %#v", data.Detail)
		}
		if revision == "999" && (data.Detail.Error == nil || data.Detail.Error.Code != protocol.ErrorNotFound) {
			t.Fatalf("missing exact version must be a named read error: %#v", data.Detail)
		}
	}
}

func TestAdminInitialVersionsKeepHistoryLightAndDetailExact(t *testing.T) {
	path, _ := query.NewPath("title")
	localization := &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr"}}}
	collection := schema.Collection{
		ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{Drafts: true},
		Fields: []schema.Field{{ID: "title", Name: "title", Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Localized: true, Text: &schema.TextField{}}},
	}
	engine, err := operationengine.New(operationengine.Config{
		Store: teststore.New(), Localization: localization, EditorCollection: "users",
		Collections: []operationengine.Collection{{Schema: collection}, adminEditorCollection()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(t.Context(), operationengine.Request{
		Operation: operation.Create, Collection: "posts", ImportID: "one", Locale: "en",
		Data: store.Values{"title": store.String("English only")},
	}); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Session: func(context.Context, string) (AuthSession, error) {
			return AuthSession{Collection: "users", User: store.Document{ID: "actor"}}, nil
		},
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "versions", Localization: localization},
			Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
		}),
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts/one/versions/1?locale=en", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "opaque"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Route == nil || state.Route.Versions == nil {
		t.Fatalf("state = %s", response.Body.String())
	}
	data := state.Route.Versions
	if data.History.Value == nil || data.Detail == nil || data.Detail.Value == nil {
		t.Fatalf("versions = %s", response.Body.String())
	}
	if title := decodedDocument(t, (*data.History.Value)[0].Snapshot)["title"]; title != "English only" {
		t.Fatalf("history should retain only the route locale: %s", (*data.History.Value)[0].Snapshot)
	}
	title, ok := decodedDocument(t, data.Detail.Value.Snapshot)["title"].(map[string]any)
	if !ok || title["en"] != "English only" || title["fr"] != nil {
		t.Fatalf("exact comparison detail must preserve missing French independently: %s", data.Detail.Value.Snapshot)
	}
	if data.Document.Document.Value == nil || decodedDocument(t, *data.Document.Document.Value)["title"] != "English only" {
		t.Fatalf("document heading must retain its ordinary localized read: %#v", data.Document)
	}
}

func TestAdminInitialNavigationReusesRuntimeButResolvesLocaleAndCapabilities(t *testing.T) {
	localization := &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr"}}}
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{}}
	engine, err := operationengine.New(operationengine.Config{
		Store: teststore.New(), Localization: localization,
		Collections: []operationengine.Collection{{Schema: collection,
			Access: map[operation.Kind]operationengine.Access{operation.Create: func(ctx operationengine.Context) (operationengine.Decision, error) {
				if ctx.Locale == "fr" {
					return operationengine.Decision{Kind: operationengine.Deny}, nil
				}
				return operationengine.Decision{Kind: operationengine.Allow}, nil
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := schema.Snapshot{Version: schema.CurrentVersion,
		Application: schema.Application{Name: strings.Repeat("manifest", 4096), Localization: localization},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	}
	assets := adminInitialAssets(t)
	var metadata adminBootstrapMetadata
	if err := json.Unmarshal(assets["ridu-admin-bootstrap.json"].Data, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.ExtensionRoutes = []string{"plugin-only"}
	metadata.BuildID = adminBootstrapBuildID(metadata)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	assets["ridu-admin-bootstrap.json"].Data = encoded
	handler := New(Config{AdminAssets: assets, Engine: engine, Manifest: schema.NewManifest(snapshot),
		ManifestForRequest: func(context.Context, *AuthIdentity) (schema.Snapshot, error) { return snapshot, nil },
	})
	readState := func(path, contextKey, accept string) (protocol.AdminPreparedRouteStateV1, int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", accept)
		request.Header.Set("Ridu-Admin-Context", contextKey)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if accept != protocol.AdminPreparedRouteStateMediaType {
			return decodeAdminInitialTemplate(t, response.Body.String()), response.Body.Len()
		}
		var state protocol.AdminPreparedRouteStateV1
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		return state, response.Body.Len()
	}
	initial, fullBytes := readState("/admin/collections/posts?locale=en", "", protocol.AdminPreparedRouteStateMediaType)
	if initial.Runtime == nil || initial.Navigation != nil || !initial.Runtime.CollectionOperations["posts"].Create {
		t.Fatalf("missing full runtime: %#v", initial)
	}
	navigation, compactBytes := readState("/admin/collections/posts?locale=fr", initial.ContextKey, protocol.AdminPreparedRouteStateMediaType)
	if navigation.Outcome != protocol.AdminPreparedRoutePrepared || navigation.Runtime != nil || navigation.Navigation == nil || navigation.ContextKey != initial.ContextKey || navigation.Navigation.ContentLocale != "fr" || navigation.Navigation.CollectionOperations["posts"].Create {
		t.Fatalf("navigation failed to refresh locale/capabilities: %#v", navigation)
	}
	if compactBytes >= fullBytes/5 {
		t.Fatalf("compact=%d full=%d", compactBytes, fullBytes)
	}
	cold, _ := readState("/admin/collections/posts?locale=fr", initial.ContextKey, "text/html")
	if cold.Runtime == nil || cold.Navigation != nil {
		t.Fatalf("HTML omitted its runtime: %#v", cold)
	}
	fallback, _ := readState("/admin/plugin-only", initial.ContextKey, protocol.AdminPreparedRouteStateMediaType)
	if fallback.Outcome != protocol.AdminPreparedRouteFallback || fallback.Runtime != nil || fallback.Navigation == nil || fallback.Route != nil {
		t.Fatalf("fallback state: %#v", fallback)
	}
	snapshot.Application.Name = "changed manifest"
	reload, _ := readState("/admin/collections/posts?locale=en", initial.ContextKey, protocol.AdminPreparedRouteStateMediaType)
	if reload.Outcome != protocol.AdminPreparedRouteReload || reload.Runtime != nil || reload.Navigation != nil || reload.Route != nil {
		t.Fatalf("manifest change did not reload: %#v", reload)
	}
}

func TestAdminInitialNavigationOversizeFallbackDropsContent(t *testing.T) {
	state := protocol.AdminPreparedRouteStateV1{
		Outcome:    protocol.AdminPreparedRoutePrepared,
		Navigation: &protocol.AdminPreparedNavigationV1{ContentLocale: "fr"},
		Route:      &protocol.AdminPreparedRouteDataV1{Kind: protocol.AdminPreparedRouteCollectionCreate, Create: &protocol.AdminCreateDataV1{Values: map[string]any{"content": strings.Repeat("x", maxAdminPreparedRouteBytes)}}},
	}
	var decoded protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(marshalBoundedAdminState(state), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Outcome != protocol.AdminPreparedRouteFallback || decoded.Runtime != nil || decoded.Navigation != nil || decoded.Route != nil || decoded.Diagnostic.Code != "snapshot_too_large" {
		t.Fatalf("oversize navigation: %#v", decoded)
	}
}

func TestAdminInitialListDoesNotRunUnusedStatusCountReads(t *testing.T) {
	statusPath, _ := query.NewPath("status")
	status := schema.Field{
		ID: "status", Name: "status", Path: statusPath, Type: schema.FieldTypeSelect, Category: schema.FieldCategoryScalar,
		Select: &schema.SelectField{Options: []schema.SelectOption{{Value: "draft", Label: "Draft"}, {Value: "published", Label: "Published"}}},
	}
	collection := schema.Collection{ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"}, Fields: []schema.Field{status}}
	afterRead := 0
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{
		Schema: collection, Hooks: operationengine.Hooks{AfterRead: []operationengine.Hook{func(operationengine.Context) error {
			afterRead++
			return nil
		}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range []string{"draft", "published"} {
		if _, err := engine.Execute(t.Context(), operationengine.Request{Operation: operation.Create, Collection: "posts", ImportID: "post-" + strconv.Itoa(index), Data: store.Values{"status": store.String(value)}}); err != nil {
			t.Fatal(err)
		}
	}
	afterRead = 0
	handler := New(Config{AdminAssets: adminInitialAssets(t), Engine: engine, Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "counts"}, Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{}})})
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts?status=draft", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Route == nil || state.Route.Data == nil || state.Route.Data.Page.Value == nil || len(state.Route.Data.Counts) != 0 {
		t.Fatalf("list data = %#v", state.Route)
	}
	if afterRead != 1 {
		t.Fatalf("after-read calls = %d, want 1 (the visible list only)", afterRead)
	}
}

func TestAdminInitialListCellsKeepCorePreparationAndMatchColumnPopulation(t *testing.T) {
	field := func(name string, kind schema.FieldType) schema.Field {
		path, err := query.NewPath(name)
		if err != nil {
			t.Fatal(err)
		}
		return schema.Field{ID: schema.StableID(name), Name: name, Path: path, Type: kind, Category: schema.FieldCategoryScalar}
	}
	custom := field("custom", schema.FieldTypeRelationship)
	custom.Relationship = &schema.RelationshipField{CollectionID: "authors", CollectionSlug: "authors"}
	standard := field("standard", schema.FieldTypeRelationship)
	standard.Relationship = &schema.RelationshipField{CollectionID: "authors", CollectionSlug: "authors"}
	other := field("other", schema.FieldTypeRelationship)
	other.Relationship = &schema.RelationshipField{CollectionID: "authors", CollectionSlug: "authors"}
	posts := schema.Collection{
		ID: "posts", Slug: "posts", Fields: []schema.Field{field("title", schema.FieldTypeText), custom, standard, other},
		Capabilities: schema.Capabilities{Trash: true},
		Admin:        schema.CollectionAdmin{DefaultColumns: []string{"custom", "other"}},
	}
	authors := schema.Collection{ID: "authors", Slug: "authors", Fields: []schema.Field{field("name", schema.FieldTypeText)}}
	snapshot := schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "cells"}, Collections: []schema.Collection{posts, authors}, Plugins: []schema.Plugin{}}
	for _, test := range []struct {
		name      string
		workspace json.RawMessage
		query     url.Values
		column    string
	}{
		{name: "defaults", column: "other"},
		{name: "workspace", workspace: json.RawMessage(`{"columns":[{"path":"custom","active":true},{"path":"standard","active":true}]}`), column: "standard"},
		{name: "mixed workspace entries", workspace: json.RawMessage(`{"columns":[{"path":"custom","active":true},7,{"path":"standard","active":true}]}`), column: "standard"},
		{name: "case sensitive workspace key", workspace: json.RawMessage(`{"Columns":["standard"]}`), column: "other"},
		{name: "ignore unrelated workspace key", workspace: json.RawMessage(`{"columns":[{"path":"standard","active":true}],"Columns":[]}`), column: "standard"},
		{name: "query", query: url.Values{"columns": {"custom,standard"}}, column: "standard"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := json.Marshal(adminListPopulation(snapshot, posts, test.workspace, test.query, []string{"custom"}))
			want, _ := json.Marshal(map[string]any{test.column: map[string]any{"select": map[string]any{"id": true, "name": true}}})
			if string(got) != string(want) {
				t.Fatalf("population = %s, want %s", got, want)
			}
		})
	}
	assets := adminInitialAssets(t)
	var metadata adminBootstrapMetadata
	if err := json.Unmarshal(assets["ridu-admin-bootstrap.json"].Data, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.ListCellFields = map[string][]string{"posts": {"custom"}}
	metadata.BuildID = adminBootstrapBuildID(metadata)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	assets["ridu-admin-bootstrap.json"].Data = encoded
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{Schema: posts}, {Schema: authors}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Config{AdminAssets: assets, Engine: engine, Manifest: schema.NewManifest(snapshot)})
	for _, suffix := range []string{"", "/trash"} {
		request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts"+suffix+"?columns=custom,standard", nil)
		request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var state protocol.AdminPreparedRouteStateV1
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.Outcome != protocol.AdminPreparedRoutePrepared || state.Route == nil || state.Route.Data == nil {
			t.Fatalf("custom-cell route = %#v", state)
		}
		if data := state.Route.Data; data.Page.Value == nil || data.Query.Trash != (suffix != "") {
			t.Fatalf("custom-cell list data = %#v", data)
		}
	}
}

func TestAdminInitialNavigationStateReturnsReloadForContextMismatch(t *testing.T) {
	handler := New(Config{
		AdminAssets: adminInitialAssets(t),
		Manifest: schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "context"}, Plugins: []schema.Plugin{},
		}),
	})
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Content-Type") != protocol.AdminPreparedRouteStateMediaType || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("prepared headers = %#v", response.Header())
	}
	var initial protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Outcome != protocol.AdminPreparedRoutePrepared || initial.ContextKey == "" {
		t.Fatalf("initial state = %#v", initial)
	}
	reloadRequest := httptest.NewRequest(http.MethodGet, "/admin", nil)
	reloadRequest.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	reloadRequest.Header.Set("Ridu-Admin-Context", "stale-context")
	reloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(reloadResponse, reloadRequest)
	var reload protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(reloadResponse.Body.Bytes(), &reload); err != nil {
		t.Fatal(err)
	}
	if reload.Outcome != protocol.AdminPreparedRouteReload || reload.Runtime != nil || reload.Diagnostic == nil {
		t.Fatalf("context mismatch = %#v", reload)
	}
}

func TestAdminInitialConditionalModuleGroups(t *testing.T) {
	datePath, _ := query.NewPath("startsAt")
	dateField := schema.Field{ID: "starts-at", Name: "startsAt", Path: datePath, Type: schema.FieldTypeDate, Category: schema.FieldCategoryScalar, Date: &schema.DateField{Format: schema.DateTime}}
	plain := schema.Collection{ID: "plain", Slug: "plain", Fields: []schema.Field{}}
	media := schema.Collection{ID: "media", Slug: "media", Capabilities: schema.Capabilities{Upload: true}, Fields: []schema.Field{dateField}}
	users := schema.Collection{ID: "users", Slug: "users", Capabilities: schema.Capabilities{Auth: true}, Auth: &schema.AuthSettings{}, Fields: []schema.Field{dateField}}
	runtime := &protocol.AdminPreparedRuntimeV1{Manifest: schema.Snapshot{Collections: []schema.Collection{plain, media, users}}}
	runtime.Manifest.Application.Admin = &schema.AdminSettings{UserCollectionID: "users", UserCollectionSlug: "users"}
	for _, test := range []struct {
		route adminRouteClassification
		want  string
	}{
		{adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionCreate, surface: "collectionCreate", segments: []string{"collections", "plain", "create"}}, "document"},
		{adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionDocument, surface: "collectionEdit", segments: []string{"collections", "media", "asset-1"}}, "document,date,upload-preview"},
		{adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionAPI, surface: "collectionEdit", segments: []string{"collections", "media", "asset-1", "api"}}, "document,date,document-api"},
		{adminRouteClassification{kind: protocol.AdminPreparedRouteSetup, surface: "setup"}, "date"},
		{adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionVersions}, "versions"},
		{adminRouteClassification{kind: protocol.AdminPreparedRouteGlobalVersions}, "versions"},
		{adminRouteClassification{kind: protocol.AdminPreparedRouteUpload}, "bulk-upload"},
	} {
		if got := strings.Join(adminRouteModuleGroups(test.route, runtime.Manifest), ","); got != test.want {
			t.Fatalf("module groups for %#v = %q, want %q", test.route, got, test.want)
		}
	}
}

func TestAdminInitialMediaTypeAndQueryMatchBrowserSemantics(t *testing.T) {
	if acceptsMediaType(protocol.AdminPreparedRouteStateMediaType+";q=0", protocol.AdminPreparedRouteStateMediaType) {
		t.Fatal("q=0 must not opt into route-state JSON")
	}
	if !acceptsMediaType("text/html, "+protocol.AdminPreparedRouteStateMediaType+"; q=0.5", protocol.AdminPreparedRouteStateMediaType) {
		t.Fatal("positive quality must opt into route-state JSON")
	}
	for encoded, want := range map[string]float64{"0x10": 16, "0o10": 8, "0b10": 2, "1e2": 100} {
		got, err := adminJavaScriptNumber(encoded)
		if err != nil || got != want {
			t.Fatalf("adminJavaScriptNumber(%q) = %v, %v; want %v", encoded, got, err, want)
		}
	}
	if got, err := adminJavaScriptNumber("  "); err != nil || got != 0 {
		t.Fatalf("whitespace JavaScript number = %v, %v", got, err)
	}
	if got, err := adminJavaScriptNumber("0x10000000000000000"); err != nil || got != 18446744073709551616 {
		t.Fatalf("wide prefixed JavaScript number = %v, %v", got, err)
	}
	for encoded, want := range map[string]int{"0x32": 50, "0o12": 10, "0b10": 2, "1e2": 100} {
		got, valid := adminIntegerQuery(encoded)
		if !valid || got != want {
			t.Fatalf("adminIntegerQuery(%q) = %d, %t; want %d", encoded, got, valid, want)
		}
	}
	localized := &protocol.AdminPreparedRuntimeV1{Manifest: schema.Snapshot{Application: schema.Application{Localization: &schema.LocalizationSettings{
		DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr"}},
	}}}, AdminPreparedNavigationV1: protocol.AdminPreparedNavigationV1{ContentLocale: "en"}}
	if got := adminRouteLocale(localized, url.Values{"locale": {"fr"}}); got != "fr" {
		t.Fatalf("valid route locale = %q", got)
	}
	if got := adminRouteLocale(localized, url.Values{"locale": {"invalid"}}); got != "en" {
		t.Fatalf("invalid route locale fallback = %q", got)
	}
	localized.Manifest.Application.Localization = nil
	if got := adminRouteLocale(localized, url.Values{"locale": {"fr"}}); got != "" {
		t.Fatalf("non-localized route locale = %q", got)
	}
	statusPath, _ := query.NewPath("status")
	radio := schema.Field{ID: "status", Name: "status", Path: statusPath, Type: schema.FieldTypeRadio, Select: &schema.SelectField{Options: []schema.SelectOption{{Value: "draft"}}}}
	if name, _ := adminStatusField(schema.Snapshot{Collections: []schema.Collection{{Slug: "posts", Fields: []schema.Field{radio}}}}, "posts"); name != "" {
		t.Fatalf("radio status field = %q", name)
	}
}

func TestAdminInitialRoutePreparationPanicReturnsStructuredFallback(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"}}
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Collections: []operationengine.Collection{{
		Schema: collection,
		Hooks: operationengine.Hooks{BeforeRead: []operationengine.Hook{func(operationengine.Context) error {
			panic("route read panic")
		}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Config{
		AdminAssets: adminInitialAssets(t), Engine: engine,
		Manifest: schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "panic"}, Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{}}),
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts", nil)
	request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || state.Outcome != protocol.AdminPreparedRouteFallback || state.Route != nil || state.Diagnostic == nil || state.Diagnostic.Code != "route_prepare_failed" {
		t.Fatalf("panic fallback status=%d state=%#v", response.Code, state)
	}
}

func adminInitialAssets(t *testing.T) fstest.MapFS {
	t.Helper()
	metadata := adminBootstrapMetadata{
		ProtocolVersion:    protocol.AdminPreparedRouteStateVersion,
		DocumentViewRoutes: []string{}, ExtensionRoutes: []string{}, ReplacedCoreViews: []string{},
		ListCellFields: map[string][]string{},
		ModuleGroups: map[string][]string{
			"date": {"assets/app.js"}, "document": {"assets/app.js"}, "document-api": {"assets/app.js"}, "versions": {"assets/app.js"},
			"entry": {"assets/app.js"}, "upload-preview": {"assets/app.js"}, "bulk-upload": {"assets/app.js"},
		},
		SeedableCoreSurfaces: []string{"account", "collectionCreate", "collectionEdit", "collectionList", "dashboard", "global", "login", "notFound", "setup"},
	}
	metadata.BuildID = adminBootstrapBuildID(metadata)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		"index.html":                {Data: []byte(`<!doctype html><html><head></head><body><div id="app"></div><script type="module" src="/admin/assets/app.js"></script></body></html>`)},
		"ridu-admin-bootstrap.json": {Data: encoded},
		"assets/app.js":             {Data: []byte("export {}")},
	}
}

func decodeAdminInitialTemplate(t *testing.T, html string) protocol.AdminPreparedRouteStateV1 {
	t.Helper()
	const prefix = `<template id="ridu-admin-initial-state">`
	start := strings.Index(html, prefix)
	end := strings.Index(html, `</template>`)
	if start < 0 || end < start {
		t.Fatalf("initial template not found: %s", html)
	}
	var state protocol.AdminPreparedRouteStateV1
	if err := json.Unmarshal([]byte(html[start+len(prefix):end]), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// adminEditorCollection is the admin's user collection, whose sessions read
// drafts; core supplies it to the engine from Config.Admin.User.
func adminEditorCollection() operationengine.Collection {
	return operationengine.Collection{Schema: schema.Collection{ID: "users", Slug: "users", Capabilities: schema.Capabilities{Auth: true}}}
}
