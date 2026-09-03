package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLiteRejectsUnrepresentablePersistedTimestamps(t *testing.T) {
	ctx := context.Background()
	valid := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	invalid := time.Date(2500, time.January, 1, 0, 0, 0, 0, time.UTC)
	want := "outside SQLite's nanosecond timestamp range"

	t.Run("document import metadata", func(t *testing.T) {
		backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion}))
		transaction, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(ctx)
		_, err = transaction.Create(ctx, store.CreateRequest{
			Collection: schema.Collection{ID: "posts"}, ID: "future", Values: store.Values{},
			CreatedAt: invalid, UpdatedAt: invalid,
		})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Create() error = %v, want timestamp range rejection", err)
		}
	})

	t.Run("authentication session", func(t *testing.T) {
		backend := newSQLiteAuthStore(t)
		err := backend.CreateSession(ctx, store.AuthSession{
			ID: "session", TokenHash: "token", CollectionID: "users", UserID: "user",
			ExpiresAt: invalid, CreatedAt: valid, LastSeenAt: valid,
		}, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSession() error = %v, want timestamp range rejection", err)
		}
	})

	t.Run("document lock", func(t *testing.T) {
		backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion}))
		_, _, err := backend.AcquireDocumentLock(ctx, store.DocumentLock{
			CollectionID: "posts", DocumentID: "post", OwnerCollectionID: "users", OwnerID: "user",
			CreatedAt: valid, UpdatedAt: valid, ExpiresAt: invalid,
		}, valid, false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("AcquireDocumentLock() error = %v, want timestamp range rejection", err)
		}
	})

	t.Run("derived task lease expiry", func(t *testing.T) {
		backend, clock := newSQLiteTaskBackend(t)
		*clock = time.Date(2262, time.April, 11, 23, 0, 0, 0, time.UTC)
		if _, err := backend.EnqueueTask(ctx, sqliteTaskFixture(*clock, "near-boundary", "")); err != nil {
			t.Fatal(err)
		}
		_, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: 2 * time.Hour})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("ClaimTasks() error = %v, want timestamp range rejection", err)
		}
	})

	t.Run("derived terminal retention", func(t *testing.T) {
		nearBoundary := time.Date(2262, time.April, 11, 23, 0, 0, 0, time.UTC)

		t.Run("cancel", func(t *testing.T) {
			backend, clock := newSQLiteTaskBackend(t)
			*clock = nearBoundary
			queued, err := backend.EnqueueTask(ctx, sqliteTaskFixture(*clock, "cancel-retention-boundary", ""))
			if err != nil {
				t.Fatal(err)
			}
			if err := backend.CancelTask(ctx, queued.ID); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CancelTask() error = %v, want timestamp range rejection", err)
			}
			stored, err := backend.FindTask(ctx, queued.ID)
			if err != nil || stored.State != store.TaskStateQueued || stored.RetainUntil != nil {
				t.Fatalf("task after rejected cancellation = %#v, %v", stored, err)
			}
		})

		t.Run("complete", func(t *testing.T) {
			backend, clock := newSQLiteTaskBackend(t)
			*clock = nearBoundary
			queued, err := backend.EnqueueTask(ctx, sqliteTaskFixture(*clock, "complete-retention-boundary", ""))
			if err != nil {
				t.Fatal(err)
			}
			claimed := claimSQLiteTask(t, backend, store.TaskClaim{Limit: 1, Slugs: []string{queued.Slug}, LeaseDuration: time.Minute})
			if err := backend.CompleteTask(ctx, claimed.ID, claimed.LeaseToken, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("CompleteTask() error = %v, want timestamp range rejection", err)
			}
			stored, err := backend.FindTask(ctx, queued.ID)
			if err != nil || stored.State != store.TaskStateRunning || stored.RetainUntil != nil {
				t.Fatalf("task after rejected completion = %#v, %v", stored, err)
			}
		})

		t.Run("fail", func(t *testing.T) {
			backend, clock := newSQLiteTaskBackend(t)
			*clock = nearBoundary
			queued, err := backend.EnqueueTask(ctx, sqliteTaskFixture(*clock, "fail-retention-boundary", ""))
			if err != nil {
				t.Fatal(err)
			}
			claimed := claimSQLiteTask(t, backend, store.TaskClaim{Limit: 1, Slugs: []string{queued.Slug}, LeaseDuration: time.Minute})
			err = backend.FailTask(ctx, store.TaskFailure{
				ID: claimed.ID, LeaseToken: claimed.LeaseToken, Code: "fatal", Message: "failed",
			})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("FailTask() error = %v, want timestamp range rejection", err)
			}
			stored, err := backend.FindTask(ctx, queued.ID)
			if err != nil || stored.State != store.TaskStateRunning || stored.RetainUntil != nil {
				t.Fatalf("task after rejected failure = %#v, %v", stored, err)
			}
		})
	})
}
