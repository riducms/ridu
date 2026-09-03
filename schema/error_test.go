package schema_test

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestValidationErrorFormatsIssuesForHumans(t *testing.T) {
	err := schema.NewValidationError([]schema.Issue{
		{Code: "missing_field", Path: "collections[0].fields[0]", Message: "field is required"},
		{Code: "duplicate_slug", Path: "collections[1].slug", Message: "slug is already used"},
	})
	want := "schema validation failed with 2 issue(s):\n" +
		"  - collections[0].fields[0] [missing_field]: field is required\n" +
		"  - collections[1].slug [duplicate_slug]: slug is already used"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}
