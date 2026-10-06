package operation

import (
	"crypto/rand"
	"fmt"
	"strconv"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// prepareRowIdentities materializes keys for all declared array, block and
// embedded rows before writes and hooks. Supplied keys survive edits and reorder;
// duplication gives every repeated row a fresh key. It reports whether it
// changed values.
func prepareRowIdentities(fields []schema.Field, values store.Values, canonical, fresh bool) (bool, error) {
	if err := embedded.ValidateValues(fields, values, "", canonical, nil); err != nil {
		return false, embeddedOperationError(err, false)
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
					if block := findBlock(field.Blocks.Definitions(), kind); block != nil {
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
	prepared := false
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
			prepared = true
		}
	}
	if traversalError != nil {
		return false, embeddedOperationError(traversalError, false)
	}
	if len(issues.values) != 0 {
		return false, &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: issues.values}
	}
	return prepared, nil
}

// An existing occurrence cannot silently change schema: callers replace it with
// a fresh key so deleted-field access and new-field validation remain explicit.
// One walk visits the before and after values together, pairing rows and
// embedded occurrences by identity and consulting each row's block definition,
// so the work follows the documents rather than the schema's placements. Both
// value maps hold one exact-locale view.
func validateBlockTypeIdentity(fields []schema.Field, beforeValues, afterValues store.Values) error {
	var issues []schema.Issue
	var object func([]schema.Field, store.Value, store.Value, string)
	var member func(schema.Field, store.Value, store.Value, string)
	object = func(fields []schema.Field, before, after store.Value, prefix string) {
		for _, field := range fields {
			if !fieldContainsRowIdentities(field) {
				continue
			}
			previous, existed := before.Lookup(field.Name)
			next, exists := after.Lookup(field.Name)
			if existed && exists && previous.Kind() != store.ValueNull && next.Kind() != store.ValueNull {
				member(field, previous, next, joinFieldPath(prefix, field.Name))
			}
		}
	}
	member = func(field schema.Field, before, after store.Value, path string) {
		if embedded.HasFields(field) {
			// The identity namespace is this occurrence of the plugin field.
			old := map[string]embedded.ReadOccurrence{}
			_ = embedded.Visit(field, before, path, nil, func(occurrence embedded.ReadOccurrence) error {
				old[occurrence.Tree.Key+"/"+occurrence.Key] = occurrence
				return nil
			})
			_ = embedded.Visit(field, after, path, nil, func(occurrence embedded.ReadOccurrence) error {
				prior, exists := old[occurrence.Tree.Key+"/"+occurrence.Key]
				switch {
				case !exists:
				case prior.Case.TagValue != occurrence.Case.TagValue || prior.Type.Slug != occurrence.Type.Slug:
					issues = append(issues, schema.Issue{Code: "block_type_identity", Path: occurrence.RuntimePath + "." + occurrence.Case.Discriminator, Message: "changing an embedded schema requires a new occurrence identity"})
				default:
					object(occurrence.Fields, prior.Payload, occurrence.Payload, occurrence.RuntimePath)
				}
				return nil
			})
			return
		}
		switch field.Type {
		case schema.FieldTypeGroup:
			if field.Nested != nil && before.Kind() == store.ValueObject && after.Kind() == store.ValueObject {
				object(field.Nested.ResolvedFields(), before, after, path)
			}
		case schema.FieldTypeArray:
			if field.Nested == nil {
				return
			}
			children := field.Nested.ResolvedFields()
			prior := identifiedRows(before, nil)
			for row := range identifiedRowsInOrder(after, nil) {
				if previous, exists := prior[row.identity]; exists {
					object(children, previous.value, row.value, path+"."+strconv.Itoa(row.index))
				}
			}
		case schema.FieldTypeBlocks:
			if field.Blocks == nil {
				return
			}
			kinds := map[string]string{}
			for row := range before.Elements() {
				key, _ := row.Get("_key").StringValue()
				kind, _ := row.Get("blockType").StringValue()
				if key != "" {
					kinds[key] = kind
				}
			}
			index := -1
			for row := range after.Elements() {
				index++
				key, _ := row.Get("_key").StringValue()
				kind, _ := row.Get("blockType").StringValue()
				if previous, found := kinds[key]; found && previous != kind {
					issues = append(issues, schema.Issue{Code: "block_type_identity", Path: fmt.Sprintf("%s.%d.blockType", path, index), Message: "changing a block type requires a new row key"})
				}
			}
			// Rows keep their nested occurrences only while their type is unchanged.
			prior := identifiedRows(before, field.Blocks)
			for row := range identifiedRowsInOrder(after, field.Blocks) {
				previous, exists := prior[row.identity]
				if !exists || previous.kind != row.kind {
					continue
				}
				if block, found := field.Blocks.Definition(row.kind); found {
					object(block.ResolvedFields(), previous.value, row.value, path+"."+strconv.Itoa(row.index))
				}
			}
		}
	}
	object(fields, store.Object(beforeValues), store.Object(afterValues), "")
	if len(issues) > 0 {
		return &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: issues}
	}
	return nil
}

// identifiedRow is an object row with the identity collectFieldLocations gives
// it: its key and occurrence number, or its index when keyless.
type identifiedRow struct {
	identity string
	kind     string
	index    int
	value    store.Value
}

// identifiedRowsInOrder yields a list's object rows with their identities.
// With blocks, only rows whose type the container defines take part.
func identifiedRowsInOrder(list store.Value, blocks *schema.BlocksField) func(func(identifiedRow) bool) {
	return func(yield func(identifiedRow) bool) {
		occurrences := map[string]int{}
		index := -1
		for row := range list.Elements() {
			index++
			if row.Kind() != store.ValueObject {
				continue
			}
			kind := ""
			if blocks != nil {
				var valid bool
				if kind, valid = row.Get("blockType").StringValue(); !valid {
					continue
				}
				if _, known := blocks.Definition(kind); !known {
					continue
				}
			}
			identity := "#" + strconv.Itoa(index)
			if key, valid := row.Get("_key").StringValue(); valid && key != "" {
				identity = keyedRowIdentity(key, occurrences[key])
				occurrences[key]++
			}
			if !yield(identifiedRow{identity: identity, kind: kind, index: index, value: row}) {
				return
			}
		}
	}
}

func identifiedRows(list store.Value, blocks *schema.BlocksField) map[string]identifiedRow {
	rows := map[string]identifiedRow{}
	for row := range identifiedRowsInOrder(list, blocks) {
		rows[row.identity] = row
	}
	return rows
}

// Only pre-write hooks may add input occurrences. Post-write/commit hooks must
// never introduce identity work or new validation failures after persistence.
func runIdentityHooks(hooks []Hook, ctx Context) error {
	for _, hook := range hooks {
		if err := runHooks([]Hook{hook}, ctx); err != nil {
			return err
		}
		if changesDocument(ctx.Operation) {
			if _, err := prepareRowIdentities(ctx.Collection.Fields, ctx.Data, false, false); err != nil {
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
