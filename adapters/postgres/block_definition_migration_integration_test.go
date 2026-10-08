package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

// A change inside a block definition migrates every stored block of it.
func TestPostgresBlockDefinitionMigrationsReachEveryPlacement(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	var backend *Store
	step := int64(1)
	blockreferences.RunDefinitionMigrations(t, blockreferences.MigrationHarness{
		Begin: func(t *testing.T, before schema.Manifest) store.Store {
			backend = migrationArtifactTestBackend(t)
			if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(step, 0), ArtifactOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{}); err != nil {
				t.Fatal(err)
			}
			return backend
		},
		Migrate: func(t *testing.T, after schema.Manifest, candidates []schemadiff.RenameCandidate) error {
			step++
			renames := make([]Rename, len(candidates))
			for index, candidate := range candidates {
				renames[index] = Rename{Kind: RenameKind(candidate.Kind), Block: candidate.Block, BeforeField: candidate.BeforeField, AfterField: candidate.AfterField}
			}
			created, err := CreateArtifact(ctx, directory, fmt.Sprintf("change-%d", step), after, time.Unix(step, 0), ArtifactOptions{Renames: renames})
			if err != nil {
				return err
			}
			if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
				_ = os.Remove(created.Path)
				return err
			}
			files, err := migrationartifact.ReadAll(directory)
			if err != nil || files[len(files)-1].Name != created.Name {
				t.Fatalf("history = %v, %v", files, err)
			}
			return nil
		},
	})
}
