package core_test

import (
	"encoding/json"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"strings"
	"testing"
)

type multiFieldPlugin struct {
	key       string
	fields    []string
	validator string
}

func (p multiFieldPlugin) Key() string { return p.key }
func (p multiFieldPlugin) Descriptor() ridu.PluginDescriptor {
	descriptor := ridu.PluginDescriptor{Version: "1.0.0", GoPackage: "example.com/shapes", APIVersion: ridu.PluginAPIVersion, Ridu: ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion}, Admin: &ridu.AdminPluginMetadata{Package: "@example/" + p.key, Export: "shapesAdminPlugin", APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: 1}}
	for _, key := range p.fields {
		descriptor.FieldTypes = append(descriptor.FieldTypes, ridu.PluginFieldType{Key: key, TypeScriptPackage: "@example/shapes", TypeScriptOutput: "Value", TypeScriptInput: "Input"})
	}
	return descriptor
}
func (p multiFieldPlugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	validators := map[string]ridu.PluginFieldValidator{}
	for _, key := range p.fields {
		validators[key] = func(ctx ridu.PluginFieldValidationContext) []schema.Issue {
			if _, ok := ctx.Value.StringValue(); !ok {
				return []schema.Issue{{Code: "shape", Path: ctx.RuntimePath, Message: "expected string"}}
			}
			return nil
		}
	}
	if p.validator != "" {
		validators[p.validator] = func(ridu.PluginFieldValidationContext) []schema.Issue { return nil }
	}
	return validators
}
func TestPluginOwnsMultipleDistinctFieldTypes(t *testing.T) {
	plugin := multiFieldPlugin{key: "shapes", fields: []string{"color", "outline"}}
	config := ridu.Config{Name: "Shapes", Plugins: []ridu.Plugin{plugin}, Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Plugin("color", "color", json.RawMessage(`{}`)), field.Plugin("outline", "outline", json.RawMessage(`{}`))}}}}
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Create(t.Context(), "posts", store.Values{"color": store.String("red"), "outline": store.String("small")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Create(t.Context(), "posts", store.Values{"color": store.Number(42)}, nil); err == nil {
		t.Fatal("field-type validator was not used")
	}
	encoded, err := json.Marshal(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Snapshot().Plugins[0].FieldTypes) != 2 {
		t.Fatal("field types lost in round trip")
	}
	config.Plugins = append(config.Plugins, multiFieldPlugin{key: "other", fields: []string{"color"}})
	if _, err := ridu.Resolve(config); err == nil || !strings.Contains(err.Error(), "duplicate_plugin_field_type") {
		t.Fatalf("duplicate owner error = %v", err)
	}
	config.Plugins = []ridu.Plugin{plugin, multiFieldPlugin{key: "other", fields: []string{"note"}, validator: "color"}}
	if _, err := ridu.Resolve(config); err == nil || !strings.Contains(err.Error(), "invalid_plugin_field_validator") {
		t.Fatalf("foreign validator error = %v", err)
	}
}
