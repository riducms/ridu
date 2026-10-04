package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Published reads join the live head before evaluating caller and access
// predicates. The existing snapshot-aware compiler then filters and sorts the
// immutable live values in SQL, not the mutable working columns.
func readSource(request store.Request) string {
	table := quote(collectionTable(request.Collection.ID))
	if !request.PublishedOnly || request.Collection.Versions == nil {
		return table
	}
	collectionID := strings.ReplaceAll(string(request.Collection.ID), "'", "''")
	return fmt.Sprintf(`(SELECT working.*, head.snapshot FROM %s AS working
JOIN ridu_published_documents AS head ON head.collection_id = '%s' AND head.document_id = working.id) AS ridu_read`, table, collectionID)
}

func readColumns(request store.Request, fields []schema.Field) string {
	if request.PublishedOnly && request.Collection.Versions != nil {
		return quote("snapshot")
	}
	return selectColumns(request.Collection, fields, request.Locales)
}

func scanRequestedDocument(row rowScanner, request store.Request, fields []schema.Field) (store.Document, error) {
	if !request.PublishedOnly || request.Collection.Versions == nil {
		return scanDocument(row, request.Collection, fields)
	}
	var encoded []byte
	if err := row.Scan(&encoded); err != nil {
		return store.Document{}, err
	}
	var document store.Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		return store.Document{}, fmt.Errorf("decode PostgreSQL published head: %w", err)
	}
	document.PublishedRevision = 0
	document.HasDraftChanges = false
	document.LocalizationSources = nil
	return document, nil
}

func (transaction *documentTransaction) attachPublishedMetadata(ctx context.Context, collection schema.Collection, documents []store.Document) error {
	if collection.Versions == nil || !collection.Versions.Drafts || len(documents) == 0 {
		return nil
	}
	ids := make([]string, len(documents))
	index := make(map[string]int, len(documents))
	for position := range documents {
		ids[position] = documents[position].ID
		index[documents[position].ID] = position
	}
	rows, err := transaction.transaction.Query(ctx, `SELECT document_id, revision, has_draft_changes
FROM ridu_published_documents WHERE collection_id = $1 AND document_id = ANY($2::text[])`, string(collection.ID), ids)
	if err != nil {
		return translateError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var revision int
		var pending bool
		if err := rows.Scan(&id, &revision, &pending); err != nil {
			return translateError(err)
		}
		position := index[id]
		documents[position].PublishedRevision = revision
		documents[position].HasDraftChanges = pending
	}
	return translateError(rows.Err())
}

func (transaction *documentTransaction) publishedHead(ctx context.Context, collection schema.Collection, id string) (store.Document, bool, error) {
	if collection.Versions == nil {
		return store.Document{}, false, nil
	}
	var encoded []byte
	var pending bool
	err := transaction.transaction.QueryRow(ctx, `SELECT snapshot, has_draft_changes FROM ridu_published_documents
WHERE collection_id = $1 AND document_id = $2`, string(collection.ID), id).Scan(&encoded, &pending)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Document{}, false, nil
		}
		return store.Document{}, false, translateError(err)
	}
	var document store.Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		return store.Document{}, false, fmt.Errorf("decode PostgreSQL published head: %w", err)
	}
	document.HasDraftChanges = pending
	return document, true, nil
}

func (transaction *documentTransaction) putPublishedHead(ctx context.Context, collection schema.Collection, document store.Document) error {
	document.PublishedRevision = 0
	document.HasDraftChanges = false
	document.LocalizationSources = nil
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode PostgreSQL published head: %w", err)
	}
	_, err = transaction.transaction.Exec(ctx, `INSERT INTO ridu_published_documents
(collection_id, document_id, revision, snapshot, has_draft_changes) VALUES ($1, $2, $3, $4, false)
ON CONFLICT (collection_id, document_id) DO UPDATE SET revision = excluded.revision,
snapshot = excluded.snapshot, has_draft_changes = false`, string(collection.ID), document.ID, document.Revision, encoded)
	if err != nil {
		return translateError(err)
	}
	return transaction.replacePublishedReferences(ctx, collection, document)
}

func (transaction *documentTransaction) setPublishedPending(ctx context.Context, collection schema.Collection, id string, pending bool) error {
	command, err := transaction.transaction.Exec(ctx, `UPDATE ridu_published_documents SET has_draft_changes = $3
WHERE collection_id = $1 AND document_id = $2`, string(collection.ID), id, pending)
	if err != nil {
		return translateError(err)
	}
	if command.RowsAffected() != 1 {
		return store.ErrConflict
	}
	return nil
}

func (transaction *documentTransaction) setPublishedDeletion(ctx context.Context, collection schema.Collection, document store.Document) error {
	if collection.Versions == nil {
		return nil
	}
	_, err := transaction.transaction.Exec(ctx, `UPDATE ridu_published_documents SET snapshot = jsonb_set(
snapshot, '{DeletedAt}', CASE WHEN $3::timestamptz IS NULL THEN 'null'::jsonb ELSE to_jsonb($3::timestamptz) END, true)
WHERE collection_id = $1 AND document_id = $2`, string(collection.ID), document.ID, document.DeletedAt)
	return translateError(err)
}

func (transaction *documentTransaction) deletePublishedHead(ctx context.Context, collection schema.Collection, id string) error {
	if collection.Versions == nil {
		return nil
	}
	_, err := transaction.transaction.Exec(ctx, `DELETE FROM ridu_published_documents
WHERE collection_id = $1 AND document_id = $2`, string(collection.ID), id)
	if err != nil {
		return translateError(err)
	}
	_, err = transaction.transaction.Exec(ctx, `DELETE FROM ridu_document_references
WHERE owner_collection_id = $1 AND owner_document_id = $2 AND published_head = true`, string(collection.ID), id)
	return translateError(err)
}

func (transaction *documentTransaction) replacePublishedReferences(ctx context.Context, collection schema.Collection, document store.Document) error {
	_, err := transaction.transaction.Exec(ctx, `DELETE FROM ridu_document_references
WHERE owner_collection_id = $1 AND owner_document_id = $2 AND published_head = true`, string(collection.ID), document.ID)
	if err != nil {
		return translateError(err)
	}
	entries, err := referenceindex.Collect(collection, document)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		_, err := transaction.transaction.Exec(ctx, `INSERT INTO ridu_document_references (
owner_collection_id, owner_document_id, field_id, target_collection_id, target_document_id, locale, occurrence, published_head
) VALUES ($1, $2, $3, $4, $5, $6, $7, true)`,
			string(entry.Owner.CollectionID), entry.Owner.DocumentID, string(entry.FieldID),
			string(entry.Target.CollectionID), entry.Target.DocumentID, string(entry.Locale), entry.Occurrence)
		if err != nil {
			return translateError(err)
		}
	}
	return nil
}
