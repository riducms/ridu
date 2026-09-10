package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// PrimitiveListField constrains the number of values in one ordered text or
// number list. Items have no stored identity or nested field definitions.
// TextField and NumberField hold the corresponding per-item constraints.
type PrimitiveListField struct {
	MinRows int `json:"minRows,omitempty"`
	MaxRows int `json:"maxRows,omitempty"`
}

func validatePrimitiveListMetadata(snapshot Snapshot) error {
	var inspect func([]Field, string) error
	inspect = func(fields []Field, prefix string) error {
		for i, f := range fields {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			isList := f.Type == FieldTypeTextList || f.Type == FieldTypeNumberList
			if !isList && f.List != nil {
				return fmt.Errorf("invalid list metadata at %s.list: only text-list and number-list fields accept list constraints", path)
			}
			if isList {
				if f.Category != FieldCategoryScalar || f.List == nil {
					return fmt.Errorf("invalid primitive list at %s: require scalar category and list metadata", path)
				}
				if f.Nested != nil || f.Blocks != nil || f.Select != nil {
					return fmt.Errorf("invalid primitive list at %s: primitive items have no children, Block types or options", path)
				}
				if f.Textarea != nil || f.Code != nil || f.Date != nil || f.Point != nil || f.UI != nil || f.Join != nil || f.Virtual != nil || f.Relationship != nil || f.Upload != nil || f.Plugin != nil {
					return fmt.Errorf("invalid primitive list at %s: only list counts and the matching text or number constraints are supported", path)
				}
				if f.Type == FieldTypeTextList && (f.Text == nil || f.Text.Slug != nil || f.Number != nil) {
					return fmt.Errorf("invalid text-list at %s: require per-item text constraints without a slug", path)
				}
				if f.Type == FieldTypeNumberList && (f.Number == nil || f.Text != nil || f.Number.Step != nil) {
					return fmt.Errorf("invalid number-list at %s: require per-item number bounds without a scalar input step", path)
				}
				if f.List.MinRows < 0 || f.List.MaxRows < 0 || f.List.MaxRows > 0 && f.List.MinRows > f.List.MaxRows {
					return fmt.Errorf("invalid primitive list bounds at %s.list: counts must be nonnegative and minimum must not exceed maximum", path)
				}
				if f.Default != nil {
					if err := validatePrimitiveListDefault(f); err != nil {
						return fmt.Errorf("invalid list default at %s.default: %w", path, err)
					}
				}
			}
			if err := inspect(EmbeddedBlocks(f), path+".plugin.embeddedTrees"); err != nil {
				return err
			}
			if f.Nested != nil {
				if err := inspect(f.Nested.ResolvedFields(), path+".nested.fields"); err != nil {
					return err
				}
			}
			if f.Blocks != nil {
				for j, b := range f.Blocks.ResolvedTypes() {
					if err := inspect(b.ResolvedFields(), fmt.Sprintf("%s.blocks.types[%d].fields", path, j)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for i, c := range snapshot.Collections {
		if err := inspect(c.Fields, fmt.Sprintf("collections[%d].fields", i)); err != nil {
			return err
		}
	}
	for i, g := range snapshot.Globals {
		if err := inspect(g.Fields, fmt.Sprintf("globals[%d].fields", i)); err != nil {
			return err
		}
	}
	return nil
}

func validatePrimitiveListDefault(f Field) error {
	raw := strings.TrimSpace(*f.Default)
	var items []json.RawMessage
	if !strings.HasPrefix(raw, "[") || json.Unmarshal([]byte(raw), &items) != nil {
		return fmt.Errorf("expected a primitive array")
	}
	if f.Required && len(items) == 0 || len(items) < f.List.MinRows || f.List.MaxRows > 0 && len(items) > f.List.MaxRows {
		return fmt.Errorf("item count violates the configured requiredness or list bounds")
	}
	for i, item := range items {
		if string(item) == "null" {
			return fmt.Errorf("item %d must not be null", i+1)
		}
		if f.Type == FieldTypeTextList {
			var value string
			if json.Unmarshal(item, &value) != nil {
				return fmt.Errorf("item %d must be a string", i+1)
			}
			length := utf8.RuneCountInString(value)
			if f.Text.MinLength != nil && length < *f.Text.MinLength || f.Text.MaxLength != nil && length > *f.Text.MaxLength {
				return fmt.Errorf("item %d violates text length bounds", i+1)
			}
		} else {
			var value float64
			if json.Unmarshal(item, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("item %d must be a finite number", i+1)
			}
			if f.Number.Min != nil && value < *f.Number.Min || f.Number.Max != nil && value > *f.Number.Max {
				return fmt.Errorf("item %d violates numeric bounds", i+1)
			}
		}
	}
	return nil
}
