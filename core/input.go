package core

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Input is an outbound presence marker for nullable generated mutation fields.
// A nil *Input omits the field, Set sends a concrete value, and Null sends
// explicit JSON null when the generated typed local API converts its input to
// store values. Input intentionally implements only json.Marshaler; generated
// mutation structs are not general-purpose JSON decode DTOs.
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
// NonNullInput intentionally implements only json.Marshaler; generated mutation
// structs are not general-purpose JSON decode DTOs.
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
