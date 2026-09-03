package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/store"
)

type namedNilableInput []string

type nullEncodingInput struct{}

func (nullEncodingInput) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func TestGeneratedInputRemainsWriteOnlyJSONContract(t *testing.T) {
	if _, ok := any(Input[string]{}).(json.Marshaler); !ok {
		t.Fatal("generated input does not implement json.Marshaler")
	}
	if _, ok := any(&Input[string]{}).(json.Unmarshaler); ok {
		t.Fatal("generated input unexpectedly implements json.Unmarshaler")
	}
	if _, ok := any(NonNullInput[[]string]{}).(json.Marshaler); !ok {
		t.Fatal("non-null generated input does not implement json.Marshaler")
	}
	if _, ok := any(&NonNullInput[[]string]{}).(json.Unmarshaler); ok {
		t.Fatal("non-null generated input unexpectedly implements json.Unmarshaler")
	}
}

func TestGeneratedInputEncodesOmittedNullAndConcreteValues(t *testing.T) {
	type input struct {
		Omitted *Input[string]   `json:"omitted,omitempty"`
		Null    *Input[string]   `json:"null,omitempty"`
		Text    *Input[string]   `json:"text,omitempty"`
		Items   *Input[[]string] `json:"items,omitempty"`
	}

	values, err := typedInputValues(input{
		Null:  Null[string](),
		Text:  Set(""),
		Items: Set([]string{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := values["omitted"]; exists {
		t.Fatal("nil generated input was encoded instead of omitted")
	}
	if values["null"].Kind() != store.ValueNull {
		t.Fatalf("explicit null kind = %q, want null", values["null"].Kind())
	}
	if text, ok := values["text"].StringValue(); !ok || text != "" {
		t.Fatalf("explicit empty string = %#v, %v", text, ok)
	}
	if items, ok := values["items"].Values(); !ok || len(items) != 0 {
		t.Fatalf("explicit empty list = %#v, %v", items, ok)
	}
}

func TestGeneratedNonNullInputPreservesRequiredAndOptionalPresence(t *testing.T) {
	type input struct {
		Required NonNullInput[[]string]           `json:"required"`
		Optional *NonNullInput[map[string]string] `json:"optional,omitempty"`
		Omitted  *NonNullInput[[]string]          `json:"omitted,omitempty"`
	}

	values, err := typedInputValues(input{
		Required: NonNull([]string{}),
		Optional: SetNonNull(map[string]string{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if items, ok := values["required"].Values(); !ok || len(items) != 0 {
		t.Fatalf("required empty list = %#v, %v", items, ok)
	}
	if object, ok := values["optional"].ObjectValue(); !ok || len(object) != 0 {
		t.Fatalf("optional present empty object = %#v, %v", object, ok)
	}
	if _, exists := values["omitted"]; exists {
		t.Fatal("nil non-null generated input was encoded instead of omitted")
	}
}

func TestGeneratedNonNullInputRejectsEveryJSONNullRepresentation(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "zero wrapper", value: NonNullInput[[]string]{}},
		{name: "nil slice", value: NonNull[[]string](nil)},
		{name: "optional-present nil slice", value: SetNonNull[[]string](nil)},
		{name: "nil map", value: NonNull[map[string]string](nil)},
		{name: "nil interface", value: NonNull[any](nil)},
		{name: "nil raw message", value: NonNull(json.RawMessage(nil))},
		{name: "null raw message", value: NonNull(json.RawMessage(" \n null \t"))},
		{name: "optional-present null raw message", value: SetNonNull(json.RawMessage("null"))},
		{name: "named nil-able value", value: NonNull(namedNilableInput(nil))},
		{name: "optional-present named nil-able value", value: SetNonNull(namedNilableInput(nil))},
		{name: "custom null marshaler", value: NonNull(nullEncodingInput{})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if encoded, err := json.Marshal(test.value); err == nil || !strings.Contains(err.Error(), "value encoded as JSON null") {
				t.Fatalf("json.Marshal() = %q, %v; want non-null error", encoded, err)
			}
		})
	}
}

func TestGeneratedNonNullInputPreservesConcreteRawJSON(t *testing.T) {
	encoded, err := json.Marshal(NonNull(json.RawMessage(`{"answer":42}`)))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"answer":42}` {
		t.Fatalf("encoded raw JSON = %s", encoded)
	}
}

func TestTypedListOptionsExcludeShapeChangingReads(t *testing.T) {
	options := reflect.TypeOf(TypedListOptions{})
	for _, name := range []string{"Populate", "AllLocales"} {
		if _, exists := options.FieldByName(name); exists {
			t.Fatalf("TypedListOptions exposes shape-changing field %q", name)
		}
	}
}
