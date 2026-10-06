package fieldchange_test

import (
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

func sharedKindConfig(score field.Node) core.Config {
	hero := field.Block{Slug: "hero", Fields: field.Fields{score, field.Text("keep")}}
	section := field.Block{Slug: "section", Fields: field.Fields{field.Blocks("children").References("hero")}}
	return core.Config{
		Name:   "Shared kinds",
		Blocks: []field.Block{hero, section},
		Collections: []core.Collection{
			{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("section", "hero")}},
			{Slug: "posts", Fields: field.Fields{field.Blocks("body").References("hero")}},
			{Slug: "notes", Fields: field.Fields{field.Text("title")}},
		},
	}
}

// A kind change inside a shared definition is one change. It counts and
// clears the field in every stored block of the definition, at any depth, in
// every resource that places it.
func TestDefinitionKindChangeAppliesAtEveryPlacement(t *testing.T) {
	resolve := func(score field.Node) schema.Snapshot {
		manifest, err := core.Resolve(sharedKindConfig(score))
		if err != nil {
			t.Fatal(err)
		}
		return manifest.Snapshot()
	}
	before, after := resolve(field.Text("score")), resolve(field.Number("score"))
	changes := fieldchange.Detect(before, after)
	if len(changes) != 1 || changes[0].Block != "hero" || len(changes[0].Resources) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	if affected := fieldchange.AffectedResources(changes); len(affected) != 2 || affected[0].Slug != "pages" || affected[1].Slug != "posts" {
		t.Fatalf("affected resources = %#v", affected)
	}
	if description := fieldchange.Description(fieldchange.Reports(changes)[0]); description != "block hero.score" {
		t.Fatalf("description = %q", description)
	}
	if roots := changes[0].Roots(before.Collections[0]); len(roots) != 1 || roots[0].Name != "layout" {
		t.Fatalf("roots = %#v", roots)
	}
	hero := func(score string) store.Value {
		return store.Object(store.Values{"blockType": store.String("hero"), "score": store.String(score), "keep": store.String("kept")})
	}
	values := store.Values{"layout": store.List(
		store.Object(store.Values{"blockType": store.String("section"), "children": store.List(hero("deep"))}),
		hero("top"),
	)}
	reports := fieldchange.Reports(changes)
	cleared, found, err := fieldchange.Process(changes, reports, before.Collections[0], values, false, true)
	if err != nil || !found || reports[0].Documents != 1 {
		t.Fatalf("clear = %t, %#v, %v", found, reports, err)
	}
	section, _ := cleared["layout"].ListItem(0)
	children, _ := section.Lookup("children")
	deep, _ := children.ListItem(0)
	top, _ := cleared["layout"].ListItem(1)
	for _, row := range []store.Value{deep, top} {
		if _, exists := row.Lookup("score"); exists {
			t.Fatal("a stored block kept the changed field")
		}
		if keep, _ := row.Get("keep").StringValue(); keep != "kept" {
			t.Fatal("clear removed an unchanged field")
		}
	}
	if _, found, _ := fieldchange.Process(changes, fieldchange.Reports(changes), before.Collections[2], store.Values{"title": store.String("x")}, false, false); found {
		t.Fatal("a resource without the block was counted")
	}
}

// Detection follows definitions: a graph with thousands of times more
// placements costs about as much as one with fewer definitions.
func TestDetectFollowsDefinitions(t *testing.T) {
	cost := func(layers int) float64 {
		beforeConfig := blockreferences.LayeredConfig(layers, 3)
		afterConfig := blockreferences.LayeredConfig(layers, 3)
		afterConfig.Blocks = append([]field.Block(nil), afterConfig.Blocks...)
		for index := range afterConfig.Blocks {
			fields := append(field.Fields(nil), afterConfig.Blocks[index].Fields...)
			if afterConfig.Blocks[index].Slug == "leaf-0" {
				fields[1] = field.Text("count")
			}
			afterConfig.Blocks[index].Fields = fields
		}
		before, err := core.Resolve(beforeConfig)
		if err != nil {
			t.Fatal(err)
		}
		after, err := core.Resolve(afterConfig)
		if err != nil {
			t.Fatal(err)
		}
		beforeSnapshot, afterSnapshot := before.Snapshot(), after.Snapshot()
		var changes []fieldchange.Change
		allocations := testing.AllocsPerRun(3, func() { changes = fieldchange.Detect(beforeSnapshot, afterSnapshot) })
		if len(changes) != 1 || changes[0].Block != "leaf-0" {
			t.Fatalf("%d layers: changes = %#v", layers, changes)
		}
		return allocations
	}
	if shallow, deep := cost(3), cost(7); deep > 4*shallow {
		t.Fatalf("detection allocations grew from %.0f to %.0f with placements", shallow, deep)
	}
}
