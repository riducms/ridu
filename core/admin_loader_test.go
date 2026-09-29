package core

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminLoaderContractAndInput(t *testing.T) {
	type input struct {
		Query   string `json:"q"`
		Limit   *int   `json:"limit"`
		Enabled bool   `json:"enabled"`
	}
	type output struct {
		Query string   `json:"q"`
		Limit *int     `json:"limit"`
		Items []string `json:"items"`
	}
	called := 0
	loader := NewAdminLoader("counts", func(_ AdminLoadContext, value input) (output, error) {
		called++
		return output{Query: value.Query, Limit: value.Limit}, nil
	})
	config := Config{Name: "Loaders", Admin: AdminConfig{Loaders: []AdminLoaderDefinition{loader}}, Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}}
	manifest, err := Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("resolve invoked executable loader")
	}
	descriptors := manifest.Snapshot().Application.AdminLoaders
	if len(descriptors) != 1 || descriptors[0].Output.Fields["items"].Nullable != true {
		t.Fatalf("descriptor = %#v", descriptors)
	}
	descriptors[0].Output.Fields["q"] = schema.AdminDataType{Kind: "boolean"}
	if manifest.Snapshot().Application.AdminLoaders[0].Output.Fields["q"].Kind != "string" {
		t.Fatal("manifest descriptor is mutable")
	}
	encoded, err := loader.run(AdminLoadContext{}, url.Values{"q": {"<script>&"}, "limit": {"12"}, "enabled": {"true"}, "locale": {"en"}})
	if err != nil || !strings.Contains(string(encoded), `"limit":12`) || strings.Contains(string(encoded), "<script>") {
		t.Fatalf("output=%s error=%v", encoded, err)
	}
	for _, values := range []url.Values{{"limit": {"x"}}, {"limit": {"1", "2"}}, {"enabled": {"invalid"}}} {
		if _, err := loader.run(AdminLoadContext{}, values); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
	if called != 1 {
		t.Fatalf("invalid input invoked handler: %d", called)
	}
	app, err := New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Handler(HandlerOptions{})
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodGet} {
		before := called
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/api/admin/loaders/counts?route=%2F%3Fq%3Drefresh", nil))
		if method == http.MethodGet {
			if response.Code != 200 || called != before+1 {
				t.Fatalf("GET = %d %s", response.Code, response.Body.String())
			}
			var data output
			if json.Unmarshal(response.Body.Bytes(), &data) != nil || data.Query != "refresh" {
				t.Fatal(response.Body.String())
			}
		} else if called != before || (method == http.MethodHead && response.Body.Len() != 0) {
			t.Fatal("non-GET ran a loader or HEAD returned a body")
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("missing personalized cache policy")
		}
	}
}

func TestAdminLoaderReadsPreservePredicatesRedactionHooksAndAudit(t *testing.T) {
	reads := 0
	title, _ := query.NewPath("title")
	var seenActor string
	config := Config{Name: "Loader read boundary", Collections: []Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }})},
		Access: CollectionAccess{Read: func(ctx AccessContext) (AccessDecision, error) {
			if ctx.Actor != nil {
				seenActor = ctx.Actor.ID
			}
			return Where(query.Equal(title, "visible")), nil
		}},
		Hooks: CollectionHooks{AfterRead: []Hook{func(HookContext) error { reads++; return nil }}},
	}}}
	type data struct {
		ID     string `json:"id"`
		Count  int    `json:"count"`
		Secret string `json:"secret,omitempty"`
	}
	config.Admin.Loaders = []AdminLoaderDefinition{NewAdminLoader("dashboard", func(ctx AdminLoadContext, _ struct{}) (data, error) {
		page, err := ctx.List("posts", ListOptions{Limit: 10, Actor: &store.Document{ID: "forged"}, ActorCollection: "forged"})
		if err != nil {
			return data{}, err
		}
		document, err := ctx.Find("posts", page.Documents[0].ID, FindOptions{})
		if err != nil {
			return data{}, err
		}
		if _, exists := document.Values["secret"]; exists {
			t.Fatal("read facade leaked redacted field")
		}
		return data{ID: document.ID, Count: page.Total}, nil
	})}
	app, err := New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"visible", "hidden"} {
		if _, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String(value), "secret": store.String("private")}, MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	reads = 0
	var audit []AuditEvent
	handler := app.Handler(HandlerOptions{Audit: func(event AuditEvent) { audit = append(audit, event) }})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/admin/loaders/dashboard?route=%2F", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"count":1`) || strings.Contains(response.Body.String(), "private") {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	if reads != 2 || len(audit) != 1 || audit[0].Action != "read" || audit[0].RequestID == "" {
		t.Fatalf("reads=%d audit=%#v", reads, audit)
	}
	if seenActor != "" {
		t.Fatal("request facade accepted a forged actor")
	}
}

func TestAdminLoaderRejectsUnsupportedContracts(t *testing.T) {
	type recursive struct {
		Next *recursive `json:"next"`
	}
	type missingTag struct{ Count int }
	loaders := []AdminLoaderDefinition{
		NewAdminLoader("bad", func(AdminLoadContext, struct{}) (map[string]string, error) { return nil, nil }),
		NewAdminLoader("bad", func(AdminLoadContext, struct{}) (recursive, error) { return recursive{}, nil }),
		NewAdminLoader("bad", func(AdminLoadContext, struct{}) (missingTag, error) { return missingTag{}, nil }),
		NewAdminLoader("bad", func(AdminLoadContext, struct {
			Items []string `json:"items"`
		}) (int, error) {
			return 0, nil
		}),
		NewAdminLoader[struct{}, string]("bad", nil),
		NewAdminLoader("bad", func(AdminLoadContext, struct {
			At time.Time `json:"at"`
		}) (int, error) {
			return 0, nil
		}),
		NewAdminLoader("bad", func(AdminLoadContext, struct {
			At *time.Time `json:"at"`
		}) (int, error) {
			return 0, nil
		}),
		NewAdminLoader("bad", func(AdminLoadContext, struct {
			Limit **int `json:"limit"`
		}) (int, error) {
			return 0, nil
		}),
		NewAdminLoader("bad", func(AdminLoadContext, struct {
			Number json.Number `json:"number"`
		}) (int, error) {
			return 0, nil
		}),
		NewAdminLoader("bad", func(AdminLoadContext, struct{}) (json.Number, error) { return "1", nil }),
		NewAdminLoader("bad", func(AdminLoadContext, struct{}) (adminLoaderTextValue, error) { return 0, nil }),
		NewAdminLoader("bad", func(AdminLoadContext, struct{}) (adminLoaderTextPointer, error) { return 0, nil }),
	}
	for _, loader := range loaders {
		if _, err := adminLoaderDescriptors([]AdminLoaderDefinition{loader}); err == nil {
			t.Fatalf("accepted %v", loader)
		}
	}
	good := NewAdminLoader("good", func(AdminLoadContext, struct{}) (int, error) { return 0, nil })
	if _, err := adminLoaderDescriptors([]AdminLoaderDefinition{good, good}); err == nil {
		t.Fatal("accepted duplicate loader keys")
	}
}

type adminLoaderTextValue int

func (adminLoaderTextValue) MarshalText() ([]byte, error) { return []byte("custom"), nil }

type adminLoaderTextPointer int

func (*adminLoaderTextPointer) MarshalText() ([]byte, error) { return []byte("custom"), nil }

func TestAdminLoaderNumbersSurviveJavaScript(t *testing.T) {
	type input struct {
		Count int64   `json:"count"`
		Ratio float64 `json:"ratio"`
	}
	called := 0
	loader := NewAdminLoader("numbers", func(_ AdminLoadContext, value input) (input, error) { called++; return value, nil })
	for _, values := range []url.Values{{"count": {"9007199254740992"}}, {"count": {"-9007199254740992"}}, {"ratio": {"NaN"}}, {"ratio": {"+Inf"}}} {
		if _, err := loader.run(AdminLoadContext{}, values); err == nil {
			t.Fatalf("accepted input %v", values)
		}
	}
	if called != 0 {
		t.Fatal("invalid number reached handler")
	}
	if _, err := loader.run(AdminLoadContext{}, url.Values{"count": {"9007199254740991"}, "ratio": {"0.5"}}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []input{{Count: 1 << 53}, {Count: -(1 << 53)}, {Ratio: math.NaN()}, {Ratio: math.Inf(1)}} {
		output := NewAdminLoader("numbers", func(AdminLoadContext, struct{}) (input, error) { return value, nil })
		if _, err := output.run(AdminLoadContext{}, nil); err == nil {
			t.Fatalf("accepted output %#v", value)
		}
	}
}

func TestAdminLoaderBindsAuthenticatedIdentityAndLocale(t *testing.T) {
	var actorID, documentID string
	var observations []HookContext
	hook := func(ctx HookContext) error { observations = append(observations, ctx); return nil }
	config := Config{Name: "Bound loader", Admin: AdminConfig{User: "users"},
		Localization: LocalizationConfig{DefaultLocale: "en", Locales: []Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}},
		Collections: []Collection{
			{Slug: "users", Auth: true, AuthConfig: AuthConfig{Password: PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Email("email").Required().Unique()}},
			{Slug: "posts", Fields: field.Fields{field.Text("title").Localized()}, Hooks: CollectionHooks{AfterRead: []Hook{hook}}},
		},
		Globals: []Global{{Slug: "settings", Fields: field.Fields{field.Text("title").Localized()}, Hooks: CollectionHooks{AfterRead: []Hook{hook}}}},
	}
	config.Admin.Loaders = []AdminLoaderDefinition{NewAdminLoader("bound", func(ctx AdminLoadContext, _ struct{}) ([]string, error) {
		if ctx.Actor().ID != actorID || ctx.ActorCollection() != "users" || ctx.Locale() != "fr" {
			t.Fatalf("context identity/locale not request-bound: %#v", ctx)
		}
		detached := ctx.Actor()
		detached.ID = "changed"
		find := FindOptions{Actor: &store.Document{ID: "forged"}, ActorCollection: "forged", Locale: "en", AllLocales: true, DisableFallback: true, FallbackLocales: []schema.LocaleCode{}}
		page, err := ctx.List("posts", ListOptions{Actor: find.Actor, ActorCollection: find.ActorCollection, Locale: find.Locale, AllLocales: true, DisableFallback: true, FallbackLocales: find.FallbackLocales})
		if err != nil {
			return nil, err
		}
		document, err := ctx.Find("posts", documentID, find)
		if err != nil {
			return nil, err
		}
		global, err := ctx.Global("settings", find)
		if err != nil {
			return nil, err
		}
		values := []string{}
		for _, doc := range []store.Document{page.Documents[0], document, global} {
			value, _ := doc.Values["title"].StringValue()
			values = append(values, value)
		}
		return values, nil
	})}
	app, err := New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	actor, err := app.Local().Create(t.Context(), "users", store.Values{"email": store.String("loader@example.test")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actorID = actor.ID
	if err := app.SetPassword(t.Context(), "users", actorID, "loader-password-value"); err != nil {
		t.Fatal(err)
	}
	session, err := app.Login(t.Context(), "users", "loader@example.test", "loader-password-value")
	if err != nil {
		t.Fatal(err)
	}
	document, err := app.Local().Create(t.Context(), "posts", store.Values{"title": store.String("English")}, MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	documentID = document.ID
	if _, err := app.Local().UpdateGlobal(t.Context(), "settings", store.Values{"title": store.String("English")}, MutationOptions{Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	observations = nil
	request := httptest.NewRequest(http.MethodGet, "/api/admin/loaders/bound?route=%2F%3Flocale%3Dfr", nil)
	request.AddCookie(&http.Cookie{Name: "ridu_session", Value: session.Token})
	response := httptest.NewRecorder()
	app.Handler(HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != 200 || strings.TrimSpace(response.Body.String()) != `["English","English","English"]` {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if len(observations) != 3 {
		t.Fatalf("read hooks = %d", len(observations))
	}
	for _, ctx := range observations {
		if ctx.Actor == nil || ctx.Actor.ID != actorID || ctx.ActorCollection != "users" || ctx.Locale != "fr" || ctx.AllLocales {
			t.Fatalf("read escaped request context: %#v", ctx)
		}
	}
}
