package generate

import (
	"maps"

	"github.com/riducms/ridu/schema"
)

func addLiveValidationSchemas(schemas map[string]openAPISchema) {
	closed := false
	schemas["LiveValidationRequest"] = openAPISchema{
		Type: "object", AdditionalProperties: &closed,
		Description: "Advisory unsaved input. Only explicitly opted-in fields run; this never replaces save validation. Maximum request body: 1 MiB.",
		Properties: map[string]any{
			"id":       map[string]any{"type": "string", "maxLength": 512},
			"data":     map[string]any{"type": "object", "description": "Raw partial document values; omitted update values are retained from exact-locale storage. Malformed typed values are skipped."},
			"fields":   map[string]any{"type": "array", "minItems": 1, "maxItems": 64, "uniqueItems": true, "items": map[string]any{"type": "string"}, "description": "Current snapshot field paths, relative to the innermost embedded payload when supplied."},
			"embedded": map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"$ref": "#/components/schemas/LiveValidationEmbeddedScope"}},
		}, Required: []string{"data", "fields"},
	}
	globalRequest := schemas["LiveValidationRequest"]
	globalRequest.Properties = maps.Clone(globalRequest.Properties)
	delete(globalRequest.Properties, "id")
	schemas["GlobalLiveValidationRequest"] = globalRequest
	scope := map[string]any{"data": map[string]any{"type": "object", "description": "Detached unsaved payload, including its declared stable identity and case discriminator."}}
	for _, key := range []string{"field", "treeKey", "caseTag", "variantSlug", "identity"} {
		scope[key] = map[string]any{"type": "string"}
	}
	schemas["LiveValidationEmbeddedScope"] = openAPISchema{Type: "object", AdditionalProperties: &closed, Properties: scope, Required: []string{"field", "treeKey", "caseTag", "variantSlug", "identity", "data"}}
	schemas["LiveValidationEvaluation"] = openAPISchema{Type: "object", Properties: map[string]any{
		"path": map[string]any{"type": "string"}, "target": map[string]any{"type": "string", "description": "Opaque stable correlation token; applications never construct it."},
		"status": map[string]any{"type": "string", "enum": []string{"checked", "skipped"}},
		"issues": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/ValidationIssue"}},
	}, Required: []string{"path", "status", "issues"}}
	schemas["LiveValidationEnvelope"] = openAPISchema{Type: "object", Properties: map[string]any{
		"evaluations": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/LiveValidationEvaluation"}},
	}, Required: []string{"evaluations"}}
}

func liveValidationOperation(name string, localization *schema.LocalizationSettings, global bool) map[string]any {
	spec := operationSpec("Check opted-in fields in unsaved input", "liveValidate"+name, "200")
	spec["description"] = "Read-only advisory evaluation with normal resource and field authorization. No defaults, mutation hooks, writes, jobs, or fallback translations run. A successful check is neither authorization nor a reservation. Operational failure returns an error, never successful validation."
	spec["requestBody"] = openAPIJSONRequest("LiveValidationRequest")
	if global {
		spec["requestBody"] = openAPIJSONRequest("GlobalLiveValidationRequest")
	}
	spec["responses"].(map[string]any)["200"] = map[string]any{"description": "Completed checks and explicitly skipped unavailable or malformed values", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/LiveValidationEnvelope"}}}}
	if localization != nil {
		locales := make([]string, 0, len(localization.Locales))
		for _, locale := range localization.Locales {
			locales = append(locales, string(locale.Code))
		}
		spec["parameters"] = []map[string]any{{"name": "locale", "in": "query", "required": false, "description": "One exact locale. All-locales and fallback requests are rejected.", "schema": map[string]any{"type": "string", "enum": locales, "default": string(localization.DefaultLocale)}}}
	}
	return spec
}
