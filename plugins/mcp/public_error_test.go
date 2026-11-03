package mcp

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

func TestPublicErrorsKeepIssuePaths(t *testing.T) {
	err := callState{}.publicError(&ridu.OperationError{Code: "bad_query", Status: 400, Message: "query is not valid", Issues: []schema.Issue{
		{Code: "unknown_field", Path: "where.titel", Message: "field is not defined"},
		{Code: "invalid_sort", Message: "sort is not valid"},
	}})
	if want := "bad_query: query is not valid (where.titel: field is not defined; sort is not valid)"; err.Error() != want {
		t.Fatalf("public error = %q, want %q", err, want)
	}
}
