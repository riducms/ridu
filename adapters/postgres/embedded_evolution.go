package postgres

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
)

// Atlas cannot inspect schema changes inside JSONB. Admit additive embedded
// schemas separately; a compiled transform must validate or repair other changes.
//
// Each surviving resource's own fields are inspected, then each block
// definition its stored roots keep placing, once per definition. A root that
// loses a definition leaves that definition's embedded payloads in retained
// JSON, as removing them would.
func validatePostgresEmbeddedEvolution(before, after schema.Snapshot, mapping referenceShapeMapping, owners atlasIdentityMap) error {
	scopes := newPostgresScopes(before, after, mapping)
	evolution := &postgresEmbeddedEvolution{mapping: mapping, owners: owners, scopes: scopes, compared: make(map[definitionComparison]error)}
	inspected := make(map[blockgraph.Key]error)
	embeddedBelow := make(map[blockgraph.Key]string)
	// holdsEmbedded names an embedded field a definition places ordinarily,
	// at any depth, or returns empty.
	holdsEmbedded := func(keys []blockgraph.Key) string {
		for key := range scopes.before.Closure(keys, false) {
			name, done := embeddedBelow[key]
			if !done {
				fields, _ := scopes.definition(key, true)
				for _, field := range fields {
					if embedded.HasFields(field.field) {
						name = key.Slug + "." + field.field.Path.String()
						break
					}
				}
				embeddedBelow[key] = name
			}
			if name != "" {
				return name
			}
		}
		return ""
	}
	for _, pair := range blockgraph.Survivors(scopes.before, scopes.after, owners.collections) {
		owner := pair.Before.Resource.ID
		storedRoots := make(map[schema.StableID]bool)
		for _, root := range pair.After.Resource.Fields {
			if root.Category != schema.FieldCategoryPresentation {
				storedRoots[root.ID] = true
			}
		}
		var beforeFields []postgresScopeField
		for _, field := range postgresScopeFields(pair.Before.Resource.Fields, owner, mapping, true) {
			// Only top-level stored fields own SQL columns. Missing descendants
			// remain in a surviving JSON column, even when an ancestor is removed.
			if storedRoots[mapping.field(owner, field.root).ID] {
				beforeFields = append(beforeFields, field)
			}
		}
		afterFields := postgresScopeFields(pair.After.Resource.Fields, pair.After.Resource.ID, referenceShapeMapping{}, false)
		if err := evolution.scope(owner, beforeFields, afterFields, false, holdsEmbedded); err != nil {
			return err
		}
		var failure error
		scopes.survivingPlacements(beforeFields, afterFields, func(_ schema.Field, key blockgraph.Key, lost bool) {
			if failure != nil {
				return
			}
			if lost {
				if name := holdsEmbedded([]blockgraph.Key{key}); name != "" {
					failure = fmt.Errorf("PostgreSQL embedded field %q was removed from retained JSON storage; register a compiled data transform to remove or migrate its stored payloads", name)
				}
				return
			}
			err, done := inspected[key]
			if !done {
				beforeDefinition, _ := scopes.definition(key, true)
				afterDefinition, _ := scopes.definition(key, false)
				err = evolution.scope(definitionOwner(key.Slug), beforeDefinition, afterDefinition, key.Localized, holdsEmbedded)
				inspected[key] = err
			}
			failure = err
		})
		if failure != nil {
			return failure
		}
	}
	return nil
}

type postgresEmbeddedEvolution struct {
	owner    schema.StableID
	mapping  referenceShapeMapping
	owners   atlasIdentityMap
	scopes   *postgresScopes
	compared map[definitionComparison]error
}

// scope inspects one scope's own fields: embedded fields must survive in a
// container whose stored layout is unchanged, and their payload schemas may
// only evolve additively. A changed container also changes the layout of the
// embedded fields its definitions place.
func (e *postgresEmbeddedEvolution) scope(owner schema.StableID, before, after []postgresScopeField, localized bool, holdsEmbedded func([]blockgraph.Key) string) error {
	targets := make(map[schema.StableID]schema.Field, len(after))
	for _, field := range after {
		targets[field.identity.ID] = field.field
	}
	// changed records, by before path, the containers above the current field
	// whose stored type or localization changes; fields are in pre-order.
	var changed []string
	for _, previous := range before {
		path := previous.field.Path.String()
		for len(changed) != 0 && !strings.HasPrefix(path, changed[len(changed)-1]+".") {
			changed = changed[:len(changed)-1]
		}
		changedContainer := ""
		if len(changed) != 0 {
			changedContainer = changed[len(changed)-1]
		}
		current, exists := targets[previous.identity.ID]
		if !exists && embedded.HasFields(previous.field) {
			return fmt.Errorf("PostgreSQL embedded field %q was removed from retained JSON storage; register a compiled data transform to remove or migrate its stored payloads", previous.field.Path.String())
		}
		if exists && (embedded.HasFields(previous.field) || embedded.HasFields(current)) {
			if changedContainer != "" {
				return fmt.Errorf("PostgreSQL embedded field %q has a changed JSON container at %q; register a compiled data transform to migrate its stored layout", previous.field.Path.String(), changedContainer)
			}
			compare := *e
			compare.owner = owner
			// The ordinary content rewriter handles confirmed outer-field
			// renames. Names inside embedded payloads still need a transform.
			if _, confirmed := e.mapping.fields[referenceShapeFieldKey(owner, previous.field.ID)]; confirmed {
				current.Name = previous.field.Name
			}
			if err := compare.fields(previous.field.Path.String(), []schema.Field{previous.field}, []schema.Field{current}); err != nil {
				return fmt.Errorf("PostgreSQL embedded schema: %w; register a compiled data transform that validates or repairs existing values", err)
			}
			continue
		}
		container := changedContainer
		if exists && (previous.field.Type != current.Type || previous.field.Localized != current.Localized) {
			container = path
			if previous.field.Nested != nil {
				changed = append(changed, path)
			}
		}
		if container != "" && previous.field.Blocks != nil {
			ordinary, _ := blockgraph.FieldSelections(previous.field, localized || previous.localized)
			if name := holdsEmbedded(ordinary); name != "" {
				return fmt.Errorf("PostgreSQL embedded field %q has a changed JSON container at %q; register a compiled data transform to migrate its stored layout", name, container)
			}
		}
	}
	return nil
}

// types compares an embedded case's variants, each definition once.
func (e *postgresEmbeddedEvolution) types(path string, before, after []schema.BlockType) error {
	byKey := map[string]schema.BlockType{}
	for _, block := range after {
		byKey[block.Slug] = block
	}
	for _, previous := range before {
		current, exists := byKey[previous.Slug]
		if !exists {
			return fmt.Errorf("embedded variant %q at %q was removed", previous.Slug, path)
		}
		// A shared view keeps one field slice, so the pair of slices
		// identifies the compared views.
		beforeFields, afterFields := previous.ResolvedFields(), current.ResolvedFields()
		var key definitionComparison
		if len(beforeFields) != 0 && len(afterFields) != 0 {
			key = definitionComparison{before: &beforeFields[0], after: &afterFields[0]}
		}
		err, done := e.compared[key]
		if !done || key.before == nil {
			compare := *e
			compare.owner = definitionOwner(previous.Slug)
			err = compare.fields(path+"."+previous.Slug, beforeFields, afterFields)
			e.compared[key] = err
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// definitionComparison identifies one compared pair of definition views.
type definitionComparison struct {
	before, after *schema.Field
}

func (e *postgresEmbeddedEvolution) fields(path string, before, after []schema.Field) error {
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
			if err := e.types(current.Path.String(), previous.Blocks.Definitions(), current.Blocks.Definitions()); err != nil {
				return err
			}
			// Variants were compared above; the container keeps the prior
			// selection and compares only its row limits.
			blocks := *previous.Blocks
			if current.Blocks.MinRows > previous.Blocks.MinRows {
				blocks.MinRows = current.Blocks.MinRows
			}
			if !(current.Blocks.MaxRows == 0 || previous.Blocks.MaxRows > 0 && current.Blocks.MaxRows >= previous.Blocks.MaxRows) {
				blocks.MaxRows = current.Blocks.MaxRows
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

func (e *postgresEmbeddedEvolution) referenceIdentity(field schema.Field, before bool) schema.Field {
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
