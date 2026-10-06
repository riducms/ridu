package sqlite

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// A change inside a block definition migrates every stored block of it, and
// a rollback moves the content back.
func TestSQLiteBlockDefinitionMigrationsReachEveryPlacement(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	var backend *Store
	step := int64(1)
	blockreferences.RunDefinitionMigrations(t, blockreferences.MigrationHarness{
		Begin: func(t *testing.T, before schema.Manifest) store.Store {
			if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(step, 0), false); err != nil {
				t.Fatal(err)
			}
			backend = newSQLiteMigrationStore(t)
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			return backend
		},
		Migrate: func(t *testing.T, after schema.Manifest, candidates []schemadiff.RenameCandidate) error {
			step++
			name := fmt.Sprintf("change-%d", step)
			var created CreatedArtifact
			var err error
			if len(candidates) != 0 {
				renames := make([]ridumigration.Rename, len(candidates))
				for index, candidate := range candidates {
					renames[index] = ridumigration.Rename{Block: candidate.Block, FieldBefore: candidate.BeforeField.Path.String(), FieldAfter: candidate.AfterField.Path.String()}
				}
				created, err = CreateArtifactWithRenames(ctx, directory, name, after, time.Unix(step, 0), renames)
			} else {
				created, err = CreateArtifact(ctx, directory, name, after, time.Unix(step, 0), false)
			}
			if err != nil {
				return err
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				_ = os.Remove(created.Path)
				return err
			}
			return nil
		},
		Rollback: func(t *testing.T) error { return backend.DownArtifacts(ctx, directory) },
	})
}
