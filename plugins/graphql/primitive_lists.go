package graphql

import (
	"fmt"
	"math"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/riducms/ridu/schema"
)

// GraphQL normally coerces a singleton into a list and can coerce variable
// scalars. Primitive fields keep the same strict wire values as REST and the
// Local API, so inspect their submitted values before using coerced arguments.
func primitiveListInputError(params enginegraphql.ResolveParams, fields []schema.Field) error {
	variables := requestFromContext(params).rawVariables
	if variables == nil {
		variables = params.Info.VariableValues
	}
	defaults := map[string]ast.Value{}
	if operation, ok := params.Info.Operation.(*ast.OperationDefinition); ok {
		for _, definition := range operation.VariableDefinitions {
			defaults[definition.Variable.Name.Value] = definition.DefaultValue
		}
	}
	var submitted func(ast.Value) interface{}
	submitted = func(value ast.Value) interface{} {
		switch value := value.(type) {
		case *ast.Variable:
			if result, exists := variables[value.Name.Value]; exists {
				return result
			}
			return submitted(defaults[value.Name.Value])
		case *ast.ListValue:
			result := make([]interface{}, len(value.Values))
			for index, child := range value.Values {
				result[index] = submitted(child)
			}
			return result
		case *ast.ObjectValue:
			result := make(map[string]interface{}, len(value.Fields))
			for _, child := range value.Fields {
				result[child.Name.Value] = submitted(child.Value)
			}
			return result
		default:
			return parseJSONLiteral(value)
		}
	}
	for _, node := range params.Info.FieldASTs {
		for _, argument := range node.Arguments {
			if argument.Name.Value == "data" {
				if err := checkPrimitiveListObject(submitted(argument.Value), fields, "data"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkPrimitiveListObject(raw interface{}, fields []schema.Field, path string) error {
	object, _ := raw.(map[string]interface{})
	for _, field := range fields {
		value, present := object[fieldName(field.Name)]
		if !present || value == nil {
			continue // Requiredness and counts remain authoritative runtime checks.
		}
		childPath := path + "." + field.Name
		switch field.Type {
		case schema.FieldTypeTextList, schema.FieldTypeNumberList:
			items, ok := value.([]interface{})
			if !ok {
				return fmt.Errorf("%s must be an array", childPath)
			}
			for index, item := range items {
				if field.Type == schema.FieldTypeTextList {
					if _, ok := item.(string); !ok {
						return fmt.Errorf("%s item %d must be a string", childPath, index+1)
					}
				} else if number, ok := item.(float64); !ok || math.IsNaN(number) || math.IsInf(number, 0) {
					return fmt.Errorf("%s item %d must be a finite number", childPath, index+1)
				}
			}
		case schema.FieldTypeGroup:
			if field.Nested != nil {
				if err := checkPrimitiveListObject(value, field.Nested.ResolvedFields(), childPath); err != nil {
					return err
				}
			}
		case schema.FieldTypeArray:
			if field.Nested == nil {
				continue
			}
			items, ok := value.([]interface{})
			if !ok {
				items = []interface{}{value} // Existing GraphQL object-row coercion.
			}
			for index, item := range items {
				if err := checkPrimitiveListObject(item, field.Nested.ResolvedFields(), fmt.Sprintf("%s[%d]", childPath, index)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func primitiveListNeedsValue(field schema.Field) bool {
	return (field.Type == schema.FieldTypeTextList || field.Type == schema.FieldTypeNumberList) && field.List != nil && field.List.MinRows > 0
}
