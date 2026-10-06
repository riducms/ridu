package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// A versioned resource stores its live content in a typed table with the
// working table's physical layout: the same columns, constraints and indexes.
// Published reads therefore use the ordinary predicate compiler, scanner,
// planner statistics and indexes; only the source table differs. The live row
// references its working row and is deleted with it.

// readAlias names every document read source so correlated subqueries can
// address the outer row without depending on the physical table name.
const readAlias = "ridu_read"

func readSource(request store.Request) string {
	working := quote(collectionTable(request.Collection.ID))
	if !request.PublishedOnly || request.Collection.Versions == nil {
		return working + " AS " + readAlias
	}
	live := quote(publishedCollectionTable(request.Collection.ID))
	if request.Lock == store.LockNone {
		return live + " AS " + readAlias
	}
	// A locked live read also locks the working row, so reference admission
	// coordinates with mutation and deletion of the document as a whole.
	return fmt.Sprintf(`(SELECT live.* FROM %s AS live JOIN %s AS working ON working.id = live.id) AS %s`, live, working, readAlias)
}

// readsPublishedMetadata reports whether a working read carries the live
// revision and pending-draft state of a draft-enabled resource.
func readsPublishedMetadata(request store.Request) bool {
	return !request.PublishedOnly && request.Collection.Versions != nil && request.Collection.Versions.Drafts
}

func readColumns(request store.Request, fields []schema.Field) string {
	columns := selectColumns(request.Collection, fields, request.Locales)
	if readsPublishedMetadata(request) {
		columns += ", " + publishedStateColumn(request.Collection, readAlias)
	}
	return columns
}

// publishedStateColumn reads the live revision and pending-draft flag as one
// correlated array for unlocked reads. A scalar subquery costs one primary-key
// probe per returned row.
//
// A locking read must not use it. When a locking read waits for a concurrent
// writer, READ COMMITTED returns the newest version of the locked row, but the
// subquery still reads the live table with the statement's original snapshot:
// a publication or unpublication committed during the wait would be invisible.
// findLockedWorking reads the live state in a statement of its own instead.
func publishedStateColumn(collection schema.Collection, outer string) string {
	return fmt.Sprintf(`(SELECT ARRAY[live._revision, CASE WHEN live.has_draft_changes THEN 1 ELSE 0 END]
FROM %s AS live WHERE live.id = %s.id)`, quote(publishedCollectionTable(collection.ID)), outer)
}

// liveStateStatement reads one document's live revision and pending-draft flag.
func liveStateStatement(collection schema.Collection) string {
	return fmt.Sprintf(`SELECT _revision, has_draft_changes FROM %s WHERE id = $1`, quote(publishedCollectionTable(collection.ID)))
}

// findLockedWorking performs a locking working read of a draft-enabled
// resource. The live state is read by a second statement in the same
// pipeline, so it costs no extra round trip. PostgreSQL executes pipelined
// statements in order and, in READ COMMITTED, takes each statement's snapshot
// when that statement starts, which is after the first statement holds the
// row lock. Every writer of the live state holds the working row lock, so the
// live state read then is the state the locked row belongs to.
func (transaction *documentTransaction) findLockedWorking(ctx context.Context, request store.Request, fields []schema.Field, predicate, lock string, arguments []any) (store.Document, error) {
	batch := &pgx.Batch{}
	batch.Queue(fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", selectColumns(request.Collection, fields, request.Locales), readSource(request), predicate, lock), arguments...)
	batch.Queue(liveStateStatement(request.Collection), request.ID)
	results := transaction.transaction.SendBatch(ctx, batch)
	document, err := scanDocument(results.QueryRow(), request.Collection, fields, request.Locales)
	if err == nil {
		err = scanLiveState(results.QueryRow(), &document)
	}
	if closeErr := results.Close(); err == nil {
		err = closeErr
	}
	return document, translateError(err)
}

// scanLiveState applies a liveStateStatement result; no row means no live head.
func scanLiveState(row pgx.Row, document *store.Document) error {
	var revision int
	var pending bool
	switch err := row.Scan(&revision, &pending); {
	case errors.Is(err, pgx.ErrNoRows):
		document.PublishedRevision, document.HasDraftChanges = 0, false
		return nil
	case err != nil:
		return err
	}
	document.PublishedRevision, document.HasDraftChanges = revision, pending
	return nil
}

func applyPublishedState(document *store.Document, state []int32) {
	if len(state) != 2 {
		document.PublishedRevision, document.HasDraftChanges = 0, false
		return
	}
	document.PublishedRevision, document.HasDraftChanges = int(state[0]), state[1] == 1
}

func scanRequestedDocument(row rowScanner, request store.Request, fields []schema.Field) (store.Document, error) {
	if !readsPublishedMetadata(request) {
		return scanDocument(row, request.Collection, fields, request.Locales)
	}
	return scanDocumentWithPublishedState(row, request.Collection, fields, request.Locales)
}

func scanDocumentWithPublishedState(row rowScanner, collection schema.Collection, fields []schema.Field, locales []schema.LocaleCode) (store.Document, error) {
	var document store.Document
	var state []int32
	destinations, finish := documentDestinations(&document, collection, fields, locales)
	destinations = append(destinations, &state)
	if err := row.Scan(destinations...); err != nil {
		return store.Document{}, err
	}
	if err := finish(); err != nil {
		return store.Document{}, err
	}
	applyPublishedState(&document, state)
	return document, nil
}

// physicalDocumentColumns lists a resource's document columns. The working
// and live tables are generated from the same definition; the live table adds
// only has_draft_changes. Locales are the application's configured locales,
// which every engine write supplies.
func physicalDocumentColumns(collection schema.Collection, locales []schema.LocaleCode) []string {
	columns := []string{"id", "created_at", "updated_at", "deleted_at"}
	if collection.Versions != nil {
		columns = append(columns, "_status")
	}
	if collection.Versions != nil || collection.Upload != nil {
		columns = append(columns, "_revision")
	}
	for _, field := range storedSchemaFields(collection) {
		if !field.Localized {
			columns = append(columns, fieldColumn(field.ID))
			continue
		}
		for _, locale := range locales {
			columns = append(columns, localizedFieldColumn(field.ID, locale))
		}
	}
	return columns
}

func (transaction *documentTransaction) publishedHead(ctx context.Context, collection schema.Collection, id string, locales []schema.LocaleCode) (store.Document, bool, error) {
	if collection.Versions == nil {
		return store.Document{}, false, nil
	}
	fields := storedSchemaFields(collection)
	return scanPublishedHead(transaction.transaction.QueryRow(ctx, fmt.Sprintf(`SELECT %s, has_draft_changes FROM %s WHERE id = $1`,
		selectColumns(collection, fields, locales), quote(publishedCollectionTable(collection.ID))), id), collection, fields, locales)
}

// restorePublishedHead clears the live row's mirrored trash state and returns
// the restored live head in the same statement.
func (transaction *documentTransaction) restorePublishedHead(ctx context.Context, collection schema.Collection, id string, locales []schema.LocaleCode) (store.Document, bool, error) {
	if collection.Versions == nil {
		return store.Document{}, false, nil
	}
	fields := storedSchemaFields(collection)
	return scanPublishedHead(transaction.transaction.QueryRow(ctx, fmt.Sprintf(`UPDATE %s SET deleted_at = NULL WHERE id = $1 RETURNING %s, has_draft_changes`,
		quote(publishedCollectionTable(collection.ID)), selectColumns(collection, fields, locales)), id), collection, fields, locales)
}

func scanPublishedHead(row pgx.Row, collection schema.Collection, fields []schema.Field, locales []schema.LocaleCode) (store.Document, bool, error) {
	var document store.Document
	var pending bool
	destinations, finish := documentDestinations(&document, collection, fields, locales)
	destinations = append(destinations, &pending)
	err := row.Scan(destinations...)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Document{}, false, nil
	}
	if err != nil {
		return store.Document{}, false, translateError(err)
	}
	if err := finish(); err != nil {
		return store.Document{}, false, err
	}
	document.HasDraftChanges = pending
	return document, true, nil
}

// putPublishedHead copies the saved working row into the live table with one
// statement. The live row is exactly the working row; no value is re-encoded.
func (transaction *documentTransaction) putPublishedHead(ctx context.Context, collection schema.Collection, document store.Document, locales []schema.LocaleCode) error {
	columns := physicalDocumentColumns(collection, locales)
	quoted := make([]string, len(columns))
	assignments := make([]string, 0, len(columns))
	for index, column := range columns {
		quoted[index] = quote(column)
		if column != "id" {
			assignments = append(assignments, quote(column)+" = excluded."+quote(column))
		}
	}
	list := strings.Join(quoted, ", ")
	statement := fmt.Sprintf(`INSERT INTO %s (%s, has_draft_changes) SELECT %s, false FROM %s WHERE id = $1
ON CONFLICT (id) DO UPDATE SET %s, has_draft_changes = false`,
		quote(publishedCollectionTable(collection.ID)), list, list, quote(collectionTable(collection.ID)), strings.Join(assignments, ", "))
	command, err := transaction.transaction.Exec(ctx, statement, document.ID)
	if err != nil {
		return translateError(err)
	}
	if command.RowsAffected() != 1 {
		return store.ErrConflict
	}
	return transaction.replacePublishedReferences(ctx, collection, document)
}

func (transaction *documentTransaction) setPublishedPending(ctx context.Context, collection schema.Collection, id string, pending bool) error {
	command, err := transaction.transaction.Exec(ctx, fmt.Sprintf(`UPDATE %s SET has_draft_changes = $2 WHERE id = $1`,
		quote(publishedCollectionTable(collection.ID))), id, pending)
	if err != nil {
		return translateError(err)
	}
	if command.RowsAffected() != 1 {
		return store.ErrConflict
	}
	return nil
}

func (transaction *documentTransaction) deletePublishedHead(ctx context.Context, collection schema.Collection, id string) error {
	if collection.Versions == nil {
		return nil
	}
	// The live row and its index rows leave together in one statement.
	_, err := transaction.transaction.Exec(ctx, fmt.Sprintf(`WITH removed AS (DELETE FROM %s WHERE id = $2)
DELETE FROM ridu_document_references
WHERE owner_collection_id = $1 AND owner_document_id = $2 AND published_head = true`, quote(publishedCollectionTable(collection.ID))), string(collection.ID), id)
	return translateError(err)
}

func (transaction *documentTransaction) replacePublishedReferences(ctx context.Context, collection schema.Collection, document store.Document) error {
	entries, err := referenceindex.Collect(collection, document)
	if err != nil {
		return err
	}
	return transaction.replaceReferenceSet(ctx, store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}, entries, true)
}
