package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// DevelopmentManifest returns the schema recorded by the database's last
// development synchronization or migration. Generated files and disposable CLI
// caches cannot establish how persisted values should be interpreted.
func (backend *Store) DevelopmentManifest(ctx context.Context) (schema.Manifest, bool, error) {
	return sqliteDevelopmentManifest(ctx, backend.db)
}

func sqliteDevelopmentManifest(ctx context.Context, runner sqlRunner) (schema.Manifest, bool, error) {
	manifest, err := readSQLiteDevelopmentManifest(ctx, runner)
	if err != nil {
		return schema.Manifest{}, false, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: cannot read the recorded SQLite schema: %w", err)
	}
	if manifest == nil {
		var installed int
		if err := runner.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_documents'`).Scan(&installed); err != nil {
			return schema.Manifest{}, false, translateError(err)
		}
		if installed != 0 {
			return schema.Manifest{}, false, fmt.Errorf("RIDU_DEVELOPMENT_SCHEMA_UNKNOWN: SQLite application tables exist without a recorded schema; restore a database backup with its schema metadata or use a new development database; generated manifests and CLI caches cannot establish the stored schema")
		}
		return schema.Manifest{}, false, nil
	}
	return *manifest, true, nil
}

// ReviewDevelopmentFieldKinds counts live (including trashed) documents and
// every retained snapshot under their previous schema, without changing data.
func (backend *Store) ReviewDevelopmentFieldKinds(ctx context.Context, before, after schema.Manifest) ([]fieldchange.Report, error) {
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	reports := fieldchange.Reports(changes)
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		return scanSQLiteFieldKinds(ctx, connection, changes, reports, false)
	})
	return reports, err
}

// ClearDevelopmentFieldKinds clears only the confirmed changed fields, in
// current values and snapshots, and adopts the resulting development schema in
// one transaction. Immutable migration history is never bypassed.
func (backend *Store) ClearDevelopmentFieldKinds(ctx context.Context, before, after schema.Manifest, expected []fieldchange.Report) error {
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	if err := fieldchange.ValidateClear(changes); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		managed, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		if managed {
			return fmt.Errorf("this SQLite database is managed by ridu migrate; field-kind recovery requires a reviewed migration")
		}
		reports := fieldchange.Reports(changes)
		if err := scanSQLiteFieldKinds(ctx, connection, changes, reports, false); err != nil {
			return err
		}
		if err := fieldchange.ConfirmCounts(expected, reports); err != nil {
			return err
		}
		reports = fieldchange.Reports(changes)
		if err := scanSQLiteFieldKinds(ctx, connection, changes, reports, true); err != nil {
			return err
		}
		return backend.migrateDevelopmentSchema(ctx, connection, after)
	})
}

func scanSQLiteFieldKinds(ctx context.Context, connection *sql.Conn, changes []fieldchange.Change, reports []fieldchange.Report, clear bool) error {
	var installed int
	if err := connection.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_documents'`).Scan(&installed); err != nil {
		return translateError(err)
	}
	if installed == 0 {
		return nil
	}
	for _, resource := range fieldchange.AffectedResources(changes) {
		for _, snapshot := range []bool{false, true} {
			query := `SELECT id, 0, values_json FROM ridu_documents WHERE collection_id = ? ORDER BY id`
			if snapshot {
				query = `SELECT document_id, revision, snapshot_json FROM ridu_versions WHERE collection_id = ? ORDER BY document_id, revision`
			}
			rows, err := connection.QueryContext(ctx, query, string(resource.ID))
			if err != nil {
				return translateError(err)
			}
			type rewrite struct {
				id       string
				revision int
				encoded  string
			}
			var rewrites []rewrite
			for rows.Next() {
				var update rewrite
				if err := rows.Scan(&update.id, &update.revision, &update.encoded); err != nil {
					rows.Close()
					return translateError(err)
				}
				var document store.Document
				if snapshot {
					err = json.Unmarshal([]byte(update.encoded), &document)
				} else {
					err = document.Values.UnmarshalJSON([]byte(update.encoded))
				}
				if err != nil {
					rows.Close()
					return fmt.Errorf("decode stored field-kind recovery document %s: %w", update.id, err)
				}
				values, found, err := fieldchange.Process(changes, reports, resource.ID, document.Values, snapshot, clear)
				if err != nil {
					rows.Close()
					return err
				}
				if !clear || !found {
					continue
				}
				document.Values = values
				var encoded []byte
				if snapshot {
					encoded, err = json.Marshal(document)
				} else {
					encoded, err = values.MarshalJSON()
				}
				if err != nil {
					rows.Close()
					return err
				}
				update.encoded = string(encoded)
				rewrites = append(rewrites, update)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return translateError(err)
			}
			if err := rows.Close(); err != nil {
				return translateError(err)
			}
			for _, update := range rewrites {
				if snapshot {
					_, err = connection.ExecContext(ctx, `UPDATE ridu_versions SET snapshot_json = ? WHERE collection_id = ? AND document_id = ? AND revision = ?`, update.encoded, string(resource.ID), update.id, update.revision)
				} else {
					_, err = connection.ExecContext(ctx, `UPDATE ridu_documents SET values_json = ? WHERE collection_id = ? AND id = ?`, update.encoded, string(resource.ID), update.id)
				}
				if err != nil {
					return translateError(err)
				}
			}
		}
	}
	return nil
}
