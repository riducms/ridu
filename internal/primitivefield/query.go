// Package primitivefield contains the private, shared primitive-list query rules.
// Storage compilation and operation lifecycle remain owned by their subsystems.
package primitivefield

import (
	"fmt"
	"math"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func IsList(field schema.Field) bool {
	return field.Type == schema.FieldTypeTextList || field.Type == schema.FieldTypeNumberList
}

// UnsupportedQueryError identifies a caller's primitive-list path rejected by
// an adapter's existing query boundary. It is private to framework packages;
// schema/access configuration failures are not marked as caller input errors.
type UnsupportedQueryError struct {
	Path  query.Path
	Cause error
}

func (err *UnsupportedQueryError) Error() string {
	return fmt.Sprintf("primitive list path %q is not supported by this adapter: %v", err.Path.String(), err.Cause)
}

func (err *UnsupportedQueryError) Unwrap() error { return err.Cause }

// UnsupportedPath must only wrap a path-resolution failure for caller input.
// Other field kinds retain their existing adapter error contract.
func UnsupportedPath(fields []schema.Field, path query.Path, cause error) error {
	if field, found := population.FieldAtPath(fields, path); cause != nil && found && IsList(field) {
		return &UnsupportedQueryError{Path: path, Cause: cause}
	}
	return cause
}

func ValidateComparison(field schema.Field, comparison query.Comparison) error {
	if !IsList(field) {
		return nil
	}
	switch comparison.Operator {
	case query.OperatorExists:
		if comparison.Value.Kind() == query.ValueBoolean {
			return nil
		}
	case query.OperatorEqual, query.OperatorNotEqual:
		if comparison.Value.Kind() == query.ValueNull {
			return nil
		}
	case query.OperatorIn:
		if comparison.Value.Kind() != query.ValueList {
			break
		}
		kind := query.ValueString
		if field.Type == schema.FieldTypeNumberList {
			kind = query.ValueNumber
		}
		for _, value := range comparison.Value.Values() {
			if value.Kind() != kind {
				return fmt.Errorf("primitive list %q membership requires %s operands; null is not an item", comparison.Path.String(), kind)
			}
			if number, ok := value.NumberValue(); ok && (math.IsNaN(number) || math.IsInf(number, 0)) {
				return fmt.Errorf("primitive list %q membership requires finite numbers", comparison.Path.String())
			}
		}
		return nil
	}
	return fmt.Errorf("primitive list %q does not support %q with this operand; use In for item membership, Exists, or Equal/NotEqual with null", comparison.Path.String(), comparison.Operator)
}

func ValidateNode(fields []schema.Field, node *query.Node) error {
	if node == nil {
		return nil
	}
	if node.Comparison != nil {
		if field, ok := population.FieldAtPath(fields, node.Comparison.Path); ok {
			if err := ValidateComparison(field, *node.Comparison); err != nil {
				return err
			}
		}
	}
	for _, child := range node.Children {
		if err := ValidateNode(fields, &child); err != nil {
			return err
		}
	}
	return nil
}

func ValidateRequest(request store.Request) error {
	if err := ValidateNode(request.Collection.Fields, request.Filter); err != nil {
		return err
	}
	if err := ValidateNode(request.Collection.Fields, request.Access); err != nil {
		return err
	}
	for _, sort := range request.Sort {
		if field, ok := population.FieldAtPath(request.Collection.Fields, sort.Path); ok && IsList(field) {
			return fmt.Errorf("primitive list %q cannot be sorted; choose a singular scalar field", sort.Path.String())
		}
	}
	return nil
}

// Membership is strict element equality. It never sorts or deduplicates values.
func Membership(value store.Value, candidates query.Value) bool {
	if value.Kind() != store.ValueList {
		return false
	}
	for item := range value.Elements() {
		for _, candidate := range candidates.Values() {
			switch candidate.Kind() {
			case query.ValueString:
				actual, valid := item.StringValue()
				expected, _ := candidate.StringValue()
				if valid && actual == expected {
					return true
				}
			case query.ValueNumber:
				actual, valid := item.NumberValue()
				expected, _ := candidate.NumberValue()
				if valid && actual == expected {
					return true
				}
			}
		}
	}
	return false
}
