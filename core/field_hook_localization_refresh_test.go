package core_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestLocalizedFieldHooksRefreshCurrentViewsAfterClear(t *testing.T) {
	for _, kind := range []string{"json", "plugin", "group"} {
		for _, phase := range []string{"write", "read_all_locales"} {
			t.Run(kind+"/"+phase, func(t *testing.T) {
				active := false
				var order []string
				type retainedView struct {
					context operation.Context
					step    int
				}
				var retained []retainedView
				initial := func(locale schema.LocaleCode) store.Value {
					value := store.String("value-" + string(locale))
					if kind == "plugin" {
						return value
					}
					return store.Object(store.Values{"child": value})
				}
				assertViews := func(ctx operation.Context, step int) {
					t.Helper()
					if ctx.AllLocales || (ctx.Locale != "en" && ctx.Locale != "fr") {
						t.Fatalf("callback locale=%q, allLocales=%v", ctx.Locale, ctx.AllLocales)
					}
					want := initial(ctx.Locale)
					if step == 2 {
						want = store.Null()
					}
					if value, present := ctx.Root.Lookup("value"); !present || !reflect.DeepEqual(value, want) {
						t.Fatalf("%s step %d root=%v present=%v, want %v", ctx.Locale, step, value, present, want)
					}
					// Exact-locale read projection omits a cleared JSON/plugin/group
					// sibling, while Root preserves the explicit null and typed input
					// represents it as Empty. Ordinary write siblings retain the null.
					wantPresent := phase == "write" || step != 2
					if value, present := ctx.Siblings.Lookup("value"); present != wantPresent || present && !reflect.DeepEqual(value, want) {
						t.Fatalf("%s step %d sibling=%v present=%v, want %v present=%v", ctx.Locale, step, value, present, want, wantPresent)
					}
					for _, view := range []operation.View{ctx.Root, ctx.Siblings} {
						if marker, _ := view.String("marker"); marker != "marker-"+string(ctx.Locale) {
							t.Fatalf("%s step %d marker=%q", ctx.Locale, step, marker)
						}
					}
				}
				observe := func(ctx operation.Context, input operation.Value[store.Value], step int) (operation.Change[store.Value], error) {
					if !active {
						return operation.Keep[store.Value](), nil
					}
					order = append(order, fmt.Sprintf("%s%d", ctx.Locale, step))
					assertViews(ctx, step)
					if value, present := input.Get(); present != (step != 2) || present && !reflect.DeepEqual(value, initial(ctx.Locale)) {
						t.Fatalf("%s step %d input=%v present=%v", ctx.Locale, step, value, present)
					}
					retained = append(retained, retainedView{context: ctx, step: step})
					if step == 1 {
						return operation.Replace(operation.Empty[store.Value]()), nil
					}
					return operation.Keep[store.Value](), nil
				}
				var writes field.Hooks[store.Value]
				var reads field.ReadHooks[store.Value]
				// Clear after an initial Keep, then observe within the same phase.
				for step := range 3 {
					if phase == "write" {
						writes.BeforeChange = append(writes.BeforeChange, func(ctx operation.WriteContext, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
							return observe(operation.Context(ctx), input, step)
						})
					} else {
						reads.AfterRead = append(reads.AfterRead, func(ctx operation.ReadContext, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
							return observe(operation.Context(ctx), input, step)
						})
					}
				}
				var definition field.Node
				var plugins []ridu.Plugin
				switch kind {
				case "json":
					definition = field.JSON("value").Localized().Hooks(writes).ReadHooks(reads)
				case "plugin":
					definition = field.Plugin("value", "color", json.RawMessage(`{}`)).Localized().Hooks(writes).ReadHooks(reads)
					plugins = []ridu.Plugin{multiFieldPlugin{key: "shapes", fields: []string{"color"}}}
				case "group":
					definition = field.Group("value", field.Fields{field.Text("child")}).Localized().Hooks(writes).ReadHooks(reads)
				}
				app, err := ridu.New(ridu.Config{
					Name: "Localized hook views", Plugins: plugins,
					Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
						{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
					}},
					Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{definition, field.Text("marker").Localized()}}},
				}, teststore.New())
				if err != nil {
					t.Fatal(err)
				}
				created, err := app.Local().Create(t.Context(), "pages", store.Values{"value": initial("en"), "marker": store.String("marker-en")}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"value": initial("fr"), "marker": store.String("marker-fr")}, nil, ridu.LocaleOptions{Locale: "fr"}); err != nil {
					t.Fatal(err)
				}
				active = true
				if phase == "write" {
					// Public writes select one locale; all-locales updates are rejected.
					for _, locale := range []schema.LocaleCode{"en", "fr"} {
						if _, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{}, nil, ridu.LocaleOptions{Locale: locale}); err != nil {
							t.Fatal(err)
						}
					}
				} else if _, err := app.Local().Find(t.Context(), "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true}); err != nil {
					t.Fatal(err)
				}
				if want := []string{"en0", "en1", "en2", "fr0", "fr1", "fr2"}; !reflect.DeepEqual(order, want) {
					t.Fatalf("hook order=%v, want %v", order, want)
				}
				for _, snapshot := range retained {
					assertViews(snapshot.context, snapshot.step)
				}
				active = false
				stored, err := app.Local().Find(t.Context(), "pages", created.ID, nil, ridu.LocaleOptions{AllLocales: true})
				if err != nil {
					t.Fatal(err)
				}
				translations, valid := stored.Values["value"].CopyObject()
				if !valid {
					t.Fatalf("stored translations=%v", stored.Values["value"])
				}
				for _, locale := range []schema.LocaleCode{"en", "fr"} {
					want := initial(locale)
					if phase == "write" {
						want = store.Null()
					}
					if !reflect.DeepEqual(translations[string(locale)], want) {
						t.Fatalf("stored %s=%v, want %v", locale, translations[string(locale)], want)
					}
				}
			})
		}
	}
}
