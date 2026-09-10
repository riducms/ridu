package operation

import (
	"crypto/rand"
	"fmt"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// prepareRowIdentities materializes keys for all declared array, block and
// embedded rows before writes and hooks. Supplied keys survive edits and reorder;
// duplication gives every repeated row a fresh key.
func prepareRowIdentities(fields []schema.Field, values store.Values, canonical, fresh bool) error {
	if err := embedded.ValidateValues(fields, values, "", canonical, nil); err != nil {
		return embeddedOperationError(err, false)
	}
	issues := &validationIssueCollector{}
	budget := embedded.NewBudget()
	var traversalError error
	var walkObject func([]schema.Field, store.Value, string) (store.Value, bool)
	var walkField func(schema.Field, store.Value, string) (store.Value, bool)
	walkObject = func(fields []schema.Field, value store.Value, prefix string) (store.Value, bool) {
		var changed store.Values
		for _, field := range fields {
			if traversalError != nil {
				break
			}
			if !fieldContainsRowIdentities(field) {
				continue
			}
			child, exists := value.Lookup(field.Name)
			if !exists {
				continue
			}
			updated, didChange := walkField(field, child, joinFieldPath(prefix, field.Name))
			if didChange {
				if changed == nil {
					changed, _ = value.CopyObject()
				}
				changed[field.Name] = updated
			}
		}
		if changed != nil {
			return store.Object(changed), true
		}
		return value, false
	}
	walkField = func(field schema.Field, value store.Value, path string) (store.Value, bool) {
		if canonical && field.Localized {
			if value.Kind() != store.ValueObject {
				return value, false
			}
			var changed store.Values
			field.Localized = false
			for code, localized := range value.Entries() {
				updated, didChange := walkField(field, localized, joinFieldPath(path, code))
				if didChange {
					if changed == nil {
						changed, _ = value.CopyObject()
					}
					changed[code] = updated
				}
			}
			if changed != nil {
				return store.Object(changed), true
			}
			return value, false
		}
		switch field.Type {
		case schema.FieldTypePlugin:
			keys := map[string]bool{}
			if err := embedded.Visit(field, value, path, nil, func(o embedded.ReadOccurrence) error {
				keys[o.Key] = true
				return nil
			}); err != nil {
				traversalError = err
				return value, false
			}
			changed := false
			updated, err := embedded.TransformValue(field, value, path, budget, func(o embedded.ReadOccurrence) (store.Value, bool, error) {
				key := ""
				if o.Key == "" || fresh {
					key = "ridu_" + rand.Text()
					for keys[key] {
						key = "ridu_" + rand.Text()
					}
					keys[key] = true
				}
				payload, didChange := walkObject(o.Fields, o.Payload, o.RuntimePath)
				if key != "" {
					object, _ := payload.CopyObject()
					object[o.Case.Identity] = store.String(key)
					payload, didChange = store.Object(object), true
				}
				changed = changed || didChange
				return payload, didChange, traversalError
			})
			if err != nil {
				traversalError = err
				return value, false
			}
			return updated, changed
		case schema.FieldTypeGroup:
			if value.Kind() == store.ValueObject && field.Nested != nil {
				return walkObject(field.Nested.ResolvedFields(), value, path)
			}
		case schema.FieldTypeArray, schema.FieldTypeBlocks:
			if value.Kind() != store.ValueList {
				return value, false
			}
			seen := make(map[string]int, value.Len())
			index := -1
			if !fresh {
				for row := range value.Elements() {
					index++
					if key, exists := row.Lookup("_key"); exists {
						issues.add(validateRowKey(store.Values{"_key": key}, store.Values{}, seen, fmt.Sprintf("%s.%d", path, index), index)...)
					}
				}
			}
			var rows []store.Value
			index = -1
			for row := range value.Elements() {
				index++
				// Preserve preparation's existing handling of non-object rows;
				// downstream validation still decides whether the shape is valid.
				if row.Kind() != store.ValueObject {
					row = store.Object(nil)
				}
				key := ""
				if _, supplied := row.Lookup("_key"); !supplied || fresh {
					key = "ridu_" + rand.Text()
					for {
						if _, exists := seen[key]; !exists {
							break
						}
						key = "ridu_" + rand.Text()
					}
					seen[key] = index
				}
				var children []schema.Field
				if field.Type == schema.FieldTypeArray && field.Nested != nil {
					children = field.Nested.ResolvedFields()
				}
				if field.Type == schema.FieldTypeBlocks && field.Blocks != nil {
					kind, _ := row.Get("blockType").StringValue()
					if block := findBlock(field.Blocks.ResolvedTypes(), kind); block != nil {
						children = block.ResolvedFields()
					}
				}
				updated, didChange := walkObject(children, row, fmt.Sprintf("%s.%d", path, index))
				if key != "" {
					object, _ := updated.CopyObject()
					object["_key"] = store.String(key)
					updated, didChange = store.Object(object), true
				}
				if didChange {
					if rows == nil {
						rows, _ = value.CopyList()
					}
					rows[index] = updated
				}
			}
			if rows != nil {
				return store.List(rows...), true
			}
		}
		return value, false
	}
	for _, field := range fields {
		if traversalError != nil {
			break
		}
		if !fieldContainsRowIdentities(field) {
			continue
		}
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		if updated, changed := walkField(field, value, field.Name); changed {
			values[field.Name] = updated
		}
	}
	if traversalError != nil {
		return embeddedOperationError(traversalError, false)
	}
	if len(issues.values) != 0 {
		return &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: issues.values}
	}
	return nil
}

// An existing occurrence cannot silently change schema: callers replace it with
// a fresh key so deleted-field access and new-field validation remain explicit.
func validateBlockTypeIdentity(fields []schema.Field, before, after store.Values, allLocales bool) error {
	var issues []schema.Issue
	var walk func([]schema.Field)
	walk = func(children []schema.Field) {
		for _, field := range children {
			if embedded.HasFields(field) {
				old := map[string]string{}
				for _, location := range fieldLocationsAtPath(fields, before, field.Path.String(), allLocales) {
					_ = embedded.Visit(field, location.value, location.runtimePath, nil, func(occurrence embedded.ReadOccurrence) error {
						old[location.identity+"/"+occurrence.Tree.Key+"/"+occurrence.Key] = occurrence.Case.TagValue + "/" + occurrence.Type.Slug
						return nil
					})
				}
				for _, location := range fieldLocationsAtPath(fields, after, field.Path.String(), allLocales) {
					_ = embedded.Visit(field, location.value, location.runtimePath, nil, func(occurrence embedded.ReadOccurrence) error {
						if kind, exists := old[location.identity+"/"+occurrence.Tree.Key+"/"+occurrence.Key]; exists && kind != occurrence.Case.TagValue+"/"+occurrence.Type.Slug {
							issues = append(issues, schema.Issue{Code: "block_type_identity", Path: occurrence.RuntimePath + "." + occurrence.Case.Discriminator, Message: "changing an embedded schema requires a new occurrence identity"})
						}
						return nil
					})
				}
			}
			if field.Type == schema.FieldTypeBlocks && field.Blocks != nil {
				old := map[string]map[string]string{}
				for _, location := range fieldLocationsAtPath(fields, before, field.Path.String(), allLocales) {
					types := map[string]string{}
					for row := range location.value.Elements() {
						key, _ := row.Get("_key").StringValue()
						kind, _ := row.Get("blockType").StringValue()
						if key != "" {
							types[key] = kind
						}
					}
					old[location.identity] = types
				}
				for _, location := range fieldLocationsAtPath(fields, after, field.Path.String(), allLocales) {
					index := -1
					for row := range location.value.Elements() {
						index++
						key, _ := row.Get("_key").StringValue()
						kind, _ := row.Get("blockType").StringValue()
						if previous, found := old[location.identity][key]; found && previous != kind {
							issues = append(issues, schema.Issue{Code: "block_type_identity", Path: fmt.Sprintf("%s.%d.blockType", location.runtimePath, index), Message: "changing a block type requires a new row key"})
						}
					}
				}
				for _, block := range field.Blocks.ResolvedTypes() {
					walk(block.ResolvedFields())
				}
			}
			embedded.SchemaFields(field, walk)
			if field.Nested != nil {
				walk(field.Nested.ResolvedFields())
			}
		}
	}
	walk(fields)
	if len(issues) > 0 {
		return &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: issues}
	}
	return nil
}

// Only pre-write hooks may add input occurrences. Post-write/commit hooks must
// never introduce identity work or new validation failures after persistence.
func runIdentityHooks(hooks []Hook, ctx Context) error {
	for _, hook := range hooks {
		if err := runHooks([]Hook{hook}, ctx); err != nil {
			return err
		}
		if changesDocument(ctx.Operation) {
			if err := prepareRowIdentities(ctx.Collection.Fields, ctx.Data, false, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func fieldContainsRowIdentities(field schema.Field) bool {
	if embedded.HasFields(field) {
		return true
	}
	if field.Type == schema.FieldTypeBlocks || field.Type == schema.FieldTypeArray {
		return true
	}
	if field.Nested != nil {
		for _, child := range field.Nested.ResolvedFields() {
			if fieldContainsRowIdentities(child) {
				return true
			}
		}
	}
	return false
}
