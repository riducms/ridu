package postgres

import (
	"encoding/json"
	"fmt"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func validateEmbeddedJSON(field schema.Field, value any, currentRoot bool) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var canonical store.Value
	if err := json.Unmarshal(encoded, &canonical); err != nil {
		return err
	}
	if currentRoot {
		field.Localized = false
	}
	return embedded.ValidateValue(field, canonical, field.Name, true, nil)
}

// PostgreSQL migration values use encoding/json objects. Only the shared
// interpreter locates payloads; migration callbacks resume ordinary traversal.
func rewriteEmbeddedCollectionReferences(value any, field schema.Field, before, after string) (bool, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	var canonical store.Value
	if err := json.Unmarshal(encoded, &canonical); err != nil {
		return false, err
	}
	changed := false
	updated, envelopeChanged, err := embedded.RewriteCollectionReferences(field, canonical, before, after, func(o embedded.Occurrence) (store.Values, error) {
		raw, err := json.Marshal(o.Payload)
		if err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		for _, child := range o.Fields {
			if candidate, exists := payload[child.Name]; exists {
				fieldChanged, err := rewriteFieldCollectionReferences(candidate, child, false, before, after)
				if err != nil {
					return nil, err
				}
				changed = fieldChanged || changed
			}
		}
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		var result store.Values
		err = json.Unmarshal(raw, &result)
		return result, err
	})
	if err != nil {
		return false, err
	}
	if !(changed || envelopeChanged) {
		return false, nil
	}
	encoded, err = json.Marshal(updated)
	if err != nil {
		return false, err
	}
	var result any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return false, err
	}
	switch current := value.(type) {
	case map[string]any:
		object, ok := result.(map[string]any)
		if !ok {
			return false, fmt.Errorf("embedded reference rewrite changed its envelope shape")
		}
		clear(current)
		for key, value := range object {
			current[key] = value
		}
	case []any:
		items, ok := result.([]any)
		if !ok || len(items) != len(current) {
			return false, fmt.Errorf("embedded reference rewrite changed its envelope shape")
		}
		copy(current, items)
	default:
		return false, fmt.Errorf("embedded reference rewrite changed its envelope shape")
	}
	return true, nil
}
