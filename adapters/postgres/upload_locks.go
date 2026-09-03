package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type uploadLockSession interface {
	Exec(context.Context, string, string) error
	Release()
	Discard()
}

type pooledUploadLockSession struct{ connection *pgxpool.Conn }

func (session *pooledUploadLockSession) Exec(ctx context.Context, statement, key string) error {
	_, err := session.connection.Exec(ctx, statement, key)
	return err
}

func (session *pooledUploadLockSession) Release() {
	session.connection.Release()
}

func (session *pooledUploadLockSession) Discard() {
	closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	underlying := session.connection.Hijack()
	_ = underlying.Close(closeContext)
}

// LockUploadObjects uses session advisory locks so object-store deletion and
// migration-owned adoption of an existing key cannot pass each other between
// database transactions or application processes.
func (backend *Store) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	keys, err := normalizedUploadObjectLockKeys(objectKeys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return func() {}, nil
	}
	if backend.uploadLockPool == nil {
		return nil, fmt.Errorf("postgresql upload-lock pool is unavailable")
	}
	lockContext := ctx
	cancel := func() {}
	if backend.uploadLockWait > 0 {
		lockContext, cancel = context.WithTimeout(ctx, backend.uploadLockWait)
	}
	defer cancel()
	connection, err := backend.uploadLockPool.Acquire(lockContext)
	if err != nil {
		return nil, err
	}
	return acquirePostgresUploadLocks(lockContext, &pooledUploadLockSession{connection: connection}, keys)
}

func acquirePostgresUploadLocks(ctx context.Context, session uploadLockSession, keys []string) (func(), error) {
	for _, key := range keys {
		if err := session.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('ridu-upload-object:' || $1, 0))`, key); err != nil {
			// An interrupted round trip can report an error after PostgreSQL has
			// acquired the session lock. Never return that session to the pool:
			// closing it is the only unambiguous way to release every possible lock.
			session.Discard()
			return nil, translateError(err)
		}
	}
	var once sync.Once
	return func() { once.Do(func() { releasePostgresUploadLocks(session, keys) }) }, nil
}

// LockUploadObjects keeps import-adoption locks on the document transaction's
// own PostgreSQL session. Transaction-scoped advisory locks are re-entrant,
// automatically released, and visible to PostgreSQL's deadlock detector.
func (transaction *documentTransaction) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	keys, err := normalizedUploadObjectLockKeys(objectKeys)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if _, err := transaction.transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ridu-upload-object:' || $1, 0))`, key); err != nil {
			return nil, translateError(err)
		}
	}
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

func releasePostgresUploadLocks(session uploadLockSession, keys []string) {
	releaseContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for index := len(keys) - 1; index >= 0; index-- {
		if err := session.Exec(releaseContext, `SELECT pg_advisory_unlock(hashtextextended('ridu-upload-object:' || $1, 0))`, keys[index]); err != nil {
			session.Discard()
			return
		}
	}
	session.Release()
}
