package core_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
	"github.com/riducms/ridu/tests/contracts/unifiedfields"
)

func TestUnifiedFieldsRESTLocalAgreement(t *testing.T) {
	app, err := ridu.New(unifiedfields.Config(), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(app.Handler(ridu.HandlerOptions{}))
	user, err := app.Local().Create(t.Context(), "users", store.Values{"name": store.String("Author")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	row := func(key, sku string) store.Value {
		return store.Object(store.Values{"_key": store.String(key), "sku": store.String(sku), "accent": store.String("blue")})
	}
	input := store.Values{
		"title": store.String("Contract"), "sku": store.String(" sku-root "), "author": store.String(user.ID),
		"meta":           store.Object(store.Values{"accent": store.String("green")}),
		"sections":       store.List(store.Object(store.Values{"_key": store.String("A"), "sku": store.String("sku-a"), "products": store.List(row("B", "sku-b"))}), row("C", "sku-c")),
		"content":        store.List(store.Object(store.Values{"_key": store.String("card-1"), "blockType": store.String("card"), "sku": store.String("sku-card"), "accent": store.String("red")})),
		"localizedTitle": store.String("English"), "localizedMeta": store.Object(store.Values{"description": store.String("English description")}),
		"privateNote": store.String("must be redacted"), "presentationHidden": store.String("still readable"),
		"body": richtextblocks.Document(richtextblocks.Block("card", "embedded-1", store.Values{"sku": store.String("sku-embedded"), "accent": store.String("orange")})),
	}
	local, err := app.Local().Create(t.Context(), "unified-articles", input, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(input)
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/unified-articles", strings.NewReader(string(encoded)), "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("REST create: %d %s", response.StatusCode, readBody(t, response))
	}
	var created protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, response, &created)
	assertUnifiedWireValues(t, local.Values, created.Doc)
	if _, leaked := created.Doc["privateNote"]; leaked || created.Doc["presentationHidden"] != "still readable" || created.Doc["summary"] != "Article: Contract" || created.Doc["defaulted"] != "Ready" {
		t.Fatalf("output: %#v", created.Doc)
	}
	restID := created.Doc["id"].(string)
	patch := store.Values{"sections": store.List(row("C", "sku-c"), store.Object(store.Values{"_key": store.String("A")})), "localizedTitle": store.String("Français")}
	local, err = app.Local().UpdateWithOptions(t.Context(), "unified-articles", local.ID, patch, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(patch)
	response = requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/unified-articles/"+restID+"?locale=fr", strings.NewReader(string(encoded)), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("REST patch: %d %s", response.StatusCode, readBody(t, response))
	}
	decodeResponse(t, response, &created)
	assertUnifiedWireValues(t, local.Values, created.Doc)
	all, err := app.Local().FindWithOptions(t.Context(), "unified-articles", restID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/unified-articles/"+restID+"?locale=all", nil, "")
	decodeResponse(t, response, &created)
	assertUnifiedWireValues(t, all.Values, created.Doc)
	authorPath, _ := query.NewPath("author")
	populated, err := app.Local().FindWithOptions(t.Context(), "unified-articles", restID, ridu.FindOptions{Populate: []query.Population{{Path: authorPath}}})
	if err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/unified-articles/"+restID+"?populate="+url.QueryEscape(`{"author":true}`), nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("REST population: %d %s", response.StatusCode, readBody(t, response))
	}
	decodeResponse(t, response, &created)
	assertUnifiedWireValues(t, populated.Values, created.Doc)
	if author, ok := populated.Values["author"].CopyDocument(); !ok || author.ID != user.ID {
		t.Fatal("Local API did not retain the logical populated-document carrier")
	}
	// Attached query restriction is visible in the canonical schema, and both
	// transports reject probes before evaluating field read access.
	path, _ := query.NewPath("privateNote")
	_, err = app.Local().List(t.Context(), "unified-articles", ridu.ListOptions{Where: query.Equal(path, query.String("must be redacted"))})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || failure.Code != "field_access_denied" {
		t.Fatalf("local protected query: %v", err)
	}
	response = requestJSON(t, client, http.MethodGet, "http://ridu.test/api/collections/unified-articles?where="+url.QueryEscape(`{"privateNote":{"equals":"must be redacted"}}`), nil, "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("REST protected query: %d", response.StatusCode)
	}
	response.Body.Close()
	// Same-value input still needs admission; Local and REST use the same binding.
	denied := store.Values{"title": store.String("Locked"), "sku": store.String("SKU-ROOT")}
	_, err = app.Local().Update(t.Context(), "unified-articles", local.ID, denied, nil)
	if !errors.As(err, &failure) || failure.Code != "field_access_denied" {
		t.Fatalf("local admission: %v", err)
	}
	response = requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/unified-articles/"+restID, strings.NewReader(`{"title":"Locked","sku":"SKU-ROOT"}`), "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("REST admission: %d %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
}

func assertUnifiedWireValues(t *testing.T, values store.Values, wire map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var logical map[string]any
	if err := json.Unmarshal(encoded, &logical); err != nil {
		t.Fatal(err)
	}
	for key, value := range logical {
		if !reflect.DeepEqual(value, wire[key]) {
			t.Fatalf("%s: local=%#v REST=%#v", key, value, wire[key])
		}
	}
}

func TestUnifiedSaveIssuesFollowServerReorderAndEmbeddedIdentity(t *testing.T) {
	config := unifiedfields.Config()
	config.Collections[1].Hooks.BeforeChange = []ridu.Hook{func(ctx ridu.HookContext) error {
		rows, _ := ctx.Data["sections"].CopyList()
		if len(rows) == 2 {
			ctx.Data["sections"] = store.List(rows[1], rows[0])
		}
		return nil
	}}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	input := store.Values{"title": store.String("Issues"), "sections": store.List(
		store.Object(store.Values{"_key": store.String("A"), "products": store.List(store.Object(store.Values{"_key": store.String("B"), "sku": store.String("invalid")}))}),
		store.Object(store.Values{"_key": store.String("C")})),
		"body": richtextblocks.Document(richtextblocks.Block("card", "embedded-A", store.Values{"sku": store.String("invalid")})),
	}
	_, err = app.Local().Create(t.Context(), "unified-articles", input, nil)
	var local *ridu.OperationError
	if !errors.As(err, &local) || len(local.Issues) != 2 {
		t.Fatalf("local issues: %v", err)
	}
	encoded, _ := json.Marshal(input)
	response := requestJSON(t, handlerClient(app.Handler(ridu.HandlerOptions{})), http.MethodPost, "http://ridu.test/api/collections/unified-articles", strings.NewReader(string(encoded)), "")
	if response.StatusCode != 422 {
		t.Fatalf("REST issues: %d %s", response.StatusCode, readBody(t, response))
	}
	var wire protocol.ErrorEnvelope
	decodeResponse(t, response, &wire)
	for i, issue := range local.Issues {
		if wire.Error.Issues[i].Path != issue.Path || wire.Error.Issues[i].Target != issue.Target || issue.Target == "" {
			t.Fatalf("issue agreement: %#v %#v", local.Issues, wire.Error.Issues)
		}
		var token []string
		if err := json.Unmarshal([]byte(issue.Target), &token); err != nil {
			t.Fatal(err)
		}
		if i == 0 && (issue.Path != "sections.1.products.0.sku" || token[1] != "A" || token[4] != "B") {
			t.Fatalf("server reorder changed stable issue: %#v %v", issue, token)
		}
		if i == 1 && (issue.Path != "body.root.children.0.fields.sku" || token[4] != "embedded-A") {
			t.Fatalf("embedded issue: %#v %v", issue, token)
		}
	}
}

func TestUnifiedComputedOutputUsesBoundContextSelectionAndRedaction(t *testing.T) {
	var contexts []operation.ReadContext
	output := field.Virtual("summary", field.ValueString, func(ctx operation.ReadContext) (operation.Value[store.Value], error) {
		contexts = append(contexts, ctx)
		value, _ := ctx.Root.String("title")
		return operation.Present(store.String(value)), nil
	}).ReadHooks(field.ReadHooks[store.Value]{AfterRead: []field.OutputTransform[store.Value]{func(_ operation.ReadContext, v operation.Value[store.Value]) (operation.Change[store.Value], error) {
		value, _ := v.Get()
		text, _ := value.StringValue()
		return operation.Replace(operation.Present(store.String(text + "!"))), nil
	}}})
	app, err := ridu.New(ridu.Config{Name: "Bound output", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title"), output, output.Rename("secret").Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }})}}}, Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("title"), output}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("Hello")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := doc.Values["summary"].StringValue(); value != "Hello!" {
		t.Fatalf("summary=%q", value)
	}
	if _, leaked := doc.Values["secret"]; leaked {
		t.Fatal("read-denied output leaked")
	}
	if len(contexts) != 2 || contexts[0].ID != operation.ID(doc.ID) || contexts[0].OccurrenceID == "" || contexts[0].OccurrenceID == contexts[0].SchemaOccurrenceID {
		t.Fatalf("computed context=%#v", contexts)
	}
	count := len(contexts)
	_, err = app.Local().FindWithOptions(t.Context(), "pages", doc.ID, ridu.FindOptions{OutputFields: []query.Path{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != count {
		t.Fatal("unselected resolver executed")
	}
	global, err := app.Local().UpdateGlobal(t.Context(), "settings", store.Values{"title": store.String("Global")}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := global.Values["summary"].StringValue(); value != "Global!" {
		t.Fatalf("global=%#v", global)
	}
	if contexts[len(contexts)-1].GlobalID == schema.StableID("") {
		t.Fatal("missing global identity")
	}
}

func TestUnifiedComputedOutputRejectsMissingAndInvalidResolvers(t *testing.T) {
	allow := field.Access{Read: func(operation.AccessContext) (bool, error) { return true, nil }}
	_, err := ridu.Resolve(ridu.Config{Name: "Missing output resolver", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Virtual("summary", field.ValueString, nil).Access(allow)}}}})
	if err == nil {
		t.Fatal("missing output resolver accepted")
	}
	for _, owner := range []string{"field", "resource"} {
		t.Run(owner, func(t *testing.T) {
			output := field.Virtual("summary", field.ValueString, func(operation.ReadContext) (operation.Value[store.Value], error) {
				return operation.Present(store.String("valid")), nil
			})
			collection := ridu.Collection{Slug: "pages"}
			if owner == "field" {
				output = output.ReadHooks(field.ReadHooks[store.Value]{AfterRead: []field.OutputTransform[store.Value]{func(operation.ReadContext, operation.Value[store.Value]) (operation.Change[store.Value], error) {
					return operation.Replace(operation.Present(store.Number(42))), nil
				}}})
			} else {
				collection.Hooks.AfterRead = []ridu.Hook{func(ctx ridu.HookContext) error { ctx.Document.Values["summary"] = store.Number(42); return nil }}
			}
			collection.Fields = field.Fields{field.Text("title"), output}
			app, err := ridu.New(ridu.Config{Name: "Output kind", Collections: []ridu.Collection{collection}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("Test")}, nil)
			var failure *ridu.OperationError
			if !errors.As(err, &failure) || failure.Code != "invalid_computed_value" {
				t.Fatalf("invalid output exposed: %v", err)
			}
		})
	}
}

func TestUnifiedCapabilitiesUseAttachedRulesForEmptyAndRepeatedFields(t *testing.T) {
	app, err := ridu.New(unifiedfields.Config(), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	data := store.Values{"title": store.String("Locked"), "sections": store.List(store.Object(store.Values{"_key": store.String("A")}))}
	capabilities, err := app.Local().Capabilities(t.Context(), "unified-articles", "", ridu.CapabilityOptions{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if entry, found := capabilities.Fields["privateNote"]; !found || entry.Read {
		t.Fatalf("missing scalar read denial: %#v", capabilities.Fields)
	}
	for _, path := range []string{"sku", "sections.0.sku", "sections.sku"} {
		if entry, found := capabilities.Fields[path]; !found || entry.Create || entry.Update {
			t.Fatalf("missing attached write denial at %s: %#v", path, capabilities.Fields)
		}
	}
	client := handlerClient(app.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/access/collections/unified-articles", strings.NewReader(`{}`), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("access response %d %s", response.StatusCode, readBody(t, response))
	}
	var wire protocol.AccessCapabilitiesEnvelope
	decodeResponse(t, response, &wire)
	if entry, found := wire.Fields["privateNote"]; !found || entry.Read {
		t.Fatalf("REST missing read denial: %#v", wire.Fields)
	}
}

func TestUnifiedCapabilitiesConjoinAttachedRestrictions(t *testing.T) {
	allow := func(operation.AccessContext) (bool, error) { return true, nil }
	deny := func(operation.AccessContext) (bool, error) { return false, nil }
	text := field.Text("title").Access(field.Access{Update: allow}).RestrictAccess(field.Access{Update: deny})
	app, err := ridu.New(ridu.Config{Name: "Attached access", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{text, field.Array("rows", field.Fields{text})}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	data := store.Values{"title": store.String("T"), "rows": store.List(store.Object(store.Values{"_key": store.String("A"), "title": store.String("Row")}))}
	capabilities, err := app.Local().Capabilities(t.Context(), "pages", "", ridu.CapabilityOptions{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"title", "rows.title", "rows.0.title"} {
		if capabilities.Fields[path].Update {
			t.Fatalf("attached restriction lost at %s", path)
		}
	}
}

func TestUnifiedComputedAllLocalesContextDescribesItsView(t *testing.T) {
	localizedCalls := 0
	title := field.Text("title").Localized().ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{func(ctx operation.ReadContext, value operation.Value[string]) (operation.Change[string], error) {
		localizedCalls++
		if ctx.AllLocales || ctx.Locale == "" {
			t.Errorf("localized callback did not receive an exact view: %#v", ctx)
		}
		return operation.Keep[string](), nil
	}}})
	output := field.Virtual("canonical", field.ValueBoolean, func(ctx operation.ReadContext) (operation.Value[store.Value], error) {
		_, object := ctx.Root.Get("title").CopyObject()
		if object != ctx.AllLocales {
			t.Errorf("all-locales flag disagrees with root view")
		}
		return operation.Present(store.Boolean(ctx.AllLocales)), nil
	})
	config := ridu.Config{Name: "Localized output", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{title, output}}}}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := app.Local().Create(t.Context(), "pages", store.Values{"title": store.String("English")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := doc.Values["canonical"].BooleanValue(); value {
		t.Fatal("exact output marked canonical")
	}
	doc, err = app.Local().FindWithOptions(t.Context(), "pages", doc.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := doc.Values["canonical"].BooleanValue(); !value {
		t.Fatal("canonical output lost all-locales context")
	}
	if localizedCalls != 2 {
		t.Fatalf("localized callbacks=%d", localizedCalls)
	}
}

func TestUnifiedRequiredReadOutputCannotBecomeNull(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "row"}[nested], func(t *testing.T) {
			text := field.Text("title").Required().ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{func(operation.ReadContext, operation.Value[string]) (operation.Change[string], error) {
				return operation.Replace(operation.Empty[string]()), nil
			}}})
			fields := field.Fields{text}
			values := store.Values{"title": store.String("Valid input")}
			if nested {
				fields = field.Fields{field.Array("rows", fields)}
				values = store.Values{"rows": store.List(store.Object(values))}
			}
			app, err := ridu.New(ridu.Config{Name: "Output nullability", Collections: []ridu.Collection{{Slug: "pages", Fields: fields}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Local().Create(t.Context(), "pages", values, nil)
			var failure *ridu.OperationError
			if !errors.As(err, &failure) || failure.Code != "invalid_field_output" {
				t.Fatalf("invalid null output: %v", err)
			}
		})
	}
}
