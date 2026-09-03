package sqlite

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"fmt"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (backend *Store) SetPasswordHash(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if _, err := loadActiveAuthDocument(ctx, connection, collection, userID); err != nil {
			return err
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_auth_credentials (
  collection_id, user_id, password_hash, failed_login_attempts, locked_until, verified
) VALUES (?, ?, ?, 0, NULL, ?)
ON CONFLICT (collection_id, user_id) DO UPDATE SET
  password_hash = excluded.password_hash,
  failed_login_attempts = 0,
  locked_until = NULL`, string(collection.ID), userID, hash, boolInteger(initiallyVerified))
		if err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = ? AND user_id = ?`, string(collection.ID), userID); err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = ? AND user_id = ?`, string(collection.ID), userID); err != nil {
			return translateError(err)
		}
		return nil
	})
}

func (backend *Store) ChangePasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if _, err := loadActiveAuthDocument(ctx, connection, collection, userID); err != nil {
			return err
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_credentials
SET password_hash = ?, failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = ? AND user_id = ? AND password_hash = ?`,
			hash, string(collection.ID), userID, expectedPasswordHash)
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		if changed == 0 {
			return store.ErrConflict
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = ? AND user_id = ?`, string(collection.ID), userID); err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = ? AND user_id = ?`, string(collection.ID), userID); err != nil {
			return translateError(err)
		}
		return nil
	})
}

func (backend *Store) UpgradePasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_credentials
SET password_hash = ?
WHERE collection_id = ? AND user_id = ? AND password_hash = ?`,
			hash, string(collection.ID), userID, expectedPasswordHash)
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		if changed == 0 {
			return store.ErrConflict
		}
		return nil
	})
}

func (backend *Store) FindAuthCredential(ctx context.Context, collection schema.Collection, identity string) (store.AuthCredential, error) {
	if collection.Auth == nil {
		return store.AuthCredential{}, fmt.Errorf("auth collection %q has no identity field", collection.Slug)
	}
	found := false
	for _, field := range collection.Fields {
		if field.Name == collection.Auth.IdentityField {
			found = true
			break
		}
	}
	if !found {
		return store.AuthCredential{}, fmt.Errorf("auth collection %q has no identity field", collection.Slug)
	}
	transaction, err := backend.db.BeginTx(ctx, nil)
	if err != nil {
		return store.AuthCredential{}, translateError(err)
	}
	defer func() { _ = transaction.Rollback() }()
	user, err := loadDocumentByIdentity(ctx, transaction, collection, identity)
	if err != nil {
		return store.AuthCredential{}, err
	}
	var credential store.AuthCredential
	var lockedUntil sql.NullInt64
	var verified int
	err = transaction.QueryRowContext(ctx, `SELECT password_hash, failed_login_attempts, locked_until, verified
FROM ridu_auth_credentials
WHERE collection_id = ? AND user_id = ?`, string(collection.ID), user.ID).Scan(
		&credential.PasswordHash, &credential.FailedLoginAttempts, &lockedUntil, &verified,
	)
	if err != nil {
		return store.AuthCredential{}, translateError(err)
	}
	credential.User = user
	credential.PasswordHash = append([]byte(nil), credential.PasswordHash...)
	credential.LockedUntil = decodeOptionalTime(lockedUntil)
	credential.Verified = verified != 0
	if err := transaction.Commit(); err != nil {
		return store.AuthCredential{}, translateError(err)
	}
	return credential, nil
}

func (backend *Store) RecordFailedLogin(ctx context.Context, collectionID schema.StableID, userID string, now time.Time, maximum int, lockDuration time.Duration) (store.AuthCredential, error) {
	if err := validateSQLiteTimes("authentication timestamp", now, now.Add(lockDuration)); err != nil {
		return store.AuthCredential{}, err
	}
	var credential store.AuthCredential
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		var lockedUntil sql.NullInt64
		nowEncoded := encodeTime(now)
		err := connection.QueryRowContext(ctx, `UPDATE ridu_auth_credentials SET
  failed_login_attempts = CASE
    WHEN locked_until IS NOT NULL AND locked_until > ? THEN failed_login_attempts
    WHEN locked_until IS NOT NULL AND locked_until <= ? THEN 1
    ELSE failed_login_attempts + 1
  END,
  locked_until = CASE
    WHEN locked_until IS NOT NULL AND locked_until > ? THEN locked_until
    WHEN (CASE WHEN locked_until IS NOT NULL AND locked_until <= ? THEN 1 ELSE failed_login_attempts + 1 END) >= ? THEN ?
    ELSE NULL
  END
WHERE collection_id = ? AND user_id = ?
RETURNING failed_login_attempts, locked_until`,
			nowEncoded, nowEncoded, nowEncoded, nowEncoded, maximum, encodeTime(now.Add(lockDuration)),
			string(collectionID), userID,
		).Scan(&credential.FailedLoginAttempts, &lockedUntil)
		if err != nil {
			return translateError(err)
		}
		credential.LockedUntil = decodeOptionalTime(lockedUntil)
		return nil
	})
	if err != nil {
		return store.AuthCredential{}, err
	}
	return credential, nil
}

func (backend *Store) ResetLoginAttempts(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) (bool, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return false, err
	}
	var reset bool
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_credentials
SET failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = ? AND user_id = ?
  AND (locked_until IS NULL OR locked_until <= ?)`, string(collectionID), userID, encodeTime(now))
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		reset = changed == 1
		return nil
	})
	return reset, err
}

func (backend *Store) ForceUnlock(ctx context.Context, collectionID schema.StableID, userID string) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		return forceUnlockAuth(ctx, connection, collectionID, userID)
	})
}

func (transaction *documentTransaction) ForceUnlockAuth(ctx context.Context, collectionID schema.StableID, userID string) error {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	return forceUnlockAuth(ctx, transaction.connection, collectionID, userID)
}

func forceUnlockAuth(ctx context.Context, runner sqlRunner, collectionID schema.StableID, userID string) error {
	result, err := runner.ExecContext(ctx, `UPDATE ridu_auth_credentials
SET failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = ? AND user_id = ?`, string(collectionID), userID)
	if err != nil {
		return translateError(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return translateError(err)
	}
	if changed == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (backend *Store) CreateSession(ctx context.Context, session store.AuthSession, expectedPasswordHash []byte) error {
	if err := validateSQLiteTimes("authentication session timestamp", session.ExpiresAt, session.CreatedAt, session.LastSeenAt); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		collection := schema.Collection{ID: session.CollectionID}
		if _, err := loadActiveAuthDocument(ctx, connection, collection, session.UserID); err != nil {
			return err
		}
		var currentPasswordHash []byte
		if err := connection.QueryRowContext(ctx, `SELECT password_hash FROM ridu_auth_credentials
WHERE collection_id = ? AND user_id = ?`, string(session.CollectionID), session.UserID).Scan(&currentPasswordHash); err != nil {
			return translateError(err)
		}
		if subtle.ConstantTimeCompare(currentPasswordHash, expectedPasswordHash) != 1 {
			return store.ErrConflict
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_auth_sessions (
  id, token_hash, collection_id, user_id, expires_at, created_at,
  last_seen_at, ip_address, user_agent
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			session.ID, session.TokenHash, string(session.CollectionID), session.UserID,
			encodeTime(session.ExpiresAt), encodeTime(session.CreatedAt), encodeTime(session.LastSeenAt),
			session.IPAddress, session.UserAgent)
		return translateError(err)
	})
}

func (backend *Store) RotateSession(ctx context.Context, currentHash string, replacement store.AuthSession, now time.Time) error {
	if err := validateSQLiteTimes("authentication session timestamp", now); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		collection := schema.Collection{ID: replacement.CollectionID}
		if _, err := loadActiveAuthDocument(ctx, connection, collection, replacement.UserID); err != nil {
			return err
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_sessions
SET token_hash = ?, last_seen_at = ?
WHERE token_hash = ? AND collection_id = ? AND user_id = ? AND expires_at > ?`,
			replacement.TokenHash, encodeTime(now), currentHash, string(replacement.CollectionID),
			replacement.UserID, encodeTime(now))
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		if changed == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}

func (backend *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions WHERE token_hash = ?`, tokenHash)
		return translateError(err)
	})
}

func (backend *Store) DeleteUserSession(ctx context.Context, collectionID schema.StableID, userID, sessionID string) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = ? AND user_id = ? AND id = ?`, string(collectionID), userID, sessionID)
		return translateError(err)
	})
}

func (backend *Store) DeleteUserSessions(ctx context.Context, collectionID schema.StableID, userID string) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = ? AND user_id = ?`, string(collectionID), userID)
		return translateError(err)
	})
}

func (backend *Store) FindSession(ctx context.Context, tokenHash string, now time.Time) (store.AuthSession, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return store.AuthSession{}, err
	}
	return scanAuthSession(backend.db.QueryRowContext(ctx, `SELECT id, token_hash, collection_id, user_id,
  expires_at, created_at, last_seen_at, ip_address, user_agent
FROM ridu_auth_sessions
WHERE token_hash = ? AND expires_at > ?`, tokenHash, encodeTime(now)))
}

func (backend *Store) ListSessions(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthSession, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return nil, err
	}
	rows, err := backend.db.QueryContext(ctx, `SELECT id, token_hash, collection_id, user_id,
  expires_at, created_at, last_seen_at, ip_address, user_agent
FROM ridu_auth_sessions
WHERE collection_id = ? AND user_id = ? AND expires_at > ?
ORDER BY last_seen_at DESC, created_at DESC`, string(collectionID), userID, encodeTime(now))
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var sessions []store.AuthSession
	for rows.Next() {
		session, err := scanAuthSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, translateError(rows.Err())
}

func (backend *Store) CreateAuthToken(ctx context.Context, token store.AuthToken) error {
	if err := validateSQLiteTimes("authentication token timestamp", token.ExpiresAt, token.CreatedAt); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		collection := schema.Collection{ID: token.CollectionID}
		if _, err := loadActiveAuthDocument(ctx, connection, collection, token.UserID); err != nil {
			return err
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_tokens
WHERE collection_id = ? AND user_id = ? AND purpose = ?`,
			string(token.CollectionID), token.UserID, string(token.Purpose)); err != nil {
			return translateError(err)
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_auth_tokens (
  token_hash, collection_id, user_id, purpose, expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?)`, token.TokenHash, string(token.CollectionID), token.UserID,
			string(token.Purpose), encodeTime(token.ExpiresAt), encodeTime(token.CreatedAt))
		return translateError(err)
	})
}

func (backend *Store) ResetPasswordWithToken(ctx context.Context, collectionID schema.StableID, tokenHash string, hash []byte, now time.Time) (string, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return "", err
	}
	var userID string
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if err := connection.QueryRowContext(ctx, `SELECT user_id FROM ridu_auth_tokens
WHERE token_hash = ? AND collection_id = ? AND purpose = ? AND expires_at > ?`,
			tokenHash, string(collectionID), string(store.AuthTokenPasswordReset), encodeTime(now)).Scan(&userID); err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_tokens
WHERE token_hash = ?`, tokenHash); err != nil {
			return translateError(err)
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_credentials
SET password_hash = ?, failed_login_attempts = 0, locked_until = NULL
WHERE collection_id = ? AND user_id = ?`, hash, string(collectionID), userID)
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		if changed != 1 {
			return store.ErrNotFound
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions
WHERE collection_id = ? AND user_id = ?`, string(collectionID), userID); err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = ? AND user_id = ?`, string(collectionID), userID); err != nil {
			return translateError(err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (backend *Store) VerifyEmailWithToken(ctx context.Context, collectionID schema.StableID, tokenHash string, now time.Time) (string, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return "", err
	}
	var userID string
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if err := connection.QueryRowContext(ctx, `SELECT user_id FROM ridu_auth_tokens
WHERE token_hash = ? AND collection_id = ? AND purpose = ? AND expires_at > ?`,
			tokenHash, string(collectionID), string(store.AuthTokenVerifyEmail), encodeTime(now)).Scan(&userID); err != nil {
			return translateError(err)
		}
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_tokens
WHERE token_hash = ?`, tokenHash); err != nil {
			return translateError(err)
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_credentials
SET verified = 1
WHERE collection_id = ? AND user_id = ?`, string(collectionID), userID)
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		if changed != 1 {
			return store.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (backend *Store) CreateAPIKey(ctx context.Context, key store.AuthAPIKey, sessionTokenHash string, now time.Time) error {
	timestamps := []time.Time{now, key.CreatedAt}
	if !key.ExpiresAt.IsZero() {
		timestamps = append(timestamps, key.ExpiresAt)
	}
	if err := validateSQLiteTimes("authentication API key timestamp", timestamps...); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		collection := schema.Collection{ID: key.CollectionID}
		if _, err := loadActiveAuthDocument(ctx, connection, collection, key.UserID); err != nil {
			return err
		}
		var marker int
		if err := connection.QueryRowContext(ctx, `SELECT 1 FROM ridu_auth_credentials
WHERE collection_id = ? AND user_id = ?`, string(key.CollectionID), key.UserID).Scan(&marker); err != nil {
			return translateError(err)
		}
		if err := connection.QueryRowContext(ctx, `SELECT 1 FROM ridu_auth_sessions
WHERE token_hash = ? AND collection_id = ? AND user_id = ? AND expires_at > ?`,
			sessionTokenHash, string(key.CollectionID), key.UserID, encodeTime(now)).Scan(&marker); err != nil {
			return translateError(err)
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_auth_api_keys (
  id, token_hash, collection_id, user_id, name, created_at, last_used_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`, key.ID, key.TokenHash, string(key.CollectionID),
			key.UserID, key.Name, encodeTime(key.CreatedAt), encodeOptionalTime(key.ExpiresAt))
		return translateError(err)
	})
}

func (backend *Store) FindAPIKey(ctx context.Context, id string, now time.Time) (store.AuthAPIKey, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return store.AuthAPIKey{}, err
	}
	return scanAuthAPIKey(backend.db.QueryRowContext(ctx, `SELECT id, token_hash, collection_id, user_id,
  name, created_at, last_used_at, expires_at
FROM ridu_auth_api_keys
WHERE id = ? AND (expires_at IS NULL OR expires_at > ?)`, id, encodeTime(now)))
}

func (backend *Store) TouchAPIKey(ctx context.Context, id string, now time.Time) error {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		result, err := connection.ExecContext(ctx, `UPDATE ridu_auth_api_keys
SET last_used_at = ?
WHERE id = ? AND (expires_at IS NULL OR expires_at > ?)`, encodeTime(now), id, encodeTime(now))
		if err != nil {
			return translateError(err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		if changed != 1 {
			return store.ErrNotFound
		}
		return nil
	})
}

func (backend *Store) ListAPIKeys(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthAPIKey, error) {
	if err := validateSQLiteTimes("authentication timestamp", now); err != nil {
		return nil, err
	}
	rows, err := backend.db.QueryContext(ctx, `SELECT id, token_hash, collection_id, user_id,
  name, created_at, last_used_at, expires_at
FROM ridu_auth_api_keys
WHERE collection_id = ? AND user_id = ? AND (expires_at IS NULL OR expires_at > ?)
ORDER BY created_at DESC`, string(collectionID), userID, encodeTime(now))
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var keys []store.AuthAPIKey
	for rows.Next() {
		key, err := scanAuthAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, translateError(rows.Err())
}

func (backend *Store) DeleteAPIKey(ctx context.Context, collectionID schema.StableID, userID, id string) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_api_keys
WHERE collection_id = ? AND user_id = ? AND id = ?`, string(collectionID), userID, id)
		return translateError(err)
	})
}

// PruneExpiredAuth removes at most limit records from each expiring auth
// family. BEGIN IMMEDIATE serializes candidates across processes so two
// maintenance workers cannot count or delete the same record.
func (backend *Store) PruneExpiredAuth(ctx context.Context, limit int) (store.AuthPruneResult, error) {
	if err := store.ValidateAuthPruneBatch(limit); err != nil {
		return store.AuthPruneResult{}, err
	}
	nowTime := backend.now().UTC()
	if err := validateSQLiteTimes("authentication timestamp", nowTime); err != nil {
		return store.AuthPruneResult{}, err
	}
	var result store.AuthPruneResult
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		now := encodeTime(nowTime)
		sessions, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_sessions
WHERE token_hash IN (
  SELECT token_hash FROM ridu_auth_sessions
  WHERE expires_at <= ?
  ORDER BY expires_at, token_hash
  LIMIT ?
)`, now, limit)
		if err != nil {
			return translateError(err)
		}
		apiKeys, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_api_keys
WHERE id IN (
  SELECT id FROM ridu_auth_api_keys
  WHERE expires_at IS NOT NULL AND expires_at <= ?
  ORDER BY expires_at, id
  LIMIT ?
)`, now, limit)
		if err != nil {
			return translateError(err)
		}
		sessionCount, err := sessions.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		apiKeyCount, err := apiKeys.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		result = store.AuthPruneResult{Sessions: int(sessionCount), APIKeys: int(apiKeyCount)}
		return nil
	})
	if err != nil {
		return store.AuthPruneResult{}, err
	}
	return result, nil
}

func (backend *Store) AllowAuthAttempt(ctx context.Context, keyHash string, now time.Time, window time.Duration, maximum int) (bool, error) {
	if err := validateSQLiteTimes("authentication rate-limit timestamp", now, now.Add(window)); err != nil {
		return false, err
	}
	var allowed bool
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		nowEncoded := encodeTime(now)
		if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_auth_rate_limits
WHERE key_hash IN (
  SELECT key_hash FROM ridu_auth_rate_limits
  WHERE expires_at <= ? AND key_hash <> ?
  ORDER BY expires_at, key_hash
  LIMIT 100
)`, nowEncoded, keyHash); err != nil {
			return translateError(err)
		}
		var attempts int
		err := connection.QueryRowContext(ctx, `INSERT INTO ridu_auth_rate_limits (
  key_hash, attempts, expires_at
) VALUES (?, 1, ?)
ON CONFLICT (key_hash) DO UPDATE SET
  attempts = CASE
    WHEN ridu_auth_rate_limits.expires_at <= ? THEN 1
    ELSE ridu_auth_rate_limits.attempts + 1
  END,
  expires_at = CASE
    WHEN ridu_auth_rate_limits.expires_at <= ? THEN excluded.expires_at
    ELSE ridu_auth_rate_limits.expires_at
  END
RETURNING attempts`, keyHash, encodeTime(now.Add(window)), nowEncoded, nowEncoded).Scan(&attempts)
		if err != nil {
			return translateError(err)
		}
		allowed = attempts <= maximum
		return nil
	})
	return allowed, err
}

func (transaction *documentTransaction) CreateAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	return createAuthCredential(ctx, transaction.connection, collection, userID, hash, initiallyVerified)
}

func createAuthCredential(ctx context.Context, runner sqlRunner, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	if _, err := loadActiveAuthDocument(ctx, runner, collection, userID); err != nil {
		return err
	}
	_, err := runner.ExecContext(ctx, `INSERT INTO ridu_auth_credentials (
  collection_id, user_id, password_hash, failed_login_attempts, locked_until, verified
) VALUES (?, ?, ?, 0, NULL, ?)`, string(collection.ID), userID, hash, boolInteger(initiallyVerified))
	return translateError(err)
}

func (transaction *documentTransaction) CreateFirstAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	var active, matching int
	err = transaction.connection.QueryRowContext(ctx, `SELECT count(*),
  coalesce(sum(CASE WHEN id = ? THEN 1 ELSE 0 END), 0)
FROM ridu_documents
WHERE collection_id = ? AND deleted_at IS NULL`, userID, string(collection.ID)).Scan(&active, &matching)
	if err != nil {
		return translateError(err)
	}
	if active != 1 || matching != 1 {
		return store.ErrAuthInitialized
	}
	return createAuthCredential(ctx, transaction.connection, collection, userID, hash, initiallyVerified)
}

type authScanner interface {
	Scan(...any) error
}

func loadActiveAuthDocument(ctx context.Context, runner sqlRunner, collection schema.Collection, id string) (store.Document, error) {
	document, err := loadDocument(ctx, runner, collection, id)
	if err != nil {
		return store.Document{}, err
	}
	if document.DeletedAt != nil {
		return store.Document{}, store.ErrNotFound
	}
	return document, nil
}

func scanAuthSession(scanner authScanner) (store.AuthSession, error) {
	var session store.AuthSession
	var collectionID string
	var expiresAt, createdAt, lastSeenAt int64
	if err := scanner.Scan(
		&session.ID, &session.TokenHash, &collectionID, &session.UserID,
		&expiresAt, &createdAt, &lastSeenAt, &session.IPAddress, &session.UserAgent,
	); err != nil {
		return store.AuthSession{}, translateError(err)
	}
	session.CollectionID = schema.StableID(collectionID)
	session.ExpiresAt = decodeTime(expiresAt)
	session.CreatedAt = decodeTime(createdAt)
	session.LastSeenAt = decodeTime(lastSeenAt)
	return session, nil
}

func scanAuthAPIKey(scanner authScanner) (store.AuthAPIKey, error) {
	var key store.AuthAPIKey
	var collectionID string
	var createdAt int64
	var lastUsedAt, expiresAt sql.NullInt64
	if err := scanner.Scan(
		&key.ID, &key.TokenHash, &collectionID, &key.UserID,
		&key.Name, &createdAt, &lastUsedAt, &expiresAt,
	); err != nil {
		return store.AuthAPIKey{}, translateError(err)
	}
	key.CollectionID = schema.StableID(collectionID)
	key.CreatedAt = decodeTime(createdAt)
	key.LastUsedAt = decodeOptionalTime(lastUsedAt)
	key.ExpiresAt = decodeOptionalTime(expiresAt)
	return key, nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

var _ store.AuthStore = (*Store)(nil)
var _ store.AuthMaintenanceStore = (*Store)(nil)
var _ store.AuthUnlockStore = (*Store)(nil)
var _ store.AuthTransaction = (*documentTransaction)(nil)
var _ store.AuthBootstrapTransaction = (*documentTransaction)(nil)
var _ store.AuthUnlockTransaction = (*documentTransaction)(nil)
