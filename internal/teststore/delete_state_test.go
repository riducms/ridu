package teststore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestDeleteDocumentStateIsAtomicAndCollectionScoped(t *testing.T) {
	backend := New()
	now := time.Now().UTC()
	target := store.DocumentReference{CollectionID: "users", DocumentID: "shared"}
	collision := store.DocumentReference{CollectionID: "staff", DocumentID: "shared"}
	seedDocumentState(backend, target, now)
	seedDocumentState(backend, collision, now)

	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Delete(context.Background(), store.Request{
		Collection: schema.Collection{ID: target.CollectionID}, ID: target.DocumentID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.DeleteDocumentState(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDocumentState(t, backend, target, true)
	assertDocumentState(t, backend, collision, true)

	transaction, err = backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Delete(context.Background(), store.Request{
		Collection: schema.Collection{ID: target.CollectionID}, ID: target.DocumentID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.DeleteDocumentState(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDocumentState(t, backend, target, false)
	assertDocumentState(t, backend, collision, true)
}

func TestAuxiliaryWritesRequireActiveDocumentReferences(t *testing.T) {
	backend := New()
	now := time.Now().UTC()
	owner := store.DocumentReference{CollectionID: "users", DocumentID: "user-1"}
	target := store.DocumentReference{CollectionID: "posts", DocumentID: "post-1"}
	seedDocumentState(backend, owner, now)
	seedDocumentState(backend, target, now)
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Delete(context.Background(), store.Request{Collection: schema.Collection{ID: owner.CollectionID}, ID: owner.DocumentID}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.DeleteDocumentState(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	assertNotFound := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s error = %v, want store.ErrNotFound", name, err)
		}
	}
	assertNotFound("session", backend.CreateSession(context.Background(), store.AuthSession{CollectionID: owner.CollectionID, UserID: owner.DocumentID, TokenHash: "late"}, []byte("missing-hash")))
	backend.sessions["orphan"] = session{record: store.AuthSession{CollectionID: owner.CollectionID, UserID: owner.DocumentID, TokenHash: "orphan", ExpiresAt: now.Add(time.Hour)}}
	assertNotFound("session rotation", backend.RotateSession(context.Background(), "orphan", store.AuthSession{CollectionID: owner.CollectionID, UserID: owner.DocumentID, TokenHash: "rotated"}, now))
	assertNotFound("auth token", backend.CreateAuthToken(context.Background(), store.AuthToken{CollectionID: owner.CollectionID, UserID: owner.DocumentID, TokenHash: "late"}))
	assertNotFound("API key", backend.CreateAPIKey(context.Background(), store.AuthAPIKey{CollectionID: owner.CollectionID, UserID: owner.DocumentID, ID: "late"}, "missing-session", time.Now()))
	_, err = backend.SetPreference(context.Background(), store.Preference{CollectionID: owner.CollectionID, UserID: owner.DocumentID, Key: "late", Value: json.RawMessage(`true`)})
	assertNotFound("preference", err)
	_, err = backend.EnqueueTask(context.Background(), store.Task{
		Slug: "late-requester", Queue: "default", Input: json.RawMessage(`{}`), RunAt: now.Add(time.Hour),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute, Backoff: store.TaskBackoffFixed,
		Timeout: time.Minute, Retention: time.Hour,
		Target:      &store.DocumentReference{CollectionID: target.CollectionID, DocumentID: target.DocumentID},
		RequestedBy: &store.DocumentReference{CollectionID: owner.CollectionID, DocumentID: owner.DocumentID},
	})
	assertNotFound("requester task", err)
	_, _, err = backend.AcquireDocumentLock(context.Background(), store.DocumentLock{
		CollectionID: target.CollectionID, DocumentID: target.DocumentID,
		OwnerCollectionID: owner.CollectionID, OwnerID: owner.DocumentID, ExpiresAt: now.Add(time.Hour),
	}, now, false)
	assertNotFound("owner lock", err)
	_, _, err = backend.AcquireDocumentLock(context.Background(), store.DocumentLock{
		CollectionID: target.CollectionID, DocumentID: target.DocumentID, OwnerCollectionID: owner.CollectionID, ExpiresAt: now.Add(time.Hour),
	}, now, false)
	assertNotFound("partial lock owner", err)
}

func TestDeleteDocumentStateRespectsTransactionalCredentialOrder(t *testing.T) {
	backend := New()
	collection := schema.Collection{ID: "users"}
	reference := store.DocumentReference{CollectionID: collection.ID, DocumentID: "user-1"}

	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(context.Background(), store.CreateRequest{Collection: collection, ID: reference.DocumentID}); err != nil {
		t.Fatal(err)
	}
	authTransaction := transaction.(store.AuthTransaction)
	if err := authTransaction.CreateAuthCredential(context.Background(), collection, reference.DocumentID, []byte("old"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Delete(context.Background(), store.Request{Collection: collection, ID: reference.DocumentID}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.DeleteDocumentState(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(backend.passwords["users:user-1"]) != 0 {
		t.Fatal("credential staged before cleanup survived the hard delete")
	}

	backend.documents["users"] = map[string]store.Document{"user-1": {ID: "user-1"}}
	backend.passwords["users:user-1"] = []byte("pre-existing")
	backend.credentials["users:user-1"] = store.AuthCredential{Verified: true}
	transaction, err = backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Delete(context.Background(), store.Request{Collection: collection, ID: reference.DocumentID}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.DeleteDocumentState(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(context.Background(), store.CreateRequest{Collection: collection, ID: reference.DocumentID}); err != nil {
		t.Fatal(err)
	}
	authTransaction = transaction.(store.AuthTransaction)
	if err := authTransaction.CreateAuthCredential(context.Background(), collection, reference.DocumentID, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := string(backend.passwords["users:user-1"]); got != "new" {
		t.Fatalf("credential staged after cleanup = %q, want new", got)
	}
}

func seedDocumentState(backend *Store, reference store.DocumentReference, now time.Time) {
	collection := string(reference.CollectionID)
	backend.documents[collection] = map[string]store.Document{
		reference.DocumentID: {ID: reference.DocumentID, Values: store.Values{"email": store.String(collection + "@example.test")}},
	}
	backend.versions[collection] = map[string][]store.Version{
		reference.DocumentID: {{DocumentID: reference.DocumentID, Revision: 1}},
	}
	authKey := collection + ":" + reference.DocumentID
	backend.passwords[authKey] = []byte("hash")
	backend.credentials[authKey] = store.AuthCredential{Verified: true}
	backend.sessions["session-"+collection] = session{record: store.AuthSession{
		ID: "session-" + collection, TokenHash: "session-" + collection,
		CollectionID: reference.CollectionID, UserID: reference.DocumentID, ExpiresAt: now.Add(time.Hour),
	}}
	backend.authTokens["token-"+collection] = store.AuthToken{
		TokenHash: "token-" + collection, CollectionID: reference.CollectionID, UserID: reference.DocumentID,
	}
	backend.apiKeys["key-"+collection] = store.AuthAPIKey{
		ID: "key-" + collection, CollectionID: reference.CollectionID, UserID: reference.DocumentID,
	}
	backend.preferences[preferenceKey(reference.CollectionID, reference.DocumentID, "theme")] = store.Preference{
		CollectionID: reference.CollectionID, UserID: reference.DocumentID, Key: "theme", Value: json.RawMessage(`"dark"`),
	}
	backend.documentLocks[documentLockKey(reference.CollectionID, reference.DocumentID)] = store.DocumentLock{
		CollectionID: reference.CollectionID, DocumentID: reference.DocumentID,
		OwnerCollectionID: "owners", OwnerID: "owner-" + collection,
	}
	backend.documentLocks[documentLockKey("posts", "owned-"+collection)] = store.DocumentLock{
		CollectionID: "posts", DocumentID: "owned-" + collection,
		OwnerCollectionID: reference.CollectionID, OwnerID: reference.DocumentID,
	}
}

func assertDocumentState(t *testing.T, backend *Store, reference store.DocumentReference, exists bool) {
	t.Helper()
	collection := string(reference.CollectionID)
	authKey := collection + ":" + reference.DocumentID
	checks := map[string]bool{
		"document":    backend.documents[collection][reference.DocumentID].ID != "",
		"version":     len(backend.versions[collection][reference.DocumentID]) != 0,
		"password":    len(backend.passwords[authKey]) != 0,
		"credential":  backend.credentials[authKey].Verified,
		"session":     backend.sessions["session-"+collection].record.ID != "",
		"token":       backend.authTokens["token-"+collection].TokenHash != "",
		"api key":     backend.apiKeys["key-"+collection].ID != "",
		"preference":  len(backend.preferences[preferenceKey(reference.CollectionID, reference.DocumentID, "theme")].Value) != 0,
		"target lock": backend.documentLocks[documentLockKey(reference.CollectionID, reference.DocumentID)].DocumentID != "",
		"owner lock":  backend.documentLocks[documentLockKey("posts", "owned-"+collection)].DocumentID != "",
	}
	for name, actual := range checks {
		if actual != exists {
			t.Errorf("%s exists = %t, want %t", name, actual, exists)
		}
	}
}
