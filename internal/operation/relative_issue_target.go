package operation

import (
	"fmt"
	"strconv"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ResolveIssueTarget resolves a callback's relative selectors only within its
// own candidate value. The caller's exact locale and embedded prefix stay bound.
func ResolveIssueTarget(field schema.Field, ctx Context, target operation.IssueTarget) (schema.Field, string, schema.LocaleCode, error) {
	path, locale := ctx.RuntimePath, ctx.Locale
	value := ctx.Value
	localized := ctx.AllLocales && field.Localized
	objectScope := false
	var children []schema.Field
	invalid := func(message string) (schema.Field, string, schema.LocaleCode, error) {
		return schema.Field{}, "", "", fmt.Errorf("invalid validation issue target at %s: %s", path, message)
	}
	if err := target.Err(); err != nil {
		return invalid(err.Error())
	}
	for _, segment := range target.Segments() {
		if segment.Locale != "" {
			if !localized || !ctx.AllLocales {
				return invalid("Locale requires a localized field in an all-locales callback; exact-locale callbacks inherit their locale")
			}
			translation, exists := value.Lookup(string(segment.Locale))
			if !exists {
				return invalid(fmt.Sprintf("exact translation %q is absent", segment.Locale))
			}
			value, localized, locale = translation, false, segment.Locale
			path = joinFieldPath(path, string(locale))
			continue
		}
		if localized {
			return invalid("select an exact translation with Locale before selecting its descendants")
		}
		if segment.Field != "" {
			if !objectScope {
				if field.Type != schema.FieldTypeGroup || field.Nested == nil {
					return invalid("Field requires a group or a selected row; use Row or Block before selecting repeated children")
				}
				children = field.Nested.ResolvedFields()
			}
			if value.Kind() != store.ValueObject {
				return invalid("cannot select a child inside an absent or non-object group; target the containing group instead")
			}
			var child *schema.Field
			for i := range children {
				if children[i].Name == segment.Field {
					child = &children[i]
					break
				}
			}
			if child == nil {
				return invalid(fmt.Sprintf("no child field named %q", segment.Field))
			}
			value, field = value.Get(segment.Field), *child
			path = joinFieldPath(path, segment.Field)
			localized = ctx.AllLocales && field.Localized
			objectScope = false
			continue
		}
		if objectScope || (segment.BlockType == "" && field.Type != schema.FieldTypeArray) || (segment.BlockType != "" && field.Type != schema.FieldTypeBlocks) {
			return invalid("Row requires an array; Block requires a Blocks field and its expected blockType")
		}
		index, i := -1, -1
		var selected store.Value
		for row := range value.Elements() {
			i++
			key, _ := row.Get("_key").StringValue()
			if key != segment.RowKey {
				continue
			}
			if index >= 0 {
				return invalid(fmt.Sprintf("row _key %q is ambiguous", segment.RowKey))
			}
			index, selected = i, row
		}
		if index < 0 {
			return invalid(fmt.Sprintf("no row with _key %q in the current candidate", segment.RowKey))
		}
		children = nil
		if segment.BlockType != "" {
			actual, _ := selected.Get("blockType").StringValue()
			if actual != segment.BlockType {
				return invalid(fmt.Sprintf("row %q has blockType %q, expected %q", segment.RowKey, actual, segment.BlockType))
			}
			if field.Blocks != nil {
				for _, block := range field.Blocks.ResolvedTypes() {
					if block.Slug == actual {
						children = block.ResolvedFields()
						break
					}
				}
			}
		} else if field.Nested != nil {
			children = field.Nested.ResolvedFields()
		}
		value, objectScope = selected, true
		path = joinFieldPath(path, strconv.Itoa(index))
	}
	return field, path, locale, nil
}
