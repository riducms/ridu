package sqlite

import (
	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"testing"
)

// phaseOneBlocks places the hero definition, after change edits it when given.
func phaseOneBlocks(t *testing.T, change func(*schema.BlockType)) schema.Field {
	path, _ := query.ParsePath("layout")
	hero := schema.BlockType{Slug: "hero", TypeName: "Hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{}}
	if change != nil {
		change(&hero)
	}
	return schematest.Bind(t, "posts", []schema.BlockType{hero}, schema.Field{ID: "layout", Name: "layout", Path: path, Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
		Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, BlockReferences: []string{"hero"}}})[0]
}
func TestSQLitePhaseOneBlockSummaryIsMetadataOnly(t *testing.T) {
	before := phaseOneBlocks(t, nil)
	after := phaseOneBlocks(t, func(hero *schema.BlockType) { hero.Admin = &schema.BlockAdmin{RowLabel: "heading"} })
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after.Blocks.MinRows++
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err == nil {
		t.Fatal("tightened bounds admitted as metadata-only")
	}
}
