// Package embeddedplugin is a small structured-outline plugin used by the
// embedded authoring reference and cross-surface contract tests. Its envelope
// deliberately differs from a rich-text editor document.
package embeddedplugin

import (
	"encoding/json"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const Key = "outline"

type Plugin struct{}

func (Plugin) Key() string { return Key }
func (Plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: "1.0.0", GoPackage: "github.com/riducms/ridu/tests/contracts/embedded_plugin", APIVersion: ridu.PluginAPIVersion, Ridu: ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
		Admin:      &ridu.AdminPluginMetadata{Package: "@ridu-test/outline", Export: "outlineAdminPlugin", APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: 1},
		FieldTypes: []ridu.PluginFieldType{{Key: Key, TypeScriptPackage: "@ridu-test/outline", TypeScriptOutput: "Outline", TypeScriptInput: "OutlineInput", GoPackage: "github.com/riducms/ridu/tests/contracts/embedded_plugin", GoType: "Document", EmbeddedTypes: []string{"widgets.widget"}, JSONSchema: []byte(`{"type":"object","properties":{"outline":{"type":"array","items":{"type":"object"}}},"required":["outline"]}`)}},
	}
}
func (Plugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{Key: func(ctx ridu.PluginFieldValidationContext) []schema.Issue {
		if ctx.Value.Kind() != store.ValueObject {
			return []schema.Issue{{Code: "outline", Path: ctx.RuntimePath, Message: "outline must be an object"}}
		}
		if ctx.Value.Get("outline").Kind() != store.ValueList {
			return []schema.Issue{{Code: "outline", Path: ctx.RuntimePath + ".outline", Message: "outline must be an array"}}
		}
		return nil
	}}
}

func Field(name string, types ...field.Block) field.PluginField {
	return field.Plugin(name, Key, json.RawMessage(`{}`)).EmbeddedTrees(field.EmbeddedTree{
		Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: types}},
	}).Admin(field.Admin{Editor: field.PluginComponent(Key, "Outline")})

}
func Widget(kind, key string, payload store.Values) store.Value {
	payload = store.CloneValues(payload)
	payload["schema"] = store.String(kind)
	if key != "" {
		payload["uid"] = store.String(key)
	}
	return store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(payload)})
}
func Value(nodes ...store.Value) store.Value {
	return store.Object(store.Values{"outline": store.List(nodes...)})
}

// Document and Node are portable envelope types; T is the generated payload
// union for this field placement, never a plugin-owned field engine.
type Document[T any] struct {
	Outline []Node[T] `json:"outline"`
}
type Node[T any] struct {
	Kind    string    `json:"kind"`
	Items   []Node[T] `json:"items,omitempty"`
	Content *T        `json:"content,omitempty"`
}
