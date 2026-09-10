package postgres

import (
	"fmt"
	"reflect"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
)

// Atlas cannot inspect schema changes inside JSONB. Admit additive embedded
// schemas separately; a compiled transform must validate or repair other changes.
func validatePostgresEmbeddedEvolution(before, after schema.Snapshot, mapping referenceShapeMapping, owners atlasIdentityMap) error {
	targets := map[string]schema.Field{}
	storedRoots := map[string]bool{}
	var index func(schema.StableID, []schema.Field)
	index = func(owner schema.StableID, fields []schema.Field) {
		for _, field := range fields {
			targets[referenceShapeFieldKey(owner, field.ID)] = field
			index(owner, schema.ChildFields(field))
		}
	}
	for _, owner := range append(after.Collections, after.Globals...) {
		for _, root := range owner.Fields {
			if root.Category != schema.FieldCategoryPresentation {
				storedRoots[referenceShapeFieldKey(owner.ID, root.ID)] = true
			}
		}
		index(owner.ID, owner.Fields)
	}
	var inspect func(schema.StableID, []schema.Field, string) error
	inspect = func(owner schema.StableID, fields []schema.Field, changedContainer string) error {
		for _, previous := range fields {
			identity := mapping.field(owner, previous)
			current, exists := targets[referenceShapeFieldKey(owners.collection(owner), identity.ID)]
			if !exists && embedded.HasFields(previous) {
				return fmt.Errorf("PostgreSQL embedded field %q was removed from retained JSON storage; register a compiled data transform to remove or migrate its stored payloads", previous.Path.String())
			}
			if exists && (embedded.HasFields(previous) || embedded.HasFields(current)) {
				if changedContainer != "" {
					return fmt.Errorf("PostgreSQL embedded field %q has a changed JSON container at %q; register a compiled data transform to migrate its stored layout", previous.Path.String(), changedContainer)
				}
				compare := postgresEmbeddedEvolution{owner: owner, mapping: mapping, owners: owners}
				// The ordinary content rewriter handles confirmed outer-field
				// renames. Names inside embedded payloads still need a transform.
				if _, confirmed := mapping.fields[referenceShapeFieldKey(owner, previous.ID)]; confirmed {
					current.Name = previous.Name
				}
				if err := compare.fields(previous.Path.String(), []schema.Field{previous}, []schema.Field{current}); err != nil {
					return fmt.Errorf("PostgreSQL embedded schema: %w; register a compiled data transform that validates or repairs existing values", err)
				}
				continue // The comparison already visits every payload descendant.
			}
			container := changedContainer
			if exists && (previous.Type != current.Type || previous.Localized != current.Localized) {
				container = previous.Path.String()
			}
			if err := inspect(owner, schema.ChildFields(previous), container); err != nil {
				return err
			}
		}
		return nil
	}
	for _, owner := range append(before.Collections, before.Globals...) {
		for _, root := range owner.Fields {
			identity := mapping.field(owner.ID, root)
			if !storedRoots[referenceShapeFieldKey(owners.collection(owner.ID), identity.ID)] {
				// Only top-level stored fields own SQL columns. Missing descendants
				// remain in a surviving JSON column, even when an ancestor is removed.
				continue
			}
			if err := inspect(owner.ID, []schema.Field{root}, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

type postgresEmbeddedEvolution struct {
	owner   schema.StableID
	mapping referenceShapeMapping
	owners  atlasIdentityMap
}

func (e postgresEmbeddedEvolution) types(path string, before, after []schema.BlockType) error {
	byKey := map[string]schema.BlockType{}
	for _, block := range after {
		byKey[block.Slug] = block
	}
	for _, previous := range before {
		current, exists := byKey[previous.Slug]
		if !exists {
			return fmt.Errorf("embedded variant %q at %q was removed", previous.Slug, path)
		}
		if err := e.fields(path+"."+previous.Slug, previous.ResolvedFields(), current.ResolvedFields()); err != nil {
			return err
		}
	}
	return nil
}

func (e postgresEmbeddedEvolution) fields(path string, before, after []schema.Field) error {
	byID := map[schema.StableID]schema.Field{}
	for _, field := range after {
		byID[field.ID] = field
	}
	for _, previous := range before {
		identity := e.mapping.field(e.owner, previous)
		current, exists := byID[identity.ID]
		if !exists {
			return fmt.Errorf("embedded child %q at %q was removed", previous.Name, path)
		}
		delete(byID, identity.ID)
		_, previouslyInvalid := embeddedMissingValue(previous)
		if _, invalid := embeddedMissingValue(current); invalid && !previouslyInvalid {
			return fmt.Errorf("embedded child %q at %q no longer accepts an omitted value", current.Name, path)
		}
		comparison := current
		// Identity/path moves are already bound by the ordinary rename planner.
		comparison.ID, comparison.Path = previous.ID, previous.Path
		comparison.Admin, comparison.Default, comparison.Index = previous.Admin, previous.Default, previous.Index
		if previous.Required {
			comparison.Required = true
		}
		if previous.Nested != nil && current.Nested != nil {
			if err := e.fields(current.Path.String(), previous.Nested.ResolvedFields(), current.Nested.ResolvedFields()); err != nil {
				return err
			}
			nested := *previous.Nested
			nested.MinRows, nested.MaxRows = current.Nested.MinRows, current.Nested.MaxRows
			if nested.MinRows <= previous.Nested.MinRows {
				nested.MinRows = previous.Nested.MinRows
			}
			if nested.MaxRows == 0 || previous.Nested.MaxRows > 0 && nested.MaxRows >= previous.Nested.MaxRows {
				nested.MaxRows = previous.Nested.MaxRows
			}
			comparison.Nested = &nested
		}
		if previous.Blocks != nil && current.Blocks != nil {
			if err := e.types(current.Path.String(), previous.Blocks.ResolvedTypes(), current.Blocks.ResolvedTypes()); err != nil {
				return err
			}
			blocks := *comparison.Blocks
			blocks.Types = previous.Blocks.ResolvedTypes()
			if blocks.MinRows <= previous.Blocks.MinRows {
				blocks.MinRows = previous.Blocks.MinRows
			}
			if blocks.MaxRows == 0 || previous.Blocks.MaxRows > 0 && blocks.MaxRows >= previous.Blocks.MaxRows {
				blocks.MaxRows = previous.Blocks.MaxRows
			}
			comparison.Blocks = &blocks
		}
		comparison = normalizeEmbeddedConstraints(previous, comparison)
		var err error
		comparison, err = embedded.CompareEvolution(previous, comparison, e.types)
		if err != nil {
			return err
		}
		// Collection slug renames rewrite payloads through the ordinary reference
		// migration step. Compare their stable targets, not their current spelling.
		previous = e.referenceIdentity(previous, true)
		comparison = e.referenceIdentity(comparison, false)
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("embedded child %q at %q changed its stored contract", current.Name, path)
		}
	}
	for _, added := range after {
		_, missingInvalid := embeddedMissingValue(added)
		if _, exists := byID[added.ID]; exists && (added.Required || missingInvalid) && added.Category != schema.FieldCategoryPresentation {
			return fmt.Errorf("embedded child %q at %q requires existing values", added.Name, path)
		}
	}
	return nil
}

func (e postgresEmbeddedEvolution) referenceIdentity(field schema.Field, before bool) schema.Field {
	if field.Relationship != nil {
		relation := *field.Relationship
		relation.CollectionSlug = ""
		relation.OptionFilters = nil // Picker metadata does not change stored references.
		if before {
			relation.CollectionID = e.owners.collection(relation.CollectionID)
		}
		relation.Targets = append([]schema.RelationshipTarget(nil), relation.Targets...)
		for i := range relation.Targets {
			relation.Targets[i].CollectionSlug = ""
			if before {
				relation.Targets[i].CollectionID = e.owners.collection(relation.Targets[i].CollectionID)
			}
		}
		field.Relationship = &relation
	}
	if field.Upload != nil {
		upload := *field.Upload
		upload.CollectionSlug = ""
		if before {
			upload.CollectionID = e.owners.collection(upload.CollectionID)
		}
		field.Upload = &upload
	}
	return field
}

// Relaxed constraints and editor metadata cannot invalidate existing payloads.
func normalizeEmbeddedConstraints(before, after schema.Field) schema.Field {
	if before.Text != nil && after.Text != nil {
		text := *after.Text
		text.MinLength = embeddedMinimum(before.Text.MinLength, text.MinLength)
		text.MaxLength = embeddedMaximum(before.Text.MaxLength, text.MaxLength)
		text.Slug = before.Text.Slug
		after.Text = &text
	}
	if before.Textarea != nil && after.Textarea != nil {
		text := *after.Textarea
		text.MinLength = embeddedMinimum(before.Textarea.MinLength, text.MinLength)
		text.MaxLength = embeddedMaximum(before.Textarea.MaxLength, text.MaxLength)
		after.Textarea = &text
	}
	if before.Code != nil && after.Code != nil {
		code := *after.Code
		code.Language = before.Code.Language
		code.MinLength = embeddedMinimum(before.Code.MinLength, code.MinLength)
		code.MaxLength = embeddedMaximum(before.Code.MaxLength, code.MaxLength)
		after.Code = &code
	}
	if before.Number != nil && after.Number != nil {
		number := *after.Number
		number.Step = before.Number.Step
		number.Min = embeddedMinimum(before.Number.Min, number.Min)
		number.Max = embeddedMaximum(before.Number.Max, number.Max)
		after.Number = &number
	}
	if before.Select != nil && after.Select != nil {
		options := map[string]bool{}
		for _, option := range after.Select.Options {
			options[option.Value] = true
		}
		retained := true
		for _, option := range before.Select.Options {
			retained = retained && options[option.Value]
		}
		if retained {
			selection := *after.Select
			selection.Options, selection.DefaultValues = before.Select.Options, before.Select.DefaultValues
			after.Select = &selection
		}
	}
	return after
}

func embeddedMinimum[T int | float64](before, after *T) *T {
	if after == nil || before != nil && *after <= *before {
		return before
	}
	return after
}
func embeddedMaximum[T int | float64](before, after *T) *T {
	if after == nil || before != nil && *after >= *before {
		return before
	}
	return after
}

// Ordinary validation materializes an absent group only when its descendants
// supply defaults. Introducing that materialization can expose a required child
// that previously lived inside an absent optional group. This checks omission
// only; literal defaults remain the ordinary value validator's responsibility.
func embeddedMissingValue(field schema.Field) (defaulted, invalid bool) {
	if field.Category == schema.FieldCategoryPresentation {
		return false, false
	}
	if field.Default != nil || field.Select != nil && field.Select.HasMany && len(field.Select.DefaultValues) != 0 {
		return true, false
	}
	if field.Type == schema.FieldTypeGroup && field.Nested != nil {
		for _, child := range field.Nested.ResolvedFields() {
			childDefault, childInvalid := embeddedMissingValue(child)
			defaulted, invalid = defaulted || childDefault, invalid || childInvalid
		}
		if defaulted {
			return true, invalid
		}
	}
	return false, field.Required || field.List != nil && field.List.MinRows > 0 || field.Type == schema.FieldTypeArray && field.Nested != nil && field.Nested.MinRows > 0
}
