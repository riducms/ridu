package core_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// hookedLayeredConfig is blockreferences.LayeredConfig(layers, 3) whose leaf
// blocks' heading carries a beforeChange hook, a validator, field access
// rules, a dynamic default and a visibility condition, and whose pages layout
// also selects the leaves directly, so one document fits every layer count.
// calls counts the hook's invocations.
func hookedLayeredConfig(layers int, calls *atomic.Int64) ridu.Config {
	config := blockreferences.LayeredConfig(layers, 3)
	allow := func(ctx operation.Context) (bool, error) { return ctx.Operation != "", nil }
	heading := field.Text("heading").Required().
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
			calls.Add(1)
			if text, _ := value.Get(); strings.TrimSpace(text) != text {
				return operation.Set(strings.TrimSpace(text)), nil
			}
			return operation.Keep[string](), nil
		}}}).
		Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
			if text, _ := value.Get(); text == "TODO" {
				return []operation.Issue{{Code: "placeholder", Message: "Replace the placeholder"}}, nil
			}
			return nil, nil
		}).
		Access(field.Access{Read: allow, Create: allow, Update: allow}).
		DefaultFrom(func(operation.Context) (operation.Value[string], error) { return operation.Present("Untitled"), nil }).
		Admin(field.Admin{VisibleWhen: field.NotEqual(field.Sibling("blockName"), "hidden")})
	var selected []string
	for index, block := range config.Blocks {
		if strings.HasPrefix(block.Slug, fmt.Sprintf("layer-%d-", layers)) {
			selected = append(selected, block.Slug)
		}
		if strings.HasPrefix(block.Slug, "leaf-") {
			fields := slices.Clone(block.Fields)
			fields[0] = heading
			config.Blocks[index].Fields = fields
			selected = append(selected, block.Slug)
		}
	}
	config.Collections[1].Fields = field.Fields{field.Text("title"), field.Blocks("layout").References(selected...)}
	return config
}

// leafLayout is rows leaf blocks with a heading, a number and two links.
func leafLayout(rows int, heading string) store.Value {
	items := make([]store.Value, rows)
	for index := range items {
		links := store.List(
			store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d-a", index)), "label": store.String("First")}),
			store.Object(store.Values{"_key": store.String(fmt.Sprintf("row-%d-b", index)), "label": store.String("Second")}),
		)
		items[index] = store.Object(store.Values{
			"_key": store.String(fmt.Sprintf("row-%d", index)), "blockType": store.String(fmt.Sprintf("leaf-%d", index%3)),
			"heading": store.String(fmt.Sprintf("%s %d", heading, index)), "count": store.Number(float64(index)), "links": links,
		})
	}
	return store.List(items...)
}

type hookedWriteCost struct {
	placements int
	// allocated is the bytes one create or update of the document allocates.
	allocated uint64
	// calls is the hook's invocations for one write.
	calls int64
}

// measureHookedWrites creates and updates a layout of rows leaf blocks in the
// layered schema and reports the work of one write.
func measureHookedWrites(t testing.TB, layers, rows int) hookedWriteCost {
	t.Helper()
	var calls atomic.Int64
	config := hookedLayeredConfig(layers, &calls)
	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	placements := 0
	schema.WalkPlacements(app.Manifest().Snapshot().Collections[1].Fields, func([]string, schema.Field, bool) bool {
		placements++
		return true
	})
	ctx := context.Background()
	created, err := app.Local().Create(ctx, "pages", store.Values{"title": store.String("Hooked"), "layout": leafLayout(rows, "First")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const writes = 4
	calls.Store(0)
	allocated := allocatedBy(func() {
		for index := range writes / 2 {
			if _, err := app.Local().Create(ctx, "pages", store.Values{"title": store.String("Hooked"), "layout": leafLayout(rows, "Created")}, ridu.MutationOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": leafLayout(rows, fmt.Sprintf("Edited %d", index))}, ridu.MutationOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	})
	return hookedWriteCost{placements: placements, allocated: allocated / writes, calls: calls.Load() / writes}
}

// A field with executable behavior in a block definition is bound once, and a
// request finds its values by walking the document, so a write's cost follows
// the document's content rather than the schema's placements. Two schemas that
// place the hooked field a few hundred and tens of thousands of times write the
// same document with the same work; doubling the document doubles it. Before
// definition bindings, the larger schema bound every placement: its
// application retained about 76 MB, and a write allocated 2.8 times what it
// did in the smaller schema.
func TestHookedBlockWritesFollowContent(t *testing.T) {
	small := measureHookedWrites(t, 2, 40)
	large := measureHookedWrites(t, 7, 40)
	if large.placements < 50*small.placements {
		t.Fatalf("fixtures place %d and %d fields", small.placements, large.placements)
	}
	// The hook runs once for each row's heading, wherever the schema places it.
	if small.calls != 40 || large.calls != 40 {
		t.Fatalf("hook calls per write = %d and %d, want one per row", small.calls, large.calls)
	}
	if large.allocated > small.allocated+small.allocated/4 {
		t.Fatalf("a write allocates %d bytes with %d placements and %d with %d", small.allocated, small.placements, large.allocated, large.placements)
	}
	doubled := measureHookedWrites(t, 7, 80)
	if doubled.allocated < large.allocated*3/2 || doubled.allocated > large.allocated*5/2 {
		t.Fatalf("40 rows allocate %d bytes per write and 80 rows %d", large.allocated, doubled.allocated)
	}

	var calls atomic.Int64
	retained := heapAfter(func() any {
		app, err := ridu.New(hookedLayeredConfig(7, &calls), teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		return app
	})
	if retained > 8<<20 {
		t.Fatalf("application retains %d bytes for %d placements", retained, large.placements)
	}
	t.Logf("bytes per write: %d placements %d, %d placements %d, doubled document %d; application retains %d", small.placements, small.allocated, large.placements, large.allocated, doubled.allocated, retained)
}

// BenchmarkHookedBlockWrites reports one create or update of a 40-row layout
// whose rows' leaf blocks carry hooked fields, against schemas that place those
// fields a few hundred and tens of thousands of times.
func BenchmarkHookedBlockWrites(b *testing.B) {
	for _, layers := range []int{2, 7} {
		b.Run(fmt.Sprintf("layers=%d", layers), func(b *testing.B) {
			var calls atomic.Int64
			app, err := ridu.New(hookedLayeredConfig(layers, &calls), teststore.New())
			if err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			created, err := app.Local().Create(ctx, "pages", store.Values{"layout": leafLayout(40, "First")}, ridu.MutationOptions{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; b.Loop(); index++ {
				if index%2 == 0 {
					_, err = app.Local().Create(ctx, "pages", store.Values{"layout": leafLayout(40, "Created")}, ridu.MutationOptions{})
				} else {
					_, err = app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": leafLayout(40, fmt.Sprintf("Edited %d", index))}, ridu.MutationOptions{})
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// nestedLeafLayout nests every layer-3 > layer-2 > layer-1 > leaf combination
// of hookedLayeredConfig(3) in one layout, copies times, so the hooked heading
// appears at 81 distinct placements. Rows carry no keys, as a client submits
// new content.
func nestedLeafLayout(copies int) store.Value {
	var rows []store.Value
	for copy := range copies {
		for outer := range 3 {
			var middles []store.Value
			for middle := range 3 {
				var inners []store.Value
				for inner := range 3 {
					var leaves []store.Value
					for leaf := range 3 {
						leaves = append(leaves, store.Object(store.Values{
							"blockType": store.String(fmt.Sprintf("leaf-%d", leaf)),
							"heading":   store.String(fmt.Sprintf(" Copy %d leaf %d ", copy, leaf)),
							"links":     store.List(store.Object(store.Values{"label": store.String("Link")})),
						}))
					}
					inners = append(inners, store.Object(store.Values{"blockType": store.String(fmt.Sprintf("layer-1-%d", inner)), "children": store.List(leaves...)}))
				}
				middles = append(middles, store.Object(store.Values{"blockType": store.String(fmt.Sprintf("layer-2-%d", middle)), "children": store.List(inners...)}))
			}
			rows = append(rows, store.Object(store.Values{"blockType": store.String(fmt.Sprintf("layer-3-%d", outer)), "children": store.List(middles...)}))
		}
	}
	return store.List(rows...)
}

// A hooked block field placed at many distinct paths in one document must not
// re-admit the whole candidate once per placement: that made a write's work
// quadratic in its distinct placements and rejected ordinary pages with
// embedded_limit. Every heading carries whitespace, so the hook replaces each
// value it sees.
func TestHookedBlockWriteOfManyPlacements(t *testing.T) {
	var calls atomic.Int64
	app, err := ridu.New(hookedLayeredConfig(3, &calls), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, copies := range []int{1, 4, 16} {
		calls.Store(0)
		created, err := app.Local().Create(ctx, "pages", store.Values{"title": store.String("Nested"), "layout": nestedLeafLayout(copies)}, ridu.MutationOptions{})
		if err != nil {
			t.Fatalf("%d copies: %v", copies, err)
		}
		if got, want := calls.Load(), int64(81*copies); got != want {
			t.Fatalf("%d copies: hook ran %d times, want %d", copies, got, want)
		}
		updated, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": nestedLeafLayout(copies)}, ridu.MutationOptions{})
		if err != nil {
			t.Fatalf("%d copies update: %v", copies, err)
		}
		for _, document := range []store.Document{created, updated} {
			leaves := 0
			var visit func(store.Value)
			visit = func(row store.Value) {
				if children, nested := row.Lookup("children"); nested {
					for child := range children.Elements() {
						visit(child)
					}
					return
				}
				heading, _ := row.Get("heading").StringValue()
				if heading != strings.TrimSpace(heading) || heading == "" {
					t.Fatalf("%d copies: stored heading %q", copies, heading)
				}
				leaves++
			}
			for row := range document.Values["layout"].Elements() {
				visit(row)
			}
			if leaves != 81*copies {
				t.Fatalf("%d copies: stored %d leaves", copies, leaves)
			}
		}
	}
}
