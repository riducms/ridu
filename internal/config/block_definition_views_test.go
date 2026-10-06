package config_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

// Every placement of a registered block shares one definition view for its
// locale scope, including placements reached through other definitions, so
// work keyed by those views follows definitions. Placement views still carry
// each placement's own persistence identity.
func TestRegisteredBlockPlacementsShareDefinitionViews(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name:         "Definition views",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Blocks: []field.Block{
			{Slug: "leaf", Fields: field.Fields{field.Text("heading").Localized()}},
			{Slug: "card", Fields: field.Fields{field.Text("title"), field.Blocks("children").References("leaf")}},
		},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			field.Blocks("layout").References("card", "leaf"),
			field.Blocks("sidebar").References("card"),
			field.Group("translated", field.Fields{field.Blocks("body").References("card")}).Localized(),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	fields := snapshot.Collections[0].Fields
	definition := func(blocks *schema.BlocksField, slug string) schema.BlockType {
		t.Helper()
		block, ok := blocks.Definition(slug)
		if !ok {
			t.Fatalf("missing definition %q", slug)
		}
		return block
	}
	same := func(left, right []schema.Field) bool { return len(left) > 0 && &left[0] == &right[0] }

	layoutCard, sidebarCard := definition(fields[0].Blocks, "card"), definition(fields[1].Blocks, "card")
	if !same(layoutCard.ResolvedFields(), sidebarCard.ResolvedFields()) {
		t.Fatal("two placements of card do not share its definition view")
	}
	nestedLeaf, layoutLeaf := definition(layoutCard.ResolvedFields()[1].Blocks, "leaf"), definition(fields[0].Blocks, "leaf")
	if !same(nestedLeaf.ResolvedFields(), layoutLeaf.ResolvedFields()) || !nestedLeaf.ResolvedFields()[0].Localized {
		t.Fatal("a placement through another definition does not share the leaf definition view")
	}
	if title := layoutCard.ResolvedFields()[0]; title.Path.String() != "title" {
		t.Fatalf("definition view path = %q, want definition-relative", title.Path)
	}

	// A localized ancestor stores one child tree per locale, so its placements
	// share a separate view with descendant localization cleared.
	translatedCard := definition(fields[2].Nested.ResolvedFields()[0].Blocks, "card")
	translatedLeaf := definition(translatedCard.ResolvedFields()[1].Blocks, "leaf")
	if same(translatedCard.ResolvedFields(), layoutCard.ResolvedFields()) || translatedLeaf.ResolvedFields()[0].Localized {
		t.Fatal("a localized ancestor shares the default locale scope")
	}

	// Placement views keep each placement's own identity.
	layoutTitle, ok := schema.FieldAtPath(fields, []string{"layout", "card", "title"})
	sidebarTitle, sidebarOK := schema.FieldAtPath(fields, []string{"sidebar", "card", "title"})
	if !ok || !sidebarOK || layoutTitle.ID == sidebarTitle.ID || layoutTitle.ID != schema.PlacementFieldID(snapshot.Collections[0].ID, []string{"layout", "card", "title"}) {
		t.Fatalf("placement identities %q and %q", layoutTitle.ID, sidebarTitle.ID)
	}
	placements := map[schema.StableID]bool{}
	schema.WalkPlacements(fields, func(segments []string, placed schema.Field, shared bool) bool {
		if shared && placed.Name == "title" {
			placements[schema.PlacementFieldID(snapshot.Collections[0].ID, segments)] = true
		}
		return true
	})
	if len(placements) != 3 || !placements[layoutTitle.ID] || !placements[sidebarTitle.ID] {
		t.Fatalf("title placements %v", placements)
	}
}
