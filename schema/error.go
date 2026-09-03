package schema

import (
	"fmt"
	"strings"
)

// Issue is one path-aware schema configuration failure.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError contains every schema issue found during one deterministic
// validation pass.
type ValidationError struct {
	Issues []Issue `json:"issues"`
}

func (validationError *ValidationError) Error() string {
	if validationError == nil || len(validationError.Issues) == 0 {
		return "schema validation failed"
	}

	var message strings.Builder
	fmt.Fprintf(&message, "schema validation failed with %d issue(s):", len(validationError.Issues))
	for _, issue := range validationError.Issues {
		fmt.Fprintf(&message, "\n  - %s [%s]: %s", issue.Path, issue.Code, issue.Message)
	}
	return message.String()
}

// NewValidationError copies issues into an inspectable validation error.
func NewValidationError(issues []Issue) *ValidationError {
	return &ValidationError{Issues: append([]Issue(nil), issues...)}
}
