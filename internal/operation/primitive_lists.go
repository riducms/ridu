package operation

import (
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Read formatting can change values and counts, but cannot change the declared
// list shape or expose numbers that the wire protocol cannot represent.
func validatePrimitiveListReadOutput(field schema.Field, value store.Value, path string) error {
	if value.Kind() == store.ValueNull {
		return nil // Requiredness is checked by the shared read-output boundary.
	}
	expected := "strings"
	if field.Type == schema.FieldTypeNumberList {
		expected = "finite numbers"
	}
	fail := func(detail string) error {
		return &Error{Code: "invalid_field_output", Status: 500, Message: fmt.Sprintf("field %q returned invalid %s output after read hooks: %s", path, field.Type, detail)}
	}
	if value.Kind() != store.ValueList {
		return fail("expected an array of " + expected)
	}
	index := -1
	for item := range value.Elements() {
		index++
		if field.Type == schema.FieldTypeTextList {
			if _, ok := item.StringValue(); !ok {
				return fail(fmt.Sprintf("item %d must be a string; null items are not supported", index+1))
			}
		} else if number, ok := item.NumberValue(); !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fail(fmt.Sprintf("item %d must be a finite number; null items are not supported", index+1))
		}
	}
	return nil
}

// Primitive items have no stable public identity. Keep their issues attached to
// the entire field, and identify the offending item in actionable message text.
func validatePrimitiveList(field schema.Field, value store.Value, path string, issues *validationIssueCollector) {
	if value.Kind() != store.ValueList {
		issues.add(schema.Issue{Code: "invalid_type", Path: path, Message: fmt.Sprintf("%s must be an array", field.Admin.Label)})
		return
	}
	if field.Required && value.Len() == 0 {
		issues.add(requiredIssue(field, path))
	}
	if field.List != nil {
		if value.Len() < field.List.MinRows {
			issues.add(schema.Issue{Code: "min_rows", Path: path, Message: fmt.Sprintf("%s must contain at least %d items", field.Admin.Label, field.List.MinRows)})
		}
		if field.List.MaxRows > 0 && value.Len() > field.List.MaxRows {
			issues.add(schema.Issue{Code: "max_rows", Path: path, Message: fmt.Sprintf("%s must contain no more than %d items", field.Admin.Label, field.List.MaxRows)})
		}
	}
	index := -1
	for item := range value.Elements() {
		index++
		if issues.full() {
			return
		}
		label := fmt.Sprintf("%s item %d", field.Admin.Label, index+1)
		add := func(code, message string) {
			issues.add(schema.Issue{Code: code, Path: path, Message: label + " " + message})
		}
		if field.Type == schema.FieldTypeTextList {
			text, ok := item.StringValue()
			if !ok {
				add("invalid_type", "must be a string (null items are not supported)")
				continue
			}
			size := utf8.RuneCountInString(text)
			if field.Text != nil {
				if field.Text.MinLength != nil && size < *field.Text.MinLength {
					add("min_length", fmt.Sprintf("must contain at least %d characters", *field.Text.MinLength))
				}
				if field.Text.MaxLength != nil && size > *field.Text.MaxLength {
					add("max_length", fmt.Sprintf("must contain no more than %d characters", *field.Text.MaxLength))
				}
			}
		} else {
			number, ok := item.NumberValue()
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
				add("invalid_number", "must be a finite number (null items are not supported)")
				continue
			}
			if field.Number != nil {
				if field.Number.Min != nil && number < *field.Number.Min {
					add("min_value", "must be at least "+strconv.FormatFloat(*field.Number.Min, 'g', -1, 64))
				}
				if field.Number.Max != nil && number > *field.Number.Max {
					add("max_value", "must be no more than "+strconv.FormatFloat(*field.Number.Max, 'g', -1, 64))
				}
			}
		}
	}
}
