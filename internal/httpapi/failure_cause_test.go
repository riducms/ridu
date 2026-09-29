package httpapi

import (
	"fmt"
	"testing"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
)

// A hook that fails because of a nested operation used to log only "after
// change hook failed"; the reason lived in its cause and issues.
func TestFailureCauseReportsWrappedErrorsAndIssues(t *testing.T) {
	reference := &operationengine.Error{Code: "validation", Status: 422, Message: "document reference validation failed",
		Issues: []schema.Issue{{Path: "learner", Message: "reference is unavailable"}}}
	hook := &operationengine.Error{Code: "hook_failed", Status: 500, Message: "after change hook failed",
		Cause: fmt.Errorf("sync learner profile: %w", reference)}

	got := failureCause(hook)
	want := "sync learner profile: document reference validation failed; learner: reference is unavailable"
	if got != want {
		t.Fatalf("failureCause = %q, want %q", got, want)
	}
	if cause := failureCause(&operationengine.Error{Message: "not found"}); cause != "" {
		t.Fatalf("an error without a cause reported %q", cause)
	}
}
