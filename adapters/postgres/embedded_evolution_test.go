package postgres

import (
	"fmt"
	"strings"
	"testing"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// postgresEmbeddedEvolutionFixture places the card and note definitions in an
// embedded tree case of the posts body field.
func postgresEmbeddedEvolutionFixture() schema.Manifest {
	return schema.NewManifest(postgresEmbeddedEvolutionSnapshot())
}

func postgresEmbeddedEvolutionSnapshot() schema.Snapshot {
	card := schema.BlockType{Slug: "card", TypeName: "Card", Fields: []schema.Field{
		atlasTextField("block-card-title", "title"), atlasBlockNameField("block-card-block-name", "blockName"),
	}}
	note := schema.BlockType{Slug: "note", TypeName: "Note", Fields: []schema.Field{atlasBlockNameField("block-note-block-name", "blockName")}}
	body := schema.Field{ID: "body", Name: "body", Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "outline", EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{
			TagValue: "widget", Payload: "data", Identity: "uid", Discriminator: "schema", BlockReferences: []string{"card", "note"},
		}}}}}}
	body.Path, _ = query.ParsePath("body")
	snapshot := atlasBlocksManifest([]schema.BlockType{card, note}, body).Snapshot()
	snapshot.Application.Localization = &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}
	return snapshot
}

// postgresEmbeddedTree is the fixture's embedded tree in snapshot.
func postgresEmbeddedTree(snapshot *schema.Snapshot) *schema.EmbeddedTree {
	return &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0]
}

// postgresEmbeddedBlock is the definition of slug in snapshot, which every
// placement of the block derives from.
func postgresEmbeddedBlock(t *testing.T, snapshot *schema.Snapshot, slug string) *schema.BlockType {
	t.Helper()
	for index := range snapshot.Blocks {
		if snapshot.Blocks[index].Slug == slug {
			return &snapshot.Blocks[index]
		}
	}
	t.Fatalf("block %q is not defined", slug)
	return nil
}

func TestPostgresEmbeddedEvolutionRequiresTransform(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*schema.EmbeddedTree, *schema.BlockType, *schema.Snapshot)
	}{
		{"root", func(tree *schema.EmbeddedTree, _ *schema.BlockType, _ *schema.Snapshot) {
			tree.Root = []string{"elsewhere"}
		}},
		{"children", func(tree *schema.EmbeddedTree, _ *schema.BlockType, _ *schema.Snapshot) { tree.Children = "children" }},
		{"payload", func(tree *schema.EmbeddedTree, _ *schema.BlockType, _ *schema.Snapshot) {
			tree.Cases[0].Payload = "payload"
		}},
		{"identity", func(tree *schema.EmbeddedTree, _ *schema.BlockType, _ *schema.Snapshot) {
			tree.Cases[0].Identity = "id"
		}},
		{"remove-variant", func(tree *schema.EmbeddedTree, _ *schema.BlockType, snapshot *schema.Snapshot) {
			tree.Cases[0].BlockReferences = tree.Cases[0].BlockReferences[:1]
			snapshot.Blocks = snapshot.Blocks[:1]
		}},
		{"remove-child", func(_ *schema.EmbeddedTree, card *schema.BlockType, _ *schema.Snapshot) {
			card.Fields = card.Fields[1:]
		}},
		{"require-child", func(_ *schema.EmbeddedTree, card *schema.BlockType, _ *schema.Snapshot) {
			card.Fields[0].Required = true
		}},
		{"localize-child", func(_ *schema.EmbeddedTree, card *schema.BlockType, _ *schema.Snapshot) {
			card.Fields[0].Localized = true
		}},
		{"add-min-array", func(_ *schema.EmbeddedTree, card *schema.BlockType, _ *schema.Snapshot) {
			path, _ := query.ParsePath("rows")
			title := atlasTextField("block-card-rows-title", "title")
			title.Path, _ = query.ParsePath("rows.title")
			card.Fields = append(card.Fields, schema.Field{ID: "block-card-rows", Name: "rows", Path: path, Type: schema.FieldTypeArray, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{MinRows: 1, Fields: []schema.Field{title}}})
		}},
		{"add-required", func(_ *schema.EmbeddedTree, card *schema.BlockType, _ *schema.Snapshot) {
			child := atlasTextField("block-card-caption", "caption")
			child.Required = true
			card.Fields = append(card.Fields, child)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := postgresEmbeddedEvolutionFixture()
			snapshot := before.Snapshot()
			test.change(postgresEmbeddedTree(&snapshot), postgresEmbeddedBlock(t, &snapshot, "card"), &snapshot)
			after := schema.NewManifest(snapshot)
			for _, destructive := range []bool{false, true} {
				if _, err := BuildArtifact(t.Context(), "embedded-change", &before, after, nil, destructive); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
					t.Fatalf("unsafe schema adopted (destructive=%v): %v", destructive, err)
				}
			}
			descriptor := ridumigration.DataTransformDescriptor{Name: "repair-embedded", Checksum: ridumigration.DataTransformChecksum([]byte("repair"))}
			artifact, err := buildPostgresTransformTestArtifact(t.Context(), "embedded-change", &before, after, descriptor)
			if err != nil {
				t.Fatalf("explicit transform: %v", err)
			}
			if err := validatePostgresPlannedSQL(t.Context(), artifact); err != nil {
				t.Fatalf("runner regeneration: %v", err)
			}
			// A re-signed manifest-only plan must also fail admission at apply.
			artifact.Phases = nil
			if err := validatePostgresPlannedSQL(t.Context(), artifact); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
				t.Fatalf("runner accepted omitted transform: %v", err)
			}
		})
	}
}

func TestPostgresEmbeddedEvolutionAllowsAdditionsAndPresentation(t *testing.T) {
	before := postgresEmbeddedEvolutionFixture()
	snapshot := before.Snapshot()
	tree := postgresEmbeddedTree(&snapshot)
	tree.Cases[0].BlockReferences = append(tree.Cases[0].BlockReferences, "new-card")
	card := postgresEmbeddedBlock(t, &snapshot, "card")
	card.Labels.Singular = "New label"
	card.Fields[0].Admin.Label = "New title label"
	value := "New default"
	card.Fields[0].Default = &value
	card.Fields = append(card.Fields, atlasTextField("block-card-caption", "caption"))
	snapshot.Blocks = append(snapshot.Blocks, schema.BlockType{Slug: "new-card", TypeName: "NewCard", Fields: []schema.Field{atlasTextField("block-new-card-title", "title"), atlasBlockNameField("block-new-card-block-name", "blockName")}})
	if _, err := BuildArtifact(t.Context(), "add-embedded", &before, schema.NewManifest(snapshot), nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresEmbeddedEvolutionAllowsRelaxedConstraints(t *testing.T) {
	min, max := 2, 10
	before := postgresEmbeddedEvolutionFixture()
	snapshot := before.Snapshot()
	child := &postgresEmbeddedBlock(t, &snapshot, "card").Fields[0]
	child.Required = true
	child.Text.MinLength, child.Text.MaxLength = &min, &max
	before = schema.NewManifest(snapshot)
	larger := 20
	child.Required = false
	child.Text.MinLength, child.Text.MaxLength = nil, &larger
	if _, err := BuildArtifact(t.Context(), "relax-embedded", &before, schema.NewManifest(snapshot), nil, false); err != nil {
		t.Fatal(err)
	}
	child.Text.MaxLength = &min
	if _, err := BuildArtifact(t.Context(), "tighten-embedded", &before, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("tightened constraint: %v", err)
	}
}

func TestPostgresEmbeddedEvolutionInspectsNestedChildren(t *testing.T) {
	before := postgresEmbeddedEvolutionFixture()
	snapshot := before.Snapshot()
	card := postgresEmbeddedBlock(t, &snapshot, "card")
	child := card.Fields[0]
	child.ID, child.Path = "block-card-settings-title", query.Field("settings", "title")
	group := schema.Field{ID: "block-card-settings", Name: "settings", Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{Fields: []schema.Field{child}}}
	group.Path, _ = query.ParsePath("settings")
	card.Fields = []schema.Field{group, card.Fields[1]}
	before = schema.NewManifest(snapshot)
	card.Fields[0].Nested.Fields[0].Required = true
	if _, err := BuildArtifact(t.Context(), "require-nested", &before, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("nested schema adoption: %v", err)
	}
	card.Fields[0].Nested.Fields[0].Required = false
	card.Fields[0].Nested.RowLabel = "title"
	if _, err := BuildArtifact(t.Context(), "label-nested", &before, schema.NewManifest(snapshot), nil, false); err != nil {
		t.Fatalf("presentation change: %v", err)
	}
}

func TestPostgresEmbeddedEvolutionRejectsDateFormatChanges(t *testing.T) {
	before := postgresEmbeddedEvolutionFixture()
	snapshot := before.Snapshot()
	child := &postgresEmbeddedBlock(t, &snapshot, "card").Fields[0]
	child.Type, child.Text, child.Date = schema.FieldTypeDate, nil, &schema.DateField{Format: schema.DateOnly}
	before = schema.NewManifest(snapshot)
	child.Date.Format = schema.TimeOnly
	if _, err := BuildArtifact(t.Context(), "change-date-format", &before, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("date wire format change: %v", err)
	}
}

func TestPostgresEmbeddedEvolutionRejectsInvalidGroupMaterialization(t *testing.T) {
	for _, addedGroup := range []bool{false, true} {
		t.Run(fmt.Sprintf("added=%v", addedGroup), func(t *testing.T) {
			before := postgresEmbeddedEvolutionFixture()
			snapshot := before.Snapshot()
			card := postgresEmbeddedBlock(t, &snapshot, "card")
			required := atlasTextField("block-card-settings-required-sibling", "requiredSibling")
			required.Path = query.Field("settings", "requiredSibling")
			required.Required = true
			caption := atlasTextField("block-card-settings-caption", "caption")
			caption.Path = query.Field("settings", "caption")
			groupPath, _ := query.ParsePath("settings")
			group := schema.Field{ID: "block-card-settings", Name: "settings", Path: groupPath, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{Fields: []schema.Field{caption, required}}}
			if !addedGroup {
				card.Fields = append(card.Fields, group)
				before = schema.NewManifest(snapshot)
			}
			value := "Default caption"
			if addedGroup {
				group.Nested.Fields[0].Default = &value
				card.Fields = append(card.Fields, group)
			} else {
				card.Fields[2].Nested.Fields[0].Default = &value
			}
			if _, err := BuildArtifact(t.Context(), "materialize-group", &before, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
				t.Fatalf("invalid materialization: %v", err)
			}
			card.Fields[2].Nested.Fields[1].Default = &value
			if _, err := BuildArtifact(t.Context(), "valid-defaults", &before, schema.NewManifest(snapshot), nil, false); err != nil {
				t.Fatalf("complete defaults must be allowed: %v", err)
			}
		})
	}
}
