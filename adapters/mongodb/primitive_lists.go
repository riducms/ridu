package mongodb

import (
	"strings"

	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoPrimitiveListJSONSchema(field schema.Field) bson.D {
	item := field
	item.Required = true
	item.Type = schema.FieldTypeText
	if field.Type == schema.FieldTypeNumberList {
		item.Type = schema.FieldTypeNumber
	}
	kind := any("array")
	if !field.Required {
		kind = bson.A{"array", "null"}
	}
	result := bson.D{{Key: "bsonType", Value: kind}, {Key: "items", Value: mongoScalarFieldJSONSchema(item, false)}}
	if field.Required {
		result = append(result, bson.E{Key: "minItems", Value: 1})
	}
	return result
}

func compileMongoPrimitiveList(path mongoPredicatePath, comparison query.Comparison) (bson.D, error) {
	field := *path.primitiveList
	if err := primitivefield.ValidateComparison(field, comparison); err != nil {
		return nil, err
	}
	value := any("$" + path.storagePath)
	if len(path.localePaths) > 0 {
		value = mongoLocalizedValueExpression(path)
	}
	// A missing value and an explicit null share the established empty-state query
	// contract. [] remains present and never falls back to another locale.
	value = bson.D{{Key: "$ifNull", Value: bson.A{value, nil}}}
	var expression any
	switch comparison.Operator {
	case query.OperatorIn:
		candidates := make(bson.A, 0, len(comparison.Value.Values()))
		for _, item := range comparison.Value.Values() {
			if field.Type == schema.FieldTypeTextList {
				text, _ := item.StringValue()
				candidates = append(candidates, text)
			} else {
				number, _ := item.NumberValue()
				candidates = append(candidates, number)
			}
		}
		safe := bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$isArray", Value: "$$list"}}, "$$list", bson.A{}}}}
		expression = bson.D{{Key: "$gt", Value: bson.A{bson.D{{Key: "$size", Value: bson.D{{Key: "$setIntersection", Value: bson.A{safe, bson.D{{Key: "$literal", Value: candidates}}}}}}}, 0}}}
	case query.OperatorExists:
		want, _ := comparison.Value.BooleanValue()
		operator := "$eq"
		if want {
			operator = "$ne"
		}
		expression = bson.D{{Key: operator, Value: bson.A{"$$list", nil}}}
	case query.OperatorEqual, query.OperatorNotEqual:
		operator := "$eq"
		if comparison.Operator == query.OperatorNotEqual {
			operator = "$ne"
		}
		expression = bson.D{{Key: operator, Value: bson.A{"$$list", nil}}}
	}
	predicate := bson.D{{Key: "$expr", Value: bson.D{{Key: "$let", Value: bson.D{{Key: "vars", Value: bson.D{{Key: "list", Value: value}}}, {Key: "in", Value: expression}}}}}}
	return mongoAnd([]bson.D{mongoPrimitiveListShape(path), predicate}), nil
}

// The same guard is hoisted outside Not/Or by the predicate compiler, so
// negating a failed shape check can never admit a malformed persisted list.
func mongoPrimitiveListShape(path mongoPredicatePath) bson.D {
	shape := mongoCollectionFieldJSONSchema(*path.primitiveList, nil)
	segments := strings.Split(path.storagePath, ".")
	for i := len(segments) - 1; i >= 0; i-- {
		shape = bson.D{{Key: "bsonType", Value: bson.A{"object", "null"}}, {Key: "properties", Value: bson.D{{Key: segments[i], Value: shape}}}}
	}
	return bson.D{{Key: "$jsonSchema", Value: shape}}
}
