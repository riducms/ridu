package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestOpenAPIValidatesRedactedAndProjectedHTTPResponses(t *testing.T) {
	deny := field.Access{Read: func(operation.Context) (bool, error) {
		return false, nil
	}}
	app, err := core.New(core.Config{Name: "Output contracts", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Text("title").Required(), field.Text("secret").Required().Access(deny), field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Required(), field.Text("secret").Required().Access(deny), field.Group("settings", field.Fields{field.Text("secret").Required().Access(deny)}), field.Array("links", field.Fields{field.Text("secret").Required().Access(deny)})}}).Required()}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := app.Local().Create(context.Background(), "pages", store.Values{"title": store.String("Page"), "secret": store.String("top secret"), "layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Visible"), "secret": store.String("block secret"), "settings": store.Object(store.Values{"secret": store.String("group secret")}), "links": store.List(store.Object(store.Values{"_key": store.String("link"), "secret": store.String("array secret")}))}))}, core.MutationOptions{})
	if err != nil {
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
	for _, suffix := range []string{"", `?select=%7B%22title%22%3Atrue%7D`} {
		response := httptest.NewRecorder()
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("GET", fmt.Sprintf("/api/collections/pages/%s%s", doc.ID, suffix), nil))
		if response.Code != 200 {
			t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Doc map[string]any `json:"doc"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Doc == nil {
			t.Fatalf("missing doc: %s", response.Body.String())
		}
		if _, exists := envelope.Doc["secret"]; exists {
			t.Fatal("root secret disclosed")
		}
		if suffix == "" {
			row := envelope.Doc["layout"].([]any)[0].(map[string]any)
			if _, exists := row["secret"]; exists {
				t.Fatal("block secret disclosed")
			}
			if len(row["settings"].(map[string]any)) != 0 {
				t.Fatal("group secret disclosed")
			}
			if _, exists := row["links"].([]any)[0].(map[string]any)["secret"]; exists {
				t.Fatal("array secret disclosed")
			}
		} else if _, exists := envelope.Doc["layout"]; exists {
			t.Fatal("selection did not omit layout")
		}
		if err := validator.Validate(envelope.Doc); err != nil {
			t.Fatalf("valid API response rejected: %v\n%s", err, response.Body.String())
		}
	}
}

func resolvedOutputSchema(t *testing.T, value any) *jsonschema.Resolved {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var contract jsonschema.Schema
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	result, err := contract.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOpenAPIDatesMatchAcceptedHTTPValues(t *testing.T) {
	for _, test := range []struct {
		name       string
		appearance field.DateFormat
		values     []string
		format     string
	}{
		{"day", field.DateOnly, []string{"2026-09-04"}, "date"},
		{"timestamp", field.DateTime, []string{"2026-09-04T14:30:00Z", "2026-09-04T14:30:00+01:00"}, "date-time"},
		{"time", field.TimeOnly, []string{"14:30", "14:30:05", "14:30:05.123", "4:30"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, err := core.New(core.Config{Name: "Dates", Collections: []core.Collection{{Slug: "events", Fields: field.Fields{field.Date("when").Required().Format(test.appearance)}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			property, err := openAPIFieldSchema(app.Manifest().Snapshot().Collections[0].Fields[0], nil)
			if err != nil {
				t.Fatal(err)
			}
			format, _ := property["format"].(string)
			if format != test.format {
				t.Fatalf("format %q, want %q", format, test.format)
			}
			validator := resolvedOutputSchema(t, property)
			for _, value := range test.values {
				doc, err := app.Local().Create(context.Background(), "events", store.Values{"when": store.String(value)}, core.MutationOptions{})
				if err != nil {
					t.Fatalf("runtime rejected %q: %v", value, err)
				}
				response := httptest.NewRecorder()
				app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("GET", "/api/collections/events/"+doc.ID, nil))
				if response.Code != 200 {
					t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
				}
				var envelope struct {
					Doc map[string]any `json:"doc"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				actual := envelope.Doc["when"]
				if err := validator.Validate(actual); err != nil {
					t.Fatalf("schema rejected %q: %v", actual, err)
				}
				// The JSON Schema validator intentionally treats format as annotation. Assert
				// its standard date/date-time semantics too, against the actual HTTP value.
				if format != "" {
					layout := time.DateOnly
					if format == "date-time" {
						layout = time.RFC3339
					}
					if _, err := time.Parse(layout, actual.(string)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if test.appearance == field.TimeOnly {
				for _, value := range []string{"24:30", "14:60", "14:30Z", "2026-09-04"} {
					if err := validator.Validate(value); err == nil {
						t.Fatalf("time schema accepted %q", value)
					}
				}
			}
		})
	}
}

// OpenAPI stores its reusable JSON Schemas under components; move them to $defs
// for a pure JSON Schema validator, preserving every generated reference.
func resolvedResourceSchema(t *testing.T, document openAPIDocument, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := json.Marshal(map[string]any{"$ref": "#/components/schemas/" + name, "$defs": document.Components.Schemas})
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(data), "#/components/schemas/", "#/$defs/")), &value); err != nil {
		t.Fatal(err)
	}
	return resolvedOutputSchema(t, value)
}
