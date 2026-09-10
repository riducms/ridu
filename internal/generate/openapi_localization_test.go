package generate

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestOpenAPILocalizedGroupValidatesItsBlockDescendants(t *testing.T) {
	app, err := core.New(core.Config{Name: "Localized section", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Group("section", field.Fields{field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}), field.JSON("metadata")}).Localized()}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := app.Local().Create(context.Background(), "pages", store.Values{"section": store.Object(store.Values{
		"layout":   store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Hello")})),
		"metadata": store.Object(store.Values{"arbitrary": store.String("retained")}),
	})}, nil)
	if err != nil {
		t.Fatal(err)
	}
	property, err := openAPIFieldSchema(app.Manifest().Snapshot().Collections[0].Fields[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	validator := resolvedOutputSchema(t, property)
	// Read access and projection can omit every authored child.
	for _, empty := range []any{map[string]any{}, map[string]any{"en": map[string]any{}}} {
		if err := validator.Validate(empty); err != nil {
			t.Fatalf("redacted group rejected: %v", err)
		}
	}
	for _, locale := range []string{"en", "all"} {
		t.Run(locale, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("GET", "/api/collections/pages/"+doc.ID+"?locale="+locale, nil))
			if response.Code != 200 {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			var envelope struct{ Doc map[string]any }
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			value := envelope.Doc["section"]
			if err := validator.Validate(value); err != nil {
				t.Fatalf("actual HTTP group rejected: %v", err)
			}
			section := value.(map[string]any)
			if locale == "all" {
				section = section["en"].(map[string]any)
			}
			row := section["layout"].([]any)[0].(map[string]any)
			row["heading"] = 42
			if err := validator.Validate(value); err == nil {
				t.Fatal("locale alternative bypassed block child validation")
			}
			row["heading"] = "Hello"
			row["blockType"] = "unknown"
			if err := validator.Validate(value); err == nil {
				t.Fatal("locale alternative bypassed block discriminator validation")
			}
		})
	}
}

func TestOpenAPIValidatesLocalizedBlocksHTTPResponses(t *testing.T) {
	for _, localizedContainer := range []bool{false, true} {
		name := "children"
		if localizedContainer {
			name = "container"
		}
		t.Run(name, func(t *testing.T) {
			heading := field.Text("heading").Required().Localized(!localizedContainer)
			layout := field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{
				heading, field.Date("when").Localized().Format(field.TimeOnly),
				field.Group("settings", field.Fields{field.Array("links", field.Fields{field.Blocks("content", field.Block{Slug: "text", Fields: field.Fields{field.Text("body").Required().Localized(!localizedContainer)}})})}),
			}}).Required().Localized(localizedContainer)
			app, err := core.New(core.Config{Name: "Locale output", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{layout}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			doc, err := app.Local().Create(context.Background(), "pages", store.Values{"layout": store.List(store.Object(store.Values{
				"blockType": store.String("hero"), "heading": store.String("Hello"), "when": store.String(""),
				"settings": store.Object(store.Values{"links": store.List(store.Object(store.Values{"_key": store.String("link"), "content": store.List(store.Object(store.Values{"blockType": store.String("text"), "body": store.String("Nested")}))}))}),
			}))}, nil)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := doc.Values["layout"].CopyList()
			frenchRow, _ := rows[0].CopyObject()
			frenchRow["heading"] = store.String("Bonjour")
			if _, err := app.Local().Update(context.Background(), "pages", doc.ID, store.Values{"layout": store.List(store.Object(frenchRow))}, nil, core.LocaleOptions{Locale: "fr"}); err != nil {
				t.Fatal(err)
			}
			generated, err := openAPI(app.Manifest())
			if err != nil {
				t.Fatal(err)
			}
			var contract openAPIDocument
			if err := json.Unmarshal(generated, &contract); err != nil {
				t.Fatal(err)
			}
			validator := resolvedResourceSchema(t, contract, string(app.Manifest().Snapshot().Collections[0].ID))
			for _, locale := range []string{"en", "fr", "all"} {
				t.Run(locale, func(t *testing.T) {
					response := httptest.NewRecorder()
					app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("GET", "/api/collections/pages/"+doc.ID+"?locale="+locale, nil))
					if response.Code != 200 {
						t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
					}
					var envelope struct {
						Doc map[string]any `json:"doc"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					layout := envelope.Doc["layout"]
					if locale == "all" && localizedContainer {
						layout = layout.(map[string]any)["en"]
					}
					row := layout.([]any)[0].(map[string]any)
					heading := row["heading"]
					if locale == "all" && !localizedContainer {
						heading = heading.(map[string]any)["en"]
					}
					expectedHeading := "Hello"
					if locale == "fr" {
						expectedHeading = "Bonjour"
					}
					if heading != expectedHeading {
						t.Fatalf("unexpected heading: %#v", heading)
					}
					nestedRow := row["settings"].(map[string]any)["links"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
					body := nestedRow["body"]
					when := row["when"]
					if locale == "all" && !localizedContainer {
						body = body.(map[string]any)["en"]
						when = when.(map[string]any)["en"]
					}
					if body != "Nested" || when != "" {
						t.Fatalf("unexpected nested/empty values: body=%#v when=%#v", body, when)
					}
					if err := validator.Validate(envelope.Doc); err != nil {
						t.Fatalf("valid %s response rejected: %v\n%s", locale, err, response.Body.String())
					}
					if locale == "all" {
						var frenchHeading any
						if localizedContainer {
							frenchHeading = envelope.Doc["layout"].(map[string]any)["fr"].([]any)[0].(map[string]any)["heading"]
						} else {
							frenchHeading = row["heading"].(map[string]any)["fr"]
						}
						if frenchHeading != "Bonjour" {
							t.Fatalf("missing French locale: %#v", frenchHeading)
						}
					}
					originalBody := nestedRow["body"]
					nestedRow["body"] = 7
					if err := validator.Validate(envelope.Doc); err == nil {
						t.Fatal("accepted malformed deeply nested block child")
					}
					nestedRow["body"] = originalBody
					if locale == "all" && !localizedContainer {
						row["heading"] = map[string]any{"en": 7}
						if err := validator.Validate(envelope.Doc); err == nil {
							t.Fatal("accepted malformed localized child")
						}
					}
					if locale == "all" && localizedContainer {
						row["blockType"] = "unknown"
						if err := validator.Validate(envelope.Doc); err == nil {
							t.Fatal("accepted unknown localized variant")
						}
					}
				})
			}
		})
	}
}
