package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/internal/enableversions"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ReviewDevelopmentVersions counts the documents, trashed ones included, that
// each resource starting to keep versions between before and after already
// stores in this database. It changes nothing.
func (backend *Store) ReviewDevelopmentVersions(ctx context.Context, before, after schema.Manifest) ([]enableversions.Report, error) {
	enabled := ridumigration.VersionsEnabled(before.Snapshot(), after.Snapshot(), nil)
	reports := make([]enableversions.Report, 0, len(enabled))
	for _, resource := range enabled {
		documents, err := countSQLiteDocuments(ctx, backend.db, resource.ID)
		if err != nil {
			return nil, err
		}
		reports = append(reports, enableversions.Report{Resource: resource, Documents: documents})
	}
	return reports, nil
}

// EnableDevelopmentVersions turns the stored documents of each resource that
// starts keeping versions into versioned documents as existing says, and
// synchronizes the database to after in the same transaction, using the
// executor migrations use. It refuses a database with migration history,
// which only ridu migrate may change.
func (backend *Store) EnableDevelopmentVersions(ctx context.Context, before, after schema.Manifest, existing map[schema.StableID]ridumigration.ExistingDocuments) error {
	if _, err := enableversions.Plan(before.Snapshot(), after.Snapshot(), nil, existing); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		managed, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		if managed {
			return fmt.Errorf("this SQLite database is managed by ridu migrate; enable versions with ridu migrate create --versions-existing and apply it with ridu migrate up")
		}
		return backend.migrateDevelopmentSchema(ctx, connection, after, existing)
	})
}

// enableSQLiteVersions turns the documents resource stores into versioned
// documents inside the caller's write transaction. Published documents get a
// live head and every document a first version. The caller rebuilds
// references and unique values, which already cover both heads.
func (backend *Store) enableSQLiteVersions(ctx context.Context, connection *sql.Conn, resource schema.Collection, existing ridumigration.ExistingDocuments) error {
	documents, err := countSQLiteDocuments(ctx, connection, resource.ID)
	if err != nil || documents == 0 {
		return err
	}
	if existing == ridumigration.ExistingRequireEmpty {
		return enableversions.NotEmptyError(resource, documents)
	}
	if err := enableversions.Validate(resource, existing); err != nil {
		return err
	}
	rows, err := connection.QueryContext(ctx, `SELECT id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE collection_id = ? ORDER BY id`, string(resource.ID))
	if err != nil {
		return translateError(err)
	}
	var converted []store.Document
	for rows.Next() {
		document, err := scanDocumentRecord(rows, resource)
		if err != nil {
			rows.Close()
			return err
		}
		converted = append(converted, enableversions.Convert(resource, document, existing))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	if err := rows.Close(); err != nil {
		return translateError(err)
	}
	transaction := &documentTransaction{store: backend, connection: connection}
	for _, document := range converted {
		if _, err := connection.ExecContext(ctx, `UPDATE ridu_documents SET status = ?, revision = ? WHERE collection_id = ? AND id = ?`,
			string(document.Status), document.Revision, string(resource.ID), document.ID); err != nil {
			return translateError(err)
		}
		if document.Status == store.StatusPublished {
			err = transaction.putPublishedHead(ctx, resource, document)
		} else {
			err = transaction.deletePublishedHead(ctx, resource, document.ID)
		}
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(document)
		if err != nil {
			return fmt.Errorf("encode version snapshot: %w", err)
		}
		// Versions a resource kept before it stopped keeping them belong to
		// earlier content; this first version starts the document's history.
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_versions WHERE collection_id = ? AND document_id = ?`, string(resource.ID), document.ID); err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `INSERT INTO ridu_versions (
  id, collection_id, document_id, revision, status, snapshot_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`, fmt.Sprintf("%s:%d", document.ID, document.Revision), string(resource.ID), document.ID, document.Revision,
			string(document.Status), string(snapshot), encodeTime(document.UpdatedAt)); err != nil {
			return translateError(err)
		}
	}
	return nil
}

// disableSQLiteVersions undoes enableSQLiteVersions when a migration is
// rolled back to a schema where resource keeps no versions. Each document
// keeps its latest working content; live heads and versions are removed.
func disableSQLiteVersions(ctx context.Context, connection *sql.Conn, resource schema.Collection) error {
	for _, statement := range []string{
		`DELETE FROM ridu_published_documents WHERE collection_id = ?`,
		`DELETE FROM ridu_versions WHERE collection_id = ?`,
	} {
		if _, err := connection.ExecContext(ctx, statement, string(resource.ID)); err != nil {
			return translateError(err)
		}
	}
	statement := `UPDATE ridu_documents SET status = '', revision = 0 WHERE collection_id = ?`
	if resource.Upload != nil {
		// An upload keeps counting its revisions without versions.
		statement = `UPDATE ridu_documents SET status = '' WHERE collection_id = ?`
	}
	_, err := connection.ExecContext(ctx, statement, string(resource.ID))
	return translateError(err)
}

func countSQLiteDocuments(ctx context.Context, runner sqlRunner, resource schema.StableID) (int64, error) {
	var installed int
	if err := runner.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_documents'`).Scan(&installed); err != nil {
		return 0, translateError(err)
	}
	if installed == 0 {
		return 0, nil
	}
	var documents int64
	err := runner.QueryRowContext(ctx, `SELECT count(*) FROM ridu_documents WHERE collection_id = ?`, string(resource)).Scan(&documents)
	return documents, translateError(err)
}

func sqliteManifestResource(manifest schema.Manifest, id schema.StableID) (schema.Collection, bool) {
	for _, resource := range sqliteManifestResources(manifest) {
		if resource.ID == id {
			return resource, true
		}
	}
	return schema.Collection{}, false
}
