package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/riducms/ridu/adapters/sqlite"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Restoring while viewing a fallback locale must restore the canonical snapshot,
// not turn the displayed fallback into a translation. Exercise the admin's HTTP
// restore path and prove that subsequent source-language edits still fall back.
func TestBlockRestorePreservesAbsentTranslations(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			ctx := context.Background()
			var backend store.Store = teststore.New()
			if adapter == "sqlite" {
				db, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "restore.sqlite")})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				backend = db
			}
			app, err := ridu.New(ridu.Config{Name: "Restore fallback", Localization: ridu.LocalizationConfig{
				DefaultLocale: "en", Locales: []ridu.Locale{
					{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
				},
			}, Collections: []ridu.Collection{{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Localized().Required(), field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading"), field.Text("translation").Localized().Required(), field.Group("settings", field.Fields{field.Text("caption").Localized().Default("Default")}), field.Array("links", field.Fields{field.Text("label").Localized().Required()})}})}}}}, backend)
			if err != nil {
				t.Fatal(err)
			}
			if db, ok := backend.(*sqlite.Store); ok {
				if err := db.Migrate(ctx, app.Manifest()); err != nil {
					t.Fatal(err)
				}
			}
			created, err := app.Local().Create(ctx, "pages", store.Values{
				"title": store.String("Original title"),
				"layout": store.List(store.Object(store.Values{
					"blockType": store.String("hero"), "heading": store.String("Before"), "translation": store.String("English original"),
					"settings": store.Object(store.Values{"caption": store.String("Original caption")}),
					"links":    store.List(store.Object(store.Values{"label": store.String("Original link")})),
				})),
			}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			draft := true
			readAll := func() store.Document {
				t.Helper()
				doc, err := app.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{AllLocales: true, Draft: &draft})
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}
			original := readAll()
			row := blockRows(created.Values["layout"])[0]
			row["heading"] = store.String("After")
			updated, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(row))}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest("POST", fmt.Sprintf("/api/collections/pages/%s/restore/%d?locale=fr&draft=true", created.ID, created.Revision), nil)
			request.Header.Set("If-Match", fmt.Sprint(updated.Revision))
			app.Handler(ridu.HandlerOptions{}).ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("restore: %d %s", response.Code, response.Body.String())
			}
			var envelope struct{ Doc map[string]any }
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Doc["title"] != "Original title" || envelope.Doc["layout"].([]any)[0].(map[string]any)["translation"] != "English original" {
				t.Fatalf("restore response lost French fallback: %s", response.Body.String())
			}
			restored := readAll()
			if !reflect.DeepEqual(restored.Values, original.Values) {
				t.Fatalf("restore changed snapshot locale presence or row identities: got %#v want %#v", restored.Values, original.Values)
			}
			row["translation"] = store.String("English updated after restore")
			if _, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": store.List(store.Object(row))}, ridu.MutationOptions{}); err != nil {
				t.Fatal(err)
			}
			french, err := app.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{Locale: "fr", Draft: &draft})
			if err != nil {
				t.Fatal(err)
			}
			if value, _ := blockRows(french.Values["layout"])[0]["translation"].StringValue(); value != "English updated after restore" {
				t.Fatalf("French stopped following English fallback: %q", value)
			}
		})
	}
}
