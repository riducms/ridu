package generate

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
)

func TestOpenAPINamedBlockInputsValidateHTTPCreate(t *testing.T) {
	storageBackend, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required().Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }}).Localized(), field.Text("summary"), field.Text("tone").Required().Default("neutral"), field.Relationship("author", "authors"), field.Upload("image", "assets"), field.Blocks("children", field.Block{Slug: "note", Fields: field.Fields{field.Text("body").Required()}})}}
	app, err := core.New(core.Config{Name: "Block inputs", Storage: storageBackend, StorageNamespace: "blocks-input-contracts", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{
		{Slug: "authors", Fields: field.Fields{field.Text("name")}},
		{Slug: "assets", Upload: true, Fields: field.Fields{field.Text("alt")}},
		{Slug: "pages", Fields: field.Fields{field.Blocks("layout", hero, field.Block{TypeName: "CTA", Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Text("url").Required()}}).Required().MinRows(1).MaxRows(2)}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, method, name string }{{"/api/collections/pages", "post", "pagesCreate"}, {"/api/collections/pages/{id}", "patch", "pagesUpdate"}} {
		body := document.Paths[test.path][test.method].(map[string]any)["requestBody"].(map[string]any)
		ref := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
		if ref != "#/components/schemas/"+test.name {
			t.Fatalf("request not wired: %#v", ref)
		}
	}
	create := resolvedResourceSchema(t, document, "pagesCreate")
	update := resolvedResourceSchema(t, document, "pagesUpdate")
	valid := `{"layout":[{"blockType":"hero","heading":"Hello","summary":null,"children":[{"blockType":"note","body":"Nested"}]},{"blockType":"cta","url":"/start","_key":"supplied"}]}`
	var input any
	if err := json.Unmarshal([]byte(valid), &input); err != nil {
		t.Fatal(err)
	}
	if err := create.Validate(input); err != nil {
		t.Fatal(err)
	}
	if err := update.Validate(map[string]any{}); err != nil {
		t.Fatalf("updates must permit omission: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/collections/pages", strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	var envelope struct{ Doc map[string]any }
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if err := resolvedResourceSchema(t, document, "pages").Validate(envelope.Doc); err != nil {
		t.Fatalf("created output: %v", err)
	}
	rows := envelope.Doc["layout"].([]any)
	first := rows[0].(map[string]any)
	if first["_key"] == nil || first["tone"] != "neutral" || rows[1].(map[string]any)["_key"] != "supplied" {
		t.Fatal("identity/default contract failed")
	}
	if _, present := first["heading"]; present {
		t.Fatal("required heading should be read-redacted")
	}
	child := first["children"].([]any)[0].(map[string]any)
	patch := map[string]any{"layout": []any{map[string]any{"blockType": "hero", "_key": first["_key"], "summary": "Edited", "children": []any{map[string]any{"blockType": "note", "_key": child["_key"]}}}}}
	if err := update.Validate(patch); err != nil {
		t.Fatalf("retained redacted row patch rejected: %v", err)
	}
	data, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest("PATCH", "/api/collections/pages/"+envelope.Doc["id"].(string), strings.NewReader(string(data)))
	request.Header.Set("Content-Type", "application/json")
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("retained patch HTTP %d: %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	patched := envelope.Doc["layout"].([]any)[0].(map[string]any)
	if patched["summary"] != "Edited" || patched["children"].([]any)[0].(map[string]any)["body"] != "Nested" {
		t.Fatal("retained child fields were lost")
	}
	invalidFresh := `{"layout":[{"blockType":"hero","_key":"fresh","summary":"Missing required heading"}]}`
	response = httptest.NewRecorder()
	request = httptest.NewRequest("PATCH", "/api/collections/pages/"+envelope.Doc["id"].(string), strings.NewReader(invalidFresh))
	request.Header.Set("Content-Type", "application/json")
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code == 200 {
		t.Fatal("fresh key must still meet create requiredness at runtime")
	}
	for _, invalidPatch := range []string{`{"layout":[{"blockType":"hero","summary":"keyless patch"}]}`, `{"layout":[{"blockType":"hero","_key":"stable","heading":null}]}`} {
		var value any
		if err := json.Unmarshal([]byte(invalidPatch), &value); err != nil {
			t.Fatal(err)
		}
		if err := update.Validate(value); err == nil {
			t.Fatalf("invalid patch accepted: %s", invalidPatch)
		}
	}
	for _, invalid := range []string{
		`{}`, `{"layout":null}`, `{"layout":[]}`,
		`{"layout":[{"blockType":"retired"}]}`,
		`{"layout":[{"heading":"Hello"}]}`,
		`{"layout":[{"blockType":"hero"}]}`,
		`{"layout":[{"blockType":"hero","heading":null}]}`,
		`{"layout":[{"blockType":"hero","heading":{"en":"Hello"}}]}`,
		`{"layout":[{"blockType":"hero","heading":"Hello","_key":" "}]}`,
		`{"layout":[{"blockType":"hero","heading":"Hello","author":{"id":"author"}}]}`,
		`{"layout":[{"blockType":"hero","heading":"Hello","image":{"id":"asset"}}]}`,
		`{"layout":[{"blockType":"hero","heading":"Hello","children":[{"blockType":"note"}]}]}`,
		`{"layout":[{"blockType":"cta","url":"/a"},{"blockType":"cta","url":"/b"},{"blockType":"cta","url":"/c"}]}`,
	} {
		var value any
		if err := json.Unmarshal([]byte(invalid), &value); err != nil {
			t.Fatal(err)
		}
		if err := create.Validate(value); err == nil {
			t.Errorf("invalid create accepted: %s", invalid)
		}
	}
	// Inputs use canonical IDs; output components additionally accept population.
	if err := create.Validate(map[string]any{"layout": []any{map[string]any{"blockType": "hero", "heading": "Hello", "author": "author-id", "image": "asset-id"}}}); err != nil {
		t.Fatal(err)
	}
}
