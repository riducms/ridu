package color

import (
	"encoding/json"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

const Key = "color"

// Value is the color type used in generated Go documents.
type Value string

type plugin struct{}

func New() ridu.Plugin     { return plugin{} }
func (plugin) Key() string { return Key }

func Field(name string) field.PluginField {
	// This field does not need any per-field configuration.
	return field.Plugin(name, Key, json.RawMessage(`{}`))
}
