package config_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
)

// doublingBlocks declares a block graph in which every level selects the level
// below twice, so the resource has 2^depth placements of the leaf.
func doublingBlocks(depth int, leaf field.Fields, inline bool) (field.Block, []field.Block) {
	current := field.Block{Slug: "leaf", Fields: leaf}
	registered := []field.Block{current}
	for level := 1; level <= depth; level++ {
		slug := fmt.Sprintf("level-%d", level)
		left, right := field.Blocks("left").References(current.Slug), field.Blocks("right").References(current.Slug)
		if inline {
			left, right = field.Blocks("left", current), field.Blocks("right", current)
		}
		current = field.Block{Slug: slug, Fields: field.Fields{left, right}}
		registered = append(registered, current)
	}
	return current, registered
}

// Resolution, binding and validation visit each definition once, so a graph
// with 2^30 placements resolves like a small one, declared inline or by
// reference, and its manifest stores each definition once.
func TestExponentialBlockGraphsResolvePerDefinition(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprintf("inline=%t", inline), func(t *testing.T) {
			root, registered := doublingBlocks(30, field.Fields{field.Text("title")}, inline)
			config := ridu.Config{Name: "Doubling", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", root)}}}}
			if !inline {
				config.Blocks = registered
				config.Collections[0].Fields = field.Fields{field.Blocks("layout").References(root.Slug)}
			}
			started := time.Now()
			manifest, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(started); elapsed > 5*time.Second {
				t.Fatalf("resolution took %s", elapsed)
			}
			snapshot := manifest.Snapshot()
			if len(snapshot.Blocks) != 31 {
				t.Fatalf("registry has %d definitions, want 31", len(snapshot.Blocks))
			}
			encoded, err := manifest.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > 200_000 {
				t.Fatalf("manifest is %d bytes", len(encoded))
			}
			parsed, err := schema.Parse(encoded)
			if err != nil || !parsed.Equal(manifest) {
				t.Fatalf("manifest does not parse unchanged: %v", err)
			}
			// One deep placement still has its own path and stable ID.
			segments := []string{"layout"}
			for level := 30; level >= 1; level-- {
				segments = append(segments, fmt.Sprintf("level-%d", level), "right")
			}
			segments = append(segments, "leaf", "title")
			deep, found := schema.FieldAtPath(snapshot.Collections[0].Fields, segments)
			if !found || deep.Path.String() != strings.Join(segments, ".") || deep.ID != schema.PlacementFieldID("pages", segments) {
				t.Fatalf("deep placement = %+v", deep)
			}
		})
	}
}

// Executable field behavior is bound once per block definition, like the rest
// of the definition, so a graph that places a field with a validator, a hook,
// access rules, a dynamic default and a visibility condition 2^30 times
// resolves and builds an application like a small one.
func TestBehaviorBindingsFollowDefinitions(t *testing.T) {
	hooked := field.Fields{field.Text("title").
		Validate(func(operation.Context, operation.Value[string]) ([]operation.Issue, error) { return nil, nil }).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
			return operation.Keep[string](), nil
		}}}).
		Access(field.Access{Read: func(operation.Context) (bool, error) { return true, nil }}).
		DefaultFrom(func(operation.Context) (operation.Value[string], error) { return operation.Present("Untitled"), nil }).
		Admin(field.Admin{VisibleWhen: field.NotEqual(field.Sibling("blockName"), "hidden")}),
	}
	root, registered := doublingBlocks(30, hooked, false)
	started := time.Now()
	config := ridu.Config{Name: "Doubling", Blocks: registered, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References(root.Slug)}}}}
	if _, err := ridu.Resolve(config); err != nil {
		t.Fatal(err)
	}
	if _, err := ridu.New(config, teststore.New()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("resolution and construction took %s", elapsed)
	}
}

// Placement paths through nested definitions stay within the nesting bound
// and remain valid canonical query paths.
func TestBlockGraphPlacementPathsAreBounded(t *testing.T) {
	nest := func(depth int, leaf field.Node) field.Node {
		for level := depth; level >= 1; level-- {
			leaf = field.Group(fmt.Sprintf("g%d", level), field.Fields{leaf})
		}
		return leaf
	}
	inner := field.Block{Slug: "inner", Fields: field.Fields{nest(25, field.Text("title"))}}
	outer := field.Block{Slug: "outer", Fields: field.Fields{nest(25, field.Blocks("body").References("inner"))}}
	_, err := ridu.Resolve(ridu.Config{Name: "Deep", Blocks: []field.Block{inner, outer}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("outer")}}}})
	if err == nil || !strings.Contains(err.Error(), "schema_depth_exceeded") || !strings.Contains(err.Error(), "levels deep through block graph outer -> inner") {
		t.Fatalf("Resolve error = %v, want a nesting failure naming the block graph", err)
	}
	root, registered := doublingBlocks(33, field.Fields{field.Text("title")}, false)
	_, err = ridu.Resolve(ridu.Config{Name: "Long", Blocks: registered, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References(root.Slug)}}}})
	if err == nil || !strings.Contains(err.Error(), "schema_depth_exceeded") || !strings.Contains(err.Error(), "segments through block graph level-32 -> level-31") {
		t.Fatalf("Resolve error = %v, want a path length failure naming the block graph", err)
	}
}

// Placement IDs join kebab-cased path segments with "-", so siblings whose
// names extend one another can derive one ID through their descendants. The
// check compares sibling subtrees once per scope instead of every placement.
func TestPlacementIDCollisionsAreDetectedPerScope(t *testing.T) {
	hero := field.Block{Slug: "hero", Fields: field.Fields{field.Text("wideTitle")}}
	heroWide := field.Block{Slug: "hero-wide", Fields: field.Fields{field.Text("title")}}
	for _, test := range []struct {
		name, want string
		config     ridu.Config
	}{
		{
			name: "resource field and block placement",
			want: `fields "layout.hero.wideTitle" and "layoutHeroWideTitle" of "pages" both derive stable ID "pages-layout-hero-wide-title"`,
			config: ridu.Config{Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
				field.Blocks("layout", hero), field.Text("layoutHeroWideTitle"),
			}}}},
		},
		{
			name: "sibling block definitions",
			want: `fields "layout.hero.wideTitle" and "layout.hero-wide.title" of "pages" both derive stable ID "pages-layout-hero-wide-title"`,
			config: ridu.Config{Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
				field.Blocks("layout", hero, heroWide),
			}}}},
		},
		{
			name: "definition-relative IDs",
			want: `fields "hero.wideTitle" and "hero-wide.title" of different blocks both derive definition ID "block-hero-wide-title"`,
			config: ridu.Config{Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
				field.Blocks("first", hero), field.Blocks("second", heroWide),
			}}}},
		},
		{
			name: "within a definition",
			want: `field name derives ID "block-card-seo-title", which is already used at blocks.card.fields[0].fields[0].name`,
			config: ridu.Config{Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
				field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{
					field.Group("seo", field.Fields{field.Text("title")}), field.Text("seoTitle"),
				}}),
			}}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.config.Name = "Collisions"
			_, err := ridu.Resolve(test.config)
			if err == nil || !strings.Contains(err.Error(), "duplicate_field_id") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve error = %v, want %s", err, test.want)
			}
		})
	}
	// Names that extend one another without matching descendants are distinct.
	if _, err := ridu.Resolve(ridu.Config{Name: "Distinct", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}, field.Block{Slug: "hero-wide", Fields: field.Fields{field.Text("heading")}}),
		field.Text("layoutHero"), field.Group("seo", field.Fields{field.Text("title")}), field.Text("seoDescription"),
	}}}}); err != nil {
		t.Fatal(err)
	}
}
