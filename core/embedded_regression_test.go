package core_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/adapters/sqlite"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func embeddedSQLiteApp(t *testing.T, config ridu.Config) *ridu.App {
	t.Helper()
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "embedded.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(t.Context(), app.Manifest()); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestEmbeddedWholeFieldLocalesHaveIndependentIdentities(t *testing.T) {
	config := embeddedConfig()
	config.Localization.Locales[1].FallbackLocales = []schema.LocaleCode{"en"}
	config.Collections[1].Fields = field.Fields{field.Plugin("body", outline.Key, json.RawMessage(`{}`)).Localized().EmbeddedTrees(field.EmbeddedTree{
		Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind",
		Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Identity: "uid", Discriminator: "schema", Types: []field.Block{field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}, field.Block{Slug: "note", Fields: field.Fields{field.Text("title")}}}}},
	})}
	app := embeddedSQLiteApp(t, config)
	values := func(kind, title string) store.Values {
		return store.Values{"body": outline.Value(outline.Widget(kind, "same-key", store.Values{"title": store.String(title)}))}
	}
	english, err := app.Local().Create(t.Context(), "pages", values("card", "English"), ridu.MutationOptions{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := app.Local().Find(t.Context(), "pages", english.ID, ridu.FindOptions{Locale: "fr"})
	if err != nil || embeddedString(embeddedPayload(t, fallback.Values["body"], 0), "schema") != "card" {
		t.Fatalf("fallback: %v", err)
	}
	if _, err := app.Local().Update(t.Context(), "pages", english.ID, values("note", "French"), ridu.MutationOptions{Locale: "fr"}); err != nil {
		t.Fatalf("first French occurrence was correlated with English fallback: %v", err)
	}
	_, err = app.Local().Update(t.Context(), "pages", english.ID, values("card", "Invalid replacement"), ridu.MutationOptions{Locale: "fr"})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || len(failure.Issues) != 1 || failure.Issues[0].Code != "block_type_identity" || failure.Issues[0].Path != "body.outline.0.content.schema" {
		t.Fatalf("retained French identity must reject a variant change: %#v, %v", failure, err)
	}
	for _, locale := range []string{"en", "fr"} {
		doc, err := app.Local().Find(t.Context(), "pages", english.ID, ridu.FindOptions{Locale: schema.LocaleCode(locale)})
		if err != nil {
			t.Fatal(err)
		}
		want := "English"
		if locale == "fr" {
			want = "French"
		}
		if got := embeddedString(embeddedPayload(t, doc.Values["body"], 0), "title"); got != want {
			t.Fatalf("%s after rollback: %q", locale, got)
		}
	}
}

func TestEmbeddedPayloadMetadataIsDeclaredByItsContainer(t *testing.T) {
	app := embeddedSQLiteApp(t, embeddedConfig())
	for _, name := range []string{"_key", "blockType"} {
		t.Run(name, func(t *testing.T) {
			payload := store.Values{"title": store.String("Valid title"), name: store.Object(store.Values{"undeclared": store.String("content")})}
			_, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", payload))}, ridu.MutationOptions{})
			var failure *ridu.OperationError
			if !errors.As(err, &failure) || len(failure.Issues) != 1 || failure.Issues[0].Code != "unknown_field" || failure.Issues[0].Path != "body.outline.0.content."+name {
				t.Fatalf("undeclared metadata: %#v, %v", failure, err)
			}
		})
	}
	payload := store.Values{"title": store.String("Valid"), "links": store.List(store.Object(store.Values{"_key": store.String("row"), "label": store.String("Link")})), "ordinary": store.Object(store.Values{"_key": store.String("opaque"), "blockType": store.String("opaque")})}
	if _, err := app.Local().Create(t.Context(), "pages", store.Values{"body": outline.Value(outline.Widget("card", "one", payload))}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("ordinary row metadata/JSON must still work: %v", err)
	}
}
