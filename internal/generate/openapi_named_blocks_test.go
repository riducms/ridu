package generate

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/blocktypes"
)

func TestOpenAPINamedBlocksReuseResolvedVariants(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required().Localized(), field.Relationship("author", "authors"), field.Upload("image", "assets"), field.Group("settings", field.Fields{field.Checkbox("wide").Required()}).Required(), field.Blocks("children", field.Block{Slug: "note", Fields: field.Fields{field.Text("body")}})}}
	manifest, err := core.Resolve(core.Config{Name: "Named output", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{
		{Slug: "authors", Fields: field.Fields{field.Text("name")}},
		{Slug: "assets", Upload: true, Fields: field.Fields{field.Text("alt")}},
		{Slug: "pages", Fields: field.Fields{field.Blocks("layout", hero).Required().MinRows(1).MaxRows(4), field.Blocks("secondary", hero), field.Blocks("translated", hero).Localized()}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := openAPI(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatal(err)
	}
	catalog, err := blocktypes.Build(manifest.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range catalog.Variants {
		for _, suffix := range []string{"", "AllLocales"} {
			definition, ok := document.Components.Schemas[variant.Name+suffix]
			if !ok {
				t.Fatalf("missing shared variant %s%s", variant.Name, suffix)
			}
			if len(definition.Required) != 2 {
				t.Fatalf("output children must remain redactable: %#v", definition.Required)
			}
			if definition.Properties["blockType"].(map[string]any)["const"] != variant.Block.Slug {
				t.Fatal("discriminator changed")
			}
		}
	}
	layout := document.Components.Schemas["pages"].Properties["layout"].(map[string]any)["anyOf"].([]any)
	single := layout[0].(map[string]any)["items"].(map[string]any)
	mapping := single["discriminator"].(map[string]any)["mapping"].(map[string]any)
	if mapping["hero"] != "#/components/schemas/Hero" {
		t.Fatal("discriminator mapping must use persisted slug, not generated symbol")
	}
	validator := resolvedResourceSchema(t, document, "pages")
	valid := map[string]any{"id": "p", "createdAt": "now", "updatedAt": "now", "layout": []any{map[string]any{"blockType": "hero", "_key": "key", "heading": "Hello", "settings": map[string]any{"wide": true}, "children": []any{map[string]any{"blockType": "note", "_key": "child", "body": nil}}, "author": "author-id", "image": map[string]any{"id": "asset-id"}}}, "secondary": nil}
	if err := validator.Validate(valid); err != nil {
		t.Fatalf("single locale: %v", err)
	}
	row := valid["layout"].([]any)[0].(map[string]any)
	row["heading"] = map[string]any{"en": "Hello", "fr": "Bonjour"}
	valid["translated"] = map[string]any{"en": []any{map[string]any{"blockType": "hero", "_key": "translated", "heading": "Hello"}}}
	if err := validator.Validate(valid); err != nil {
		t.Fatalf("all locales: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func()
		valid  bool
	}{
		{"missing identity", func() { delete(row, "_key") }, false},
		{"unknown discriminator", func() { row["blockType"] = "retired" }, false},
		{"invalid locale child", func() { row["heading"] = map[string]any{"en": 17} }, false},
		{"maximum is request-only", func() { valid["layout"] = []any{row, row, row, row, row} }, true},
		{"minimum is request-only", func() { valid["layout"] = []any{} }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			row["_key"] = "key"
			row["blockType"] = "hero"
			row["heading"] = map[string]any{"en": "Hello"}
			valid["layout"] = []any{row}
			test.mutate()
			if err := validator.Validate(valid); (err == nil) != test.valid {
				t.Fatalf("output validation: %v", err)
			}
		})
	}
}
