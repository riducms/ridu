package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const postgresDevelopmentSchemaTable = "ridu_postgres_schema"

func postgresSchemaMetadataTable(name string) bool {
	return name == "ridu_migrations" || name == "ridu_migration_steps" || name == postgresDevelopmentSchemaTable
}

func unknownPostgresDevelopmentSchema() error {
	return fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: PostgreSQL has existing schema but no last-synchronized schema record, so the schema that produced its stored content is unknown; restore a database backup with its schema record, or preserve any needed content and use a new dedicated development database; generated files, .ridu caches and physical tables cannot establish the previous schema")
}

// DevelopmentManifest returns the database-owned last synchronized schema.
// A missing record is fresh only when no application tables or migration
// history exist; otherwise the schema is unknown and an error is returned.
func (backend *Store) DevelopmentManifest(ctx context.Context) (schema.Manifest, bool, error) {
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return schema.Manifest{}, false, err
	}
	defer transaction.Rollback()
	return postgresDevelopmentManifest(ctx, transaction)
}

// postgresDevelopmentManifest reads the schema record. Migration history or
// application tables without a record make the schema unknown; a fresh
// database has neither.
func postgresDevelopmentManifest(ctx context.Context, transaction *sql.Tx) (schema.Manifest, bool, error) {
	if exists, err := transactionTableExists(ctx, transaction, "ridu_migration_steps"); err != nil {
		return schema.Manifest{}, false, err
	} else if exists {
		appliedExists, err := transactionTableExists(ctx, transaction, "ridu_migrations")
		if err != nil {
			return schema.Manifest{}, false, err
		}
		query := `SELECT EXISTS(SELECT 1 FROM ridu_migration_steps)`
		if appliedExists {
			query = `SELECT EXISTS(SELECT 1 FROM ridu_migration_steps steps WHERE steps.state <> 'complete' OR NOT EXISTS (SELECT 1 FROM ridu_migrations applied WHERE applied.name = steps.artifact_name AND applied.artifact_digest = steps.artifact_digest))`
		}
		var incomplete bool
		if err := transaction.QueryRowContext(ctx, query).Scan(&incomplete); err != nil {
			return schema.Manifest{}, false, err
		}
		if incomplete {
			return schema.Manifest{}, false, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: PostgreSQL migration work is incomplete; finish it using the original immutable migration history before development synchronization")
		}
	}
	manifest, err := readPostgresDevelopmentManifest(ctx, transaction)
	if err != nil {
		return schema.Manifest{}, false, err
	}
	if manifest != nil {
		return *manifest, true, nil
	}
	if exists, err := transactionTableExists(ctx, transaction, "ridu_migrations"); err != nil {
		return schema.Manifest{}, false, err
	} else if exists {
		var recordedHistory bool
		if err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ridu_migrations)`).Scan(&recordedHistory); err != nil {
			return schema.Manifest{}, false, err
		}
		if recordedHistory {
			return schema.Manifest{}, false, unknownPostgresDevelopmentSchema()
		}
	}
	tables, err := unmanagedPostgresTables(ctx, transaction)
	if err != nil {
		return schema.Manifest{}, false, err
	}
	if len(tables) != 0 {
		return schema.Manifest{}, false, unknownPostgresDevelopmentSchema()
	}
	return schema.Manifest{}, false, nil
}

func readPostgresDevelopmentManifest(ctx context.Context, transaction *sql.Tx) (*schema.Manifest, error) {
	exists, err := transactionTableExists(ctx, transaction, postgresDevelopmentSchemaTable)
	if err != nil || !exists {
		return nil, err
	}
	var encoded, recordedDigest string
	err = transaction.QueryRowContext(ctx, `SELECT manifest_json, manifest_digest FROM ridu_postgres_schema WHERE singleton = 1`).Scan(&encoded, &recordedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, unknownPostgresDevelopmentSchema()
	}
	if err != nil {
		return nil, err
	}
	manifest, err := schema.ParseHistorical([]byte(encoded))
	if err != nil {
		return nil, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: invalid recorded PostgreSQL manifest: %w", err)
	}
	digest, err := ridumigration.DigestManifest(manifest)
	if err != nil || digest != recordedDigest {
		return nil, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: recorded PostgreSQL manifest digest does not match its canonical schema")
	}
	return &manifest, nil
}

func writePostgresDevelopmentManifest(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest) error {
	encoded, err := manifest.Bytes()
	if err != nil {
		return err
	}
	digest, err := ridumigration.DigestManifest(manifest)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ridu_postgres_schema (
singleton integer PRIMARY KEY CHECK (singleton = 1),
manifest_json text NOT NULL,
manifest_digest text NOT NULL
)`); err != nil {
		return fmt.Errorf("create PostgreSQL schema record: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO ridu_postgres_schema (singleton, manifest_json, manifest_digest) VALUES (1, $1, $2)
ON CONFLICT (singleton) DO UPDATE SET manifest_json = excluded.manifest_json, manifest_digest = excluded.manifest_digest`, string(encoded), digest)
	return err
}

func requirePostgresDevelopmentManifest(ctx context.Context, connection *sql.Conn, expected schema.Manifest) error {
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	manifest, err := readPostgresDevelopmentManifest(ctx, transaction)
	if err != nil {
		return err
	}
	if manifest == nil {
		return unknownPostgresDevelopmentSchema()
	}
	if !manifest.SameStorage(expected) {
		return fmt.Errorf("recorded PostgreSQL schema differs from the requested migration head; keep the authoritative schema record and create matching reviewed history before baseline adoption or migration")
	}
	return nil
}

// SyncDevelopmentSchema plans, applies, verifies and records safe development
// changes in one transaction under the migration lock. Production artifacts
// retain their separate concurrent-index phases.
func (backend *Store) SyncDevelopmentSchema(ctx context.Context, manifest schema.Manifest) error {
	if err := primitivefield.ValidateManifestIndexes(manifest); err != nil {
		return err
	}
	options, err := normalizeRunnerOptions(RunnerOptions{})
	if err != nil {
		return err
	}
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := acquireMigrationLock(ctx, connection, options.AdvisoryLockWait); err != nil {
		return err
	}
	defer connection.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	before, exists, err := postgresDevelopmentManifest(ctx, transaction)
	if err != nil {
		return err
	}
	if exists {
		// A resource that starts keeping versions while it stores no document
		// needs only the plan's new columns and live table.
		if err := requireEmptyForDevelopmentVersions(ctx, transaction, before, manifest); err != nil {
			return err
		}
		changes := fieldchange.Detect(before.Snapshot(), manifest.Snapshot())
		if len(changes) != 0 {
			var stored []fieldchange.Change
			tables := make(map[schema.StableID]bool)
			for _, resource := range fieldchange.AffectedResources(changes) {
				for index, table := range documentTables(resource, resource.ID) {
					exists, err := transactionTableExists(ctx, transaction, table)
					if err != nil {
						return err
					}
					if !exists {
						continue
					}
					if index == 0 {
						tables[resource.ID] = true
					}
					if _, err := transaction.ExecContext(ctx, `LOCK TABLE `+quote(table)+` IN SHARE ROW EXCLUSIVE MODE`); err != nil {
						return err
					}
				}
			}
			for _, change := range changes {
				if tables[change.Resource.ID] {
					stored = append(stored, change)
				}
			}
			versionsExist, err := transactionTableExists(ctx, transaction, "ridu_versions")
			if err != nil {
				return err
			}
			if versionsExist {
				if _, err := transaction.ExecContext(ctx, `LOCK TABLE ridu_versions IN SHARE ROW EXCLUSIVE MODE`); err != nil {
					return err
				}
			}
			// Reuse the existing raw-value scanner through the native bridge
			// already used by compiled transforms. Raw excludes SQL cancellation
			// rollback while the scanner uses this active transaction.
			if err := connection.Raw(func(driverConnection any) error {
				native, ok := driverConnection.(*stdlib.Conn)
				if !ok || native.Conn() == nil || native.Conn().IsClosed() || native.Conn().PgConn().TxStatus() != 'T' {
					return fmt.Errorf("PostgreSQL development transaction is no longer active")
				}
				reports := fieldchange.Reports(changes)
				if err := scanPostgresFieldKinds(ctx, postgresMigrationPGXTransaction{connection: native.Conn()}, before, changes, reports, false); err != nil {
					return err
				}
				return fieldchange.RequireEmpty(reports)
			}); err != nil {
				return err
			}
			// Every changed value is empty here, but PostgreSQL cannot cast
			// between the scalar column types, so the plan below would fail.
			if err := retypeEmptyPostgresColumns(before, stored, func(statement string) error {
				_, err := transaction.ExecContext(ctx, statement)
				return err
			}); err != nil {
				return err
			}
		}
	}
	statements, err := planPostgresDevelopmentSchema(ctx, transaction, manifest)
	if err != nil {
		return err
	}
	// A field must not become required while stored documents lack its value.
	// The audit reads the current layout before any statement runs, under the
	// same locks that keep writers out until this transaction commits.
	if exists {
		var locales []schema.LocaleCode
		if localization := manifest.Snapshot().Application.Localization; localization != nil {
			locales = localization.LocaleCodes()
		}
		requirements := requiredfield.Detect(before.Snapshot(), manifest.Snapshot(), requiredfield.Renames{})
		if err := auditPostgresRequiredValues(ctx, transaction, locales, requirements, true, true); err != nil {
			return err
		}
	}
	// Reject cross-table unique conflicts before creating the indexes.
	if err := checkMigrationPublishedUniqueUnion(ctx, transaction, manifest); err != nil {
		return fmt.Errorf("verify development uniqueness: %w", err)
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.sql); err != nil {
			return fmt.Errorf("apply %s: %w", statement.kind, migrationConflict(err))
		}
	}
	if err := assertPhysicalSchema(ctx, transaction, manifest); err != nil {
		return fmt.Errorf("verify development schema: %w", err)
	}
	if err := checkMigrationPublishedUniqueUnion(ctx, transaction, manifest); err != nil {
		return fmt.Errorf("verify development uniqueness: %w", err)
	}
	// Writes leave index rows alone when a document's references are
	// unchanged, so rows for a removed or changed reference field would
	// otherwise outlive it. Migrations add StepBackfillReferences for the
	// same topology changes.
	if exists && referenceIndexTopologyChanged(before.Snapshot(), manifest.Snapshot()) {
		if err := rebuildReferenceIndex(ctx, transaction, manifest); err != nil {
			return fmt.Errorf("rebuild development reference index: %w", err)
		}
	}
	if err := writePostgresDevelopmentManifest(ctx, transaction, manifest); err != nil {
		return err
	}
	return transaction.Commit()
}
