package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	embeddedplugin "github.com/riducms/ridu/tests/contracts/embedded_plugin"
	"github.com/riducms/ridu/tests/contracts/livevalidation"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
	"golang.org/x/crypto/bcrypt"
)

func newLiveApp(t *testing.T, config core.Config) (*core.App, *teststore.Store) {
	t.Helper()
	config.Admin.User = "live-users"
	config.Collections = append(config.Collections, core.Collection{Slug: "live-users", Auth: true,
		Fields: field.Fields{field.Text("email").Required().Unique()},
		AuthConfig: core.AuthConfig{Password: core.PasswordPolicy{BcryptCost: bcrypt.MinCost}, Strategies: []core.AuthStrategy{{Name: "live-test", Authenticate: func(ctx core.AuthStrategyContext) (core.AuthStrategyResult, error) {
			return core.AuthStrategyResult{Authenticated: slices.Equal(ctx.Headers["X-Live-Test"], []string{"editor"}), UserID: "live-editor"}, nil
		}}}},
	})
	backend := teststore.New()
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Import(t.Context(), "live-users", store.Values{"email": store.String("editor@example.test")}, core.ImportOptions{ID: "live-editor", Status: store.StatusPublished}, nil); err != nil {
		t.Fatal(err)
	}
	return app, backend
}

func liveRequest(t *testing.T, app *core.App, target string, input any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://ridu.test/api/"+target, bytes.NewReader(encoded)).WithContext(t.Context())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Live-Test", "editor")
	response := httptest.NewRecorder()
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	return response
}

func liveChecked(t *testing.T, response *httptest.ResponseRecorder) []protocol.LiveValidationEvaluation {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("live response = %d: %s", response.Code, response.Body.String())
	}
	var envelope protocol.LiveValidationEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Evaluations
}

func TestLiveValidationIsOptInAndDoesNotRunSaveLifecycle(t *testing.T) {
	liveCalls, saveCalls, defaults, hooks := 0, 0, 0, 0
	observeHook := func(core.HookContext) error { hooks++; return nil }
	sku := livevalidation.SKU("sku").ReplaceLiveValidators(func(ctx operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
		liveCalls++
		return livevalidation.CheckSKU(ctx.Siblings, value)
	})
	saveOnly := field.Text("saveOnly").Validate(func(operation.ValidationContext, operation.Value[string]) ([]operation.Issue, error) {
		saveCalls++
		return nil, nil
	})
	config := core.Config{Name: "Live isolation", Collections: []core.Collection{{Slug: "products",
		Fields: field.Fields{field.Text("supplier"), sku, saveOnly, field.Text("defaulted").DefaultFrom(func(operation.DefaultContext) (operation.Value[string], error) {
			defaults++
			return operation.Present("server default"), nil
		})},
		Hooks: core.CollectionHooks{BeforeValidate: []core.Hook{observeHook}, BeforeChange: []core.Hook{observeHook}, BeforeOperation: []core.Hook{observeHook}, BeforeRead: []core.Hook{observeHook}, AfterChange: []core.Hook{observeHook}, AfterRead: []core.Hook{observeHook}, AfterOperation: []core.Hook{observeHook}, AfterCommit: []core.Hook{observeHook}},
	}}}
	app, backend := newLiveApp(t, config)
	initialEvents := len(backend.Events())
	evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{
		"data": map[string]any{"supplier": "acme", "sku": "G-123"}, "fields": []string{"sku"},
	}))
	if len(evaluations) != 1 || evaluations[0].Status != "checked" || len(evaluations[0].Issues) != 1 || evaluations[0].Issues[0].Code != "supplier_sku" {
		t.Fatalf("live feedback = %#v", evaluations)
	}
	if liveCalls != 1 || saveCalls != 0 || defaults != 0 || hooks != 0 {
		t.Fatalf("unexpected live lifecycle: live=%d save=%d defaults=%d hooks=%d", liveCalls, saveCalls, defaults, hooks)
	}
	for _, event := range backend.Events()[initialEvents:] {
		if event == "create" || event == "update" || event == "delete" {
			t.Fatalf("live validation wrote storage: %v", backend.Events()[initialEvents:])
		}
	}
	// Requesting a save-only field cannot opt it into running from the browser.
	denied := liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{"saveOnly": "draft"}, "fields": []string{"saveOnly"}})
	if denied.Code != http.StatusBadRequest || saveCalls != 0 {
		t.Fatalf("save-only request = %d %s; calls=%d", denied.Code, denied.Body.String(), saveCalls)
	}
	valid := liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{"supplier": "acme", "sku": "A-123"}, "fields": []string{"sku"}}))
	if len(valid) != 1 || len(valid[0].Issues) != 0 {
		t.Fatalf("valid feedback = %#v", valid)
	}
	// A previous successful advisory check cannot authorize a different write.
	_, err := app.Local().Create(t.Context(), "products", store.Values{"supplier": store.String("globex"), "sku": store.String("A-123")}, nil)
	var failure *core.OperationError
	if !errors.As(err, &failure) || failure.Status != 422 || !slices.ContainsFunc(failure.Issues, func(issue schema.Issue) bool { return issue.Code == "supplier_sku" }) {
		t.Fatalf("Local save did not validate independently: %v", err)
	}
	response := requestJSON(t, handlerClient(app.Handler(core.HandlerOptions{})), http.MethodPost, "http://ridu.test/api/collections/products", strings.NewReader(`{"supplier":"globex","sku":"A-123"}`), "")
	if response.StatusCode != 422 {
		t.Fatalf("REST save = %d: %s", response.StatusCode, readBody(t, response))
	}
	_ = response.Body.Close()
	if liveCalls != 2 {
		t.Fatalf("Save unexpectedly invoked live callbacks: %d", liveCalls)
	}
}

func TestLiveValidationTypedDecodingMissingInputAndOperationalFailure(t *testing.T) {
	var values []operation.Value[string]
	check := func(_ operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
		values = append(values, value)
		text, _ := value.Get()
		if text == "fail" {
			return nil, errors.New("private internal service password")
		}
		return nil, nil
	}
	app, _ := newLiveApp(t, core.Config{Name: "Live types", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Text("sku").LiveValidate(check), field.Group("group", field.Fields{field.Text("sku").LiveValidate(check)}),
	}}}})
	for _, test := range []struct {
		name, path, status string
		data               map[string]any
		calls              int
		present            bool
	}{
		{"typed value", "sku", "checked", map[string]any{"sku": "A-1"}, 1, true},
		{"missing scalar", "sku", "checked", map[string]any{}, 1, false},
		{"explicit null", "sku", "checked", map[string]any{"sku": nil}, 1, false},
		{"malformed scalar", "sku", "skipped", map[string]any{"sku": 123}, 0, false},
		{"absent group", "group.sku", "skipped", map[string]any{}, 0, false},
		{"null group", "group.sku", "skipped", map[string]any{"group": nil}, 0, false},
		{"malformed group", "group.sku", "skipped", map[string]any{"group": "bad"}, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			values = nil
			evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{"data": test.data, "fields": []string{test.path}}))
			if len(evaluations) != 1 || evaluations[0].Status != test.status || len(values) != test.calls {
				t.Fatalf("evaluations=%#v typed calls=%d", evaluations, len(values))
			}
			if len(values) > 0 {
				if _, present := values[0].Get(); present != test.present {
					t.Fatalf("typed presence=%t want %t", present, test.present)
				}
			}
		})
	}
	response := liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{"sku": "fail"}, "fields": []string{"sku"}})
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), `"checked"`) {
		t.Fatalf("operational failure = %d %s", response.Code, response.Body.String())
	}
}

func TestLiveValidationContextUsesRetainedExactPriorAndInput(t *testing.T) {
	var seen operation.LiveValidationContext
	var current operation.Value[string]
	sku := field.Text("sku").LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
		seen, current = ctx, value
		return nil, nil
	})
	config := livevalidation.Config()
	config.Collections = []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Text("supplier"), sku,
		field.Group("seo", field.Fields{field.Text("supplier"), sku}),
		field.Array("variants", field.Fields{field.Text("supplier"), sku}),
		field.Blocks("blocks", field.Block{Slug: "card", Fields: field.Fields{sku}}, field.Block{Slug: "note", Fields: field.Fields{sku}}),
		sku.Rename("localizedSKU").Localized(),
	}}}
	app, _ := newLiveApp(t, config)
	document, err := app.Local().Create(t.Context(), "products", store.Values{
		"supplier": store.String("acme"), "sku": store.String("A-root"),
		"seo":          store.Object(store.Values{"supplier": store.String("acme"), "sku": store.String("A-group")}),
		"variants":     store.List(store.Object(store.Values{"_key": store.String("A"), "supplier": store.String("acme"), "sku": store.String("A-first")}), store.Object(store.Values{"_key": store.String("B"), "supplier": store.String("globex"), "sku": store.String("G-second")})),
		"blocks":       store.List(store.Object(store.Values{"_key": store.String("C"), "blockType": store.String("card"), "sku": store.String("A-block")})),
		"localizedSKU": store.String("A-English"),
	}, nil, core.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, locale, prior, supplier, value string
		data                                       map[string]any
		inputHasSKU                                bool
	}{
		{"root retained", "sku", "en", "A-root", "globex", "A-root", map[string]any{"supplier": "globex"}, false},
		{"group retained", "seo.sku", "en", "A-group", "globex", "A-group", map[string]any{"seo": map[string]any{"supplier": "globex"}}, false},
		{"reordered retained row", "variants.1.sku", "en", "A-first", "globex", "A-first", map[string]any{"variants": []any{map[string]any{"_key": "B"}, map[string]any{"_key": "A", "supplier": "globex"}}}, false},
		{"new repeated row", "variants.0.sku", "en", "", "acme", "A-new", map[string]any{"variants": []any{map[string]any{"_key": "new", "supplier": "acme", "sku": "A-new"}}}, true},
		{"replaced Block case", "blocks.0.sku", "en", "", "", "A-new", map[string]any{"blocks": []any{map[string]any{"_key": "C", "blockType": "note", "sku": "A-new"}}}, true},
		{"missing exact locale", "localizedSKU", "fr", "", "acme", "G-French", map[string]any{"localizedSKU": "G-French"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen = operation.LiveValidationContext{}
			evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate?locale="+test.locale, map[string]any{"id": document.ID, "data": test.data, "fields": []string{test.path}}))
			if len(evaluations) != 1 || evaluations[0].Status != "checked" {
				t.Fatalf("evaluations=%#v", evaluations)
			}
			name := "sku"
			if test.path == "localizedSKU" {
				name = "localizedSKU"
			}
			prior, _ := seen.Prior.String(name)
			supplier, _ := seen.Siblings.String("supplier")
			value, _ := current.Get()
			if prior != test.prior || supplier != test.supplier || value != test.value {
				t.Fatalf("prior=%q supplier=%q value=%q; want %q/%q/%q", prior, supplier, value, test.prior, test.supplier, test.value)
			}
			if seen.Operation != operation.Update || seen.ID != operation.ID(document.ID) || seen.CollectionID != "products" || seen.GlobalID != "" || seen.Actor.ID != "live-editor" || seen.Actor.Collection != "live-users" || seen.Locale != schema.LocaleCode(test.locale) || seen.Context == nil || seen.Local == nil {
				t.Fatalf("identity/context=%#v", seen)
			}
			if _, present := seen.Input.Lookup("sku"); present != test.inputHasSKU {
				t.Fatalf("Input sku membership=%t want %t", present, test.inputHasSKU)
			}
			if rootSupplier, _ := seen.Root.String("supplier"); rootSupplier == "" {
				t.Fatal("retained root supplier disappeared")
			}
		})
	}
}

func TestLiveValidationRepeatedDescendantsAndEmbeddedForms(t *testing.T) {
	app, _ := newLiveApp(t, livevalidation.Config())
	link := map[string]any{"_key": "link-b", "url": "invalid"}
	for _, test := range []struct {
		name, field, locale, expected string
		value                         any
		keys                          []string
	}{
		{"array child", "sections", "en", "sections.0.sku", []any{map[string]any{"_key": "section-a", "sku": "invalid"}}, []string{"section-a"}},
		{"nested array child", "sections", "en", "sections.0.links.0.url", []any{map[string]any{"_key": "section-a", "links": []any{link}}}, []string{"section-a", "link-b"}},
		{"Block child", "content", "en", "content.0.sku", []any{map[string]any{"_key": "block-a", "blockType": "card", "sku": "invalid"}}, []string{"block-a", "card"}},
		{"Block nested array", "content", "fr", "content.0.links.0.url", []any{map[string]any{"_key": "block-a", "blockType": "card", "links": []any{link}}}, []string{"block-a", "card", "link-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluations := liveChecked(t, liveRequest(t, app, "collections/live-validation/validate?locale="+test.locale, map[string]any{"data": map[string]any{test.field: test.value}, "fields": []string{test.field}}))
			if len(evaluations) != 1 || len(evaluations[0].Issues) != 1 {
				t.Fatalf("evaluations=%#v", evaluations)
			}
			issue := evaluations[0].Issues[0]
			if issue.Path != test.expected || issue.FieldID == "" || issue.CollectionID != "live-validation" || issue.Locale != schema.LocaleCode(test.locale) {
				t.Fatalf("resolved issue=%#v", issue)
			}
			var target []string
			if err := json.Unmarshal([]byte(issue.Target), &target); err != nil {
				t.Fatal(err)
			}
			for _, key := range test.keys {
				if !slices.Contains(target, key) {
					t.Fatalf("issue target=%v missing %q", target, key)
				}
			}
		})
	}
	for _, test := range []struct {
		name, field, tree, tag, locale string
		value                          store.Value
	}{
		{"rich text", "body", "blocks", "block", "en", richtextblocks.Document(richtextblocks.Block("card", "embed-a", store.Values{"sku": store.String("A-original"), "supplier": store.String("acme")}))},
		{"localized rich text", "localizedBody", "blocks", "block", "fr", richtextblocks.Document(richtextblocks.Block("card", "embed-a", store.Values{"sku": store.String("A-original"), "supplier": store.String("acme")}))},
		{"generic plugin", "outline", "widgets", "widget", "en", embeddedplugin.Value(embeddedplugin.Widget("card", "embed-a", store.Values{"sku": store.String("A-original"), "supplier": store.String("acme")}))},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := map[string]any{"supplier": "acme", "sku": "G-123", "links": []any{link}, "_key": "embed-a", "blockType": "card"}
			if test.field == "outline" {
				delete(payload, "_key")
				delete(payload, "blockType")
				payload["uid"], payload["schema"] = "embed-a", "card"
			}
			evaluations := liveChecked(t, liveRequest(t, app, "collections/live-validation/validate?locale="+test.locale, map[string]any{
				"data": map[string]any{"title": "Unsaved parent", test.field: test.value}, "fields": []string{"links"},
				"embedded": []any{map[string]any{"field": test.field, "treeKey": test.tree, "caseTag": test.tag, "variantSlug": "card", "identity": "embed-a", "data": payload}},
			}))
			if len(evaluations) != 1 || evaluations[0].Path != "links" || len(evaluations[0].Issues) != 1 || evaluations[0].Issues[0].Path != "links.0.url" || evaluations[0].Issues[0].Locale != schema.LocaleCode(test.locale) {
				t.Fatalf("embedded feedback=%#v", evaluations)
			}
		})
	}
}

func TestLiveValidationManifestNeverExecutesCallbacks(t *testing.T) {
	calls := 0
	config := livevalidation.Config()
	config.Collections[0].Fields = append(config.Collections[0].Fields, field.Text("never").LiveValidate(func(operation.LiveValidationContext, operation.Value[string]) ([]operation.Issue, error) {
		calls++
		return nil, nil
	}))
	first, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("manifest invoked callback or changed: calls=%d", calls)
	}
	encoded, _ := json.Marshal(first)
	if bytes.Contains(encoded, []byte("private supplier")) || bytes.Contains(encoded, []byte("LiveValidationContext")) {
		t.Fatal("manifest serialized callback behavior")
	}
}

func TestLiveValidationDetachedContextUsesParentRootAndPersistedPrior(t *testing.T) {
	for _, generic := range []bool{false, true} {
		name := "rich text"
		if generic {
			name = "generic plugin"
		}
		t.Run(name, func(t *testing.T) {
			var seen operation.LiveValidationContext
			sku := field.Text("sku").LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
				seen = ctx
				return nil, nil
			})
			card := field.Block{Slug: "card", Fields: field.Fields{field.Text("supplier"), sku}}
			owner := richtext.Field("body", richtext.Config{Blocks: []field.Block{card}})
			original := richtextblocks.Document(richtextblocks.Block("card", "item-a", store.Values{"supplier": store.String("acme"), "sku": store.String("A-persisted")}))
			tree, tag, identityName, discriminator := "blocks", "block", "_key", "blockType"
			if generic {
				owner = embeddedplugin.Field("body", card)
				original = embeddedplugin.Value(embeddedplugin.Widget("card", "item-a", store.Values{"supplier": store.String("acme"), "sku": store.String("A-persisted")}))
				tree, tag, identityName, discriminator = "widgets", "widget", "uid", "schema"
			}
			config := livevalidation.Config()
			config.Collections = []core.Collection{{Slug: "products", Fields: field.Fields{field.Text("title"), owner}}}
			app, _ := newLiveApp(t, config)
			document, err := app.Local().Create(t.Context(), "products", store.Values{"title": store.String("Saved parent"), "body": original}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, identity := range []string{"item-a", "new-item"} {
				payload := map[string]any{identityName: identity, discriminator: "card", "supplier": "globex", "sku": "G-pending"}
				liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{
					"id": document.ID, "data": map[string]any{"title": "Unsaved parent", "body": original}, "fields": []string{"sku"},
					"embedded": []any{map[string]any{"field": "body", "treeKey": tree, "caseTag": tag, "variantSlug": "card", "identity": identity, "data": payload}},
				}))
				prior, _ := seen.Prior.String("sku")
				wantPrior := "A-persisted"
				if identity == "new-item" {
					wantPrior = ""
				}
				if prior != wantPrior {
					t.Fatalf("identity=%s Prior=%q want %q", identity, prior, wantPrior)
				}
				if title, _ := seen.Root.String("title"); title != "Unsaved parent" {
					t.Fatalf("Root title=%q", title)
				}
				if pending, _ := seen.Siblings.String("sku"); pending != "G-pending" {
					t.Fatalf("detached Siblings sku=%q", pending)
				}
				if !reflect.DeepEqual(seen.Root.Get("body"), original) {
					t.Fatal("detached advisory edit replaced the parent Root before Apply")
				}
			}
		})
	}
}

func TestLiveValidationGlobalUsesTheSameAdvisoryContract(t *testing.T) {
	var seen operation.LiveValidationContext
	app, _ := newLiveApp(t, core.Config{Name: "Live global", Globals: []core.Global{{Slug: "settings", Fields: field.Fields{
		field.Text("sku").LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
			seen = ctx
			return []operation.Issue{{Code: "sku", Message: "Review the global SKU"}}, nil
		}),
	}}}})
	evaluations := liveChecked(t, liveRequest(t, app, "globals/settings/validate", map[string]any{"data": map[string]any{"sku": "draft"}, "fields": []string{"sku"}}))
	if len(evaluations) != 1 || len(evaluations[0].Issues) != 1 || evaluations[0].Issues[0].GlobalID != "global-settings" || seen.GlobalID != "global-settings" || seen.CollectionID != "" || seen.Operation != operation.Update {
		t.Fatalf("global live context=%#v evaluations=%#v", seen, evaluations)
	}
}

func TestLiveValidationHTTPHonorsAuthenticationAndProtectsCookieOrigins(t *testing.T) {
	config := livevalidation.Config()
	requireActor := func(ctx core.AccessContext) (core.AccessDecision, error) {
		if ctx.Actor == nil {
			return core.Deny(), nil
		}
		return core.Allow(), nil
	}
	config.Collections[0].Access = core.CollectionAccess{Create: requireActor, Read: requireActor, Update: requireActor}
	app, _ := newLiveApp(t, config)
	input := []byte(`{"data":{"supplier":"acme","sku":"G-123"},"fields":["sku"]}`)
	request := httptest.NewRequest(http.MethodPost, "http://ridu.test/api/collections/live-validation/validate", bytes.NewReader(input))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("anonymous check = %d %s", response.Code, response.Body.String())
	}
	if err := app.SetPassword(t.Context(), "live-users", "live-editor", "isolated-test-password"); err != nil {
		t.Fatal(err)
	}
	session, err := app.Login(t.Context(), "live-users", "editor@example.test", "isolated-test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, origin, fetchSite string
		status                  int
	}{
		{"same origin", "http://ridu.test", "same-origin", http.StatusOK},
		{"foreign origin", "https://other.example.test", "cross-site", http.StatusForbidden},
		{"missing cross-site origin", "", "cross-site", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://ridu.test/api/collections/live-validation/validate", bytes.NewReader(input))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Cookie", "ridu_session="+session.Token)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			response := httptest.NewRecorder()
			app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("cookie origin response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestLiveValidationBoundReaderKeepsActorExactLocaleAndCancellation(t *testing.T) {
	var targetID string
	var found store.Document
	var readError error
	var cancel context.CancelFunc
	readCalls, readHooks := 0, 0
	config := livevalidation.Config()
	config.Collections = []core.Collection{
		{Slug: "suppliers", Fields: field.Fields{field.Text("name").Localized(), field.Text("secret").Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }})},
			Access: core.CollectionAccess{Read: func(ctx core.AccessContext) (core.AccessDecision, error) {
				readCalls++
				if ctx.Actor == nil || ctx.Actor.ID != "live-editor" || ctx.ActorCollection != "live-users" || ctx.Locale != "fr" {
					return core.Deny(), nil
				}
				return core.Allow(), nil
			}},
			Hooks: core.CollectionHooks{BeforeRead: []core.Hook{func(core.HookContext) error { readHooks++; return nil }}, AfterRead: []core.Hook{func(core.HookContext) error { readHooks++; return nil }}},
		},
		{Slug: "products", Fields: field.Fields{field.Text("sku").LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
			if cancel != nil {
				cancel()
			}
			found, readError = ctx.Local.FindByID(context.Background(), "suppliers", operation.ID(targetID))
			return nil, readError
		})}},
	}
	app, backend := newLiveApp(t, config)
	supplier, err := app.Local().Create(t.Context(), "suppliers", store.Values{"name": store.String("English supplier"), "secret": store.String("private")}, nil, core.LocaleOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	targetID = supplier.ID
	readHooks = 0
	before := len(backend.Events())
	liveChecked(t, liveRequest(t, app, "collections/products/validate?locale=fr", map[string]any{"data": map[string]any{"sku": "A-123"}, "fields": []string{"sku"}}))
	if readError != nil || readCalls != 1 || readHooks != 0 {
		t.Fatalf("reader error=%v calls=%d lifecycle=%d", readError, readCalls, readHooks)
	}
	if value, _ := found.Values["name"].StringValue(); value != "" {
		t.Fatalf("reader used locale fallback: %q", value)
	}
	if _, exists := found.Values["secret"]; exists {
		t.Fatal("bound reader disclosed read-protected field")
	}
	// HTTP authentication opens its own read snapshot; the actual live callback
	// and bound lookup must share the one evaluation transaction.
	if begins := count(backend.Events()[before:], "begin"); begins != 2 {
		t.Fatalf("bound reader started another transaction: %v", backend.Events()[before:])
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	cancel = stop
	request := httptest.NewRequest(http.MethodPost, "http://ridu.test/api/collections/products/validate?locale=fr", strings.NewReader(`{"data":{"sku":"A-123"},"fields":["sku"]}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Live-Test", "editor")
	response := httptest.NewRecorder()
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if !errors.Is(readError, context.Canceled) || response.Code < 400 {
		t.Fatalf("reader escaped original cancellation: error=%v response=%d %s", readError, response.Code, response.Body.String())
	}
}

func TestLiveValidationAccessReadsDoNotReplayLifecycle(t *testing.T) {
	for _, boundary := range []string{"field", "resource find", "resource list"} {
		t.Run(boundary, func(t *testing.T) {
			var supplierID string
			hooks := 0
			sku := field.Text("sku").LiveValidate(func(operation.LiveValidationContext, operation.Value[string]) ([]operation.Issue, error) {
				return nil, nil
			})
			product := core.Collection{Slug: "products"}
			if boundary == "field" {
				sku = sku.Access(field.Access{Read: func(ctx operation.AccessContext) (bool, error) {
					_, err := ctx.Local.FindByID(context.Background(), "suppliers", operation.ID(supplierID))
					return err == nil, err
				}})
			} else {
				product.Access.Read = func(ctx core.AccessContext) (core.AccessDecision, error) {
					if boundary == "resource list" {
						_, err := ctx.Local.List(ctx.Context, "suppliers", core.ListOptions{Limit: 1, Actor: ctx.Actor, ActorCollection: ctx.ActorCollection, DisableFallback: true})
						return core.Allow(), err
					}
					_, err := ctx.Local.FindWithOptions(ctx.Context, "suppliers", supplierID, core.FindOptions{Actor: ctx.Actor, ActorCollection: ctx.ActorCollection, DisableFallback: true})
					return core.Allow(), err
				}
			}
			product.Fields = field.Fields{sku}
			config := core.Config{Name: "Read-only authorization", Collections: []core.Collection{
				{Slug: "suppliers", Fields: field.Fields{field.Text("name")}, Hooks: core.CollectionHooks{BeforeRead: []core.Hook{func(core.HookContext) error { hooks++; return nil }}, AfterRead: []core.Hook{func(core.HookContext) error { hooks++; return nil }}}}, product,
			}}
			app, _ := newLiveApp(t, config)
			supplier, err := app.Local().Create(t.Context(), "suppliers", store.Values{"name": store.String("Acme")}, nil)
			if err != nil {
				t.Fatal(err)
			}
			supplierID = supplier.ID
			hooks = 0
			liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{"sku": "A-123"}, "fields": []string{"sku"}}))
			if hooks != 0 {
				t.Fatalf("%s authorization lookup replayed %d supplier lifecycle hooks", boundary, hooks)
			}
		})
	}
}

func TestLiveValidationCannotReadDraftsHiddenFromAnonymousReaders(t *testing.T) {
	calls := 0
	app, _ := newLiveApp(t, core.Config{Name: "Live draft access", Collections: []core.Collection{{Slug: "products", Versions: true, VersionConfig: core.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").LiveValidate(func(operation.LiveValidationContext, operation.Value[string]) ([]operation.Issue, error) {
			calls++
			return nil, nil
		})},
	}}})
	draft, err := app.Local().Create(t.Context(), "products", store.Values{"title": store.String("Private draft")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Find(t.Context(), "products", draft.ID, nil); err == nil {
		t.Fatal("fixture draft unexpectedly readable anonymously")
	}
	encoded, _ := json.Marshal(map[string]any{"id": draft.ID, "data": map[string]any{}, "fields": []string{"title"}})
	request := httptest.NewRequest(http.MethodPost, "http://ridu.test/api/collections/products/validate", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || calls != 0 {
		t.Fatalf("advisory exposed unreadable draft: %d %s callbacks=%d", response.Code, response.Body.String(), calls)
	}
}

func TestLiveValidationDecodesLogicalValuesWithoutCoercion(t *testing.T) {
	seen := map[string]any{}
	fields := field.Fields{
		field.Number("number").LiveValidate(func(_ operation.LiveValidationContext, input operation.Value[float64]) ([]operation.Issue, error) {
			value, _ := input.Get()
			seen["number"] = value
			return nil, nil
		}),
		field.Checkbox("checkbox").LiveValidate(func(_ operation.LiveValidationContext, input operation.Value[bool]) ([]operation.Issue, error) {
			value, _ := input.Get()
			seen["checkbox"] = value
			return nil, nil
		}),
		field.MultiSelect("choices", "one").LiveValidate(func(_ operation.LiveValidationContext, input operation.Value[[]string]) ([]operation.Issue, error) {
			value, _ := input.Get()
			seen["choices"] = value
			return nil, nil
		}),
		field.Relationship("supplier", "suppliers").LiveValidate(func(_ operation.LiveValidationContext, input operation.Value[operation.ID]) ([]operation.Issue, error) {
			value, _ := input.Get()
			seen["supplier"] = value
			return nil, nil
		}),
		field.Relationships("suppliers", "suppliers").LiveValidate(func(_ operation.LiveValidationContext, input operation.Value[[]operation.ID]) ([]operation.Issue, error) {
			value, _ := input.Get()
			seen["suppliers"] = value
			return nil, nil
		}),
	}
	app, _ := newLiveApp(t, core.Config{Name: "Typed live values", Collections: []core.Collection{{Slug: "products", Fields: fields}, {Slug: "suppliers", Fields: field.Fields{field.Text("name")}}}})
	for _, test := range []struct {
		field          string
		valid, invalid any
		decoded        any
	}{
		{"number", 0, "0", float64(0)},
		{"checkbox", false, "false", false},
		{"choices", []string{"one"}, []any{"one", 1}, []string{"one"}},
		{"supplier", "supplier-one", map[string]any{"id": "supplier-one"}, operation.ID("supplier-one")},
		{"suppliers", []string{"supplier-one"}, []any{"supplier-one", 1}, []operation.ID{"supplier-one"}},
	} {
		t.Run(test.field, func(t *testing.T) {
			for _, input := range []struct {
				value  any
				status string
			}{{test.valid, "checked"}, {test.invalid, "skipped"}} {
				delete(seen, test.field)
				evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{test.field: input.value}, "fields": []string{test.field}}))
				if len(evaluations) != 1 || evaluations[0].Status != input.status {
					t.Fatalf("logical decoding=%#v", evaluations)
				}
				value, called := seen[test.field]
				if input.status == "skipped" && called || input.status == "checked" && (!called || !reflect.DeepEqual(value, test.decoded)) {
					t.Fatalf("callback=%t value=%#v expected=%#v status=%s", called, value, test.decoded, input.status)
				}
			}
		})
	}
}
