package graphql

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

type whereField struct {
	field schema.Field
	name  string
	path  string
}

func whereExpression(current resource, raw interface{}) (query.Expression, error) {
	values, _ := raw.(map[string]interface{})
	if len(values) == 0 {
		return nil, nil
	}
	paths := map[string]string{"id": "id"}
	for _, field := range flattenWhereFields(current.Fields) {
		paths[field.name] = field.path
	}
	return parseWhere(values, paths)
}

func flattenWhereFields(fields []schema.Field) []whereField {
	result := make([]whereField, 0, len(fields))
	var walk func([]schema.Field, []string)
	walk = func(current []schema.Field, prefix []string) {
		for _, field := range current {
			path := append(append([]string(nil), prefix...), field.Name)
			switch field.Type {
			case schema.FieldTypeGroup, schema.FieldTypeArray:
				if field.Nested != nil {
					walk(field.Nested.Fields, path)
				}
				continue
			case schema.FieldTypeBlocks:
				if field.Blocks != nil {
					for _, block := range field.Blocks.Types {
						walk(block.Fields, append(path, block.Key))
					}
				}
				continue
			case schema.FieldTypeRelationship:
				if field.Relationship == nil || field.Relationship.Polymorphic || field.Relationship.HasMany {
					continue
				}
			case schema.FieldTypeUpload:
				if field.Upload == nil || field.Upload.HasMany {
					continue
				}
			case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeCode, schema.FieldTypeDate,
				schema.FieldTypeNumber, schema.FieldTypeCheckbox, schema.FieldTypeSelect, schema.FieldTypeRadio:
			default:
				continue
			}
			pathName := strings.Join(path, ".")
			result = append(result, whereField{field: field, name: fieldName(strings.Join(path, "__")), path: pathName})
		}
	}
	walk(fields, nil)
	return result
}

func parseWhere(values map[string]interface{}, paths map[string]string) (query.Expression, error) {
	var expressions []query.Expression
	for _, key := range sortedKeys(values) {
		value := values[key]
		switch key {
		case "and", "or", "AND", "OR":
			rawChildren, _ := value.([]interface{})
			children := make([]query.Expression, 0, len(rawChildren))
			for _, rawChild := range rawChildren {
				childMap, _ := rawChild.(map[string]interface{})
				child, err := parseWhere(childMap, paths)
				if err != nil {
					return nil, err
				}
				if child != nil {
					children = append(children, child)
				}
			}
			combined, err := combineLogical(strings.ToLower(key), children)
			if err != nil {
				return nil, err
			}
			if combined != nil {
				expressions = append(expressions, combined)
			}
		case "not", "NOT":
			childMap, _ := value.(map[string]interface{})
			child, err := parseWhere(childMap, paths)
			if err != nil {
				return nil, err
			}
			if child != nil {
				negated, err := query.Not(child)
				if err != nil {
					return nil, err
				}
				expressions = append(expressions, negated)
			}
		default:
			pathValue, exists := paths[key]
			if !exists {
				return nil, fmt.Errorf("unknown where field %q", key)
			}
			path, err := query.ParsePath(pathValue)
			if err != nil {
				return nil, err
			}
			operators, _ := value.(map[string]interface{})
			for _, operator := range sortedKeys(operators) {
				expression, err := comparison(path, operator, operators[operator])
				if err != nil {
					return nil, err
				}
				expressions = append(expressions, expression)
			}
		}
	}
	return combineLogical("and", expressions)
}

func comparison(path query.Path, operator string, raw interface{}) (query.Expression, error) {
	value := queryValue(raw)
	var kind query.Operator
	switch operator {
	case "equals":
		kind = query.OperatorEqual
	case "not_equals":
		kind = query.OperatorNotEqual
	case "in", "not_in":
		kind = query.OperatorIn
	case "contains":
		kind = query.OperatorContains
	case "like":
		kind = query.OperatorLike
	case "greater_than":
		kind = query.OperatorGreaterThan
	case "greater_than_equal":
		kind = query.OperatorGreaterThanEqual
	case "less_than":
		kind = query.OperatorLessThan
	case "less_than_equal":
		kind = query.OperatorLessThanEqual
	case "exists":
		kind = query.OperatorExists
	default:
		return nil, fmt.Errorf("unknown where operator %q", operator)
	}
	expression, err := query.Compare(path, kind, value)
	if err != nil {
		return nil, err
	}
	if operator == "not_in" {
		return query.Not(expression)
	}
	return expression, nil
}

func queryValue(raw interface{}) query.Value {
	switch value := raw.(type) {
	case nil:
		return query.Null()
	case string:
		return query.String(value)
	case float64:
		return query.Number(value)
	case int:
		return query.Number(float64(value))
	case bool:
		return query.Boolean(value)
	case []interface{}:
		items := make([]query.Value, len(value))
		for index, item := range value {
			items[index] = queryValue(item)
		}
		return query.List(items...)
	default:
		return query.String(fmt.Sprint(value))
	}
}

func combineLogical(kind string, expressions []query.Expression) (query.Expression, error) {
	switch len(expressions) {
	case 0:
		return nil, nil
	case 1:
		return expressions[0], nil
	}
	if kind == "or" {
		return query.Or(expressions...)
	}
	return query.And(expressions...)
}
