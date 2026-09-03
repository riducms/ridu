package httpapi

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestNestedQueryPathsAreValidatedAgainstTheSchema(t *testing.T) {
	collection := nestedQueryCollection()
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

func TestNestedGroupSortIsAcceptedButRepeatedPathsAreRejected(t *testing.T) {
	collection := nestedQueryCollection()
	sorts, err := decodeSort([]string{"-seo.title"}, collection)
	if err != nil {
		t.Fatalf("decodeSort: %v", err)
	}
	if len(sorts) != 1 || sorts[0].Path.String() != "seo.title" || sorts[0].Direction != query.Descending {
		t.Fatalf("sorts = %#v", sorts)
	}
	if _, err := decodeSort([]string{"links.label"}, collection); err == nil {
		t.Fatal("array leaf sort succeeded")
	}
}

func nestedQueryCollection() schema.Collection {
	return schema.Collection{Slug: "posts", Fields: []schema.Field{
		{ID: "seo", Name: "seo", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "seo-title", Name: "title", Type: schema.FieldTypeText},
		}}},
		{ID: "links", Name: "links", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "links-label", Name: "label", Type: schema.FieldTypeText},
		}}},
		{ID: "layout", Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{
			{Key: "hero", Label: "Hero", Fields: []schema.Field{{ID: "hero-heading", Name: "heading", Type: schema.FieldTypeText}}},
		}}},
	}}
}
