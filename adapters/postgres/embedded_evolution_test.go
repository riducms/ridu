package postgres

import (
	"fmt"
	"strings"
	"testing"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func postgresEmbeddedEvolutionFixture() schema.Manifest {
	title := atlasTextField("title", "title")
	title.Path, _ = query.ParsePath("body.widgets.widget.card.title")
	body := schema.Field{ID: "body", Name: "body", Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "outline", EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "data", Identity: "uid", Discriminator: "schema", Types: []schema.BlockType{
			{Slug: "card", Fields: []schema.Field{title}}, {Slug: "note"},
		}}}}}}}
	body.Path, _ = query.ParsePath("body")
	manifest := atlasTestManifest(body)
	snapshot := manifest.Snapshot()
	snapshot.Application.Localization = &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}
	return schema.NewManifest(snapshot)
}

func TestPostgresEmbeddedEvolutionRequiresTransform(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*schema.EmbeddedTree)
	}{
		{"root", func(tree *schema.EmbeddedTree) { tree.Root = []string{"elsewhere"} }},
		{"children", func(tree *schema.EmbeddedTree) { tree.Children = "children" }},
		{"payload", func(tree *schema.EmbeddedTree) { tree.Cases[0].Payload = "payload" }},
		{"identity", func(tree *schema.EmbeddedTree) { tree.Cases[0].Identity = "id" }},
		{"remove-variant", func(tree *schema.EmbeddedTree) { tree.Cases[0].Types = tree.Cases[0].ResolvedTypes()[:1] }},
		{"remove-child", func(tree *schema.EmbeddedTree) { tree.Cases[0].ResolvedTypes()[0].Fields = nil }},
		{"require-child", func(tree *schema.EmbeddedTree) { tree.Cases[0].ResolvedTypes()[0].ResolvedFields()[0].Required = true }},
		{"localize-child", func(tree *schema.EmbeddedTree) { tree.Cases[0].ResolvedTypes()[0].ResolvedFields()[0].Localized = true }},
		{"add-min-array", func(tree *schema.EmbeddedTree) {
			path, _ := query.ParsePath("body.widgets.widget.card.rows")
			tree.Cases[0].ResolvedTypes()[0].Fields = append(tree.Cases[0].ResolvedTypes()[0].ResolvedFields(), schema.Field{ID: "rows", Name: "rows", Path: path, Type: schema.FieldTypeArray, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{MinRows: 1, Fields: []schema.Field{atlasTextField("row-title", "title")}}})
		}},
		{"add-required", func(tree *schema.EmbeddedTree) {
			child := atlasTextField("caption", "caption")
			child.Required = true
			tree.Cases[0].ResolvedTypes()[0].Fields = append(tree.Cases[0].ResolvedTypes()[0].ResolvedFields(), child)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := postgresEmbeddedEvolutionFixture()
			snapshot := before.Snapshot()
			test.change(&snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0])
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
	tree := &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0]
	tree.Cases[0].Types = append(tree.Cases[0].ResolvedTypes(), schema.BlockType{Slug: "new-card", Fields: []schema.Field{atlasTextField("new-title", "title")}})
	card := &tree.Cases[0].ResolvedTypes()[0]
	card.Labels.Singular = "New label"
	card.ResolvedFields()[0].Admin.Label = "New title label"
	value := "New default"
	card.ResolvedFields()[0].Default = &value
	card.Fields = append(card.ResolvedFields(), atlasTextField("caption", "caption"))
	if _, err := BuildArtifact(t.Context(), "add-embedded", &before, schema.NewManifest(snapshot), nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresEmbeddedEvolutionAllowsRelaxedConstraints(t *testing.T) {
	min, max := 2, 10
	before := postgresEmbeddedEvolutionFixture()
	snapshot := before.Snapshot()
	child := &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[0]
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
	card := &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0]
	child := card.ResolvedFields()[0]
	group := schema.Field{ID: "group", Name: "settings", Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{Fields: []schema.Field{child}}}
	group.Path, _ = query.ParsePath("body.widgets.widget.card.settings")
	card.Fields = []schema.Field{group}
	before = schema.NewManifest(snapshot)
	card.ResolvedFields()[0].Nested.ResolvedFields()[0].Required = true
	if _, err := BuildArtifact(t.Context(), "require-nested", &before, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("nested schema adoption: %v", err)
	}
	card.ResolvedFields()[0].Nested.ResolvedFields()[0].Required = false
	card.ResolvedFields()[0].Nested.RowLabel = "title"
	if _, err := BuildArtifact(t.Context(), "label-nested", &before, schema.NewManifest(snapshot), nil, false); err != nil {
		t.Fatalf("presentation change: %v", err)
	}
}

func TestPostgresEmbeddedEvolutionRejectsDateFormatChanges(t *testing.T) {
	before := postgresEmbeddedEvolutionFixture()
	snapshot := before.Snapshot()
	child := &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[0]
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
			card := &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0]
			required := atlasTextField("required-sibling", "requiredSibling")
			required.Required = true
			caption := atlasTextField("caption", "caption")
			groupPath, _ := query.ParsePath("body.widgets.widget.card.settings")
			group := schema.Field{ID: "settings", Name: "settings", Path: groupPath, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{Fields: []schema.Field{caption, required}}}
			if !addedGroup {
				card.Fields = append(card.ResolvedFields(), group)
				before = schema.NewManifest(snapshot)
			}
			value := "Default caption"
			if addedGroup {
				group.Nested.ResolvedFields()[0].Default = &value
				card.Fields = append(card.ResolvedFields(), group)
			} else {
				card.ResolvedFields()[1].Nested.ResolvedFields()[0].Default = &value
			}
			if _, err := BuildArtifact(t.Context(), "materialize-group", &before, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
				t.Fatalf("invalid materialization: %v", err)
			}
			card.ResolvedFields()[1].Nested.ResolvedFields()[1].Default = &value
			if _, err := BuildArtifact(t.Context(), "valid-defaults", &before, schema.NewManifest(snapshot), nil, false); err != nil {
				t.Fatalf("complete defaults must be allowed: %v", err)
			}
		})
	}
}
