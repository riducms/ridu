package postgres

import (
	"context"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"strings"
	"testing"
)

// phaseOneBlocks is a bounded blocks field selecting phaseOneHero.
func phaseOneBlocks() schema.Field {
	path, _ := query.ParsePath("layout")
	return schema.Field{ID: "layout", Name: "layout", Path: path, Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
		Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, BlockReferences: []string{"hero"}}}
}

func phaseOneHero() schema.BlockType {
	return schema.BlockType{Slug: "hero", TypeName: "Hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{atlasBlockNameField("block-hero-block-name", "blockName")}}
}

func phaseOneManifest() schema.Manifest {
	return atlasBlocksManifest([]schema.BlockType{phaseOneHero()}, phaseOneBlocks())
}

func TestPostgresPhaseOneTightenedBoundsPlanner(t *testing.T) {
	before := phaseOneManifest()
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields[0].Blocks.MinRows = 3
	after := schema.NewManifest(afterSnapshot)
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "tighten-blocks", &before, after); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("unsafe planner adoption: %v", err)
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "repair-blocks", Checksum: ridumigration.DataTransformChecksum([]byte("repair-v1"))}
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "tighten-blocks", &before, after, descriptor); err != nil {
		t.Fatalf("explicit data-transform plan: %v", err)
	}
	afterSnapshot.Collections[0].Fields[0].Blocks.MinRows = 1
	afterSnapshot.Collections[0].Fields[0].Blocks.MaxRows = 0
	relaxed := schema.NewManifest(afterSnapshot)
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "relax-blocks", &before, relaxed); err != nil {
		t.Fatalf("relaxed bounds: %v", err)
	}
}

func TestPostgresPhaseOneBoundsGuardFollowsOwnerAndFieldRenames(t *testing.T) {
	// The bounded field belongs to the section definition, which every
	// placement shares: its identity is definition-relative.
	beforeField := phaseOneBlocks()
	beforeField.ID = "block-section-layout"
	afterField := phaseOneBlocks()
	afterField.ID = "block-section-content"
	afterField.Name = "content"
	afterField.Path, _ = query.ParsePath("content")
	afterField.Blocks.MaxRows = 2
	groupPath, _ := query.ParsePath("wrapper")
	snapshot := func(owner schema.StableID, field schema.Field) schema.Snapshot {
		section := schema.BlockType{Slug: "section", TypeName: "Section", Fields: []schema.Field{field}}
		value := schema.Snapshot{Blocks: []schema.BlockType{section, phaseOneHero()}, Collections: []schema.Collection{{ID: owner, Fields: []schema.Field{{ID: owner + "-wrapper", Name: "wrapper", Path: groupPath, Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{BlockReferences: []string{"section"}}}}}}}
		if err := schema.BindBlockReferences(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before, after := snapshot("home", beforeField), snapshot("landing", afterField)
	mapping := referenceShapeMapping{fields: map[string]referenceShapeFieldIdentity{
		referenceShapeFieldKey(definitionOwner("section"), "block-section-layout"): {ID: "block-section-content", Path: "content"},
	}}
	owners := atlasIdentityMap{collections: map[schema.StableID]schema.StableID{"home": "landing"}}
	if path := tightenedBlockBounds(before, after, mapping, owners); path != "block section.content" {
		t.Fatalf("renamed nested bound tightening path = %q", path)
	}
	// Without the reviewed field rename the bounds are not compared.
	if path := tightenedBlockBounds(before, after, referenceShapeMapping{}, owners); path != "" {
		t.Fatalf("unmapped nested bound tightening path = %q", path)
	}
	// Without the collection rename no placement survives the change.
	if path := tightenedBlockBounds(before, after, mapping, atlasIdentityMap{}); path != "" {
		t.Fatalf("unplaced bound tightening path = %q", path)
	}
}
