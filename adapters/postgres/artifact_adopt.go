package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
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
	if err := validatePostgresSemanticHistory(files); err != nil {
		return nil, err
	}
	options, err := normalizeRunnerOptions(RunnerOptions{})
	if err != nil {
		return nil, err
	}
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	// The same lock serializes migration runners and `ridu dev` schema sync.
	if err := acquireMigrationLock(ctx, connection, options.AdvisoryLockWait); err != nil {
		return nil, fmt.Errorf("lock migrations: %w", err)
	}
	defer connection.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)

	exists, err := artifactLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	var applied []artifactLedgerRow
	if exists {
		if err := validateArtifactLedger(ctx, connection); err != nil {
			return nil, err
		}
		if applied, err = readArtifactLedger(ctx, connection); err != nil {
			return nil, err
		}
	}
	if err := validateAppliedArtifacts(files, applied); err != nil {
		return nil, err
	}
	stepLedgerExists, err := artifactStepLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	if stepLedgerExists {
		if err := validateArtifactStepLedger(ctx, connection); err != nil {
			return nil, err
		}
		if err := validateArtifactStepHistory(ctx, connection, files); err != nil {
			return nil, err
		}
	}
	if err := validateCompletedArtifactSteps(ctx, connection, files, len(applied), stepLedgerExists); err != nil {
		return nil, err
	}
	inProgress, err := artifactExecutionInProgress(ctx, connection, files, len(applied), stepLedgerExists)
	if err != nil {
		return nil, err
	}
	if inProgress {
		return nil, fmt.Errorf("a migration is partly applied; finish it with ridu migrate up before adopting")
	}
	if len(applied) == len(files) {
		return nil, nil
	}
	if err := validatePendingArtifactInspection(ctx, files[len(applied):]); err != nil {
		return nil, err
	}
	adoptEnd, err := migrationartifact.AdoptionEnd(files, len(applied), postgresBlockingStep, func(index int) (bool, error) {
		manifest := schema.NewManifest(files[index].Artifact.After)
		if err := verifyPostgresPhysicalState(ctx, connection, &manifest, postgresArtifactTargetContract(files[index].Artifact)); err != nil {
			return false, ctx.Err()
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	pending := files[len(applied):adoptEnd]
	if len(pending) == 0 {
		return nil, nil
	}

	if err := ensureArtifactLedger(ctx, connection); err != nil {
		return nil, err
	}
	if err := ensureArtifactStepLedger(ctx, connection); err != nil {
		return nil, err
	}
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback()
	adopted := make([]string, 0, len(pending))
	for _, file := range pending {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migration_steps
(artifact_name, artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts, completed_at)
VALUES ($1, $2, $3, $4, $5, 'complete', '{}'::jsonb, 1, now())`, file.Name, file.Digest, phase.ID, step.ID, phase.Mode); err != nil {
					return nil, fmt.Errorf("record adopted migration %s step %s: %w", file.Name, step.ID, err)
				}
			}
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migrations (name, artifact_digest, from_digest, to_digest, planner_name, planner_version) VALUES ($1, $2, $3, $4, $5, $6)`,
			file.Name, file.Digest, file.Artifact.FromDigest, file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version); err != nil {
			return nil, fmt.Errorf("record adopted migration %s: %w", file.Name, err)
		}
		adopted = append(adopted, file.Name)
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit adopted migrations: %w", err)
	}
	return adopted, nil
}

// postgresBlockingStep names the first step only a migration runner performs.
func postgresBlockingStep(file migrationartifact.File) string {
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			if migrationStepRequiresMaintenance(step.Kind) {
				return string(step.Kind)
			}
		}
	}
	return ""
}
