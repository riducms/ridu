package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (backend *Store) GetPreference(ctx context.Context, collectionID schema.StableID, userID, key string) (store.Preference, error) {
	preference := store.Preference{CollectionID: collectionID, UserID: userID, Key: key}
	var encoded string
	var updatedAt int64
	err := backend.db.QueryRowContext(ctx, `SELECT value_json, updated_at
FROM ridu_preferences
WHERE collection_id = ? AND user_id = ? AND key = ?`, string(collectionID), userID, key).Scan(&encoded, &updatedAt)
	if err != nil {
		return store.Preference{}, translateError(err)
	}
	preference.Value = append(json.RawMessage(nil), encoded...)
	preference.UpdatedAt = decodeTime(updatedAt)
	return preference, nil
}

func (backend *Store) SetPreference(ctx context.Context, preference store.Preference) (store.Preference, error) {
	if !json.Valid(preference.Value) {
		return store.Preference{}, fmt.Errorf("preference value must be valid JSON")
	}
	updatedAt := backend.now().UTC()
	if err := validateSQLiteTimes("preference timestamp", updatedAt); err != nil {
		return store.Preference{}, err
	}
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if err := requireActiveDocuments(ctx, connection, store.DocumentReference{
			CollectionID: preference.CollectionID,
			DocumentID:   preference.UserID,
		}); err != nil {
			return err
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_preferences
  (collection_id, user_id, key, value_json, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(collection_id, user_id, key) DO UPDATE SET
  value_json = excluded.value_json,
  updated_at = excluded.updated_at`,
			string(preference.CollectionID), preference.UserID, preference.Key,
			string(preference.Value), encodeTime(updatedAt),
		)
		return translateError(err)
	})
	if err != nil {
		return store.Preference{}, err
	}
	preference.Value = append(json.RawMessage(nil), preference.Value...)
	preference.UpdatedAt = updatedAt
	return preference, nil
}

func (backend *Store) DeletePreference(ctx context.Context, collectionID schema.StableID, userID, key string) error {
	_, err := backend.db.ExecContext(ctx, `DELETE FROM ridu_preferences
WHERE collection_id = ? AND user_id = ? AND key = ?`, string(collectionID), userID, key)
	return translateError(err)
}

func (backend *Store) DeletePreferences(ctx context.Context, collectionID schema.StableID, userID string) error {
	_, err := backend.db.ExecContext(ctx, `DELETE FROM ridu_preferences
WHERE collection_id = ? AND user_id = ?`, string(collectionID), userID)
	return translateError(err)
}

func (backend *Store) FindDocumentLock(ctx context.Context, collectionID schema.StableID, documentID string, now time.Time) (store.DocumentLock, error) {
	if err := validateSQLiteTimes("document lock timestamp", now); err != nil {
		return store.DocumentLock{}, err
	}
	lock := store.DocumentLock{CollectionID: collectionID, DocumentID: documentID}
	var createdAt, updatedAt, expiresAt int64
	err := backend.db.QueryRowContext(ctx, `SELECT
  owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at
FROM ridu_document_locks
WHERE collection_id = ? AND document_id = ? AND expires_at > ?`,
		string(collectionID), documentID, encodeTime(now),
	).Scan(&lock.OwnerCollectionID, &lock.OwnerID, &lock.OwnerLabel, &createdAt, &updatedAt, &expiresAt)
	if err != nil {
		return store.DocumentLock{}, translateError(err)
	}
	lock.CreatedAt = decodeTime(createdAt)
	lock.UpdatedAt = decodeTime(updatedAt)
	lock.ExpiresAt = decodeTime(expiresAt)
	return lock, nil
}

func (backend *Store) AcquireDocumentLock(ctx context.Context, candidate store.DocumentLock, now time.Time, takeover bool) (store.DocumentLock, bool, error) {
	if err := validateSQLiteTimes("document lock timestamp", candidate.CreatedAt, candidate.UpdatedAt, candidate.ExpiresAt, now); err != nil {
		return store.DocumentLock{}, false, err
	}
	var acquired store.DocumentLock
	var didAcquire bool
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if err := requireActiveDocuments(ctx, connection,
			store.DocumentReference{CollectionID: candidate.CollectionID, DocumentID: candidate.DocumentID},
			store.DocumentReference{CollectionID: candidate.OwnerCollectionID, DocumentID: candidate.OwnerID},
		); err != nil {
			return err
		}

		acquired = candidate
		var createdAt, updatedAt, expiresAt int64
		err := connection.QueryRowContext(ctx, `INSERT INTO ridu_document_locks (
  collection_id, document_id, owner_collection_id, owner_id, owner_label,
  created_at, updated_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(collection_id, document_id) DO UPDATE SET
  owner_collection_id = excluded.owner_collection_id,
  owner_id = excluded.owner_id,
  owner_label = excluded.owner_label,
  created_at = CASE
    WHEN ridu_document_locks.owner_collection_id = excluded.owner_collection_id
      AND ridu_document_locks.owner_id = excluded.owner_id
    THEN ridu_document_locks.created_at
    ELSE excluded.created_at
  END,
  updated_at = excluded.updated_at,
  expires_at = excluded.expires_at
WHERE ridu_document_locks.expires_at <= ?
  OR (
    ridu_document_locks.owner_collection_id = excluded.owner_collection_id
    AND ridu_document_locks.owner_id = excluded.owner_id
  )
  OR ?
RETURNING owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at`,
			string(candidate.CollectionID), candidate.DocumentID,
			string(candidate.OwnerCollectionID), candidate.OwnerID, candidate.OwnerLabel,
			encodeTime(candidate.CreatedAt), encodeTime(candidate.UpdatedAt), encodeTime(candidate.ExpiresAt),
			encodeTime(now), takeover,
		).Scan(&acquired.OwnerCollectionID, &acquired.OwnerID, &acquired.OwnerLabel, &createdAt, &updatedAt, &expiresAt)
		if err == nil {
			acquired.CreatedAt = decodeTime(createdAt)
			acquired.UpdatedAt = decodeTime(updatedAt)
			acquired.ExpiresAt = decodeTime(expiresAt)
			didAcquire = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return translateError(err)
		}

		current := store.DocumentLock{CollectionID: candidate.CollectionID, DocumentID: candidate.DocumentID}
		err = connection.QueryRowContext(ctx, `SELECT
  owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at
FROM ridu_document_locks
WHERE collection_id = ? AND document_id = ? AND expires_at > ?`,
			string(candidate.CollectionID), candidate.DocumentID, encodeTime(now),
		).Scan(&current.OwnerCollectionID, &current.OwnerID, &current.OwnerLabel, &createdAt, &updatedAt, &expiresAt)
		if err != nil {
			return translateError(err)
		}
		current.CreatedAt = decodeTime(createdAt)
		current.UpdatedAt = decodeTime(updatedAt)
		current.ExpiresAt = decodeTime(expiresAt)
		acquired = current
		return nil
	})
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	return acquired, didAcquire, nil
}

func (backend *Store) ReleaseDocumentLock(ctx context.Context, collectionID schema.StableID, documentID string, ownerCollectionID schema.StableID, ownerID string) error {
	_, err := backend.db.ExecContext(ctx, `DELETE FROM ridu_document_locks
WHERE collection_id = ? AND document_id = ?
  AND owner_collection_id = ? AND owner_id = ?`,
		string(collectionID), documentID, string(ownerCollectionID), ownerID,
	)
	return translateError(err)
}

func requireActiveDocuments(ctx context.Context, runner sqlRunner, references ...store.DocumentReference) error {
	unique := make(map[string]store.DocumentReference, len(references))
	for _, reference := range references {
		if reference.CollectionID == "" || reference.DocumentID == "" {
			return store.ErrNotFound
		}
		unique[string(reference.CollectionID)+"\x00"+reference.DocumentID] = reference
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		reference := unique[key]
		var marker int
		err := runner.QueryRowContext(ctx, `SELECT 1 FROM ridu_documents
WHERE collection_id = ? AND id = ? AND deleted_at IS NULL`,
			string(reference.CollectionID), reference.DocumentID,
		).Scan(&marker)
		if err != nil {
			return translateError(err)
		}
	}
	return nil
}

// LockUploadObjects serializes upload admission and cleanup across every
// process sharing a file database. SQLite memory databases are private to one
// Store and use the equivalent in-process gate.
func (backend *Store) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	keys, err := normalizedUploadObjectLockKeys(objectKeys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return func() {}, nil
	}
	fileLock, gate, err := backend.acquireUploadObjectLock(ctx)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if fileLock != nil {
				_ = fileLock.Close()
			}
			if gate {
				backend.uploadGate <- struct{}{}
			}
		})
	}, nil
}

func (backend *Store) acquireUploadObjectLock(ctx context.Context) (*flock.Flock, bool, error) {
	if backend.lockPath != "" {
		fileLock := flock.New(backend.lockPath, flock.SetPermissions(0o600))
		locked, err := fileLock.TryLockContext(ctx, 10*time.Millisecond)
		if err != nil {
			_ = fileLock.Close()
			return nil, false, err
		}
		if !locked {
			_ = fileLock.Close()
			return nil, false, fmt.Errorf("SQLite upload-object lock was not acquired")
		}
		return fileLock, false, nil
	}
	select {
	case <-backend.uploadGate:
		return nil, true, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// tryUploadObjectLock never waits. A document transaction already owns the
// SQLite writer, so waiting here while ordinary upload preparation owns the
// file lock would invert the lock order and deadlock both operations. The
// caller can safely retry the complete transaction on ErrConflict.
func (backend *Store) tryUploadObjectLock(ctx context.Context) (*flock.Flock, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if backend.lockPath != "" {
		fileLock := flock.New(backend.lockPath, flock.SetPermissions(0o600))
		locked, err := fileLock.TryLock()
		if err != nil {
			_ = fileLock.Close()
			return nil, false, err
		}
		if !locked {
			_ = fileLock.Close()
			return nil, false, fmt.Errorf("SQLite upload-object lock is busy: %w", store.ErrConflict)
		}
		return fileLock, false, nil
	}
	select {
	case <-backend.uploadGate:
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("SQLite upload-object lock is busy: %w", store.ErrConflict)
	}
}

// LockUploadObjects holds the same file/process lock until the transaction
// finishes. Repeated calls are re-entrant because one SQLite writer owns the
// complete transaction.
func (transaction *documentTransaction) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return nil, err
	}
	defer leave()
	keys, err := normalizedUploadObjectLockKeys(objectKeys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 || transaction.uploadLock != nil || transaction.uploadGate {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fileLock, gate, err := transaction.store.tryUploadObjectLock(ctx)
	if err != nil {
		return nil, err
	}
	transaction.uploadLock = fileLock
	transaction.uploadGate = gate
	return func() {}, nil
}

func normalizedUploadObjectLockKeys(objectKeys []string) ([]string, error) {
	unique := make(map[string]struct{}, len(objectKeys))
	for _, key := range objectKeys {
		if key == "" {
			return nil, fmt.Errorf("upload object lock key is empty")
		}
		unique[key] = struct{}{}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// ReferencedUploadObjects checks current, trashed, and immutable version
// snapshots in one bounded query. It returns only requested keys in byte order.
func (transaction *documentTransaction) ReferencedUploadObjects(ctx context.Context, request store.UploadReferenceRequest) ([]string, error) {
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	defer leave()
	keys, collectionIDs, versionCollectionIDs, err := validateUploadReferenceRequest(request)
	if err != nil {
		return nil, err
	}
	encodedKeys, _ := json.Marshal(keys)
	encodedCollections, _ := json.Marshal(collectionIDs)
	encodedVersionCollections, _ := json.Marshal(versionCollectionIDs)
	rows, err := transaction.connection.QueryContext(ctx, sqliteUploadReferenceQuery,
		string(encodedKeys), string(encodedCollections), string(encodedVersionCollections),
	)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	result := make([]string, 0, len(keys))
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	if err := rows.Err(); err != nil {
		return nil, translateError(err)
	}
	return result, nil
}

func validateUploadReferenceRequest(request store.UploadReferenceRequest) (keys, collectionIDs, versionCollectionIDs []string, err error) {
	if len(request.ObjectKeys) == 0 || len(request.ObjectKeys) > store.MaxUploadReferenceCandidates {
		return nil, nil, nil, fmt.Errorf("upload reference lookup requires between 1 and %d object keys", store.MaxUploadReferenceCandidates)
	}
	seenKeys := make(map[string]struct{}, len(request.ObjectKeys))
	for _, key := range request.ObjectKeys {
		if key == "" {
			return nil, nil, nil, fmt.Errorf("upload reference lookup contains an empty object key")
		}
		if _, duplicate := seenKeys[key]; duplicate {
			return nil, nil, nil, fmt.Errorf("upload reference lookup contains duplicate object key %q", key)
		}
		seenKeys[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	collections := append([]schema.Collection(nil), request.Collections...)
	sort.Slice(collections, func(left, right int) bool { return collections[left].ID < collections[right].ID })
	seenCollections := make(map[schema.StableID]struct{}, len(collections))
	for _, collection := range collections {
		if collection.Upload == nil {
			return nil, nil, nil, fmt.Errorf("upload reference lookup collection %q is not upload-enabled", collection.ID)
		}
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			return nil, nil, nil, fmt.Errorf("upload reference lookup contains duplicate collection %q", collection.ID)
		}
		seenCollections[collection.ID] = struct{}{}
		if !hasUploadMetadataFields(collection) {
			return nil, nil, nil, fmt.Errorf("upload collection %q is missing framework object metadata", collection.ID)
		}
		collectionIDs = append(collectionIDs, string(collection.ID))
		if collection.Versions != nil {
			versionCollectionIDs = append(versionCollectionIDs, string(collection.ID))
		}
	}
	if len(collectionIDs) == 0 {
		return nil, nil, nil, fmt.Errorf("upload reference lookup requires at least one upload collection")
	}
	return keys, collectionIDs, versionCollectionIDs, nil
}

func hasUploadMetadataFields(collection schema.Collection) bool {
	var objectKey, sizes bool
	for _, field := range collection.Fields {
		if field.Category != schema.FieldCategoryUpload {
			continue
		}
		switch field.Name {
		case "objectKey":
			objectKey = true
		case "sizes":
			sizes = true
		}
	}
	return objectKey && sizes
}

const sqliteUploadReferenceQuery = `WITH candidate_keys(object_key) AS (
  SELECT value FROM json_each(?)
), current_references(object_key) AS (
  SELECT candidate.object_key
  FROM candidate_keys AS candidate
  WHERE EXISTS (
    SELECT 1
    FROM ridu_documents AS document
    WHERE document.collection_id IN (SELECT value FROM json_each(?))
      AND (
        json_extract(document.values_json, '$.objectKey') = candidate.object_key
        OR EXISTS (
          SELECT 1
          FROM json_each(document.values_json, '$.sizes') AS image_size
          WHERE json_extract(image_size.value, '$.objectKey') = candidate.object_key
        )
      )
  )
), version_references(object_key) AS (
  SELECT candidate.object_key
  FROM candidate_keys AS candidate
  WHERE EXISTS (
    SELECT 1
    FROM ridu_versions AS version
    WHERE version.collection_id IN (SELECT value FROM json_each(?))
      AND (
        json_extract(version.snapshot_json, '$.Values.objectKey') = candidate.object_key
        OR EXISTS (
          SELECT 1
          FROM json_each(version.snapshot_json, '$.Values.sizes') AS image_size
          WHERE json_extract(image_size.value, '$.objectKey') = candidate.object_key
        )
      )
  )
)
SELECT object_key FROM current_references
UNION
SELECT object_key FROM version_references
ORDER BY object_key`

var _ store.PreferenceStore = (*Store)(nil)
var _ store.DocumentLockStore = (*Store)(nil)
var _ store.UploadObjectLocker = (*Store)(nil)
var _ store.UploadObjectLocker = (*documentTransaction)(nil)
var _ store.UploadReferenceTransaction = (*documentTransaction)(nil)
