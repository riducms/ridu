package core

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Input is an outbound presence marker for nullable generated mutation fields.
// A nil *Input omits the field, Set sends a concrete value, and Null sends
// explicit JSON null when the generated typed local API converts its input to
// store values. Generated block codecs also decode these wrappers to preserve explicit nulls.
type Input[T any] struct {
	value T
	null  bool
}

// Set constructs a present generated mutation field with value.
func Set[T any](value T) *Input[T] {
	return &Input[T]{value: value}
}

// Null constructs a present generated mutation field whose JSON value is null.
func Null[T any]() *Input[T] {
	return &Input[T]{null: true}
}

// Get returns the concrete value, or the zero value and false for omission or
// explicit null. A non-nil input with no concrete value represents explicit null.
func (input *Input[T]) Get() (T, bool) {
	if input == nil || input.null {
		return *new(T), false
	}
	return input.value, true
}

// MarshalJSON implements json.Marshaler. Nil *Input fields are omitted by the
// generated parent struct's omitempty tag before this method is called.
func (input Input[T]) MarshalJSON() ([]byte, error) {
	if input.null {
		return []byte("null"), nil
	}
	return json.Marshal(input.value)
}

// NonNullInput is an outbound value marker for generated mutation fields whose
// Go representation can otherwise encode JSON null even though the schema does
// not allow null. NonNull constructs a required value and SetNonNull constructs
// an omittable present value. JSON encoding fails instead of emitting null for a
// nil slice, nil map, nil interface, or null json.RawMessage.
//
// Decoding rejects explicit null just as encoding does.
type NonNullInput[T any] struct {
	value T
}

// NonNull constructs a non-omittable generated mutation value.
func NonNull[T any](value T) NonNullInput[T] {
	return NonNullInput[T]{value: value}
}

// SetNonNull constructs an omittable generated mutation value that is present.
func SetNonNull[T any](value T) *NonNullInput[T] {
	input := NonNull(value)
	return &input
}

// Get returns the supplied value, or the zero value and false for an omitted
// input. Encoding still validates that the value does not encode as JSON null.
func (input *NonNullInput[T]) Get() (T, bool) {
	if input == nil {
		return *new(T), false
	}
	return input.value, true
}

// MarshalJSON implements json.Marshaler and rejects an encoded JSON null.
func (input NonNullInput[T]) MarshalJSON() ([]byte, error) {
	encoded, err := json.Marshal(input.value)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return nil, errors.New("core.NonNullInput: value encoded as JSON null")
	}
	return encoded, nil
}

// UnmarshalJSON preserves a concrete or explicit-null mutation value.
func (input *Input[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*input = Input[T]{null: true}
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*input = Input[T]{value: value}
	return nil
}

// UnmarshalJSON rejects null for a non-null mutation value.
func (input *NonNullInput[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("core.NonNullInput: JSON null is not allowed")
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	input.value = value
	return nil
}
