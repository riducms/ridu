package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Link = field.Text("url").Required().Validate(validateHTTPS)

func validateHTTPS(
	// This rule only needs the value, so Go's _ ignores the context.
	_ operation.Context,
	value operation.Value[string],
) ([]operation.Issue, error) {
	url, present := value.Get()
	// Leave missing or empty values to Required; HTTPS values pass.
	if !present || url == "" || strings.HasPrefix(url, "https://") {
		return nil, nil
	}
	// Return a message the author can fix beside this field.
	return []operation.Issue{{
		Code:    "https_required",
		Message: "Use a URL that starts with https://",
	}}, nil
}
