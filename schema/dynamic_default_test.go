package schema_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestManifestDynamicDefaultIsCapabilityMetadata(t *testing.T) {
	path, _ := query.ParsePath("title")
	literal := "Untitled"
	for _, test := range []struct {
		name  string
		field schema.Field
		want  string
	}{
		{"text", schema.Field{Type: schema.FieldTypeText, Text: &schema.TextField{}}, ""},
		{"conflicting literal", schema.Field{Type: schema.FieldTypeText, Text: &schema.TextField{}, Default: &literal}, "conflicting defaults"},
		{"aggregate", schema.Field{Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{}}, "unsupported dynamic default"},
		{"JSON", schema.Field{Type: schema.FieldTypeJSON}, "unsupported dynamic default"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := test.field
			candidate.ID, candidate.Name, candidate.Path, candidate.DynamicDefault = "title", "title", path, true
			candidate.Admin.Label = "Title"
			if candidate.Category == "" {
				candidate.Category = schema.FieldCategoryScalar
			}
			manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "Defaults"}, Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{candidate}}}})
			data, err := manifest.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := schema.Parse(data)
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("parse error = %v; want %s", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !parsed.Snapshot().Collections[0].Fields[0].DynamicDefault {
				t.Fatal("lost default capability")
			}
		})
	}
}
