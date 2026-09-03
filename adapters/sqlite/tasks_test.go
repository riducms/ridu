package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLiteTasksFenceLeasesAndUseTheStoreClock(t *testing.T) {
	backend, clock := newSQLiteTaskBackend(t)
	ctx := context.Background()
	poisoned := sqliteTaskFixture(*clock, "sqlite-lifecycle", "")
	poisoned.State = store.TaskStateSucceeded
	poisoned.Attempts = 99
	poisoned.Output = json.RawMessage(`{"forged":true}`)
	poisoned.LeaseToken = "forged"
	poisoned.LastErrorCode = "forged"
	poisoned.LastError = "forged"
	queued, err := backend.EnqueueTask(ctx, poisoned)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != store.TaskStateQueued || queued.Attempts != 0 || len(queued.Output) != 0 || queued.LeaseToken != "" || queued.LastError != "" || queued.CompletedAt != nil || queued.RetainUntil != nil {
		t.Fatalf("caller lifecycle leaked through admission: %#v", queued)
	}
	if !queued.CreatedAt.Equal(*clock) || !queued.UpdatedAt.Equal(*clock) {
		t.Fatalf("enqueue clock = %v, %v; want %v", queued.CreatedAt, queued.UpdatedAt, *clock)
	}

	first := claimSQLiteTask(t, backend, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if first.ID != queued.ID || first.Attempts != 1 || first.LeaseToken == "" || first.LeaseExpiresAt == nil || !first.LeaseExpiresAt.Equal(clock.Add(time.Minute)) {
		t.Fatalf("first claim = %#v", first)
	}
	*clock = clock.Add(30 * time.Second)
	if err := backend.HeartbeatTask(ctx, first.ID, first.LeaseToken, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := backend.FindTask(ctx, first.ID)
	if err != nil || heartbeat.LeaseExpiresAt == nil || !heartbeat.LeaseExpiresAt.Equal(clock.Add(2*time.Minute)) || !heartbeat.UpdatedAt.Equal(*clock) {
		t.Fatalf("heartbeat = %#v, %v", heartbeat, err)
	}
	*clock = clock.Add(2*time.Minute + time.Nanosecond)
	if err := backend.HeartbeatTask(ctx, first.ID, first.LeaseToken, time.Minute); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("expired heartbeat = %v", err)
	}
	if err := backend.CompleteTask(ctx, first.ID, first.LeaseToken, json.RawMessage(`{"stale":true}`)); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("expired completion = %v", err)
	}

	second := claimSQLiteTask(t, backend, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if second.ID != first.ID || second.Attempts != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("reclaimed task = %#v", second)
	}
	if err := backend.ReleaseTask(ctx, second.ID, second.LeaseToken, 5*time.Second, "worker_shutdown", "shutdown"); err != nil {
		t.Fatal(err)
	}
	released, err := backend.FindTask(ctx, second.ID)
	if err != nil || released.State != store.TaskStateQueued || !released.RunAt.Equal(clock.Add(5*time.Second)) || released.LastErrorCode != "worker_shutdown" {
		t.Fatalf("released task = %#v, %v", released, err)
	}
	if claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute}); err != nil || len(claimed) != 0 {
		t.Fatalf("early release claim = %#v, %v", claimed, err)
	}
	*clock = clock.Add(6 * time.Second)
	third := claimSQLiteTask(t, backend, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	retryAfter := 10 * time.Second
	if err := backend.FailTask(ctx, store.TaskFailure{
		ID: third.ID, LeaseToken: third.LeaseToken, Code: "temporary", Message: "try again", RetryAfter: &retryAfter,
	}); err != nil {
		t.Fatal(err)
	}
	retrying, err := backend.FindTask(ctx, third.ID)
	if err != nil || retrying.State != store.TaskStateQueued || !retrying.RunAt.Equal(clock.Add(retryAfter)) || retrying.CompletedAt != nil || retrying.RetainUntil != nil {
		t.Fatalf("retrying task = %#v, %v", retrying, err)
	}
	*clock = clock.Add(retryAfter + time.Nanosecond)
	fourth := claimSQLiteTask(t, backend, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	output := json.RawMessage(`{"ok":true}`)
	if err := backend.CompleteTask(ctx, fourth.ID, fourth.LeaseToken, output); err != nil {
		t.Fatal(err)
	}
	completed, err := backend.FindTask(ctx, fourth.ID)
	if err != nil || completed.State != store.TaskStateSucceeded || string(completed.Output) != string(output) || completed.CompletedAt == nil || !completed.CompletedAt.Equal(*clock) || completed.RetainUntil == nil || !completed.RetainUntil.Equal(clock.Add(time.Hour)) || completed.LastError != "" {
		t.Fatalf("completed task = %#v, %v", completed, err)
	}
	if err := backend.CancelTask(ctx, completed.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("terminal cancel = %v", err)
	}
	*clock = clock.Add(time.Hour)
	if pruned, err := backend.PruneTasks(ctx, 1); err != nil || pruned != 1 {
		t.Fatalf("prune = %d, %v", pruned, err)
	}
	if _, err := backend.FindTask(ctx, completed.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pruned task lookup = %v", err)
	}
}

func TestSQLiteTasksClaimConcurrencyKeysAtomicallyAndDismissTargets(t *testing.T) {
	backend, clock := newSQLiteTaskBackend(t)
	ctx := context.Background()
	first, err := backend.EnqueueTask(ctx, sqliteTaskFixture(clock.Add(-time.Second), "sqlite-concurrency", "account"))
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Millisecond)
	second, err := backend.EnqueueTask(ctx, sqliteTaskFixture(clock.Add(-time.Second), "sqlite-concurrency", "account"))
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	claims := make(chan []store.Task, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claimed, claimErr := backend.ClaimTasks(ctx, store.TaskClaim{
				Limit: 1, Slugs: []string{"sqlite-concurrency"}, LeaseDuration: time.Minute,
			})
			claims <- claimed
			errorsFound <- claimErr
		}()
	}
	close(start)
	workers.Wait()
	close(claims)
	close(errorsFound)
	var claimed []store.Task
	for items := range claims {
		claimed = append(claimed, items...)
	}
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(claimed) != 1 || claimed[0].ID != first.ID {
		t.Fatalf("same-key concurrent claims = %#v", claimed)
	}
	if err := backend.CancelTask(ctx, claimed[0].ID); err != nil {
		t.Fatal(err)
	}
	follower := claimSQLiteTask(t, backend, store.TaskClaim{
		Limit: 1, Slugs: []string{"sqlite-concurrency"}, LeaseDuration: time.Minute,
	})
	if follower.ID != second.ID {
		t.Fatalf("same-key follower = %#v", follower)
	}

	target := store.DocumentReference{CollectionID: "posts", DocumentID: "post-1"}
	if _, err := backend.EnqueueTask(ctx, sqliteTargetTaskFixture(*clock, "missing-target", target)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing target enqueue = %v", err)
	}
	if err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		now := encodeTime(*clock)
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES (?, ?, ?, ?, '{}')`, string(target.CollectionID), target.DocumentID, now, now)
		return translateError(err)
	}); err != nil {
		t.Fatal(err)
	}
	targeted, err := backend.EnqueueTask(ctx, sqliteTargetTaskFixture(*clock, "targeted-task", target))
	if err != nil {
		t.Fatal(err)
	}
	targetClaim := claimSQLiteTask(t, backend, store.TaskClaim{
		Limit: 1, Slugs: []string{"targeted-task"}, LeaseDuration: time.Minute,
	})
	if err := backend.FailTask(ctx, store.TaskFailure{
		ID: targetClaim.ID, LeaseToken: targetClaim.LeaseToken, Code: "terminal", Message: "terminal",
	}); err != nil {
		t.Fatal(err)
	}
	wrongTarget := target
	wrongTarget.DocumentID += "-wrong"
	if err := backend.DismissTaskForTarget(ctx, targeted.ID, targeted.Slug, wrongTarget); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong-target dismiss = %v", err)
	}
	if err := backend.DismissTaskForTarget(ctx, targeted.ID, targeted.Slug, target); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindTask(ctx, targeted.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("dismissed task lookup = %v", err)
	}
	if _, err := backend.EnqueueTask(ctx, store.Task{
		Slug: "oversized-task", Queue: "default",
		Input: json.RawMessage(`"` + strings.Repeat("x", store.MaxTaskPayloadBytes) + `"`),
		RunAt: *clock, MaxAttempts: 1, RetryDelay: time.Second, MaxRetryDelay: time.Second,
		Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
	}); err == nil {
		t.Fatal("oversized task admission was accepted")
	}
	outOfRange := sqliteTaskFixture(time.Date(2500, time.January, 1, 0, 0, 0, 0, time.UTC), "out-of-range-task", "")
	if _, err := backend.EnqueueTask(ctx, outOfRange); err == nil || !strings.Contains(err.Error(), "nanosecond timestamp range") {
		t.Fatalf("out-of-range SQLite task timestamp = %v", err)
	}
}

func TestSQLiteTasksClaimAtomicallyAcrossStores(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tasks.db")
	firstStore, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := firstStore.Close(); err != nil {
			t.Error(err)
		}
	})
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})
	if err := firstStore.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	secondStore, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := secondStore.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	firstStore.now = func() time.Time { return now }
	secondStore.now = func() time.Time { return now }
	if _, err := firstStore.EnqueueTask(ctx, sqliteTaskFixture(now.Add(-time.Second), "cross-store-claim", "account")); err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.EnqueueTask(ctx, sqliteTaskFixture(now.Add(-time.Second), "cross-store-claim", "account")); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	claims := make(chan []store.Task, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for _, backend := range []*Store{firstStore, secondStore} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claimed, claimErr := backend.ClaimTasks(ctx, store.TaskClaim{
				Limit: 1, Slugs: []string{"cross-store-claim"}, LeaseDuration: time.Minute,
			})
			claims <- claimed
			errorsFound <- claimErr
		}()
	}
	close(start)
	workers.Wait()
	close(claims)
	close(errorsFound)

	var claimed []store.Task
	for items := range claims {
		claimed = append(claimed, items...)
	}
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(claimed) != 1 {
		t.Fatalf("cross-store same-key claims = %#v", claimed)
	}
}

func newSQLiteTaskBackend(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	ctx := context.Background()
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	backend.now = func() time.Time { return clock }
	return backend, &clock
}

func sqliteTaskFixture(runAt time.Time, slug, concurrencyKey string) store.Task {
	return store.Task{
		Slug: slug, Queue: "default", ConcurrencyKey: concurrencyKey,
		Input: json.RawMessage(`{}`), RunAt: runAt,
		MaxAttempts: 5, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffExponential, Timeout: time.Minute, Retention: time.Hour,
	}
}

func sqliteTargetTaskFixture(runAt time.Time, slug string, target store.DocumentReference) store.Task {
	task := sqliteTaskFixture(runAt, slug, "")
	task.Target = &target
	return task
}

func claimSQLiteTask(t *testing.T, backend *Store, request store.TaskClaim) store.Task {
	t.Helper()
	claimed, err := backend.ClaimTasks(context.Background(), request)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	return claimed[0]
}
