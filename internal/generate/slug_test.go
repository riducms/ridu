package generate

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestGeneratedGoCreateModelAllowsServerGeneratedSlug(t *testing.T) {
	titlePath, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}
	slugPath, err := query.ParsePath("slug")
	if err != nil {
		t.Fatal(err)
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Slug contracts"},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			Fields: []schema.Field{
				{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Required: true, Text: &schema.TextField{}},
				{ID: "posts-slug", Name: "slug", Path: slugPath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Required: true, Unique: true, Index: true, Text: &schema.TextField{Slug: &schema.SlugField{SourcePath: titlePath}}},
			},
		}},
	})
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if count := strings.Count(text, "`json:\"slug,omitempty\"`"); count != 3 {
		t.Fatalf("generated Go slug presence tags = %d, want output/create/update to remain optional:\n%s", count, text)
	}
}
