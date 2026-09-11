package generate

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestOpenAPIUnifiedRequestsWithoutBlocksMatchRuntime(t *testing.T) {
	app, err := core.New(core.Config{Name: "Unified inputs", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("title").Required().MinLength(2).MaxLength(5).AfterRead(func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
			return operation.Replace(operation.Present("Published page title")), nil
		}),
		field.Number("score").Min(1).Max(5).AfterRead(func(_ operation.Context, value operation.Value[float64]) (operation.Change[float64], error) {
			if _, present := value.Get(); present {
				return operation.Replace(operation.Present[float64](100)), nil
			}
			return operation.Keep[float64](), nil
		}),
		field.JSON("opaque"),
		field.Text("status").Required().Default("ready"),
		field.Text("optional"),
		field.Group("meta", field.Fields{field.Text("required").Required()}),
		field.Array("rows", field.Fields{field.Text("title").Required()}),
		field.Virtual("summary", field.ValueString, func(operation.Context) (operation.Value[store.Value], error) {
			return operation.Present(store.String("computed")), nil
		}),
	}}}, Globals: []core.Global{{Slug: "settings", Fields: field.Fields{field.Text("name").Required()}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	name := string(app.Manifest().Snapshot().Collections[0].ID)
	create := resolvedResourceSchema(t, document, name+"Create")
	update := resolvedResourceSchema(t, document, name+"Update")
	output := resolvedResourceSchema(t, document, name)
	errors := resolvedResourceSchema(t, document, "ErrorEnvelope")
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{"title":"Page"}`, true},
		{`{"title":"Page","optional":null}`, true},
		{`{"title":"Page","score":3,"opaque":{"unknown":"retained","summary":"JSON-owned"}}`, true},
		{`{"title":"Page","meta":{"required":"yes"},"rows":[{"title":"Row"}]}`, true},
		{`{}`, false},
		{`{"title":null}`, false},
		{`{"title":""}`, false},
		{`{"title":"too long"}`, false},
		{`{"title":"X"}`, false},
		{`{"title":"Page","score":6}`, false},
		{`{"title":"Page","summary":"forged"}`, false},
		{`{"title":"Page","unknown":"value"}`, false},
		{`{"title":"Page","id":"unqualified"}`, false},
		{`{"title":"Page","createdAt":"2026-09-07"}`, false},
		{`{"title":"Page","meta":{"required":"yes","unknown":1}}`, false},
		{`{"title":"Page","rows":[{"title":"Row","unknown":1}]}`, false},
		{`{"title":"Page","meta":{}}`, false},
		{`{"title":"Page","rows":[{}]}`, false},
	} {
		t.Run(test.raw, func(t *testing.T) {
			var payload any
			if err := json.Unmarshal([]byte(test.raw), &payload); err != nil {
				t.Fatal(err)
			}
			if err := create.Validate(payload); (err == nil) != test.valid {
				t.Fatalf("create schema: %v", err)
			}
			response := httptest.NewRecorder()
			app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("POST", "/api/collections/pages", strings.NewReader(test.raw)))
			if (response.Code == 201) != test.valid {
				t.Fatalf("runtime %d: %s", response.Code, response.Body.String())
			}
			var envelope map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if test.valid {
				if err := output.Validate(envelope["doc"]); err != nil {
					t.Fatalf("response: %v", err)
				}
			} else if err := errors.Validate(envelope); err != nil {
				t.Fatalf("issues: %v", err)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"optional":null}`, `{"rows":[{"_key":"A"}]}`} {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := update.Validate(value); err != nil {
			t.Fatalf("valid patch %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"summary":"forged"}`, `{"unknown":"value"}`, `{"meta":{"unknown":1}}`, `{"rows":[{"_key":"A","unknown":1}]}`} {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := update.Validate(value); err == nil {
			t.Fatalf("invalid patch accepted: %s", raw)
		}
	}
	if _, exists := document.Components.Schemas[name+"Create"].Properties["summary"]; exists {
		t.Fatal("computed field accepted in create shape")
	}
	if _, exists := document.Components.Schemas[name+"Update"].Properties["summary"]; exists {
		t.Fatal("computed field accepted in update shape")
	}
	for path, methods := range map[string][]string{"/api/collections/pages": {"post"}, "/api/collections/pages/{id}": {"patch"}, "/api/globals/settings": {"patch"}} {
		for _, method := range methods {
			if document.Paths[path][method].(map[string]any)["requestBody"] == nil {
				t.Errorf("missing %s %s request contract", method, path)
			}
		}
	}
}

func TestOpenAPIArrayResponseRequiresIdentityAndKeepsRedactedChildrenOptional(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Array identity", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Array("rows", field.Fields{field.Text("title").Required()})}}}})
	if err != nil {
		t.Fatal(err)
	}
	property, err := openAPIFieldSchema(manifest.Snapshot().Collections[0].Fields[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	validator := resolvedOutputSchema(t, property)
	for _, test := range []struct {
		raw   string
		valid bool
	}{{`[{"_key":"A"}]`, true}, {`[{"title":"Page"}]`, false}, {`[{"_key":""}]`, false}, {`null`, true}} {
		var value any
		if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := validator.Validate(value); (err == nil) != test.valid {
			t.Errorf("array response %s: %v", test.raw, err)
		}
	}
}

func TestOpenAPIUnifiedBlockInputsPreserveManagedAndOpaqueValues(t *testing.T) {
	app, err := core.New(core.Config{Name: "Closed inputs", AllowIDOnCreate: true, Collections: []core.Collection{
		{Slug: "pages", Fields: field.Fields{
			field.Blocks("content", field.Block{Slug: "hero", Fields: field.Fields{field.Text("title").Required(), field.JSON("opaque")}}),
		}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	create := resolvedResourceSchema(t, document, "pagesCreate")
	update := resolvedResourceSchema(t, document, "pagesUpdate")
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{"id":"supplied-id","content":[{"blockType":"hero","_key":"A","title":"Hero","opaque":{"unknown":"value"}}]}`, true},
		{`{"content":[{"blockType":"hero","title":"Hero","unknown":"value"}]}`, false},
	} {
		var value any
		if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := create.Validate(value); (err == nil) != test.valid {
			t.Fatalf("create %s: %v", test.raw, err)
		}
		response := httptest.NewRecorder()
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("POST", "/api/collections/pages", strings.NewReader(test.raw)))
		if (response.Code == 201) != test.valid {
			t.Fatalf("runtime %d: %s", response.Code, response.Body.String())
		}
	}
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{"content":[{"blockType":"hero","_key":"A","opaque":{"anything":"value"}}]}`, true},
		{`{"content":[{"blockType":"hero","_key":"A","unknown":"value"}]}`, false},
	} {
		var value any
		if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := update.Validate(value); (err == nil) != test.valid {
			t.Fatalf("update %s: %v", test.raw, err)
		}
		response := httptest.NewRecorder()
		app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("PATCH", "/api/collections/pages/supplied-id", strings.NewReader(test.raw)))
		if (response.Code == 200) != test.valid {
			t.Fatalf("runtime %d: %s", response.Code, response.Body.String())
		}
	}
	manifest, err := core.Resolve(core.Config{Name: "Auth inputs", AllowIDOnCreate: true, Admin: core.AdminConfig{User: "users"}, Collections: []core.Collection{{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("name")}}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if err := resolvedResourceSchema(t, document, "usersCreate").Validate(map[string]any{"id": "person", "email": "person@example.com", "password": "correct horse battery staple", "name": "Person"}); err != nil {
		t.Fatalf("managed auth inputs rejected: %v", err)
	}
	if err := resolvedResourceSchema(t, document, "usersUpdate").Validate(map[string]any{"email": "new@example.com", "password": "another correct horse battery staple"}); err != nil {
		t.Fatalf("managed auth update rejected: %v", err)
	}
}

func TestOpenAPIReadTransformsDoNotReapplyCollectionBounds(t *testing.T) {
	rows := field.Array("rows", field.Fields{field.Text("title")}).Required().MinRows(2).MaxRows(3).
		AfterRead(func(operation.Context, operation.Value[store.Value]) (operation.Change[store.Value], error) {
			return operation.Replace(operation.Present(store.List())), nil
		})
	content := field.Blocks("content", field.Block{Slug: "hero", Fields: field.Fields{field.Text("title")}}).Required().MinRows(2).MaxRows(3).
		AfterRead(func(operation.Context, operation.Value[store.Value]) (operation.Change[store.Value], error) {
			return operation.Replace(operation.Present(store.List())), nil
		})
	app, err := core.New(core.Config{Name: "Transformed output", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{rows, content}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("POST", "/api/collections/pages", strings.NewReader(`{"rows":[{},{}],"content":[{"blockType":"hero"},{"blockType":"hero"}]}`)))
	if response.Code != 201 {
		t.Fatalf("runtime %d: %s", response.Code, response.Body.String())
	}
	var envelope struct{ Doc map[string]any }
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if err := resolvedResourceSchema(t, document, "pages").Validate(envelope.Doc); err != nil {
		t.Fatalf("transformed output rejected: %v", err)
	}
	if err := resolvedResourceSchema(t, document, "pagesCreate").Validate(map[string]any{"rows": []any{}, "content": []any{}}); err == nil {
		t.Fatal("request lost authored collection bounds")
	}
}
