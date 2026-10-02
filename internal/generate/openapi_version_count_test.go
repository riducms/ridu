package generate

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestOpenAPIVersionCountsDescribeTheSharedCountEnvelope(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{Name: "Version count API", Localization: &schema.LocalizationSettings{
			Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}, DefaultLocale: "en",
		}},
		Collections: []schema.Collection{
			{ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"}, Versions: &schema.VersionSettings{}, Fields: []schema.Field{{ID: "title", Name: "title", Type: schema.FieldTypeText, Localized: true}}},
			{ID: "pages", Slug: "pages", Labels: schema.CollectionLabels{Singular: "Page", Plural: "Pages"}},
		},
		Globals: []schema.Global{
			{ID: "settings", Slug: "settings", Labels: schema.CollectionLabels{Singular: "Settings", Plural: "Settings"}, Versions: &schema.VersionSettings{}, Fields: []schema.Field{{ID: "title", Name: "title", Type: schema.FieldTypeText, Localized: true}}},
			{ID: "branding", Slug: "branding", Labels: schema.CollectionLabels{Singular: "Branding", Plural: "Branding"}},
		},
	})
	encoded, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	paths := requiredOpenAPIMap(t, document, "paths")
	for _, test := range []struct {
		path, operationID string
		needsID           bool
	}{
		{"/api/collections/posts/{id}/versions/count", "countVersionsposts", true},
		{"/api/globals/settings/versions/count", "countVersionssettings", false},
	} {
		get := requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, test.path), "get")
		if get["operationId"] != test.operationID {
			t.Fatalf("%s operation ID = %#v", test.path, get["operationId"])
		}
		parameters, ok := get["parameters"].([]any)
		if !ok {
			t.Fatalf("%s parameters = %#v", test.path, get["parameters"])
		}
		seen := map[string]bool{}
		for _, raw := range parameters {
			parameter := raw.(map[string]any)
			seen[parameter["name"].(string)] = true
		}
		if !seen["locale"] || !seen["fallback-locale"] || seen["id"] != test.needsID {
			t.Fatalf("%s parameters = %#v", test.path, parameters)
		}
		responses := requiredOpenAPIMap(t, get, "responses")
		content := requiredOpenAPIMap(t, requiredOpenAPIMap(t, responses, "200"), "content")
		result := requiredOpenAPIMap(t, requiredOpenAPIMap(t, content, "application/json"), "schema")
		if result["$ref"] != "#/components/schemas/CountEnvelope" {
			t.Fatalf("%s response schema = %#v", test.path, result)
		}
	}
	for _, path := range []string{
		"/api/collections/pages/{id}/versions/count",
		"/api/globals/branding/versions/count",
	} {
		if _, exists := paths[path]; exists {
			t.Fatalf("unversioned resource exposes %s", path)
		}
	}
	schemas := requiredOpenAPIMap(t, requiredOpenAPIMap(t, document, "components"), "schemas")
	count := requiredOpenAPIMap(t, schemas, "CountEnvelope")
	if count["type"] != "object" {
		t.Fatalf("count envelope type = %#v", count["type"])
	}
	properties := requiredOpenAPIMap(t, count, "properties")
	if len(properties) != 1 || requiredOpenAPIMap(t, properties, "totalDocs")["type"] != "integer" {
		t.Fatalf("count envelope properties = %#v", properties)
	}
	if required, _ := json.Marshal(count["required"]); string(required) != `["totalDocs"]` {
		t.Fatalf("count envelope required fields = %s", required)
	}
}
