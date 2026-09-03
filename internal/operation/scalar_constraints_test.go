package operation

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestScalarConstraintsUseUnicodeCodePointsAndStableIssuePaths(t *testing.T) {
	titlePath, _ := query.ParsePath("title")
	scorePath, _ := query.ParsePath("score")
	two, three := 2, 3
	minimum, maximum, step := 1.5, 4.5, 2.0
	fields := []schema.Field{
		{
			ID: "title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
			Admin: schema.FieldAdmin{Label: "Title"}, Text: &schema.TextField{MinLength: &two, MaxLength: &three},
		},
		{
			ID: "score", Name: "score", Path: scorePath, Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar,
			Admin: schema.FieldAdmin{Label: "Score"}, Number: &schema.NumberField{Min: &minimum, Max: &maximum, Step: &step},
		},
	}

	_, issues := validate(fields, store.Values{"title": store.String("🙂"), "score": store.Number(1)}, true, nil)
	assertIssue(t, issues, "min_length", "title")
	assertIssue(t, issues, "min_value", "score")

	_, issues = validate(fields, store.Values{"title": store.String("🙂界éa"), "score": store.Number(5)}, true, nil)
	assertIssue(t, issues, "max_length", "title")
	assertIssue(t, issues, "max_value", "score")

	// Step is deliberately input metadata: 2.25 is inside the inclusive bounds
	// even though it is not divisible by the configured increment.
	_, issues = validate(fields, store.Values{"title": store.String("🙂界"), "score": store.Number(2.25)}, true, nil)
	if len(issues) != 0 {
		t.Fatalf("valid constrained values issues = %#v", issues)
	}
}

func assertIssue(t *testing.T, issues []schema.Issue, code, path string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.Path == path {
			return
		}
	}
	t.Fatalf("issues = %#v, want %s at %s", issues, code, path)
}
