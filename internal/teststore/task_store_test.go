package teststore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
)

func TestTaskStoreRecoversStaleLeasesAndFencesTheOldWorker(t *testing.T) {
	backend := New()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	current := now
	backend.now = func() time.Time { return current }
	task := enqueueTaskFixture(t, backend, "recover", "")
	first := claimTaskFixture(t, backend, time.Minute)
	if first.ID != task.ID || first.Attempts != 1 || first.LeaseToken == "" {
		t.Fatalf("first lease = %#v", first)
	}
	current = now.Add(2 * time.Minute)
	for name, mutation := range map[string]func() error{
		"heartbeat": func() error {
			return backend.HeartbeatTask(context.Background(), task.ID, first.LeaseToken, time.Minute)
		},
		"complete": func() error {
			return backend.CompleteTask(context.Background(), task.ID, first.LeaseToken, json.RawMessage(`{}`))
		},
		"fail": func() error {
			return backend.FailTask(context.Background(), store.TaskFailure{ID: task.ID, LeaseToken: first.LeaseToken, Code: "stale", Message: "stale"})
		},
		"release": func() error {
			return backend.ReleaseTask(context.Background(), task.ID, first.LeaseToken, 0, "stale", "stale")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutation(); !errors.Is(err, store.ErrTaskLeaseLost) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	second := claimTaskFixture(t, backend, time.Minute)
	if second.ID != task.ID || second.Attempts != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("recovered lease = %#v", second)
	}
	if err := backend.CompleteTask(context.Background(), task.ID, second.LeaseToken, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	stored, err := backend.FindTask(context.Background(), task.ID)
	if err != nil || stored.State != store.TaskStateSucceeded || stored.Attempts != 2 {
		t.Fatalf("completed recovery = %#v, %v", stored, err)
	}
}

func TestTaskStoreSerializesConcurrencyKeysWithoutBlockingOtherKeys(t *testing.T) {
	backend := New()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	backend.now = func() time.Time { return now }
	first := enqueueTaskFixture(t, backend, "first", "account-1")
	second := enqueueTaskFixture(t, backend, "second", "account-1")
	third := enqueueTaskFixture(t, backend, "third", "account-2")
	claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 3, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	ids := map[string]store.Task{}
	for _, task := range claimed {
		ids[task.ID] = task
	}
	if _, ok := ids[first.ID]; !ok {
		t.Fatalf("deterministic first key owner missing: %#v", claimed)
	}
	if _, ok := ids[third.ID]; !ok {
		t.Fatalf("independent key missing: %#v", claimed)
	}
	if _, ok := ids[second.ID]; ok {
		t.Fatalf("same-key follower was claimed concurrently: %#v", claimed)
	}
	if err := backend.CompleteTask(context.Background(), first.ID, ids[first.ID].LeaseToken, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	follower := claimTaskFixture(t, backend, time.Minute)
	if follower.ID != second.ID {
		t.Fatalf("follower = %#v, want %s", follower, second.ID)
	}
}

func TestTaskStorePrunesOnlyExpiredTerminalRecords(t *testing.T) {
	backend := New()
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	current := now
	backend.now = func() time.Time { return current }
	task := enqueueTaskFixture(t, backend, "prune", "")
	claimed := claimTaskFixture(t, backend, time.Minute)
	if err := backend.FailTask(context.Background(), store.TaskFailure{ID: task.ID, LeaseToken: claimed.LeaseToken, Code: "terminal", Message: "terminal"}); err != nil {
		t.Fatal(err)
	}
	current = now.Add(59 * time.Minute)
	if count, err := backend.PruneTasks(context.Background(), 10); err != nil || count != 0 {
		t.Fatalf("early prune = %d, %v", count, err)
	}
	current = now.Add(time.Hour)
	if count, err := backend.PruneTasks(context.Background(), 10); err != nil || count != 1 {
		t.Fatalf("due prune = %d, %v", count, err)
	}
	if _, err := backend.FindTask(context.Background(), task.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pruned lookup = %v", err)
	}
}

func TestTaskStoreRejectsUnboundedRawCallsAndClearsCallerLifecycle(t *testing.T) {
	backend := New()
	backend.now = func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }
	valid := store.Task{
		Slug: "bounded-task", Queue: "default", Input: json.RawMessage(`{}`), RunAt: backend.now(),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
	}
	oversized := json.RawMessage(`"` + strings.Repeat("x", store.MaxTaskPayloadBytes) + `"`)
	invalidAdmission := valid
	invalidAdmission.Input = oversized
	if _, err := backend.EnqueueTask(context.Background(), invalidAdmission); err == nil {
		t.Fatal("oversized raw task input was accepted")
	}
	invalidAdmission = valid
	invalidAdmission.Timeout = time.Millisecond + time.Nanosecond
	if _, err := backend.EnqueueTask(context.Background(), invalidAdmission); err == nil {
		t.Fatal("sub-millisecond raw task policy was accepted")
	}

	poisoned := valid
	poisoned.State = store.TaskStateSucceeded
	poisoned.Attempts = 99
	poisoned.Output = json.RawMessage(`{"forged":true}`)
	poisoned.LeaseToken = "forged"
	poisoned.LastErrorCode = "forged"
	poisoned.LastError = "forged"
	completed := backend.now()
	poisoned.CompletedAt = &completed
	poisoned.RetainUntil = &completed
	queued, err := backend.EnqueueTask(context.Background(), poisoned)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != store.TaskStateQueued || queued.Attempts != 0 || len(queued.Output) != 0 || queued.LeaseToken != "" || queued.LastError != "" || queued.CompletedAt != nil || queued.RetainUntil != nil {
		t.Fatalf("caller-owned lifecycle leaked through admission: %#v", queued)
	}
	if _, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: store.MaxTaskBatch + 1, LeaseDuration: time.Minute}); err == nil {
		t.Fatal("unbounded claim was accepted")
	}
	claimed := claimTaskFixture(t, backend, time.Minute)
	if err := backend.CompleteTask(context.Background(), claimed.ID, claimed.LeaseToken, oversized); err == nil {
		t.Fatal("oversized raw task output was accepted")
	}
	if err := backend.FailTask(context.Background(), store.TaskFailure{
		ID: claimed.ID, LeaseToken: claimed.LeaseToken, Code: "provider_failed", Message: strings.Repeat("x", store.MaxTaskErrorBytes+1),
	}); err == nil {
		t.Fatal("oversized raw task failure was accepted")
	}
	if err := backend.ReleaseTask(context.Background(), claimed.ID, claimed.LeaseToken, time.Nanosecond, "worker_shutdown", "shutdown"); err == nil {
		t.Fatal("sub-millisecond raw task release was accepted")
	}
	if err := backend.CompleteTask(context.Background(), claimed.ID, claimed.LeaseToken, json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
}

func TestTaskStoreDismissesTargetedTerminalRecordsButGeneralCancelRetainsStatus(t *testing.T) {
	backend := New()
	backend.now = func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }
	target := store.DocumentReference{CollectionID: "posts", DocumentID: "post-1"}
	task := enqueueTaskFixture(t, backend, "dismiss-task", "")
	backend.mu.Lock()
	stored := backend.tasks[task.ID]
	stored.Target = &target
	backend.tasks[task.ID] = stored
	backend.mu.Unlock()
	claimed := claimTaskFixture(t, backend, time.Minute)
	if err := backend.FailTask(context.Background(), store.TaskFailure{ID: claimed.ID, LeaseToken: claimed.LeaseToken, Code: "terminal", Message: "terminal"}); err != nil {
		t.Fatal(err)
	}
	if err := backend.DismissTaskForTarget(context.Background(), task.ID, task.Slug, target); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindTask(context.Background(), task.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("dismissed lookup = %v", err)
	}

	canceled := enqueueTaskFixture(t, backend, "cancel-task", "")
	if err := backend.CancelTask(context.Background(), canceled.ID); err != nil {
		t.Fatal(err)
	}
	retained, err := backend.FindTask(context.Background(), canceled.ID)
	if err != nil || retained.State != store.TaskStateCanceled || retained.RetainUntil == nil {
		t.Fatalf("general cancellation was not retained: %#v, %v", retained, err)
	}
}

func enqueueTaskFixture(t *testing.T, backend *Store, slug, concurrencyKey string) store.Task {
	t.Helper()
	task, err := backend.EnqueueTask(context.Background(), store.Task{
		Slug: slug, Queue: "default", ConcurrencyKey: concurrencyKey,
		Input: json.RawMessage(`{}`), RunAt: backend.now().Add(-time.Second),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func claimTaskFixture(t *testing.T, backend *Store, lease time.Duration) store.Task {
	t.Helper()
	claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 1, LeaseDuration: lease})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	return claimed[0]
}
