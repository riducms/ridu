package mongodb

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// Planning a change inside block definitions follows definitions. The deep
// layered graph has twice the definitions of the shallow one and about 80
// times its placements; planning that visited placements cost hundreds of
// times more there.
func TestMongoDBPlanningFollowsDefinitions(t *testing.T) {
	ctx := context.Background()
	plan := func(layers int) map[string]float64 {
		costs := make(map[string]float64)
		for _, change := range blockreferences.PlanningChanges(layers, 3) {
			before, err := ridu.Resolve(change.Before)
			if err != nil {
				t.Fatal(err)
			}
			after, err := ridu.Resolve(change.After)
			if err != nil {
				t.Fatal(err)
			}
			var renames []ridumigration.Rename
			for _, candidate := range schemadiff.RenameCandidates(before, after) {
				renames = append(renames, ridumigration.Rename{Block: candidate.Block, FieldBefore: candidate.BeforeField.Path.String(), FieldAfter: candidate.AfterField.Path.String()})
			}
			costs[change.Name] = testing.AllocsPerRun(2, func() {
				if _, err := buildMongoDBArtifact(ctx, change.Name, &before, after, ArtifactOptions{Renames: renames}); err != nil {
					t.Fatalf("%s: %v", change.Name, err)
				}
			})
		}
		return costs
	}
	shallow, deep := plan(3), plan(7)
	for name, cost := range deep {
		if cost > 4*shallow[name] {
			t.Errorf("%s planning allocations grew from %.0f to %.0f with placements", name, shallow[name], cost)
		}
	}
}
