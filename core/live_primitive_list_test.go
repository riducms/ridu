package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestLivePrimitiveListStrictTypedValuesAndEmptyStates(t *testing.T) {
	var texts []string
	var numbers []float64
	var present bool
	calls := 0
	app, _ := newLiveApp(t, core.Config{Name: "Live primitive types", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.TextList("points").Required().MaxLength(1).MaxRows(1).LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[[]string]) ([]operation.Issue, error) {
			calls++
			texts, present = value.Get()
			return nil, nil
		}),
		field.NumberList("sizes").Required().Min(1).MaxRows(1).LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[[]float64]) ([]operation.Issue, error) {
			calls++
			numbers, present = value.Get()
			return nil, nil
		}),
	}}}})
	for _, test := range []struct {
		name, path, status string
		value              any
		omit, present      bool
		texts              []string
		numbers            []float64
	}{
		{name: "ordered duplicate text", path: "points", status: "checked", value: []string{"", " Oak ", " Oak "}, present: true, texts: []string{"", " Oak ", " Oak "}},
		{name: "ordered duplicate numbers", path: "sizes", status: "checked", value: []float64{0, 8.5, 8.5}, present: true, numbers: []float64{0, 8.5, 8.5}},
		{name: "empty text list", path: "points", status: "checked", value: []string{}, present: true, texts: []string{}},
		{name: "empty number list", path: "sizes", status: "checked", value: []float64{}, present: true, numbers: []float64{}},
		{name: "omitted text", path: "points", status: "checked", omit: true},
		{name: "omitted numbers", path: "sizes", status: "checked", omit: true},
		{name: "null text", path: "points", status: "checked"},
		{name: "null numbers", path: "sizes", status: "checked"},
		{name: "scalar text", path: "points", status: "skipped", value: "one"},
		{name: "scalar number", path: "sizes", status: "skipped", value: 1},
		{name: "mixed text", path: "points", status: "skipped", value: []any{"one", 2}},
		{name: "mixed numbers", path: "sizes", status: "skipped", value: []any{1, "2"}},
		{name: "null text item", path: "points", status: "skipped", value: []any{"one", nil}},
		{name: "null number item", path: "sizes", status: "skipped", value: []any{1, nil}},
		{name: "object item", path: "points", status: "skipped", value: []any{map[string]any{"value": "one"}}},
		{name: "nested list", path: "sizes", status: "skipped", value: []any{[]any{1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls = 0
			texts = nil
			numbers = nil
			present = false
			data := map[string]any{}
			if !test.omit {
				data[test.path] = test.value
			}
			results := liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{"data": data, "fields": []string{test.path}}))
			if len(results) != 1 || results[0].Status != test.status {
				t.Fatalf("evaluations=%#v", results)
			}
			wantCalls := 0
			if test.status == "checked" {
				wantCalls = 1
			}
			if calls != wantCalls || present != test.present {
				t.Fatalf("calls=%d present=%v", calls, present)
			}
			if !reflect.DeepEqual(texts, test.texts) || !reflect.DeepEqual(numbers, test.numbers) {
				t.Fatalf("typed values texts=%#v numbers=%#v", texts, numbers)
			}
		})
	}
}

func TestLivePrimitiveListPoliciesDoNotRunAuthoritativeLifecycle(t *testing.T) {
	live, save, defaults, hooks := 0, 0, 0, 0
	rule := func(value operation.Value[[]string]) ([]operation.Issue, error) {
		values, _ := value.Get()
		if slices.Contains(values, "invalid") {
			return []operation.Issue{{Code: "list_rule", Message: "Review the complete list"}}, nil
		}
		return nil, nil
	}
	points := field.TextList("points").DefaultFrom(func(operation.Context) (operation.Value[[]string], error) {
		defaults++
		return operation.Present([]string{"default"}), nil
	}).
		Hooks(field.Hooks[[]string]{BeforeValidate: []field.RawTransform{func(operation.Context, operation.Value[store.Value]) (operation.Change[store.Value], error) {
			hooks++
			return operation.Keep[store.Value](), nil
		}}, BeforeChange: []field.Transform[[]string]{func(operation.Context, operation.Value[[]string]) (operation.Change[[]string], error) {
			hooks++
			return operation.Keep[[]string](), nil
		}}}).
		Validate(func(_ operation.Context, value operation.Value[[]string]) ([]operation.Issue, error) {
			save++
			return rule(value)
		}).
		LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[[]string]) ([]operation.Issue, error) {
			live++
			values, ok := value.Get()
			if ok && len(values) > 0 {
				original := values[0]
				values[0] = "mutated slice"
				stored, _ := ctx.Root.Get("points").CopyList()
				text, _ := stored[0].StringValue()
				if text != original {
					t.Fatal("typed slice mutated Root")
				}
				values[0] = original
			}
			return rule(value)
		})
	saveOnlyCalls := 0
	app, backend := newLiveApp(t, core.Config{Name: "Whole list advisory lifecycle", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{points, field.NumberList("sizes").DefaultFrom(func(operation.Context) (operation.Value[[]float64], error) {
		defaults++
		return operation.Present([]float64{0}), nil
	}), field.TextList("saveOnly").Validate(func(operation.Context, operation.Value[[]string]) ([]operation.Issue, error) {
		saveOnlyCalls++
		return nil, nil
	})}}}})
	before := len(backend.Events())
	for _, input := range []map[string]any{{}, {"points": []string{"ok"}}, {"points": []string{"invalid"}}} {
		evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate", map[string]any{"data": input, "fields": []string{"points"}}))
		if len(evaluations) != 1 || evaluations[0].Path != "points" {
			t.Fatalf("evaluations=%#v", evaluations)
		}
	}
	if live != 3 || save != 0 || defaults != 0 || hooks != 0 || saveOnlyCalls != 0 {
		t.Fatalf("live=%d save=%d defaults=%d hooks=%d saveOnly=%d", live, save, defaults, hooks, saveOnlyCalls)
	}
	for _, event := range backend.Events()[before:] {
		if event == "create" || event == "update" || event == "delete" {
			t.Fatal("advisory persisted data")
		}
	}
	response := liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{"saveOnly": []string{"x"}}, "fields": []string{"saveOnly"}})
	if response.Code != 400 || saveOnlyCalls != 0 {
		t.Fatal("save-only list opted in implicitly")
	}
	_, err := app.Local().Create(t.Context(), "products", store.Values{"points": store.List(store.String("invalid"))}, core.MutationOptions{})
	var failure *core.OperationError
	if !errors.As(err, &failure) || failure.Status != 422 {
		t.Fatalf("Local accepted invalid list: %v", err)
	}
	response = liveRequest(t, app, "collections/products", map[string]any{"points": []string{"invalid"}})
	if response.Code != 422 {
		t.Fatalf("REST accepted invalid list: %d %s", response.Code, response.Body.String())
	}
	if live != 3 || save != 2 || hooks == 0 {
		t.Fatalf("save reused advisory or omitted lifecycle live=%d save=%d hooks=%d", live, save, hooks)
	}
}

func TestLivePrimitiveListRetainedIdentityLocaleAndWholeListTargets(t *testing.T) {
	var seen operation.LiveValidationContext
	var current []string
	var available bool
	points := field.TextList("points").LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[[]string]) ([]operation.Issue, error) {
		seen = ctx
		current, available = value.Get()
		return []operation.Issue{{Code: "review_list", Message: "Review the list"}}, nil
	})
	children := field.Fields{field.Text("label"), points, field.NumberList("sizes")}
	app, _ := newLiveApp(t, core.Config{Name: "List identity", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}}, Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.Text("title"), points, points.Rename("translated").Localized(), field.Group("details", children), field.Array("variants", children), field.Blocks("blocks", field.Block{Slug: "card", Fields: children}, field.Block{Slug: "note", Fields: children})}}}})
	old := store.List(store.String("old"), store.String("old"))
	row := func(key, text string) store.Value {
		return store.Object(store.Values{"_key": store.String(key), "points": store.List(store.String(text)), "label": store.String(key)})
	}
	document, err := app.Local().Create(t.Context(), "products", store.Values{"title": store.String("persisted"), "points": old, "translated": old, "details": store.Object(store.Values{"points": old}), "variants": store.List(row("A", "first"), row("B", "second")), "blocks": store.List(store.Object(store.Values{"_key": store.String("C"), "blockType": store.String("card"), "points": old}))}, core.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, locale, fieldName string
		data                          map[string]any
		value, prior                  []string
		input, present                bool
		tokens                        []string
	}{
		{name: "root retained", path: "points", locale: "en", fieldName: "points", data: map[string]any{"title": "unsaved"}, value: []string{"old", "old"}, prior: []string{"old", "old"}, present: true},
		{name: "whole replacement", path: "points", locale: "en", fieldName: "points", data: map[string]any{"points": []string{"new"}}, value: []string{"new"}, prior: []string{"old", "old"}, input: true, present: true},
		{name: "explicit empty replacement", path: "points", locale: "en", fieldName: "points", data: map[string]any{"points": []string{}}, value: []string{}, prior: []string{"old", "old"}, input: true, present: true},
		{name: "explicit null", path: "points", locale: "en", fieldName: "points", data: map[string]any{"points": nil}, prior: []string{"old", "old"}, input: true},
		{name: "group retained", path: "details.points", locale: "en", fieldName: "points", data: map[string]any{"details": map[string]any{"label": "new"}}, value: []string{"old", "old"}, prior: []string{"old", "old"}, present: true},
		{name: "array reorder", path: "variants.1.points", locale: "en", fieldName: "points", data: map[string]any{"variants": []any{map[string]any{"_key": "B"}, map[string]any{"_key": "A", "label": "edited"}}}, value: []string{"first"}, prior: []string{"first"}, present: true, tokens: []string{"A"}},
		{name: "new array row", path: "variants.0.points", locale: "en", fieldName: "points", data: map[string]any{"variants": []any{map[string]any{"_key": "new", "points": []string{"new"}}}}, value: []string{"new"}, input: true, present: true, tokens: []string{"new"}},
		{name: "Block replacement", path: "blocks.0.points", locale: "en", fieldName: "points", data: map[string]any{"blocks": []any{map[string]any{"_key": "C", "blockType": "note", "points": []string{"new"}}}}, value: []string{"new"}, input: true, present: true, tokens: []string{"C", "note"}},
		{name: "missing exact locale", path: "translated", locale: "fr", fieldName: "translated", data: map[string]any{}, present: false},
		{name: "new translation", path: "translated", locale: "fr", fieldName: "translated", data: map[string]any{"translated": []string{"French", "French"}}, value: []string{"French", "French"}, input: true, present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen = operation.LiveValidationContext{}
			current = nil
			available = false
			evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate?locale="+test.locale, map[string]any{"id": document.ID, "data": test.data, "fields": []string{test.path}}))
			if !reflect.DeepEqual(current, test.value) || available != test.present {
				t.Fatalf("current=%#v present=%v", current, available)
			}
			if _, ok := seen.Input.Lookup(test.fieldName); ok != test.input {
				t.Fatalf("submitted presence=%v", ok)
			}
			priorValues, present := seen.Prior.Get(test.fieldName).CopyList()
			var prior []string
			if present {
				prior = make([]string, len(priorValues))
				for i, value := range priorValues {
					prior[i], _ = value.StringValue()
				}
			}
			if !reflect.DeepEqual(prior, test.prior) {
				t.Fatalf("prior=%#v want=%#v", prior, test.prior)
			}
			if seen.Operation != operation.Update || seen.ID != operation.ID(document.ID) || seen.CollectionID != "products" || seen.Locale != schema.LocaleCode(test.locale) || seen.Actor.ID != "live-editor" {
				t.Fatalf("context=%#v", seen)
			}
			issue := evaluations[0].Issues[0]
			if issue.Path != test.path || issue.FieldID == "" || issue.Locale != schema.LocaleCode(test.locale) {
				t.Fatalf("issue=%#v", issue)
			}
			var tokens []string
			if err := json.Unmarshal([]byte(issue.Target), &tokens); err != nil {
				t.Fatal(err)
			}
			for _, token := range test.tokens {
				if !slices.Contains(tokens, token) {
					t.Fatalf("target=%v missing=%s", tokens, token)
				}
			}
		})
	}
	response := liveRequest(t, app, "collections/products/validate", map[string]any{"id": document.ID, "data": map[string]any{}, "fields": []string{"points.0"}})
	if response.Code != 400 {
		t.Fatalf("primitive item became occurrence: %d %s", response.Code, response.Body.String())
	}
}

func TestLivePrimitiveListEmbeddedContextAndReader(t *testing.T) {
	var seen operation.LiveValidationContext
	var current []float64
	var supplierID string
	reads := 0
	sizes := field.NumberList("sizes").LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[[]float64]) ([]operation.Issue, error) {
		seen = ctx
		current, _ = value.Get()
		supplier, err := ctx.Local.FindByID(context.Background(), "suppliers", operation.ID(supplierID))
		if err != nil {
			return nil, err
		}
		if _, ok := supplier.Values["secret"]; ok {
			t.Fatal("reader exposed denied data")
		}
		if _, ok := supplier.Values["translated"]; ok {
			t.Fatal("reader used English fallback")
		}
		return []operation.Issue{{Code: "review_sizes", Message: "Review size choices"}}, nil
	})
	card := field.Block{Slug: "card", Fields: field.Fields{field.Text("label"), sizes, field.TextList("points")}}
	config := core.Config{Name: "Embedded live lists", Plugins: []core.Plugin{richtext.New()}, Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}}, Collections: []core.Collection{
		{Slug: "suppliers", Fields: field.Fields{field.Text("name"), field.TextList("translated").Localized(), field.TextList("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }})}, Access: core.CollectionAccess{Read: func(ctx core.AccessContext) (core.AccessDecision, error) {
			reads++
			if ctx.Actor == nil || ctx.Actor.ID != "live-editor" || ctx.ActorCollection != "live-users" || ctx.Locale != "fr" {
				t.Errorf("reader lost actor/locale: %#v", ctx)
			}
			return core.Allow(), nil
		}}, Hooks: core.CollectionHooks{BeforeRead: []core.Hook{func(core.HookContext) error { t.Fatal("advisory reader executed lifecycle"); return nil }}}},
		{Slug: "products", Fields: field.Fields{field.Text("title"), richtext.Field("body", richtext.Config{Blocks: []field.Block{card}}), richtext.Field("localizedBody", richtext.Config{Blocks: []field.Block{card}}).Localized()}},
	}}
	app, _ := newLiveApp(t, config)
	supplier, err := app.Local().Create(t.Context(), "suppliers", store.Values{"name": store.String("Supplier"), "translated": store.List(store.String("English")), "secret": store.List(store.String("private"))}, core.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	supplierID = supplier.ID
	payload := store.Values{"sizes": store.List(store.Number(0), store.Number(8), store.Number(8)), "points": store.List(store.String("oak"))}
	body := richtextblocks.Document(richtextblocks.Block("card", "E", payload))
	document, err := app.Local().Create(t.Context(), "products", store.Values{"title": store.String("saved"), "body": body, "localizedBody": body}, core.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"body", "localizedBody"} {
		t.Run(owner, func(t *testing.T) {
			evaluations := liveChecked(t, liveRequest(t, app, "collections/products/validate?locale=fr", map[string]any{"id": document.ID, "data": map[string]any{"title": "unsaved", owner: body}, "fields": []string{"sizes"}, "embedded": []any{map[string]any{"field": owner, "treeKey": "blocks", "caseTag": "block", "variantSlug": "card", "identity": "E", "data": map[string]any{"_key": "E", "blockType": "card", "sizes": []float64{9, 9}, "label": "draft"}}}}))
			if !reflect.DeepEqual(current, []float64{9, 9}) {
				t.Fatalf("typed sizes=%v", current)
			}
			prior, _ := seen.Prior.Get("sizes").CopyList()
			if len(prior) != 3 {
				t.Fatalf("prior=%#v", prior)
			}
			if title, _ := seen.Root.String("title"); title != "unsaved" {
				t.Fatalf("Root=%q", title)
			}
			if !reflect.DeepEqual(seen.Root.Get(owner), body) {
				t.Fatal("detached draft changed parent Root")
			}
			if _, ok := seen.Input.Lookup("sizes"); !ok {
				t.Fatal("submitted list missing from Input")
			}
			issue := evaluations[0].Issues[0]
			if issue.Path != "sizes" || issue.Locale != "fr" || issue.FieldID == "" {
				t.Fatalf("issue=%#v", issue)
			}
		})
	}
	if reads != 2 {
		t.Fatalf("bound reader calls=%d", reads)
	}
}

func TestLivePrimitiveListRelativeItemTargetRejected(t *testing.T) {
	app, _ := newLiveApp(t, core.Config{Name: "No primitive item occurrences", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.TextList("points").LiveValidate(func(operation.LiveValidationContext, operation.Value[[]string]) ([]operation.Issue, error) {
		return []operation.Issue{{Code: "invalid", Message: "Wrong target", Target: operation.At().Row("0")}}, nil
	})}}}})
	response := liveRequest(t, app, "collections/products/validate", map[string]any{"data": map[string]any{"points": []string{"one", "one"}}, "fields": []string{"points"}})
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "checked") {
		t.Fatalf("item target=%d %s", response.Code, response.Body.String())
	}
}
