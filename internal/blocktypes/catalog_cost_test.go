package blocktypes_test

import (
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// The catalog visits each registered definition once per locale context. In
// this graph of 24 definitions and about 56,000 placements, a per-placement
// catalog indexed 3,280 containers and made over five million allocations.
func TestCatalogFollowsDefinitions(t *testing.T) {
	manifest, err := core.Resolve(blockreferences.LayeredConfig(7, 3))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	var catalog *blocktypes.Catalog
	allocations := testing.AllocsPerRun(3, func() {
		if catalog, err = blocktypes.Build(snapshot); err != nil {
			t.Fatal(err)
		}
	})
	// One root container and the children container of each of 21 layers.
	if len(catalog.Variants) != 24 || len(catalog.Fields) != 22 {
		t.Fatalf("catalog has %d variants and %d containers", len(catalog.Variants), len(catalog.Fields))
	}
	if allocations > 15000 {
		t.Fatalf("Build made %.0f allocations", allocations)
	}
}
