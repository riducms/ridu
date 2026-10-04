package generate

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestOpenAPIUploadRoutesDescribeCurrentRequests(t *testing.T) {
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Media API"},
		Collections: []schema.Collection{{
			ID: "media", Slug: "media", Labels: schema.CollectionLabels{Singular: "Asset", Plural: "Assets"},
			Upload: &schema.UploadSettings{MaxFileSize: 1 << 20},
			Fields: []schema.Field{{ID: "alt", Name: "alt", Type: schema.FieldTypeText, Required: true}},
		}},
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
	create := requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/media"), "post")
	createContent := requiredOpenAPIMap(t, requiredOpenAPIMap(t, create, "requestBody"), "content")
	if _, unexpected := createContent["application/json"]; unexpected {
		t.Fatal("upload creation advertises JSON without a file")
	}
	createSchema := requiredOpenAPIMap(t, requiredOpenAPIMap(t, createContent, "multipart/form-data"), "schema")
	createProperties := requiredOpenAPIMap(t, createSchema, "properties")
	for _, field := range []string{"file", "data", "image"} {
		if _, present := createProperties[field]; !present {
			t.Errorf("upload creation is missing multipart %q", field)
		}
	}
	if _, present := createProperties["publish"]; present {
		t.Fatal("upload creation advertises a second publication selector")
	}
	remote := requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/media/remote-upload"), "post")
	remoteContent := requiredOpenAPIMap(t, requiredOpenAPIMap(t, remote, "requestBody"), "content")
	remoteSchema := requiredOpenAPIMap(t, requiredOpenAPIMap(t, remoteContent, "application/json"), "schema")
	if _, present := requiredOpenAPIMap(t, remoteSchema, "properties")["publish"]; present || remoteSchema["additionalProperties"] != false {
		t.Fatal("remote upload creation permits an obsolete publication selector")
	}
	if required, _ := json.Marshal(createSchema["required"]); string(required) != `["file"]` {
		t.Errorf("upload creation required fields = %s", required)
	}
	if got := requiredOpenAPIMap(t, requiredOpenAPIMap(t, createProperties, "data"), "contentSchema")["$ref"]; got != "#/components/schemas/mediaCreate" {
		t.Errorf("upload creation data schema = %#v", got)
	}
	if got := requiredOpenAPIMap(t, createProperties, "data")["contentMediaType"]; got != "application/json" {
		t.Errorf("upload creation data content type = %#v", got)
	}
	if _, present := requiredOpenAPIMap(t, requiredOpenAPIMap(t, requiredOpenAPIMap(t, document, "components"), "schemas"), "mediaCreate")["properties"]; !present {
		t.Fatal("upload creation data refers to a missing typed schema")
	}

	update := requiredOpenAPIMap(t, requiredOpenAPIMap(t, paths, "/api/collections/media/{id}/upload"), "patch")
	updateContent := requiredOpenAPIMap(t, requiredOpenAPIMap(t, update, "requestBody"), "content")
	for mediaType, fields := range map[string][]string{
		"multipart/form-data": {"file", "data", "image", "publish"},
		"application/json":    {"data", "image", "filename", "publish"},
	} {
		properties := requiredOpenAPIMap(t, requiredOpenAPIMap(t, updateContent, mediaType), "schema")
		for _, field := range fields {
			if _, present := requiredOpenAPIMap(t, properties, "properties")[field]; !present {
				t.Errorf("upload update %s is missing %q", mediaType, field)
			}
		}
	}

	source := requiredOpenAPIMap(t, paths, "/api/collections/media/{id}/upload-source")
	get := requiredOpenAPIMap(t, source, "get")
	head := requiredOpenAPIMap(t, source, "head")
	if get["operationId"] == head["operationId"] {
		t.Fatal("GET and HEAD source operations share an operation ID")
	}
	if _, present := requiredOpenAPIMap(t, head, "responses")["200"]; !present {
		t.Fatal("source HEAD is missing its success response")
	}
	parameters, ok := head["parameters"].([]any)
	if !ok || len(parameters) == 0 || parameters[0].(map[string]any)["name"] != "id" {
		t.Fatalf("source HEAD path parameters = %#v", head["parameters"])
	}
}
