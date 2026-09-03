package sqlite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLiteAuthCredentialSessionTokenAndAPIKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteAuthStore(t)
	collection := sqliteAuthCollection()
	now := time.Date(2026, time.August, 29, 12, 0, 0, 123, time.UTC)
	insertSQLiteAuthDocument(t, backend, collection, "user-1", store.Values{
		"email": store.String("User@example.com"),
	}, nil)

	firstHash := []byte("first-hash")
	if err := backend.SetPasswordHash(ctx, collection, "user-1", firstHash, false); err != nil {
		t.Fatal(err)
	}
	credential, err := backend.FindAuthCredential(ctx, collection, "USER@EXAMPLE.COM")
	if err != nil {
		t.Fatal(err)
	}
	if credential.User.ID != "user-1" || !bytes.Equal(credential.PasswordHash, firstHash) || credential.Verified {
		t.Fatalf("FindAuthCredential() = %#v", credential)
	}

	failed, err := backend.RecordFailedLogin(ctx, collection.ID, "user-1", now, 2, time.Minute)
	if err != nil || failed.FailedLoginAttempts != 1 || !failed.LockedUntil.IsZero() {
		t.Fatalf("first RecordFailedLogin() = %#v, %v", failed, err)
	}
	locked, err := backend.RecordFailedLogin(ctx, collection.ID, "user-1", now, 2, time.Minute)
	if err != nil || locked.FailedLoginAttempts != 2 || !locked.LockedUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("second RecordFailedLogin() = %#v, %v", locked, err)
	}
	stillLocked, err := backend.RecordFailedLogin(ctx, collection.ID, "user-1", now.Add(time.Second), 2, time.Minute)
	if err != nil || stillLocked.FailedLoginAttempts != 2 || !stillLocked.LockedUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("locked RecordFailedLogin() = %#v, %v", stillLocked, err)
	}
	if reset, err := backend.ResetLoginAttempts(ctx, collection.ID, "user-1", now.Add(time.Second)); err != nil || reset {
		t.Fatalf("ResetLoginAttempts() while locked = %v, %v", reset, err)
	}
	if err := backend.ForceUnlock(ctx, collection.ID, "user-1"); err != nil {
		t.Fatal(err)
	}

	secondHash := []byte("second-hash")
	if err := backend.UpgradePasswordHash(ctx, collection, "user-1", []byte("wrong"), secondHash); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("UpgradePasswordHash() stale error = %v", err)
	}
	if err := backend.UpgradePasswordHash(ctx, collection, "user-1", firstHash, secondHash); err != nil {
		t.Fatal(err)
	}
	session := store.AuthSession{
		ID: "session-id", TokenHash: "session-hash-1", CollectionID: collection.ID, UserID: "user-1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
		IPAddress: "127.0.0.1", UserAgent: "test",
	}
	if err := backend.CreateSession(ctx, session, firstHash); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("CreateSession() stale password error = %v", err)
	}
	if err := backend.CreateSession(ctx, session, secondHash); err != nil {
		t.Fatal(err)
	}
	replacement := store.AuthSession{TokenHash: "session-hash-2", CollectionID: collection.ID, UserID: "user-1"}
	if err := backend.RotateSession(ctx, session.TokenHash, replacement, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindSession(ctx, session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("FindSession() with rotated hash error = %v", err)
	}
	rotated, err := backend.FindSession(ctx, replacement.TokenHash, now)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ID != session.ID || !rotated.ExpiresAt.Equal(session.ExpiresAt) || rotated.IPAddress != session.IPAddress || !rotated.LastSeenAt.Equal(now.Add(time.Second)) {
		t.Fatalf("rotated session = %#v", rotated)
	}

	key := store.AuthAPIKey{
		ID: "key-id", TokenHash: "key-hash", CollectionID: collection.ID, UserID: "user-1",
		Name: "automation", CreatedAt: now, ExpiresAt: now.Add(2 * time.Hour),
	}
	if err := backend.CreateAPIKey(ctx, key, replacement.TokenHash, now); err != nil {
		t.Fatal(err)
	}
	if err := backend.TouchAPIKey(ctx, key.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	storedKey, err := backend.FindAPIKey(ctx, key.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if storedKey.TokenHash != key.TokenHash || !storedKey.LastUsedAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("FindAPIKey() = %#v", storedKey)
	}

	thirdHash := []byte("third-hash")
	if err := backend.ChangePasswordHash(ctx, collection, "user-1", secondHash, thirdHash); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindSession(ctx, replacement.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session survived password change: %v", err)
	}
	if _, err := backend.FindAPIKey(ctx, key.ID, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("API key survived password change: %v", err)
	}

	oldReset := store.AuthToken{
		TokenHash: "reset-old", Purpose: store.AuthTokenPasswordReset, CollectionID: collection.ID,
		UserID: "user-1", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	newReset := oldReset
	newReset.TokenHash = "reset-new"
	if err := backend.CreateAuthToken(ctx, oldReset); err != nil {
		t.Fatal(err)
	}
	if err := backend.CreateAuthToken(ctx, newReset); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ResetPasswordWithToken(ctx, collection.ID, oldReset.TokenHash, []byte("unused"), now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced reset token error = %v", err)
	}
	fourthHash := []byte("fourth-hash")
	userID, err := backend.ResetPasswordWithToken(ctx, collection.ID, newReset.TokenHash, fourthHash, now)
	if err != nil || userID != "user-1" {
		t.Fatalf("ResetPasswordWithToken() = %q, %v", userID, err)
	}
	if _, err := backend.ResetPasswordWithToken(ctx, collection.ID, newReset.TokenHash, fourthHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed reset token error = %v", err)
	}

	verification := store.AuthToken{
		TokenHash: "verify", Purpose: store.AuthTokenVerifyEmail, CollectionID: collection.ID,
		UserID: "user-1", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := backend.CreateAuthToken(ctx, verification); err != nil {
		t.Fatal(err)
	}
	if userID, err := backend.VerifyEmailWithToken(ctx, collection.ID, verification.TokenHash, now); err != nil || userID != "user-1" {
		t.Fatalf("VerifyEmailWithToken() = %q, %v", userID, err)
	}
	credential, err = backend.FindAuthCredential(ctx, collection, "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(credential.PasswordHash, fourthHash) || !credential.Verified || credential.FailedLoginAttempts != 0 || !credential.LockedUntil.IsZero() {
		t.Fatalf("credential after recovery = %#v", credential)
	}
}

func TestSQLiteAuthIdentityCanonicalizationAtDirectStoreBoundary(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteAuthStore(t)
	collection := sqliteAuthCollection()
	createValues := store.Values{"email": store.String(" \tUser@Example.COM\u00a0")}

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "canonical-user", Values: createValues})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := created.Values["email"].StringValue(); got != "user@example.com" {
		t.Fatalf("created identity = %q", got)
	}
	if got, _ := createValues["email"].StringValue(); got != " \tUser@Example.COM\u00a0" {
		t.Fatalf("Create mutated caller value to %q", got)
	}
	updateValues := store.Values{"email": store.String(" Next@Example.COM ")}
	updated, err := transaction.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: created.ID}, Values: updateValues,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := updated.Values["email"].StringValue(); got != "next@example.com" {
		t.Fatalf("updated identity = %q", got)
	}
	if got, _ := updateValues["email"].StringValue(); got != " Next@Example.COM " {
		t.Fatalf("Update mutated caller value to %q", got)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	assertSQLiteAuthIdentityPair(t, backend, collection, "kelvin", "\u212a@example.test", "ascii-k", "K@example.test", true)
	assertSQLiteAuthIdentityPair(t, backend, collection, "long-s", "\u017f@example.test", "ascii-s", "S@example.test", false)
	assertSQLiteAuthIdentityPair(t, backend, collection, "sigma", "\u03a3@example.test", "final-sigma", "\u03c2@example.test", false)
	assertSQLiteAuthIdentityPair(t, backend, collection, "dotted-i", "\u0130@example.test", "ascii-i", "I@example.test", true)
	assertSQLiteAuthIdentityPair(t, backend, collection, "dotless-i", "\u0131@dotless.test", "ascii-i-distinct", "i@dotless.test", false)
}

func assertSQLiteAuthIdentityPair(t *testing.T, backend *Store, collection schema.Collection, firstID, firstIdentity, secondID, secondIdentity string, conflict bool) {
	t.Helper()
	ctx := context.Background()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: firstID, Values: store.Values{"email": store.String(firstIdentity)}}); err != nil {
		t.Fatal(err)
	}
	_, err = transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: secondID, Values: store.Values{"email": store.String(secondIdentity)}})
	if conflict {
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second identity error = %v, want conflict", err)
		}
		if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("distinct second identity: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteAuthBootstrapIsTransactionOwned(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteAuthStore(t)
	collection := sqliteAuthCollection()

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection, ID: "first", Values: store.Values{"email": store.String("first@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	bootstrap := transaction.(store.AuthBootstrapTransaction)
	if err := bootstrap.CreateFirstAuthCredential(ctx, collection, "first", []byte("hash"), true); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{
		Collection: collection, ID: "second", Values: store.Values{"email": store.String("second@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	bootstrap = transaction.(store.AuthBootstrapTransaction)
	if err := bootstrap.CreateFirstAuthCredential(ctx, collection, "second", []byte("hash"), true); !errors.Is(err, store.ErrAuthInitialized) {
		t.Fatalf("second CreateFirstAuthCredential() error = %v", err)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDocument(ctx, backend.db, collection, "second"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled-back bootstrap document error = %v", err)
	}

	if _, err := backend.RecordFailedLogin(ctx, collection.ID, "first", time.Now(), 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.(store.AuthUnlockTransaction).ForceUnlockAuth(ctx, collection.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	credential, err := backend.FindAuthCredential(ctx, collection, "first@example.com")
	if err != nil || credential.FailedLoginAttempts != 0 || !credential.LockedUntil.IsZero() {
		t.Fatalf("credential after transaction unlock = %#v, %v", credential, err)
	}
}

func TestSQLiteAuthPruningAndRateLimitBoundaries(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteAuthStore(t)
	collection := sqliteAuthCollection()
	now := time.Date(2026, time.August, 29, 14, 0, 0, 789, time.UTC)
	backend.now = func() time.Time { return now }
	insertSQLiteAuthDocument(t, backend, collection, "user-1", store.Values{"email": store.String("user@example.com")}, nil)
	if err := backend.SetPasswordHash(ctx, collection, "user-1", []byte("hash"), true); err != nil {
		t.Fatal(err)
	}

	for _, session := range []store.AuthSession{
		{ID: "expired-1", TokenHash: "expired-session-1", CollectionID: collection.ID, UserID: "user-1", ExpiresAt: now.Add(-time.Second), CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)},
		{ID: "expired-2", TokenHash: "expired-session-2", CollectionID: collection.ID, UserID: "user-1", ExpiresAt: now, CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)},
		{ID: "active", TokenHash: "active-session", CollectionID: collection.ID, UserID: "user-1", ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now},
	} {
		if err := backend.CreateSession(ctx, session, []byte("hash")); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []store.AuthAPIKey{
		{ID: "expired-key-1", TokenHash: "expired-key-hash-1", CollectionID: collection.ID, UserID: "user-1", Name: "old", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second)},
		{ID: "expired-key-2", TokenHash: "expired-key-hash-2", CollectionID: collection.ID, UserID: "user-1", Name: "boundary", CreatedAt: now.Add(-time.Hour), ExpiresAt: now},
		{ID: "active-key", TokenHash: "active-key-hash", CollectionID: collection.ID, UserID: "user-1", Name: "active", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		{ID: "permanent-key", TokenHash: "permanent-key-hash", CollectionID: collection.ID, UserID: "user-1", Name: "permanent", CreatedAt: now},
	} {
		if err := backend.CreateAPIKey(ctx, key, "active-session", now); err != nil {
			t.Fatal(err)
		}
	}

	first, err := backend.PruneExpiredAuth(ctx, 1)
	if err != nil || first != (store.AuthPruneResult{Sessions: 1, APIKeys: 1}) {
		t.Fatalf("first PruneExpiredAuth() = %#v, %v", first, err)
	}
	second, err := backend.PruneExpiredAuth(ctx, 1)
	if err != nil || second != (store.AuthPruneResult{Sessions: 1, APIKeys: 1}) {
		t.Fatalf("second PruneExpiredAuth() = %#v, %v", second, err)
	}
	third, err := backend.PruneExpiredAuth(ctx, 1)
	if err != nil || third.Total() != 0 {
		t.Fatalf("third PruneExpiredAuth() = %#v, %v", third, err)
	}
	if _, err := backend.FindSession(ctx, "active-session", now); err != nil {
		t.Fatal(err)
	}
	if keys, err := backend.ListAPIKeys(ctx, collection.ID, "user-1", now); err != nil || len(keys) != 2 {
		t.Fatalf("ListAPIKeys() after prune = %#v, %v", keys, err)
	}
	if _, err := backend.PruneExpiredAuth(ctx, 0); err == nil {
		t.Fatal("PruneExpiredAuth() accepted a zero batch")
	}

	for index, want := range []bool{true, true, false} {
		allowed, err := backend.AllowAuthAttempt(ctx, "login", now, time.Minute, 2)
		if err != nil || allowed != want {
			t.Fatalf("AllowAuthAttempt() call %d = %v, %v", index+1, allowed, err)
		}
	}
	if allowed, err := backend.AllowAuthAttempt(ctx, "login", now.Add(time.Minute), time.Minute, 2); err != nil || !allowed {
		t.Fatalf("AllowAuthAttempt() at expiry = %v, %v", allowed, err)
	}
}

func TestSQLiteAuthMaintenanceUsesExpiryIndexes(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteAuthStore(t)
	for _, test := range []struct {
		name, statement, index string
		arguments              []any
	}{
		{
			name:      "sessions",
			statement: `SELECT token_hash FROM ridu_auth_sessions WHERE expires_at <= ? ORDER BY expires_at, token_hash LIMIT ?`,
			index:     "ridu_auth_sessions_expiry_idx",
			arguments: []any{encodeTime(time.Now().UTC()), 100},
		},
		{
			name:      "API keys",
			statement: `SELECT id FROM ridu_auth_api_keys WHERE expires_at IS NOT NULL AND expires_at <= ? ORDER BY expires_at, id LIMIT ?`,
			index:     "ridu_auth_api_keys_expiry_idx",
			arguments: []any{encodeTime(time.Now().UTC()), 100},
		},
		{
			name:      "rate limits",
			statement: `SELECT key_hash FROM ridu_auth_rate_limits WHERE expires_at <= ? AND key_hash <> ? ORDER BY expires_at, key_hash LIMIT 100`,
			index:     "ridu_auth_rate_limits_expiry_idx",
			arguments: []any{encodeTime(time.Now().UTC()), "current-key"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := sqliteExplainPlan(t, ctx, backend.db, test.statement, test.arguments...)
			if !strings.Contains(plan, test.index) || strings.Contains(plan, "USE TEMP B-TREE") {
				t.Fatalf("expiry maintenance did not use %s:\n%s", test.index, plan)
			}
		})
	}
}

func TestSQLiteAuthCrossStoreBootstrapAndFailedAttemptsAreAtomic(t *testing.T) {
	ctx := context.Background()
	collection := sqliteAuthCollection()
	first, second := newSQLiteFileAuthStores(t)

	winning, err := first.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := winning.Create(ctx, store.CreateRequest{
		Collection: collection, ID: "first", Values: store.Values{"email": store.String("first@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := winning.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(ctx, collection, "first", []byte("hash"), true); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	contenderResult := make(chan error, 1)
	go func() {
		close(started)
		contender, beginErr := second.Begin(ctx)
		if beginErr != nil {
			contenderResult <- fmt.Errorf("begin contender: %w", beginErr)
			return
		}
		if _, createErr := contender.Create(ctx, store.CreateRequest{
			Collection: collection, ID: "second", Values: store.Values{"email": store.String("second@example.com")},
		}); createErr != nil {
			_ = contender.Rollback(ctx)
			contenderResult <- fmt.Errorf("create contender: %w", createErr)
			return
		}
		bootstrapErr := contender.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(ctx, collection, "second", []byte("hash"), true)
		rollbackErr := contender.Rollback(ctx)
		if !errors.Is(bootstrapErr, store.ErrAuthInitialized) {
			contenderResult <- fmt.Errorf("bootstrap contender: %w", bootstrapErr)
			return
		}
		contenderResult <- rollbackErr
	}()
	<-started
	if err := winning.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-contenderResult; err != nil {
		t.Fatal(err)
	}

	verification, err := second.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := verification.List(ctx, store.Request{Collection: collection, Page: 1, Limit: 10})
	if err != nil {
		_ = verification.Rollback(ctx)
		t.Fatal(err)
	}
	if err := verification.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != "first" {
		t.Fatalf("documents after bootstrap race = %#v", page)
	}

	const concurrentAttempts = 12
	now := time.Date(2026, time.August, 29, 16, 0, 0, 0, time.UTC)
	errorsByAttempt := make(chan error, concurrentAttempts)
	var attempts sync.WaitGroup
	for index := 0; index < concurrentAttempts; index++ {
		backend := first
		if index%2 != 0 {
			backend = second
		}
		attempts.Add(1)
		go func() {
			defer attempts.Done()
			_, err := backend.RecordFailedLogin(ctx, collection.ID, "first", now, 100, time.Minute)
			errorsByAttempt <- err
		}()
	}
	attempts.Wait()
	close(errorsByAttempt)
	for err := range errorsByAttempt {
		if err != nil {
			t.Fatal(err)
		}
	}
	next, err := first.RecordFailedLogin(ctx, collection.ID, "first", now, 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.FailedLoginAttempts != concurrentAttempts+1 || !next.LockedUntil.IsZero() {
		t.Fatalf("failed-attempt state after cross-store increments = %#v", next)
	}
}

func TestSQLiteAuthCrossStoreRevocationAndExpiryFences(t *testing.T) {
	ctx := context.Background()
	collection := sqliteAuthCollection()
	first, second := newSQLiteFileAuthStores(t)
	now := time.Date(2026, time.August, 29, 17, 0, 0, 0, time.UTC)
	insertSQLiteAuthDocument(t, first, collection, "user-1", store.Values{"email": store.String("user@example.com")}, nil)
	passwordHash := []byte("password-hash")
	if err := first.SetPasswordHash(ctx, collection, "user-1", passwordHash, true); err != nil {
		t.Fatal(err)
	}

	authorizing := store.AuthSession{
		ID: "authorizing-session", TokenHash: "authorizing-session-hash", CollectionID: collection.ID, UserID: "user-1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
	}
	if err := first.CreateSession(ctx, authorizing, passwordHash); err != nil {
		t.Fatal(err)
	}

	revocation, err := first.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := revocation.DeleteDocumentState(ctx, store.DocumentReference{CollectionID: collection.ID, DocumentID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	creationStarted := make(chan struct{})
	creationResult := make(chan error, 1)
	go func() {
		close(creationStarted)
		creationResult <- second.CreateAPIKey(ctx, store.AuthAPIKey{
			ID: "late-key", TokenHash: "late-key-hash", CollectionID: collection.ID,
			UserID: "user-1", Name: "late", CreatedAt: now,
		}, authorizing.TokenHash, now)
	}()
	<-creationStarted
	if err := revocation.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-creationResult; !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("API key created after cross-store session revocation: %v", err)
	}
	if _, err := first.FindAPIKey(ctx, "late-key", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late API key survived revocation: %v", err)
	}

	if err := second.SetPasswordHash(ctx, collection, "user-1", passwordHash, true); err != nil {
		t.Fatal(err)
	}
	expiredSession := store.AuthSession{
		ID: "expired-session", TokenHash: "expired-session-hash", CollectionID: collection.ID, UserID: "user-1",
		ExpiresAt: now, CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour),
	}
	if err := first.CreateSession(ctx, expiredSession, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := second.FindSession(ctx, expiredSession.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session remained active at exact expiry: %v", err)
	}
	if err := second.CreateAPIKey(ctx, store.AuthAPIKey{
		ID: "expired-session-key", TokenHash: "expired-session-key-hash", CollectionID: collection.ID,
		UserID: "user-1", Name: "expired", CreatedAt: now,
	}, expiredSession.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired session authorized an API key: %v", err)
	}

	activeSession := authorizing
	activeSession.ID = "active-session"
	activeSession.TokenHash = "active-session-hash"
	if err := second.CreateSession(ctx, activeSession, passwordHash); err != nil {
		t.Fatal(err)
	}
	expiredKey := store.AuthAPIKey{
		ID: "expired-key", TokenHash: "expired-key-hash", CollectionID: collection.ID,
		UserID: "user-1", Name: "boundary", CreatedAt: now, ExpiresAt: now,
	}
	if err := first.CreateAPIKey(ctx, expiredKey, activeSession.TokenHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := second.FindAPIKey(ctx, expiredKey.ID, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("API key remained active at exact expiry: %v", err)
	}
	if err := second.TouchAPIKey(ctx, expiredKey.ID, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired API key was touched: %v", err)
	}

	activeKey := store.AuthAPIKey{
		ID: "active-key", TokenHash: "active-key-hash", CollectionID: collection.ID,
		UserID: "user-1", Name: "active", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := first.CreateAPIKey(ctx, activeKey, activeSession.TokenHash, now); err != nil {
		t.Fatal(err)
	}
	reset := store.AuthToken{
		TokenHash: "reset-token", Purpose: store.AuthTokenPasswordReset, CollectionID: collection.ID,
		UserID: "user-1", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := second.CreateAuthToken(ctx, reset); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ResetPasswordWithToken(ctx, collection.ID, reset.TokenHash, []byte("replacement-hash"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := second.FindSession(ctx, activeSession.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session survived token password reset: %v", err)
	}
	if _, err := second.FindAPIKey(ctx, activeKey.ID, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("API key survived token password reset: %v", err)
	}
}

func newSQLiteAuthStore(t *testing.T) *Store {
	t.Helper()
	backend, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "auth"}, Plugins: []schema.Plugin{},
	})
	if err := backend.Migrate(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	return backend
}

func newSQLiteFileAuthStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.sqlite")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "auth"}, Plugins: []schema.Plugin{},
	})
	if err := first.Migrate(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	return first, second
}

func sqliteAuthCollection() schema.Collection {
	return schema.Collection{
		ID: "users", Slug: "users", Capabilities: schema.Capabilities{Auth: true},
		Auth: &schema.AuthSettings{IdentityField: "email"},
		Fields: []schema.Field{{
			ID: "email", Name: "email", Type: schema.FieldTypeEmail, Category: schema.FieldCategoryScalar,
			Required: true, Unique: true,
		}},
	}
}

func insertSQLiteAuthDocument(t *testing.T, backend *Store, collection schema.Collection, id string, values store.Values, deletedAt *time.Time) {
	t.Helper()
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(context.Background(), store.CreateRequest{
		Collection: collection, ID: id, Values: values,
		CreatedAt: time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		_ = transaction.Rollback(context.Background())
		t.Fatal(err)
	}
	if deletedAt != nil {
		originalNow := backend.now
		backend.now = func() time.Time { return deletedAt.UTC() }
		if _, err := transaction.Trash(context.Background(), store.Request{Collection: collection, ID: id}); err != nil {
			backend.now = originalNow
			_ = transaction.Rollback(context.Background())
			t.Fatal(err)
		}
		backend.now = originalNow
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}
