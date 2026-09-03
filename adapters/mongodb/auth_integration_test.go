package mongodb

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBAuthCredentialSessionTokenAndAPIKeyLifecycle(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoAuthTestCollection(t, "auth-users")
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	callerValues := store.Values{"email": store.String(" \tUser@Example.COM\u00a0")}
	transaction := mongoBegin(t, backend, false)
	user, err := transaction.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "user-1", Values: callerValues})
	if err != nil {
		t.Fatal(err)
	}
	if identity, _ := user.Values["email"].StringValue(); identity != "user@example.com" {
		t.Fatalf("canonical auth identity = %q", identity)
	}
	if identity, _ := callerValues["email"].StringValue(); identity != " \tUser@Example.COM\u00a0" {
		t.Fatalf("MongoDB Create mutated caller identity to %q", identity)
	}
	firstHash := []byte("first-password-hash")
	if err := transaction.(store.AuthTransaction).CreateAuthCredential(t.Context(), collection, user.ID, firstHash, false); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, transaction)

	credential, err := backend.FindAuthCredential(t.Context(), collection, "USER@EXAMPLE.COM")
	if err != nil || credential.User.ID != user.ID || !bytes.Equal(credential.PasswordHash, firstHash) || credential.Verified {
		t.Fatalf("FindAuthCredential() = %#v, %v", credential, err)
	}

	updateValues := store.Values{"email": store.String(" Next@Example.COM ")}
	update := mongoBegin(t, backend, false)
	updated, err := update.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: user.ID}, Values: updateValues,
	})
	if err != nil {
		t.Fatal(err)
	}
	if identity, _ := updated.Values["email"].StringValue(); identity != "next@example.com" {
		t.Fatalf("canonical updated auth identity = %q", identity)
	}
	if identity, _ := updateValues["email"].StringValue(); identity != " Next@Example.COM " {
		t.Fatalf("MongoDB Update mutated caller identity to %q", identity)
	}
	mongoCommit(t, update)

	conflict := mongoBegin(t, backend, false)
	_, err = conflict.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "user-2", Values: store.Values{"email": store.String(" NEXT@example.com")},
	})
	if !errors.Is(err, store.ErrConflict) {
		mongoRollback(t, conflict)
		t.Fatalf("canonical identity collision error = %v, want ErrConflict", err)
	}
	mongoRollback(t, conflict)

	now := time.Now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond)
	failed, err := backend.RecordFailedLogin(t.Context(), collection.ID, user.ID, now, 2, time.Minute)
	if err != nil || failed.FailedLoginAttempts != 1 || !failed.LockedUntil.IsZero() {
		t.Fatalf("first RecordFailedLogin() = %#v, %v", failed, err)
	}
	locked, err := backend.RecordFailedLogin(t.Context(), collection.ID, user.ID, now, 2, time.Minute)
	if err != nil || locked.FailedLoginAttempts != 2 || !locked.LockedUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("second RecordFailedLogin() = %#v, %v", locked, err)
	}
	if reset, err := backend.ResetLoginAttempts(t.Context(), collection.ID, user.ID, now.Add(time.Second)); err != nil || reset {
		t.Fatalf("ResetLoginAttempts() while locked = %v, %v", reset, err)
	}
	if err := backend.ForceUnlock(t.Context(), collection.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	secondHash := []byte("second-password-hash")
	if err := backend.UpgradePasswordHash(t.Context(), collection, user.ID, []byte("wrong"), secondHash); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale UpgradePasswordHash() error = %v", err)
	}
	if err := backend.UpgradePasswordHash(t.Context(), collection, user.ID, firstHash, secondHash); err != nil {
		t.Fatal(err)
	}
	session := store.AuthSession{
		ID: "session-id", TokenHash: "session-digest-1", CollectionID: collection.ID, UserID: user.ID,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IPAddress: "127.0.0.1", UserAgent: "integration",
	}
	if err := backend.CreateSession(t.Context(), session, firstHash); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale session password error = %v", err)
	}
	if err := backend.CreateSession(t.Context(), session, secondHash); err != nil {
		t.Fatal(err)
	}
	if found, err := backend.FindSession(t.Context(), session.TokenHash, now); err != nil || found.ID != session.ID {
		t.Fatalf("FindSession() = %#v, %v", found, err)
	}
	replacement := store.AuthSession{TokenHash: "session-digest-2", CollectionID: collection.ID, UserID: user.ID}
	rollbackSeenAt := now.Add(-time.Second)
	if err := backend.RotateSession(t.Context(), session.TokenHash, replacement, rollbackSeenAt); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindSession(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rotated session old digest error = %v", err)
	}
	rotated, err := backend.FindSession(t.Context(), replacement.TokenHash, now)
	if err != nil || rotated.ID != session.ID || !rotated.ExpiresAt.Equal(session.ExpiresAt) || !rotated.LastSeenAt.Equal(rollbackSeenAt) {
		t.Fatalf("rotated session = %#v, %v", rotated, err)
	}
	if sessions, err := backend.ListSessions(t.Context(), collection.ID, user.ID, now); err != nil || len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("ListSessions() = %#v, %v", sessions, err)
	}

	key := store.AuthAPIKey{
		ID: "api-key-id", TokenHash: "api-key-digest", CollectionID: collection.ID, UserID: user.ID,
		Name: "automation", CreatedAt: now, LastUsedAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour),
	}
	if err := backend.CreateAPIKey(t.Context(), key, replacement.TokenHash, now); err != nil {
		t.Fatal(err)
	}
	storedKey, err := backend.FindAPIKey(t.Context(), key.ID, now)
	if err != nil || !storedKey.LastUsedAt.IsZero() {
		t.Fatalf("new API key = %#v, %v; caller LastUsedAt must be ignored", storedKey, err)
	}
	rollbackUsedAt := now.Add(-2 * time.Second)
	if err := backend.TouchAPIKey(t.Context(), key.ID, rollbackUsedAt); err != nil {
		t.Fatal(err)
	}
	if keys, err := backend.ListAPIKeys(t.Context(), collection.ID, user.ID, now); err != nil || len(keys) != 1 || !keys[0].LastUsedAt.Equal(rollbackUsedAt) {
		t.Fatalf("ListAPIKeys() = %#v, %v", keys, err)
	}

	thirdHash := []byte("third-password-hash")
	if err := backend.ChangePasswordHash(t.Context(), collection, user.ID, secondHash, thirdHash); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindSession(t.Context(), replacement.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session survived password change: %v", err)
	}
	if _, err := backend.FindAPIKey(t.Context(), key.ID, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("API key survived password change: %v", err)
	}

	oldReset := store.AuthToken{
		TokenHash: "reset-digest-old", Purpose: store.AuthTokenPasswordReset,
		CollectionID: collection.ID, UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	newReset := oldReset
	newReset.TokenHash = "reset-digest-new"
	if err := backend.CreateAuthToken(t.Context(), oldReset); err != nil {
		t.Fatal(err)
	}
	if err := backend.CreateAuthToken(t.Context(), newReset); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ResetPasswordWithToken(t.Context(), collection.ID, oldReset.TokenHash, []byte("unused"), now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced reset token error = %v", err)
	}
	fourthHash := []byte("fourth-password-hash")
	if userID, err := backend.ResetPasswordWithToken(t.Context(), collection.ID, newReset.TokenHash, fourthHash, now); err != nil || userID != user.ID {
		t.Fatalf("ResetPasswordWithToken() = %q, %v", userID, err)
	}
	if _, err := backend.ResetPasswordWithToken(t.Context(), collection.ID, newReset.TokenHash, fourthHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed reset token error = %v", err)
	}
	verification := store.AuthToken{
		TokenHash: "verification-digest", Purpose: store.AuthTokenVerifyEmail,
		CollectionID: collection.ID, UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := backend.CreateAuthToken(t.Context(), verification); err != nil {
		t.Fatal(err)
	}
	if userID, err := backend.VerifyEmailWithToken(t.Context(), collection.ID, verification.TokenHash, now); err != nil || userID != user.ID {
		t.Fatalf("VerifyEmailWithToken() = %q, %v", userID, err)
	}
	if err := backend.SetPasswordHash(t.Context(), collection, user.ID, fourthHash, false); err != nil {
		t.Fatal(err)
	}
	credential, err = backend.FindAuthCredential(t.Context(), collection, "next@example.com")
	if err != nil || !credential.Verified {
		t.Fatalf("SetPasswordHash cleared existing verification: %#v, %v", credential, err)
	}

	for index, want := range []bool{true, true, false} {
		allowed, err := backend.AllowAuthAttempt(t.Context(), "rate-limit-digest", now, time.Minute, 2)
		if err != nil || allowed != want {
			t.Fatalf("AllowAuthAttempt() %d = %v, %v", index+1, allowed, err)
		}
	}
	if allowed, err := backend.AllowAuthAttempt(t.Context(), "rate-limit-digest", now.Add(time.Minute), time.Minute, 2); err != nil || !allowed {
		t.Fatalf("AllowAuthAttempt() at expiry = %v, %v", allowed, err)
	}
}

func TestMongoDBAuthMaintenanceConcurrencyBootstrapAndCleanup(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoAuthTestCollection(t, "auth-maintenance-users")
	manifest := mongoIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	secondBackend := mongoAdditionalIntegrationStore(t, backend)
	if err := secondBackend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	createMongoAuthUser(t, backend, collection, "owner", "owner@example.com", []byte("password-hash"))

	const failedWorkers = 12
	start := make(chan struct{})
	errorsFound := make(chan error, failedWorkers)
	var workers sync.WaitGroup
	for index := 0; index < failedWorkers; index++ {
		workerBackend := backend
		if index%2 != 0 {
			workerBackend = secondBackend
		}
		workers.Add(1)
		go func(workerBackend *Store) {
			defer workers.Done()
			<-start
			_, err := workerBackend.RecordFailedLogin(t.Context(), collection.ID, "owner", now, 100, time.Minute)
			errorsFound <- err
		}(workerBackend)
	}
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	next, err := backend.RecordFailedLogin(t.Context(), collection.ID, "owner", now, 100, time.Minute)
	if err != nil || next.FailedLoginAttempts != failedWorkers+1 {
		t.Fatalf("concurrent failed attempts = %#v, %v", next, err)
	}
	unlock := mongoBegin(t, backend, false)
	if err := unlock.(store.AuthUnlockTransaction).ForceUnlockAuth(t.Context(), collection.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, unlock)
	unlocked, err := backend.FindAuthCredential(t.Context(), collection, "owner@example.com")
	if err != nil || unlocked.FailedLoginAttempts != 0 || !unlocked.LockedUntil.IsZero() {
		t.Fatalf("credential after transaction-owned unlock = %#v, %v", unlocked, err)
	}

	singleUse := store.AuthToken{
		TokenHash: "single-use-verification-digest", Purpose: store.AuthTokenVerifyEmail,
		CollectionID: collection.ID, UserID: "owner", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := backend.CreateAuthToken(t.Context(), singleUse); err != nil {
		t.Fatal(err)
	}
	type tokenConsumption struct {
		userID string
		err    error
	}
	tokenStart := make(chan struct{})
	tokenResults := make(chan tokenConsumption, 2)
	for _, workerBackend := range []*Store{backend, secondBackend} {
		go func(workerBackend *Store) {
			<-tokenStart
			userID, err := workerBackend.VerifyEmailWithToken(t.Context(), collection.ID, singleUse.TokenHash, now)
			tokenResults <- tokenConsumption{userID: userID, err: err}
		}(workerBackend)
	}
	close(tokenStart)
	var consumed, rejected int
	for range 2 {
		result := <-tokenResults
		switch {
		case result.err == nil && result.userID == "owner":
			consumed++
		case errors.Is(result.err, store.ErrNotFound):
			rejected++
		default:
			t.Fatalf("concurrent token consumption = %q, %v", result.userID, result.err)
		}
	}
	if consumed != 1 || rejected != 1 {
		t.Fatalf("concurrent token outcomes: consumed=%d rejected=%d", consumed, rejected)
	}

	const rateWorkers = 20
	var admitted atomic.Int64
	start = make(chan struct{})
	errorsFound = make(chan error, rateWorkers)
	for index := 0; index < rateWorkers; index++ {
		workerBackend := backend
		if index%2 != 0 {
			workerBackend = secondBackend
		}
		workers.Add(1)
		go func(workerBackend *Store) {
			defer workers.Done()
			<-start
			allowed, err := workerBackend.AllowAuthAttempt(t.Context(), "concurrent-rate-digest", now, time.Minute, 5)
			if allowed {
				admitted.Add(1)
			}
			errorsFound <- err
		}(workerBackend)
	}
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if admitted.Load() != 5 {
		t.Fatalf("concurrent rate-limit admissions = %d, want 5", admitted.Load())
	}
	expiredBuckets := make([]any, 105)
	for index := range expiredBuckets {
		encoded, err := encodeMongoAuthRateLimit(mongoAuthRateLimit{
			KeyHash: "expired-rate-" + string(rune(0x100+index)), Attempts: 1, ExpiresAt: now.Add(-time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		expiredBuckets[index] = encoded
	}
	if _, err := backend.authRateLimitCollection().InsertMany(t.Context(), expiredBuckets); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AllowAuthAttempt(t.Context(), "bounded-cleanup-current", now, time.Minute, 5); err != nil {
		t.Fatal(err)
	}
	expiredCount, err := backend.authRateLimitCollection().CountDocuments(t.Context(), bson.D{{Key: "expiresAt", Value: bson.D{{Key: "$lte", Value: now.UnixNano()}}}})
	if err != nil {
		t.Fatal(err)
	}
	if expiredCount != 5 {
		t.Fatalf("bounded rate-limit cleanup left %d expired buckets, want 5", expiredCount)
	}

	active := store.AuthSession{
		ID: "active-session", TokenHash: "active-session-digest", CollectionID: collection.ID, UserID: "owner",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := backend.CreateSession(t.Context(), active, []byte("password-hash")); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		expired := store.AuthSession{
			ID: "expired-session-" + string(rune('a'+index)), TokenHash: "expired-session-digest-" + string(rune('a'+index)),
			CollectionID: collection.ID, UserID: "owner", CreatedAt: now.Add(-2 * time.Hour),
			LastSeenAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
		}
		if err := backend.CreateSession(t.Context(), expired, []byte("password-hash")); err != nil {
			t.Fatal(err)
		}
		key := store.AuthAPIKey{
			ID: "expired-key-" + string(rune('a'+index)), TokenHash: "expired-key-digest-" + string(rune('a'+index)),
			CollectionID: collection.ID, UserID: "owner", Name: "expired", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
		}
		if err := backend.CreateAPIKey(t.Context(), key, active.TokenHash, now); err != nil {
			t.Fatal(err)
		}
	}
	pruneStart := make(chan struct{})
	pruneResults := make(chan store.AuthPruneResult, 2)
	pruneErrors := make(chan error, 2)
	for _, workerBackend := range []*Store{backend, secondBackend} {
		workers.Add(1)
		go func(workerBackend *Store) {
			defer workers.Done()
			<-pruneStart
			result, err := workerBackend.PruneExpiredAuth(t.Context(), 1)
			pruneResults <- result
			pruneErrors <- err
		}(workerBackend)
	}
	close(pruneStart)
	workers.Wait()
	close(pruneResults)
	close(pruneErrors)
	var concurrentPrune store.AuthPruneResult
	for err := range pruneErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range pruneResults {
		if result.Sessions > 1 || result.APIKeys > 1 {
			t.Fatalf("concurrent prune exceeded batch: %#v", result)
		}
		concurrentPrune.Sessions += result.Sessions
		concurrentPrune.APIKeys += result.APIKeys
	}
	if concurrentPrune != (store.AuthPruneResult{Sessions: 2, APIKeys: 2}) {
		t.Fatalf("concurrent auth prune = %#v", concurrentPrune)
	}
	second, err := backend.PruneExpiredAuth(t.Context(), 2)
	if err != nil || second != (store.AuthPruneResult{Sessions: 1, APIKeys: 1}) {
		t.Fatalf("final auth prune = %#v, %v", second, err)
	}
	if _, err := backend.FindSession(t.Context(), active.TokenHash, now); err != nil {
		t.Fatalf("active session pruned: %v", err)
	}

	bootstrapCollection := mongoAuthTestCollection(t, "bootstrap-users")
	corruptBootstrapCollection := mongoAuthTestCollection(t, "bootstrap-corrupt-users")
	bootstrapRaceCollection := mongoAuthTestCollection(t, "bootstrap-race-users")
	bootstrapManifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "auth bootstrap"},
		Collections: []schema.Collection{collection, bootstrapCollection, corruptBootstrapCollection, bootstrapRaceCollection}, Plugins: []schema.Plugin{},
	})
	if err := backend.SyncIndexes(t.Context(), bootstrapManifest); err != nil {
		t.Fatal(err)
	}
	if err := secondBackend.VerifyIndexes(t.Context(), bootstrapManifest); err != nil {
		t.Fatal(err)
	}
	corrupt, err := encodeDocument(store.Document{
		ID: "corrupt", CreatedAt: now, UpdatedAt: now,
		Values: store.Values{"email": store.String("corrupt@example.com"), "unexpected": store.String("out-of-band")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.database.Collection(physicalCollectionName(corruptBootstrapCollection.ID)).InsertOne(t.Context(), corrupt); err != nil {
		t.Fatal(err)
	}
	corruptBootstrap := mongoBegin(t, backend, false)
	if _, err := corruptBootstrap.Create(t.Context(), store.CreateRequest{
		Collection: corruptBootstrapCollection, ID: "candidate", Values: store.Values{"email": store.String("candidate@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := corruptBootstrap.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(t.Context(), corruptBootstrapCollection, "candidate", []byte("candidate-hash"), true); !errors.Is(err, store.ErrAuthInitialized) {
		mongoRollback(t, corruptBootstrap)
		t.Fatalf("bootstrap with malformed active row error = %v, want ErrAuthInitialized", err)
	}
	mongoRollback(t, corruptBootstrap)
	winningBootstrap := mongoBegin(t, backend, false)
	if _, err := winningBootstrap.Create(t.Context(), store.CreateRequest{
		Collection: bootstrapRaceCollection, ID: "winner", Values: store.Values{"email": store.String("winner@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := winningBootstrap.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(t.Context(), bootstrapRaceCollection, "winner", []byte("winner-hash"), true); err != nil {
		t.Fatal(err)
	}
	contenderStarted := make(chan struct{})
	contenderResult := make(chan error, 1)
	go func() {
		close(contenderStarted)
		contender, err := secondBackend.Begin(t.Context())
		if err != nil {
			contenderResult <- err
			return
		}
		if _, err := contender.Create(t.Context(), store.CreateRequest{
			Collection: bootstrapRaceCollection, ID: "contender", Values: store.Values{"email": store.String("contender@example.com")},
		}); err != nil {
			_ = contender.Rollback(context.Background())
			contenderResult <- err
			return
		}
		bootstrapErr := contender.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(t.Context(), bootstrapRaceCollection, "contender", []byte("contender-hash"), true)
		if bootstrapErr != nil {
			_ = contender.Rollback(context.Background())
			contenderResult <- bootstrapErr
			return
		}
		contenderResult <- contender.Commit(t.Context())
	}()
	<-contenderStarted
	winnerErr := winningBootstrap.Commit(t.Context())
	var contenderErr error
	select {
	case contenderErr = <-contenderResult:
	case <-time.After(5 * time.Second):
		t.Fatal("bootstrap contender did not finish after the winner committed")
	}
	successes := 0
	for name, err := range map[string]error{"winner": winnerErr, "contender": contenderErr} {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, store.ErrAuthInitialized) {
			t.Fatalf("bootstrap %s error = %v, want ErrAuthInitialized", name, err)
		}
	}
	if successes != 1 {
		t.Fatalf("bootstrap race successes = %d, want exactly 1 (winner=%v contender=%v)", successes, winnerErr, contenderErr)
	}
	verification := mongoBegin(t, secondBackend, true)
	page, err := verification.List(t.Context(), store.Request{Collection: bootstrapRaceCollection, Page: 1, Limit: 10})
	if err != nil {
		mongoRollback(t, verification)
		t.Fatal(err)
	}
	mongoCommit(t, verification)
	if page.Total != 1 || len(page.Documents) != 1 || (page.Documents[0].ID != "winner" && page.Documents[0].ID != "contender") {
		t.Fatalf("documents after bootstrap race = %#v", page)
	}
	bootstrap := mongoBegin(t, backend, false)
	if _, err := bootstrap.Create(t.Context(), store.CreateRequest{
		Collection: bootstrapCollection, ID: "first", Values: store.Values{"email": store.String("first@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(t.Context(), bootstrapCollection, "first", []byte("first-hash"), true); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, bootstrap)
	secondBootstrap := mongoBegin(t, backend, false)
	if _, err := secondBootstrap.Create(t.Context(), store.CreateRequest{
		Collection: bootstrapCollection, ID: "second", Values: store.Values{"email": store.String("second@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := secondBootstrap.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(t.Context(), bootstrapCollection, "second", []byte("second-hash"), true); !errors.Is(err, store.ErrAuthInitialized) {
		mongoRollback(t, secondBootstrap)
		t.Fatalf("second bootstrap error = %v", err)
	}
	mongoRollback(t, secondBootstrap)
	deleteFirst := mongoBegin(t, backend, false)
	if _, err := deleteFirst.Delete(t.Context(), store.Request{Collection: bootstrapCollection, ID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := deleteFirst.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: bootstrapCollection.ID, DocumentID: "first"}); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, deleteFirst)
	rebootstrap := mongoBegin(t, backend, false)
	if _, err := rebootstrap.Create(t.Context(), store.CreateRequest{
		Collection: bootstrapCollection, ID: "replacement", Values: store.Values{"email": store.String("replacement@example.com")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := rebootstrap.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(t.Context(), bootstrapCollection, "replacement", []byte("replacement-hash"), true); err != nil {
		t.Fatalf("bootstrap guard prevented reinitialization after all active users were removed: %v", err)
	}
	mongoCommit(t, rebootstrap)

	cleanup := mongoBegin(t, backend, false)
	if _, err := cleanup.Delete(t.Context(), store.Request{Collection: collection, ID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := cleanup.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: collection.ID, DocumentID: "owner"}); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, cleanup)
	if _, err := backend.FindSession(t.Context(), active.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session survived hard-delete cleanup: %v", err)
	}
	if _, err := backend.FindAuthCredential(t.Context(), collection, "owner@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("credential survived hard-delete cleanup: %v", err)
	}
}

func TestMongoDBAuthRevocationAndIncarnationFencesAcrossClients(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoAuthTestCollection(t, "auth-fence-users")
	manifest := mongoIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	second := mongoAdditionalIntegrationStore(t, backend)
	if err := second.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	createMongoAuthUser(t, backend, collection, "owner", "owner@example.com", []byte("first-hash"))
	session := store.AuthSession{
		ID: "race-session", TokenHash: "race-session-digest", CollectionID: collection.ID, UserID: "owner",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := backend.CreateSession(t.Context(), session, []byte("first-hash")); err != nil {
		t.Fatal(err)
	}
	revocation := mongoBegin(t, second, false)
	revocationTransaction := revocation.(*documentTransaction)
	revocationContext, leave, err := revocationTransaction.enter(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revocationTransaction.authSessionCollection().DeleteOne(revocationContext, bson.D{{Key: "_id", Value: session.TokenHash}}); err != nil {
		leave()
		t.Fatal(err)
	}
	leave()
	lateKeyResult := make(chan error, 1)
	go func() {
		lateKeyResult <- backend.CreateAPIKey(t.Context(), store.AuthAPIKey{
			ID: "late-key", TokenHash: "late-key-digest", CollectionID: collection.ID,
			UserID: "owner", Name: "late", CreatedAt: now,
		}, session.TokenHash, now)
	}()
	select {
	case err := <-lateKeyResult:
		mongoRollback(t, revocation)
		t.Fatalf("API-key creation crossed an uncommitted session revocation without waiting: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	mongoCommit(t, revocation)
	select {
	case err := <-lateKeyResult:
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("API-key creation after session revocation = %v, want ErrNotFound", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("API-key creation did not finish after session revocation committed")
	}
	if _, err := backend.FindAPIKey(t.Context(), "late-key", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late API key survived session revocation: %v", err)
	}
	if err := backend.CreateSession(t.Context(), session, []byte("first-hash")); err != nil {
		t.Fatal(err)
	}
	ownedRotation := stageMongoAuthSessionRotation(t, second, session, "owned-rotated-digest", now.Add(time.Second))
	ownedRevocationResult := make(chan error, 1)
	go func() {
		ownedRevocationResult <- backend.DeleteUserSession(t.Context(), collection.ID, "owner", session.ID)
	}()
	select {
	case err := <-ownedRevocationResult:
		mongoRollback(t, ownedRotation)
		t.Fatalf("owned-session revocation crossed an uncommitted rotation without waiting: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	mongoCommit(t, ownedRotation)
	select {
	case err := <-ownedRevocationResult:
		if err != nil {
			t.Fatalf("owned-session revocation after rotation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned-session revocation did not finish after rotation committed")
	}
	if _, err := backend.FindSession(t.Context(), "owned-rotated-digest", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rotated session survived public-ID revocation: %v", err)
	}

	if err := backend.CreateSession(t.Context(), session, []byte("first-hash")); err != nil {
		t.Fatal(err)
	}
	allRotation := stageMongoAuthSessionRotation(t, second, session, "all-rotated-digest", now.Add(time.Second))
	allRevocationResult := make(chan error, 1)
	go func() {
		allRevocationResult <- backend.DeleteUserSessions(t.Context(), collection.ID, "owner")
	}()
	select {
	case err := <-allRevocationResult:
		mongoRollback(t, allRotation)
		t.Fatalf("all-session revocation crossed an uncommitted rotation without waiting: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	mongoCommit(t, allRotation)
	select {
	case err := <-allRevocationResult:
		if err != nil {
			t.Fatalf("all-session revocation after rotation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("all-session revocation did not finish after rotation committed")
	}
	if sessions, err := backend.ListSessions(t.Context(), collection.ID, "owner", now); err != nil || len(sessions) != 0 {
		t.Fatalf("sessions after rotate/delete-all race = %#v, %v", sessions, err)
	}

	if err := backend.CreateSession(t.Context(), session, []byte("first-hash")); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	changeResult := make(chan error, 1)
	keyResult := make(chan error, 1)
	go func() {
		<-start
		changeResult <- backend.ChangePasswordHash(t.Context(), collection, "owner", []byte("first-hash"), []byte("second-hash"))
	}()
	go func() {
		<-start
		keyResult <- second.CreateAPIKey(t.Context(), store.AuthAPIKey{
			ID: "race-key", TokenHash: "race-key-digest", CollectionID: collection.ID,
			UserID: "owner", Name: "race", CreatedAt: now,
		}, session.TokenHash, now)
	}()
	close(start)
	if err := <-changeResult; err != nil {
		t.Fatal(err)
	}
	if err := <-keyResult; err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrConflict) {
		t.Fatalf("concurrent API-key creation error = %v", err)
	}
	if _, err := backend.FindAPIKey(t.Context(), "race-key", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("API key survived concurrent password revocation: %v", err)
	}
	if _, err := backend.FindSession(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session survived concurrent password revocation: %v", err)
	}

	staleSession := store.AuthSession{
		ID: "stale-session", TokenHash: "stale-session-digest", CollectionID: collection.ID, UserID: "owner",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := backend.CreateSession(t.Context(), staleSession, []byte("second-hash")); err != nil {
		t.Fatal(err)
	}
	staleRaw, err := backend.authSessionCollection().FindOne(t.Context(), bson.D{{Key: "_id", Value: staleSession.TokenHash}}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	cleanup := mongoBegin(t, backend, false)
	if _, err := cleanup.Delete(t.Context(), store.Request{Collection: collection, ID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := cleanup.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: collection.ID, DocumentID: "owner"}); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, cleanup)
	createMongoAuthUser(t, backend, collection, "owner", "replacement@example.com", []byte("replacement-hash"))
	if _, err := backend.authSessionCollection().InsertOne(t.Context(), staleRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := second.FindSession(t.Context(), staleSession.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale session attached to same-ID recreation: %v", err)
	}
}

func TestMongoDBAuthPasswordMutationsSerializeWithSessionCreation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoAuthTestCollection(t, "auth-password-race-users")
	manifest := mongoIndexTestManifest(collection)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	second := mongoAdditionalIntegrationStore(t, backend)
	if err := second.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second).Add(987654321 * time.Nanosecond)
	tests := []struct {
		name            string
		mutate          func(string, []byte, []byte) error
		sessionSurvives bool
	}{
		{
			name: "set",
			mutate: func(userID string, _, replacement []byte) error {
				return second.SetPasswordHash(t.Context(), collection, userID, replacement, true)
			},
		},
		{
			name: "change",
			mutate: func(userID string, current, replacement []byte) error {
				return second.ChangePasswordHash(t.Context(), collection, userID, current, replacement)
			},
		},
		{
			name: "upgrade",
			mutate: func(userID string, current, replacement []byte) error {
				return second.UpgradePasswordHash(t.Context(), collection, userID, current, replacement)
			},
			sessionSurvives: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID := test.name + "-user"
			identity := test.name + "@example.com"
			current := []byte(test.name + "-current-hash")
			replacement := []byte(test.name + "-replacement-hash")
			createMongoAuthUser(t, backend, collection, userID, identity, current)
			session := store.AuthSession{
				ID: test.name + "-session", TokenHash: test.name + "-session-digest",
				CollectionID: collection.ID, UserID: userID,
				CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
			}
			creation := stageMongoAuthSessionCreation(t, backend, session, current)
			mutationResult := make(chan error, 1)
			go func() {
				mutationResult <- test.mutate(userID, current, replacement)
			}()
			select {
			case err := <-mutationResult:
				mongoRollback(t, creation)
				t.Fatalf("password %s crossed an uncommitted session creation without waiting: %v", test.name, err)
			case <-time.After(150 * time.Millisecond):
			}
			mongoCommit(t, creation)
			select {
			case err := <-mutationResult:
				if err != nil {
					t.Fatalf("password %s after session creation = %v", test.name, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("password %s did not finish after session creation committed", test.name)
			}
			found, err := backend.FindSession(t.Context(), session.TokenHash, now)
			if test.sessionSurvives {
				if err != nil || found.ID != session.ID {
					t.Fatalf("session after password %s = %#v, %v", test.name, found, err)
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("session survived password %s: %#v, %v", test.name, found, err)
			}
			credential, err := backend.FindAuthCredential(t.Context(), collection, identity)
			if err != nil || !bytes.Equal(credential.PasswordHash, replacement) {
				t.Fatalf("credential after password %s = %#v, %v", test.name, credential, err)
			}
		})
	}
}

func mongoAuthTestCollection(t *testing.T, id schema.StableID) schema.Collection {
	t.Helper()
	path, err := query.ParsePath("email")
	if err != nil {
		t.Fatal(err)
	}
	return schema.Collection{
		ID: id, Slug: schema.CollectionSlug(id),
		Capabilities: schema.Capabilities{Auth: true},
		Auth:         &schema.AuthSettings{IdentityField: "email"},
		Fields: []schema.Field{{
			ID: schema.StableID(string(id) + "-email"), Name: "email", Path: path,
			Type: schema.FieldTypeEmail, Category: schema.FieldCategoryScalar,
			Required: true, Unique: true,
		}},
	}
}

func createMongoAuthUser(t *testing.T, backend *Store, collection schema.Collection, id, identity string, hash []byte) {
	t.Helper()
	transaction := mongoBegin(t, backend, false)
	if _, err := transaction.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: id, Values: store.Values{"email": store.String(identity)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.(store.AuthTransaction).CreateAuthCredential(t.Context(), collection, id, hash, true); err != nil {
		t.Fatal(err)
	}
	mongoCommit(t, transaction)
}

func mongoAdditionalIntegrationStore(t *testing.T, source *Store) *Store {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + source.database.Name()
	backend, err := OpenWithConfig(context.Background(), Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close additional MongoDB integration store: %v", err)
		}
	})
	return backend
}

func stageMongoAuthSessionRotation(t *testing.T, backend *Store, session store.AuthSession, replacementHash string, lastSeenAt time.Time) store.Transaction {
	t.Helper()
	reference := store.DocumentReference{CollectionID: session.CollectionID, DocumentID: session.UserID}
	references, err := backend.captureActiveDocumentReferences(t.Context(), reference)
	if err != nil {
		t.Fatal(err)
	}
	transaction := mongoBegin(t, backend, false)
	documentTransaction := transaction.(*documentTransaction)
	sessionContext, leave, err := documentTransaction.enter(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	if err := documentTransaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
		t.Fatal(err)
	}
	current, found, err := findMongoAuthSessionByToken(sessionContext, documentTransaction.authSessionCollection(), session.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("session to stage for rotation was not found")
	}
	result, err := documentTransaction.authSessionCollection().DeleteOne(sessionContext, bson.D{
		{Key: "_id", Value: session.TokenHash},
		{Key: "tokenHash", Value: session.TokenHash},
		{Key: "userIncarnation", Value: current.UserIncarnation},
		{Key: "fence", Value: current.Fence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("staged rotation deleted %d sessions, want 1", result.DeletedCount)
	}
	current.Session.TokenHash = replacementHash
	current.Session.LastSeenAt = lastSeenAt
	current.Fence++
	encoded, err := encodeMongoAuthSession(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := documentTransaction.authSessionCollection().InsertOne(sessionContext, encoded); err != nil {
		t.Fatal(err)
	}
	return transaction
}

func stageMongoAuthSessionCreation(t *testing.T, backend *Store, session store.AuthSession, expectedPasswordHash []byte) store.Transaction {
	t.Helper()
	reference := store.DocumentReference{CollectionID: session.CollectionID, DocumentID: session.UserID}
	references, err := backend.captureActiveDocumentReferences(t.Context(), reference)
	if err != nil {
		t.Fatal(err)
	}
	transaction := mongoBegin(t, backend, false)
	documentTransaction := transaction.(*documentTransaction)
	sessionContext, leave, err := documentTransaction.enter(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	if err := documentTransaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
		t.Fatal(err)
	}
	incarnation, err := mongoCapturedAuthIncarnation(references, reference)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := documentTransaction.requireMongoAuthCredentialIncarnation(sessionContext, session.CollectionID, session.UserID, incarnation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(credential.PasswordHash, expectedPasswordHash) {
		t.Fatal("credential changed before the staged session creation")
	}
	encoded, err := encodeMongoAuthSession(mongoAuthSession{Session: session, UserIncarnation: incarnation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := documentTransaction.authSessionCollection().InsertOne(sessionContext, encoded); err != nil {
		t.Fatal(err)
	}
	return transaction
}
