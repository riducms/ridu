package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
)

func TestLiveValidationGenerationIsCapabilityOnly(t *testing.T) {
	calls := 0
	check := func(operation.LiveValidationContext, operation.Value[string]) ([]operation.Issue, error) {
		calls++
		return nil, nil
	}
	manifest, err := core.Resolve(core.Config{Name: "Live validation", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.Text("sku").Required().LiveValidate(check)}}}, Globals: []core.Global{{Slug: "settings", Fields: field.Fields{field.Text("title").LiveValidate(check)}}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if !snapshot.Collections[0].Fields[0].LiveValidation || !snapshot.Globals[0].Fields[0].LiveValidation {
		t.Fatal("missing capability flag")
	}
	bytes, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/collections/products/validate", "/api/globals/settings/validate"} {
		spec, ok := document.Paths[path]["post"].(map[string]any)
		if !ok {
			t.Fatalf("missing operation %s", path)
		}
		encoded, _ := json.Marshal(spec)
		if strings.Contains(string(encoded), `"all"`) || strings.Contains(string(encoded), `"fallback-locale"`) {
			t.Fatalf("live transport exposes nonexact locale: %s", encoded)
		}
	}
	for _, name := range []string{"LiveValidationRequest", "LiveValidationEnvelope", "LiveValidationEmbeddedScope", "LiveValidationEvaluation"} {
		if _, ok := document.Components.Schemas[name]; !ok {
			t.Fatalf("missing schema %s", name)
		}
	}
	globalInput := resolvedResourceSchema(t, document, "GlobalLiveValidationRequest")
	input := map[string]any{"data": map[string]any{}, "fields": []any{"title"}}
	if err := globalInput.Validate(input); err != nil {
		t.Fatal(err)
	}
	input["id"] = "settings"
	if err := globalInput.Validate(input); err == nil {
		t.Fatal("global validation request schema accepts a forged document ID")
	}
	resource, _ := json.Marshal(document.Components.Schemas[string(snapshot.Collections[0].ID)+"Create"])
	if strings.Contains(string(resource), "liveValidation") {
		t.Fatalf("callback metadata leaked into write value: %s", resource)
	}
	for _, generate := range []func(schema.Manifest) ([]byte, error){goClient, typescript.Client, openAPI} {
		first, err := generate(manifest)
		if err != nil {
			t.Fatal(err)
		}
		second, err := generate(manifest)
		if err != nil || string(first) != string(second) {
			t.Fatal("nondeterministic advisory generation")
		}
	}
	if calls != 0 {
		t.Fatal("generation executed a live callback")
	}
}
