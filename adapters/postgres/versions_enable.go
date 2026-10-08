package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/internal/enableversions"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// enableVersionsBatchSize bounds the documents whose first versions one
// statement records.
const enableVersionsBatchSize = 500

// ReviewDevelopmentVersions counts the documents, trashed ones included, that
// each resource starting to keep versions between before and after already
// stores in this database. A resource without a table stores none. It
// changes nothing.
func (backend *Store) ReviewDevelopmentVersions(ctx context.Context, before, after schema.Manifest) ([]enableversions.Report, error) {
	enabled := ridumigration.VersionsEnabled(before.Snapshot(), after.Snapshot(), nil)
	reports := make([]enableversions.Report, 0, len(enabled))
	if len(enabled) == 0 {
		return reports, nil
	}
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, translateError(err)
	}
	defer rollbackPostgresTransaction(ctx, transaction)
	for _, resource := range enabled {
		table := collectionTable(resource.ID)
		var exists bool
		if err := transaction.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return nil, translateError(err)
		}
		var documents int64
		if exists {
			if err := transaction.QueryRow(ctx, "SELECT count(*) FROM "+quote(table)).Scan(&documents); err != nil {
				return nil, translateError(err)
			}
		}
		reports = append(reports, enableversions.Report{Resource: resource, Documents: documents})
	}
	return reports, nil
}

// requireEmptyForDevelopmentVersions refuses development synchronization
// that would start keeping versions on a resource that stores documents,
// trashed ones included: only a reviewed migration decides what they become.
// The lock keeps writers out until the synchronization commits.
func requireEmptyForDevelopmentVersions(ctx context.Context, transaction *sql.Tx, before, after schema.Manifest) error {
	for _, resource := range ridumigration.VersionsEnabled(before.Snapshot(), after.Snapshot(), nil) {
		table := collectionTable(resource.ID)
		exists, err := transactionTableExists(ctx, transaction, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		documents, err := countLockedDocuments(ctx, transaction, table)
		if err != nil {
			return err
		}
		if documents != 0 {
			return enableversions.StoredDocumentsError(resource, documents)
		}
	}
	return nil
}

// countLockedDocuments locks a working table against writers until the
// transaction ends and counts its rows, trashed documents included.
func countLockedDocuments(ctx context.Context, transaction *sql.Tx, table string) (int64, error) {
	if _, err := transaction.ExecContext(ctx, "LOCK TABLE "+quote(table)+" IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return 0, err
	}
	var documents int64
	err := transaction.QueryRowContext(ctx, "SELECT count(*) FROM "+quote(table)).Scan(&documents)
	return documents, err
}

// planVersionsEnable returns a step for each resource that starts keeping
// versions. A conversion rewrites the documents a resource stores, so it takes
// a migration of its own: renames, including the content rewrite of a changed
// collection slug, and data transforms rewrite the same content.
func planVersionsEnable(before *schema.Manifest, after schema.Manifest, renames []Rename, transforms []ridumigration.DataTransformDescriptor, choices enableversions.Choices) ([]enableversions.Step, error) {
	var previous schema.Snapshot
	if before != nil {
		previous = before.Snapshot()
	}
	collectionRenames := postgresCollectionRenameIDs(renames)
	if enabled := ridumigration.VersionsEnabled(previous, after.Snapshot(), collectionRenames); len(enabled) != 0 && (len(renames) != 0 || len(transforms) != 0) {
		return nil, fmt.Errorf("enable versions on %s in a PostgreSQL migration of its own; it cannot share one with renames, collection slug changes or data transforms", enableversions.Describe(enabled[0]))
	}
	return enableversions.Plan(previous, after.Snapshot(), collectionRenames, choices)
}

// validateRecordedVersionsEnable admits a resource that starts keeping
// versions only together with the artifact's record of what its stored
// documents become, following confirmed collection renames.
func validateRecordedVersionsEnable(artifact ridumigration.Artifact, renames []Rename) error {
	recorded, err := enableversions.Recorded(artifact)
	if err != nil {
		return err
	}
	_, err = enableversions.Plan(*artifact.Before, artifact.After, postgresCollectionRenameIDs(renames), recorded)
	return err
}

// enableVersionsRewritesDocuments reports whether an enable-versions step
// converts stored documents, which only a migration runner does, rather than
// checking that the resource stores none. An unreadable payload counts as a
// conversion.
func enableVersionsRewritesDocuments(step ridumigration.Step) bool {
	var payload ridumigration.EnableVersionsPayload
	return json.Unmarshal(step.Payload, &payload) != nil || payload.Existing != ridumigration.ExistingRequireEmpty
}

// enablePostgresVersions applies one enable-versions step inside a migration
// transaction. require-empty stops the migration while the working table
// stores a row; it runs before the DDL and reads nothing else. A conversion
// runs after the DDL has added the versioned columns and live table: every
// document gets an explicit status and a revision of at least 1, a published
// document a live row and live index rows copied from its working ones, and
// every document a first version holding what the engine would have saved.
func enablePostgresVersions(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest, payload ridumigration.EnableVersionsPayload) error {
	snapshot := manifest.Snapshot()
	resource, found := postgresResource(snapshot, payload.ResourceID)
	if !found {
		return fmt.Errorf("versions are enabled on absent resource %s", payload.ResourceID)
	}
	working := quote(collectionTable(resource.ID))
	documents, err := countLockedDocuments(ctx, transaction, collectionTable(resource.ID))
	if err != nil || documents == 0 {
		return err
	}
	if payload.Existing == ridumigration.ExistingRequireEmpty {
		return enableversions.NotEmptyError(resource, documents)
	}
	if err := enableversions.Validate(resource, payload.Existing); err != nil {
		return err
	}
	status := store.StatusDraft
	if payload.Existing == ridumigration.ExistingPublished {
		status = store.StatusPublished
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE `+working+` SET "_status" = $1, "_revision" = GREATEST("_revision", 1)`, string(status)); err != nil {
		return err
	}
	var locales []schema.LocaleCode
	if localization := snapshot.Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	// The resource kept no live head or version until now, so any such row
	// belongs to earlier content; each document's history starts here.
	for _, statement := range []string{
		`DELETE FROM ridu_document_references WHERE owner_collection_id = $1 AND published_head = true`,
		`DELETE FROM ridu_versions WHERE collection_id = $1`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, string(resource.ID)); err != nil {
			return err
		}
	}
	if status == store.StatusPublished {
		columns := physicalDocumentColumns(resource, locales)
		for index, column := range columns {
			columns[index] = quote(column)
		}
		list := strings.Join(columns, ", ")
		// Trashed rows are copied too: the live row mirrors the working row's
		// trash state, as putPublishedHead keeps it.
		if _, err := transaction.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (%s, has_draft_changes) SELECT %s, false FROM %s`,
			quote(publishedCollectionTable(resource.ID)), list, list, working)); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id, target_collection_id, target_document_id, locale, occurrence, published_head
) SELECT owner_collection_id, owner_document_id, field_id, target_collection_id, target_document_id, locale, occurrence, true
FROM ridu_document_references WHERE owner_collection_id = $1 AND published_head = false`, string(resource.ID)); err != nil {
			return err
		}
	}
	return recordFirstVersions(ctx, transaction, resource, locales, payload.Existing)
}

// recordFirstVersions saves each converted document of resource as its first
// version, in batches. The snapshot is the document the engine would have
// saved, and the version dates from the document's last update.
func recordFirstVersions(ctx context.Context, transaction *sql.Tx, resource schema.Collection, locales []schema.LocaleCode, existing ridumigration.ExistingDocuments) error {
	fields := storedSchemaFields(resource)
	statement := fmt.Sprintf(`SELECT %s FROM %s WHERE "id" > $1 ORDER BY "id" LIMIT %d`,
		selectColumns(resource, fields, locales), quote(collectionTable(resource.ID)), enableVersionsBatchSize)
	last := ""
	for {
		rows, err := transaction.QueryContext(ctx, statement, last)
		if err != nil {
			return err
		}
		var ids, statuses, snapshots []string
		var revisions []int32
		var created []time.Time
		for rows.Next() {
			document, err := scanDocument(rows, resource, fields, locales)
			if err != nil {
				rows.Close()
				return err
			}
			document = enableversions.Convert(resource, document, existing)
			encoded, err := json.Marshal(document)
			if err != nil {
				rows.Close()
				return fmt.Errorf("encode version snapshot: %w", err)
			}
			ids = append(ids, document.ID)
			revisions = append(revisions, int32(document.Revision))
			statuses = append(statuses, string(document.Status))
			snapshots = append(snapshots, string(encoded))
			created = append(created, document.UpdatedAt)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_versions (collection_id, document_id, revision, status, snapshot, created_at)
SELECT $1, version.document_id, version.revision, version.status, version.snapshot::jsonb, version.created_at
FROM unnest($2::text[], $3::integer[], $4::text[], $5::text[], $6::timestamptz[])
  AS version(document_id, revision, status, snapshot, created_at)`,
			string(resource.ID), ids, revisions, statuses, snapshots, created); err != nil {
			return err
		}
		if len(ids) < enableVersionsBatchSize {
			return nil
		}
		last = ids[len(ids)-1]
	}
}

// postgresResource finds a collection or global of snapshot by stable ID.
func postgresResource(snapshot schema.Snapshot, id schema.StableID) (schema.Collection, bool) {
	for _, resources := range [][]schema.Collection{snapshot.Collections, snapshot.Globals} {
		for _, resource := range resources {
			if resource.ID == id {
				return resource, true
			}
		}
	}
	return schema.Collection{}, false
}
