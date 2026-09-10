package core_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func blockRows(value store.Value) []store.Values {
	rows, _ := value.CopyList()
	out := make([]store.Values, len(rows))
	for i, row := range rows {
		out[i], _ = row.CopyObject()
	}
	return out
}
func blockKey(row store.Values) string { key, _ := row["_key"].StringValue(); return key }
func blockIdentityFields() field.Fields {
	return field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Required().Localized(), field.Blocks("children", field.Block{Slug: "text", Fields: field.Fields{field.Text("body")}})}}, field.Block{Slug: "quote", Fields: field.Fields{field.Text("quote")}})}
}
func TestBlocksIdentityLifecycle(t *testing.T) {
	ctx := context.Background()
	calls := 0
	hook := func(h ridu.HookContext) error {
		if h.Operation == operation.Create {
			for _, row := range blockRows(h.Data["layout"]) {
				if blockKey(row) == "" {
					t.Fatal("before-validation hook received missing key")
				}
			}
			calls++
		}
		return nil
	}
	app, err := ridu.New(ridu.Config{Name: "Blocks identity", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: blockIdentityFields(), Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{hook}}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	values := store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("First"), "children": store.List(store.Object(store.Values{"blockType": store.String("text"), "body": store.String("Nested")}))}), store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Second")}))}
	created, err := app.Local().Create(ctx, "pages", values, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := blockRows(created.Values["layout"])
	first, second := blockKey(rows[0]), blockKey(rows[1])
	if first == "" || second == "" || first == second || calls != 1 {
		t.Fatalf("keys %q %q calls %d", first, second, calls)
	}
	nested := blockKey(blockRows(rows[0]["children"])[0])
	if nested == "" {
		t.Fatal("nested key missing")
	}
	rows[0]["heading"] = store.String("Premier")
	rows[1]["heading"] = store.String("Deuxième")
	updated, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(rows[1]), store.Object(rows[0]))}, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	english, err := app.Local().Find(ctx, "pages", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	englishRows := blockRows(english.Values["layout"])
	heading, _ := englishRows[0]["heading"].StringValue()
	if blockKey(englishRows[0]) != second || blockKey(englishRows[1]) != first || heading != "Second" {
		t.Fatalf("reorder lost locale identity %#v", englishRows)
	}
	duplicate, err := app.Local().Duplicate(ctx, "pages", created.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	copied := blockRows(duplicate.Values["layout"])
	if blockKey(copied[0]) == second || blockKey(copied[1]) == first || blockKey(blockRows(copied[1]["children"])[0]) == nested {
		t.Fatal("duplicate reused identity")
	}
	for _, test := range []struct {
		name       string
		key        store.Value
		code, path string
	}{
		{"empty", store.String(" "), "invalid_row_key", "layout.0._key"}, {"number", store.Number(3), "invalid_row_key", "layout.0._key"}, {"null", store.Null(), "invalid_row_key", "layout.0._key"}, {"duplicate", store.String(second), "duplicate_row_key", "layout.1._key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := blockRows(updated.Values["layout"])
			bad[0]["_key"] = test.key
			bad[1]["_key"] = store.String(second)
			_, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(bad[0]), store.Object(bad[1]))}, nil)
			var issue *ridu.OperationError
			if !errors.As(err, &issue) || len(issue.Issues) == 0 || issue.Issues[0].Code != test.code || issue.Issues[0].Path != test.path {
				t.Fatalf("error %v", err)
			}
		})
	}
	replacement := store.Values{"_key": store.String(second), "blockType": store.String("quote"), "quote": store.String("Converted")}
	_, err = app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(replacement))}, nil)
	var issue *ridu.OperationError
	if !errors.As(err, &issue) || issue.Issues[0].Code != "block_type_identity" {
		t.Fatalf("same-key type replacement = %v", err)
	}
	delete(replacement, "_key")
	if _, err = app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(replacement))}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestBlockRevisionRestorePreservesCreatedIdentities(t *testing.T) {
	ctx := context.Background()
	app, err := ridu.New(ridu.Config{Name: "Block revisions", Collections: []ridu.Collection{{
		Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(ctx, "pages", store.Values{"layout": store.List(
		store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("First")}),
		store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Second")}),
	)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := blockRows(created.Values["layout"])
	rows[0]["heading"] = store.String("Edited")
	updated, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(rows[1]), store.Object(rows[0]))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := app.Local().RestoreAsDraft(ctx, "pages", created.ID, created.Revision, updated.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Values["layout"], created.Values["layout"]) {
		t.Fatal("restoring a revision changed its identities or content")
	}
}

func TestBlockIdentityReorderAndProtectedRemoval(t *testing.T) {
	ctx := context.Background()
	var runtimePaths []string
	app, err := ridu.New(ridu.Config{
		Name: "Protected blocks",
		Collections: []ridu.Collection{{
			Slug: "pages",
			Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("secret").Required().Access(field.Access{Update: func(c operation.AccessContext,

			) (bool, error) {
				runtimePaths = append(runtimePaths, string(c.OccurrenceID))
				return false, nil
			}})}}, field.Block{Slug: "quote", Fields: field.Fields{field.Text("quote")}})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	created, err := app.Local().Create(ctx, "pages", store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "secret": store.String("One")}), store.Object(store.Values{"blockType": store.String("hero"), "secret": store.String("Two")}))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := blockRows(created.Values["layout"])
	// Reordering submits only identity and membership; no protected value is authored.
	for _, row := range rows {
		delete(row, "secret")
	}

	_, err = app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(rows[1]), store.Object(rows[0]))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimePaths) != 0 {
		t.Fatalf("reorder treated unchanged values as edits %v", runtimePaths)
	}
	for _, values := range []store.Values{{"layout": store.List(store.Object(rows[1]))}, {"layout": store.List(store.Object(store.Values{"blockType": store.String("quote"), "quote": store.String("Replacement")}))}} {
		_, err = app.Local().Update(ctx, "pages", created.ID, values, nil)
		var issue *ridu.OperationError
		if !errors.As(err, &issue) || issue.Code != "field_access_denied" {
			t.Fatalf("protected removal allowed: %v", err)
		}
	}
	if len(runtimePaths) == 0 {
		t.Fatal("deleted block child access was not checked")
	}
}

func TestCopyLocalePreservesExactDistinctBlockKeys(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "Exact block keys", Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: blockIdentityFields()}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := app.Local().Create(ctx, "pages", store.Values{"layout": store.List(store.Object(store.Values{"_key": store.String("x"), "blockType": store.String("hero"), "heading": store.String("One")}), store.Object(store.Values{"_key": store.String(" x "), "blockType": store.String("hero"), "heading": store.String("Two")}))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Local().CopyLocale(ctx, "pages", created.ID, "en", "fr", created.Revision, nil); err != nil {
		t.Fatal(err)
	}
}
