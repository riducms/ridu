package operation

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestAccessPredicatesFollowTheQueryContractWhereRulesRun(t *testing.T) {
	collection := Collection{Schema: schema.Collection{ID: "posts-id", Slug: "posts", Fields: []schema.Field{
		{Name: "title", Type: schema.FieldTypeText},
		{Name: "secret", Type: schema.FieldTypeText, QueryRestricted: true},
		{Name: "authors", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{HasMany: true, CollectionSlug: "people"}},
	}}}
	rule := func(expression query.Expression) Access {
		return func(Context) (Decision, error) {
			node := expression.Node()
			return Decision{Kind: Where, Access: &node}, nil
		}
	}

	// A trusted rule may compare fields with read rules and use membership.
	collection.Access = map[operation.Kind]Access{operation.Read: rule(query.And(query.Equal("secret", "s"), query.In("authors", "a1")))}
	if _, err := authorize(collection, Context{Operation: operation.Read, Collection: collection.Schema}); err != nil {
		t.Fatalf("valid trusted predicate rejected: %v", err)
	}

	for _, test := range []struct {
		expression        query.Expression
		operationKind     operation.Kind
		role              operation.Kind
		path, issue, word string
	}{
		{query.Or(query.Equal("title", "t"), query.Equal("authors", "a1")), operation.Read, operation.Read, "authors", "unsupported_operator", `operator "equal"`},
		{query.Not(query.Contains("authors", "a1")), operation.DeletePermanent, operation.Delete, "authors", "unsupported_operator", `operator "contains"`},
		{query.Equal("missing", "x"), operation.Update, operation.Update, "missing", "invalid_path", `operator "equal"`},
	} {
		collection.Access = map[operation.Kind]Access{test.role: rule(test.expression)}
		_, err := authorize(collection, Context{Operation: test.operationKind, Collection: collection.Schema})
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != invalidAccessPredicateCode || failure.Status != 500 ||
			len(failure.Issues) != 1 || failure.Issues[0].Code != test.issue || failure.Issues[0].Path != test.path || failure.Issues[0].CollectionID != "posts-id" {
			t.Fatalf("%s rule error = %#v", test.role, err)
		}
		for _, fragment := range []string{`collection "posts" ` + string(test.role) + " access rule", "during the " + string(test.operationKind) + " operation", test.word} {
			if !strings.Contains(failure.Message, fragment) {
				t.Errorf("%s rule message %q lacks %q", test.role, failure.Message, fragment)
			}
		}
		// Callers that wrap rule failures keep the structured error.
		if wrapped := accessRuleError("read access rule failed", err); wrapped != failure {
			t.Errorf("accessRuleError replaced the invalid predicate error with %#v", wrapped)
		}
	}
	if wrapped := accessRuleError("read access rule failed", errors.New("rule failed")); wrapped.Code != "access_failed" || wrapped.Status != 500 {
		t.Fatalf("ordinary rule failure = %#v", wrapped)
	}
}
