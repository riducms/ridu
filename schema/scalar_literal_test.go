package schema_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestScalarLiteralDecode(t *testing.T) {
	for _, test := range []struct {
		literal schema.ScalarLiteral
		want    query.Value
	}{
		{schema.ScalarLiteral{Type: schema.ValueTypeString, Value: ""}, query.String("")},
		{schema.ScalarLiteral{Type: schema.ValueTypeString, Value: "published"}, query.String("published")},
		{schema.ScalarLiteral{Type: schema.ValueTypeBoolean, Value: "true"}, query.Boolean(true)},
		{schema.ScalarLiteral{Type: schema.ValueTypeBoolean, Value: "false"}, query.Boolean(false)},
		{schema.ScalarLiteral{Type: schema.ValueTypeNumber, Value: "0"}, query.Number(0)},
		{schema.ScalarLiteral{Type: schema.ValueTypeNumber, Value: "-1.25e2"}, query.Number(-125)},
		{schema.ScalarLiteral{Type: schema.ValueTypeNumber, Value: "0x1p2"}, query.Number(4)},
	} {
		t.Run(string(test.literal.Type)+"/"+test.literal.Value, func(t *testing.T) {
			value, err := test.literal.Decode()
			if err != nil || !reflect.DeepEqual(value, test.want) {
				t.Fatalf("Decode(%v) = %v, %v; want %v", test.literal, value, err, test.want)
			}
		})
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
