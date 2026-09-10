package core_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestVisibilityTreeResolvesDottedReferencesInReusedLocalizedAndRepeatedFields(t *testing.T) {
	condition := field.All(
		field.Equal(field.Sibling("settings.kind"), "card"),
		field.Any(
			field.NotEqual(field.Root("branding.mode"), "hidden"),
			field.Not(field.Equal(field.Root("disabled"), true)),
		),
	)
	children := field.Fields{
		field.Group("settings", field.Fields{field.Select("kind", "card", "link")}),
		field.Text("code").Admin(field.Admin{VisibleWhen: condition}),
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name:         "Visibility",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			field.Group("branding", field.Fields{field.Text("mode")}), field.Checkbox("disabled"),
			field.Group("primary", children), field.Group("secondary", children).Localized(),
			field.Array("rows", children), field.Blocks("content", field.Block{Slug: "card", Fields: children}),
			field.Tabs(field.Fields{field.NamedTab("panel", "Panel", children)}),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var inspect func([]schema.Field)
	inspect = func(fields []schema.Field) {
		for _, candidate := range fields {
			if candidate.Name == "code" {
				count++
				condition := candidate.Admin.Condition
				if condition == nil || condition.Kind != schema.FieldConditionKindAll || len(condition.Conditions) != 2 {
					t.Fatalf("missing unified condition at %s", candidate.Path)
				}
				sibling := condition.Conditions[0].Predicate
				root := condition.Conditions[1].Conditions[0].Predicate
				if sibling.Scope != schema.FieldConditionSibling || sibling.Path.String() != "settings.kind" || root.Scope != schema.FieldConditionDocument || root.Path.String() != "branding.mode" || root.Operator != schema.FieldConditionNotEquals {
					t.Fatalf("condition reference contract changed at %s: %#v", candidate.Path, condition)
				}
			}
			inspect(schema.ChildFields(candidate))
		}
	}
	inspect(manifest.Snapshot().Collections[0].Fields)
	if count != 5 {
		t.Fatalf("verified %d reused visibility conditions, want 5", count)
	}
}

func TestVisibilityCompoundReferenceErrorsUseCurrentAuthoredPolicyPaths(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Invalid visibility", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("status"),
		field.Text("title").Admin(field.Admin{VisibleWhen: field.All(
			field.Equal(field.Sibling("status"), "published"),
			field.Any(field.Equal(field.Root("missing.title"), "x"), field.NotEqual(field.Root("status"), "hidden")),
		)}),
	}}}})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected configuration diagnostic, got %v", err)
	}
	if !slices.ContainsFunc(validationError.Issues, func(issue schema.Issue) bool {
		return issue.Code == "invalid_field_condition_path" && issue.Path == "collections[0].fields[1].admin.visibleWhen.conditions[1].conditions[0].reference.path" && strings.Contains(issue.Message, `no field named "missing"`)
	}) {
		t.Fatalf("unexpected diagnostics: %#v", validationError.Issues)
	}
	if strings.Contains(err.Error(), "occurrence") || strings.Contains(err.Error(), ".options") {
		t.Fatalf("internal diagnostic vocabulary: %v", err)
	}
}

func TestVisibilityRetainsScalarReferenceIDConditions(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Reference visibility", Collections: []ridu.Collection{
		{Slug: "authors", Fields: field.Fields{field.Text("name")}},
		{Slug: "media", Upload: true},
		{Slug: "posts", Fields: field.Fields{
			field.Relationship("author", "authors"),
			field.Upload("cover", "media"),
			field.Text("credit").Admin(field.Admin{VisibleWhen: field.Any(
				field.Equal(field.Sibling("author"), "featured-author"),
				field.NotEqual(field.Sibling("cover"), "default-cover"),
			)}),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	condition := manifest.Snapshot().Collections[2].Fields[2].Admin.Condition
	if condition == nil || condition.Conditions[0].Predicate.Values[0].Type != schema.ValueTypeString || condition.Conditions[1].Predicate.Values[0].Type != schema.ValueTypeString {
		t.Fatalf("scalar reference ID conditions lost: %#v", condition)
	}
}

func TestMalformedVisibilityReferenceHasOnlyAuthoringDiagnostics(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{Name: "Invalid visibility", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("title").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("settings..kind"), "card")}),
	}}}})
	var validationError *schema.ValidationError
	if !errors.As(err, &validationError) || len(validationError.Issues) != 1 {
		t.Fatalf("expected one authoring diagnostic, got %v", err)
	}
	issue := validationError.Issues[0]
	if issue.Code != "invalid_condition_reference" || issue.Path != "collections[0].fields[0].admin.visibleWhen.reference" || !strings.Contains(issue.Message, "field names separated by dots") {
		t.Fatalf("unhelpful reference diagnostic: %#v", issue)
	}
}
