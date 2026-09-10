package sqlite

import (
	"github.com/riducms/ridu/schema"
	"testing"
)

func TestBlockGeneratedNameIsMetadataOnly(t *testing.T) {
	before, after := phaseOneBlocks(), phaseOneBlocks()
	after.Blocks.ResolvedTypes()[0].TypeName = "Hero"
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after.Blocks.ResolvedTypes()[0].TypeName = "Banner"
	if err := validateSQLiteAdditiveFields("posts", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
}
