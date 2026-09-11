package schema

import (
	"fmt"
	"math"
	"strconv"

	"github.com/riducms/ridu/query"
)

// ScalarLiteral stores a canonical string, finite number, or boolean operand
// for visibility conditions and relationship filters.
type ScalarLiteral struct {
	Type  ValueType `json:"type"`
	Value string    `json:"value"`
}

// Decode validates the literal and returns its immutable query value. Numbers
// retain Go's floating-point parsing rules; booleans must be "true" or "false".
func (literal ScalarLiteral) Decode() (query.Value, error) {
	switch literal.Type {
	case ValueTypeString:
		return query.String(literal.Value), nil
	case ValueTypeNumber:
		number, err := strconv.ParseFloat(literal.Value, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return query.Value{}, fmt.Errorf("invalid number scalar literal %q", literal.Value)
		}
		return query.Number(number), nil
	case ValueTypeBoolean:
		if literal.Value != "true" && literal.Value != "false" {
			return query.Value{}, fmt.Errorf("invalid boolean scalar literal %q", literal.Value)
		}
		return query.Boolean(literal.Value == "true"), nil
	default:
		return query.Value{}, fmt.Errorf("invalid scalar literal type %q", literal.Type)
	}
}
