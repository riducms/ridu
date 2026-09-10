package field

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
)

func normalizeDefault[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](value Value) (DefaultValue, error) {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.String:
		return DefaultValue{kind: DefaultString, text: reflected.String()}, nil
	case reflect.Bool:
		return DefaultValue{kind: DefaultBoolean, text: strconv.FormatBool(reflected.Bool())}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return DefaultValue{kind: DefaultNumber, text: strconv.FormatInt(reflected.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return DefaultValue{kind: DefaultNumber, text: strconv.FormatUint(reflected.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		number := reflected.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return DefaultValue{}, fmt.Errorf("number field default must be finite")
		}
		return DefaultValue{kind: DefaultNumber, text: strconv.FormatFloat(number, 'g', -1, reflected.Type().Bits())}, nil
	default:
		return DefaultValue{}, fmt.Errorf("unsupported default type %T", value)
	}
}
