package mongodb

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoNestedFixture(t *testing.T) schema.Collection {
	t.Helper()
	note := field.Block{Slug: "note", Fields: field.Fields{field.Text("text"), field.Text("caption").Localized()}}
	card := field.Block{Slug: "card", Fields: field.Fields{field.Blocks("children", note)}}
	manifest, err := core.Resolve(core.Config{Name: "Nested paths",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{
			field.Blocks("layout", card),
			field.Array("outer", field.Fields{field.Group("local", field.Fields{field.Array("entries", field.Fields{field.Text("word")})}).Localized()}),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest.Snapshot().Collections[0]
}

// Each repeated level is one $elemMatch nested in its parent row's, so the
// block discriminator and the remaining path bind to the same row.
func TestMongoNestedPathsBindEachLevelInOneElementMatch(t *testing.T) {
	collection := mongoNestedFixture(t)
	path := mongoMustPath(t, "layout.card.children.note.text")
	compiled, err := compileMongoNode(collection, query.Equal(path, "x").Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	cards := mongoElementMatches(compiled, "values.layout")
	if len(cards) != 1 || !mongoContainsNestedOperator(cards[0], "blockType", "$eq", "card") {
		t.Fatalf("level one = %#v", cards)
	}
	notes := mongoElementMatches(cards[0], "children")
	if len(notes) != 1 || !mongoContainsNestedOperator(notes[0], "blockType", "$eq", "note") || !mongoContainsNestedOperator(notes[0], "text", "$eq", "x") {
		t.Fatalf("level two = %#v", notes)
	}
	if mongoContainsKey(notes[0], "$expr") {
		t.Fatalf("$expr cannot run inside $elemMatch: %#v", notes[0])
	}
	// The shape guard describes both levels' rows and checks both lists' keys.
	encoded, err := bson.MarshalExtJSON(compiled, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"$jsonSchema"`, `"enum":["card"]`, `"enum":["note"]`, `"text"`, `"$$riduRow0.children"`, `"$setUnion"`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("nested shape guard lacks %s: %s", fragment, encoded)
		}
	}
	// A type mismatch matches no row without a constant inside $elemMatch.
	compiled, err = compileMongoNode(collection, query.Not(query.Equal(path, 3)).Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mongoElementMatches(compiled, "values.layout")) != 0 || !mongoContainsConstant(compiled, false) {
		t.Fatalf("incompatible operand = %#v", compiled)
	}
}

// A localized segment reaches the first locale of the chain that fallback
// selects: one branch per locale, guarded by the earlier locales being empty.
func TestMongoNestedPathsSelectOneLocaleForEachRow(t *testing.T) {
	collection := mongoNestedFixture(t)
	scope := mongoPredicateScope{localeChain: []schema.LocaleCode{"fr", "en"}}
	for _, name := range []string{"layout.card.children.note.caption", "outer.local.entries.word"} {
		compiled, err := compileMongoNode(collection, query.Equal(mongoMustPath(t, name), "x").Node(), "filter", scope)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := bson.MarshalExtJSON(compiled, false, false)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		for _, fragment := range []string{`.fr"`, `.en"`, `"$or"`, `"$eq":""`} {
			if !strings.Contains(text, fragment) {
				t.Fatalf("%s lacks %s: %s", name, fragment, text)
			}
		}
	}
	if _, err := compileMongoNode(collection, query.Equal(mongoMustPath(t, "outer.local.entries.word"), "x").Node(), "filter", mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), "requires a locale chain") {
		t.Fatalf("localized nested path without a chain: %v", err)
	}
}
