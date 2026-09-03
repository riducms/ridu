package store

import (
	"encoding/json"
	"fmt"

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

// Value is the finite application-value vocabulary supported by the initial
// text/select/relationship/group vertical slice.
type Value struct {
	kind     ValueKind
	text     string
	object   Values
	document *Document
	number   float64
	boolean  bool
	list     []Value
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
func List(values ...Value) Value { return Value{kind: ValueList, list: cloneValueList(values)} }

func (value Value) Kind() ValueKind { return value.kind }
func (value Value) StringValue() (string, bool) {
	return value.text, value.kind == ValueString
}
func (value Value) ObjectValue() (Values, bool) {
	return CloneValues(value.object), value.kind == ValueObject
}
func (value Value) DocumentValue() (Document, bool) {
	if value.kind != ValueDocument || value.document == nil {
		return Document{}, false
	}
	return CloneDocument(*value.document), true
}
func (value Value) NumberValue() (float64, bool) { return value.number, value.kind == ValueNumber }
func (value Value) BooleanValue() (bool, bool)   { return value.boolean, value.kind == ValueBoolean }
func (value Value) Values() ([]Value, bool) {
	return cloneValueList(value.list), value.kind == ValueList
}

func CloneValues(values Values) Values {
	if values == nil {
		return Values{}
	}
	cloned := make(Values, len(values))
	for name, value := range values {
		cloned[name] = value.clone()
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

func (value Value) clone() Value {
	value.object = CloneValues(value.object)
	if value.document != nil {
		document := CloneDocument(*value.document)
		value.document = &document
	}
	value.list = cloneValueList(value.list)
	return value
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
		return json.Marshal(value.list)
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
	for index, value := range values {
		cloned[index] = value.clone()
	}
	return cloned
}
