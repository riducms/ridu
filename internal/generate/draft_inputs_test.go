package generate

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/internal/typescript"
)

func draftInputApp(t *testing.T) *core.App {
	t.Helper()
	app, err := core.New(core.Config{Name: "Editorial inputs", Collections: []core.Collection{{
		Slug: "pages", Versions: true, VersionConfig: core.VersionConfig{Drafts: true},
		Fields: field.Fields{
			field.Text("title").Required().MinLength(3).MaxLength(10),
			field.Number("score").Min(1).Max(5),
			field.Email("contact").Required(),
			field.Group("meta", field.Fields{field.Text("description").Required()}),
			field.Array("rows", field.Fields{field.Text("label").Required()}).MinRows(2).MaxRows(3),
			field.Blocks("layout", field.Block{Slug: "hero", TypeName: "Hero", Fields: field.Fields{field.Text("heading").Required()}}),
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestDraftInputSchemasDeferCompletenessNotStructure(t *testing.T) {
	app := draftInputApp(t)
	encoded, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	name := string(app.Manifest().Snapshot().Collections[0].ID)
	draft := resolvedResourceSchema(t, document, name+"DraftCreate")
	strict := resolvedResourceSchema(t, document, name+"Create")
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{}`, true},
		{`{"title":null,"contact":"","meta":{},"rows":[{}],"layout":[{"blockType":"hero"}]}`, true},
		{`{"title":"x","rows":[]}`, true},
		{`{"title":3}`, false},
		{`{"title":"too long for a page"}`, false},
		{`{"score":6}`, false},
		{`{"rows":[{},{},{},{}]}`, false},
		{`{"meta":{"unknown":1}}`, false},
		{`{"layout":[{}]}`, false},
		{`{"layout":[{"blockType":"unknown"}]}`, false},
		{`{"layout":[{"blockType":"hero","heading":3}]}`, false},
		{`{"_revision":1}`, false},
	} {
		t.Run(test.raw, func(t *testing.T) {
			var input any
			if err := json.Unmarshal([]byte(test.raw), &input); err != nil {
				t.Fatal(err)
			}
			if err := draft.Validate(input); (err == nil) != test.valid {
				t.Fatalf("draft schema validation = %v; want valid %v", err, test.valid)
			}
			response := httptest.NewRecorder()
			app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("POST", "/api/collections/pages?draft=true", strings.NewReader(test.raw)))
			if (response.Code == 201) != test.valid {
				t.Fatalf("draft runtime = %d: %s", response.Code, response.Body.String())
			}
		})
	}
	if err := strict.Validate(map[string]any{}); err == nil {
		t.Fatal("strict create schema permits an incomplete document")
	}
	// OpenAPI's format is an annotation; the engine owns email admission.
	response := httptest.NewRecorder()
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, httptest.NewRequest("POST", "/api/collections/pages?draft=true", strings.NewReader(`{"contact":"invalid"}`)))
	if response.Code != 422 {
		t.Fatalf("malformed email in draft = %d: %s", response.Code, response.Body.String())
	}
}

func TestDraftDiscardOpenAPIRequiresDocumentID(t *testing.T) {
	app := draftInputApp(t)
	encoded, err := openAPI(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	path := document.Paths["/api/collections/pages/{id}/discard-draft"]
	parameters, ok := path["parameters"].([]any)
	if !ok {
		t.Fatalf("discard route has no path parameters: %#v", path)
	}
	for _, parameter := range parameters {
		value, ok := parameter.(map[string]any)
		if ok && value["name"] == "id" && value["in"] == "path" && value["required"] == true {
			return
		}
	}
	t.Fatalf("discard route does not require id: %#v", parameters)
}

func TestDraftInputGeneratorsPreserveExactFields(t *testing.T) {
	manifest := draftInputApp(t).Manifest()
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"type PageDraft struct", "*core.Input[string]", "type HeroDraft struct",
		"core.NewTypedCollection[Page, PageCreate, PageUpdate, PageDraft]",
		`json:"_publishedRevision,omitempty"`, `json:"_hasDraftChanges,omitempty"`,
	} {
		if !strings.Contains(string(generated), fragment) {
			t.Fatalf("Go draft contract missing %q", fragment)
		}
	}
	compileGeneratedGo(t, generated)
	runGeneratedGoConsumer(t, generated, generatedDraftReadConsumer)
	client, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"export interface PagesDraftCreate", "export interface PagesDraftUpdate",
		"\"meta\"?: {\n\t\t\"description\"?: string | null;\n\t} | null;",
		"HeroDraftInput", "HeroDraftUpdate", "draftCreate: PagesDraftCreate;", "draftUpdate: PagesDraftUpdate;",
	} {
		if !strings.Contains(string(client), fragment) {
			t.Fatalf("TypeScript draft contract missing %q:\n%s", fragment, client)
		}
	}
}

func TestDraftAuthContractsKeepIdentityRequiredAndNonNull(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Auth drafts", Admin: core.AdminConfig{User: "users"}, Collections: []core.Collection{{
		Slug: "users", Auth: true, Versions: true, VersionConfig: core.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("displayName").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	name := string(manifest.Snapshot().Collections[0].ID)
	create := resolvedResourceSchema(t, document, name+"DraftCreate")
	for _, input := range []map[string]any{{}, {"email": nil}, {"email": "editor@example.test", "password": "secret"}} {
		if err := create.Validate(input); err == nil {
			t.Fatalf("draft auth resource create accepts missing identity or credential: %#v", input)
		}
	}
	if err := create.Validate(map[string]any{"email": "editor@example.test"}); err != nil {
		t.Fatalf("draft auth resource create requires editorial completeness: %v", err)
	}
	if err := resolvedResourceSchema(t, document, name+"DraftUpdate").Validate(map[string]any{"email": nil}); err == nil {
		t.Fatal("draft auth update accepts null identity")
	}
	for _, suffix := range []string{"Create", "Update", "DraftCreate", "DraftUpdate"} {
		resource := document.Components.Schemas[name+suffix]
		if _, hasPassword := resource.Properties["password"]; hasPassword {
			t.Fatalf("%s resource schema includes credential data", suffix)
		}
		if err := resolvedResourceSchema(t, document, name+suffix).Validate(map[string]any{"password": "secret"}); err == nil {
			t.Fatalf("%s resource schema accepts credential data", suffix)
		}
	}
	strict := resolvedResourceSchema(t, document, name+"Create")
	if err := strict.Validate(map[string]any{"email": "editor@example.test"}); err == nil {
		t.Fatal("strict auth resource create accepts missing authored required field")
	}
	if err := strict.Validate(map[string]any{"email": "editor@example.test", "displayName": "Editor"}); err != nil {
		t.Fatalf("complete strict auth resource create = %v", err)
	}
	envelope := resolvedResourceSchema(t, document, name+"AuthCreateUserRequest")
	for _, input := range []map[string]any{
		{"data": map[string]any{"email": "editor@example.test"}},
		{"password": "secret"},
		{"data": map[string]any{"email": nil}, "password": "secret"},
		{"data": map[string]any{"email": "editor@example.test", "password": "nested"}, "password": "secret"},
	} {
		if err := envelope.Validate(input); err == nil {
			t.Fatalf("auth credential envelope accepted invalid input: %#v", input)
		}
	}
	if err := envelope.Validate(map[string]any{"data": map[string]any{"email": "editor@example.test"}, "password": "secret"}); err != nil {
		t.Fatalf("auth credential envelope rejected incomplete draft data: %v", err)
	}
	createUser := document.Paths["/api/auth/users/create-user"]["post"].(map[string]any)
	requestBody := createUser["requestBody"].(map[string]any)
	content := requestBody["content"].(map[string]any)["application/json"].(map[string]any)
	requestSchema := content["schema"].(map[string]any)
	if requestSchema["$ref"] != "#/components/schemas/"+name+"AuthCreateUserRequest" {
		t.Fatalf("auth create-user request body = %#v", requestSchema)
	}
	parameters := createUser["parameters"].([]any)
	foundDraft := false
	for _, parameter := range parameters {
		entry := parameter.(map[string]any)
		if entry["name"] == "draft" && entry["in"] == "query" {
			foundDraft = true
		}
	}
	if !foundDraft {
		t.Fatalf("auth create-user omitted draft selector: %#v", parameters)
	}
	client, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`export interface UsersDraftCreate {` + "\n\t\"email\": string;",
		`export interface UsersDraftUpdate {` + "\n\t\"email\"?: string;",
	} {
		if !strings.Contains(string(client), fragment) {
			t.Fatalf("TypeScript auth draft identity contract missing %q", fragment)
		}
	}
}

const generatedDraftReadConsumer = `package generated_test
import ("encoding/json"; "testing"; g "example.com/stable-consumer")
func TestIncompleteBlockRead(t *testing.T) {
 var page g.Page
 if err := json.Unmarshal([]byte("{\"id\":\"page-1\",\"_status\":\"draft\",\"_revision\":1,\"title\":null,\"contact\":null,\"layout\":[{\"blockType\":\"hero\",\"_key\":\"hero-1\",\"heading\":null}]}"), &page); err != nil {t.Fatal(err)}
 var hero g.Hero
 if err := json.Unmarshal([]byte("{\"blockType\":\"hero\",\"_key\":\"hero-1\",\"heading\":null}"), &hero); err != nil {t.Fatal(err)}
 if hero.Heading != nil {t.Fatal("unfinished heading was not null")}
}
`
