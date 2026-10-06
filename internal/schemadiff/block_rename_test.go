package schemadiff_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

func resolveRenameConfig(t testing.TB, config ridu.Config) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func sharedHeroConfig(heading string, pagesSlug string) ridu.Config {
	hero := field.Block{Slug: "hero", Fields: field.Fields{field.Text(heading), field.Number("height")}}
	section := field.Block{Slug: "section", Fields: field.Fields{field.Blocks("children").References("hero")}}
	return ridu.Config{
		Name:   "Shared hero",
		Blocks: []field.Block{hero, section},
		Collections: []ridu.Collection{
			{Slug: schema.CollectionSlug(pagesSlug), Fields: field.Fields{field.Text("title"), field.Blocks("layout").References("section", "hero")}},
			{Slug: "posts", Fields: field.Fields{field.Blocks("body").References("hero")}},
		},
		Globals: []ridu.Global{{Slug: "site", Fields: field.Fields{field.Blocks("footer").References("section")}}},
	}
}

// A field renamed inside a shared definition is one candidate, however many
// placements the definition has, and its fields are definition-relative.
func TestBlockFieldRenameIsOneCandidatePerDefinition(t *testing.T) {
	before, after := resolveRenameConfig(t, sharedHeroConfig("heading", "pages")), resolveRenameConfig(t, sharedHeroConfig("headline", "pages"))
	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.Kind != schemadiff.RenameBlockField || candidate.Block != "hero" || candidate.BeforeCollection.ID != "" ||
		candidate.BeforeField.Path.String() != "heading" || candidate.AfterField.ID != "block-hero-headline" {
		t.Fatalf("block field candidate = %#v", candidate)
	}
}

// A collection rename keeps its blocks: the shapes compare the definitions a
// container selects, so a definition changed in the same save neither hides
// the collection rename nor expands its field pairs into placements.
func TestCollectionRenameKeepsSharedDefinitions(t *testing.T) {
	before, after := resolveRenameConfig(t, sharedHeroConfig("heading", "pages")), resolveRenameConfig(t, sharedHeroConfig("headline", "articles"))
	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 2 || candidates[0].Kind != schemadiff.RenameCollection || candidates[1].Kind != schemadiff.RenameBlockField {
		t.Fatalf("candidates = %#v", candidates)
	}
	if pairs := candidates[0].Fields; len(pairs) != 2 || pairs[0].After.ID != "articles-layout" || pairs[1].After.ID != "articles-title" {
		t.Fatalf("collection rename pairs = %#v", pairs)
	}
}

// Rename detection follows definitions: a graph with thousands of times more
// placements costs about as much as one with fewer definitions.
func TestRenameCandidatesFollowDefinitions(t *testing.T) {
	rename := func(layers int) (schema.Manifest, schema.Manifest) {
		before := blockreferences.LayeredConfig(layers, 3)
		after := blockreferences.LayeredConfig(layers, 3)
		after.Blocks = append([]field.Block(nil), after.Blocks...)
		after.Blocks[0].Fields = append(field.Fields{field.Text("headline").Required()}, after.Blocks[0].Fields[1:]...)
		return resolveRenameConfig(t, before), resolveRenameConfig(t, after)
	}
	cost := func(before, after schema.Manifest) float64 {
		var candidates []schemadiff.RenameCandidate
		allocations := testing.AllocsPerRun(3, func() { candidates = schemadiff.RenameCandidates(before, after) })
		if len(candidates) != 1 || candidates[0].Block != "leaf-0" {
			t.Fatalf("candidates = %#v", candidates)
		}
		return allocations
	}
	shallow, deep := cost(rename(3)), cost(rename(7))
	// 12 and 24 definitions with about 250 and 20,000 placements.
	if deep > 4*shallow {
		t.Fatalf("rename detection allocations grew from %.0f to %.0f with placements", shallow, deep)
	}
}
