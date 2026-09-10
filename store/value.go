package store

import (
	"encoding/json"
	"fmt"
	"iter"
	"math"
	"reflect"

	"github.com/riducms/ridu/schema"
)

type ValueKind string

const (
	ValueNull     ValueKind = "null"
	ValueString   ValueKind = "string"
	ValueObject   ValueKind = "object"
	ValueDocument ValueKind = "document"
	ValueNumber   ValueKind = "number"
	ValueBoolean  ValueKind = "boolean"
	ValueList     ValueKind = "list"
)

// Value is the finite application-value vocabulary. Its private backing data is
// immutable. Lookup, Get, Entries and Elements read shared child values without
// copying their containers. Constructors detach mutable inputs; CopyObject,
// CopyList and CopyDocument explicitly produce detached mutable containers for
// editing or interoperability. WithListItem replaces one item while sharing
// unchanged list branches. All these operations preserve retained values.
type Value struct {
	kind     ValueKind
	text     string
	object   Values
	document *Document
	number   float64
	boolean  bool
	list     *valueList
}

func Null() Value               { return Value{kind: ValueNull} }
func String(value string) Value { return Value{kind: ValueString, text: value} }
func Object(value Values) Value { return Value{kind: ValueObject, object: CloneValues(value)} }
func Populated(document Document) Value {
	cloned := CloneDocument(document)
	return Value{kind: ValueDocument, document: &cloned}
}
func Number(value float64) Value { return Value{kind: ValueNumber, number: value} }
func Boolean(value bool) Value   { return Value{kind: ValueBoolean, boolean: value} }
func List(values ...Value) Value { return Value{kind: ValueList, list: newValueList(values)} }

func (value Value) Kind() ValueKind { return value.kind }

// SameBacking reports whether values share immutable container backing or have
// exactly equal scalar values. It does not compare container contents: separately
// constructed nonempty containers can return false even when their values match.
// This is useful for bounded caches that retain their source Values. It exposes
// no backing address, and numeric comparisons distinguish positive and negative zero.
func (value Value) SameBacking(other Value) bool {
	if value.kind != other.kind {
		return false
	}
	switch value.kind {
	case ValueNull:
		return true
	case ValueString:
		return value.text == other.text
	case ValueNumber:
		return math.Float64bits(value.number) == math.Float64bits(other.number)
	case ValueBoolean:
		return value.boolean == other.boolean
	case ValueObject:
		// reflect.Value.Pointer supports maps. Compare the live map handles only;
		// never retain uintptr addresses as cache keys or reconstruct pointers.
		return reflect.ValueOf(value.object).Pointer() == reflect.ValueOf(other.object).Pointer()
	case ValueList:
		return value.list == other.list
	case ValueDocument:
		return value.document == other.document
	default:
		return false
	}
}

func (value Value) StringValue() (string, bool) {
	return value.text, value.kind == ValueString
}

// CopyObject returns a detached mutable map and reports whether value is an
// object. Its child Values remain immutable. Use Lookup/Get or Entries to read
// an object without copying it.
func (value Value) CopyObject() (Values, bool) {
	return CloneValues(value.object), value.kind == ValueObject
}

// CopyDocument returns a detached populated document, including its mutable
// metadata and field containers. Child Values remain immutable.
func (value Value) CopyDocument() (Document, bool) {
	if value.kind != ValueDocument || value.document == nil {
		return Document{}, false
	}
	return CloneDocument(*value.document), true
}
func (value Value) NumberValue() (float64, bool) { return value.number, value.kind == ValueNumber }
func (value Value) BooleanValue() (bool, bool)   { return value.boolean, value.kind == ValueBoolean }

// CopyList returns a detached mutable slice and reports whether value is a
// list. Use Elements or ListItem for reads without materializing a slice.
func (value Value) CopyList() ([]Value, bool) {
	items := make([]Value, value.list.len())
	value.list.copyTo(items)
	return items, value.kind == ValueList
}

// Lookup reads a direct object member without copying its container. It returns
// false for a missing member or a non-object value. Explicit Null members are
// present. Names are literal keys, not dotted paths.
func (value Value) Lookup(name string) (Value, bool) {
	if value.kind != ValueObject {
		return Value{}, false
	}
	child, exists := value.object[name]
	return child, exists
}

// Get reads a direct object member, returning Null for a missing member or a
// non-object value. Use Lookup when membership matters.
func (value Value) Get(name string) Value {
	if child, exists := value.Lookup(name); exists {
		return child
	}
	return Null()
}

// Len returns the number of object members or list elements, or zero for other
// kinds. Use Kind when an empty container must be distinguished from a scalar.
func (value Value) Len() int {
	switch value.kind {
	case ValueObject:
		return len(value.object)
	case ValueList:
		return value.list.len()
	default:
		return 0
	}
}

// Entries iterates immutable direct object members without copying the map.
// Order is unspecified. Non-object values produce no entries. The iterator
// retains this snapshot and may be used again, including after an early stop.
func (value Value) Entries() iter.Seq2[string, Value] {
	return func(yield func(string, Value) bool) {
		if value.kind != ValueObject {
			return
		}
		for name, child := range value.object {
			if !yield(name, child) {
				return
			}
		}
	}
}

// Elements iterates immutable list elements in order without creating a slice.
// Non-list values produce no elements. The iterator retains this snapshot and
// may be used again, including after an early stop.
func (value Value) Elements() iter.Seq[Value] {
	return func(yield func(Value) bool) {
		if value.kind == ValueList {
			value.list.visit(yield)
		}
	}
}

// ListItem returns one immutable list item without copying the enclosing list.
// It returns false for a non-list value or an out-of-range index.
func (value Value) ListItem(index int) (Value, bool) {
	if value.kind != ValueList || index < 0 || index >= value.list.len() {
		return Value{}, false
	}
	return value.list.item(index, valueListShift(value.list.len())), true
}

// WithListItem returns a list with one item replaced, sharing unchanged immutable
// items with the original. It returns the original value and false for a non-list
// value or an out-of-range index. Existing values and retained snapshots are never
// mutated; use List to change a list's length or order.
func (value Value) WithListItem(index int, replacement Value) (Value, bool) {
	if value.kind != ValueList || index < 0 || index >= value.list.len() {
		return value, false
	}
	value.list = value.list.withItem(index, replacement, valueListShift(value.list.len()))
	return value, true
}

func CloneValues(values Values) Values {
	if values == nil {
		return Values{}
	}
	cloned := make(Values, len(values))
	for name, value := range values {
		cloned[name] = value
	}
	return cloned
}

func CloneDocument(document Document) Document {
	cloned := document
	if document.DeletedAt != nil {
		deletedAt := *document.DeletedAt
		cloned.DeletedAt = &deletedAt
	}
	cloned.Values = CloneValues(document.Values)
	if document.LocalizationSources != nil {
		cloned.LocalizationSources = make(map[string]schema.LocaleCode, len(document.LocalizationSources))
		for path, locale := range document.LocalizationSources {
			cloned.LocalizationSources[path] = locale
		}
	}
	return cloned
}

func (value Value) MarshalJSON() ([]byte, error) {
	switch value.kind {
	case ValueNull:
		return []byte("null"), nil
	case ValueString:
		return json.Marshal(value.text)
	case ValueObject:
		return json.Marshal(value.object)
	case ValueDocument:
		if value.document == nil {
			return []byte("null"), nil
		}
		return json.Marshal(documentMap(*value.document))
	case ValueNumber:
		return json.Marshal(value.number)
	case ValueBoolean:
		return json.Marshal(value.boolean)
	case ValueList:
		return value.list.marshalJSON()
	default:
		return nil, fmt.Errorf("unknown store value kind %q", value.kind)
	}
}

func documentMap(document Document) map[string]any {
	result := map[string]any{
		"id": document.ID, "createdAt": document.CreatedAt, "updatedAt": document.UpdatedAt,
	}
	if document.Status != "" {
		result["_status"] = document.Status
		result["_revision"] = document.Revision
	}
	for name, value := range document.Values {
		result[name] = value
	}
	return result
}

func (value *Value) UnmarshalJSON(encoded []byte) error {
	if string(encoded) == "null" {
		*value = Null()
		return nil
	}
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		*value = String(text)
		return nil
	}
	var boolean bool
	if err := json.Unmarshal(encoded, &boolean); err == nil {
		*value = Boolean(boolean)
		return nil
	}
	var number float64
	if err := json.Unmarshal(encoded, &number); err == nil {
		*value = Number(number)
		return nil
	}
	var list []Value
	if err := json.Unmarshal(encoded, &list); err == nil && list != nil {
		*value = List(list...)
		return nil
	}
	var object Values
	if err := json.Unmarshal(encoded, &object); err == nil && object != nil {
		*value = Object(object)
		return nil
	}
	return fmt.Errorf("value must be JSON data")
}

func cloneValueList(values []Value) []Value {
	cloned := make([]Value, len(values))
	copy(cloned, values)
	return cloned
}
