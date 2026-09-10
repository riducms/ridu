package field

import "strings"

// OptionFilter derives one relationship-choice predicate from current document data.
func OptionFilter(targetPath string, operator RelationshipFilterOperator, sourcePath string) RelationshipFilterRule {
	return RelationshipFilterRule{TargetPath: strings.TrimSpace(targetPath), Operator: operator, SourcePath: strings.TrimSpace(sourcePath)}
}

// OptionFilterFor limits an option-filter rule to one target of a polymorphic relationship.
func OptionFilterFor(collection, targetPath string, operator RelationshipFilterOperator, sourcePath string) RelationshipFilterRule {
	rule := OptionFilter(targetPath, operator, sourcePath)
	rule.Collection = strings.TrimSpace(collection)
	return rule
}

// OptionFilterValue compares a target field with one static scalar value.
// Literal filters are applied by both admin pickers and server-side reference admission.
func OptionFilterValue[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](targetPath string, operator RelationshipFilterOperator, value Value) RelationshipFilterRule {
	literal, err := normalizeDefault(value)
	rule := RelationshipFilterRule{TargetPath: strings.TrimSpace(targetPath), Operator: operator}
	if err == nil {
		rule.Literal = &literal
	}
	return rule
}

// OptionFilterValueFor limits a static option filter to one polymorphic target.
func OptionFilterValueFor[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](collection, targetPath string, operator RelationshipFilterOperator, value Value) RelationshipFilterRule {
	rule := OptionFilterValue(targetPath, operator, value)
	rule.Collection = strings.TrimSpace(collection)
	return rule
}

func validRelationshipFilterOperator(operator RelationshipFilterOperator) bool {
	switch operator {
	case FilterEquals, FilterNotEquals, FilterLike, FilterContains, FilterGreaterThan, FilterGreaterThanEqual, FilterLessThan, FilterLessThanEqual:
		return true
	default:
		return false
	}
}
