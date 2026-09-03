package graphql

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

func TestTransportErrorRedactsInternalIssues(t *testing.T) {
	result := transportError(&ridu.OperationError{
		Code: "store_failed", Status: 500, Message: "postgres://user:super-secret@db",
		Issues: []schema.Issue{{Code: "internal_detail", Path: "database", Message: "token=super-secret"}},
	})
	failure, ok := result.(transportFailure)
	if !ok {
		t.Fatalf("transport error type = %T", result)
	}
	if failure.message != "internal server error" || failure.extensions["code"] != "internal_error" {
		t.Fatalf("public GraphQL error = %#v", failure.extendedError)
	}
	if _, leaked := failure.extensions["issues"]; leaked {
		t.Fatalf("internal GraphQL issues leaked: %#v", failure.extensions)
	}
}
