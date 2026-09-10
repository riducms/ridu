package schema_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestManifestPublicAdminExtensionsOwnNestedJSON(t *testing.T) {
	extensions := map[string]json.RawMessage{"catalog": json.RawMessage(`{"value":"original"}`)}
	path, _ := query.ParsePath("title")
	input := schema.Snapshot{Version: schema.CurrentVersion, Collections: []schema.Collection{{ID: "pages", Slug: "pages", Fields: []schema.Field{{ID: "title", Name: "title", Path: path, Type: schema.FieldTypeText, Admin: schema.FieldAdmin{
		Extensions:  extensions,
		Row:         &schema.FieldRow{ID: "row", Extensions: extensions},
		Collapsible: &schema.FieldCollapsible{ID: "collapse", Extensions: extensions},
		TabGroup:    &schema.FieldTabGroup{ID: "tabs", Extensions: extensions},
	}}}}}}
	manifest := schema.NewManifest(input)
	original, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	extensions["catalog"][2] = 'X'
	extensions["new"] = json.RawMessage(`true`)
	for _, attached := range []func(schema.FieldAdmin) map[string]json.RawMessage{
		func(a schema.FieldAdmin) map[string]json.RawMessage { return a.Extensions },
		func(a schema.FieldAdmin) map[string]json.RawMessage { return a.Row.Extensions },
		func(a schema.FieldAdmin) map[string]json.RawMessage { return a.Collapsible.Extensions },
		func(a schema.FieldAdmin) map[string]json.RawMessage { return a.TabGroup.Extensions },
	} {
		metadata := attached(manifest.Snapshot().Collections[0].Fields[0].Admin)
		metadata["catalog"][2] = 'Y'
		delete(metadata, "catalog")
	}
	after, err := manifest.Bytes()
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("public JSON attachments escaped immutable manifest ownership", err)
	}
}
