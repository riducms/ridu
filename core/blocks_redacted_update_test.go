package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/adapters/sqlite"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestRedactedBlockUpdatesPreserveStoredChildren(t *testing.T) {
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
				calls := 0
				secret := func() field.TextField {
					f := field.Text("secret").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }, Update: func(operation.Context) (bool, error) { calls++; return false, nil }})
					if required {
						return f.Required()
					}
					return f.Default("configured default")
				}
				app, err := ridu.New(ridu.Config{Name: "Redacted block updates", Collections: []ridu.Collection{{
					Slug:   "pages",
					Fields: field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading"), secret(), field.Group("settings", field.Fields{field.Text("label"), secret()}), field.Array("links", field.Fields{field.Text("label"), secret()})}})},
					Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
						if ctx.Operation == operation.Update {
							rows, _ := ctx.Data["layout"].CopyList()
							row, _ := rows[0].CopyObject()
							// The second validation pass must preserve omissions too.
							delete(row, "secret")
							rows[0] = store.Object(row)
							ctx.Data["layout"] = store.List(rows...)
						}
						return nil
					}}},
				}}}, backend)
				if err != nil {
					t.Fatal(err)
				}
				if db, ok := backend.(*sqlite.Store); ok {
					if err := db.Migrate(ctx, app.Manifest()); err != nil {
						t.Fatal(err)
					}
				}
				row := func(heading, value string) store.Value {
					return store.Object(store.Values{
						"blockType": store.String("hero"), "heading": store.String(heading), "secret": store.String(value),
						"settings": store.Object(store.Values{"label": store.String("Settings"), "secret": store.String(value)}),
						"links":    store.List(store.Object(store.Values{"label": store.String("Link"), "secret": store.String(value)})),
					})
				}
				created, err := app.Local().Create(ctx, "pages", store.Values{"layout": store.List(row("A", "stored A"), row("B", "stored B"))}, ridu.MutationOptions{})
				if err != nil {
					t.Fatal(err)
				}
				handler := app.Handler(ridu.HandlerOptions{})
				read := httptest.NewRecorder()
				handler.ServeHTTP(read, httptest.NewRequest("GET", "/api/collections/pages/"+created.ID, nil))
				if read.Code != 200 {
					t.Fatalf("GET %d: %s", read.Code, read.Body.String())
				}
				var envelope struct{ Doc map[string]any }
				if err := json.Unmarshal(read.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				rows := envelope.Doc["layout"].([]any)
				first := rows[0].(map[string]any)
				for _, object := range []map[string]any{first, first["settings"].(map[string]any), first["links"].([]any)[0].(map[string]any)} {
					if _, exists := object["secret"]; exists {
						t.Fatal("response disclosed secret")
					}
				}
				first["heading"] = "Edited A"
				first["settings"].(map[string]any)["label"] = "Edited settings"
				first["links"].([]any)[0].(map[string]any)["label"] = "Edited link"
				patch := func(rows []any) *httptest.ResponseRecorder {
					t.Helper()
					body, err := json.Marshal(map[string]any{"layout": rows})
					if err != nil {
						t.Fatal(err)
					}
					request := httptest.NewRequest("PATCH", "/api/collections/pages/"+created.ID, bytes.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					return response
				}
				response := patch([]any{rows[1], rows[0]})
				if response.Code != 200 {
					t.Fatalf("sibling edit: HTTP %d: %s", response.Code, response.Body.String())
				}
				if calls != 0 {
					t.Fatalf("unchanged secrets triggered %d access checks", calls)
				}
				collection := app.Manifest().Snapshot().Collections[0]
				assertStored := func() {
					t.Helper()
					tx, err := backend.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					stored, err := tx.Find(ctx, store.Request{Collection: collection, ID: created.ID})
					_ = tx.Rollback(ctx)
					if err != nil {
						t.Fatal(err)
					}
					actual := blockRows(stored.Values["layout"])
					if len(actual) != 2 {
						t.Fatalf("stored row count = %d", len(actual))
					}
					if stringValue(actual[1]["heading"]) != "Edited A" {
						t.Fatal("allowed edit was not stored")
					}
					for i, expected := range []string{"stored B", "stored A"} {
						settings, _ := actual[i]["settings"].CopyObject()
						link := blockRows(actual[i]["links"])[0]
						if stringValue(actual[i]["secret"]) != expected || stringValue(settings["secret"]) != expected || stringValue(link["secret"]) != expected {
							t.Fatalf("omitted secrets changed on row %d", i)
						}
					}
				}
				assertStored()
				// An explicit mutation must still be authorized and must leave storage intact.
				first["secret"] = "unauthorized"
				response = patch([]any{rows[1], rows[0]})
				if response.Code != 403 || calls == 0 {
					t.Fatalf("secret mutation: HTTP %d, calls %d: %s", response.Code, calls, response.Body.String())
				}
				assertStored()
				delete(first, "secret")
				calls = 0
				response = patch([]any{rows[1], rows[0], map[string]any{"blockType": "hero", "heading": "New row"}})
				if required {
					if response.Code != 422 {
						t.Fatalf("new row missing required secret: HTTP %d", response.Code)
					}
				} else {
					if response.Code != 200 || calls != 0 {
						t.Fatalf("trusted default admission: HTTP %d, calls %d", response.Code, calls)
					}
					tx, err := backend.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					stored, err := tx.Find(ctx, store.Request{Collection: collection, ID: created.ID})
					_ = tx.Rollback(ctx)
					if err != nil {
						t.Fatal(err)
					}
					actual := blockRows(stored.Values["layout"])
					if len(actual) != 3 || stringValue(actual[2]["secret"]) != "configured default" {
						t.Fatalf("new row default missing: %#v", actual)
					}
				}
				if required {
					assertStored()
				}
			})
		}
	}
}
