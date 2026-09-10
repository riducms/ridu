package embedded

import (
	"fmt"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ValidateValues preflights declared envelopes before consumers whose ordinary
// traversal assumes admitted values. It does not validate ordinary field values.
// allLocales describes canonical storage, rather than a selected-locale view.
func ValidateValues(fields []schema.Field, values store.Values, prefix string, allLocales bool, budget *Budget) error {
	if budget == nil {
		budget = NewBudget()
	}
	for _, field := range fields {
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		if err := ValidateValue(field, value, join(prefix, field.Name), allLocales, budget); err != nil {
			return err
		}
	}
	return nil
}

// ValidateValue is the field-root counterpart of ValidateValues.
func ValidateValue(field schema.Field, value store.Value, path string, allLocales bool, budget *Budget) error {
	if budget == nil {
		budget = NewBudget()
	}
	if value.Kind() == store.ValueNull {
		return nil
	}
	if err := budget.Enter(path); err != nil {
		return err
	}
	defer budget.Leave()
	if field.Localized && allLocales {
		if value.Kind() != store.ValueObject {
			return nil
		}
		field.Localized = false
		for locale, candidate := range value.Entries() {
			if err := ValidateValue(field, candidate, join(path, locale), allLocales, budget); err != nil {
				return err
			}
		}
		return nil
	}
	if HasFields(field) {
		return Visit(field, value, path, budget, func(o ReadOccurrence) error {
			return validateObject(o.Fields, o.Payload, o.RuntimePath, allLocales, budget)
		})
	}
	switch field.Type {
	case schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject && field.Nested != nil {
			return validateObject(field.Nested.ResolvedFields(), value, path, allLocales, budget)
		}
	case schema.FieldTypeArray, schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList {
			return nil
		}
		i := -1
		for row := range value.Elements() {
			i++
			if row.Kind() != store.ValueObject {
				continue
			}
			var fields []schema.Field
			if field.Type == schema.FieldTypeArray && field.Nested != nil {
				fields = field.Nested.ResolvedFields()
			} else if field.Type == schema.FieldTypeBlocks && field.Blocks != nil {
				kind, _ := row.Get("blockType").StringValue()
				for _, block := range field.Blocks.ResolvedTypes() {
					if block.Slug == kind {
						fields = block.ResolvedFields()
						break
					}
				}
			}
			if err := validateObject(fields, row, fmt.Sprintf("%s.%d", path, i), allLocales, budget); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateObject reads immutable children directly; detached maps are reserved
// for callbacks that accept ownership of an occurrence payload.
func validateObject(fields []schema.Field, object store.Value, prefix string, allLocales bool, budget *Budget) error {
	for _, field := range fields {
		value, exists := object.Lookup(field.Name)
		if exists {
			if err := ValidateValue(field, value, join(prefix, field.Name), allLocales, budget); err != nil {
				return err
			}
		}
	}
	return nil
}
