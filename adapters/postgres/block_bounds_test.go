package postgres

import (
	"context"
	"fmt"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"strings"
	"testing"
)

func phaseOneBlocks() schema.Field {
	path, _ := query.ParsePath("layout")
	return schema.Field{ID: "layout", Name: "layout", Path: path, Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
		Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 3, Types: []schema.BlockType{{Slug: "hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{}}}}}
}
func phaseOneBlockRows(count int) store.Value {
	rows := make([]store.Value, count)
	for i := range rows {
		rows[i] = store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d", i)), "blockType": store.String("hero")})
	}
	return store.List(rows...)
}

func TestPostgresPhaseOneBlockBoundsValidation(t *testing.T) {
	field := phaseOneBlocks()
	transaction := &postgresMigrationDataTransaction{locales: map[string]struct{}{"en": {}}}
	validate := func(field schema.Field, values store.Values) error {
		return transaction.validateValues([]schema.Field{field}, values, "posts", nil)
	}
	for _, values := range []store.Values{{}, {"layout": store.Null()}} {
		if err := validate(field, values); err != nil {
			t.Fatalf("optional absent/null: %v", err)
		}
	}
	for _, count := range []int{0, 1, 2, 3, 4} {
		err := validate(field, store.Values{"layout": phaseOneBlockRows(count)})
		invalid := count < 2 || count > 3
		if (err != nil) != invalid {
			t.Fatalf("%d rows: %v", count, err)
		}
		if err != nil && !strings.Contains(err.Error(), "layout") {
			t.Fatalf("missing precise field path: %v", err)
		}
	}
	field.Localized = true
	if err := validate(field, store.Values{"layout": store.Object(store.Values{"en": phaseOneBlockRows(1)})}); err == nil || !strings.Contains(err.Error(), "layout.en") {
		t.Fatalf("localized bounds: %v", err)
	}
}

func TestPostgresPhaseOneTightenedBoundsPlanner(t *testing.T) {
	before := atlasTestManifest(phaseOneBlocks())
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
	beforeField := phaseOneBlocks()
	afterField := phaseOneBlocks()
	afterField.ID = "content"
	afterField.Name = "content"
	afterField.Path, _ = query.ParsePath("wrapper.hero.content")
	afterField.Blocks.MaxRows = 2
	groupPath, _ := query.ParsePath("wrapper")
	before := schema.Snapshot{Globals: []schema.Collection{{ID: "home", Fields: []schema.Field{{ID: "wrapper", Name: "wrapper", Path: groupPath, Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{beforeField}}}}}}}}}
	after := schema.Snapshot{Globals: []schema.Collection{{ID: "landing", Fields: []schema.Field{{ID: "wrapper", Name: "wrapper", Path: groupPath, Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{afterField}}}}}}}}}
	mapping := referenceShapeMapping{fields: map[string]referenceShapeFieldIdentity{referenceShapeFieldKey("home", "layout"): {ID: "content", Path: afterField.Path.String()}}}
	owners := atlasIdentityMap{collections: map[schema.StableID]schema.StableID{"home": "landing"}}
	if path := tightenedBlockBounds(before, after, mapping, owners); path != "wrapper.hero.content" {
		t.Fatalf("renamed nested bound tightening path = %q", path)
	}
}
