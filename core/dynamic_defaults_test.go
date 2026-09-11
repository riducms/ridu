package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func dynamicDefaultsApp(t *testing.T, fields field.Fields) *ridu.App {
	t.Helper()
	app, err := ridu.New(ridu.Config{Name: "Dynamic defaults", Collections: []ridu.Collection{{Slug: "pages", Fields: fields}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestDynamicDefaultsSupportedLogicalValuesAndOmission(t *testing.T) {
	calls := map[string]int{}
	text := func(name, value string) field.DefaultFunc[string] {
		return func(operation.Context) (operation.Value[string], error) {
			calls[name]++
			return operation.Present(value), nil
		}
	}
	values := []string{"one", "two"}
	app := dynamicDefaultsApp(t, field.Fields{
		field.Text("title").DefaultFrom(text("title", "Untitled")),
		field.Textarea("description").DefaultFrom(text("description", "Description")),
		field.Code("source").DefaultFrom(text("source", "const a = 1")),
		field.Email("email").DefaultFrom(text("email", "hello@example.com")),
		field.Date("date").Format(field.DateTime).DefaultFrom(text("date", "2026-09-08T10:00:00Z")),
		field.Select("choice", values...).DefaultFrom(text("choice", "one")),
		field.Radio("radio", values...).DefaultFrom(text("radio", "two")),
		field.Number("count").DefaultFrom(func(operation.Context) (operation.Value[float64], error) {
			calls["count"]++
			return operation.Present(float64(0)), nil
		}),
		field.Checkbox("enabled").DefaultFrom(func(operation.Context) (operation.Value[bool], error) {
			calls["enabled"]++
			return operation.Present(false), nil
		}),
		field.MultiSelect("roles", values...).DefaultFrom(func(operation.Context) (operation.Value[[]string], error) {
			calls["roles"]++
			return operation.Present([]string{}), nil
		}),
		field.Text("empty").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
			calls["empty"]++
			return operation.Empty[string](), nil
		}),
	})
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{"title": "Untitled", "description": "Description", "source": "const a = 1", "email": "hello@example.com", "date": "2026-09-08T10:00:00Z", "choice": "one", "radio": "two"} {
		if actual, ok := created.Values[name].StringValue(); !ok || actual != expected {
			t.Fatalf("%s = %#v, want %q", name, created.Values[name], expected)
		}
	}
	if got, ok := created.Values["count"].NumberValue(); !ok || got != 0 {
		t.Fatalf("zero default lost: %#v", created.Values["count"])
	}
	if got, ok := created.Values["enabled"].BooleanValue(); !ok || got {
		t.Fatalf("false default lost: %#v", created.Values["enabled"])
	}
	if got, ok := created.Values["roles"].CopyList(); !ok || len(got) != 0 {
		t.Fatalf("empty-list default lost: %#v", created.Values["roles"])
	}
	if got, exists := created.Values["empty"]; exists && got.Kind() != store.ValueNull {
		t.Fatalf("Empty supplied a value: %#v", got)
	}
	for name, count := range calls {
		if count != 1 {
			t.Fatalf("%s regenerated across operation phases: calls=%d", name, count)
		}
	}
	before := len(calls)
	for name := range calls {
		calls[name] = 0
	}
	explicit := store.Values{
		"title": store.Null(), "description": store.String(""), "source": store.String(""), "email": store.String(""), "date": store.Null(),
		"choice": store.Null(), "radio": store.Null(), "count": store.Number(0), "enabled": store.Boolean(false), "roles": store.List(), "empty": store.Null(),
	}
	document, err := app.Local().Create(t.Context(), "pages", explicit, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range explicit {
		if !reflect.DeepEqual(document.Values[name], expected) {
			t.Fatalf("explicit %s changed: %#v, want %#v", name, document.Values[name], expected)
		}
	}
	if len(calls) != before {
		t.Fatal("unexpected callback")
	}
	for name, count := range calls {
		if count != 0 {
			t.Fatalf("default replaced explicit %s: calls=%d", name, count)
		}
	}
}

func TestDynamicDefaultsStillValidateAndAbortOnFailure(t *testing.T) {
	failure := errors.New("default service unavailable")
	for _, test := range []struct {
		name string
		node field.Node
		code string
	}{
		{"empty required", field.Text("value").Required().DefaultFrom(func(operation.Context) (operation.Value[string], error) { return operation.Empty[string](), nil }), "required"},
		{"empty required string", field.Text("value").Required().DefaultFrom(func(operation.Context) (operation.Value[string], error) { return operation.Present(""), nil }), "required"},
		{"constraint", field.Text("value").MaxLength(3).DefaultFrom(func(operation.Context) (operation.Value[string], error) {
			return operation.Present("too long"), nil
		}), "max_length"},
		{"email", field.Email("value").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
			return operation.Present("not an email"), nil
		}), "invalid_email"},
		{"date", field.Date("value").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
			return operation.Present("2026-99-99"), nil
		}), "invalid_date"},
		{"choice", field.Select("value", "one").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
			return operation.Present("unknown"), nil
		}), "invalid_option"},
		{"nonfinite number", field.Number("value").DefaultFrom(func(operation.Context) (operation.Value[float64], error) {
			return operation.Present(math.NaN()), nil
		}), ""},
		{"callback error", field.Text("value").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
			return operation.Present("discard"), failure
		}), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := dynamicDefaultsApp(t, field.Fields{test.node})
			_, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
			if err == nil {
				t.Fatal("invalid default reached storage")
			}
			if test.name == "callback error" && !errors.Is(err, failure) {
				t.Fatalf("callback error lost: %v", err)
			}
			if test.code != "" {
				var failed *ridu.OperationError
				if !errors.As(err, &failed) || failed.Status != 422 || len(failed.Issues) == 0 || failed.Issues[0].Code != test.code || failed.Issues[0].Path != "value" {
					t.Fatalf("validation result = %#v, %v", failed, err)
				}
			}
			page, readErr := app.Local().List(t.Context(), "pages", ridu.ListOptions{})
			if readErr != nil || len(page.Documents) != 0 {
				t.Fatalf("failed default left a document: %#v %v", page, readErr)
			}
		})
	}
}

func TestDynamicDefaultsInitializeOnlyNewScopes(t *testing.T) {
	var seen []operation.Context
	initial := func(ctx operation.Context) (operation.Value[string], error) {
		seen = append(seen, ctx)
		label, _ := ctx.Siblings.String("label")
		return operation.Present("default:" + label), nil
	}
	children := field.Fields{field.Text("label"), field.Text("value").DefaultFrom(initial)}
	app := dynamicDefaultsApp(t, field.Fields{field.Text("title").DefaultFrom(initial), field.Group("meta", children), field.Array("rows", children), field.Blocks("content", field.Block{Slug: "card", Fields: children})})
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Operation != operation.Create || seen[0].CollectionID != "pages" || seen[0].GlobalID != "" || seen[0].Local == nil {
		t.Fatalf("create context = %#v", seen)
	}
	if _, hasPrior := seen[0].Prior.Lookup("title"); hasPrior {
		t.Fatal("create acquired persisted prior values")
	}
	if value, exists := created.Values["meta"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("child default materialized optional parent: %#v", value)
	}
	seen = nil
	row := func(key, label string) store.Value {
		return store.Object(store.Values{"_key": store.String(key), "label": store.String(label)})
	}
	block := func(key, label string) store.Value {
		return store.Object(store.Values{"_key": store.String(key), "blockType": store.String("card"), "label": store.String(label)})
	}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"meta": store.Object(store.Values{"label": store.String("group")}), "rows": store.List(row("A", "row")), "content": store.List(block("C", "block"))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Fatalf("new object/row/block callback count=%d", len(seen))
	}
	for _, ctx := range seen {
		if ctx.Operation != operation.Update || string(ctx.ID) != created.ID {
			t.Fatalf("new scope lost enclosing update identity: %#v", ctx)
		}
		if prior, exists := ctx.Prior.Lookup("label"); exists && prior.Kind() != store.ValueNull {
			t.Fatalf("new scope acquired prior label: %#v", prior)
		}
		if title, _ := ctx.Root.String("title"); title != "default:" {
			t.Fatalf("new-scope root lost retained title: %q; root=%#v original=%#v", title, ctx.Root.Get("title"), created.Values)
		}
	}
	meta, _ := updated.Values["meta"].CopyObject()
	rows, _ := updated.Values["rows"].CopyList()
	first, _ := rows[0].CopyObject()
	blocks, _ := updated.Values["content"].CopyList()
	card, _ := blocks[0].CopyObject()
	for name, values := range map[string]store.Values{"group": meta, "row": first, "block": card} {
		if value, _ := values["value"].StringValue(); value != "default:"+name {
			t.Fatalf("new %s default=%#v", name, values)
		}
	}
	seen = nil
	updated, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"meta": store.Object(store.Values{"label": store.String("edited")}), "rows": store.List(row("B", "new"), row("A", "edited")), "content": store.List(block("C", "edited"), block("D", "new"))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("retained scopes reran callbacks or new rows were skipped: %d", len(seen))
	}
	rows, _ = updated.Values["rows"].CopyList()
	first, _ = rows[1].CopyObject()
	if value, _ := first["value"].StringValue(); value != "default:row" {
		t.Fatalf("reordered row lost retained default: %#v", first)
	}
	seen = nil
	if _, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{}); err != nil || len(seen) != 0 {
		t.Fatalf("read ran defaults: calls=%d error=%v", len(seen), err)
	}
}

func TestDynamicDefaultsRetainExistingPortableEmpty(t *testing.T) {
	backend := teststore.New()
	config := ridu.Config{Name: "Schema evolution", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title"), field.Array("rows", field.Fields{field.Text("label")})}}}}
	old, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := old.Local().Create(t.Context(), "pages", store.Values{"rows": store.List(store.Object(store.Values{"_key": store.String("A"), "label": store.String("old")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	initial := func(operation.Context) (operation.Value[string], error) {
		calls++
		return operation.Present("new default"), nil
	}
	config.Collections[0].Fields = field.Fields{field.Text("title").DefaultFrom(initial), field.Array("rows", field.Fields{field.Text("label"), field.Text("value").DefaultFrom(initial)})}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"rows": store.List(store.Object(store.Values{"_key": store.String("A"), "label": store.String("edited")}))}, ridu.MutationOptions{})
	if err != nil || calls != 0 {
		t.Fatalf("schema addition backfilled retained values: calls=%d err=%v", calls, err)
	}
	rowValues, _ := updated.Values["rows"].CopyList()
	row, _ := rowValues[0].CopyObject()
	for _, value := range []store.Value{updated.Values["title"], row["value"]} {
		if value.Kind() != store.ValueNull && value.Kind() != (store.Value{}).Kind() {
			t.Fatalf("portable empty changed: %#v", value)
		}
	}
}

func TestDynamicDefaultsRespectExactLocaleInitialization(t *testing.T) {
	var seen []operation.Context
	initial := func(ctx operation.Context) (operation.Value[string], error) {
		seen = append(seen, ctx)
		return operation.Present("initial:" + string(ctx.Locale)), nil
	}
	config := ridu.Config{Name: "Localized defaults", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("title").Localized().DefaultFrom(initial), field.Group("seo", field.Fields{field.Text("label"), field.Text("value").DefaultFrom(initial)}).Localized(),
		field.Array("rows", field.Fields{field.Text("label"), field.Text("translation").Localized().DefaultFrom(initial)}),
	}}}}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"seo": store.Object(store.Values{"label": store.String("English")}), "rows": store.List(store.Object(store.Values{"_key": store.String("A"), "label": store.String("English")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Fatalf("initial calls=%d", len(seen))
	}
	seen = nil
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"seo": store.Object(store.Values{"label": store.String("French")}), "rows": store.List(store.Object(store.Values{"_key": store.String("A"), "label": store.String("edited")}), store.Object(store.Values{"_key": store.String("B"), "label": store.String("new")}))}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("French new group/row callbacks=%d, want 2", len(seen))
	}
	for _, ctx := range seen {
		if ctx.Locale != "fr" || ctx.Operation != operation.Update || ctx.AllLocales {
			t.Fatalf("exact callback=%#v", ctx)
		}
		if value, exists := ctx.Prior.Lookup("value"); exists && value.Kind() != store.ValueNull {
			t.Fatalf("fallback became prior=%#v", value)
		}
	}
	_ = updated
	all, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	title, _ := all.Values["title"].CopyObject()
	if _, exists := title["fr"]; exists {
		t.Fatalf("omitted retained title default persisted fallback translation: %#v", title)
	}
	seoLocales, _ := all.Values["seo"].CopyObject()
	seo, _ := seoLocales["fr"].CopyObject()
	if stringValue(seo["value"]) != "initial:fr" {
		t.Fatalf("new exact group default=%#v", seo)
	}
	rows, _ := all.Values["rows"].CopyList()
	first, _ := rows[0].CopyObject()
	translations, _ := first["translation"].CopyObject()
	if _, exists := translations["fr"]; exists {
		t.Fatalf("retained row acquired omitted translation: %#v", translations)
	}
	second, _ := rows[1].CopyObject()
	translations, _ = second["translation"].CopyObject()
	if stringValue(translations["fr"]) != "initial:fr" {
		t.Fatalf("new row lost exact default: %#v", translations)
	}
	seen = nil
	if _, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Locale: "fr"}); err != nil || len(seen) != 0 {
		t.Fatalf("fallback read ran default: %v %d", err, len(seen))
	}
}

func TestDynamicDefaultsHookCheckpointAndProvenance(t *testing.T) {
	calls, admissions := 0, 0
	var events []string
	useRaw := false
	node := field.Text("protected").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
		calls++
		events = append(events, "default")
		return operation.Present("default"), nil
	}).Access(field.Access{
		Create: func(operation.Context) (bool, error) { admissions++; return false, nil },
		Read:   func(ctx operation.Context) (bool, error) { return ctx.Actor.ID == "reader", nil },
	}).Hooks(field.Hooks[string]{
		BeforeValidate: []field.RawTransform{func(_ operation.Context, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
			events = append(events, "raw")
			if useRaw {
				return operation.Replace(operation.Present(store.String("raw value"))), nil
			}
			return operation.Keep[store.Value](), nil
		}},
		BeforeChange: []field.Transform[string]{func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
			text, _ := value.Get()
			events = append(events, "typed:"+text)
			return operation.Keep[string](), nil
		}},
	})
	app := dynamicDefaultsApp(t, field.Fields{node})
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
	if err != nil || calls != 1 || admissions != 0 {
		t.Fatalf("default provenance calls=%d access=%d error=%v", calls, admissions, err)
	}
	if !reflect.DeepEqual(events, []string{"raw", "default", "typed:default"}) {
		t.Fatalf("checkpoint ordering=%v", events)
	}
	if _, exists := created.Values["protected"]; exists {
		t.Fatal("server default bypassed final read redaction")
	}
	read, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{Actor: &store.Document{ID: "reader"}})
	if err != nil || stringValue(read.Values["protected"]) != "default" {
		t.Fatalf("default was not persisted: %#v %v", read.Values, err)
	}
	for _, submitted := range []store.Value{store.String("default"), store.Null(), store.String("")} {
		if _, err := app.Local().Create(t.Context(), "pages", store.Values{"protected": submitted}, ridu.MutationOptions{}); !fieldAccessIssue(err, "protected") {
			t.Fatalf("explicit input bypassed access: %v", err)
		}
	}
	calls = 0
	events = nil
	useRaw = true
	_, err = app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
	if err != nil || calls != 0 || !reflect.DeepEqual(events, []string{"raw", "typed:raw value"}) {
		t.Fatalf("raw-supplied value defaulted: calls=%d events=%v error=%v", calls, events, err)
	}
}

func TestDynamicDefaultsDuplicateAndRestoreKeepExistingValues(t *testing.T) {
	backend := teststore.New()
	calls := 0
	initial := func(operation.Context) (operation.Value[string], error) {
		calls++
		return operation.Present(fmt.Sprintf("generated-%d", calls)), nil
	}
	config := ridu.Config{Name: "Default versions", Collections: []ridu.Collection{{Slug: "pages", Versions: true, Fields: field.Fields{field.Text("title").DefaultFrom(initial), field.Text("unique").Unique().DefaultFrom(initial)},
		// A duplicate retains copied values. An application can explicitly clear
		// a unique value at its existing before-duplicate checkpoint to initialize it.
		Hooks: ridu.CollectionHooks{BeforeDuplicate: []ridu.Hook{func(ctx ridu.HookContext) error { delete(ctx.Data, "unique"); return nil }}},
	}}}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("create defaults=%d", calls)
	}
	duplicated, err := app.Local().Duplicate(t.Context(), "pages", created.ID, nil, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || stringValue(duplicated.Values["title"]) != stringValue(created.Values["title"]) || stringValue(duplicated.Values["unique"]) == stringValue(created.Values["unique"]) {
		t.Fatalf("duplicate defaults=%d values=%#v", calls, duplicated.Values)
	}
	config.Collections[0].Fields = append(config.Collections[0].Fields, field.Text("newField").DefaultFrom(initial))
	expanded, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := expanded.Local().PublishChanges(t.Context(), "pages", created.ID, store.Values{"title": store.String("changed"), "newField": store.String("current")}, ridu.MutationOptions{ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := expanded.Local().Restore(t.Context(), "pages", created.ID, created.Revision, ridu.MutationOptions{ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || stringValue(restored.Values["title"]) != stringValue(created.Values["title"]) {
		t.Fatalf("restore regenerated defaults: calls=%d values=%#v", calls, restored.Values)
	}
	if value, exists := restored.Values["newField"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("restore backfilled absent snapshot field: %#v", value)
	}
}

func TestDynamicDefaultsBoundReaderAuthorizationLocaleAndCancellation(t *testing.T) {
	backend := teststore.New()
	var targetID string
	var seen operation.Context
	var readActors []string
	var cancel context.CancelFunc
	initial := func(ctx operation.Context) (operation.Value[string], error) {
		seen = ctx
		if cancel != nil {
			cancel()
		}
		doc, err := ctx.Local.FindByID(context.Background(), "policies", operation.ID(targetID))
		if err != nil {
			return operation.Empty[string](), err
		}
		if value, ok := doc.Values["name"].StringValue(); ok {
			return operation.Present(value), nil
		}
		return operation.Present("no exact translation"), nil
	}
	config := ridu.Config{Name: "Default reader", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}}, Collections: []ridu.Collection{
		{Slug: "policies", Fields: field.Fields{field.Text("name").Localized()}, Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
			id := ""
			if ctx.Actor != nil {
				id = ctx.Actor.ID
			}
			readActors = append(readActors, id)
			if id != "allowed" {
				return ridu.Deny(), nil
			}
			return ridu.Allow(), nil
		}}},
		{Slug: "pages", Fields: field.Fields{field.Text("title").DefaultFrom(initial)}},
	}}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	target, err := app.Local().Create(t.Context(), "policies", store.Values{"name": store.String("English policy")}, ridu.MutationOptions{Actor: &store.Document{ID: "allowed"}})
	if err != nil {
		t.Fatal(err)
	}
	targetID = target.ID
	before := count(backend.Events(), "begin")
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{Actor: &store.Document{ID: "allowed"}, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(created.Values["title"]) != "no exact translation" || seen.Actor.ID != "allowed" || seen.Locale != "fr" || seen.Context == nil {
		t.Fatalf("default reader context/value=%#v %#v", seen, created.Values)
	}
	if count(backend.Events(), "begin") != before+1 {
		t.Fatalf("bound reader opened nested transaction: %v", backend.Events())
	}
	if _, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{Actor: &store.Document{ID: "denied"}}); err == nil {
		t.Fatal("bound reader bypassed actor access")
	}
	if len(readActors) == 0 || readActors[len(readActors)-1] != "denied" {
		t.Fatalf("bound reader actor=%v", readActors)
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	cancel = stop
	if _, err := app.Local().Create(ctx, "pages", store.Values{}, ridu.MutationOptions{Actor: &store.Document{ID: "allowed"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("bound reader escaped original cancellation: %v", err)
	}
}

func TestDynamicDefaultsRESTAndEmbeddedLifecycle(t *testing.T) {
	calls := 0
	initial := func(operation.Context) (operation.Value[string], error) {
		calls++
		return operation.Present("Server title"), nil
	}
	config := embeddedConfig(field.Text("title").Required().DefaultFrom(initial))
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	values := store.Values{"body": outline.Value(outline.Widget("card", "A", store.Values{}))}
	created, err := app.Local().Create(t.Context(), "pages", values, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || embeddedString(embeddedPayload(t, created.Values["body"], 0), "title") != "Server title" {
		t.Fatalf("embedded initial default calls=%d values=%#v", calls, created.Values)
	}
	encoded, _ := json.Marshal(values)
	response := requestJSON(t, handlerClient(app.Handler(ridu.HandlerOptions{})), http.MethodPost, "http://ridu.test/api/collections/pages", strings.NewReader(string(encoded)), "")
	if response.StatusCode != 201 {
		t.Fatalf("REST embedded save=%d %s", response.StatusCode, readBody(t, response))
	}
	var envelope struct{ Doc map[string]json.RawMessage }
	decodeResponse(t, response, &envelope)
	var body store.Value
	if err := json.Unmarshal(envelope.Doc["body"], &body); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || embeddedString(embeddedPayload(t, body, 0), "title") != "Server title" {
		t.Fatalf("REST embedded default=%#v calls=%d", body, calls)
	}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": outline.Value(outline.Widget("card", "A", store.Values{"caption": store.String("edited")}), outline.Widget("card", "B", store.Values{}))}, ridu.MutationOptions{})
	if err != nil || calls != 3 {
		t.Fatalf("embedded retained/new default calls=%d err=%v", calls, err)
	}
	if embeddedString(embeddedPayload(t, updated.Values["body"], 0), "title") != "Server title" {
		t.Fatal("retained embedded title lost")
	}
}

func TestDynamicDefaultsCacheResultsAcrossLaterHookDeletion(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%t", empty), func(t *testing.T) {
			calls := 0
			app, err := ridu.New(ridu.Config{Name: "Stable defaults", Collections: []ridu.Collection{{
				Slug: "pages", Fields: field.Fields{field.Text("value").DefaultFrom(func(operation.Context) (operation.Value[string], error) {
					calls++
					if empty {
						return operation.Empty[string](), nil
					}
					return operation.Present(fmt.Sprintf("generated-%d", calls)), nil
				})},
				Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error { delete(ctx.Data, "value"); return nil }}},
			}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
			if err != nil || calls != 1 {
				t.Fatalf("later validation reevaluated default: calls=%d error=%v", calls, err)
			}
			if empty {
				if value, exists := created.Values["value"]; exists && value.Kind() != store.ValueNull {
					t.Fatalf("Empty acquired a value: %#v", value)
				}
			} else if value, _ := created.Values["value"].StringValue(); value != "generated-1" {
				t.Fatalf("later validation lost cached default: %#v", created.Values)
			}
		})
	}
}

func TestDynamicDefaultsDetachReturnedListsBeforeLaterCallbacks(t *testing.T) {
	selected := []string{"one"}
	validated := false
	app, err := ridu.New(ridu.Config{Name: "Detached default values", Collections: []ridu.Collection{{
		Slug: "pages", Fields: field.Fields{field.MultiSelect("roles", "one", "two").
			DefaultFrom(func(operation.Context) (operation.Value[[]string], error) {
				return operation.Present(selected), nil
			}).
			Validate(func(_ operation.Context, input operation.Value[[]string]) ([]operation.Issue, error) {
				values, present := input.Get()
				if !present || !reflect.DeepEqual(values, []string{"one"}) {
					return nil, fmt.Errorf("default result mutated before validation: %v", values)
				}
				values[0] = "two"
				validated = true
				return nil, nil
			})},
		Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ridu.HookContext) error { selected[0] = "two"; return nil }}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values, _ := created.Values["roles"].CopyList()
	if !validated || len(values) != 1 || stringValue(values[0]) != "one" {
		t.Fatalf("application callback acquired mutable stored default: %#v", created.Values)
	}
}

func TestDynamicDefaultsGlobalInitializesOnFirstWriteOnly(t *testing.T) {
	calls := 0
	var seen operation.Context
	app, err := ridu.New(ridu.Config{Name: "Global defaults", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title")}}}, Globals: []ridu.Global{{
		Slug: "site", Fields: field.Fields{field.Text("name").DefaultFrom(func(ctx operation.Context) (operation.Value[string], error) {
			calls++
			seen = ctx
			return operation.Present("Ridu"), nil
		})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Global(t.Context(), "site", ridu.FindOptions{}); err != nil || calls != 0 {
		t.Fatalf("missing-global read ran executable defaults: calls=%d error=%v", calls, err)
	}
	created, err := app.Local().UpdateGlobal(t.Context(), "site", store.Values{}, ridu.MutationOptions{})
	if err != nil || calls != 1 || stringValue(created.Values["name"]) != "Ridu" {
		t.Fatalf("first global write: values=%#v calls=%d error=%v", created.Values, calls, err)
	}
	if seen.Operation != operation.Update || seen.GlobalID != "global-site" || seen.CollectionID != "" || seen.ID != "site" {
		t.Fatalf("global callback identity=%#v", seen)
	}
	if _, err := app.Local().UpdateGlobal(t.Context(), "site", store.Values{}, ridu.MutationOptions{ExpectedRevision: created.Revision}); err != nil || calls != 1 {
		t.Fatalf("retained-global write defaulted: calls=%d error=%v", calls, err)
	}
}

func TestDynamicDefaultsCacheFollowsStableRowsAfterHookReorder(t *testing.T) {
	calls := 0
	app, err := ridu.New(ridu.Config{Name: "Reordered initialization", Collections: []ridu.Collection{{
		Slug: "pages", Fields: field.Fields{field.Array("rows", field.Fields{
			field.Text("label"), field.Text("value").DefaultFrom(func(ctx operation.Context) (operation.Value[string], error) {
				calls++
				label, _ := ctx.Siblings.String("label")
				return operation.Present(label), nil
			}),
		})},
		Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
			rows, _ := ctx.Data["rows"].CopyList()
			for i, row := range rows {
				values, _ := row.CopyObject()
				delete(values, "value")
				values["label"] = store.String("edited after initialization")
				rows[i] = store.Object(values)
			}
			rows[0], rows[1] = rows[1], rows[0]
			ctx.Data["rows"] = store.List(rows...)
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"rows": store.List(
		store.Object(store.Values{"_key": store.String("A"), "label": store.String("first")}),
		store.Object(store.Values{"_key": store.String("B"), "label": store.String("second")}),
	)}, ridu.MutationOptions{})
	if err != nil || calls != 2 {
		t.Fatalf("reordered defaults calls=%d error=%v", calls, err)
	}
	rows, _ := created.Values["rows"].CopyList()
	for index, expected := range []string{"second", "first"} {
		row, _ := rows[index].CopyObject()
		if stringValue(row["value"]) != expected {
			t.Fatalf("cached default followed row index: %#v", row)
		}
	}
}

func TestDynamicDefaultsRespectLiteralCreatedAndAbsentOptionalGroups(t *testing.T) {
	calls := 0
	initial := func(operation.Context) (operation.Value[string], error) {
		calls++
		return operation.Present("server"), nil
	}
	mixed := field.Group("settings", field.Fields{
		field.Text("literal").Default("existing literal"),
		field.Text("dynamic").Required().DefaultFrom(initial),
		field.Group("optional", field.Fields{field.Text("value").DefaultFrom(initial)}),
	})
	app := dynamicDefaultsApp(t, field.Fields{
		mixed,
		field.Group("dynamicOnly", field.Fields{field.Text("value").DefaultFrom(initial)}),
		field.Array("rows", field.Fields{mixed}),
		field.Blocks("content", field.Block{Slug: "card", Fields: field.Fields{mixed}}),
	})
	created, err := app.Local().Create(t.Context(), "pages", store.Values{
		"rows":    store.List(store.Object(store.Values{"_key": store.String("A")})),
		"content": store.List(store.Object(store.Values{"_key": store.String("B"), "blockType": store.String("card")})),
	}, ridu.MutationOptions{})
	if err != nil || calls != 3 {
		t.Fatalf("literal-created group initialization: calls=%d error=%v", calls, err)
	}
	rows, _ := created.Values["rows"].CopyList()
	row, _ := rows[0].CopyObject()
	blocks, _ := created.Values["content"].CopyList()
	block, _ := blocks[0].CopyObject()
	for name, value := range map[string]store.Value{"root": created.Values["settings"], "row": row["settings"], "block": block["settings"]} {
		settings, ok := value.CopyObject()
		if !ok || stringValue(settings["literal"]) != "existing literal" || stringValue(settings["dynamic"]) != "server" {
			t.Fatalf("%s literal-created group=%#v", name, settings)
		}
		if optional, exists := settings["optional"]; exists && optional.Kind() != store.ValueNull {
			t.Fatalf("%s dynamic child created optional parent: %#v", name, optional)
		}
	}
	if optional, exists := created.Values["dynamicOnly"]; exists && optional.Kind() != store.ValueNull {
		t.Fatalf("dynamic-only root group was materialized: %#v", optional)
	}
	calls = 0
	_, err = app.Local().Create(t.Context(), "pages", store.Values{
		"settings": store.Null(), "dynamicOnly": store.Null(),
		"rows":    store.List(store.Object(store.Values{"_key": store.String("A"), "settings": store.Null()})),
		"content": store.List(store.Object(store.Values{"_key": store.String("B"), "blockType": store.String("card"), "settings": store.Null()})),
	}, ridu.MutationOptions{})
	if err != nil || calls != 0 {
		t.Fatalf("explicit null parents ran child defaults: calls=%d error=%v", calls, err)
	}
}

func TestDynamicDefaultsShareFrozenCheckpointViews(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		t.Run(fmt.Sprintf("reversed=%t", reversed), func(t *testing.T) {
			seen := map[string]operation.Context{}
			initial := func(name, other string) field.DefaultFunc[string] {
				return func(ctx operation.Context) (operation.Value[string], error) {
					seen[name] = ctx
					if _, present := ctx.Siblings.String(other); present {
						return operation.Empty[string](), fmt.Errorf("%s observed sibling default %s during the same checkpoint", name, other)
					}
					if _, present := ctx.Root.String(other); present {
						return operation.Empty[string](), fmt.Errorf("%s observed root default %s during the same checkpoint", name, other)
					}
					if seed, _ := ctx.Root.String("seed"); seed != "submitted" {
						return operation.Empty[string](), fmt.Errorf("%s lost submitted checkpoint value", name)
					}
					return operation.Present(name), nil
				}
			}
			fields := field.Fields{
				field.Text("seed"),
				field.Text("first").DefaultFrom(initial("first", "second")),
				field.Text("second").DefaultFrom(initial("second", "first")),
			}
			if reversed {
				fields[1], fields[2] = fields[2], fields[1]
			}
			app := dynamicDefaultsApp(t, fields)
			created, err := app.Local().Create(t.Context(), "pages", store.Values{"seed": store.String("submitted")}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(seen) != 2 || stringValue(created.Values["first"]) != "first" || stringValue(created.Values["second"]) != "second" {
				t.Fatalf("independent defaults lost results: contexts=%d values=%#v", len(seen), created.Values)
			}
			for name, ctx := range seen {
				for _, fieldName := range []string{"first", "second"} {
					if _, present := ctx.Siblings.String(fieldName); present {
						t.Fatalf("%s snapshot mutated after callback: %s", name, fieldName)
					}
				}
			}
		})
	}
}

func TestDynamicDefaultsDuplicatePriorMatchesPersistedEnclosingScope(t *testing.T) {
	var rootPrior, rowPrior operation.Context
	app, err := ridu.New(ridu.Config{Name: "Duplicate default context", Collections: []ridu.Collection{{
		Slug: "pages", Fields: field.Fields{
			field.Text("title"),
			field.Text("value").DefaultFrom(func(ctx operation.Context) (operation.Value[string], error) {
				rootPrior = ctx
				return operation.Present("new root"), nil
			}),
			field.Array("rows", field.Fields{field.Text("label"), field.Text("value").DefaultFrom(func(ctx operation.Context) (operation.Value[string], error) {
				rowPrior = ctx
				return operation.Present("new row"), nil
			})}),
		},
		Hooks: ridu.CollectionHooks{BeforeDuplicate: []ridu.Hook{func(ctx ridu.HookContext) error {
			delete(ctx.Data, "value")
			rows, _ := ctx.Data["rows"].CopyList()
			for index, row := range rows {
				values, _ := row.CopyObject()
				delete(values, "value")
				rows[index] = store.Object(values)
			}
			ctx.Data["rows"] = store.List(rows...)
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{
		"title": store.String("source title"), "value": store.String("source root"),
		"rows": store.List(store.Object(store.Values{"_key": store.String("A"), "label": store.String("source row"), "value": store.String("source child")})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	duplicated, err := app.Local().Duplicate(t.Context(), "pages", created.ID, nil, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rootPrior.Operation != operation.Duplicate || stringValue(rootPrior.Prior.Get("value")) != "source root" || stringValue(rootPrior.Prior.Get("title")) != "source title" {
		t.Fatalf("root default lost persisted source scope: %#v", rootPrior)
	}
	rows, _ := duplicated.Values["rows"].CopyList()
	row, _ := rows[0].CopyObject()
	if stringValue(row["_key"]) == "A" {
		t.Fatal("duplicate retained the source repeated identity")
	}
	if rowPrior.Operation != operation.Duplicate || stringValue(rowPrior.Siblings.Get("label")) != "source row" {
		t.Fatalf("new duplicate row context=%#v", rowPrior)
	}
	if _, present := rowPrior.Prior.Lookup("label"); present {
		t.Fatal("rekeyed duplicate row inherited a prior occurrence by array index")
	}
	if stringValue(duplicated.Values["value"]) != "new root" || stringValue(row["value"]) != "new row" {
		t.Fatalf("duplicate reinitialization values=%#v row=%#v", duplicated.Values, row)
	}
}
