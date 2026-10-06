package sqlite

import (
	"github.com/riducms/ridu/schema"
	"testing"
)

func TestBlockGeneratedNameIsMetadataOnly(t *testing.T) {
	before := phaseOneBlocks(t, func(hero *schema.BlockType) { hero.TypeName = "Hero" })
	after := phaseOneBlocks(t, func(hero *schema.BlockType) { hero.TypeName = "Hero" })
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after = phaseOneBlocks(t, func(hero *schema.BlockType) { hero.TypeName = "Banner" })
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
}
