package core_test

import (
	"encoding/json"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
)

func TestFieldGraphSubmittedInputAdmission(t *testing.T) {
	for _, phase := range []string{"beforeValidate", "beforeChange"} {
		t.Run(phase, func(t *testing.T) {
			var derive, erase bool
			calls := 0
			transform := func(ctx ridu.HookContext) error {
				if ctx.Operation != operation.Update {
					return nil
				}
				if derive {
					ctx.Data["protected"] = store.String("server derived")
				}
				if erase {
					delete(ctx.Data, "protected")
				}
				return nil
			}
			hooks := ridu.CollectionHooks{}
			if phase == "beforeValidate" {
				hooks.BeforeValidate = []ridu.Hook{transform}
			} else {
				hooks.BeforeChange = []ridu.Hook{transform}
			}
			app, err := ridu.New(ridu.Config{Name: "Graph admission", Collections: []ridu.Collection{{
				Slug: "pages", Fields: field.Fields{
					field.Text("public"),
					field.Text("protected").Access(field.Access{Update: func(operation.Context) (bool, error) { calls++; return false, nil }}).
						Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(_ operation.Context, raw operation.Value[store.Value]) (operation.Change[store.Value], error) {
							if value, present := raw.Get(); present {
								if text, ok := value.StringValue(); ok {
									return operation.Replace(operation.Present(store.String(strings.TrimSpace(text)))), nil
								}
							}
							return operation.Keep[store.Value](), nil
						}}}),
				}, Hooks: hooks,
			}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			created, err := app.Local().Create(t.Context(), "pages", store.Values{"public": store.String("initial"), "protected": store.String("existing")}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				name  string
				value store.Value
				erase bool
			}{
				{"changed", store.String("changed"), false},
				{"same", store.String("existing"), false},
				{"explicit null", store.Null(), false},
				{"normalized to same", store.String(" existing "), false},
				{"erased by resource hook", store.String("forbidden"), true},
			} {
				t.Run(test.name, func(t *testing.T) {
					erase = test.erase
					_, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"protected": test.value}, ridu.MutationOptions{})
					if !fieldAccessIssue(err, "protected") {
						t.Fatalf("explicit submission admitted: %v", err)
					}
				})
			}
			erase = false
			calls = 0
			updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"public": store.String("omission")}, ridu.MutationOptions{})
			if err != nil || stringValue(updated.Values["protected"]) != "existing" || calls != 0 {
				t.Fatalf("omitted value: document=%#v calls=%d error=%v", updated.Values, calls, err)
			}
			derive = true
			updated, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"public": store.String("derive")}, ridu.MutationOptions{})
			if err != nil || stringValue(updated.Values["protected"]) != "server derived" || calls != 0 {
				t.Fatalf("trusted derivation treated as caller input: document=%#v calls=%d error=%v", updated.Values, calls, err)
			}
		})
	}
}

func TestFieldGraphFinalCandidateAdmission(t *testing.T) {
	var observed []string
	app, err := ridu.New(ridu.Config{Name: "Final graph admission", Collections: []ridu.Collection{{
		Slug: "pages", Fields: field.Fields{field.Text("mode"), field.Text("protected").Access(field.Access{Update: func(ctx operation.Context) (bool, error) {
			mode, _ := ctx.Root.String("mode")
			observed = append(observed, mode)
			return mode != "locked", nil
		}})}, Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
			if ctx.Operation == operation.Update {
				ctx.Data["mode"] = store.String("locked")
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"mode": store.String("open"), "protected": store.String("one")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{"mode": store.String("open"), "protected": store.String("two")}, ridu.MutationOptions{})
	if !fieldAccessIssue(err, "protected") {
		t.Fatalf("final candidate access was bypassed: %v, observed=%v", err, observed)
	}
	if len(observed) != 2 || observed[0] != "open" || observed[1] != "locked" {
		t.Fatalf("admission checkpoints=%v", observed)
	}
	current, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{})
	if err != nil || stringValue(current.Values["protected"]) != "one" || stringValue(current.Values["mode"]) != "open" {
		t.Fatalf("denied final candidate reached storage: %#v %v", current.Values, err)
	}
}

func TestFieldGraphCreateDefaultIsTrustedButExplicitDefaultRequiresAccess(t *testing.T) {
	calls := 0
	app, err := ridu.New(ridu.Config{Name: "Create graph provenance", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("public"), field.Text("protected").Default("server default").Access(field.Access{Create: func(operation.Context) (bool, error) {
			calls++
			return false, nil
		}}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"public": store.String("only submitted input")}, ridu.MutationOptions{})
	if err != nil || stringValue(created.Values["protected"]) != "server default" || calls != 0 {
		t.Fatalf("default provenance: values=%#v calls=%d error=%v", created.Values, calls, err)
	}
	for _, value := range []store.Value{store.String("server default"), store.Null()} {
		if _, err := app.Local().Create(t.Context(), "pages", store.Values{"protected": value}, ridu.MutationOptions{}); !fieldAccessIssue(err, "protected") {
			t.Fatalf("explicit protected default/null admitted: %v", err)
		}
	}
}

func TestFieldGraphStructuredAdmissionUsesCallerMembership(t *testing.T) {
	allow := true
	secret := field.Text("secret").Access(field.Access{
		Create: func(operation.Context) (bool, error) { return allow, nil },
		Update: func(operation.Context) (bool, error) { return allow, nil },
	})
	children := field.Fields{secret, field.Text("public")}
	app, err := ridu.New(ridu.Config{Name: "Structured graph admission", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Group("meta", children), field.Array("rows", children), field.Blocks("content", field.Block{Slug: "card", Fields: children}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	row := func(key string, secret bool) store.Value {
		values := store.Values{"_key": store.String(key), "public": store.String("public")}
		if secret {
			values["secret"] = store.String(key)
		}
		return store.Object(values)
	}
	block := func(key string, secret bool) store.Value {
		values, _ := row(key, secret).CopyObject()
		values["blockType"] = store.String("card")
		return store.Object(values)
	}
	initial := store.Values{
		"meta":    store.Object(store.Values{"secret": store.String("meta"), "public": store.String("public")}),
		"rows":    store.List(row("A", true), row("B", true)),
		"content": store.List(block("A", true), block("B", true)),
	}
	created, err := app.Local().Create(t.Context(), "pages", initial, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	allow = false
	for _, test := range []struct {
		name, path string
		patch      store.Values
	}{
		{"group same", "meta.secret", store.Values{"meta": store.Object(store.Values{"secret": store.String("meta")})}},
		{"group clear", "meta.secret", store.Values{"meta": store.Null()}},
		{"array same reordered", "rows.0.secret", store.Values{"rows": store.List(row("B", true), row("A", false))}},
		{"array removal", "rows.0.secret", store.Values{"rows": store.List(row("B", false))}},
		{"block same reordered", "content.0.secret", store.Values{"content": store.List(block("B", true), block("A", false))}},
		{"block removal", "content.0.secret", store.Values{"content": store.List(block("B", false))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := app.Local().Update(t.Context(), "pages", created.ID, test.patch, ridu.MutationOptions{}); !fieldAccessIssue(err, test.path) {
				t.Fatalf("protected occurrence admitted: %v", err)
			}
		})
	}
	updated, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{
		"meta":    store.Object(store.Values{"public": store.String("updated")}),
		"rows":    store.List(row("B", false), row("A", false)),
		"content": store.List(block("B", false), block("A", false)),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("reordered omitted secrets denied: %v", err)
	}
	for _, name := range []string{"rows", "content"} {
		rows, _ := updated.Values[name].CopyList()
		for i, key := range []string{"B", "A"} {
			values, _ := rows[i].CopyObject()
			if stringValue(values["secret"]) != key {
				t.Fatalf("%s reorder lost prior identity: %#v", name, updated.Values)
			}
		}
	}
	for name, value := range initial {
		t.Run("create "+name, func(t *testing.T) {
			if _, err := app.Local().Create(t.Context(), "pages", store.Values{name: value}, ridu.MutationOptions{}); !operationCode(err, "field_access_denied") {
				t.Fatalf("protected create admitted: %v", err)
			}
		})
	}
}

func TestFieldGraphRichTextSubmittedAccess(t *testing.T) {
	allow := true
	secret := field.Text("secret").Access(field.Access{
		Create: func(operation.Context) (bool, error) { return allow, nil },
		Update: func(operation.Context) (bool, error) { return allow, nil },
	})
	body := richtext.Field("body", richtext.Config{Blocks: []field.Block{{Slug: "card", Fields: field.Fields{secret, field.Text("public")}}}})
	app, err := ridu.New(ridu.Config{Name: "Embedded graph admission", Plugins: []ridu.Plugin{richtext.New()}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{body}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document := func(nodes string) store.Value {
		var value store.Value
		if err := json.Unmarshal([]byte(`{"version":1,"root":{"type":"root","version":1,"format":"","indent":0,"direction":null,"children":[`+nodes+`]}}`), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	initial := document(`{"type":"block","version":1,"fields":{"blockType":"card","_key":"A","secret":"protected","public":"initial"}}`)
	created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": initial}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	allow = false
	if _, err := app.Local().Create(t.Context(), "pages", store.Values{"body": initial}, ridu.MutationOptions{}); !fieldAccessIssue(err, "body.root.children.0.fields.secret") {
		t.Fatalf("embedded create admitted: %v", err)
	}
	for _, test := range []struct{ name, nodes string }{
		{"same", `{"type":"block","version":1,"fields":{"blockType":"card","_key":"A","secret":"protected"}}`},
		{"null", `{"type":"block","version":1,"fields":{"blockType":"card","_key":"A","secret":null}}`},
		{"removed", ``},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": document(test.nodes)}, ridu.MutationOptions{}); !fieldAccessIssue(err, "body.root.children.0.fields.secret") {
				t.Fatalf("embedded update admitted: %v", err)
			}
		})
	}
	if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": document(`{"type":"block","version":1,"fields":{"blockType":"card","_key":"A","public":"updated"}}`)}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("embedded omission denied: %v", err)
	}
}

func TestFieldGraphLocalizedAdmissionRetainsExactProvenance(t *testing.T) {
	denyFrench := false
	var locales []string
	access := func(ctx operation.Context) (bool, error) {
		locales = append(locales, string(ctx.Locale))
		return !denyFrench || ctx.Locale != "fr", nil
	}
	app, err := ridu.New(ridu.Config{Name: "Localized graph admission", Localization: ridu.LocalizationConfig{
		DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
	}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("public"), field.Text("protected").Localized().Access(field.Access{Update: access}),
		field.Array("rows", field.Fields{field.Text("public"), field.Text("secret").Localized().Access(field.Access{Update: access})}),
		field.Array("translated", field.Fields{field.Text("public"), field.Text("secret").Access(field.Access{Update: access})}).Localized(),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	row := func(key, value string) store.Value {
		data := store.Values{"_key": store.String(key), "public": store.String("public")}
		if value != "" {
			data["secret"] = store.String(value)
		}
		return store.Object(data)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{
		"protected":  store.String("English"),
		"rows":       store.List(row("A", "English A"), row("B", "English B")),
		"translated": store.List(row("English", "English")),
	}, ridu.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Update(t.Context(), "pages", created.ID, store.Values{
		"rows":       store.List(row("A", "French A"), row("B", "French B")),
		"translated": store.List(row("French", "French")),
	}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	denyFrench, locales = true, nil
	if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"public": store.String("omitted")}, ridu.MutationOptions{Locale: "fr"}); err != nil || len(locales) != 0 {
		t.Fatalf("fallback became caller input: locales=%v error=%v", locales, err)
	}
	for _, value := range []store.Value{store.String("English"), store.Null()} {
		if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"protected": value}, ridu.MutationOptions{Locale: "fr"}); !operationCode(err, "field_access_denied") {
			t.Fatalf("exact French submission admitted: %v", err)
		}
	}
	// Shared row membership belongs to every retained translation. Removing A
	// in English also removes its French secret and must admit that occurrence.
	if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"rows": store.List(row("B", ""))}, ridu.MutationOptions{Locale: "en"}); !fieldAccessIssue(err, "rows.0.secret.fr") {
		t.Fatalf("shared removal bypassed protected French descendant: %v", err)
	}
	// A localized container's membership is exact-locale input. Changing its
	// English structure does not submit or remove the unrelated French rows.
	if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"translated": store.List()}, ridu.MutationOptions{Locale: "en"}); err != nil {
		t.Fatalf("English localized membership treated as French write: %v", err)
	}
	current, err := app.Local().Find(t.Context(), "pages", created.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	localized, _ := current.Values["translated"].CopyObject()
	frenchRows, _ := localized["fr"].CopyList()
	if len(frenchRows) != 1 {
		t.Fatalf("French localized membership lost: %#v", current.Values)
	}
	protected, _ := current.Values["protected"].CopyObject()
	if _, exists := protected["fr"]; exists {
		t.Fatal("fallback English value was persisted in French")
	}
}

func TestFieldGraphLocalizedAccessAndHookShareOccurrenceIdentity(t *testing.T) {
	for _, containerLocalized := range []bool{false, true} {
		t.Run(map[bool]string{false: "localized child", true: "localized container"}[containerLocalized], func(t *testing.T) {
			var accessIDs []operation.OccurrenceID
			var hookID operation.OccurrenceID
			secret := field.Text("secret").Access(field.Access{Update: func(ctx operation.Context) (bool, error) {
				accessIDs = append(accessIDs, ctx.OccurrenceID)
				return true, nil
			}}).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.Context, _ operation.Value[string]) (operation.Change[string], error) {
				if ctx.Operation == operation.Update {
					hookID = ctx.OccurrenceID
				}
				return operation.Keep[string](), nil
			}}})
			if !containerLocalized {
				secret = secret.Localized()
			}
			rows := field.Array("rows", field.Fields{secret})
			if containerLocalized {
				rows = rows.Localized()
			}
			app, err := ridu.New(ridu.Config{Name: "Locale callback identity", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{rows}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			data := func(value string) store.Values {
				return store.Values{"rows": store.List(store.Object(store.Values{"_key": store.String("A"), "secret": store.String(value)}))}
			}
			created, err := app.Local().Create(t.Context(), "pages", data("English"), ridu.MutationOptions{Locale: "en"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.Local().Update(t.Context(), "pages", created.ID, data("French"), ridu.MutationOptions{Locale: "fr"}); err != nil {
				t.Fatal(err)
			}
			if len(accessIDs) != 2 || hookID == "" || accessIDs[0] != hookID || accessIDs[1] != hookID {
				t.Fatalf("locale occurrence changed between admission and hook: accesses=%v hook=%s", accessIDs, hookID)
			}
		})
	}
}
