package field

import (
	"encoding/json"
	"regexp"
)

var editorReference = regexp.MustCompile(`^app:[A-Za-z][A-Za-z0-9_]*$`)

// Editor returns a detached application editor selection.
func (d View) Editor() (string, json.RawMessage) {
	c := d.admin.Editor
	if c.PluginKey != "" {
		return "", nil
	}
	return c.Key, componentConfig(c)
}

// LocalRowLabel returns a detached application row heading selection.
func (d View) LocalRowLabel() (string, json.RawMessage) {
	c := d.admin.RowLabel
	if c.PluginKey != "" {
		return "", nil
	}
	return c.Key, componentConfig(c)
}
func componentConfig(c ComponentRef) json.RawMessage {
	if c.Config.Kind() == "" {
		return nil
	}
	raw, _ := json.Marshal(c.Config)
	return raw
}
