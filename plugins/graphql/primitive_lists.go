package graphql

import (
	"fmt"
	"math"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/riducms/ridu/schema"
)

// GraphQL can substitute a variable default for submitted null and coerce
// singleton lists. Inspect the submitted data before using coerced arguments;
// child nulls remain valid clears and field validation stays in the engine.
func submittedDataInputError(params enginegraphql.ResolveParams, fields []schema.Field, requiredData bool) error {
	submitted, present := submittedInputArgument(params, "data")
	if requiredData && (!present || submitted == nil) {
		return fmt.Errorf("data must be a non-null object")
	}
	if present {
		return checkPrimitiveListObject(submitted, fields, "data")
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
