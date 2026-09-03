package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBDeleteDocumentStateRequiresVerifiedSystemIndexes(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, _, _, _ := mongoSystemStoreManifest()
	transaction := mongoBegin(t, backend, false)
	err := transaction.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: users.ID, DocumentID: "owner"})
	if err == nil || !strings.Contains(err.Error(), "preference indexes are not verified") {
		mongoRollback(t, transaction)
		t.Fatalf("unverified system cleanup error = %v", err)
	}
	mongoRollback(t, transaction)
}

func TestMongoDBPreferencesAndDocumentLocksMatchStoreSemantics(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, staff, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 19, 20, 21, 123456789, time.UTC)
	backend.now = func() time.Time { return now }
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "user-1", "user-2", "trashed-user"),
		mongoSystemDocuments(staff, "user-1"),
		mongoSystemDocuments(posts, "post-1", "post-2", "trashed-post"),
	)
	trashMongoSystemDocument(t, backend, users, "trashed-user")
	trashMongoSystemDocument(t, backend, posts, "trashed-post")

	preference, err := backend.SetPreference(t.Context(), store.Preference{
		CollectionID: users.ID, UserID: "user-1", Key: "theme", Value: json.RawMessage(`{"mode":"dark"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if preference.UpdatedAt != now || string(preference.Value) != `{"mode":"dark"}` {
		t.Fatalf("set preference = %#v", preference)
	}
	preference.Value[0] = '['
	stored, err := backend.GetPreference(t.Context(), users.ID, "user-1", "theme")
	if err != nil {
		t.Fatal(err)
	}
	if stored.UpdatedAt != now || string(stored.Value) != `{"mode":"dark"}` {
		t.Fatalf("detached stored preference = %#v", stored)
	}
	if _, err := backend.SetPreference(t.Context(), store.Preference{
		CollectionID: staff.ID, UserID: "user-1", Key: "theme", Value: json.RawMessage(`"staff"`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.DeletePreference(t.Context(), users.ID, "user-1", "theme"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.GetPreference(t.Context(), users.ID, "user-1", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted preference error = %v, want ErrNotFound", err)
	}
	if staffPreference, err := backend.GetPreference(t.Context(), staff.ID, "user-1", "theme"); err != nil || string(staffPreference.Value) != `"staff"` {
		t.Fatalf("collection-scoped preference = %#v, %v", staffPreference, err)
	}
	for _, key := range []string{"theme", "layout"} {
		if _, err := backend.SetPreference(t.Context(), store.Preference{
			CollectionID: users.ID, UserID: "user-1", Key: key, Value: json.RawMessage(`true`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := backend.DeletePreferences(t.Context(), users.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"theme", "layout"} {
		if _, err := backend.GetPreference(t.Context(), users.ID, "user-1", key); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("reset preference %q error = %v", key, err)
		}
	}
	if _, err := backend.SetPreference(t.Context(), store.Preference{
		CollectionID: users.ID, UserID: "user-1", Key: "invalid", Value: json.RawMessage(`{`),
	}); err == nil {
		t.Fatal("invalid preference JSON was accepted")
	}
	if _, err := backend.SetPreference(t.Context(), store.Preference{
		CollectionID: users.ID, UserID: "trashed-user", Key: "late", Value: json.RawMessage(`true`),
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("trashed preference owner error = %v, want ErrNotFound", err)
	}

	first := store.DocumentLock{
		CollectionID: posts.ID, DocumentID: "post-1",
		OwnerCollectionID: users.ID, OwnerID: "user-1", OwnerLabel: "First",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	lock, acquired, err := backend.AcquireDocumentLock(t.Context(), first, now, false)
	if err != nil || !acquired || lock.OwnerID != first.OwnerID {
		t.Fatalf("first lock = %#v, %t, %v", lock, acquired, err)
	}
	refresh := first
	refresh.OwnerLabel = "Refreshed"
	refresh.CreatedAt = now.Add(10 * time.Second)
	refresh.UpdatedAt = now.Add(10 * time.Second)
	refresh.ExpiresAt = now.Add(2 * time.Minute)
	lock, acquired, err = backend.AcquireDocumentLock(t.Context(), refresh, now.Add(10*time.Second), false)
	if err != nil || !acquired || lock.CreatedAt != first.CreatedAt || lock.OwnerLabel != "Refreshed" {
		t.Fatalf("refreshed lock = %#v, %t, %v", lock, acquired, err)
	}
	second := store.DocumentLock{
		CollectionID: posts.ID, DocumentID: "post-1",
		OwnerCollectionID: users.ID, OwnerID: "user-2", OwnerLabel: "Second",
		CreatedAt: now.Add(20 * time.Second), UpdatedAt: now.Add(20 * time.Second), ExpiresAt: now.Add(3 * time.Minute),
	}
	lock, acquired, err = backend.AcquireDocumentLock(t.Context(), second, now.Add(20*time.Second), false)
	if err != nil || acquired || lock.OwnerID != first.OwnerID {
		t.Fatalf("blocked lock = %#v, %t, %v", lock, acquired, err)
	}
	lock, acquired, err = backend.AcquireDocumentLock(t.Context(), second, now.Add(20*time.Second), true)
	if err != nil || !acquired || lock.OwnerID != second.OwnerID || lock.CreatedAt != second.CreatedAt {
		t.Fatalf("taken-over lock = %#v, %t, %v", lock, acquired, err)
	}
	if err := backend.ReleaseDocumentLock(t.Context(), posts.ID, "post-1", users.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if current, err := backend.FindDocumentLock(t.Context(), posts.ID, "post-1", now); err != nil || current.OwnerID != "user-2" {
		t.Fatalf("wrong-owner release changed lock = %#v, %v", current, err)
	}
	if err := backend.ReleaseDocumentLock(t.Context(), posts.ID, "post-1", users.ID, "user-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindDocumentLock(t.Context(), posts.ID, "post-1", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("released lock error = %v, want ErrNotFound", err)
	}

	expired := first
	expired.DocumentID = "post-2"
	expired.ExpiresAt = now.Add(time.Second)
	if _, acquired, err := backend.AcquireDocumentLock(t.Context(), expired, now, false); err != nil || !acquired {
		t.Fatalf("seed expiring lock = %t, %v", acquired, err)
	}
	if _, err := backend.FindDocumentLock(t.Context(), posts.ID, "post-2", expired.ExpiresAt); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("lock at exact expiry error = %v, want ErrNotFound", err)
	}
	second.DocumentID = "post-2"
	if lock, acquired, err := backend.AcquireDocumentLock(t.Context(), second, expired.ExpiresAt, false); err != nil || !acquired || lock.OwnerID != second.OwnerID {
		t.Fatalf("expired lock replacement = %#v, %t, %v", lock, acquired, err)
	}

	trashedOwner := first
	trashedOwner.DocumentID = "post-1"
	trashedOwner.OwnerID = "trashed-user"
	if _, _, err := backend.AcquireDocumentLock(t.Context(), trashedOwner, now, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("trashed lock owner error = %v, want ErrNotFound", err)
	}
	trashedTarget := first
	trashedTarget.DocumentID = "trashed-post"
	if _, _, err := backend.AcquireDocumentLock(t.Context(), trashedTarget, now, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("trashed lock target error = %v, want ErrNotFound", err)
	}
}

func TestMongoDBSystemLogicalIdentityDriftFailsClosedWithoutMutation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, _, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 21, 22, 23, 123456789, time.UTC)
	backend.now = func() time.Time { return now }
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "preference-owner", "lock-owner", "replacement-owner"),
		mongoSystemDocuments(posts, "lock-target"),
	)

	assertPromptCollision := func(t *testing.T, operation func(context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		err := operation(ctx)
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("identity drift error = %v, want ErrConflict", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("identity drift waited for context deadline: %v", err)
		}
	}

	t.Run("preference", func(t *testing.T) {
		preference := store.Preference{
			CollectionID: users.ID, UserID: "preference-owner", Key: "theme",
			Value: json.RawMessage(`"before"`), UpdatedAt: now,
		}
		encoded, err := encodeMongoPreference(preference)
		if err != nil {
			t.Fatal(err)
		}
		wrongID := "z_preference_out_of_band"
		encoded[0].Value = wrongID
		if _, err := backend.preferenceCollection().InsertOne(t.Context(), encoded); err != nil {
			t.Fatal(err)
		}
		ownerFence, ownerUpdatedAt := mongoStoredFence(t, backend, users, preference.UserID)

		assertPromptCollision(t, func(ctx context.Context) error {
			_, err := backend.GetPreference(ctx, preference.CollectionID, preference.UserID, preference.Key)
			return err
		})
		assertPromptCollision(t, func(ctx context.Context) error {
			candidate := preference
			candidate.Value = json.RawMessage(`"after"`)
			_, err := backend.SetPreference(ctx, candidate)
			return err
		})
		assertPromptCollision(t, func(ctx context.Context) error {
			return backend.DeletePreference(ctx, preference.CollectionID, preference.UserID, preference.Key)
		})

		if fence, updatedAt := mongoStoredFence(t, backend, users, preference.UserID); fence != ownerFence || updatedAt != ownerUpdatedAt {
			t.Fatalf("preference collision changed owner fence = (%d, %d), want (%d, %d)", fence, updatedAt, ownerFence, ownerUpdatedAt)
		}
		raw, err := backend.preferenceCollection().FindOne(t.Context(), bson.D{{Key: "_id", Value: wrongID}}).Raw()
		if err != nil {
			t.Fatalf("wrong-ID preference was removed: %v", err)
		}
		if subtype, value, ok := raw.Lookup("value").BinaryOK(); !ok || subtype != 0 || string(value) != `"before"` {
			t.Fatalf("wrong-ID preference value changed = subtype %d, %q, %t", subtype, value, ok)
		}
		if count, err := backend.preferenceCollection().CountDocuments(t.Context(), bson.D{{Key: "_id", Value: mongoPreferenceID(preference)}}); err != nil || count != 0 {
			t.Fatalf("deterministic preference row count = %d, %v", count, err)
		}
	})

	t.Run("document lock", func(t *testing.T) {
		lock := mongoSystemLock(posts.ID, "lock-target", users.ID, "lock-owner", now)
		encoded, err := encodeMongoDocumentLock(lock)
		if err != nil {
			t.Fatal(err)
		}
		wrongID := "z_document_lock_out_of_band"
		encoded[0].Value = wrongID
		if _, err := backend.documentLockCollection().InsertOne(t.Context(), encoded); err != nil {
			t.Fatal(err)
		}
		targetFence, targetUpdatedAt := mongoStoredFence(t, backend, posts, lock.DocumentID)
		ownerFence, ownerUpdatedAt := mongoStoredFence(t, backend, users, lock.OwnerID)

		assertPromptCollision(t, func(ctx context.Context) error {
			_, err := backend.FindDocumentLock(ctx, lock.CollectionID, lock.DocumentID, now)
			return err
		})
		assertPromptCollision(t, func(ctx context.Context) error {
			candidate := mongoSystemLock(posts.ID, lock.DocumentID, users.ID, "replacement-owner", now.Add(time.Second))
			_, _, err := backend.AcquireDocumentLock(ctx, candidate, now.Add(time.Second), true)
			return err
		})
		assertPromptCollision(t, func(ctx context.Context) error {
			return backend.ReleaseDocumentLock(ctx, lock.CollectionID, lock.DocumentID, lock.OwnerCollectionID, lock.OwnerID)
		})

		if fence, updatedAt := mongoStoredFence(t, backend, posts, lock.DocumentID); fence != targetFence || updatedAt != targetUpdatedAt {
			t.Fatalf("lock collision changed target fence = (%d, %d), want (%d, %d)", fence, updatedAt, targetFence, targetUpdatedAt)
		}
		if fence, updatedAt := mongoStoredFence(t, backend, users, lock.OwnerID); fence != ownerFence || updatedAt != ownerUpdatedAt {
			t.Fatalf("lock collision changed owner fence = (%d, %d), want (%d, %d)", fence, updatedAt, ownerFence, ownerUpdatedAt)
		}
		raw, err := backend.documentLockCollection().FindOne(t.Context(), bson.D{{Key: "_id", Value: wrongID}}).Raw()
		if err != nil {
			t.Fatalf("wrong-ID document lock was removed: %v", err)
		}
		if label, ok := raw.Lookup("ownerLabel").StringValueOK(); !ok || label != lock.OwnerLabel {
			t.Fatalf("wrong-ID document lock owner label changed = %q, %t", label, ok)
		}
		if count, err := backend.documentLockCollection().CountDocuments(t.Context(), bson.D{{Key: "_id", Value: mongoDocumentLockID(lock)}}); err != nil || count != 0 {
			t.Fatalf("deterministic document-lock row count = %d, %v", count, err)
		}
	})
}

func TestMongoDBPreferenceWritesSerializeAcrossClients(t *testing.T) {
	backend := mongoIntegrationStore(t)
	peer := mongoSystemStorePeer(t, backend)
	users, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := peer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	seedMongoSystemDocuments(t, backend, mongoSystemDocuments(users, "preference-owner"))

	type preferenceResult struct {
		candidate store.Preference
		stored    store.Preference
		err       error
	}
	run := func(candidates []store.Preference) []preferenceResult {
		t.Helper()
		start := make(chan struct{})
		results := make(chan preferenceResult, len(candidates))
		clients := []*Store{backend, peer}
		for index, candidate := range candidates {
			client := clients[index%len(clients)]
			candidate := candidate
			go func() {
				<-start
				stored, err := client.SetPreference(t.Context(), candidate)
				results <- preferenceResult{candidate: candidate, stored: stored, err: err}
			}()
		}
		close(start)
		collected := make([]preferenceResult, 0, len(candidates))
		for range candidates {
			collected = append(collected, <-results)
		}
		return collected
	}

	sameKey := make([]store.Preference, 12)
	for index := range sameKey {
		sameKey[index] = store.Preference{
			CollectionID: users.ID,
			UserID:       "preference-owner",
			Key:          "theme",
			Value:        json.RawMessage(`"writer-` + string(rune('a'+index)) + `"`),
		}
	}
	for _, outcome := range run(sameKey) {
		if outcome.err != nil {
			t.Fatalf("same-key preference contention leaked error: %v", outcome.err)
		}
		if string(outcome.stored.Value) != string(outcome.candidate.Value) {
			t.Fatalf("same-key preference returned another writer's value: got %s want %s", outcome.stored.Value, outcome.candidate.Value)
		}
	}

	differentKeys := make([]store.Preference, 12)
	for index := range differentKeys {
		differentKeys[index] = store.Preference{
			CollectionID: users.ID,
			UserID:       "preference-owner",
			Key:          "key-" + string(rune('a'+index)),
			Value:        json.RawMessage(`true`),
		}
	}
	for _, outcome := range run(differentKeys) {
		if outcome.err != nil {
			t.Fatalf("different-key preference contention leaked error: %v", outcome.err)
		}
	}
	for _, candidate := range differentKeys {
		stored, err := backend.GetPreference(t.Context(), candidate.CollectionID, candidate.UserID, candidate.Key)
		if err != nil || string(stored.Value) != string(candidate.Value) {
			t.Fatalf("different-key preference %q = %#v, %v", candidate.Key, stored, err)
		}
	}
}

func TestMongoDBDeleteDocumentStateCleansPreferencesAndBothLockSides(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, staff, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 20, 0, 0, 987654321, time.UTC)
	backend.now = func() time.Time { return now }
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "shared"),
		mongoSystemDocuments(staff, "shared"),
		mongoSystemDocuments(posts, "owned-target", "staff-target"),
	)
	if _, err := backend.SetPreference(t.Context(), store.Preference{CollectionID: users.ID, UserID: "shared", Key: "theme", Value: json.RawMessage(`"user"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(t.Context(), store.Preference{CollectionID: staff.ID, UserID: "shared", Key: "theme", Value: json.RawMessage(`"staff"`)}); err != nil {
		t.Fatal(err)
	}
	ownedLock := mongoSystemLock(posts.ID, "owned-target", users.ID, "shared", now)
	targetLock := mongoSystemLock(users.ID, "shared", staff.ID, "shared", now)
	staffLock := mongoSystemLock(posts.ID, "staff-target", staff.ID, "shared", now)
	for _, candidate := range []store.DocumentLock{ownedLock, targetLock, staffLock} {
		if _, acquired, err := backend.AcquireDocumentLock(t.Context(), candidate, now, false); err != nil || !acquired {
			t.Fatalf("seed lock %#v = %t, %v", candidate, acquired, err)
		}
	}

	reference := store.DocumentReference{CollectionID: users.ID, DocumentID: "shared"}
	rolledBack := mongoBegin(t, backend, false)
	if err := rolledBack.DeleteDocumentState(t.Context(), reference); err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	mongoRollback(t, rolledBack)
	if _, err := backend.GetPreference(t.Context(), users.ID, "shared", "theme"); err != nil {
		t.Fatalf("preference did not survive rolled-back cleanup: %v", err)
	}
	for _, target := range []store.DocumentLock{ownedLock, targetLock} {
		if _, err := backend.FindDocumentLock(t.Context(), target.CollectionID, target.DocumentID, now); err != nil {
			t.Fatalf("lock did not survive rolled-back cleanup: %v", err)
		}
	}

	cleanup := mongoBegin(t, backend, false)
	if _, err := cleanup.Delete(t.Context(), store.Request{Collection: users, ID: "shared"}); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	if err := cleanup.DeleteDocumentState(t.Context(), reference); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	mongoCommit(t, cleanup)
	if _, err := backend.GetPreference(t.Context(), users.ID, "shared", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cleaned preference error = %v, want ErrNotFound", err)
	}
	for _, target := range []store.DocumentLock{ownedLock, targetLock} {
		if _, err := backend.FindDocumentLock(t.Context(), target.CollectionID, target.DocumentID, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("cleaned lock %#v error = %v, want ErrNotFound", target, err)
		}
	}
	if preference, err := backend.GetPreference(t.Context(), staff.ID, "shared", "theme"); err != nil || string(preference.Value) != `"staff"` {
		t.Fatalf("collection-collision preference = %#v, %v", preference, err)
	}
	if lock, err := backend.FindDocumentLock(t.Context(), staffLock.CollectionID, staffLock.DocumentID, now); err != nil || lock.OwnerCollectionID != staff.ID {
		t.Fatalf("collection-collision lock = %#v, %v", lock, err)
	}

	seedMongoSystemDocuments(t, backend, mongoSystemDocuments(users, "shared"))
	if _, err := backend.GetPreference(t.Context(), users.ID, "shared", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("same-ID recreation inherited preference: %v", err)
	}
	if _, err := backend.SetPreference(t.Context(), store.Preference{CollectionID: users.ID, UserID: "shared", Key: "theme", Value: json.RawMessage(`"new"`)}); err != nil {
		t.Fatalf("new incarnation preference: %v", err)
	}
}

func TestMongoDBDocumentLockFirstAcquireRacesReturnWinningOwnership(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, _, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 21, 0, 0, 111222333, time.UTC)
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "owner-a", "owner-b"),
		mongoSystemDocuments(posts, "raced-target", "same-owner-target"),
	)
	type result struct {
		lock     store.DocumentLock
		acquired bool
		err      error
	}
	runRace := func(documentID string, owners []string) []result {
		t.Helper()
		start := make(chan struct{})
		results := make(chan result, len(owners))
		for _, owner := range owners {
			owner := owner
			go func() {
				<-start
				candidate := mongoSystemLock(posts.ID, documentID, users.ID, owner, now)
				lock, acquired, err := backend.AcquireDocumentLock(t.Context(), candidate, now, false)
				results <- result{lock: lock, acquired: acquired, err: err}
			}()
		}
		close(start)
		collected := make([]result, 0, len(owners))
		for range owners {
			collected = append(collected, <-results)
		}
		return collected
	}

	differentOwners := runRace("raced-target", []string{"owner-a", "owner-b"})
	winner := ""
	for _, outcome := range differentOwners {
		if outcome.err != nil {
			t.Fatalf("different-owner first acquire race leaked error: %v", outcome.err)
		}
		if outcome.acquired {
			if winner != "" {
				t.Fatalf("different-owner race had multiple winners: %#v", differentOwners)
			}
			winner = outcome.lock.OwnerID
		}
	}
	if winner == "" {
		t.Fatalf("different-owner race had no winner: %#v", differentOwners)
	}
	for _, outcome := range differentOwners {
		if outcome.lock.OwnerID != winner {
			t.Fatalf("race result did not return current winner %q: %#v", winner, outcome)
		}
	}

	sameOwner := runRace("same-owner-target", []string{"owner-a", "owner-a"})
	for _, outcome := range sameOwner {
		if outcome.err != nil || !outcome.acquired || outcome.lock.OwnerID != "owner-a" {
			t.Fatalf("same-owner first acquire race = %#v", sameOwner)
		}
	}
}

func TestMongoDBDocumentLockTransactionsSerializeAcrossClients(t *testing.T) {
	backend := mongoIntegrationStore(t)
	peer := mongoSystemStorePeer(t, backend)
	users, _, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := peer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "owner-a", "owner-b", "owner-c"),
		mongoSystemDocuments(posts, "first", "refresh", "blocked", "takeover", "expired"),
	)
	base := time.Date(2026, time.August, 30, 23, 0, 0, 100, time.UTC)

	type lockCall struct {
		client    *Store
		candidate store.DocumentLock
		now       time.Time
		takeover  bool
	}
	type lockResult struct {
		call     lockCall
		lock     store.DocumentLock
		acquired bool
		err      error
	}
	run := func(calls []lockCall) []lockResult {
		t.Helper()
		start := make(chan struct{})
		results := make(chan lockResult, len(calls))
		for _, call := range calls {
			call := call
			go func() {
				<-start
				lock, acquired, err := call.client.AcquireDocumentLock(t.Context(), call.candidate, call.now, call.takeover)
				results <- lockResult{call: call, lock: lock, acquired: acquired, err: err}
			}()
		}
		close(start)
		collected := make([]lockResult, 0, len(calls))
		for range calls {
			collected = append(collected, <-results)
		}
		return collected
	}
	assertCallerFields := func(outcome lockResult) {
		t.Helper()
		if outcome.err != nil || !outcome.acquired {
			t.Fatalf("lock contention outcome = %#v", outcome)
		}
		if outcome.lock.OwnerCollectionID != outcome.call.candidate.OwnerCollectionID ||
			outcome.lock.OwnerID != outcome.call.candidate.OwnerID ||
			outcome.lock.OwnerLabel != outcome.call.candidate.OwnerLabel ||
			!outcome.lock.UpdatedAt.Equal(outcome.call.candidate.UpdatedAt) ||
			!outcome.lock.ExpiresAt.Equal(outcome.call.candidate.ExpiresAt) {
			t.Fatalf("lock contention returned another caller's mutation: got %#v want %#v", outcome.lock, outcome.call.candidate)
		}
	}

	t.Run("same-owner first acquire applies both callers", func(t *testing.T) {
		first := mongoSystemLock(posts.ID, "first", users.ID, "owner-a", base)
		first.OwnerLabel = "first-alpha"
		first.UpdatedAt = base.Add(time.Second)
		first.ExpiresAt = base.Add(time.Hour)
		second := first
		second.OwnerLabel = "first-beta"
		second.CreatedAt = base.Add(2 * time.Second)
		second.UpdatedAt = base.Add(3 * time.Second)
		second.ExpiresAt = base.Add(2 * time.Hour)
		outcomes := run([]lockCall{
			{client: backend, candidate: first, now: base},
			{client: peer, candidate: second, now: base},
		})
		for _, outcome := range outcomes {
			assertCallerFields(outcome)
		}
		if !outcomes[0].lock.CreatedAt.Equal(outcomes[1].lock.CreatedAt) {
			t.Fatalf("same-owner refresh did not preserve the winning creation time: %#v", outcomes)
		}
	})

	t.Run("existing same-owner refresh", func(t *testing.T) {
		seed := mongoSystemLock(posts.ID, "refresh", users.ID, "owner-a", base)
		if _, acquired, err := backend.AcquireDocumentLock(t.Context(), seed, base, false); err != nil || !acquired {
			t.Fatalf("seed refresh lock = %t, %v", acquired, err)
		}
		first := seed
		first.OwnerLabel = "refresh-alpha"
		first.UpdatedAt = base.Add(10 * time.Second)
		first.ExpiresAt = base.Add(2 * time.Hour)
		second := seed
		second.OwnerLabel = "refresh-beta"
		second.UpdatedAt = base.Add(20 * time.Second)
		second.ExpiresAt = base.Add(3 * time.Hour)
		outcomes := run([]lockCall{
			{client: backend, candidate: first, now: base.Add(30 * time.Second)},
			{client: peer, candidate: second, now: base.Add(30 * time.Second)},
		})
		for _, outcome := range outcomes {
			assertCallerFields(outcome)
			if !outcome.lock.CreatedAt.Equal(seed.CreatedAt) {
				t.Fatalf("refresh changed CreatedAt: got %v want %v", outcome.lock.CreatedAt, seed.CreatedAt)
			}
		}
	})

	t.Run("active different owner blocks", func(t *testing.T) {
		seed := mongoSystemLock(posts.ID, "blocked", users.ID, "owner-a", base)
		if _, acquired, err := backend.AcquireDocumentLock(t.Context(), seed, base, false); err != nil || !acquired {
			t.Fatalf("seed blocked lock = %t, %v", acquired, err)
		}
		first := mongoSystemLock(posts.ID, "blocked", users.ID, "owner-b", base.Add(time.Second))
		second := mongoSystemLock(posts.ID, "blocked", users.ID, "owner-c", base.Add(2*time.Second))
		outcomes := run([]lockCall{
			{client: backend, candidate: first, now: base.Add(3 * time.Second)},
			{client: peer, candidate: second, now: base.Add(3 * time.Second)},
		})
		for _, outcome := range outcomes {
			if outcome.err != nil || outcome.acquired || outcome.lock.OwnerID != seed.OwnerID || outcome.lock.OwnerLabel != seed.OwnerLabel {
				t.Fatalf("blocked contention outcome = %#v", outcome)
			}
		}
	})

	t.Run("active takeover applies both callers", func(t *testing.T) {
		seed := mongoSystemLock(posts.ID, "takeover", users.ID, "owner-a", base)
		if _, acquired, err := backend.AcquireDocumentLock(t.Context(), seed, base, false); err != nil || !acquired {
			t.Fatalf("seed takeover lock = %t, %v", acquired, err)
		}
		first := mongoSystemLock(posts.ID, "takeover", users.ID, "owner-b", base.Add(time.Second))
		first.OwnerLabel = "takeover-beta"
		second := mongoSystemLock(posts.ID, "takeover", users.ID, "owner-c", base.Add(2*time.Second))
		second.OwnerLabel = "takeover-gamma"
		outcomes := run([]lockCall{
			{client: backend, candidate: first, now: base.Add(3 * time.Second), takeover: true},
			{client: peer, candidate: second, now: base.Add(3 * time.Second), takeover: true},
		})
		for _, outcome := range outcomes {
			assertCallerFields(outcome)
			if !outcome.lock.CreatedAt.Equal(outcome.call.candidate.CreatedAt) {
				t.Fatalf("takeover did not apply caller CreatedAt: got %v want %v", outcome.lock.CreatedAt, outcome.call.candidate.CreatedAt)
			}
		}
	})

	t.Run("expired replacement has one current owner", func(t *testing.T) {
		seed := mongoSystemLock(posts.ID, "expired", users.ID, "owner-a", base)
		seed.ExpiresAt = base.Add(time.Second)
		if _, acquired, err := backend.AcquireDocumentLock(t.Context(), seed, base, false); err != nil || !acquired {
			t.Fatalf("seed expired lock = %t, %v", acquired, err)
		}
		first := mongoSystemLock(posts.ID, "expired", users.ID, "owner-b", base.Add(2*time.Second))
		first.OwnerLabel = "expired-beta"
		second := mongoSystemLock(posts.ID, "expired", users.ID, "owner-c", base.Add(3*time.Second))
		second.OwnerLabel = "expired-gamma"
		outcomes := run([]lockCall{
			{client: backend, candidate: first, now: base.Add(4 * time.Second)},
			{client: peer, candidate: second, now: base.Add(4 * time.Second)},
		})
		winner := ""
		for _, outcome := range outcomes {
			if outcome.err != nil {
				t.Fatalf("expired contention leaked error: %v", outcome.err)
			}
			if outcome.acquired {
				assertCallerFields(outcome)
				if winner != "" {
					t.Fatalf("expired contention had two winners: %#v", outcomes)
				}
				winner = outcome.lock.OwnerID
			}
		}
		if winner == "" {
			t.Fatalf("expired contention had no winner: %#v", outcomes)
		}
		for _, outcome := range outcomes {
			if outcome.lock.OwnerID != winner {
				t.Fatalf("expired contention did not return current winner %q: %#v", winner, outcome)
			}
		}
	})
}

func TestMongoDBSystemTransactionContentionHonorsCallerCancellation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	peer := mongoSystemStorePeer(t, backend)
	users, _, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := peer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "cancel-owner"),
		mongoSystemDocuments(posts, "cancel-target"),
	)
	blocker := mongoBegin(t, backend, false)
	if _, err := blocker.Find(t.Context(), store.Request{Collection: posts, ID: "cancel-target", Lock: store.LockMutation}); err != nil {
		mongoRollback(t, blocker)
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	now := time.Date(2026, time.August, 30, 23, 30, 0, 0, time.UTC)
	go func() {
		_, _, err := peer.AcquireDocumentLock(ctx, mongoSystemLock(posts.ID, "cancel-target", users.ID, "cancel-owner", now), now, false)
		result <- err
	}()
	select {
	case err := <-result:
		mongoRollback(t, blocker)
		t.Fatalf("contended lock returned before cancellation: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			mongoRollback(t, blocker)
			t.Fatalf("canceled contended lock error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		mongoRollback(t, blocker)
		t.Fatal("canceled contended lock did not terminate")
	}
	mongoRollback(t, blocker)
}

func TestMongoDBSystemWritesConflictWithStagedSameIDRecreation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	peer := mongoSystemStorePeer(t, backend)
	users, _, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := peer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 22, 0, 0, 444555666, time.UTC)
	backend.now = func() time.Time { return now }
	peer.now = func() time.Time { return now }
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "preference-owner", "lock-owner"),
		mongoSystemDocuments(posts, "lock-target"),
	)

	t.Run("preference", func(t *testing.T) {
		candidate := store.Preference{
			CollectionID: users.ID, UserID: "preference-owner", Key: "theme", Value: json.RawMessage(`"stale"`),
		}
		references, err := peer.captureActiveDocumentReferences(t.Context(), store.DocumentReference{
			CollectionID: users.ID, DocumentID: "preference-owner",
		})
		if err != nil {
			t.Fatal(err)
		}
		recreation := stageMongoSystemRecreation(t, backend, users, "preference-owner")
		result := make(chan error, 1)
		go func() {
			result <- peer.runSystemTransaction(t.Context(), func(transaction *documentTransaction) error {
				_, operationErr := transaction.setPreference(t.Context(), candidate, references)
				return operationErr
			})
		}()
		select {
		case err := <-result:
			mongoRollback(t, recreation)
			t.Fatalf("preference did not serialize behind staged recreation: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		mongoCommit(t, recreation)
		err = <-result
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("preference racing staged recreation error = %v, want ErrConflict", err)
		}
		if _, err := backend.GetPreference(t.Context(), users.ID, "preference-owner", "theme"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("recreated owner inherited racing preference: %v", err)
		}
	})

	t.Run("document lock", func(t *testing.T) {
		candidate := mongoSystemLock(posts.ID, "lock-target", users.ID, "lock-owner", now)
		references, err := peer.captureActiveDocumentReferences(
			t.Context(),
			store.DocumentReference{CollectionID: posts.ID, DocumentID: "lock-target"},
			store.DocumentReference{CollectionID: users.ID, DocumentID: "lock-owner"},
		)
		if err != nil {
			t.Fatal(err)
		}
		recreation := stageMongoSystemRecreation(t, backend, users, "lock-owner")
		result := make(chan error, 1)
		go func() {
			result <- peer.runSystemTransaction(t.Context(), func(transaction *documentTransaction) error {
				_, _, operationErr := transaction.acquireDocumentLock(t.Context(), candidate, now, false, references)
				return operationErr
			})
		}()
		select {
		case err := <-result:
			mongoRollback(t, recreation)
			t.Fatalf("document lock did not serialize behind staged recreation: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		mongoCommit(t, recreation)
		err = <-result
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("lock racing staged recreation error = %v, want ErrConflict", err)
		}
		if _, err := backend.FindDocumentLock(t.Context(), posts.ID, "lock-target", now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("recreated owner inherited racing lock: %v", err)
		}
	})
}

func mongoSystemStoreManifest() (schema.Collection, schema.Collection, schema.Collection, schema.Manifest) {
	users := mongoSystemTestCollection("system-users", "system-users", true, false)
	staff := mongoSystemTestCollection("system-staff", "system-staff", false, false)
	posts := mongoSystemTestCollection("system-posts", "system-posts", true, true)
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "MongoDB system stores"},
		Collections: []schema.Collection{users, staff, posts}, Plugins: []schema.Plugin{},
	})
	return users, staff, posts, manifest
}

func mongoSystemTestCollection(id schema.StableID, slug schema.CollectionSlug, trash, locking bool) schema.Collection {
	name, _ := query.NewPath("name")
	collection := schema.Collection{
		ID: id, Slug: slug, Labels: schema.CollectionLabels{Singular: string(slug), Plural: string(slug)},
		Capabilities: schema.Capabilities{Trash: trash, Locking: locking},
		Fields: []schema.Field{{
			ID: id + "-name", Name: "name", Path: name, Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
		}},
	}
	if locking {
		collection.DocumentLock = &schema.DocumentLockSettings{DurationSeconds: 300}
	}
	return collection
}

type mongoSystemDocumentFixture struct {
	collection schema.Collection
	ids        []string
}

func mongoSystemDocuments(collection schema.Collection, ids ...string) mongoSystemDocumentFixture {
	return mongoSystemDocumentFixture{collection: collection, ids: ids}
}

func seedMongoSystemDocuments(t *testing.T, backend *Store, fixtures ...mongoSystemDocumentFixture) {
	t.Helper()
	transaction := mongoBegin(t, backend, false)
	for _, fixture := range fixtures {
		for _, id := range fixture.ids {
			if _, err := transaction.Create(t.Context(), store.CreateRequest{
				Collection: fixture.collection, ID: id, Values: store.Values{"name": store.String(id)},
			}); err != nil {
				mongoRollback(t, transaction)
				t.Fatalf("create MongoDB system-store fixture %s/%s: %v", fixture.collection.ID, id, err)
			}
		}
	}
	mongoCommit(t, transaction)
}

func trashMongoSystemDocument(t *testing.T, backend *Store, collection schema.Collection, id string) {
	t.Helper()
	transaction := mongoBegin(t, backend, false)
	if _, err := transaction.Trash(t.Context(), store.Request{Collection: collection, ID: id}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	mongoCommit(t, transaction)
}

func mongoSystemLock(
	collectionID schema.StableID,
	documentID string,
	ownerCollectionID schema.StableID,
	ownerID string,
	now time.Time,
) store.DocumentLock {
	return store.DocumentLock{
		CollectionID: collectionID, DocumentID: documentID,
		OwnerCollectionID: ownerCollectionID, OwnerID: ownerID, OwnerLabel: ownerID,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
}

func stageMongoSystemRecreation(t *testing.T, backend *Store, collection schema.Collection, id string) store.Transaction {
	t.Helper()
	transaction := mongoBegin(t, backend, false)
	if _, err := transaction.Delete(t.Context(), store.Request{Collection: collection, ID: id}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if err := transaction.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: collection.ID, DocumentID: id}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transaction.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: id, Values: store.Values{"name": store.String("recreated-" + id)},
	}); err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	return transaction
}

func mongoSystemStorePeer(t *testing.T, backend *Store) *Store {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	parsed.Path = "/" + backend.database.Name()
	peer, err := OpenWithConfig(t.Context(), Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("open MongoDB system-store peer: %v", err)
	}
	t.Cleanup(func() {
		if err := peer.Close(); err != nil {
			t.Errorf("close MongoDB system-store peer: %v", err)
		}
	})
	return peer
}
