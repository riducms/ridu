package mongodb

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoTaskCodecIsStrictDetachedAndLifecycleAware(t *testing.T) {
	base := time.Date(2026, time.August, 30, 12, 34, 56, 789123456, time.UTC)
	for _, task := range []store.Task{
		mongoStoredTaskFixture(base, store.TaskStateQueued),
		mongoStoredTaskFixture(base, store.TaskStateRunning),
		mongoStoredTaskFixture(base, store.TaskStateSucceeded),
		mongoStoredTaskFixture(base, store.TaskStateFailed),
		mongoStoredTaskFixture(base, store.TaskStateCanceled),
	} {
		t.Run(string(task.State), func(t *testing.T) {
			encoded, err := encodeMongoTask(task)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeMongoTask(marshalMongoSystemFixture(t, encoded))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, task) {
				t.Fatalf("decoded task = %#v, want %#v", decoded, task)
			}
			decoded.Input[0] = '['
			again, err := decodeMongoTask(marshalMongoSystemFixture(t, encoded))
			if err != nil || string(again.Input) != `{"input":true}` {
				t.Fatalf("detached task input = %q, %v", again.Input, err)
			}
		})
	}

	queued := mongoStoredTaskFixture(base, store.TaskStateQueued)
	queued.LastErrorCode = "worker_shutdown"
	queued.LastError = ""
	if _, err := encodeMongoTask(queued); err != nil {
		t.Fatalf("empty failure message rejected: %v", err)
	}

	encoded, err := encodeMongoTask(mongoStoredTaskFixture(base, store.TaskStateQueued))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		mutate       func(bson.D) bson.D
		want         string
		wantConflict bool
	}{
		{
			name: "unknown key", want: "unknown key",
			mutate: func(document bson.D) bson.D { return append(document, bson.E{Key: "future", Value: true}) },
		},
		{
			name: "wrong task ID", want: "invalid ID",
			mutate: func(document bson.D) bson.D {
				document[mongoTaskElementIndex(document, "_id")].Value = "wrong"
				return document
			},
		},
		{
			name: "wrong concurrency identity", want: "logical identity collision", wantConflict: true,
			mutate: func(document bson.D) bson.D {
				document[mongoTaskElementIndex(document, "concurrencyID")].Value = "z_task_concurrency_wrong"
				return document
			},
		},
		{
			name: "input is not binary", want: "invalid input",
			mutate: func(document bson.D) bson.D {
				document[mongoTaskElementIndex(document, "input")].Value = `{"input":true}`
				return document
			},
		},
		{
			name: "running without lease", want: "lease lifecycle",
			mutate: func(document bson.D) bson.D {
				document[mongoTaskElementIndex(document, "state")].Value = string(store.TaskStateRunning)
				return document
			},
		},
		{
			name: "message without code", want: "error pair",
			mutate: func(document bson.D) bson.D {
				document[mongoTaskElementIndex(document, "lastError")].Value = "unsafe"
				return document
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := append(bson.D(nil), encoded...)
			fixture = test.mutate(fixture)
			_, err := decodeMongoTask(marshalMongoSystemFixture(t, fixture))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want containing %q", err, test.want)
			}
			if test.wantConflict && !errors.Is(err, store.ErrConflict) {
				t.Fatalf("decode error = %v, want ErrConflict", err)
			}
		})
	}

	chronologyTests := []struct {
		name   string
		task   store.Task
		mutate func(bson.D, store.Task) bson.D
		want   string
	}{
		{
			name: "update before creation", task: mongoStoredTaskFixture(base, store.TaskStateQueued), want: "update chronology",
			mutate: func(document bson.D, task store.Task) bson.D {
				document[mongoTaskElementIndex(document, "updatedAt")].Value = task.CreatedAt.Add(-time.Millisecond).UnixNano()
				return document
			},
		},
		{
			name: "running lease not after update", task: mongoStoredTaskFixture(base, store.TaskStateRunning), want: "lease chronology",
			mutate: func(document bson.D, task store.Task) bson.D {
				document[mongoTaskElementIndex(document, "leaseExpiresAt")].Value = task.UpdatedAt.UnixNano()
				return document
			},
		},
		{
			name: "succeeded with failure", task: mongoStoredTaskFixture(base, store.TaskStateSucceeded), want: "succeeded task has failure state",
			mutate: func(document bson.D, _ store.Task) bson.D {
				document[mongoTaskElementIndex(document, "lastErrorCode")].Value = "stale_failure"
				return document
			},
		},
		{
			name: "completion differs from update", task: mongoStoredTaskFixture(base, store.TaskStateSucceeded), want: "terminal chronology",
			mutate: func(document bson.D, task store.Task) bson.D {
				completed := task.UpdatedAt.Add(time.Millisecond)
				document[mongoTaskElementIndex(document, "completedAt")].Value = completed.UnixNano()
				document[mongoTaskElementIndex(document, "retainUntil")].Value = completed.Add(task.Retention).UnixNano()
				return document
			},
		},
		{
			name: "retention before completion", task: mongoStoredTaskFixture(base, store.TaskStateSucceeded), want: "terminal chronology",
			mutate: func(document bson.D, task store.Task) bson.D {
				document[mongoTaskElementIndex(document, "retainUntil")].Value = task.CompletedAt.Add(-time.Millisecond).UnixNano()
				return document
			},
		},
		{
			name: "retention does not match policy", task: mongoStoredTaskFixture(base, store.TaskStateSucceeded), want: "terminal chronology",
			mutate: func(document bson.D, task store.Task) bson.D {
				document[mongoTaskElementIndex(document, "retainUntil")].Value = task.CompletedAt.Add(task.Retention + time.Millisecond).UnixNano()
				return document
			},
		},
	}
	for _, test := range chronologyTests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := encodeMongoTask(test.task)
			if err != nil {
				t.Fatal(err)
			}
			fixture := test.mutate(append(bson.D(nil), encoded...), test.task)
			_, err = decodeMongoTask(marshalMongoSystemFixture(t, fixture))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoTaskConcurrencyGuardCodecIsStrict(t *testing.T) {
	base := time.Date(2026, time.August, 30, 12, 34, 56, 789000000, time.UTC)
	guard := mongoTaskConcurrencyGuard{
		Queue: "default", Key: "account-1", TaskID: "task_0123456789abcdef01234567",
		Token: "lease_0123456789abcdef01234567", ExpiresAt: base.Add(time.Minute), UpdatedAt: base,
		Target: &store.DocumentReference{CollectionID: "posts", DocumentID: "post-1"},
	}
	guard.ID = mongoTaskConcurrencyID(guard.Queue, guard.Key)
	encoded, err := encodeMongoTaskConcurrencyGuard(guard)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMongoTaskConcurrencyGuard(marshalMongoSystemFixture(t, encoded))
	if err != nil || !reflect.DeepEqual(decoded, guard) {
		t.Fatalf("decoded guard = %#v, %v", decoded, err)
	}
	for _, test := range []struct {
		name   string
		field  string
		value  any
		wanted string
	}{
		{name: "wrong deterministic ID", field: "_id", value: "wrong", wanted: "logical identity collision"},
		{name: "invalid token", field: "token", value: "lease-invalid", wanted: "invalid ownership"},
		{name: "invalid expiry", field: "expiresAt", value: "later", wanted: "invalid task concurrency expiresAt"},
		{name: "expiry not after update", field: "expiresAt", value: base.UnixNano(), wanted: "lease chronology"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := append(bson.D(nil), encoded...)
			fixture[mongoTaskElementIndex(fixture, test.field)].Value = test.value
			_, err := decodeMongoTaskConcurrencyGuard(marshalMongoSystemFixture(t, fixture))
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("decode error = %v, want containing %q", err, test.wanted)
			}
		})
	}
}

func TestMongoTaskAdmissionClearsCallerLifecycleAndNormalizesTime(t *testing.T) {
	completed := time.Now()
	task := mongoTaskFixture(time.Date(2026, 8, 30, 12, 0, 0, 123456789, time.FixedZone("caller", 3600)), "normalize", "key")
	task.ID = "caller-id"
	task.State = store.TaskStateSucceeded
	task.Attempts = 99
	task.Output = json.RawMessage(`{"forged":true}`)
	task.LeaseToken = "forged"
	task.LeaseExpiresAt = &completed
	task.LastErrorCode = "forged"
	task.LastError = "forged"
	task.CompletedAt = &completed
	task.RetainUntil = &completed
	normalized, err := normalizeMongoTaskAdmission(task)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.ID != "caller-id" || normalized.State != store.TaskStateQueued || normalized.Attempts != 0 ||
		len(normalized.Output) != 0 || normalized.LeaseToken != "" || normalized.LeaseExpiresAt != nil ||
		normalized.LastErrorCode != "" || normalized.LastError != "" || normalized.CompletedAt != nil || normalized.RetainUntil != nil ||
		normalized.RunAt.Location() != time.UTC {
		t.Fatalf("normalized task = %#v", normalized)
	}
}

func TestMongoTaskClaimPipelineGroupsBeforeGlobalLimitAndUsesServerTime(t *testing.T) {
	pipeline := mongoTaskClaimPipeline(store.TaskClaim{
		Limit: 7, Queues: []string{"default"}, Slugs: []string{"worker"}, LeaseDuration: time.Minute,
	})
	wantStages := []string{"$match", "$sort", "$lookup", "$match", "$group", "$replaceWith", "$project", "$sort", "$limit"}
	if len(pipeline) != len(wantStages) {
		t.Fatalf("claim pipeline stages = %#v", pipeline)
	}
	for index, stage := range pipeline {
		if len(stage) != 1 || stage[0].Key != wantStages[index] {
			t.Fatalf("claim pipeline stage %d = %#v, want %s", index, stage, wantStages[index])
		}
	}
	encoded, err := bson.MarshalExtJSON(bson.D{{Key: "pipeline", Value: pipeline}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, fragment := range []string{"$$NOW", mongoTaskConcurrencyCollectionName, `"$group"`, `"$limit":7`} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("claim pipeline lacks %q: %s", fragment, text)
		}
	}
}

func TestMongoTaskBlockingGuardPipelineRoutesActiveRowsThroughStrictDecode(t *testing.T) {
	pipeline := mongoTaskBlockingGuardPipeline(store.TaskClaim{
		Limit: 7, Queues: []string{"default"}, Slugs: []string{"worker"}, LeaseDuration: time.Minute,
	})
	wantStages := []string{"$match", "$lookup", "$unwind", "$replaceWith", "$group", "$replaceWith"}
	if len(pipeline) != len(wantStages) {
		t.Fatalf("blocking-guard pipeline stages = %#v", pipeline)
	}
	for index, stage := range pipeline {
		if len(stage) != 1 || stage[0].Key != wantStages[index] {
			t.Fatalf("blocking-guard pipeline stage %d = %#v, want %s", index, stage, wantStages[index])
		}
	}
	encoded, err := bson.MarshalExtJSON(bson.D{{Key: "pipeline", Value: pipeline}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"$$NOW", mongoTaskConcurrencyCollectionName, `"$_riduActiveConcurrency"`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("blocking-guard pipeline lacks %q: %s", fragment, encoded)
		}
	}
}

func TestMongoTaskSystemIndexPlansAreExactAndHaveNoTTL(t *testing.T) {
	plans := mongoSystemIndexPlans(nil)
	var tasks, guards *mongoSystemIndexPlan
	for index := range plans {
		switch plans[index].kind {
		case mongoSystemTaskIndexes:
			tasks = &plans[index]
		case mongoSystemTaskConcurrencyIndexes:
			guards = &plans[index]
		}
	}
	if tasks == nil || guards == nil || len(tasks.definitions) != 5 || len(guards.definitions) != 3 {
		t.Fatalf("task index plans = %#v, %#v", tasks, guards)
	}
	want := map[string]bson.D{
		mongoTaskDueIndexName:       {{Key: "state", Value: int32(1)}, {Key: "runAt", Value: int32(1)}, {Key: "createdAt", Value: int32(1)}, {Key: "_id", Value: int32(1)}},
		mongoTaskLeaseIndexName:     {{Key: "state", Value: int32(1)}, {Key: "leaseExpiresAt", Value: int32(1)}, {Key: "runAt", Value: int32(1)}, {Key: "createdAt", Value: int32(1)}, {Key: "_id", Value: int32(1)}},
		mongoTaskTargetIndexName:    {{Key: "target.collection", Value: int32(1)}, {Key: "target.document", Value: int32(1)}, {Key: "slug", Value: int32(1)}, {Key: "state", Value: int32(1)}, {Key: "runAt", Value: int32(1)}, {Key: "createdAt", Value: int32(1)}, {Key: "_id", Value: int32(1)}},
		mongoTaskRequesterIndexName: {{Key: "requestedBy.collection", Value: int32(1)}, {Key: "requestedBy.document", Value: int32(1)}},
		mongoTaskRetentionIndexName: {{Key: "retainUntil", Value: int32(1)}, {Key: "_id", Value: int32(1)}},
	}
	for _, definition := range tasks.definitions {
		if definition.name == "" || !reflect.DeepEqual(definition.keys, want[definition.name]) {
			t.Fatalf("task index %q = %#v", definition.name, definition)
		}
	}
	if !guards.definitions[0].unique || guards.definitions[0].name != mongoTaskConcurrencyIdentityIndexName ||
		!reflect.DeepEqual(guards.definitions[0].keys, bson.D{{Key: "queue", Value: int32(1)}, {Key: "key", Value: int32(1)}}) {
		t.Fatalf("task guard identity index = %#v", guards.definitions[0])
	}
	// mongoIndexDefinition has no TTL option. The exact definitions above are
	// therefore also a structural proof that task correctness never depends on
	// background TTL cleanup.
}

func mongoStoredTaskFixture(base time.Time, state store.TaskState) store.Task {
	task := store.Task{
		ID: "task_0123456789abcdef01234567", Slug: "codec-task", Queue: "default", ConcurrencyKey: "account-1",
		Input: json.RawMessage(`{"input":true}`), State: state, RunAt: base.Add(-time.Minute), Attempts: 2,
		MaxAttempts: 5, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffExponential, Timeout: time.Minute, Retention: time.Hour,
		Target:      &store.DocumentReference{CollectionID: "posts", DocumentID: "post-1"},
		RequestedBy: &store.DocumentReference{CollectionID: "users", DocumentID: "user-1"},
		CreatedAt:   base, UpdatedAt: base.Add(time.Second),
	}
	switch state {
	case store.TaskStateRunning:
		task.LeaseToken = "lease_0123456789abcdef01234567"
		expires := base.Add(time.Minute)
		task.LeaseExpiresAt = &expires
	case store.TaskStateSucceeded:
		task.Output = json.RawMessage(`{"ok":true}`)
		completed, retain := base.Add(time.Second), base.Add(time.Hour+time.Second)
		task.CompletedAt, task.RetainUntil = &completed, &retain
	case store.TaskStateFailed:
		task.LastErrorCode, task.LastError = "terminal", "failed"
		completed, retain := base.Add(time.Second), base.Add(time.Hour+time.Second)
		task.CompletedAt, task.RetainUntil = &completed, &retain
	case store.TaskStateCanceled:
		completed, retain := base.Add(time.Second), base.Add(time.Hour+time.Second)
		task.CompletedAt, task.RetainUntil = &completed, &retain
	}
	return task
}

func mongoTaskElementIndex(document bson.D, key string) int {
	for index, element := range document {
		if element.Key == key {
			return index
		}
	}
	return -1
}
