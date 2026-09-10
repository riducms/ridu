package schema

import (
	"fmt"
	"strings"
)

// Issue describes one configuration or document validation failure at a field path.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	// Target correlates a save issue by portable schema and stable row identity.
	// It is opaque to transports and scoped to the operation's locale/form snapshot.
	Target string `json:"target,omitempty"`
	// FieldID and resource identity describe a resolved application-validator target.
	FieldID      StableID `json:"fieldId,omitempty"`
	CollectionID StableID `json:"collectionId,omitempty"`
	GlobalID     StableID `json:"globalId,omitempty"`
	// Locale is the exact translation addressed by a save issue, without fallback.
	Locale LocaleCode `json:"locale,omitempty"`
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
