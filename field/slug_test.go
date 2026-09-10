package field_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestSlugUsesRequiredUniqueIndexedTextStorage(t *testing.T) {
	definition := field.Snapshot(field.Slug("slug", "seo.title").Label("URL slug"))
	source, configured := definition.SlugSource()
	if definition.Kind() != field.KindText || source != "seo.title" || !configured {
		t.Fatalf("slug identity = %q %q %t", definition.Kind(), source, configured)
	}
	if !definition.Required() || !definition.Unique() || !definition.Index() {
		t.Fatalf("slug constraints = required:%t unique:%t index:%t", definition.Required(), definition.Unique(), definition.Index())
	}
	if definition.Label() != "URL slug" || len(definition.Issues()) != 0 {
		t.Fatalf("slug options/issues = %q %#v", definition.Label(), definition.Issues())
	}
}

func TestSlugRejectsEmptySourceLocalizationAndDefaults(t *testing.T) {
	definition := field.Snapshot(field.Slug("slug", " ").Localized().Default("fallback"))
	var codes []string
	for _, issue := range definition.Issues() {
		codes = append(codes, issue.Code)
	}
	want := []string{"missing_slug_source", "unsupported_slug_localization", "unsupported_slug_default"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("issue codes = %v, want %v", codes, want)
	}
}

func TestNormalizeSlugIsDeterministic(t *testing.T) {
	tests := map[string]string{
		"  Hello,  WORLD!  ": "hello-world",
		"already--slugged":   "already-slugged",
		"snake_case":         "snake_case",
		"Café & Tea":         "caf-tea",
		"---":                "",
	}
	for input, expected := range tests {
		if actual := field.NormalizeSlug(input); actual != expected {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", input, actual, expected)
		}
	}
}
