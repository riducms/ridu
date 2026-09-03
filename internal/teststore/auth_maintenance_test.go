package teststore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAuthMaintenanceIsBoundedAndSafeForConcurrentWorkers(t *testing.T) {
	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	backend := New()
	backend.now = func() time.Time { return now }
	collectionID := schema.StableID("users")
	for index := 0; index < 6; index++ {
		expiresAt := now.Add(time.Duration(index-6) * time.Minute)
		tokenHash := "expired-session-" + string(rune('a'+index))
		backend.sessions[tokenHash] = session{record: store.AuthSession{
			ID: tokenHash, TokenHash: tokenHash, CollectionID: collectionID,
			UserID: "owner", ExpiresAt: expiresAt, CreatedAt: expiresAt.Add(-time.Hour),
		}}
		id := "expired-key-" + string(rune('a'+index))
		backend.apiKeys[id] = store.AuthAPIKey{
			ID: id, CollectionID: collectionID, UserID: "owner",
			ExpiresAt: expiresAt, CreatedAt: expiresAt.Add(-time.Hour),
		}
	}
	backend.sessions["active-session"] = session{record: store.AuthSession{
		ID: "active-session", TokenHash: "active-session", CollectionID: collectionID,
		UserID: "owner", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}}
	backend.apiKeys["active-key"] = store.AuthAPIKey{
		ID: "active-key", CollectionID: collectionID, UserID: "owner",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	backend.apiKeys["unbounded-key"] = store.AuthAPIKey{
		ID: "unbounded-key", CollectionID: collectionID, UserID: "owner", CreatedAt: now,
	}

	first, err := backend.PruneExpiredAuth(context.Background(), 2)
	if err != nil || first.Sessions != 2 || first.APIKeys != 2 || first.Total() != 4 {
		t.Fatalf("first bounded prune = %#v, %v", first, err)
	}
	if _, exists := backend.sessions["expired-session-a"]; exists {
		t.Fatal("oldest expired session survived the ordered batch")
	}
	if _, exists := backend.sessions["expired-session-c"]; !exists {
		t.Fatal("session prune exceeded its batch")
	}
	if _, exists := backend.apiKeys["expired-key-a"]; exists {
		t.Fatal("oldest expired API key survived the ordered batch")
	}
	if _, exists := backend.apiKeys["expired-key-c"]; !exists {
		t.Fatal("API-key prune exceeded its batch")
	}

	start := make(chan struct{})
	results := make(chan store.AuthPruneResult, 8)
	errorsFound := make(chan error, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, pruneError := backend.PruneExpiredAuth(context.Background(), 1)
			results <- result
			errorsFound <- pruneError
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsFound)
	for pruneError := range errorsFound {
		if pruneError != nil {
			t.Fatal(pruneError)
		}
	}
	var concurrent store.AuthPruneResult
	for result := range results {
		if result.Sessions > 1 || result.APIKeys > 1 {
			t.Fatalf("concurrent worker exceeded its per-family batch: %#v", result)
		}
		concurrent.Sessions += result.Sessions
		concurrent.APIKeys += result.APIKeys
	}
	if concurrent.Sessions != 4 || concurrent.APIKeys != 4 {
		t.Fatalf("concurrent workers double-counted or missed expired rows: %#v", concurrent)
	}
	if final, err := backend.PruneExpiredAuth(context.Background(), store.MaxAuthPruneBatch); err != nil || final.Total() != 0 {
		t.Fatalf("fully drained prune = %#v, %v", final, err)
	}
	if _, err := backend.FindSession(context.Background(), "active-session", now); err != nil {
		t.Fatalf("active session was pruned: %v", err)
	}
	if _, err := backend.FindAPIKey(context.Background(), "active-key", now); err != nil {
		t.Fatalf("active API key was pruned: %v", err)
	}
	if _, err := backend.FindAPIKey(context.Background(), "unbounded-key", now); err != nil {
		t.Fatalf("non-expiring API key was pruned: %v", err)
	}
	if _, err := backend.PruneExpiredAuth(context.Background(), 0); err == nil {
		t.Fatal("zero auth prune batch was accepted")
	}
	if _, err := backend.PruneExpiredAuth(context.Background(), store.MaxAuthPruneBatch+1); err == nil {
		t.Fatal("oversized auth prune batch was accepted")
	}
}
