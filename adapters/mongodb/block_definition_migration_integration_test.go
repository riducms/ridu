package mongodb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// A change inside a block definition migrates every stored block of it.
func TestMongoDBBlockDefinitionMigrationsReachEveryPlacement(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	var backend *Store
	step := int64(1)
	blockreferences.RunDefinitionMigrations(t, blockreferences.MigrationHarness{
		Begin: func(t *testing.T, before schema.Manifest) store.Store {
			backend = mongoIntegrationStore(t)
			if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(step, 0), ArtifactOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			return backend
		},
		Migrate: func(t *testing.T, after schema.Manifest, candidates []schemadiff.RenameCandidate) error {
			step++
			renames := make([]ridumigration.Rename, len(candidates))
			for index, candidate := range candidates {
				renames[index] = ridumigration.Rename{Block: candidate.Block, FieldBefore: candidate.BeforeField.Path.String(), FieldAfter: candidate.AfterField.Path.String()}
			}
			created, err := CreateArtifact(ctx, directory, fmt.Sprintf("change-%d", step), after, time.Unix(step, 0), ArtifactOptions{Renames: renames})
			if err != nil {
				return err
			}
			if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
				_ = os.Remove(created.Path)
				// A refused migration leaves the store to serve the head again.
				head, _, headErr := migrationartifact.LatestManifest(directory)
				if headErr != nil {
					t.Fatal(headErr)
				}
				if verifyErr := backend.VerifyIndexes(ctx, head); verifyErr != nil {
					t.Fatal(verifyErr)
				}
				return err
			}
			return nil
		},
	})
}
