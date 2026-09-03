package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBTaskStoreLifecycleUsesServerTimeAndStrictLeases(t *testing.T) {
	backend := mongoIntegrationStore(t)
	_, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	backend.now = func() time.Time { return time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) }
	before := time.Now().Add(-time.Second)
	task, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(time.Now().Add(-time.Second), "lifecycle", ""))
	if err != nil {
		t.Fatal(err)
	}
	if task.State != store.TaskStateQueued || task.Attempts != 0 || task.CreatedAt.IsZero() ||
		task.CreatedAt.UnixNano()%int64(time.Millisecond) != 0 || !task.CreatedAt.Equal(task.UpdatedAt) ||
		task.CreatedAt.Before(before) || task.CreatedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("enqueued task = %#v", task)
	}
	claimed, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	lease := claimed[0]
	if lease.ID != task.ID || lease.State != store.TaskStateRunning || lease.Attempts != 1 || lease.LeaseToken == "" || lease.LeaseExpiresAt == nil {
		t.Fatalf("claimed task = %#v", lease)
	}
	if err := backend.HeartbeatTask(t.Context(), lease.ID, lease.LeaseToken, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := backend.CompleteTask(t.Context(), lease.ID, lease.LeaseToken, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	stored, err := backend.FindTask(t.Context(), task.ID)
	if err != nil || stored.State != store.TaskStateSucceeded || string(stored.Output) != `{"ok":true}` || stored.CompletedAt == nil || stored.RetainUntil == nil {
		t.Fatalf("completed task = %#v, %v", stored, err)
	}
	expiredRetainUntil := time.Now().Add(-time.Second).UTC().Truncate(time.Millisecond)
	expiredCompletion := expiredRetainUntil.Add(-stored.Retention)
	if _, err := backend.taskCollection().UpdateOne(t.Context(), bson.D{{Key: "_id", Value: task.ID}}, bson.D{{
		Key: "$set", Value: bson.D{
			{Key: "createdAt", Value: expiredCompletion.Add(-time.Second).UnixNano()},
			{Key: "updatedAt", Value: expiredCompletion.UnixNano()},
			{Key: "completedAt", Value: expiredCompletion.UnixNano()},
			{Key: "retainUntil", Value: expiredRetainUntil.UnixNano()},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if count, err := backend.PruneTasks(t.Context(), 1); err != nil || count != 1 {
		t.Fatalf("prune = %d, %v", count, err)
	}
	if _, err := backend.FindTask(t.Context(), task.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pruned task lookup = %v", err)
	}
}

func TestMongoDBTaskClaimsAreAtomicAcrossClientsAndDoNotStarveIndependentKeys(t *testing.T) {
	backend := mongoIntegrationStore(t)
	peer := mongoSystemStorePeer(t, backend)
	_, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	if err := peer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(now.Add(-4*time.Second), "concurrent", "account"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(now.Add(-3*time.Second), "concurrent", "account"))
	if err != nil {
		t.Fatal(err)
	}
	independent, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(now.Add(-time.Second), "concurrent", "other"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	claims := make(chan []store.Task, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for _, worker := range []*Store{backend, peer} {
		worker := worker
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, claimErr := worker.ClaimTasks(ctx, store.TaskClaim{
				Limit: 2, Slugs: []string{"concurrent"}, LeaseDuration: time.Minute,
			})
			claims <- result
			errorsFound <- claimErr
		}()
	}
	close(start)
	workers.Wait()
	close(claims)
	close(errorsFound)
	claimed := make(map[string]store.Task)
	for items := range claims {
		for _, task := range items {
			if _, duplicate := claimed[task.ID]; duplicate {
				t.Fatalf("task was claimed twice: %#v", task)
			}
			claimed[task.ID] = task
		}
	}
	for claimErr := range errorsFound {
		if claimErr != nil {
			t.Fatal(claimErr)
		}
	}
	if len(claimed) != 2 || claimed[first.ID].ID == "" || claimed[independent.ID].ID == "" || claimed[second.ID].ID != "" {
		t.Fatalf("cross-client claims = %#v", claimed)
	}
	if err := backend.CompleteTask(t.Context(), first.ID, claimed[first.ID].LeaseToken, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	follower, err := peer.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"concurrent"}, LeaseDuration: time.Minute})
	if err != nil || len(follower) != 1 || follower[0].ID != second.ID {
		t.Fatalf("same-key follower = %#v, %v", follower, err)
	}

	owner, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(now.Add(-5*time.Second), "fair", "blocked"))
	if err != nil {
		t.Fatal(err)
	}
	ownerClaim, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"fair"}, LeaseDuration: time.Minute})
	if err != nil || len(ownerClaim) != 1 || ownerClaim[0].ID != owner.ID {
		t.Fatalf("fairness owner = %#v, %v", ownerClaim, err)
	}
	for index := 0; index < 8; index++ {
		if _, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(now.Add(time.Duration(-4+index)*time.Millisecond), "fair", "blocked")); err != nil {
			t.Fatal(err)
		}
	}
	fairIndependent, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(now.Add(-time.Millisecond), "fair", "free"))
	if err != nil {
		t.Fatal(err)
	}
	fairClaim, err := peer.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"fair"}, LeaseDuration: time.Minute})
	if err != nil || len(fairClaim) != 1 || fairClaim[0].ID != fairIndependent.ID {
		t.Fatalf("pre-limit anti-starvation claim = %#v, %v", fairClaim, err)
	}
}

func TestMongoDBTaskLeaseReclaimFencesStaleWorkers(t *testing.T) {
	backend := mongoIntegrationStore(t)
	_, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	task, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(time.Now().Add(-time.Second), "reclaim", "account"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, LeaseDuration: 20 * time.Millisecond})
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim = %#v, %v", first, err)
	}
	time.Sleep(35 * time.Millisecond)
	for name, mutation := range map[string]func() error{
		"heartbeat": func() error { return backend.HeartbeatTask(t.Context(), task.ID, first[0].LeaseToken, time.Minute) },
		"complete": func() error {
			return backend.CompleteTask(t.Context(), task.ID, first[0].LeaseToken, json.RawMessage(`{}`))
		},
		"fail": func() error {
			return backend.FailTask(t.Context(), store.TaskFailure{ID: task.ID, LeaseToken: first[0].LeaseToken, Code: "stale"})
		},
		"release": func() error { return backend.ReleaseTask(t.Context(), task.ID, first[0].LeaseToken, 0, "stale", "") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutation(); !errors.Is(err, store.ErrTaskLeaseLost) {
				t.Fatalf("stale mutation error = %v", err)
			}
		})
	}
	second, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(second) != 1 || second[0].ID != task.ID || second[0].Attempts != 2 || second[0].LeaseToken == first[0].LeaseToken {
		t.Fatalf("reclaimed task = %#v, %v", second, err)
	}
	if err := backend.CompleteTask(t.Context(), task.ID, second[0].LeaseToken, json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
}

func TestMongoDBTaskGuardLogicalDriftFailsPromptly(t *testing.T) {
	backend := mongoIntegrationStore(t)
	_, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	task, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(time.Now().Add(-time.Second), "drift", "account"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = backend.taskConcurrencyCollection().InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: "wrong-id"},
		{Key: "codec", Value: mongoTaskCodecVersion},
		{Key: "queue", Value: task.Queue},
		{Key: "key", Value: task.ConcurrencyKey},
		{Key: "task", Value: task.ID},
		{Key: "token", Value: "lease_0123456789abcdef01234567"},
		{Key: "expiresAt", Value: now.Add(time.Minute).UnixNano()},
		{Key: "updatedAt", Value: now.UnixNano()},
		{Key: "target", Value: nil},
		{Key: "requestedBy", Value: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute}); !errors.Is(err, store.ErrConflict) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong-ID logical guard claim = %v, want prompt ErrConflict", err)
	}
	stored, err := backend.FindTask(t.Context(), task.ID)
	if err != nil || stored.State != store.TaskStateQueued {
		t.Fatalf("drift claim mutated task = %#v, %v", stored, err)
	}
}

func TestMongoDBTaskClaimRejectsCorruptActiveGuardBeforeMutation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	_, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	task, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(time.Now().Add(-time.Second), "corrupt-blocker", "account"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	guardID := mongoTaskConcurrencyID(task.Queue, task.ConcurrencyKey)
	if _, err := backend.taskConcurrencyCollection().InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: guardID},
		{Key: "codec", Value: mongoTaskCodecVersion + 1},
		{Key: "queue", Value: task.Queue},
		{Key: "key", Value: task.ConcurrencyKey},
		{Key: "task", Value: task.ID},
		{Key: "token", Value: "lease_0123456789abcdef01234567"},
		{Key: "expiresAt", Value: now.Add(time.Minute).UnixNano()},
		{Key: "updatedAt", Value: now.UnixNano()},
		{Key: "target", Value: nil},
		{Key: "requestedBy", Value: nil},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute}); !errors.Is(err, store.ErrConflict) || errors.Is(err, context.DeadlineExceeded) || len(claimed) != 0 {
		t.Fatalf("corrupt active guard claim = %#v, %v, want prompt ErrConflict", claimed, err)
	}
	stored, err := backend.FindTask(t.Context(), task.ID)
	if err != nil || stored.State != store.TaskStateQueued || stored.Attempts != 0 || stored.LeaseToken != "" {
		t.Fatalf("corrupt active guard mutated task = %#v, %v", stored, err)
	}
	raw, err := backend.taskConcurrencyCollection().FindOne(t.Context(), bson.D{{Key: "_id", Value: guardID}}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != mongoTaskCodecVersion+1 {
		t.Fatalf("corrupt active guard was mutated to codec %d", codec)
	}
}

func TestMongoDBTaskReferencesCleanupAndFenceSameIDRecreation(t *testing.T) {
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
		mongoSystemDocuments(users, "requester", "race-requester", "cancel-requester"),
		mongoSystemDocuments(posts, "target", "race-target", "cancel-target"),
	)
	missing := store.DocumentReference{CollectionID: posts.ID, DocumentID: "missing"}
	invalid := mongoTaskFixture(time.Now().Add(-time.Second), "missing-reference", "")
	invalid.Target = &missing
	if _, err := backend.EnqueueTask(t.Context(), invalid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing target enqueue = %v", err)
	}

	target := store.DocumentReference{CollectionID: posts.ID, DocumentID: "target"}
	requester := store.DocumentReference{CollectionID: users.ID, DocumentID: "requester"}
	cleanupTask := mongoTaskFixture(time.Now().Add(-time.Second), "cleanup", "document")
	cleanupTask.Target = &target
	cleanupTask.RequestedBy = &requester
	queued, err := backend.EnqueueTask(t.Context(), cleanupTask)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"cleanup"}, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != queued.ID {
		t.Fatalf("cleanup task claim = %#v, %v", claimed, err)
	}
	cleanup := mongoBegin(t, backend, false)
	if _, err := cleanup.Delete(t.Context(), store.Request{Collection: users, ID: requester.DocumentID}); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	if err := cleanup.DeleteDocumentState(t.Context(), requester); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	mongoCommit(t, cleanup)
	if _, err := backend.FindTask(t.Context(), queued.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("requester cleanup left task: %v", err)
	}
	if count, err := backend.taskConcurrencyCollection().CountDocuments(t.Context(), bson.D{}); err != nil || count != 0 {
		t.Fatalf("requester cleanup guards = %d, %v", count, err)
	}

	racingTarget := store.DocumentReference{CollectionID: posts.ID, DocumentID: "race-target"}
	racingRequester := store.DocumentReference{CollectionID: users.ID, DocumentID: "race-requester"}
	candidate := mongoTaskFixture(time.Now().Add(-time.Second), "recreation", "")
	candidate.Target = &racingTarget
	candidate.RequestedBy = &racingRequester
	recreation := stageMongoSystemRecreation(t, backend, users, racingRequester.DocumentID)
	result := make(chan error, 1)
	go func() {
		_, enqueueErr := peer.EnqueueTask(t.Context(), candidate)
		result <- enqueueErr
	}()
	select {
	case err := <-result:
		mongoRollback(t, recreation)
		t.Fatalf("task enqueue did not serialize behind staged recreation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	mongoCommit(t, recreation)
	if err := <-result; !errors.Is(err, store.ErrConflict) {
		t.Fatalf("task racing same-ID recreation = %v, want ErrConflict", err)
	}
	listed, err := backend.ListTasks(t.Context(), store.TaskList{Slug: "recreation", Target: &racingTarget, Limit: 10})
	if err != nil || len(listed) != 0 {
		t.Fatalf("recreated requester inherited task = %#v, %v", listed, err)
	}

	blocker := mongoBegin(t, backend, false)
	if _, err := blocker.Find(t.Context(), store.Request{Collection: posts, ID: "cancel-target", Lock: store.LockMutation}); err != nil {
		mongoRollback(t, blocker)
		t.Fatal(err)
	}
	cancelTarget := store.DocumentReference{CollectionID: posts.ID, DocumentID: "cancel-target"}
	cancelRequester := store.DocumentReference{CollectionID: users.ID, DocumentID: "cancel-requester"}
	blocked := mongoTaskFixture(time.Now().Add(-time.Second), "cancel-admission", "")
	blocked.Target = &cancelTarget
	blocked.RequestedBy = &cancelRequester
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() {
		_, enqueueErr := peer.EnqueueTask(ctx, blocked)
		canceled <- enqueueErr
	}()
	select {
	case err := <-canceled:
		mongoRollback(t, blocker)
		t.Fatalf("contended enqueue returned before cancellation: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			mongoRollback(t, blocker)
			t.Fatalf("canceled task admission = %v", err)
		}
	case <-time.After(2 * time.Second):
		mongoRollback(t, blocker)
		t.Fatal("canceled task admission did not terminate")
	}
	mongoRollback(t, blocker)
}

func TestMongoDBTaskCancellationFailureReleaseAndTargetedDismissal(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, _, posts, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	seedMongoSystemDocuments(t, backend,
		mongoSystemDocuments(users, "requester"), mongoSystemDocuments(posts, "target"),
	)
	target := store.DocumentReference{CollectionID: posts.ID, DocumentID: "target"}
	requester := store.DocumentReference{CollectionID: users.ID, DocumentID: "requester"}
	task := mongoTaskFixture(time.Now().Add(-time.Second), "retry-flow", "account")
	task.Target, task.RequestedBy = &target, &requester
	queued, err := backend.EnqueueTask(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"retry-flow"}, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("retry claim = %#v, %v", claimed, err)
	}
	if err := backend.ReleaseTask(t.Context(), queued.ID, claimed[0].LeaseToken, 0, "worker_shutdown", ""); err != nil {
		t.Fatal(err)
	}
	released, err := backend.FindTask(t.Context(), queued.ID)
	if err != nil || released.State != store.TaskStateQueued || released.LastErrorCode != "worker_shutdown" || released.LastError != "" {
		t.Fatalf("released task = %#v, %v", released, err)
	}
	claimed, err = backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"retry-flow"}, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("retry second claim = %#v, %v", claimed, err)
	}
	retryAfter := 5 * time.Millisecond
	if err := backend.FailTask(t.Context(), store.TaskFailure{
		ID: queued.ID, LeaseToken: claimed[0].LeaseToken, Code: "provider_failed", Message: "retry", RetryAfter: &retryAfter,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	claimed, err = backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"retry-flow"}, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 3 {
		t.Fatalf("retry third claim = %#v, %v", claimed, err)
	}
	if err := backend.FailTask(t.Context(), store.TaskFailure{
		ID: queued.ID, LeaseToken: claimed[0].LeaseToken, Code: "terminal", Message: "failed",
	}); err != nil {
		t.Fatal(err)
	}
	wrong := target
	wrong.DocumentID = "other"
	if err := backend.DismissTaskForTarget(t.Context(), queued.ID, queued.Slug, wrong); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong-target dismissal = %v", err)
	}
	if err := backend.DismissTaskForTarget(t.Context(), queued.ID, queued.Slug, target); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindTask(t.Context(), queued.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("dismissed task lookup = %v", err)
	}

	cancelTask, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(time.Now().Add(-time.Second), "cancel", "account"))
	if err != nil {
		t.Fatal(err)
	}
	cancelClaim, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{"cancel"}, LeaseDuration: time.Minute})
	if err != nil || len(cancelClaim) != 1 {
		t.Fatalf("cancel claim = %#v, %v", cancelClaim, err)
	}
	if err := backend.CancelTask(t.Context(), cancelTask.ID); err != nil {
		t.Fatal(err)
	}
	canceled, err := backend.FindTask(t.Context(), cancelTask.ID)
	if err != nil || canceled.State != store.TaskStateCanceled || canceled.RetainUntil == nil || canceled.LeaseToken != "" {
		t.Fatalf("canceled task = %#v, %v", canceled, err)
	}
	if err := backend.HeartbeatTask(t.Context(), cancelTask.ID, cancelClaim[0].LeaseToken, time.Minute); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("canceled lease heartbeat = %v", err)
	}
}

func TestMongoDBTaskIndexesFailClosedAcrossBothCollections(t *testing.T) {
	for _, test := range []struct {
		name       string
		collection string
		index      string
	}{
		{name: "task documents", collection: mongoTaskCollectionName, index: mongoTaskDueIndexName},
		{name: "concurrency guards", collection: mongoTaskConcurrencyCollectionName, index: mongoTaskConcurrencyIdentityIndexName},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := mongoIntegrationStore(t)
			_, _, _, manifest := mongoSystemStoreManifest()
			if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
				t.Fatal(err)
			}
			if err := backend.database.Collection(test.collection).Indexes().DropOne(t.Context(), test.index); err != nil {
				t.Fatal(err)
			}
			if err := backend.VerifyIndexes(t.Context(), manifest); err == nil || !strings.Contains(err.Error(), "required index") {
				t.Fatalf("missing task index verification error = %v", err)
			}
			if err := backend.requireVerifiedTaskIndexes(); err == nil || !strings.Contains(err.Error(), "not verified") {
				t.Fatalf("failed task verification retained authorization: %v", err)
			}
			before, err := backend.taskCollection().CountDocuments(t.Context(), bson.D{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.EnqueueTask(t.Context(), mongoTaskFixture(time.Now(), "unverified", "")); err == nil || !strings.Contains(err.Error(), "not verified") {
				t.Fatalf("unverified task enqueue error = %v", err)
			}
			after, err := backend.taskCollection().CountDocuments(t.Context(), bson.D{})
			if err != nil || after != before {
				t.Fatalf("unverified task enqueue mutated collection: before=%d after=%d err=%v", before, after, err)
			}
		})
	}
}

func TestMongoDBTaskCleanupGatePrecedesEveryDocumentStateMutation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	users, _, _, manifest := mongoSystemStoreManifest()
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	seedMongoSystemDocuments(t, backend, mongoSystemDocuments(users, "cleanup-owner"))
	want, err := backend.SetPreference(t.Context(), store.Preference{
		CollectionID: users.ID,
		UserID:       "cleanup-owner",
		Key:          "cleanup-proof",
		Value:        json.RawMessage(`true`),
	})
	if err != nil {
		t.Fatal(err)
	}
	backend.indexesMu.Lock()
	backend.verifiedTaskIndexes = false
	backend.indexesMu.Unlock()

	transaction := mongoBegin(t, backend, false)
	err = transaction.DeleteDocumentState(t.Context(), store.DocumentReference{
		CollectionID: users.ID,
		DocumentID:   "cleanup-owner",
	})
	if err == nil || !strings.Contains(err.Error(), "task indexes are not verified") {
		mongoRollback(t, transaction)
		t.Fatalf("unverified task cleanup error = %v", err)
	}
	// Commit the transaction despite the application-level error. If any cleanup
	// mutation ran before the task gate, this makes that sequencing bug visible.
	mongoCommit(t, transaction)
	backend.indexesMu.Lock()
	backend.verifiedTaskIndexes = true
	backend.indexesMu.Unlock()
	got, err := backend.GetPreference(t.Context(), users.ID, "cleanup-owner", "cleanup-proof")
	if err != nil || string(got.Value) != string(want.Value) {
		t.Fatalf("task-gated cleanup mutated preference = %#v, %v", got, err)
	}
}

func mongoTaskFixture(runAt time.Time, slug, concurrencyKey string) store.Task {
	return store.Task{
		Slug: slug, Queue: "default", ConcurrencyKey: concurrencyKey,
		Input: json.RawMessage(`{}`), RunAt: runAt,
		MaxAttempts: 5, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffExponential, Timeout: time.Minute, Retention: time.Hour,
	}
}
