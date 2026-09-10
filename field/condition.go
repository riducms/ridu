package field

import (
	"encoding/json"
	"fmt"
	"strings"
)

// All requires every child condition to match.
func All(conditions ...Condition) Condition {
	return Condition{kind: ConditionKindAll, conditions: cloneConditions(conditions)}
}

// Any requires at least one child condition to match.
func Any(conditions ...Condition) Condition {
	return Condition{kind: ConditionKindAny, conditions: cloneConditions(conditions)}
}

// Not negates one child condition.
func Not(condition Condition) Condition {
	return Condition{kind: ConditionKindNot, conditions: []Condition{cloneCondition(condition)}}
}

// Equal compares a referenced scalar field with one finite value.
func Equal[Value ConditionScalar](reference Reference, value Value) Condition {
	return conditionPredicate(reference, ConditionEquals, value)
}

// NotEqual compares a referenced scalar field with one finite value.
func NotEqual[Value ConditionScalar](reference Reference, value Value) Condition {
	return conditionPredicate(reference, ConditionNotEquals, value)
}

// OneOf matches any configured finite value of one scalar type.
func OneOf[Value ConditionScalar](reference Reference, values ...Value) Condition {
	return conditionPredicate(reference, ConditionOneOf, values...)
}

func conditionPredicate[Value ConditionScalar](reference Reference, operator ConditionOperator, values ...Value) Condition {
	condition := Condition{kind: ConditionKindPredicate, reference: reference, operator: operator}
	condition.values = make([]DefaultValue, len(values))
	for index, value := range values {
		normalized, err := normalizeDefault(value)
		if err != nil {
			condition.issues = append(condition.issues, Issue{
				Code: "invalid_condition_value", Path: fmt.Sprintf("values[%d]", index), Message: "condition values must be finite scalars",
			})
			continue
		}
		condition.values[index] = normalized
	}
	return condition
}

func validateCondition(builder *declarationIssues, condition Condition, path string, depth int, nodes *int) {
	const (
		maximumDepth = 32
		maximumNodes = 256
	)
	*nodes++
	if depth > maximumDepth {
		builder.issue("condition_too_deep", path, fmt.Sprintf("field condition supports at most %d nested levels", maximumDepth))
		return
	}
	if *nodes > maximumNodes {
		builder.issue("condition_too_large", path, fmt.Sprintf("field condition supports at most %d nodes", maximumNodes))
		return
	}
	for _, issue := range condition.issues {
		builder.issue(issue.Code, joinIssuePath(path, issue.Path), issue.Message)
	}
	switch condition.kind {
	case ConditionKindAll, ConditionKindAny:
		if len(condition.conditions) < 2 {
			builder.issue("invalid_condition_group", path+".conditions", "all and any conditions require at least two children")
		}
		if !condition.reference.IsZero() || condition.operator != "" || len(condition.values) != 0 {
			builder.issue("invalid_condition_shape", path, "logical conditions cannot contain predicate properties")
		}
	case ConditionKindNot:
		if len(condition.conditions) != 1 {
			builder.issue("invalid_condition_group", path+".conditions", "not conditions require exactly one child")
		}
		if !condition.reference.IsZero() || condition.operator != "" || len(condition.values) != 0 {
			builder.issue("invalid_condition_shape", path, "not conditions cannot contain predicate properties")
		}
	case ConditionKindPredicate:
		if len(condition.conditions) != 0 {
			builder.issue("invalid_condition_shape", path, "predicate conditions cannot contain child conditions")
		}
		if err := condition.reference.Err(); err != nil {
			builder.issue("invalid_condition_reference", path+".reference", err.Error())
		}
		switch condition.operator {
		case ConditionEquals, ConditionNotEquals:
			if len(condition.values) != 1 {
				builder.issue("invalid_condition_values", path+".values", "equals and notEquals conditions require exactly one value")
			}
		case ConditionOneOf:
			if len(condition.values) == 0 {
				builder.issue("invalid_condition_values", path+".values", "oneOf conditions require at least one value")
			}
		default:
			builder.issue("invalid_condition_operator", path+".operator", fmt.Sprintf("unsupported condition operator %q", condition.operator))
		}
		seen := make(map[string]struct{}, len(condition.values))
		for index, value := range condition.values {
			key := string(value.Kind()) + "\x00" + value.String()
			if _, duplicate := seen[key]; duplicate {
				builder.issue("duplicate_condition_value", fmt.Sprintf("%s.values[%d]", path, index), "condition value was configured more than once")
			}
			seen[key] = struct{}{}
			if index > 0 && value.Kind() != condition.values[0].Kind() {
				builder.issue("mixed_condition_values", fmt.Sprintf("%s.values[%d]", path, index), "oneOf condition values must use one scalar type")
			}
		}
	default:
		builder.issue("invalid_condition_kind", path+".kind", fmt.Sprintf("unsupported condition kind %q", condition.kind))
	}
	for index, child := range condition.conditions {
		validateCondition(builder, child, fmt.Sprintf("%s.conditions[%d]", path, index), depth+1, nodes)
	}
}

func joinIssuePath(prefix, suffix string) string {
	if suffix == "" {
		return prefix
	}
	return prefix + "." + suffix
}

// Err checks a condition's finite declaration without resolving its references.
func (condition Condition) Err() error {
	if condition.IsZero() {
		return nil
	}
	issues := &declarationIssues{}
	nodes := 0
	validateCondition(issues, condition, "visibleWhen", 0, &nodes)
	if len(issues.issues) == 0 {
		return nil
	}
	messages := make([]string, len(issues.issues))
	for index, issue := range issues.issues {
		messages[index] = issue.Path + ": " + issue.Message
	}
	return fmt.Errorf("%s", strings.Join(messages, "; "))
}

// MarshalJSON serializes the finite authoring tree without resolving references.
func (condition Condition) MarshalJSON() ([]byte, error) {
	if condition.IsZero() {
		return []byte("null"), nil
	}
	if err := condition.Err(); err != nil {
		return nil, err
	}
	if condition.kind == ConditionKindPredicate {
		values := make([]json.RawMessage, len(condition.values))
		for index, value := range condition.values {
			if value.Kind() == DefaultString {
				values[index], _ = json.Marshal(value.String())
			} else {
				values[index] = json.RawMessage(value.String())
			}
		}
		return json.Marshal(struct {
			Kind      ConditionKind     `json:"kind"`
			Reference Reference         `json:"reference"`
			Operator  ConditionOperator `json:"operator"`
			Values    []json.RawMessage `json:"values"`
		}{condition.kind, condition.reference, condition.operator, values})
	}
	return json.Marshal(struct {
		Kind       ConditionKind `json:"kind"`
		Conditions []Condition   `json:"conditions"`
	}{condition.kind, condition.conditions})
}
