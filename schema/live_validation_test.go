package schema_test

import (
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"strings"
	"testing"
)

func TestManifestLiveValidationRequiresWritableValue(t *testing.T) {
	path, _ := query.ParsePath("title")
	for _, test := range []struct {
		name  string
		field schema.Field
		valid bool
	}{
		{"text", schema.Field{Type: schema.FieldTypeText, Text: &schema.TextField{}}, true},
		{"group", schema.Field{Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{}}, true},
		{"UI", schema.Field{Type: schema.FieldTypeUI, Category: schema.FieldCategoryPresentation}, false},
		{"virtual", schema.Field{Type: schema.FieldTypeVirtual}, false},
		{"join", schema.Field{Type: schema.FieldTypeJoin}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := test.field
			f.ID, f.Name, f.Path, f.LiveValidation = "title", "title", path, true
			f.Admin.Label = "Title"
			if f.Category == "" {
				f.Category = schema.FieldCategoryScalar
			}
			manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "Live"}, Collections: []schema.Collection{{ID: "posts", Slug: "posts", Fields: []schema.Field{f}}}})
			encoded, err := manifest.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := schema.Parse(encoded)
			if !test.valid {
				if err == nil || !strings.Contains(err.Error(), "unsupported live validation") {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || !parsed.Snapshot().Collections[0].Fields[0].LiveValidation {
				t.Fatalf("capability parse = %v", err)
			}
		})
	}
}
