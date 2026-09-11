package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/mail"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestOpenAPIOptionalDatesValidateHTTPEmptyValues(t *testing.T) {
	for _, appearance := range []field.DateFormat{field.DateOnly, field.DateTime, field.TimeOnly} {
		t.Run(string(appearance), func(t *testing.T) {
			fields := field.Fields{field.Date("when").Format(appearance), field.Blocks("layout", field.Block{Slug: "event", Fields: field.Fields{field.Date("when").Format(appearance)}})}
			app, err := core.New(core.Config{Name: "Optional dates", Collections: []core.Collection{{Slug: "events", Fields: fields}}}, teststore.New())
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
			property, err := openAPIFieldSchema(app.Manifest().Snapshot().Collections[0].Fields[0], nil)
			if err != nil {
				t.Fatal(err)
			}
			valid := "2026-09-04"
			invalid := "2026-99-04"
			switch appearance {
			case field.DateTime:
				valid = "2026-09-04T14:30:00Z"
				invalid = "2026-09-04T25:30:00Z"
			case field.TimeOnly:
				valid = "14:30"
				invalid = "25:30"
			}
			// Negative controls prove that format annotations are enforced, including
			// through the optional anyOf branches, rather than merely ignored.
			if err := validateFormattedOutput(t, property, invalid); err == nil {
				t.Fatalf("invalid date accepted: %q", invalid)
			}
			for _, test := range []struct {
				name    string
				value   store.Value
				present bool
			}{
				{"absent", store.Null(), false}, {"null", store.Null(), true}, {"empty", store.String(""), true}, {"valid", store.String(valid), true},
			} {
				t.Run(test.name, func(t *testing.T) {
					row := store.Values{"blockType": store.String("event")}
					values := store.Values{}
					if test.present {
						values["when"] = test.value
						row["when"] = test.value
					}
					values["layout"] = store.List(store.Object(row))
					doc, err := app.Local().Create(context.Background(), "events", values, core.MutationOptions{})
					if err != nil {
						t.Fatal(err)
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
					if err := validator.Validate(envelope.Doc); err != nil {
						t.Fatalf("schema rejected actual HTTP response: %v\n%s", err, response.Body.String())
					}
					child := envelope.Doc["layout"].([]any)[0].(map[string]any)
					for _, actual := range []map[string]any{envelope.Doc, child} {
						value, exists := actual["when"]
						if exists != test.present {
							t.Fatalf("date presence = %v, want %v: %s", exists, test.present, response.Body.String())
						}
						if exists {
							var expected any
							if text, ok := test.value.StringValue(); ok {
								expected = text
							}
							if value != expected {
								t.Fatalf("date value = %#v, want %#v", value, expected)
							}
							if err := validateFormattedOutput(t, property, value); err != nil {
								t.Fatalf("format assertion rejected actual HTTP value %#v: %v", value, err)
							}
						}
					}
				})
			}
			required := app.Manifest().Snapshot().Collections[0].Fields[0]
			required.Required = true
			requiredSchema, err := openAPIFieldSchema(required, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateFormattedOutput(t, requiredSchema, ""); err == nil {
				t.Fatal("required date schema accepted empty string")
			}
			requiredApp, err := core.New(core.Config{Name: "Required dates", Collections: []core.Collection{{Slug: "events", Fields: field.Fields{field.Date("when").Required().Format(appearance)}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := requiredApp.Local().Create(context.Background(), "events", store.Values{"when": store.String("")}, core.MutationOptions{}); err == nil {
				t.Fatal("runtime accepted required empty date")
			}
		})
	}
}

func TestDateAdminReplacementPreservesRuntimeAndGeneratedValueFormat(t *testing.T) {
	for _, test := range []struct {
		format  field.DateFormat
		valid   string
		invalid string
	}{
		{field.DateOnly, "2026-09-07", "2026-09-07T10:30:00Z"},
		{field.DateTime, "2026-09-07T10:30:00Z", "2026-09-07"},
		{field.TimeOnly, "10:30", "2026-09-07"},
	} {
		t.Run(string(test.format), func(t *testing.T) {
			factory := field.Date("when").Required().Format(test.format)
			refined := factory.Admin(field.Admin{Description: "Presentation only"})
			app, err := core.New(core.Config{Name: "Dates", Collections: []core.Collection{{Slug: "events", Fields: field.Fields{refined}}}}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			resolved := app.Manifest().Snapshot().Collections[0].Fields[0]
			if resolved.Date == nil || string(resolved.Date.Format) != string(test.format) {
				t.Fatalf("resolved date contract changed: %#v", resolved.Date)
			}
			property, err := openAPIFieldSchema(resolved, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateFormattedOutput(t, property, test.valid); err != nil {
				t.Fatal(err)
			}
			if err := validateFormattedOutput(t, property, test.invalid); err == nil {
				t.Fatalf("generated contract accepted incompatible date %q", test.invalid)
			}
			if _, err := app.Local().Create(context.Background(), "events", store.Values{"when": store.String(test.valid)}, core.MutationOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := app.Local().Create(context.Background(), "events", store.Values{"when": store.String(test.invalid)}, core.MutationOptions{}); err == nil {
				t.Fatalf("runtime accepted incompatible date %q", test.invalid)
			}
		})
	}
}

// google/jsonschema-go treats format as annotation. Supplement its validation
// with format assertions for each candidate branch, so anyOf's explicit
// empty alternative succeeds while malformed nonempty values still fail.
func validateFormattedOutput(t *testing.T, property map[string]any, value any) error {
	t.Helper()
	if err := resolvedOutputSchema(t, property).Validate(value); err != nil {
		return err
	}
	if alternatives, ok := property["anyOf"].([]any); ok {
		for _, alternative := range alternatives {
			if err := validateFormattedOutput(t, alternative.(map[string]any), value); err == nil {
				return nil
			}
		}
		return fmt.Errorf("no schema alternative accepts %#v", value)
	}
	if text, ok := value.(string); ok {
		switch property["format"] {
		case "email":
			address, err := mail.ParseAddress(text)
			if err != nil || address.Address != text {
				return fmt.Errorf("invalid email %q", text)
			}
		case "date":
			_, err := time.Parse(time.DateOnly, text)
			return err
		case "date-time":
			_, err := time.Parse(time.RFC3339, text)
			return err
		}
	}
	return nil
}
