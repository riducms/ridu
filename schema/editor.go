package schema

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

func cloneAdminExtensions(extensions map[string]json.RawMessage) map[string]json.RawMessage {
	if extensions == nil {
		return nil
	}
	result := make(map[string]json.RawMessage, len(extensions))
	for key, value := range extensions {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}

func validateAdminExtensions(extensions map[string]json.RawMessage, path string) error {
	for _, namespace := range slices.Sorted(maps.Keys(extensions)) {
		if namespace == "" || !json.Valid(extensions[namespace]) {
			return fmt.Errorf("%s.extensions[%q]: public extension metadata requires a nonempty namespace and finite JSON", path, namespace)
		}
	}
	return nil
}

var localEditorReference = regexp.MustCompile(`^app:[A-Za-z][A-Za-z0-9_]*$`)

// ValidateFieldEditor validates presentation metadata independently of frontend code.
func ValidateFieldEditor(kind FieldType, editor *FieldEditor) error {
	if editor == nil {
		return nil
	}
	if !localEditorReference.MatchString(editor.Reference) {
		return fmt.Errorf("invalid editor reference %q; expected app:name", editor.Reference)
	}
	switch kind {
	case FieldTypeText, FieldTypeTextarea, FieldTypeEmail, FieldTypeDate, FieldTypeCode, FieldTypeNumber, FieldTypeCheckbox, FieldTypeTextList, FieldTypeNumberList:
	default:
		return fmt.Errorf("editor %q requires a string, number, checkbox, text-list or number-list field, got %s", editor.Reference, kind)
	}
	if len(editor.Config) != 0 && !isJSONObject(editor.Config) {
		return fmt.Errorf("editor %q config must be a JSON object", editor.Reference)
	}
	return nil
}

// ValidateFieldEditors checks local editor declarations before generation or runtime use.
func ValidateFieldEditors(snapshot Snapshot) error {
	var inspect func([]Field, string) error
	inspect = func(fields []Field, owner string) error {
		for _, field := range fields {
			path := owner + "." + field.Path.String()
			if err := validateAdminExtensions(field.Admin.Extensions, path+".admin"); err != nil {
				return err
			}
			if field.Admin.Row != nil {
				if err := validateAdminExtensions(field.Admin.Row.Extensions, path+".admin.row"); err != nil {
					return err
				}
			}
			if field.Admin.Collapsible != nil {
				if err := validateAdminExtensions(field.Admin.Collapsible.Extensions, path+".admin.collapsible"); err != nil {
					return err
				}
			}
			if field.Admin.TabGroup != nil {
				if err := validateAdminExtensions(field.Admin.TabGroup.Extensions, path+".admin.tabGroup"); err != nil {
					return err
				}
			}
			if editor := field.Admin.Editor; editor != nil {
				path := owner + "." + field.Path.String()
				if err := ValidateFieldEditor(field.Type, editor); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				if field.Admin.Component != nil {
					return fmt.Errorf("%s selects both an editor and an admin component", path)
				}
			}
			if err := inspect(EmbeddedBlocks(field), owner); err != nil {
				return err
			}
			if field.Nested != nil {
				if label := field.Nested.RowLabelComponent; label != nil && label.Reference != "" {
					if field.Type != FieldTypeArray && field.Type != FieldTypeBlocks {
						return fmt.Errorf("%s: local row label requires an array or blocks field", path)
					}
					if err := validateRowLabelComponent(label, owner+"."+field.Path.String(), nil); err != nil {
						return err
					}
				}
				if err := inspect(field.Nested.ResolvedFields(), owner); err != nil {
					return err
				}
			}
			if field.Blocks != nil {
				for _, block := range field.Blocks.ResolvedTypes() {
					if err := inspect(block.ResolvedFields(), owner); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for _, collection := range snapshot.Collections {
		if err := inspect(collection.Fields, string(collection.Slug)); err != nil {
			return err
		}
	}
	for _, global := range snapshot.Globals {
		if err := inspect(global.Fields, string(global.Slug)); err != nil {
			return err
		}
	}
	return nil
}
