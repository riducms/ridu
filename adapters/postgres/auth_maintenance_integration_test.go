package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestPostgresExpiredAuthMaintenanceIsBoundedAndConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config := ridu.Config{
		Name: "PostgreSQL auth retention", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			AuthConfig: ridu.AuthConfig{
				Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost},
				APIKeys:  true,
			},
			Fields: field.Fields{field.Email("email").Required().Unique()},
		}},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.CreateAuthUser(ctx, "users", store.Values{"email": store.String("retention@example.test")}, "correct-horse", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(ctx, "users", "retention@example.test", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	credential, err := backend.FindAuthCredential(ctx, collection, "retention@example.test")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for index := 0; index < 6; index++ {
		expiresAt := now.Add(-time.Duration(index+1) * time.Minute)
		id := fmt.Sprintf("%032x", index+1)
		if err := backend.CreateSession(ctx, store.AuthSession{
			ID: id, TokenHash: "expired-session-" + id, CollectionID: collection.ID,
			UserID: user.ID, ExpiresAt: expiresAt, CreatedAt: expiresAt.Add(-time.Hour), LastSeenAt: expiresAt,
		}, credential.PasswordHash); err != nil {
			t.Fatal(err)
		}
		if err := backend.CreateAPIKey(ctx, store.AuthAPIKey{
			ID: "expired-key-" + id, TokenHash: "expired-key-hash-" + id,
			CollectionID: collection.ID, UserID: user.ID, Name: "expired",
			CreatedAt: expiresAt.Add(-time.Hour), ExpiresAt: expiresAt,
		}, authMaintenanceTokenDigest(session.Token), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := backend.CreateAPIKey(ctx, store.AuthAPIKey{
		ID: "active-key", TokenHash: "active-key-hash", CollectionID: collection.ID,
		UserID: user.ID, Name: "active", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, authMaintenanceTokenDigest(session.Token), now); err != nil {
		t.Fatal(err)
	}
	if err := backend.CreateAPIKey(ctx, store.AuthAPIKey{
		ID: "unbounded-key", TokenHash: "unbounded-key-hash", CollectionID: collection.ID,
		UserID: user.ID, Name: "unbounded", CreatedAt: now,
	}, authMaintenanceTokenDigest(session.Token), now); err != nil {
		t.Fatal(err)
	}

	first, err := application.PruneExpiredAuth(ctx, 2)
	if err != nil || first.Sessions != 2 || first.APIKeys != 2 {
		t.Fatalf("first PostgreSQL auth prune = %#v, %v", first, err)
	}
	start := make(chan struct{})
	results := make(chan store.AuthPruneResult, 8)
	errorsFound := make(chan error, 10)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, pruneError := application.PruneExpiredAuth(ctx, 1)
			results <- result
			errorsFound <- pruneError
		}()
	}
	rotated := make(chan ridu.AuthSession, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		replacement, rotateError := application.RotateSession(ctx, session.Token)
		rotated <- replacement
		errorsFound <- rotateError
	}()
	loggedIn := make(chan ridu.AuthSession, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		created, loginError := application.Login(ctx, "users", "retention@example.test", "correct-horse")
		loggedIn <- created
		errorsFound <- loginError
	}()
	close(start)
	workers.Wait()
	close(results)
	close(errorsFound)
	for operationError := range errorsFound {
		if operationError != nil {
			t.Fatal(operationError)
		}
	}
	var concurrent store.AuthPruneResult
	for result := range results {
		if result.Sessions > 1 || result.APIKeys > 1 {
			t.Fatalf("PostgreSQL worker exceeded its per-family batch: %#v", result)
		}
		concurrent.Sessions += result.Sessions
		concurrent.APIKeys += result.APIKeys
	}
	if concurrent.Sessions != 4 || concurrent.APIKeys != 4 {
		t.Fatalf("PostgreSQL workers double-counted or missed expired rows: %#v", concurrent)
	}
	if final, err := application.PruneExpiredAuth(ctx, store.MaxAuthPruneBatch); err != nil || final.Total() != 0 {
		t.Fatalf("fully drained PostgreSQL auth prune = %#v, %v", final, err)
	}
	for name, active := range map[string]ridu.AuthSession{"rotated": <-rotated, "concurrent login": <-loggedIn} {
		if active.Token == "" {
			t.Fatalf("%s returned an empty active session", name)
		}
		if _, err := application.Session(ctx, active.Token); err != nil {
			t.Fatalf("%s session was pruned: %v", name, err)
		}
	}
	if _, err := backend.FindAPIKey(ctx, "active-key", time.Now().UTC()); err != nil {
		t.Fatalf("active API key was pruned: %v", err)
	}
	if _, err := backend.FindAPIKey(ctx, "unbounded-key", time.Now().UTC()); err != nil {
		t.Fatalf("non-expiring API key was pruned: %v", err)
	}
}

func authMaintenanceTokenDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
