package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

// AdoptArtifacts records pending migrations as applied, without running them,
// through the newest one whose schema the database already has, such as a
// database ridu dev synchronized. See ridu migrate baseline.
func (backend *Store) AdoptArtifacts(ctx context.Context, directory string) ([]string, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial migration before adopting")
	}
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeMongoMigrationRunnerOptions(RunnerOptions{})
	if err != nil {
		return nil, err
	}
	var adopted []string
	err = backend.withMongoMigrationLease(ctx, normalized, func(runContext context.Context, lease *mongoMigrationLease) error {
		adopted, err = backend.adoptMongoMigrationReplay(runContext, lease, files, replay)
		return err
	})
	if err != nil {
		return nil, err
	}
	return adopted, nil
}

func (backend *Store) adoptMongoMigrationReplay(ctx context.Context, lease *mongoMigrationLease, files []migrationartifact.File, replay []mongoDBArtifactReplayPlan) ([]string, error) {
	state, err := backend.readMongoMigrationLedgerState(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateMongoMigrationLedgerAgainstFiles(files, state); err != nil {
		return nil, err
	}
	applied := len(state.artifacts)
	if applied == len(files) {
		return nil, nil
	}
	for _, row := range state.steps {
		if row.ArtifactName == files[applied].Name {
			return nil, fmt.Errorf("a migration is partly applied; finish it with ridu migrate up before adopting")
		}
	}
	positions := make(map[string]int, len(files))
	for index, file := range files {
		positions[file.Name] = index
	}
	blockingStep := func(file migrationartifact.File) string {
		for _, step := range replay[positions[file.Name]].steps {
			if step.kind != ridumigration.StepMongoDBCreateIndex && step.kind != ridumigration.StepMongoDBAssertSchema {
				return string(step.kind)
			}
		}
		return ""
	}
	end, err := migrationartifact.AdoptionEnd(files, applied, blockingStep, func(index int) (bool, error) {
		backend.clearVerifiedIndexes()
		physical := replay[index].physical
		if err := backend.verifyIndexPlansWithoutAuthorization(ctx, physical.collections, physical.system); err != nil {
			return false, ctx.Err()
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	adopted := make([]string, 0, end-applied)
	for index := applied; index < end; index++ {
		file := files[index]
		for _, step := range replay[index].steps {
			completed, err := backend.startMongoMigrationStep(ctx, lease, file, step)
			if err != nil {
				return nil, err
			}
			if completed {
				continue
			}
			if err := backend.completeMongoMigrationStep(ctx, lease, file, step); err != nil {
				return nil, err
			}
		}
		if err := backend.completeMongoMigrationArtifact(ctx, lease, index, file); err != nil {
			return nil, err
		}
		adopted = append(adopted, file.Name)
	}
	if len(adopted) != 0 {
		backend.clearVerifiedIndexes()
		physical := replay[end-1].physical
		if err := backend.verifyIndexPlans(ctx, physical.collections, physical.system); err != nil {
			return nil, fmt.Errorf("adopted MongoDB migration state: %w", err)
		}
	}
	return adopted, nil
}
