package adoption

import (
	"slices"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestDocumentationFieldCatalogUsesItsIntendedConstructors(t *testing.T) {
	got := make([]string, 0, len(Fields()))
	for _, node := range Fields() {
		view := field.Snapshot(node)
		identity := string(view.Kind())
		if _, configured := view.SlugSource(); configured {
			identity = "slug"
		}
		got = append(got, identity)
	}
	slices.Sort(got)
	want := []string{
		"array", "blocks", "checkbox", "code", "collapsible", "date", "email", "group",
		"join", "json", "number", "plugin", "point", "radio", "relationship", "row", "select",
		"slug", "tabs", "text", "textarea", "ui", "upload", "virtual",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("documentation field constructor identities = %v, want %v", got, want)
	}
}
