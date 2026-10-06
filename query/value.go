package query

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"
)

// ValueKind identifies the concrete scalar, reference, or list represented by
// a Value.
type ValueKind string

const (
	ValueString  ValueKind = "string"
	ValueNumber  ValueKind = "number"
	ValueBoolean ValueKind = "boolean"
	ValueNull    ValueKind = "null"
	ValueList    ValueKind = "list"
	// ValueReference names one document among a polymorphic relationship's
	// target collections, in the relationship's {relationTo, id} value shape.
	ValueReference ValueKind = "reference"
)

// Value is an immutable query operand. Explicit constructors prevent arbitrary
// application values from leaking into the public query contract.
type Value struct {
	kind    ValueKind
	text    string
	number  float64
	boolean bool
	items   []Value
	// relationTo is a reference's collection slug; text holds its document ID.
	relationTo string
}

// Operand is a value compared with a field: a string, boolean, integer, or
// float, including named types such as a string enum, or a Value built with
// String, Number, Boolean, Null, Reference, or List.
type Operand interface {
	~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64 | Value
}

// Ordered is an Operand that greater-than and less-than comparisons accept:
// strings and numbers, or a Value holding one.
type Ordered interface {
	~string | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64 | Value
}

// maxExactInteger is the largest integer a float64 Number holds exactly.
const maxExactInteger = 1 << 53

// valueOf converts an Operand. Integers beyond ±2^53 and non-finite floats
// panic because a Number could not represent them exactly.
func valueOf[V Operand](value V) Value {
	switch typed := any(value).(type) {
	case Value:
		return typed
	case string:
		return String(typed)
	case bool:
		return Boolean(typed)
	case int:
		return integerValue(int64(typed))
	case int64:
		return integerValue(typed)
	case float64:
		return floatValue(typed)
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.String:
		return String(reflected.String())
	case reflect.Bool:
		return Boolean(reflected.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return integerValue(reflected.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		unsigned := reflected.Uint()
		if unsigned > maxExactInteger {
			panic(fmt.Errorf("query value %d is larger than 2^53; pass it as a string", unsigned))
		}
		return Number(float64(unsigned))
	case reflect.Float32, reflect.Float64:
		return floatValue(reflected.Float())
	}
	panic(fmt.Errorf("unsupported query value type %T", value))
}

func integerValue(value int64) Value {
	if value > maxExactInteger || value < -maxExactInteger {
		panic(fmt.Errorf("query value %d is outside ±2^53; pass it as a string", value))
	}
	return Number(float64(value))
}

func floatValue(value float64) Value {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic(fmt.Errorf("query value %v is not a finite number", value))
	}
	return Number(value)
}

// String constructs a string operand.
func String(value string) Value { return Value{kind: ValueString, text: value} }

// Number constructs a number operand.
func Number(value float64) Value { return Value{kind: ValueNumber, number: value} }

// Boolean constructs a boolean operand.
func Boolean(value bool) Value { return Value{kind: ValueBoolean, boolean: value} }

// Null constructs a null operand.
func Null() Value { return Value{kind: ValueNull} }

// Reference constructs a polymorphic relationship candidate: document id in
// collection relationTo. It equals a stored {relationTo, id} reference, as in
// In("subjects", Reference("posts", postID)).
func Reference(relationTo, id string) Value {
	return Value{kind: ValueReference, relationTo: relationTo, text: id}
}

// List constructs a copied list operand.
func List(values ...Value) Value {
	return Value{kind: ValueList, items: cloneValues(values)}
}

// dateTimeLayout matches the admin's saved date-and-time values: UTC with
// milliseconds, such as 2026-09-29T14:05:00.000Z.
const dateTimeLayout = "2006-01-02T15:04:05.000Z"

// DateTime converts t to the text a date-and-time field stores, so it can be
// compared with that field or with createdAt and updatedAt, as in
// GreaterThan("publishedAt", DateTime(time.Now())). Date-only fields store
// "2006-01-02"; compare them with t.Format(time.DateOnly).
func DateTime(t time.Time) Value {
	return String(t.UTC().Format(dateTimeLayout))
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

// ReferenceValue returns the collection slug, document ID, and true when this
// is a reference operand.
func (value Value) ReferenceValue() (relationTo, id string, ok bool) {
	if value.kind != ValueReference {
		return "", "", false
	}
	return value.relationTo, value.text, true
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
	case ValueReference:
		return json.Marshal(struct {
			RelationTo string `json:"relationTo"`
			ID         string `json:"id"`
		}{value.relationTo, value.text})
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
