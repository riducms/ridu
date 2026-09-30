package schemadiff

import (
	"fmt"

	"github.com/riducms/ridu/schema"
)

// ValidateFieldRenameOnly refuses a confirmed field rename that changes more
// than the field's name: one whose localization changes, or whose group,
// array, blocks or embedded-block children are renamed or change localization
// in the same migration.
//
// A field rename moves the stored value whole, so renamed children would stay
// under their old names inside it, where the new config does not read them,
// while the migration reports the data preserved. RenameCandidates cannot rule
// these pairs out: it matches a removed and an added field by shape and
// ignores child names, which is what lets a collection rename carry renamed
// fields, but a standalone field rename records no pairs for its children.
func ValidateFieldRenameOnly(before, after schema.Field) error {
	if before.Localized != after.Localized {
		return fmt.Errorf("field %q cannot be renamed to %q in the same migration that changes whether it is localized; rename the field first, then change its localization in a separate migration",
			before.Path.String(), after.Path.String())
	}
	if child, change := renamedFieldChildChange(before, after); child != "" {
		return fmt.Errorf("field %q cannot be renamed to %q in the same migration that %s its child %q; rename the field first, then change its children in a separate migration",
			before.Path.String(), after.Path.String(), change, child)
	}
	return nil
}

// renamedFieldChildChange reports the first child of before, by its path, that
// after does not keep under the same name and localization.
func renamedFieldChildChange(before, after schema.Field) (child, change string) {
	compare := func(beforeFields, afterFields []schema.Field) (string, string) {
		afterByName := make(map[string]schema.Field, len(afterFields))
		for _, field := range afterFields {
			afterByName[field.Name] = field
		}
		// A child that is gone counts as renamed only when a new name appeared
		// beside it. A plain removal is the planner's to judge.
		beforeNames := make(map[string]struct{}, len(beforeFields))
		for _, field := range beforeFields {
			beforeNames[field.Name] = struct{}{}
		}
		appeared := false
		for _, field := range afterFields {
			if _, existed := beforeNames[field.Name]; !existed {
				appeared = true
			}
		}
		for _, field := range beforeFields {
			kept, exists := afterByName[field.Name]
			if !exists {
				if appeared {
					return field.Path.String(), "renames"
				}
				continue
			}
			if field.Localized != kept.Localized {
				return field.Path.String(), "changes the localization of"
			}
			if child, change := renamedFieldChildChange(field, kept); child != "" {
				return child, change
			}
		}
		return "", ""
	}
	compareBlocks := func(beforeTypes, afterTypes []schema.BlockType) (string, string) {
		afterBySlug := make(map[string]schema.BlockType, len(afterTypes))
		for _, block := range afterTypes {
			afterBySlug[block.Slug] = block
		}
		for _, block := range beforeTypes {
			if kept, exists := afterBySlug[block.Slug]; exists {
				if child, change := compare(block.ResolvedFields(), kept.ResolvedFields()); child != "" {
					return child, change
				}
			}
		}
		return "", ""
	}
	if before.Nested != nil && after.Nested != nil {
		if child, change := compare(before.Nested.ResolvedFields(), after.Nested.ResolvedFields()); child != "" {
			return child, change
		}
	}
	if before.Blocks != nil && after.Blocks != nil {
		if child, change := compareBlocks(before.Blocks.ResolvedTypes(), after.Blocks.ResolvedTypes()); child != "" {
			return child, change
		}
	}
	afterEmbedded := make(map[string]schema.Field)
	for _, container := range schema.EmbeddedBlocks(after) {
		afterEmbedded[container.Name] = container
	}
	for _, container := range schema.EmbeddedBlocks(before) {
		if kept, exists := afterEmbedded[container.Name]; exists && container.Blocks != nil && kept.Blocks != nil {
			if child, change := compareBlocks(container.Blocks.ResolvedTypes(), kept.Blocks.ResolvedTypes()); child != "" {
				return child, change
			}
		}
	}
	return "", ""
}
