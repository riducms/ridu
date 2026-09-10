package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/adapters/sqlite"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestBlockSiblingUpdatePreservesAbsentTranslations(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		for _, required := range []bool{false, true} {
			name := "default"
			if required {
				name = "required"
			}
			t.Run(adapter+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				var backend store.Store = teststore.New()
				if adapter == "sqlite" {
					db, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "blocks.sqlite")})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = db.Close() })
					backend = db
				}
				translation := func() field.TextField {
					if required {
						return field.Text("translation").Localized().Required()
					}
					return field.Text("translation").Localized().Default("configured default")
				}
				app, err := ridu.New(ridu.Config{Name: "Absent block translations",
					Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
						{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
					}},
					Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading"), translation(), field.Group("settings", field.Fields{field.Text("label"), translation()}), field.Array("links", field.Fields{field.Text("label"), translation()})}})}}},
				}, backend)
				if err != nil {
					t.Fatal(err)
				}
				if db, ok := backend.(*sqlite.Store); ok {
					if err := db.Migrate(ctx, app.Manifest()); err != nil {
						t.Fatal(err)
					}
				}
				row := func(name string) store.Value {
					return store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String(name), "translation": store.String("English " + name),
						"settings": store.Object(store.Values{"label": store.String(name), "translation": store.String("English " + name)}),
						"links":    store.List(store.Object(store.Values{"label": store.String(name), "translation": store.String("English " + name)})),
					})
				}
				created, err := app.Local().Create(ctx, "pages", store.Values{"layout": store.List(row("A"), row("B"))}, nil)
				if err != nil {
					t.Fatal(err)
				}
				handler := app.Handler(ridu.HandlerOptions{})
				read := func(locale string) []any {
					t.Helper()
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/collections/pages/"+created.ID+"?locale="+locale, nil))
					if response.Code != 200 {
						t.Fatalf("GET: %d %s", response.Code, response.Body.String())
					}
					var envelope struct{ Doc map[string]any }
					if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					return envelope.Doc["layout"].([]any)
				}
				children := func(row map[string]any) []map[string]any {
					return []map[string]any{row, row["settings"].(map[string]any), row["links"].([]any)[0].(map[string]any)}
				}
				rows := read("fr")
				for _, item := range rows {
					object := item.(map[string]any)
					name := object["heading"].(string)
					for _, child := range children(object) {
						if child["translation"] != "English "+name {
							t.Fatalf("missing initial fallback: %#v", child)
						}
						delete(child, "translation")
					}
					object["heading"] = "Edited " + name
				}
				rows[0], rows[1] = rows[1], rows[0]
				patch := func(rows []any) *httptest.ResponseRecorder {
					t.Helper()
					body, err := json.Marshal(map[string]any{"layout": rows})
					if err != nil {
						t.Fatal(err)
					}
					response := httptest.NewRecorder()
					request := httptest.NewRequest("PATCH", "/api/collections/pages/"+created.ID+"?locale=fr", bytes.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					handler.ServeHTTP(response, request)
					return response
				}
				response := patch(rows)
				if response.Code != 200 {
					t.Fatalf("sibling edit: %d %s", response.Code, response.Body.String())
				}
				for i, item := range read("all") {
					object := item.(map[string]any)
					name := []string{"B", "A"}[i]
					if object["heading"] != "Edited "+name {
						t.Fatal("sibling edit or reorder was lost")
					}
					for _, child := range children(object) {
						locales := child["translation"].(map[string]any)
						if _, exists := locales["fr"]; exists {
							t.Fatalf("materialized omitted French translation: %#v", locales)
						}
						if locales["en"] != "English "+name {
							t.Fatal("English translation changed")
						}
					}
				}
				for i, item := range read("fr") {
					for _, child := range children(item.(map[string]any)) {
						if child["translation"] != "English "+[]string{"B", "A"}[i] {
							t.Fatal("fallback changed after sibling edit")
						}
					}
				}
				// New nested rows cannot inherit the retained parent's omission semantics.
				first := rows[0].(map[string]any)
				links := first["links"].([]any)
				first["links"] = append(links, map[string]any{"label": "New link"})
				response = patch(rows)
				if required {
					if response.Code != 422 || !strings.Contains(response.Body.String(), "layout.0.links.1.translation") {
						t.Fatalf("new required nested child: %d %s", response.Code, response.Body.String())
					}
				} else {
					if response.Code != 200 {
						t.Fatalf("new defaulted nested child: %d %s", response.Code, response.Body.String())
					}
					added := read("all")[0].(map[string]any)["links"].([]any)[1].(map[string]any)
					if added["translation"].(map[string]any)["fr"] != "configured default" {
						t.Fatal("new nested occurrence lost its default")
					}
				}
				first["links"] = links
				// The same missing child on a new occurrence still has create semantics.
				response = patch(append(rows, map[string]any{"blockType": "hero", "heading": "New"}))
				if required {
					if response.Code != 422 || !strings.Contains(response.Body.String(), "layout.2.translation") {
						t.Fatalf("new required child: %d %s", response.Code, response.Body.String())
					}
				} else {
					if response.Code != 200 {
						t.Fatalf("new defaulted child: %d %s", response.Code, response.Body.String())
					}
					added := read("all")[2].(map[string]any)
					if added["translation"].(map[string]any)["fr"] != "configured default" {
						t.Fatal("new occurrence lost its default")
					}
				}
				// Explicit null/empty values are not omissions and still undergo validation.
				for _, value := range []any{nil, ""} {
					first["translation"] = value
					response = patch(rows)
					if required {
						if response.Code != 422 || !strings.Contains(response.Body.String(), "layout.0.translation") {
							t.Fatalf("explicit required value: %d %s", response.Code, response.Body.String())
						}
					} else {
						if response.Code != 200 {
							t.Fatalf("explicit optional value: %d %s", response.Code, response.Body.String())
						}
						locales := read("all")[0].(map[string]any)["translation"].(map[string]any)
						actual, exists := locales["fr"]
						if !exists || actual != value || locales["en"] != "English B" {
							t.Fatalf("explicit null/empty changed: %#v", locales)
						}
					}
				}
				// Supplying the translation is a real write, even on the retained occurrence.
				rows[0].(map[string]any)["translation"] = "French explicit"
				response = patch(rows)
				if response.Code != 200 {
					t.Fatalf("explicit translation: %d %s", response.Code, response.Body.String())
				}
				locales := read("all")[0].(map[string]any)["translation"].(map[string]any)
				if locales["fr"] != "French explicit" || locales["en"] != "English B" {
					t.Fatalf("explicit locale write: %#v", locales)
				}
			})
		}
	}
}
