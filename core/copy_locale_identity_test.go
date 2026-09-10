package core_test

import (
	"encoding/json"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func TestCopyLocaleReplacesIndependentVariantsWithoutBypassingTargetAccess(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		for _, denied := range []bool{false, true} {
			name := "blocks"
			if embedded {
				name = "outline"
			}
			if denied {
				name += "-target-denied"
			}
			t.Run(name, func(t *testing.T) {
				variants := []field.Block{field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}, field.Block{Slug: "note", Fields: field.Fields{field.Text("title").Access(field.Access{Update: func(operation.AccessContext) (bool, error) { return !denied, nil }})}}}
				var definition field.Node = field.Blocks("body", variants...).Localized()
				value := func(kind, title string) store.Value {
					return store.List(store.Object(store.Values{"_key": store.String("same"), "blockType": store.String(kind), "title": store.String(title)}))
				}
				payload := func(value store.Value) store.Values { return blockRows(value)[0] }
				config := ridu.Config{Name: "Locale identity", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}}
				if embedded {
					config.Plugins = embeddedConfig().Plugins
					definition = field.Plugin("body", outline.Key, json.RawMessage(`{}`)).Localized().EmbeddedTrees(field.EmbeddedTree{Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Identity: "uid", Discriminator: "schema", Types: variants}}})
					value = func(kind, title string) store.Value {
						return outline.Value(outline.Widget(kind, "same", store.Values{"title": store.String(title)}))
					}
					payload = func(value store.Value) store.Values { return embeddedPayload(t, value, 0) }
				}
				copying := false
				hookCalls := 0
				config.Collections = []ridu.Collection{{Slug: "pages", Fields: field.Fields{definition}, Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
					if copying {
						hookCalls++
						if ctx.Original == nil || embeddedString(payload(ctx.Original.Values["body"]), "title") != "French" {
							t.Fatal("copy hook Original did not retain target locale")
						}
					}
					return nil
				}}}}}
				app, err := ridu.New(config, teststore.New())
				if err != nil {
					t.Fatal(err)
				}
				created, err := app.Local().Create(t.Context(), "pages", store.Values{"body": value("card", "English")}, nil, ridu.LocaleOptions{Locale: "en"})
				if err != nil {
					t.Fatal(err)
				}
				// Populate the independent target while its update access is allowed.
				savedDenied := denied
				denied = false
				target, err := app.Local().Update(t.Context(), "pages", created.ID, store.Values{"body": value("note", "French")}, nil, ridu.LocaleOptions{Locale: "fr"})
				denied = savedDenied
				if err != nil {
					t.Fatal(err)
				}
				copying = true
				copied, err := app.Local().CopyLocale(t.Context(), "pages", created.ID, "en", "fr", target.Revision, nil)
				if denied {
					var failure *ridu.OperationError
					if !errors.As(err, &failure) || failure.Code != "field_access_denied" {
						t.Fatalf("target removal access: %#v, %v", failure, err)
					}
					actual, findErr := app.Local().Find(t.Context(), "pages", created.ID, nil, ridu.LocaleOptions{Locale: "fr"})
					if findErr != nil {
						t.Fatal(findErr)
					}
					if actual.Revision != target.Revision || embeddedString(payload(actual.Values["body"]), "title") != "French" {
						t.Fatal("denied copy changed target")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				keyName := "_key"
				if embedded {
					keyName = "uid"
				}
				if got := payload(copied.Values["body"]); embeddedString(got, "title") != "English" || embeddedString(got, keyName) != "same" || hookCalls != 1 {
					t.Fatalf("copied payload %#v hooks %d", got, hookCalls)
				}
			})
		}
	}
}
