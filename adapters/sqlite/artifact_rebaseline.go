package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
)

// ReplaceBaseline replaces a verified development ledger with committed history
// whose physical schema is already present. It never runs migration data steps:
// replacement artifacts from the first unrecorded data or semantic step onward
// stay pending for ApplyArtifacts.
func (backend *Store) ReplaceBaseline(ctx context.Context, directory string, options migration.BaselineReplacementOptions) ([]string, error) {
	files, previous, err := migrationartifact.ReplacementHistories(directory, options)
	if err != nil {
		return nil, err
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return nil, err
	}
	if err := preflightSQLiteArtifacts(ctx, previous); err != nil {
		return nil, err
	}
	var recorded []string
	err = backend.withImmediate(ctx, func(connection *sql.Conn) error {
		accepted, acceptedExists, err := sqliteDevelopmentManifest(ctx, connection)
		if err != nil {
			return err
		}
		exists, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		if !exists {
			return migrationartifact.ErrNoRecordedHistory
		}
		if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
			return err
		}
		applied, err := readSQLiteArtifactLedger(ctx, connection)
		if err != nil {
			return err
		}
		if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
			return err
		}
		if err := validateSQLiteAppliedArtifacts(previous, applied); err != nil {
			return migrationartifact.PreviousHistoryError(err)
		}
		end, err := migrationartifact.CheckReplacement(files, previous, len(applied), sqliteBlockingStep)
		if err != nil {
			return err
		}
		oldHead := previous[len(applied)-1]
		oldManifest, err := oldHead.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		head := files[end-1]
		after, err := head.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		if err := migrationartifact.RequireRetainedResources(oldManifest.Snapshot(), after.Snapshot()); err != nil {
			return err
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, after, true); err != nil {
			return migrationartifact.ReplacementSchemaDriftError(err)
		}
		// Migrations and schema sync also rebuild derived reference/uniqueness
		// state. Identical JSON tables do not prove those effects happened, nor
		// that stored values fit the replacement's field kinds; one of them must
		// already have recorded the target storage contract. Admin-only
		// presentation changes require no derived data work.
		if !acceptedExists || !accepted.SameStorage(after) {
			return fmt.Errorf("replacement schema metadata differs from the synchronized database; retain the original history and apply a migration before replacing its ledger")
		}
		if migrationartifact.SameRecordedHistory(files[:end], previous, len(applied)) {
			return nil
		}
		if !options.AllowProduction {
			if assertSQLiteManifestDigest(ctx, connection, oldHead.Artifact.ToDigest) == nil &&
				assertSQLiteExpectedArtifactDigest(ctx, connection, oldHead.Digest) == nil &&
				assertSQLitePhysicalSchema(ctx, connection, oldManifest, true) == nil {
				return migration.ErrReplacementNeedsConfirmation
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_migrations`); err != nil {
			return translateError(err)
		}
		now := backend.now().UTC()
		for index, file := range files[:end] {
			if _, err := connection.ExecContext(ctx, `INSERT INTO ridu_migrations
  (position, name, artifact_digest, previous_artifact_digest, from_digest, to_digest, planner_name, planner_version, applied_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, index+1, file.Name, file.Digest, file.Artifact.PreviousArtifactDigest,
				file.Artifact.FromDigest, file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version, encodeTime(now)); err != nil {
				return translateError(err)
			}
			recorded = append(recorded, file.Name)
		}
		// Only framework schema/history metadata changes; content and derived
		// document state are not rebuilt by a ledger replacement.
		return writeSQLiteManifest(ctx, connection, after, head.Digest, now)
	})
	if err != nil {
		return nil, err
	}
	return recorded, nil
}
