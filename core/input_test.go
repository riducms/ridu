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

func TestGeneratedInputSupportsBlockCodecJSONContract(t *testing.T) {
	if _, ok := any(Input[string]{}).(json.Marshaler); !ok {
		t.Fatal("generated input does not implement json.Marshaler")
	}
	if _, ok := any(&Input[string]{}).(json.Unmarshaler); !ok {
		t.Fatal("generated input does not implement json.Unmarshaler")
	}
	if _, ok := any(NonNullInput[[]string]{}).(json.Marshaler); !ok {
		t.Fatal("non-null generated input does not implement json.Marshaler")
	}
	if _, ok := any(&NonNullInput[[]string]{}).(json.Unmarshaler); !ok {
		t.Fatal("non-null generated input does not implement json.Unmarshaler")
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
	if items, ok := values["items"].CopyList(); !ok || len(items) != 0 {
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
	if items, ok := values["required"].CopyList(); !ok || len(items) != 0 {
		t.Fatalf("required empty list = %#v, %v", items, ok)
	}
	if object, ok := values["optional"].CopyObject(); !ok || len(object) != 0 {
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
	for _, name := range []string{"AllLocales"} {
		if _, exists := options.FieldByName(name); exists {
			t.Fatalf("TypedListOptions exposes shape-changing field %q", name)
		}
	}
}

func TestInputJSONDecoding(t *testing.T) {
	var nullable Input[string]
	for _, raw := range []string{`null`, `"hello"`} {
		if err := json.Unmarshal([]byte(raw), &nullable); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(nullable)
		if err != nil || string(encoded) != raw {
			t.Fatalf("roundtrip %s: %s %v", raw, encoded, err)
		}
	}
	var required NonNullInput[[]string]
	if err := json.Unmarshal([]byte(`null`), &required); err == nil {
		t.Fatal("accepted null")
	}
	if err := json.Unmarshal([]byte(`["hello"]`), &required); err != nil {
		t.Fatal(err)
	}
}

func TestInputGetPreservesPresenceAndZeroValues(t *testing.T) {
	for _, input := range []*Input[bool]{nil, Null[bool]()} {
		if _, ok := input.Get(); ok {
			t.Fatal("absent or null input returned a concrete value")
		}
	}
	if value, ok := Set(false).Get(); !ok || value {
		t.Fatal("false was not a concrete value")
	}
	if value, ok := Set([]string{}).Get(); !ok || value == nil || len(value) != 0 {
		t.Fatal("empty slice lost its presence")
	}
	var absent *NonNullInput[bool]
	if _, ok := absent.Get(); ok {
		t.Fatal("omitted non-null input was present")
	}
	if value, ok := SetNonNull(false).Get(); !ok || value {
		t.Fatal("non-null false was not a concrete value")
	}
	nilSlice := NonNull[[]string](nil)
	if value, ok := nilSlice.Get(); !ok || value != nil {
		t.Fatal("Get silently changed the supplied value")
	}
	if _, err := json.Marshal(nilSlice); err == nil {
		t.Fatal("Get bypassed non-null encoding validation")
	}
}
