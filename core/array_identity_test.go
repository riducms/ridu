package core_test

import (
	"context"
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestArrayIdentityLifecycle(t *testing.T) {
	ctx := context.Background()
	keys := func(value store.Value) []string {
		t.Helper()
		var result []string
		seen := map[string]bool{}
		for _, row := range blockRows(value) {
			key := blockKey(row)
			if key == "" || seen[key] {
				t.Fatalf("missing or duplicate array identity %q", key)
			}
			seen[key] = true
			result = append(result, key)
		}
		return result
	}
	var beforeValidateKeys []string
	app, err := ridu.New(ridu.Config{
		Name: "Array identities",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug:   "pages",
			Fields: field.Fields{field.Group("content", field.Fields{field.Array("items", field.Fields{field.Text("heading").Localized(), field.Array("links", field.Fields{field.Text("label")})})}), field.Array("translations", field.Fields{field.Text("label")}).Localized()},
			Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(h ridu.HookContext) error {
				if content, ok := h.Data["content"].CopyObject(); ok {
					beforeValidateKeys = keys(content["items"])
					for _, row := range blockRows(content["items"]) {
						keys(row["links"])
					}
				}
				return nil
			}}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(ctx, "pages", store.Values{
		"content": store.Object(store.Values{"items": store.List(
			store.Object(store.Values{"heading": store.String("First"), "links": store.List(store.Object(store.Values{"label": store.String("Nested")}))}),
			store.Object(store.Values{"_key": store.String("supplied"), "heading": store.String("Second")}),
		)}),
		"translations": store.List(store.Object(store.Values{"label": store.String("English")})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := created.Values["content"].CopyObject()
	originalKeys := keys(content["items"])
	if len(originalKeys) != 2 || originalKeys[1] != "supplied" || !reflect.DeepEqual(beforeValidateKeys, originalKeys) {
		t.Fatalf("created keys %v, hook keys %v", originalKeys, beforeValidateKeys)
	}
	rows := blockRows(content["items"])
	nestedKeys := keys(rows[0]["links"])
	englishKeys := keys(created.Values["translations"])
	rows[0]["heading"] = store.String("Premier")
	rows[1]["heading"] = store.String("Deuxième")
	_, err = app.Local().Update(ctx, "pages", created.ID, store.Values{
		"content":      store.Object(store.Values{"items": store.List(store.Object(rows[1]), store.Object(rows[0]), store.Object(store.Values{"heading": store.String("Nouveau")}))}),
		"translations": store.List(store.Object(store.Values{"label": store.String("Français")})),
	}, ridu.MutationOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	english, err := app.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ = english.Values["content"].CopyObject()
	updatedKeys := keys(content["items"])
	rows = blockRows(content["items"])
	heading, _ := rows[1]["heading"].StringValue()
	if len(updatedKeys) != 3 || updatedKeys[0] != originalKeys[1] || updatedKeys[1] != originalKeys[0] || heading != "First" {
		t.Fatalf("reorder lost identity or locale: keys %v, heading %q", updatedKeys, heading)
	}
	if !reflect.DeepEqual(keys(rows[1]["links"]), nestedKeys) || !reflect.DeepEqual(keys(english.Values["translations"]), englishKeys) {
		t.Fatal("update replaced unchanged nested or localized identities")
	}
	duplicate, err := app.Local().Duplicate(ctx, "pages", created.ID, nil, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	content, _ = duplicate.Values["content"].CopyObject()
	for index, key := range keys(content["items"]) {
		if key == updatedKeys[index] {
			t.Fatal("duplicate reused array identity")
		}
	}
	if keys(blockRows(content["items"])[1]["links"])[0] == nestedKeys[0] || keys(duplicate.Values["translations"])[0] == englishKeys[0] {
		t.Fatal("duplicate reused nested or localized array identity")
	}
	french, err := app.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	frenchCopy, err := app.Local().Find(ctx, "pages", duplicate.ID, ridu.FindOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if keys(frenchCopy.Values["translations"])[0] == keys(french.Values["translations"])[0] {
		t.Fatal("duplicate reused identity in another locale")
	}
}
