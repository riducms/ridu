package generate

import (
	"encoding/json"
	"github.com/riducms/ridu/schema"
	"reflect"
	"testing"
)

func TestOpenAPIRecursiveBlocksSchemas(t *testing.T) {
	field := schema.Field{Name: "layout", Type: schema.FieldTypeBlocks, Required: true, Blocks: &schema.BlocksField{MinRows: 2, MaxRows: 4, Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{
		{Name: "heading", Type: schema.FieldTypeText, Required: true},
		{Name: "count", Type: schema.FieldTypeNumber, Required: true},
		{Name: "settings", Type: schema.FieldTypeGroup, Required: true, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "enabled", Type: schema.FieldTypeCheckbox, Required: true}}}},
		{Name: "items", Type: schema.FieldTypeArray, Required: true, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "body", Type: schema.FieldTypePlugin, Required: true, Plugin: &schema.PluginField{Key: "editor"}}}}},
		{Name: "inner", Type: schema.FieldTypeBlocks, Required: true, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "callout", Fields: []schema.Field{{Name: "body", Type: schema.FieldTypeTextarea}}}}}},
	}}, {Slug: "image", Fields: []schema.Field{{Name: "caption", Type: schema.FieldTypeText}}}}}}
	result, err := openAPIFieldSchema(field, map[string]json.RawMessage{"editor": json.RawMessage(`{"type":"object","properties":{"root":{"type":"object"}}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if result["type"] != "array" || result["minItems"] != nil || result["maxItems"] != nil {
		t.Fatalf("response must not apply write row bounds: %#v", result)
	}
	items := result["items"].(map[string]any)
	if items["discriminator"].(map[string]any)["propertyName"] != "blockType" {
		t.Fatal("missing discriminator")
	}
	variants := items["oneOf"].([]any)
	if len(variants) != 2 {
		t.Fatal("variants lost")
	}
	hero := variants[0].(map[string]any)
	properties := hero["properties"].(map[string]any)
	if properties["blockType"].(map[string]any)["const"] != "hero" || properties["count"].(map[string]any)["type"] != "number" {
		t.Fatal("wrong scalar/discriminator")
	}
	if !reflect.DeepEqual(hero["required"], []string{"blockType", "_key"}) {
		t.Fatalf("required: %#v", hero["required"])
	}
	settings := properties["settings"].(map[string]any)["properties"].(map[string]any)
	if settings["enabled"].(map[string]any)["type"] != "boolean" {
		t.Fatal("group shape lost")
	}
	rows := properties["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if rows["body"].(map[string]any)["type"] != "object" {
		t.Fatal("plugin schema lost")
	}
	inner := properties["inner"].(map[string]any)["items"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if inner["blockType"].(map[string]any)["const"] != "callout" {
		t.Fatal("nested blocks lost")
	}
	field.Required = false
	optional, err := openAPIFieldSchema(field, nil)
	if err != nil {
		t.Fatal(err)
	}
	alternatives := optional["anyOf"].([]any)
	if alternatives[1].(map[string]any)["type"] != "null" || alternatives[0].(map[string]any)["minItems"] != nil {
		t.Fatal("optional/null response semantics lost")
	}
}
