package schema_test

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func equalFieldsSnapshot(t *testing.T, label string, localized bool) schema.Snapshot {
	t.Helper()
	path := func(value string) query.Path {
		parsed, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	text := func(id schema.StableID, name, location string) schema.Field {
		return schema.Field{ID: id, Name: name, Path: path(location), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: label}}
	}
	heading := text("block-card-heading", "heading", "heading")
	heading.Localized = localized
	children := schema.Field{ID: "block-card-children", Name: "children", Path: path("children"), Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested, Blocks: &schema.BlocksField{BlockReferences: []string{"note"}}, Admin: schema.FieldAdmin{Label: "Children"}}
	snapshot := schema.Snapshot{
		Blocks: []schema.BlockType{
			{Slug: "card", TypeName: "Card", Labels: schema.BlockLabels{Singular: "Card", Plural: "Cards"}, Fields: []schema.Field{heading, children}},
			{Slug: "note", TypeName: "Note", Labels: schema.BlockLabels{Singular: "Note", Plural: "Notes"}, Fields: []schema.Field{text("block-note-text", "text", "text")}},
		},
		Collections: []schema.Collection{{ID: "pages", Slug: "pages", Fields: []schema.Field{
			{ID: "pages-layout", Name: "layout", Path: path("layout"), Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested, Blocks: &schema.BlocksField{BlockReferences: []string{"card", "note"}}, Admin: schema.FieldAdmin{Label: "Layout"}},
		}}},
	}
	if err := schema.BindBlockReferences(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// Equality follows the definitions a list selects, not how its lazy
// placement views happen to have been traversed.
func TestEqualFieldsComparesDefinitionsNotBindings(t *testing.T) {
	left := equalFieldsSnapshot(t, "Heading", false)
	right := equalFieldsSnapshot(t, "Heading", false)
	// Materialize placement views on one side only.
	for _, block := range left.Collections[0].Fields[0].Blocks.ResolvedTypes() {
		for _, field := range block.ResolvedFields() {
			if field.Blocks != nil {
				_ = field.Blocks.ResolvedTypes()
			}
		}
	}
	if !schema.EqualFields(left.Collections[0].Fields, right.Collections[0].Fields) {
		t.Fatal("equal schemas compared unequal")
	}
	for name, changed := range map[string]schema.Snapshot{
		"definition field metadata": equalFieldsSnapshot(t, "Title", false),
		"definition localization":   equalFieldsSnapshot(t, "Heading", true),
	} {
		if schema.EqualFields(left.Collections[0].Fields, changed.Collections[0].Fields) {
			t.Fatalf("%s change compared equal", name)
		}
	}
}
