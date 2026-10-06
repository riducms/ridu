package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
)

func TestAdminPresentationSharesManifestWithLocalizationOverride(t *testing.T) {
	localization := &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en"}, {Code: "fr", FallbackLocales: []schema.LocaleCode{"en"}}}}
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{}}
	engine, err := operationengine.New(operationengine.Config{Store: teststore.New(), Localization: localization, Collections: []operationengine.Collection{{Schema: collection}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := schema.Snapshot{Version: schema.CurrentVersion,
		Application: schema.Application{Name: "Presented", Localization: localization},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	}
	narrowed := &schema.LocalizationSettings{DefaultLocale: "fr", Locales: []schema.Locale{{Code: "fr", FallbackLocales: []schema.LocaleCode{}}}}
	handler := func(override func(context.Context, *AuthIdentity) (*schema.LocalizationSettings, error)) http.Handler {
		return New(Config{AdminAssets: adminInitialAssets(t), Engine: engine, Manifest: schema.NewManifest(snapshot), LocalizationForRequest: override})
	}
	read := func(handler http.Handler) (protocol.AdminPreparedRouteStateV1, json.RawMessage) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/admin/collections/posts", nil)
		request.Header.Set("Accept", protocol.AdminPreparedRouteStateMediaType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var state protocol.AdminPreparedRouteStateV1
		var raw struct {
			Runtime struct {
				Manifest json.RawMessage `json:"manifest"`
			} `json:"runtime"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if state.Runtime == nil {
			t.Fatalf("missing runtime: %s", response.Body.String())
		}
		return state, raw.Runtime.Manifest
	}

	base, baseManifest := read(handler(nil))
	expected, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseManifest, expected) {
		t.Fatalf("presented manifest differs from its canonical encoding:\n%s\n%s", baseManifest, expected)
	}
	unchanged, _ := read(handler(func(context.Context, *AuthIdentity) (*schema.LocalizationSettings, error) { return nil, nil }))
	if unchanged.ContextKey != base.ContextKey {
		t.Fatalf("an unchanged localization changed the context: %q != %q", unchanged.ContextKey, base.ContextKey)
	}
	overridden, _ := read(handler(func(context.Context, *AuthIdentity) (*schema.LocalizationSettings, error) { return narrowed, nil }))
	presented := overridden.Runtime.Manifest.Application.Localization
	if presented == nil || len(presented.Locales) != 1 || presented.Locales[0].Code != "fr" || presented.DefaultLocale != "fr" || overridden.Runtime.Manifest.Application.Name != "Presented" {
		t.Fatalf("overridden presentation = %#v", overridden.Runtime.Manifest.Application)
	}
	if overridden.ContextKey == base.ContextKey {
		t.Fatal("a localization override must change the runtime context")
	}
	if len(snapshot.Application.Localization.Locales) != 2 {
		t.Fatalf("an override mutated the shared manifest: %#v", snapshot.Application.Localization)
	}
}
