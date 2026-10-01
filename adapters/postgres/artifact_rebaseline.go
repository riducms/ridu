package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// ReplaceBaseline replaces a verified development ledger without running
// migrations or changing content. The database must already have the schema of
// the last replacement artifact it can record; later artifacts with data or
// semantic steps stay pending for ApplyArtifacts.
func (backend *Store) ReplaceBaseline(ctx context.Context, directory string, options migration.BaselineReplacementOptions) ([]string, error) {
	files, previous, err := migrationartifact.ReplacementHistories(directory, options)
	if err != nil {
		return nil, err
	}
	if err := validatePostgresSemanticHistory(files); err != nil {
		return nil, err
	}
	if err := validatePostgresSemanticHistory(previous); err != nil {
		return nil, err
	}
	runner, err := normalizeRunnerOptions(RunnerOptions{})
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
	if err := acquireMigrationLock(ctx, connection, runner.AdvisoryLockWait); err != nil {
		return nil, err
	}
	defer connection.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	exists, err := artifactLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, migrationartifact.ErrNoRecordedHistory
	}
	if err := validateArtifactLedger(ctx, connection); err != nil {
		return nil, err
	}
	applied, err := readArtifactLedger(ctx, connection)
	if err != nil {
		return nil, err
	}
	if err := validateAppliedArtifacts(previous, applied); err != nil {
		return nil, migrationartifact.PreviousHistoryError(err)
	}
	end, err := migrationartifact.CheckReplacement(files, previous, len(applied), postgresBlockingStep)
	if err != nil {
		return nil, err
	}
	oldHead := previous[len(applied)-1]
	oldManifest := schema.NewManifest(oldHead.Artifact.After)
	head := files[end-1]
	after := schema.NewManifest(head.Artifact.After)
	if err := migrationartifact.RequireRetainedResources(oldManifest.Snapshot(), after.Snapshot()); err != nil {
		return nil, err
	}
	stepsExist, err := artifactStepLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	if !stepsExist {
		return nil, fmt.Errorf("recorded phase/step ledger is missing; restore the original complete history")
	}
	if err := validateArtifactStepLedger(ctx, connection); err != nil {
		return nil, err
	}
	if err := validateArtifactStepHistory(ctx, connection, previous); err != nil {
		return nil, migrationartifact.PreviousHistoryError(err)
	}
	if err := validateCompletedArtifactSteps(ctx, connection, previous, len(applied), true); err != nil {
		return nil, err
	}
	inProgress, err := artifactExecutionInProgress(ctx, connection, previous, len(applied), true)
	if err != nil {
		return nil, err
	}
	if inProgress {
		return nil, migrationartifact.ErrReplacementPartlyApplied
	}
	if err := validatePendingArtifactInspection(ctx, files); err != nil {
		return nil, err
	}
	if err := verifyPostgresPhysicalState(ctx, connection, &after); err != nil {
		return nil, migrationartifact.ReplacementSchemaDriftError(err)
	}
	// The schema record is written only after ridu dev scanned and verified a
	// change, or by a migration, so it vouches for stored values that identical
	// tables cannot.
	if err := requirePostgresDevelopmentManifest(ctx, connection, after); err != nil {
		return nil, err
	}
	if migrationartifact.SameRecordedHistory(files[:end], previous, len(applied)) {
		return nil, nil
	}
	if !options.AllowProduction {
		err := verifyPostgresPhysicalState(ctx, connection, &oldManifest)
		if err == nil {
			return nil, migration.ErrReplacementNeedsConfirmation
		}
		if postgresVerificationUnavailable(err) {
			return nil, fmt.Errorf("check whether the database is at its recorded migration head: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM ridu_migration_steps`); err != nil {
		return nil, err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM ridu_migrations`); err != nil {
		return nil, err
	}
	recorded := make([]string, 0, end)
	for _, file := range files[:end] {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migration_steps
 (artifact_name, artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts, completed_at)
 VALUES ($1, $2, $3, $4, $5, 'complete', '{}'::jsonb, 1, now())`, file.Name, file.Digest, phase.ID, step.ID, phase.Mode); err != nil {
					return nil, err
				}
			}
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migrations
 (name, artifact_digest, from_digest, to_digest, planner_name, planner_version) VALUES ($1, $2, $3, $4, $5, $6)`,
			file.Name, file.Digest, file.Artifact.FromDigest, file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version); err != nil {
			return nil, err
		}
		recorded = append(recorded, file.Name)
	}
	if err := writePostgresDevelopmentManifest(ctx, transaction, after); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit replacement baseline: %w", err)
	}
	return recorded, nil
}

// postgresVerificationUnavailable reports a verification that failed for an
// operational reason. Schema drift comes from the comparison itself; server,
// connection, and context failures are not evidence that the schema differs.
func postgresVerificationUnavailable(err error) bool {
	var serverError *pgconn.PgError
	var connectError *pgconn.ConnectError
	var networkError net.Error
	return errors.As(err, &serverError) || errors.As(err, &connectError) || errors.As(err, &networkError) ||
		pgconn.Timeout(err) || pgconn.SafeToRetry(err) || errors.Is(err, context.Canceled) ||
		errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone)
}
