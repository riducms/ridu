package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (backend *Store) FindDocumentLock(ctx context.Context, collectionID schema.StableID, documentID string, now time.Time) (store.DocumentLock, error) {
	lock := store.DocumentLock{CollectionID: collectionID, DocumentID: documentID}
	err := backend.pool.QueryRow(ctx, `SELECT owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at FROM ridu_document_locks WHERE collection_id = $1 AND document_id = $2 AND expires_at > $3`, collectionID, documentID, now).Scan(
		&lock.OwnerCollectionID, &lock.OwnerID, &lock.OwnerLabel, &lock.CreatedAt, &lock.UpdatedAt, &lock.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.DocumentLock{}, store.ErrNotFound
	}
	return lock, err
}

func (backend *Store) AcquireDocumentLock(ctx context.Context, candidate store.DocumentLock, now time.Time, takeover bool) (store.DocumentLock, bool, error) {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction,
		store.DocumentReference{CollectionID: candidate.CollectionID, DocumentID: candidate.DocumentID},
		store.DocumentReference{CollectionID: candidate.OwnerCollectionID, DocumentID: candidate.OwnerID},
	); err != nil {
		return store.DocumentLock{}, false, err
	}
	lock := candidate
	err = transaction.QueryRow(ctx, `INSERT INTO ridu_document_locks (collection_id, document_id, owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (collection_id, document_id) DO UPDATE SET owner_collection_id = EXCLUDED.owner_collection_id, owner_id = EXCLUDED.owner_id, owner_label = EXCLUDED.owner_label, created_at = CASE WHEN ridu_document_locks.owner_collection_id = EXCLUDED.owner_collection_id AND ridu_document_locks.owner_id = EXCLUDED.owner_id THEN ridu_document_locks.created_at ELSE EXCLUDED.created_at END, updated_at = EXCLUDED.updated_at, expires_at = EXCLUDED.expires_at
WHERE ridu_document_locks.expires_at <= $9 OR (ridu_document_locks.owner_collection_id = EXCLUDED.owner_collection_id AND ridu_document_locks.owner_id = EXCLUDED.owner_id) OR $10
RETURNING owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at`, candidate.CollectionID, candidate.DocumentID, candidate.OwnerCollectionID, candidate.OwnerID, candidate.OwnerLabel, candidate.CreatedAt, candidate.UpdatedAt, candidate.ExpiresAt, now, takeover).Scan(
		&lock.OwnerCollectionID, &lock.OwnerID, &lock.OwnerLabel, &lock.CreatedAt, &lock.UpdatedAt, &lock.ExpiresAt,
	)
	if err == nil {
		if err := transaction.Commit(ctx); err != nil {
			return store.DocumentLock{}, false, err
		}
		return lock, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.DocumentLock{}, false, err
	}
	current := store.DocumentLock{CollectionID: candidate.CollectionID, DocumentID: candidate.DocumentID}
	findError := transaction.QueryRow(ctx, `SELECT owner_collection_id, owner_id, owner_label, created_at, updated_at, expires_at FROM ridu_document_locks WHERE collection_id = $1 AND document_id = $2 AND expires_at > $3`, candidate.CollectionID, candidate.DocumentID, now).Scan(
		&current.OwnerCollectionID, &current.OwnerID, &current.OwnerLabel, &current.CreatedAt, &current.UpdatedAt, &current.ExpiresAt,
	)
	if errors.Is(findError, pgx.ErrNoRows) {
		return store.DocumentLock{}, false, store.ErrNotFound
	}
	if findError != nil {
		return store.DocumentLock{}, false, findError
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.DocumentLock{}, false, err
	}
	return current, false, nil
}

func (backend *Store) ReleaseDocumentLock(ctx context.Context, collectionID schema.StableID, documentID string, ownerCollectionID schema.StableID, ownerID string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_document_locks WHERE collection_id = $1 AND document_id = $2 AND owner_collection_id = $3 AND owner_id = $4`, collectionID, documentID, ownerCollectionID, ownerID)
	return err
}

var _ store.DocumentLockStore = (*Store)(nil)
