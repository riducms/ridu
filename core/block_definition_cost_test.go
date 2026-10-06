package core_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// layeredLayout nests one row of each slug inside the previous one's children,
// keyed by its slug.
func layeredLayout(slugs []string, heading string) store.Value {
	last := len(slugs) - 1
	row := store.Values{"_key": store.String(slugs[last]), "blockType": store.String(slugs[last]), "heading": store.String(heading)}
	for index := last - 1; index >= 0; index-- {
		row = store.Values{"_key": store.String(slugs[index]), "blockType": store.String(slugs[index]), "children": store.List(store.Object(row))}
	}
	return store.List(store.Object(row))
}

// A registered block's schema work is shared by its placements. In this
// layered graph 24 definitions have about 56,000 placements; the application
// must not retain, or a request walk, anything for each of them. Per-placement
// work costs hundreds of bytes per placement: before definition views this
// application retained 67 MB and allocated 6 MB for each small request.
func TestRegisteredBlockWorkFollowsDefinitions(t *testing.T) {
	config := blockreferences.LayeredConfig(7, 3)
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	placements := 0
	schema.WalkPlacements(snapshot.Collections[1].Fields, func([]string, schema.Field, bool) bool {
		placements++
		return true
	})
	if placements < 50000 || len(snapshot.Blocks) != 24 {
		t.Fatalf("fixture has %d placements and %d definitions", placements, len(snapshot.Blocks))
	}

	retained := heapAfter(func() any {
		app, err := ridu.New(config, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		return app
	})
	if retained > 8<<20 {
		t.Fatalf("application retains %d bytes for %d placements", retained, placements)
	}

	app, err := ridu.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	chain := []string{"layer-7-1", "layer-6-0", "layer-5-2", "layer-4-1", "layer-3-0", "layer-2-0", "layer-1-2", "leaf-0"}
	created, err := app.Local().Create(ctx, "pages", store.Values{"title": store.String("Layered"), "layout": layeredLayout(chain, "First")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const updates = 5
	allocated := allocatedBy(func() {
		for range updates {
			if _, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": layeredLayout(chain, "Edited")}, ridu.MutationOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := app.Local().Find(ctx, "pages", created.ID, ridu.FindOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	})
	if perRequest := allocated / (2 * updates); perRequest > 1<<20 {
		t.Fatalf("a request for a small document allocates %d bytes across %d placements", perRequest, placements)
	}
}

// Identity validation walks the submitted rows instead of every placement, and
// still rejects a retained row whose block type changes at any depth.
func TestRegisteredBlockIdentityAtDepth(t *testing.T) {
	ctx := context.Background()
	app, err := ridu.New(blockreferences.LayeredConfig(3, 2), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	chain := []string{"layer-3-0", "layer-2-1", "layer-1-0", "leaf-1"}
	created, err := app.Local().Create(ctx, "pages", store.Values{"layout": layeredLayout(chain, "First")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	replacements := []string{"layer-3-1", "layer-2-0", "layer-1-1", "leaf-0"}
	for depth := range chain {
		changed := append([]string(nil), chain...)
		changed[depth] = replacements[depth]
		// Keeping the original key claims the stored row's identity.
		layout := rekeyed(layeredLayout(changed, "Changed"), depth, chain[depth])
		_, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": layout}, ridu.MutationOptions{})
		want := "layout.0" + strings.Repeat(".children.0", depth) + ".blockType"
		var failure *ridu.OperationError
		if !errors.As(err, &failure) || len(failure.Issues) != 1 || failure.Issues[0].Code != "block_type_identity" || failure.Issues[0].Path != want {
			t.Fatalf("depth %d returned %v", depth, err)
		}
	}
	// A new key is a new row, so the same change is accepted.
	changed := append([]string(nil), chain...)
	changed[2] = replacements[2]
	if _, err := app.Local().Update(ctx, "pages", created.ID, store.Values{"layout": layeredLayout(changed, "Replaced")}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
}

func rekeyed(layout store.Value, depth int, key string) store.Value {
	rows, _ := layout.CopyList()
	row, _ := rows[0].CopyObject()
	if depth == 0 {
		row["_key"] = store.String(key)
	} else {
		row["children"] = rekeyed(row["children"], depth-1, key)
	}
	rows[0] = store.Object(row)
	return store.List(rows...)
}

func heapAfter(build func() any) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	value := build()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(value)
	if after.HeapAlloc < before.HeapAlloc {
		return 0
	}
	return after.HeapAlloc - before.HeapAlloc
}

func allocatedBy(run func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
