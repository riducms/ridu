package blockgraph_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

func resolve(t testing.TB, config core.Config) schema.Snapshot {
	t.Helper()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return manifest.Snapshot()
}

// The graph records each definition view once. A layered graph of 24
// definitions places them along about 20,000 paths; a graph that visited
// placements would cost thousands of times more than one of 15 definitions.
func TestGraphFollowsDefinitions(t *testing.T) {
	deep, shallow := resolve(t, blockreferences.LayeredConfig(7, 3)), resolve(t, blockreferences.LayeredConfig(4, 3))
	graph := blockgraph.New(deep)
	if keys := graph.Keys(); len(keys) != 24 {
		t.Fatalf("graph has %d views", len(keys))
	}
	depths := graph.Depths()
	if depths["layer-7-0"] != 0 || depths["layer-1-0"] != 6 || depths["leaf-0"] != 7 {
		t.Fatalf("depths = %v", depths)
	}
	pages, _ := graph.ResourceOf(deep.Collections[1])
	if placed := pages.Placed(graph, false); len(placed) != 24 {
		t.Fatalf("pages places %d views", len(placed))
	}
	cost := func(snapshot schema.Snapshot) float64 {
		return testing.AllocsPerRun(3, func() {
			graph := blockgraph.New(snapshot)
			graph.Depths()
			for _, resource := range graph.Resources() {
				resource.Placed(graph, true)
			}
		})
	}
	if deepCost, shallowCost := cost(deep), cost(shallow); deepCost > 3*shallowCost {
		t.Fatalf("graph allocations grew from %.0f to %.0f with placements", shallowCost, deepCost)
	}
}

func scopedConfig() core.Config {
	hero := field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Localized(), field.Group("cta", field.Fields{field.Text("label")})}}
	section := field.Block{Slug: "section", Fields: field.Fields{field.Blocks("children").References("hero")}}
	return core.Config{
		Name:         "Scopes",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Blocks:       []field.Block{hero, section},
		Collections: []core.Collection{
			{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("section", "hero"), field.Blocks("translated").References("hero").Localized()}},
			{Slug: "notes", Fields: field.Fields{field.Text("title")}},
		},
	}
}

// A definition placed beneath a localized ancestor has a second view, and
// only resources that survive a change share placements.
func TestGraphSeparatesLocalizationScopesAndSurvivors(t *testing.T) {
	snapshot := resolve(t, scopedConfig())
	graph := blockgraph.New(snapshot)
	want := []blockgraph.Key{{Slug: "hero"}, {Slug: "hero", Localized: true}, {Slug: "section"}}
	if keys := graph.Keys(); !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v", keys)
	}
	localized, _ := graph.View(blockgraph.Key{Slug: "hero", Localized: true})
	plain, _ := graph.View(blockgraph.Key{Slug: "hero"})
	if localized.ResolvedFields()[0].Localized || !plain.ResolvedFields()[0].Localized {
		t.Fatal("views do not carry their localization scope")
	}
	after := snapshot
	after.Collections = []schema.Collection{snapshot.Collections[1]}
	if shared := blockgraph.Shared(graph, blockgraph.New(after), nil, false); len(shared) != 0 {
		t.Fatalf("a removed resource shares %v", shared)
	}
	if shared := blockgraph.Shared(graph, graph, nil, false); len(shared) != 3 {
		t.Fatalf("shared = %v", shared)
	}
}

// The walker finds every stored block of a chosen definition at any depth,
// in each translation of a localized container, and rewrites copies only.
func TestWalkerRewritesEveryStoredBlock(t *testing.T) {
	snapshot := resolve(t, scopedConfig())
	graph := blockgraph.New(snapshot)
	hero := func(heading store.Value) store.Value {
		return store.Object(store.Values{"blockType": store.String("hero"), "heading": heading, "cta": store.Object(store.Values{"label": store.String("Go")})})
	}
	values := store.Values{
		"layout": store.List(
			store.Object(store.Values{"blockType": store.String("section"), "children": store.List(hero(store.Object(store.Values{"en": store.String("Nested")})))}),
			hero(store.Object(store.Values{"en": store.String("Top")})),
		),
		"translated": store.Object(store.Values{
			"en": store.List(hero(store.String("English"))),
			"fr": store.List(hero(store.String("Français"))),
		}),
		"title": store.String("not a block"),
	}
	original := store.CloneValues(values)
	walker := graph.Walker(map[string]bool{"hero": true})
	pages := snapshot.Collections[0]
	var locales []schema.LocaleCode
	if err := walker.Visit(pages.Fields, func(name string) (store.Value, bool) { value, exists := values[name]; return value, exists }, "", func(row blockgraph.Row, _ store.Value) error {
		locales = append(locales, row.Locale)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(locales, []schema.LocaleCode{"", "", "en", "fr"}) {
		t.Fatalf("visited hero rows in locales %v", locales)
	}
	rewritten, changed, err := walker.Rewrite(pages.Fields, values, "", func(row blockgraph.Row, item store.Values) (bool, error) {
		item["headline"] = item["heading"]
		delete(item, "heading")
		return true, nil
	})
	if err != nil || !changed {
		t.Fatalf("rewrite = %t, %v", changed, err)
	}
	if !reflect.DeepEqual(values, original) {
		t.Fatal("rewrite changed its input")
	}
	count := 0
	_ = walker.Visit(pages.Fields, func(name string) (store.Value, bool) { value, exists := rewritten[name]; return value, exists }, "", func(_ blockgraph.Row, item store.Value) error {
		if _, renamed := item.Lookup("headline"); renamed {
			count++
		}
		if _, kept := item.Lookup("heading"); kept {
			t.Fatal("a stored block kept the old key")
		}
		return nil
	})
	if count != 4 {
		t.Fatalf("renamed %d of 4 stored blocks", count)
	}
}
