package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return nil, err
	}
	var adopted []string
	err = backend.withImmediate(ctx, func(connection *sql.Conn) error {
		recorded, recordedExists, err := sqliteDevelopmentManifest(ctx, connection)
		if err != nil {
			return err
		}
		exists, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		var applied []sqliteArtifactLedgerRow
		if exists {
			if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
				return err
			}
			if applied, err = readSQLiteArtifactLedger(ctx, connection); err != nil {
				return err
			}
			if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
				return err
			}
		}
		if err := validateSQLiteAppliedArtifacts(files, applied); err != nil {
			return err
		}
		if len(applied) == len(files) {
			return nil
		}
		end, err := migrationartifact.AdoptionEnd(files, len(applied), sqliteBlockingStep, func(index int) (bool, error) {
			after, err := files[index].Artifact.AfterManifest()
			if err != nil {
				return false, err
			}
			if err := assertSQLitePhysicalSchema(ctx, connection, after, exists); err != nil {
				return false, ctx.Err()
			}
			if recordedExists {
				return recorded.SameStorage(after), nil
			}
			return true, nil
		})
		if err != nil {
			return err
		}
		pending := files[len(applied):end]
		if len(pending) == 0 {
			return nil
		}
		head := files[end-1]
		after, err := head.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		// Rebuild derived state as the runner's final schema step does, so an
		// adopted database is as consistent as a migrated one.
		if err := reconcileDocumentIndexes(ctx, connection, after); err != nil {
			return err
		}
		if err := rebuildDocumentReferences(ctx, connection, after); err != nil {
			return fmt.Errorf("rebuild references: %w", err)
		}
		if err := rebuildUniqueValues(ctx, connection, after); err != nil {
			return fmt.Errorf("rebuild uniqueness: %w", err)
		}
		if _, err := connection.ExecContext(ctx, sqliteArtifactLedgerSQL); err != nil {
			return fmt.Errorf("create SQLite migration ledger: %w", translateError(err))
		}
		now := backend.now().UTC()
		for offset, file := range pending {
			if _, err := connection.ExecContext(ctx, `INSERT INTO ridu_migrations
  (position, name, artifact_digest, previous_artifact_digest, from_digest, to_digest, planner_name, planner_version, applied_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, len(applied)+offset+1, file.Name, file.Digest, file.Artifact.PreviousArtifactDigest, file.Artifact.FromDigest,
				file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version, encodeTime(now)); err != nil {
				return fmt.Errorf("record adopted SQLite migration %s: %w", file.Name, translateError(err))
			}
			adopted = append(adopted, file.Name)
		}
		if err := writeSQLiteManifest(ctx, connection, after, files[len(files)-1].Digest, now); err != nil {
			return fmt.Errorf("record SQLite manifest for migration %s: %w", head.Name, err)
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, after, true); err != nil {
			return fmt.Errorf("adopted SQLite migration state: %w", err)
		}
		return assertSQLiteManifestDigest(ctx, connection, head.Artifact.ToDigest)
	})
	if err != nil {
		return nil, err
	}
	return adopted, nil
}

// HasMigrationHistory reports whether ridu migrate manages this database.
func (backend *Store) HasMigrationHistory(ctx context.Context) (bool, error) {
	return sqliteArtifactLedgerExists(ctx, backend.db)
}

// requiresEmptyResource reports whether step enables versions only on a
// resource that stores no documents.
func requiresEmptyResource(step ridumigration.Step) bool {
	if step.Kind != ridumigration.StepEnableVersions {
		return false
	}
	var payload ridumigration.EnableVersionsPayload
	return json.Unmarshal(step.Payload, &payload) == nil && payload.Existing == ridumigration.ExistingRequireEmpty
}

// sqliteBlockingStep names the first step only a migration runner performs.
func sqliteBlockingStep(file migrationartifact.File) string {
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			// Schema sync runs the same required-value audit, so the audit does
			// not block adopting history that sync already brought forward.
			// Enabling versions on a resource that must be empty converts
			// nothing, which is all schema sync does too.
			if step.Kind != ridumigration.StepSQL && step.Kind != ridumigration.StepAuditRequiredValues && step.Kind != ridumigration.StepAssertSchema &&
				!requiresEmptyResource(step) {
				return string(step.Kind)
			}
		}
	}
	return ""
}
