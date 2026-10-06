package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/jsonlimit"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

const (
	maxWhereDepth       = 16
	maxWhereExpressions = 100
	maxWhereInValues    = 100
)

type whereBudget struct{ expressions int }

func decodeWhere(encoded []byte, collection schema.Collection) (query.Expression, error) {
	if err := jsonlimit.Validate(encoded, maxWhereDepth*4, 4096); err != nil {
		return nil, fmt.Errorf("where JSON is invalid or exceeds its structural limit: %w", err)
	}
	return new(whereBudget).decode(encoded, collection, 1)
}

func (budget *whereBudget) decode(encoded []byte, collection schema.Collection, depth int) (query.Expression, error) {
	if depth > maxWhereDepth {
		return nil, fmt.Errorf("where expression depth exceeds %d", maxWhereDepth)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("where must not be empty")
	}
	budget.expressions += len(object)
	if budget.expressions > maxWhereExpressions {
		return nil, fmt.Errorf("where contains more than %d expressions", maxWhereExpressions)
	}
	var expressions []query.Expression
	for name, raw := range object {
		switch name {
		case "and", "or":
			var children []json.RawMessage
			if err := json.Unmarshal(raw, &children); err != nil || len(children) < 2 {
				return nil, fmt.Errorf("%s requires at least two expressions", name)
			}
			decoded := make([]query.Expression, len(children))
			for index, child := range children {
				expression, err := budget.decode(child, collection, depth+1)
				if err != nil {
					return nil, err
				}
				decoded[index] = expression
			}
			// The length check above keeps these helpers from panicking on input.
			if name == "and" {
				expressions = append(expressions, query.And(decoded...))
			} else {
				expressions = append(expressions, query.Or(decoded...))
			}
		case "not":
			child, err := budget.decode(raw, collection, depth+1)
			if err != nil {
				return nil, err
			}
			expressions = append(expressions, query.Not(child))
		default:
			path, err := query.ParsePath(name)
			if err != nil {
				return nil, err
			}
			if !queryableSystemField(collection, name) {
				if _, _, exists := schemaFieldAtPath(collection, path); !exists {
					return nil, fmt.Errorf("field %q is not defined", name)
				}
			}
			expression, err := decodeComparison(path, raw)
			if err != nil {
				return nil, err
			}
			expressions = append(expressions, expression)
		}
	}
	return query.And(expressions...), nil
}

func queryableSystemField(collection schema.Collection, name string) bool {
	switch name {
	case "id", "createdAt", "updatedAt":
		return true
	case "_status":
		return collection.Versions != nil
	default:
		return false
	}
}

func decodeComparison(path query.Path, encoded []byte) (query.Expression, error) {
	var operators map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &operators); err != nil || len(operators) != 1 {
		return nil, fmt.Errorf("field comparison requires exactly one operator")
	}
	for operator, raw := range operators {
		switch operator {
		case "equals", "notEquals", "not_equals":
			value, err := queryValueForPath(path, raw)
			if err != nil {
				return nil, err
			}
			if operator == "equals" {
				return query.Equal(path, value), nil
			}
			return query.NotEqual(path, value), nil
		case "greaterThan", "greater_than", "greaterThanEqual", "greater_than_equal", "lessThan", "less_than", "lessThanEqual", "less_than_equal":
			value, err := queryValueForPath(path, raw)
			if err != nil {
				return nil, err
			}
			operators := map[string]query.Operator{
				"greaterThan": query.OperatorGreaterThan, "greater_than": query.OperatorGreaterThan,
				"greaterThanEqual": query.OperatorGreaterThanEqual, "greater_than_equal": query.OperatorGreaterThanEqual,
				"lessThan": query.OperatorLessThan, "less_than": query.OperatorLessThan,
				"lessThanEqual": query.OperatorLessThanEqual, "less_than_equal": query.OperatorLessThanEqual,
			}
			return query.Compare(path, operators[operator], value)
		case "contains", "like":
			if isTimestampPath(path) {
				return nil, fmt.Errorf("timestamp field %q does not support %s", path.String(), operator)
			}
			value, err := queryValue(raw)
			if err != nil {
				return nil, err
			}
			queryOperator := query.OperatorContains
			if operator == "like" {
				queryOperator = query.OperatorLike
			}
			return query.Compare(path, queryOperator, value)
		case "in":
			var rawValues []json.RawMessage
			if err := json.Unmarshal(raw, &rawValues); err != nil || len(rawValues) == 0 || len(rawValues) > maxWhereInValues {
				return nil, fmt.Errorf("in requires between 1 and %d values", maxWhereInValues)
			}
			values := make([]query.Value, len(rawValues))
			for index, item := range rawValues {
				value, err := queryValueForPath(path, item)
				if err != nil {
					return nil, err
				}
				values[index] = value
			}
			return query.In(path, values...), nil
		case "exists":
			var exists bool
			if err := json.Unmarshal(raw, &exists); err != nil {
				return nil, fmt.Errorf("exists requires a boolean")
			}
			return query.Compare(path, query.OperatorExists, query.Boolean(exists))
		default:
			return nil, fmt.Errorf("unsupported comparison operator %q", operator)
		}
	}
	return nil, fmt.Errorf("missing comparison")
}

func queryValue(encoded []byte) (query.Value, error) {
	if string(encoded) == "null" {
		return query.Null(), nil
	}
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		return query.String(text), nil
	}
	var number float64
	if err := json.Unmarshal(encoded, &number); err == nil {
		return query.Number(number), nil
	}
	var boolean bool
	if err := json.Unmarshal(encoded, &boolean); err == nil {
		return query.Boolean(boolean), nil
	}
	if reference, ok := decodeReference(encoded); ok {
		return reference, nil
	}
	return query.Value{}, fmt.Errorf("comparison value must be a scalar, null, or a polymorphic {\"relationTo\", \"id\"} reference")
}

// decodeReference reads a polymorphic relationship candidate in the
// relationship's own value shape. The engine decides where one is accepted.
func decodeReference(encoded []byte) (query.Value, bool) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var reference struct {
		RelationTo *string `json:"relationTo"`
		ID         *string `json:"id"`
	}
	if decoder.Decode(&reference) != nil || reference.RelationTo == nil || reference.ID == nil {
		return query.Value{}, false
	}
	return query.Reference(*reference.RelationTo, *reference.ID), true
}

func queryValueForPath(path query.Path, encoded []byte) (query.Value, error) {
	value, err := queryValue(encoded)
	if err != nil || !isTimestampPath(path) || value.Kind() == query.ValueNull {
		return value, err
	}
	text, ok := value.StringValue()
	if !ok {
		return query.Value{}, fmt.Errorf("timestamp field %q requires an RFC3339 string or null", path.String())
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return query.Value{}, fmt.Errorf("timestamp field %q requires an RFC3339 string: %w", path.String(), err)
	}
	return query.String(parsed.UTC().Format(time.RFC3339Nano)), nil
}

func isTimestampPath(path query.Path) bool {
	name := path.String()
	return name == "createdAt" || name == "updatedAt"
}

func hasTopLevelField(collection schema.Collection, name string) bool {
	for _, field := range collection.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func schemaFieldAtPath(
	collection schema.Collection,
	path query.Path,
) (*schema.Field, bool, bool) {
	segments := path.Segments()
	if len(segments) == 0 {
		return nil, false, false
	}
	return schemaFieldIn(segments, 0, collection.Fields, false)
}

func schemaFieldIn(
	segments []string,
	index int,
	fields []schema.Field,
	many bool,
) (*schema.Field, bool, bool) {
	for fieldIndex := range fields {
		field := &fields[fieldIndex]
		if field.Name != segments[index] {
			continue
		}
		if index == len(segments)-1 {
			return field, many, true
		}
		switch field.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if field.Nested == nil {
				return nil, false, false
			}
			return schemaFieldIn(
				segments,
				index+1,
				field.Nested.ResolvedFields(),
				many || field.Type == schema.FieldTypeArray,
			)
		case schema.FieldTypeBlocks:
			if field.Blocks == nil || index+2 >= len(segments) {
				return nil, false, false
			}
			if block, found := field.Blocks.Definition(segments[index+1]); found {
				return schemaFieldIn(segments, index+2, block.ResolvedFields(), true)
			}
		}
		return nil, false, false
	}
	return nil, false, false
}

// bracketWhereExample rewrites qs-style where[field][operator]=value
// parameters as the JSON Ridu expects, so a client ported from a
// bracket-style API sees its own filter. Logical groups and list indexes do
// not translate unambiguously and fall back to a generic example.
func bracketWhereExample(values url.Values) string {
	const generic = `{"status":{"equals":"published"}}`
	example := map[string]map[string]string{}
	for key, entries := range values {
		if !strings.HasPrefix(key, "where[") {
			continue
		}
		segments := strings.Split(strings.TrimSuffix(strings.TrimPrefix(key, "where["), "]"), "][")
		if len(segments) != 2 || len(entries) != 1 || segments[0] == "" || segments[1] == "" {
			return generic
		}
		field, operator := segments[0], segments[1]
		if field == "and" || field == "or" || field == "not" {
			return generic
		}
		if example[field] == nil {
			example[field] = map[string]string{}
		}
		example[field][operator] = entries[0]
	}
	encoded, err := json.Marshal(example)
	if err != nil || len(example) == 0 {
		return generic
	}
	return string(encoded)
}
