package httpapi

import (
	"testing"

	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestNestedQueryPathsAreValidatedAgainstTheSchema(t *testing.T) {
	collection := nestedQueryCollection(t)
	for _, encoded := range []string{
		`{"seo.title":{"like":"Ridu"}}`,
		`{"links.label":{"equals":"Docs"}}`,
		`{"layout.hero.heading":{"exists":true}}`,
	} {
		expression, err := decodeWhere([]byte(encoded), collection)
		if err != nil {
			t.Fatalf("decodeWhere(%s): %v", encoded, err)
		}
		if expression.Node().Comparison.Path.String() == "" {
			t.Fatalf("decodeWhere(%s) returned an empty path", encoded)
		}
	}
	if _, err := decodeWhere([]byte(`{"seo.missing":{"equals":"no"}}`), collection); err == nil {
		t.Fatal("unknown nested field succeeded")
	}
}

// The operation engine decides which paths sort, so REST and the Local API
// reject the same paths with the same unsupported_path issue.
func TestSortTermsAreOnlyParsed(t *testing.T) {
	sorts, err := decodeSort([]string{"-seo.title", "links.label"})
	if err != nil {
		t.Fatalf("decodeSort: %v", err)
	}
	if len(sorts) != 2 || sorts[0].Path.String() != "seo.title" || sorts[0].Direction != query.Descending || sorts[1].Path.String() != "links.label" {
		t.Fatalf("sorts = %#v", sorts)
	}
	for _, terms := range [][]string{{"title", "-title"}, {"seo..title"}} {
		if _, err := decodeSort(terms); err == nil {
			t.Fatalf("decodeSort(%q) accepted a malformed term", terms)
		}
	}
}

func nestedQueryCollection(t *testing.T) schema.Collection {
	path := func(raw string) query.Path {
		parsed, err := query.ParsePath(raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	hero := schema.BlockType{Slug: "hero", TypeName: "Hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{{ID: "block-hero-heading", Name: "heading", Path: path("heading"), Type: schema.FieldTypeText}}}
	return schema.Collection{Slug: "posts", Fields: schematest.Bind(t, "posts", []schema.BlockType{hero},
		schema.Field{ID: "seo", Name: "seo", Path: path("seo"), Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "seo-title", Name: "title", Path: path("seo.title"), Type: schema.FieldTypeText},
		}}},
		schema.Field{ID: "links", Name: "links", Path: path("links"), Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "links-label", Name: "label", Path: path("links.label"), Type: schema.FieldTypeText},
		}}},
		schema.Field{ID: "layout", Name: "layout", Path: path("layout"), Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"hero"}}},
	)}
}
