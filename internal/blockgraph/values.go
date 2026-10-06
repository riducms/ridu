package blockgraph

import (
	"sort"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Walker finds the stored rows of chosen definitions in stored values. It
// follows the schema through groups, arrays and Blocks containers, reading
// each row's blockType to enter its definition view, and enters only the
// containers whose definitions can hold a chosen row, so its cost follows the
// stored values it reads. Plugin values are opaque and never entered.
type Walker struct {
	targets map[string]bool
	within  map[string]bool
	holds   map[*schema.Field]bool
}

// Walker prepares a walk for the rows of the definitions named by targets.
func (graph *Graph) Walker(targets map[string]bool) *Walker {
	return &Walker{targets: targets, within: graph.Containing(targets), holds: make(map[*schema.Field]bool)}
}

// Row is one stored row of a chosen definition.
type Row struct {
	// Block is the definition view the row's placement shares.
	Block schema.BlockType
	// Locale is the translation of a localized ancestor that holds the row,
	// or empty when no ancestor is localized.
	Locale schema.LocaleCode
}

// Holds reports whether stored values of fields can hold a chosen row.
func (walker *Walker) Holds(fields []schema.Field) bool {
	if len(fields) == 0 {
		return false
	}
	if held, known := walker.holds[&fields[0]]; known {
		return held
	}
	held := false
	for _, field := range fields {
		if walker.fieldHolds(field) {
			held = true
			break
		}
	}
	walker.holds[&fields[0]] = held
	return held
}

func (walker *Walker) fieldHolds(field schema.Field) bool {
	if field.Nested != nil && walker.Holds(field.Nested.ResolvedFields()) {
		return true
	}
	if field.Blocks != nil {
		for _, slug := range field.Blocks.BlockReferences {
			if walker.within[slug] {
				return true
			}
		}
	}
	return false
}

// Visit calls visit for every chosen row stored in values of fields, before
// the rows nested in it. locale is the translation values belong to, or empty.
func (walker *Walker) Visit(fields []schema.Field, values func(string) (store.Value, bool), locale schema.LocaleCode, visit func(Row, store.Value) error) error {
	if !walker.Holds(fields) {
		return nil
	}
	for _, field := range fields {
		if !walker.fieldHolds(field) {
			continue
		}
		if value, exists := values(field.Name); exists {
			if err := walker.VisitField(field, value, locale, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

// VisitField calls visit for every chosen row stored in one field's value.
func (walker *Walker) VisitField(field schema.Field, value store.Value, locale schema.LocaleCode, visit func(Row, store.Value) error) error {
	if value.IsZero() || value.Kind() == store.ValueNull || !walker.fieldHolds(field) {
		return nil
	}
	if field.Localized && locale == "" {
		if value.Kind() != store.ValueObject {
			return nil
		}
		field.Localized = false
		for _, code := range sortedKeys(value) {
			if err := walker.VisitField(field, value.Get(code), schema.LocaleCode(code), visit); err != nil {
				return err
			}
		}
		return nil
	}
	switch {
	case field.Blocks != nil:
		if value.Kind() != store.ValueList {
			return nil
		}
		for item := range value.Elements() {
			if item.Kind() != store.ValueObject {
				continue
			}
			slug, _ := item.Get("blockType").StringValue()
			if !walker.within[slug] {
				continue
			}
			view, found := field.Blocks.Definition(slug)
			if !found {
				continue
			}
			if walker.targets[slug] {
				if err := visit(Row{Block: view, Locale: locale}, item); err != nil {
					return err
				}
			}
			if err := walker.Visit(view.ResolvedFields(), item.Lookup, locale, visit); err != nil {
				return err
			}
		}
	case field.Nested != nil && field.Type == schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject {
			return walker.Visit(field.Nested.ResolvedFields(), value.Lookup, locale, visit)
		}
	case field.Nested != nil:
		if value.Kind() != store.ValueList {
			return nil
		}
		for item := range value.Elements() {
			if item.Kind() == store.ValueObject {
				if err := walker.Visit(field.Nested.ResolvedFields(), item.Lookup, locale, visit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Rewrite calls rewrite for every chosen row stored in values of fields,
// before the rows nested in it, which it then finds through the row as
// rewritten. rewrite may change its private copy of the row and reports
// whether it did. Rewrite returns values with every changed row, copying only
// the containers that hold one; values itself is never changed.
func (walker *Walker) Rewrite(fields []schema.Field, values store.Values, locale schema.LocaleCode, rewrite func(Row, store.Values) (bool, error)) (store.Values, bool, error) {
	if !walker.Holds(fields) {
		return values, false, nil
	}
	var result store.Values
	for _, field := range fields {
		if !walker.fieldHolds(field) {
			continue
		}
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		updated, changed, err := walker.RewriteField(field, value, locale, rewrite)
		if err != nil {
			return values, false, err
		}
		if !changed {
			continue
		}
		if result == nil {
			result = make(store.Values, len(values))
			for key, existing := range values {
				result[key] = existing
			}
		}
		result[field.Name] = updated
	}
	if result == nil {
		return values, false, nil
	}
	return result, true, nil
}

// RewriteField is Rewrite for one field's stored value.
func (walker *Walker) RewriteField(field schema.Field, value store.Value, locale schema.LocaleCode, rewrite func(Row, store.Values) (bool, error)) (store.Value, bool, error) {
	if value.IsZero() || value.Kind() == store.ValueNull || !walker.fieldHolds(field) {
		return value, false, nil
	}
	if field.Localized && locale == "" {
		if value.Kind() != store.ValueObject {
			return value, false, nil
		}
		field.Localized = false
		var result store.Values
		for _, code := range sortedKeys(value) {
			updated, changed, err := walker.RewriteField(field, value.Get(code), schema.LocaleCode(code), rewrite)
			if err != nil {
				return value, false, err
			}
			if changed {
				if result == nil {
					result, _ = value.CopyObject()
				}
				result[code] = updated
			}
		}
		if result == nil {
			return value, false, nil
		}
		return store.Object(result), true, nil
	}
	switch {
	case field.Blocks != nil:
		return walker.rewriteRows(value, locale, func(item store.Values) ([]schema.Field, *schema.BlockType) {
			slug, _ := item["blockType"].StringValue()
			if !walker.within[slug] {
				return nil, nil
			}
			view, found := field.Blocks.Definition(slug)
			if !found {
				return nil, nil
			}
			if walker.targets[slug] {
				return view.ResolvedFields(), &view
			}
			return view.ResolvedFields(), nil
		}, rewrite)
	case field.Nested != nil && field.Type == schema.FieldTypeGroup:
		object, valid := value.CopyObject()
		if !valid {
			return value, false, nil
		}
		updated, changed, err := walker.Rewrite(field.Nested.ResolvedFields(), object, locale, rewrite)
		if err != nil || !changed {
			return value, false, err
		}
		return store.Object(updated), true, nil
	case field.Nested != nil:
		children := field.Nested.ResolvedFields()
		return walker.rewriteRows(value, locale, func(store.Values) ([]schema.Field, *schema.BlockType) { return children, nil }, rewrite)
	}
	return value, false, nil
}

// rewriteRows rewrites each object row of a list. row returns the row's fields
// and, for a chosen row, its definition view.
func (walker *Walker) rewriteRows(value store.Value, locale schema.LocaleCode, row func(store.Values) ([]schema.Field, *schema.BlockType), rewrite func(Row, store.Values) (bool, error)) (store.Value, bool, error) {
	items, valid := value.CopyList()
	if !valid {
		return value, false, nil
	}
	changed := false
	for index, item := range items {
		object, valid := item.CopyObject()
		if !valid {
			continue
		}
		fields, chosen := row(object)
		if fields == nil && chosen == nil {
			continue
		}
		rowChanged := false
		if chosen != nil {
			rewritten, err := rewrite(Row{Block: *chosen, Locale: locale}, object)
			if err != nil {
				return value, false, err
			}
			rowChanged = rewritten
		}
		nested, nestedChanged, err := walker.Rewrite(fields, object, locale, rewrite)
		if err != nil {
			return value, false, err
		}
		if rowChanged || nestedChanged {
			items[index] = store.Object(nested)
			changed = true
		}
	}
	if !changed {
		return value, false, nil
	}
	return store.List(items...), true, nil
}

func sortedKeys(value store.Value) []string {
	keys := make([]string, 0, value.Len())
	for key := range value.Entries() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
