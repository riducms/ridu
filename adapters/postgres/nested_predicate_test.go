package postgres

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// Consecutive repeated levels share one jsonpath row source; a localized
// container between levels needs SQL locale fallback and starts another.
func TestPostgresNestedPathsEnumerateRowsPerHop(t *testing.T) {
	note := field.Block{Slug: "note", Fields: field.Fields{field.Text("text")}}
	card := field.Block{Slug: "card", Fields: field.Fields{field.Blocks("children", note)}}
	manifest, err := core.Resolve(core.Config{Name: "Nested hops",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
			field.Blocks("layout", card),
			field.Array("outer", field.Fields{field.Group("local", field.Fields{field.Array("entries", field.Fields{field.Text("word")})}).Localized()}),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	compile := func(value string) (string, []any) {
		t.Helper()
		path, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr", "en"}}
		compiled, err := compiler.compile(query.Equal(path, "x").Node())
		if err != nil {
			t.Fatal(err)
		}
		return compiled, compiler.arguments
	}

	compiled, arguments := compile("layout.card.children.note.text")
	if strings.Count(compiled, "jsonb_path_query(") != 1 || strings.Contains(compiled, "jsonb_array_elements(") {
		t.Fatalf("two block levels use one jsonpath row source: %s", compiled)
	}
	wantPath := `strict $ ? (@.type() == "array")[*] ? (@."blockType" == $block0 && exists(@."children"))."children" ? (@.type() == "array")[*] ? (@."blockType" == $block1)`
	if len(arguments) != 3 || arguments[0] != wantPath || arguments[1] != `{"block0":"card","block1":"note"}` || arguments[2] != "x" {
		t.Fatalf("jsonpath arguments = %#v", arguments)
	}

	compiled, _ = compile("outer.local.entries.word")
	if strings.Count(compiled, "jsonb_array_elements(") != 2 || strings.Contains(compiled, "jsonb_path_query(") {
		t.Fatalf("a localized container between levels starts a second row source: %s", compiled)
	}
	for _, fragment := range []string{"ridu_item.value #> '{local,fr}'", "ridu_item.value #> '{local,en}'", "AS ridu_item_2(value)", "ridu_item_2.value #>> '{word}'"} {
		if !strings.Contains(compiled, fragment) {
			t.Fatalf("localized hop predicate %s is missing %q", compiled, fragment)
		}
	}
}
