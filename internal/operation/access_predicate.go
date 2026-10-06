package operation

import (
	"errors"
	"fmt"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// invalidAccessPredicateCode identifies an access rule that returned a
// predicate outside the query contract. It is an application defect, not a
// caller error, so the operation fails closed with a server error.
const invalidAccessPredicateCode = "invalid_access_predicate"

// validateAccessPredicate applies the caller query contract to a trusted
// access predicate: membership operators on set-valued fields and canonical
// schema paths. Field read rules do not apply, because the rule itself is
// trusted configuration. Validating here, where rules are evaluated, keeps a
// store from compiling the predicate or reporting an anonymous failure.
func validateAccessPredicate(collection Collection, role, kind operation.Kind, node query.Node) error {
	if node.Comparison != nil {
		comparison := *node.Comparison
		if err := authorizeQueryNode(collection, true, query.Node{Kind: query.ExpressionComparison, Comparison: &comparison}); err != nil {
			return invalidAccessPredicateError(collection, role, kind, comparison, err)
		}
	}
	for _, child := range node.Children {
		if err := validateAccessPredicate(collection, role, kind, child); err != nil {
			return err
		}
	}
	return nil
}

func invalidAccessPredicateError(collection Collection, role, kind operation.Kind, comparison query.Comparison, cause error) error {
	issueCode, reason := "invalid_predicate", cause.Error()
	var rejected *Error
	if errors.As(cause, &rejected) && len(rejected.Issues) == 1 {
		issueCode, reason = rejected.Issues[0].Code, rejected.Issues[0].Message
	}
	path := comparison.Path.String()
	return &Error{
		Code: invalidAccessPredicateCode, Status: 500, Cause: cause,
		Message: fmt.Sprintf("collection %q %s access rule returned an invalid predicate during the %s operation: operator %q on %q: %s",
			collection.Schema.Slug, role, kind, comparison.Operator, path, reason),
		Issues: []schema.Issue{{
			Code: issueCode, Path: path, CollectionID: collection.Schema.ID,
			Message: fmt.Sprintf("the %s access rule cannot compare %q with operator %q: %s", role, path, comparison.Operator, reason),
		}},
	}
}

// accessRuleError reports an access rule that could not be evaluated. An
// invalid predicate keeps its own error, which names the collection, the
// rule, the operation and the offending comparison.
func accessRuleError(message string, cause error) *Error {
	var invalid *Error
	if errors.As(cause, &invalid) && invalid.Code == invalidAccessPredicateCode {
		return invalid
	}
	return &Error{Code: "access_failed", Status: 500, Message: message, Cause: cause}
}
