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

func TestOpenAPIOptionalEmailMatchesHTTPOutput(t *testing.T) {
	app, err := core.New(core.Config{Name: "Optional contact", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
		field.Email("contact"), field.Blocks("layout", field.Block{Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Email("contact")}}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	fields := app.Manifest().Snapshot().Collections[0].Fields
	contactSchema, err := openAPIFieldSchema(fields[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	layoutSchema, err := openAPIFieldSchema(fields[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		value   store.Value
		present bool
	}{
		{"absent", store.Null(), false}, {"null", store.Null(), true},
		{"empty", store.String(""), true}, {"address", store.String("hello@example.com"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := store.Values{"blockType": store.String("cta")}
			values := store.Values{}
			if test.present {
				row["contact"], values["contact"] = test.value, test.value
			}
			values["layout"] = store.List(store.Object(row))
			doc, err := app.Local().Create(context.Background(), "pages", values, nil)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("GET", "/api/collections/pages/"+doc.ID, nil))
			if response.Code != 200 {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			var envelope struct{ Doc map[string]any }
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if err := resolvedOutputSchema(t, layoutSchema).Validate(envelope.Doc["layout"]); err != nil {
				t.Fatal(err)
			}
			child := envelope.Doc["layout"].([]any)[0].(map[string]any)
			for _, actual := range []map[string]any{envelope.Doc, child} {
				value, present := actual["contact"]
				if present != test.present {
					t.Fatalf("contact presence = %v, want %v", present, test.present)
				}
				if present {
					if err := validateFormattedOutput(t, contactSchema, value); err != nil {
						t.Fatalf("valid HTTP contact rejected: %v", err)
					}
				}
			}
		})
	}
	if err := validateFormattedOutput(t, contactSchema, "not-an-email"); err == nil {
		t.Fatal("malformed address accepted")
	}
	required := fields[0]
	required.Required = true
	requiredSchema, err := openAPIFieldSchema(required, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFormattedOutput(t, requiredSchema, ""); err == nil {
		t.Fatal("required empty email accepted")
	}
}
