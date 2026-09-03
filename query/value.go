package query

import (
	"encoding/json"
	"fmt"
)

// ValueKind identifies the concrete scalar or list represented by a Value.
type ValueKind string

const (
	ValueString  ValueKind = "string"
	ValueNumber  ValueKind = "number"
	ValueBoolean ValueKind = "boolean"
	ValueNull    ValueKind = "null"
	ValueList    ValueKind = "list"
)

// Value is an immutable query operand. Explicit constructors prevent arbitrary
// application values from leaking into the public query contract.
type Value struct {
	kind    ValueKind
	text    string
	number  float64
	boolean bool
	items   []Value
}

// String constructs a string operand.
func String(value string) Value { return Value{kind: ValueString, text: value} }

// Number constructs a number operand.
func Number(value float64) Value { return Value{kind: ValueNumber, number: value} }

// Boolean constructs a boolean operand.
func Boolean(value bool) Value { return Value{kind: ValueBoolean, boolean: value} }

// Null constructs a null operand.
func Null() Value { return Value{kind: ValueNull} }

// List constructs a copied list operand.
func List(values ...Value) Value {
	return Value{kind: ValueList, items: cloneValues(values)}
}

// Kind returns the operand's discriminant.
func (value Value) Kind() ValueKind { return value.kind }

// StringValue returns the string and true when this is a string operand.
func (value Value) StringValue() (string, bool) {
	return value.text, value.kind == ValueString
}

// NumberValue returns the number and true when this is a number operand.
func (value Value) NumberValue() (float64, bool) {
	return value.number, value.kind == ValueNumber
}

// BooleanValue returns the boolean and true when this is a boolean operand.
func (value Value) BooleanValue() (bool, bool) {
	return value.boolean, value.kind == ValueBoolean
}

// Values returns a deep copy for a list operand.
func (value Value) Values() []Value {
	return cloneValues(value.items)
}

func (value Value) MarshalJSON() ([]byte, error) {
	switch value.kind {
	case ValueString:
		return json.Marshal(value.text)
	case ValueNumber:
		return json.Marshal(value.number)
	case ValueBoolean:
		return json.Marshal(value.boolean)
	case ValueNull:
		return []byte("null"), nil
	case ValueList:
		return json.Marshal(value.items)
	default:
		return nil, fmt.Errorf("cannot marshal query value with kind %q", value.kind)
	}
}

func cloneValues(values []Value) []Value {
	if values == nil {
		return nil
	}
	cloned := make([]Value, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].items = cloneValues(value.items)
	}
	return cloned
}
