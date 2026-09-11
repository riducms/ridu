package schema_test

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestScalarLiteralDecode(t *testing.T) {
	for _, literal := range []schema.ScalarLiteral{
		{Type: schema.ValueTypeString, Value: ""},
		{Type: schema.ValueTypeBoolean, Value: "true"},
		{Type: schema.ValueTypeBoolean, Value: "false"},
		{Type: schema.ValueTypeNumber, Value: "0"},
		{Type: schema.ValueTypeNumber, Value: "-1.25e2"},
		{Type: schema.ValueTypeNumber, Value: "0x1p2"},
	} {
		value, err := literal.Decode()
		if err != nil || string(value.Kind()) != string(literal.Type) {
			t.Fatalf("valid literal %v: %v %v", literal, value, err)
		}
	}
	for _, value := range []string{"", "1", "0", "t", "FALSE", " true"} {
		if _, err := (schema.ScalarLiteral{Type: schema.ValueTypeBoolean, Value: value}).Decode(); err == nil {
			t.Fatalf("accepted malformed boolean %q", value)
		}
	}
	for _, value := range []string{"", "NaN", "Inf", "+Inf", "-Inf", "1e999", "twelve"} {
		if _, err := (schema.ScalarLiteral{Type: schema.ValueTypeNumber, Value: value}).Decode(); err == nil {
			t.Fatalf("accepted malformed or nonfinite number %q", value)
		}
	}
	if _, err := (schema.ScalarLiteral{Type: schema.ValueType("null")}).Decode(); err == nil {
		t.Fatal("accepted non-scalar literal type")
	}
}
