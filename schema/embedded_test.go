package schema

import (
	"strings"
	"testing"
)

func TestEmbeddedMetadataRejectsCyclicSchemaBeforeCloning(t *testing.T) {
	fields := []Field{{Name: "cycle", Type: FieldTypeGroup}}
	fields[0].Nested = &NestedField{Fields: fields}
	err := ValidateEmbeddedMetadata(Snapshot{Collections: []Collection{{Fields: fields}}})
	if err == nil || !strings.Contains(err.Error(), "recursive schemas are unsupported") {
		t.Fatalf("cycle admitted: %v", err)
	}
}

func TestEmbeddedMappingsUseFieldTypeIdentityRegardlessOfPluginOrder(t *testing.T) {
	owner := Plugin{Key: "shapes", Version: "1.0.0", GoPackage: "example.com/shapes", APIVersion: CurrentPluginAPIVersion,
		Ridu:       &PluginCompatibility{Minimum: "0.1.0"},
		FieldTypes: []PluginFieldType{{Key: "outline", TypeScriptPackage: "@example/shapes", TypeScriptOutput: "Value", TypeScriptInput: "Value", EmbeddedTypes: []string{"widgets.widget"}}},
	}
	unrelated := owner
	unrelated.Key, unrelated.FieldTypes = "outline", nil
	field := Field{Type: FieldTypePlugin, Plugin: &PluginField{Key: "outline", EmbeddedTrees: []EmbeddedTree{{
		Version: EmbeddedTreeVersion, Key: "widgets", Children: "items", Tag: "kind",
		Cases: []EmbeddedTreeCase{{TagValue: "widget", Payload: "data", Discriminator: "blockType", Identity: "_key"}},
	}}}}
	for _, plugins := range [][]Plugin{{owner, unrelated}, {unrelated, owner}} {
		if err := validatePluginBuildMetadata(plugins); err != nil {
			t.Fatal(err)
		}
		snapshot := Snapshot{Plugins: plugins, Collections: []Collection{{Fields: []Field{field}}}}
		owner.FieldTypes[0].EmbeddedTypes = []string{"widgets.widget"}
		if err := ValidateEmbeddedMetadata(snapshot); err != nil {
			t.Fatal(err)
		}
		owner.FieldTypes[0].EmbeddedTypes = []string{"missing.case"}
		if err := ValidateEmbeddedMetadata(snapshot); err == nil || !strings.Contains(err.Error(), "missing.case") {
			t.Fatalf("plugin order %s, %s hid an invalid field mapping: %v", plugins[0].Key, plugins[1].Key, err)
		}
	}
}

func TestMinimalPluginFieldClaimsCannotOverlapDescriptorClaims(t *testing.T) {
	owner := Plugin{Key: "shapes", Version: "1.0.0", GoPackage: "example.com/shapes", APIVersion: CurrentPluginAPIVersion,
		Ridu:       &PluginCompatibility{Minimum: "0.1.0"},
		FieldTypes: []PluginFieldType{{Key: "outline", TypeScriptPackage: "@example/shapes", TypeScriptOutput: "Value", TypeScriptInput: "Value"}},
	}
	minimal := Plugin{Key: "outline"}
	for _, plugins := range [][]Plugin{{owner, minimal}, {minimal, owner}} {
		if err := validatePluginBuildMetadata(plugins); err == nil || !strings.Contains(err.Error(), "already owned") {
			t.Fatalf("ambiguous field claims admitted: %v", err)
		}
	}
	// A versioned plugin without this field claim has a distinct plugin identity.
	unrelated := owner
	unrelated.Key, unrelated.FieldTypes = "outline", nil
	if err := validatePluginBuildMetadata([]Plugin{owner, unrelated}); err != nil {
		t.Fatalf("independent plugin and field-type identities collided: %v", err)
	}
}
