package field_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// The TypeScript normalizer in @riducms/protocol checks the same fixture, so a
// preview in the admin or an app matches what the server stores.
func TestNormalizeSlugMatchesTheSharedProtocolFixture(t *testing.T) {
	encoded, err := os.ReadFile(filepath.Join("..", "testdata", "protocol", "slug.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Input, Slug string }
	if err := json.Unmarshal(encoded, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("slug fixture = %d cases, %v", len(cases), err)
	}
	for _, test := range cases {
		if actual := field.NormalizeSlug(test.Input); actual != test.Slug {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", test.Input, actual, test.Slug)
		}
	}
}
