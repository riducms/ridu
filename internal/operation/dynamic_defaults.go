package operation

import (
	"fmt"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type defaultResult struct {
	value   store.Value
	present bool
}

// defaultResults belongs to one operation, not its transaction or application.
// Both supplied and empty results survive the later validation checkpoint.
type defaultResults map[string]defaultResult

func initializeDynamicDefaults(collection Collection, ctx Context, values store.Values, options validationOptions, results defaultResults) (store.Values, error) {
	hasDefaults := false
	for _, binding := range collection.Bindings {
		hasDefaults = hasDefaults || binding.Default != nil
	}
	if !hasDefaults {
		return values, nil
	}
	budget := embedded.NewWorkBudget()
	if err := embedded.ValidateValues(collection.Schema.Fields, values, "", false, budget); err != nil {
		return nil, embeddedOperationError(err, false)
	}
	// A literal child can already initialize a Group under the existing rules.
	// Prepare those scopes before discovering callbacks; a dynamic child alone
	// never creates its optional parent. No callback runs during this traversal.
	candidate, err := prepareDefaultGroups(collection.Schema.Fields, values, options)
	if err != nil {
		return nil, embeddedOperationError(err, false)
	}
	// Validation retains a patch at the root but completes submitted objects.
	// Context views include the other exact-locale persisted root values too.
	root := store.CloneValues(options.previous)
	if root == nil {
		root = store.Values{}
	}
	for name, value := range candidate {
		root[name] = value
	}
	ctx.Data, ctx.Document = root, nil
	patch := &runtimePatch{}
	for _, binding := range collection.Bindings {
		if binding.Default == nil {
			continue
		}
		path := binding.Field.Path.String()
		retained := originalFieldLocations(ctx, path, options.previous)
		prior := originalFieldLocations(ctx, path, originalFieldValues(ctx))
		for _, location := range fieldLocationsAtPath(collection.Schema.Fields, candidate, path, false, true) {
			if location.value.Kind() != "" || location.parentPath == "" && !options.requireMissing {
				continue
			}
			if _, exists := retained[location.identity]; exists {
				continue
			}
			scoped := scopedBindingContext(ctx, binding, location, prior)
			result, evaluated := results[scoped.OccurrenceID]
			if !evaluated {
				if err := ctx.Context.Err(); err != nil {
					return nil, hookError("field default", err)
				}
				if err := budget.Enter(location.runtimePath); err != nil {
					return nil, embeddedOperationError(err, false)
				}
				value, present, err := binding.Default(scoped)
				budget.Leave()
				if err != nil {
					return nil, hookError("field default", fmt.Errorf("field %q: %w", location.runtimePath, err))
				}
				if err := ctx.Context.Err(); err != nil {
					return nil, hookError("field default", err)
				}
				result = defaultResult{value: value, present: present}
				results[scoped.OccurrenceID] = result
			}
			if result.present {
				patch.add(location.runtimePath, result.value, false)
			}
		}
	}
	// Every callback at this checkpoint sees the same available candidate. This
	// deliberately does not establish declaration-order dependencies between them.
	patch.apply(candidate)
	return candidate, nil
}

// prepareDefaultGroups preserves the literal validator's existing parent-scope
// initialization. Scalars remain for their normal validation/default checkpoint.
func prepareDefaultGroups(fields []schema.Field, values store.Values, options validationOptions) (store.Values, error) {
	object := validationObject{values: values}
	if err := prepareDefaultMembers(fields, &object, options); err != nil {
		return nil, err
	}
	if object.edited != nil {
		return object.edited, nil
	}
	return store.CloneValues(values), nil
}

func prepareDefaultObject(fields []schema.Field, value store.Value, options validationOptions) (store.Value, bool, error) {
	object := validationObject{value: value}
	if err := prepareDefaultMembers(fields, &object, options); err != nil {
		return value, false, err
	}
	updated, changed := object.result()
	return updated, changed, nil
}

func prepareDefaultMembers(fields []schema.Field, object *validationObject, options validationOptions) error {
	for _, field := range fields {
		value, exists := object.lookup(field.Name)
		if !exists && field.Type == schema.FieldTypeGroup && options.requireMissing && !(field.Localized && options.retained()) {
			value, exists = missingDefaultValue(field)
			if exists {
				object.set(field.Name, value)
			}
		}
		if !exists || value.Kind() == store.ValueNull {
			continue
		}
		path := joinFieldPath(options.prefix, field.Name)
		switch field.Type {
		case schema.FieldTypeGroup:
			if value.Kind() != store.ValueObject || field.Nested == nil {
				continue
			}
			// The preparatory pass has always treated a supplied group as a prior
			// scope even if its previous value was absent or not an object.
			child, changed, err := prepareDefaultObject(field.Nested.ResolvedFields(), value, validationOptions{prefix: path, requireMissing: true, previousValue: options.prior(field.Name), previousScope: true})
			if err != nil {
				return err
			}
			if changed {
				object.set(field.Name, child)
			}
		case schema.FieldTypeArray, schema.FieldTypeBlocks:
			if value.Kind() != store.ValueList {
				continue
			}
			previousRows := validationRowsByKey(options.prior(field.Name))
			var rows []store.Value
			i := -1
			for row := range value.Elements() {
				i++
				if row.Kind() != store.ValueObject {
					continue
				}
				key, _ := row.Get("_key").StringValue()
				previous := previousRows[key]
				var children []schema.Field
				if field.Type == schema.FieldTypeArray && field.Nested != nil {
					children = field.Nested.ResolvedFields()
				} else if field.Blocks != nil {
					kind, _ := row.Get("blockType").StringValue()
					if block := findBlock(field.Blocks.ResolvedTypes(), kind); block != nil {
						children = block.ResolvedFields()
					}
					if previousKind, _ := previous.Get("blockType").StringValue(); previousKind != kind {
						previous = store.Value{}
					}
				}
				updated, changed, err := prepareDefaultObject(children, row, validationOptions{prefix: fmt.Sprintf("%s.%d", path, i), requireMissing: true, previousValue: previous})
				if err != nil {
					return err
				}
				if changed {
					replaceValidationItem(value, &rows, i, updated)
				}
			}
			if rows != nil {
				object.set(field.Name, store.List(rows...))
			}
		case schema.FieldTypePlugin:
			if !embedded.HasFields(field) {
				continue
			}
			previous := validationEmbeddedByIdentity(field, options.prior(field.Name), path, nil)
			changed := false
			updated, err := embedded.TransformValue(field, value, path, nil, func(occurrence embedded.ReadOccurrence) (store.Value, bool, error) {
				child, childChanged, err := prepareDefaultObject(occurrence.Fields, occurrence.Payload, validationOptions{prefix: occurrence.RuntimePath, requireMissing: true, previousValue: previous[occurrence.Identity]})
				changed = changed || childChanged
				return child, childChanged, err
			})
			if err != nil {
				return err
			}
			if changed {
				object.set(field.Name, updated)
			}
		}
	}
	return nil
}
