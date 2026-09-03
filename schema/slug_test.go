package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestSlugMetadataRoundTripsThroughManifestParsing(t *testing.T) {
	snapshot := slugSnapshot(t)
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	slug := manifest.Snapshot().Collections[0].Fields[1]
	if slug.Text == nil || slug.Text.Slug == nil || slug.Text.Slug.SourcePath.String() != "seo.title" {
		t.Fatalf("slug metadata = %#v", slug.Text)
	}
}

func TestSlugMetadataRejectsInvalidSourcesAndConstraints(t *testing.T) {
	tests := map[string]func(*schema.Snapshot){
		"missing source": func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[1].Text.Slug.SourcePath = path(t, "missing")
		},
		"slug source": func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[1].Text.Slug.SourcePath = path(t, "otherSlug")
			snapshot.Collections[0].Fields = append(snapshot.Collections[0].Fields, slugField(t, "otherSlug", "seo.title"))
		},
		"not unique": func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[1].Unique = false
		},
		"localized source": func(snapshot *schema.Snapshot) {
			snapshot.Collections[0].Fields[0].Localized = true
			snapshot.Application.Localization = &schema.LocalizationSettings{
				DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}},
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := slugSnapshot(t)
			mutate(&snapshot)
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "slug") {
				t.Fatalf("Parse() error = %v", err)
			}
		})
	}
}

func slugSnapshot(t *testing.T) schema.Snapshot {
	t.Helper()
	return schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "Slug test"},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts",
			Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			Fields: []schema.Field{
				{
					ID: "seo", Name: "seo", Path: path(t, "seo"), Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
					Admin: schema.FieldAdmin{Label: "SEO"}, Nested: &schema.NestedField{Fields: []schema.Field{{
						ID: "seo-title", Name: "title", Path: path(t, "seo.title"), Type: schema.FieldTypeText,
						Category: schema.FieldCategoryScalar, Required: true, Admin: schema.FieldAdmin{Label: "Title"}, Text: &schema.TextField{},
					}}},
				},
				slugField(t, "slug", "seo.title"),
			},
		}},
		Plugins: []schema.Plugin{},
	}
}

func slugField(t *testing.T, name, source string) schema.Field {
	t.Helper()
	return schema.Field{
		ID: schema.StableID(name), Name: name, Path: path(t, name), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
		Required: true, Unique: true, Index: true, Admin: schema.FieldAdmin{Label: "Slug"},
		Text: &schema.TextField{Slug: &schema.SlugField{SourcePath: path(t, source)}},
	}
}

func path(t *testing.T, value string) query.Path {
	t.Helper()
	parsed, err := query.ParsePath(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
