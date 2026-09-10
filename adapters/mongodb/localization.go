package mongodb

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// mongoPredicateScope keeps physical storage location and request locale
// selection together. Version predicates compile directly beneath snapshot;
// rewriting a completed predicate would miss field references embedded in
// $expr expressions.
type mongoPredicateScope struct {
	localeChain   []schema.LocaleCode
	storagePrefix string
}

func (scope mongoPredicateScope) path(path string) string {
	return scope.storagePrefix + path
}

func compileMongoAccessNode(
	collection schema.Collection,
	node query.Node,
	role string,
	scope mongoPredicateScope,
	allLocales bool,
	locales []schema.LocaleCode,
) (bson.D, error) {
	if !allLocales || len(locales) == 0 {
		return compileMongoNode(collection, node, role, scope)
	}
	if _, err := mongoConfiguredLocales(locales); err != nil {
		return nil, err
	}
	predicates := make([]bson.D, 0, len(locales))
	for _, locale := range locales {
		localeScope := scope
		localeScope.localeChain = []schema.LocaleCode{locale}
		compiled, err := compileMongoNode(collection, node, role, localeScope)
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, compiled)
	}
	return mongoAnd(predicates), nil
}

func mongoLocalizedValueExpression(path mongoPredicatePath) any {
	var fallback any = nil
	for index := len(path.localePaths) - 1; index >= 0; index-- {
		candidate := "$" + path.localePaths[index]
		visible := bson.A{
			bson.D{{Key: "$ne", Value: bson.A{bson.D{{Key: "$type", Value: candidate}}, "missing"}}},
			bson.D{{Key: "$ne", Value: bson.A{candidate, nil}}},
		}
		if path.kind == mongoStringScalar && index < len(path.localePaths)-1 {
			visible = append(visible, bson.D{{Key: "$ne", Value: bson.A{candidate, ""}}})
		}
		fallback = bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$and", Value: visible}}, candidate, fallback,
		}}}
	}
	return fallback
}

func compileMongoLocalizedComparison(path mongoPredicatePath, comparison query.Comparison) (bson.D, error) {
	if comparison.Operator == query.OperatorIn {
		predicates := make([]bson.D, 0, len(comparison.Value.Values()))
		for _, value := range comparison.Value.Values() {
			candidate := comparison
			candidate.Operator = query.OperatorEqual
			candidate.Value = value
			compiled, err := compileMongoLocalizedComparison(path, candidate)
			if err != nil {
				return nil, err
			}
			predicates = append(predicates, compiled)
		}
		if len(predicates) == 0 {
			return mongoConstant(false), nil
		}
		if len(predicates) == 1 {
			return predicates[0], nil
		}
		return bson.D{{Key: "$or", Value: mongoDocumentArray(predicates)}}, nil
	}

	nullableAncestors := false
	var expression any
	switch comparison.Operator {
	case query.OperatorExists:
		want, _ := comparison.Value.BooleanValue()
		expression = bson.D{{Key: "$ne", Value: bson.A{"$$riduValue", nil}}}
		if !want {
			expression = bson.D{{Key: "$eq", Value: bson.A{"$$riduValue", nil}}}
			nullableAncestors = true
		}
	case query.OperatorEqual, query.OperatorNotEqual:
		nullableAncestors = comparison.Operator == query.OperatorNotEqual || comparison.Value.Kind() == query.ValueNull
		if comparison.Value.Kind() == query.ValueNull {
			operator := "$eq"
			if comparison.Operator == query.OperatorNotEqual {
				operator = "$ne"
			}
			expression = bson.D{{Key: operator, Value: bson.A{"$$riduValue", nil}}}
			break
		}
		expected, compatible, err := mongoOperand(path, comparison.Value)
		if err != nil {
			return nil, err
		}
		if !compatible {
			return mongoLocalizedConstant(path, comparison.Operator == query.OperatorNotEqual, nullableAncestors), nil
		}
		equal := bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$$riduValue"}}, mongoBSONType(path.kind)}}},
			bson.D{{Key: "$eq", Value: bson.A{"$$riduValue", expected}}},
		}}}
		expression = equal
		if comparison.Operator == query.OperatorNotEqual {
			expression = bson.D{{Key: "$not", Value: bson.A{equal}}}
		}
	case query.OperatorContains, query.OperatorLike:
		if path.kind != mongoStringScalar {
			return mongoLocalizedConstant(path, false, false), nil
		}
		text, _ := comparison.Value.StringValue()
		words := []string{text}
		if comparison.Operator == query.OperatorLike {
			words = strings.Fields(text)
		}
		matches := make(bson.A, 0, len(words))
		for _, word := range words {
			matches = append(matches, bson.D{{Key: "$regexMatch", Value: bson.D{
				{Key: "input", Value: "$$riduValue"},
				{Key: "regex", Value: regexp.QuoteMeta(word)},
				{Key: "options", Value: "i"},
			}}})
		}
		matched := any(true)
		if len(matches) == 1 {
			matched = matches[0]
		} else if len(matches) > 1 {
			matched = bson.D{{Key: "$and", Value: matches}}
		}
		expression = bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$$riduValue"}}, "string"}}},
			matched,
			false,
		}}}
	default:
		expected, compatible, err := mongoOperand(path, comparison.Value)
		if err != nil {
			return nil, err
		}
		if !compatible {
			return mongoLocalizedConstant(path, false, false), nil
		}
		operator := map[query.Operator]string{
			query.OperatorGreaterThan:      "$gt",
			query.OperatorGreaterThanEqual: "$gte",
			query.OperatorLessThan:         "$lt",
			query.OperatorLessThanEqual:    "$lte",
		}[comparison.Operator]
		if operator == "" {
			return nil, nil
		}
		expression = bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$$riduValue"}}, mongoBSONType(path.kind)}}},
			bson.D{{Key: operator, Value: bson.A{"$$riduValue", expected}}},
		}}}
	}
	return mongoLocalizedExpressionPredicate(path, expression, nullableAncestors), nil
}

func mongoLocalizedConstant(path mongoPredicatePath, value, nullableAncestors bool) bson.D {
	return mongoLocalizedExpressionPredicate(path, value, nullableAncestors)
}

func mongoLocalizedExpressionPredicate(path mongoPredicatePath, expression any, nullableAncestors bool) bson.D {
	guards := mongoObjectAncestorGuards(path)
	if nullableAncestors {
		guards = mongoNullableObjectAncestorGuards(path)
	}
	predicate := bson.D{{Key: "$expr", Value: bson.D{{Key: "$let", Value: bson.D{
		{Key: "vars", Value: bson.D{{Key: "riduValue", Value: mongoLocalizedValueExpression(path)}}},
		{Key: "in", Value: expression},
	}}}}}
	return mongoAnd(append(guards, predicate))
}

// mongoPatchAssignments builds aggregation expressions for the authored
// top-level values in one Update. Localized locale maps and supported group
// objects merge against their stored counterparts; ordinary leaves retain the
// adapter's replacement semantics. Every caller-owned value is wrapped in
// $literal so strings beginning with '$' cannot become aggregation references.
func mongoPatchAssignments(collection schema.Collection, values store.Values) (bson.D, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	assignments := make(bson.D, 0, len(keys))
	for _, key := range keys {
		field, exists := mongoFieldNamed(collection.Fields, key)
		if !exists || field.Category == schema.FieldCategoryPresentation {
			return nil, fmt.Errorf("MongoDB update field %q is not stored", key)
		}
		expression, err := mongoPatchValueExpression(field, "values."+key, values[key])
		if err != nil {
			return nil, fmt.Errorf("encode MongoDB value %q: %w", key, err)
		}
		assignments = append(assignments, bson.E{Key: "values." + key, Value: expression})
	}
	return assignments, nil
}

func mongoPatchValueExpression(field schema.Field, storagePath string, value store.Value) (any, error) {
	encoded, err := encodeValue(value)
	if err != nil {
		return nil, err
	}
	if value.Kind() == store.ValueNull {
		return mongoLiteral(encoded), nil
	}
	if field.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return nil, fmt.Errorf("localized field %q does not have a canonical locale map", field.Path.String())
		}
		locales := make([]string, 0, len(localized))
		for locale := range localized {
			locales = append(locales, locale)
		}
		sort.Strings(locales)
		unlocalized := field
		unlocalized.Localized = false
		patch := make(bson.D, 0, len(locales))
		for _, locale := range locales {
			expression, err := mongoPatchValueExpression(unlocalized, storagePath+"."+locale, localized[locale])
			if err != nil {
				return nil, err
			}
			patch = append(patch, bson.E{Key: locale, Value: expression})
		}
		return bson.D{{Key: "$mergeObjects", Value: bson.A{"$" + storagePath, patch}}}, nil
	}
	if field.Type != schema.FieldTypeGroup {
		return mongoLiteral(encoded), nil
	}
	object, valid := value.CopyObject()
	if !valid || field.Nested == nil {
		return nil, fmt.Errorf("group field %q does not have a canonical object value", field.Path.String())
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	patch := make(bson.D, 0, len(keys))
	for _, key := range keys {
		child, exists := mongoFieldNamed(field.Nested.ResolvedFields(), key)
		if !exists || child.Category == schema.FieldCategoryPresentation {
			return nil, fmt.Errorf("group field %q child %q is not stored", field.Path.String(), key)
		}
		expression, err := mongoPatchValueExpression(child, storagePath+"."+key, object[key])
		if err != nil {
			return nil, err
		}
		patch = append(patch, bson.E{Key: key, Value: expression})
	}
	return bson.D{{Key: "$mergeObjects", Value: bson.A{"$" + storagePath, patch}}}, nil
}

func mongoLiteral(value any) bson.D {
	return bson.D{{Key: "$literal", Value: value}}
}
