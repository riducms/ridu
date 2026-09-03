package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (backend *Store) SetPasswordHash(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: collection.ID, DocumentID: userID}); err != nil {
		return err
	}
	tag, err := transaction.Exec(ctx, `INSERT INTO ridu_auth_credentials (
  collection_id, user_id, password_hash, failed_login_attempts, locked_until, verified
) VALUES ($1, $2, $3, 0, NULL, $4)
ON CONFLICT (collection_id, user_id) DO UPDATE SET
  password_hash = excluded.password_hash,
  failed_login_attempts = 0,
  locked_until = NULL`, string(collection.ID), userID, hash, initiallyVerified)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = $1 AND user_id = $2`, string(collection.ID), userID); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = $1 AND user_id = $2`, string(collection.ID), userID); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (backend *Store) ChangePasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: collection.ID, DocumentID: userID}); err != nil {
		return err
	}
	tag, err := transaction.Exec(ctx, `UPDATE ridu_auth_credentials
	SET password_hash = $4,
  failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = $1 AND user_id = $2 AND password_hash = $3`, string(collection.ID), userID, expectedPasswordHash, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrConflict
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = $1 AND user_id = $2`, string(collection.ID), userID); err != nil {
		return err
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = $1 AND user_id = $2`, string(collection.ID), userID); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (backend *Store) UpgradePasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_auth_credentials
SET password_hash = $4
WHERE collection_id = $1 AND user_id = $2 AND password_hash = $3`, string(collection.ID), userID, expectedPasswordHash, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrConflict
	}
	return nil
}

func (backend *Store) FindAuthCredential(ctx context.Context, collection schema.Collection, identity string) (store.AuthCredential, error) {
	var identityField *schema.Field
	for index := range collection.Fields {
		if collection.Fields[index].Name == collection.Auth.IdentityField {
			identityField = &collection.Fields[index]
			break
		}
	}
	if identityField == nil {
		return store.AuthCredential{}, fmt.Errorf("auth collection %q has no identity field", collection.Slug)
	}
	identity = store.CanonicalAuthIdentity(identity)
	statement := fmt.Sprintf(`SELECT %s, credentials.password_hash,
  credentials.failed_login_attempts, credentials.locked_until, credentials.verified
FROM %s documents
JOIN ridu_auth_credentials credentials
  ON credentials.collection_id = $1 AND credentials.user_id = documents.id
WHERE documents.%s = $2 AND documents.%s IS NULL`, selectColumns(collection, collection.Fields, nil), quote(collectionTable(collection.ID)), quote(fieldColumn(identityField.ID)), quote("deleted_at"))
	row := backend.pool.QueryRow(ctx, statement, string(collection.ID), identity)
	var credential store.AuthCredential
	destinations, finish := documentDestinations(&credential.User, collection, collection.Fields)
	var lockedUntil *time.Time
	destinations = append(destinations, &credential.PasswordHash, &credential.FailedLoginAttempts, &lockedUntil, &credential.Verified)
	if err := row.Scan(destinations...); err != nil {
		return store.AuthCredential{}, translateError(err)
	}
	if err := finish(); err != nil {
		return store.AuthCredential{}, err
	}
	credential.PasswordHash = append([]byte(nil), credential.PasswordHash...)
	if lockedUntil != nil {
		credential.LockedUntil = lockedUntil.UTC()
	}
	return credential, nil
}

func (backend *Store) RecordFailedLogin(ctx context.Context, collectionID schema.StableID, userID string, now time.Time, maximum int, lockDuration time.Duration) (store.AuthCredential, error) {
	lockUntil := now.Add(lockDuration)
	var credential store.AuthCredential
	var locked *time.Time
	err := backend.pool.QueryRow(ctx, `UPDATE ridu_auth_credentials SET
  failed_login_attempts = CASE
    WHEN locked_until IS NOT NULL AND locked_until > $3 THEN failed_login_attempts
    WHEN locked_until IS NOT NULL AND locked_until <= $3 THEN 1
    ELSE failed_login_attempts + 1
  END,
  locked_until = CASE
    WHEN locked_until IS NOT NULL AND locked_until > $3 THEN locked_until
    WHEN (CASE WHEN locked_until IS NOT NULL AND locked_until <= $3 THEN 1 ELSE failed_login_attempts + 1 END) >= $4 THEN $5
    ELSE NULL
  END
WHERE collection_id = $1 AND user_id = $2
RETURNING failed_login_attempts, locked_until`, string(collectionID), userID, now, maximum, lockUntil).Scan(&credential.FailedLoginAttempts, &locked)
	if err != nil {
		return store.AuthCredential{}, translateError(err)
	}
	if locked != nil {
		credential.LockedUntil = locked.UTC()
	}
	return credential, nil
}

func (backend *Store) ResetLoginAttempts(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) (bool, error) {
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_auth_credentials
SET failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = $1 AND user_id = $2
  AND (locked_until IS NULL OR locked_until <= $3)`, string(collectionID), userID, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (backend *Store) ForceUnlock(ctx context.Context, collectionID schema.StableID, userID string) error {
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_auth_credentials
SET failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (transaction *documentTransaction) ForceUnlockAuth(ctx context.Context, collectionID schema.StableID, userID string) error {
	tag, err := transaction.transaction.Exec(ctx, `UPDATE ridu_auth_credentials
SET failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (backend *Store) CreateSession(ctx context.Context, session store.AuthSession, expectedPasswordHash []byte) error {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: session.CollectionID, DocumentID: session.UserID}); err != nil {
		return err
	}
	var currentPasswordHash []byte
	if err := transaction.QueryRow(ctx, `SELECT password_hash FROM ridu_auth_credentials
WHERE collection_id = $1 AND user_id = $2 FOR UPDATE`, string(session.CollectionID), session.UserID).Scan(&currentPasswordHash); err != nil {
		return translateError(err)
	}
	if subtle.ConstantTimeCompare(currentPasswordHash, expectedPasswordHash) != 1 {
		return store.ErrConflict
	}
	_, err = transaction.Exec(ctx, `INSERT INTO ridu_auth_sessions (
  id, token_hash, collection_id, user_id, expires_at, created_at,
  last_seen_at, ip_address, user_agent
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		session.ID, session.TokenHash, string(session.CollectionID), session.UserID,
		session.ExpiresAt, session.CreatedAt, session.LastSeenAt,
		session.IPAddress, session.UserAgent)
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (backend *Store) RotateSession(ctx context.Context, currentHash string, replacement store.AuthSession, now time.Time) error {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: replacement.CollectionID, DocumentID: replacement.UserID}); err != nil {
		return err
	}
	var id string
	err = transaction.QueryRow(ctx, `WITH current AS (
  DELETE FROM ridu_auth_sessions
  WHERE token_hash = $1 AND collection_id = $4 AND user_id = $5 AND expires_at > $3
  RETURNING id, collection_id, user_id, expires_at, created_at, ip_address, user_agent
)
INSERT INTO ridu_auth_sessions (
  id, token_hash, collection_id, user_id, expires_at, created_at,
  last_seen_at, ip_address, user_agent
)
SELECT id, $2, collection_id, user_id, expires_at, created_at, $3, ip_address, user_agent
FROM current
RETURNING id`, currentHash, replacement.TokenHash, now, replacement.CollectionID, replacement.UserID).Scan(&id)
	if err != nil {
		return translateError(err)
	}
	return transaction.Commit(ctx)
}

func (backend *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_auth_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (backend *Store) DeleteUserSession(ctx context.Context, collectionID schema.StableID, userID, sessionID string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = $1 AND user_id = $2 AND id = $3`, string(collectionID), userID, sessionID)
	return err
}

func (backend *Store) DeleteUserSessions(ctx context.Context, collectionID schema.StableID, userID string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID)
	return err
}

func (backend *Store) FindSession(ctx context.Context, tokenHash string, now time.Time) (store.AuthSession, error) {
	var session store.AuthSession
	err := backend.pool.QueryRow(ctx, `SELECT id, token_hash, collection_id, user_id,
  expires_at, created_at, last_seen_at, ip_address, user_agent
FROM ridu_auth_sessions
WHERE token_hash = $1 AND expires_at > $2`, tokenHash, now).Scan(
		&session.ID, &session.TokenHash, &session.CollectionID, &session.UserID,
		&session.ExpiresAt, &session.CreatedAt, &session.LastSeenAt,
		&session.IPAddress, &session.UserAgent,
	)
	if err != nil {
		return store.AuthSession{}, translateError(err)
	}
	return session, nil
}

func (backend *Store) ListSessions(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthSession, error) {
	rows, err := backend.pool.Query(ctx, `SELECT id, token_hash, collection_id, user_id,
  expires_at, created_at, last_seen_at, ip_address, user_agent
FROM ridu_auth_sessions
WHERE collection_id = $1 AND user_id = $2 AND expires_at > $3
ORDER BY last_seen_at DESC, created_at DESC`, string(collectionID), userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []store.AuthSession
	for rows.Next() {
		var session store.AuthSession
		if err := rows.Scan(
			&session.ID, &session.TokenHash, &session.CollectionID, &session.UserID,
			&session.ExpiresAt, &session.CreatedAt, &session.LastSeenAt,
			&session.IPAddress, &session.UserAgent,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (backend *Store) CreateAuthToken(ctx context.Context, token store.AuthToken) error {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: token.CollectionID, DocumentID: token.UserID}); err != nil {
		return err
	}
	_, err = transaction.Exec(ctx, `WITH removed AS (
  DELETE FROM ridu_auth_tokens
  WHERE collection_id = $1 AND user_id = $2 AND purpose = $3
)
INSERT INTO ridu_auth_tokens (
  token_hash, collection_id, user_id, purpose, expires_at, created_at
) VALUES ($4, $1, $2, $3, $5, $6)`,
		string(token.CollectionID), token.UserID, string(token.Purpose),
		token.TokenHash, token.ExpiresAt, token.CreatedAt)
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (backend *Store) ResetPasswordWithToken(ctx context.Context, collectionID schema.StableID, tokenHash string, hash []byte, now time.Time) (string, error) {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	var userID string
	if err := transaction.QueryRow(ctx, `DELETE FROM ridu_auth_tokens
WHERE token_hash = $1 AND collection_id = $2 AND purpose = $3 AND expires_at > $4
RETURNING user_id`, tokenHash, string(collectionID), string(store.AuthTokenPasswordReset), now).Scan(&userID); err != nil {
		return "", translateError(err)
	}
	tag, err := transaction.Exec(ctx, `UPDATE ridu_auth_credentials
	SET password_hash = $3,
  failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID, hash)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", store.ErrNotFound
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID); err != nil {
		return "", err
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID); err != nil {
		return "", err
	}
	if err := transaction.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

func (backend *Store) VerifyEmailWithToken(ctx context.Context, collectionID schema.StableID, tokenHash string, now time.Time) (string, error) {
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	var userID string
	if err := transaction.QueryRow(ctx, `DELETE FROM ridu_auth_tokens
WHERE token_hash = $1 AND collection_id = $2 AND purpose = $3 AND expires_at > $4
RETURNING user_id`, tokenHash, string(collectionID), string(store.AuthTokenVerifyEmail), now).Scan(&userID); err != nil {
		return "", translateError(err)
	}
	tag, err := transaction.Exec(ctx, `UPDATE ridu_auth_credentials
SET verified = true
WHERE collection_id = $1 AND user_id = $2`, string(collectionID), userID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", store.ErrNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

func (backend *Store) CreateAPIKey(ctx context.Context, key store.AuthAPIKey, sessionTokenHash string, now time.Time) error {
	var expiresAt any
	if !key.ExpiresAt.IsZero() {
		expiresAt = key.ExpiresAt
	}
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	if err := lockDocumentReferences(ctx, transaction, store.DocumentReference{CollectionID: key.CollectionID, DocumentID: key.UserID}); err != nil {
		return err
	}
	var credentialExists int
	if err := transaction.QueryRow(ctx, `SELECT 1 FROM ridu_auth_credentials
WHERE collection_id = $1 AND user_id = $2 FOR UPDATE`, string(key.CollectionID), key.UserID).Scan(&credentialExists); err != nil {
		return translateError(err)
	}
	var activeSession int
	if err := transaction.QueryRow(ctx, `SELECT 1 FROM ridu_auth_sessions
WHERE token_hash = $1 AND collection_id = $2 AND user_id = $3 AND expires_at > $4
FOR KEY SHARE`, sessionTokenHash, string(key.CollectionID), key.UserID, now).Scan(&activeSession); err != nil {
		return translateError(err)
	}
	_, err = transaction.Exec(ctx, `INSERT INTO ridu_auth_api_keys (
  id, token_hash, collection_id, user_id, name, created_at, last_used_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL, $7)`, key.ID, key.TokenHash, string(key.CollectionID), key.UserID, key.Name, key.CreatedAt, expiresAt)
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (transaction *documentTransaction) CreateFirstAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	if _, err := transaction.transaction.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, authBootstrapLockID(collection.ID)); err != nil {
		return err
	}
	statement := fmt.Sprintf(`SELECT count(*), count(*) FILTER (WHERE id = $1)
FROM %s WHERE deleted_at IS NULL`, quote(collectionTable(collection.ID)))
	var active, matching int
	if err := transaction.transaction.QueryRow(ctx, statement, userID).Scan(&active, &matching); err != nil {
		return err
	}
	if active != 1 || matching != 1 {
		return store.ErrAuthInitialized
	}
	_, err := transaction.transaction.Exec(ctx, `INSERT INTO ridu_auth_credentials (
  collection_id, user_id, password_hash, failed_login_attempts, locked_until, verified
) VALUES ($1, $2, $3, 0, NULL, $4)`, string(collection.ID), userID, hash, initiallyVerified)
	return translateError(err)
}

func authBootstrapLockID(collectionID schema.StableID) int64 {
	digest := sha256.Sum256([]byte("ridu:first-auth-user\x00" + string(collectionID)))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

func (backend *Store) FindAPIKey(ctx context.Context, id string, now time.Time) (store.AuthAPIKey, error) {
	var key store.AuthAPIKey
	var lastUsedAt, expiresAt *time.Time
	err := backend.pool.QueryRow(ctx, `SELECT id, token_hash, collection_id, user_id,
  name, created_at, last_used_at, expires_at
FROM ridu_auth_api_keys
WHERE id = $1 AND (expires_at IS NULL OR expires_at > $2)`, id, now).Scan(
		&key.ID, &key.TokenHash, &key.CollectionID, &key.UserID,
		&key.Name, &key.CreatedAt, &lastUsedAt, &expiresAt,
	)
	if err != nil {
		return store.AuthAPIKey{}, translateError(err)
	}
	if lastUsedAt != nil {
		key.LastUsedAt = lastUsedAt.UTC()
	}
	if expiresAt != nil {
		key.ExpiresAt = expiresAt.UTC()
	}
	return key, nil
}

func (backend *Store) TouchAPIKey(ctx context.Context, id string, now time.Time) error {
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_auth_api_keys
SET last_used_at = $2
WHERE id = $1 AND (expires_at IS NULL OR expires_at > $2)`, id, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return store.ErrNotFound
	}
	return nil
}

func (backend *Store) ListAPIKeys(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthAPIKey, error) {
	rows, err := backend.pool.Query(ctx, `SELECT id, token_hash, collection_id, user_id,
  name, created_at, last_used_at, expires_at
FROM ridu_auth_api_keys
WHERE collection_id = $1 AND user_id = $2 AND (expires_at IS NULL OR expires_at > $3)
ORDER BY created_at DESC`, string(collectionID), userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []store.AuthAPIKey
	for rows.Next() {
		var key store.AuthAPIKey
		var lastUsedAt, expiresAt *time.Time
		if err := rows.Scan(
			&key.ID, &key.TokenHash, &key.CollectionID, &key.UserID,
			&key.Name, &key.CreatedAt, &lastUsedAt, &expiresAt,
		); err != nil {
			return nil, err
		}
		if lastUsedAt != nil {
			key.LastUsedAt = lastUsedAt.UTC()
		}
		if expiresAt != nil {
			key.ExpiresAt = expiresAt.UTC()
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (backend *Store) DeleteAPIKey(ctx context.Context, collectionID schema.StableID, userID, id string) error {
	_, err := backend.pool.Exec(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = $1 AND user_id = $2 AND id = $3`, string(collectionID), userID, id)
	return err
}

// PruneExpiredAuth removes one bounded batch from each expiring durable
// credential family. The candidate ordering matches the lifecycle indexes;
// row locks let every process run the same maintenance cycle without duplicate
// work or an application-wide coordinator.
func (backend *Store) PruneExpiredAuth(ctx context.Context, limit int) (store.AuthPruneResult, error) {
	if err := store.ValidateAuthPruneBatch(limit); err != nil {
		return store.AuthPruneResult{}, err
	}
	transaction, err := backend.pool.Begin(ctx)
	if err != nil {
		return store.AuthPruneResult{}, translateError(err)
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()

	sessions, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_sessions WHERE token_hash IN (
  SELECT token_hash FROM ridu_auth_sessions
  WHERE expires_at <= now()
  ORDER BY expires_at, token_hash
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)`, limit)
	if err != nil {
		return store.AuthPruneResult{}, translateError(err)
	}
	apiKeys, err := transaction.Exec(ctx, `DELETE FROM ridu_auth_api_keys WHERE id IN (
  SELECT id FROM ridu_auth_api_keys
  WHERE expires_at IS NOT NULL AND expires_at <= now()
  ORDER BY expires_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)`, limit)
	if err != nil {
		return store.AuthPruneResult{}, translateError(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.AuthPruneResult{}, translateError(err)
	}
	return store.AuthPruneResult{Sessions: int(sessions.RowsAffected()), APIKeys: int(apiKeys.RowsAffected())}, nil
}

func (backend *Store) AllowAuthAttempt(ctx context.Context, keyHash string, now time.Time, window time.Duration, maximum int) (bool, error) {
	var allowed bool
	err := backend.pool.QueryRow(ctx, `WITH expired AS (
  DELETE FROM ridu_auth_rate_limits
  WHERE ctid IN (
    SELECT ctid FROM ridu_auth_rate_limits
    WHERE expires_at <= $2 AND key_hash <> $1
    LIMIT 100
  )
)
INSERT INTO ridu_auth_rate_limits (
  key_hash, attempts, window_started_at, expires_at
) VALUES ($1, 1, $2, $3)
ON CONFLICT (key_hash) DO UPDATE SET
  attempts = CASE
    WHEN ridu_auth_rate_limits.expires_at <= $2 THEN 1
    ELSE ridu_auth_rate_limits.attempts + 1
  END,
  window_started_at = CASE
    WHEN ridu_auth_rate_limits.expires_at <= $2 THEN $2
    ELSE ridu_auth_rate_limits.window_started_at
  END,
  expires_at = CASE
    WHEN ridu_auth_rate_limits.expires_at <= $2 THEN $3
    ELSE ridu_auth_rate_limits.expires_at
  END
RETURNING attempts <= $4`, keyHash, now, now.Add(window), maximum).Scan(&allowed)
	return allowed, err
}

var _ store.AuthStore = (*Store)(nil)
