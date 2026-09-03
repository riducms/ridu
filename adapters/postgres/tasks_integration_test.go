package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

type postgresTaskInput struct {
	ID int `json:"id"`
}

type postgresTaskOutput struct {
	ID int `json:"id"`
}

func TestPostgresDurableTasksClaimConcurrentlyAndRecoverExpiredLeases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var callsMu sync.Mutex
	calls := make(map[int]int)
	task := ridu.NewTask("postgres-task", func(_ ridu.TaskContext, input postgresTaskInput) (postgresTaskOutput, error) {
		callsMu.Lock()
		calls[input.ID]++
		callsMu.Unlock()
		return postgresTaskOutput{ID: input.ID}, nil
	})
	config := postgresTaskConfig(task)
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}

	const total = 32
	receipts := make([]ridu.TaskReceipt[postgresTaskOutput], total)
	for index := range receipts {
		receipts[index], err = task.Enqueue(ctx, application, postgresTaskInput{ID: index}, ridu.TaskEnqueueOptions{})
		if err != nil {
			t.Fatal(err)
		}
		queued, err := task.Result(ctx, application, receipts[index].ID)
		if err != nil || queued.State != store.TaskStateQueued {
			t.Fatalf("queued task %d = %#v, %v", index, queued, err)
		}
	}
	var workers sync.WaitGroup
	errorsFound := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				summary, err := application.RunTasks(ctx, 4)
				if err != nil {
					errorsFound <- err
					return
				}
				if summary.Claimed == 0 {
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	for index, receipt := range receipts {
		result, err := task.Result(ctx, application, receipt.ID)
		if err != nil || result.State != store.TaskStateSucceeded || !result.HasOutput || result.Output.ID != index {
			t.Fatalf("result %d = %#v, %v", index, result, err)
		}
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	for index := 0; index < total; index++ {
		if calls[index] != 1 {
			t.Fatalf("handler %d calls = %d", index, calls[index])
		}
	}

	crashReceipt, err := task.Enqueue(ctx, application, postgresTaskInput{ID: 1000}, ridu.TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{task.TaskSlug()}, LeaseDuration: 20 * time.Millisecond})
	if err != nil || len(first) != 1 || first[0].ID != crashReceipt.ID {
		t.Fatalf("first crash lease = %#v, %v", first, err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := backend.HeartbeatTask(ctx, crashReceipt.ID, first[0].LeaseToken, time.Minute); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("expired heartbeat = %v", err)
	}
	output, _ := json.Marshal(postgresTaskOutput{ID: 1000})
	if err := backend.CompleteTask(ctx, crashReceipt.ID, first[0].LeaseToken, output); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("expired completion before reclaim = %v", err)
	}
	second, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{task.TaskSlug()}, LeaseDuration: time.Minute})
	if err != nil || len(second) != 1 || second[0].ID != crashReceipt.ID || second[0].Attempts != 2 || second[0].LeaseToken == first[0].LeaseToken {
		t.Fatalf("recovered crash lease = %#v, %v", second, err)
	}
	if err := backend.CompleteTask(ctx, crashReceipt.ID, first[0].LeaseToken, output); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("stale completion = %v", err)
	}
	if err := backend.CompleteTask(ctx, crashReceipt.ID, second[0].LeaseToken, output); err != nil {
		t.Fatal(err)
	}
	recovered, err := task.Result(ctx, application, crashReceipt.ID)
	if err != nil || recovered.State != store.TaskStateSucceeded || recovered.Attempts != 2 || recovered.Output.ID != 1000 {
		t.Fatalf("recovered result = %#v, %v", recovered, err)
	}
}

func TestPostgresDurableTaskConcurrencyBacklogDoesNotStarveIndependentKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	task := ridu.NewTask("postgres-fair-task", func(_ ridu.TaskContext, input postgresTaskInput) (postgresTaskOutput, error) {
		return postgresTaskOutput{ID: input.ID}, nil
	})
	config := postgresTaskConfig(task)
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}

	owner, err := task.Enqueue(ctx, application, postgresTaskInput{ID: 1}, ridu.TaskEnqueueOptions{ConcurrencyKey: "occupied"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{task.TaskSlug()}, LeaseDuration: time.Minute})
	if err != nil || len(active) != 1 || active[0].ID != owner.ID {
		t.Fatalf("active key claim = %#v, %v", active, err)
	}
	for index := 0; index < 12; index++ {
		if _, err := task.Enqueue(ctx, application, postgresTaskInput{ID: 10 + index}, ridu.TaskEnqueueOptions{ConcurrencyKey: "occupied"}); err != nil {
			t.Fatal(err)
		}
	}
	independent, err := task.Enqueue(ctx, application, postgresTaskInput{ID: 99}, ridu.TaskEnqueueOptions{ConcurrencyKey: "independent"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{task.TaskSlug()}, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != independent.ID {
		t.Fatalf("independent key was starved behind occupied followers: %#v, %v", claimed, err)
	}
}

func TestPostgresDurableTaskConcurrencyKeysCancelAndUnknownSlugs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	blocking := ridu.NewTask("postgres-blocking-task", func(_ ridu.TaskContext, input postgresTaskInput) (postgresTaskOutput, error) {
		if input.ID == 99 {
			close(started)
			<-release
		}
		return postgresTaskOutput{ID: input.ID}, nil
	})
	config := postgresTaskConfig(blocking)
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}

	first, err := blocking.Enqueue(ctx, application, postgresTaskInput{ID: 1}, ridu.TaskEnqueueOptions{ConcurrencyKey: "account-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := blocking.Enqueue(ctx, application, postgresTaskInput{ID: 2}, ridu.TaskEnqueueOptions{ConcurrencyKey: "account-1"})
	if err != nil {
		t.Fatal(err)
	}
	claimStart := make(chan struct{})
	claims := make(chan []store.Task, 2)
	claimErrors := make(chan error, 2)
	for worker := 0; worker < 2; worker++ {
		go func() {
			<-claimStart
			claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{blocking.TaskSlug()}, LeaseDuration: time.Minute})
			claims <- claimed
			claimErrors <- err
		}()
	}
	close(claimStart)
	claimed := append(<-claims, (<-claims)...)
	if err := <-claimErrors; err != nil {
		t.Fatal(err)
	}
	if err := <-claimErrors; err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != first.ID {
		t.Fatalf("same-key concurrent claims = %#v", claimed)
	}
	output, _ := json.Marshal(postgresTaskOutput{ID: 1})
	if err := backend.CompleteTask(ctx, claimed[0].ID, claimed[0].LeaseToken, output); err != nil {
		t.Fatal(err)
	}
	follower, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, Slugs: []string{blocking.TaskSlug()}, LeaseDuration: time.Minute})
	if err != nil || len(follower) != 1 || follower[0].ID != second.ID {
		t.Fatalf("same-key follower = %#v, %v", follower, err)
	}
	if err := backend.CompleteTask(ctx, follower[0].ID, follower[0].LeaseToken, output); err != nil {
		t.Fatal(err)
	}

	cancelReceipt, err := blocking.Enqueue(ctx, application, postgresTaskInput{ID: 99}, ridu.TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run := make(chan error, 1)
	go func() {
		_, err := application.RunTasks(ctx, 1)
		run <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking task did not start")
	}
	if err := blocking.Cancel(ctx, application, cancelReceipt.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	var taskError *ridu.TaskError
	if err := <-run; !errors.As(err, &taskError) || taskError.Code != ridu.TaskErrorLeaseLost {
		t.Fatalf("canceled handler completion = %T %v", err, err)
	}
	canceled, err := blocking.Result(ctx, application, cancelReceipt.ID)
	if err != nil || canceled.State != store.TaskStateCanceled {
		t.Fatalf("canceled result = %#v, %v", canceled, err)
	}

	unknown, err := backend.EnqueueTask(ctx, store.Task{
		Slug: "removed-postgres-task", Queue: "default", Input: json.RawMessage(`{}`), RunAt: time.Now().Add(-time.Second),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(ctx, 10)
	if err != nil || summary.Failed != 1 {
		t.Fatalf("unknown task run = %#v, %v", summary, err)
	}
	storedUnknown, err := backend.FindTask(ctx, unknown.ID)
	if err != nil || storedUnknown.State != store.TaskStateFailed || storedUnknown.LastErrorCode != "task_not_registered" {
		t.Fatalf("unknown task = %#v, %v", storedUnknown, err)
	}
}

func TestPostgresDurableTaskReferencesRaceSafelyWithHardDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	task := ridu.NewTask("postgres-document-task", func(_ ridu.TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })
	config := postgresTaskConfig(task)
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	collectionID := application.Manifest().Snapshot().Collections[0].ID

	target, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("requester")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	targetReference := store.DocumentReference{CollectionID: collectionID, DocumentID: target.ID}
	requesterReference := store.DocumentReference{CollectionID: collectionID, DocumentID: requester.ID}
	receipt, err := task.Enqueue(ctx, application, struct{}{}, ridu.TaskEnqueueOptions{Target: &targetReference, RequestedBy: &requesterReference})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(ctx, "posts", requester.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := task.Result(ctx, application, receipt.ID); !hasTaskCode(err, ridu.TaskErrorNotFound) {
		t.Fatalf("requester cleanup = %v", err)
	}

	for iteration := 0; iteration < 12; iteration++ {
		document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String(fmt.Sprintf("race-%d", iteration))}, nil)
		if err != nil {
			t.Fatal(err)
		}
		reference := store.DocumentReference{CollectionID: collectionID, DocumentID: document.ID}
		start := make(chan struct{})
		type enqueueResult struct {
			receipt ridu.TaskReceipt[struct{}]
			err     error
		}
		enqueued := make(chan enqueueResult, 1)
		deleted := make(chan error, 1)
		go func() {
			<-start
			receipt, err := task.Enqueue(ctx, application, struct{}{}, ridu.TaskEnqueueOptions{Target: &reference})
			enqueued <- enqueueResult{receipt: receipt, err: err}
		}()
		go func() {
			<-start
			_, err := application.Local().Delete(ctx, "posts", document.ID, nil)
			deleted <- err
		}()
		close(start)
		admission, deleteError := <-enqueued, <-deleted
		if deleteError != nil {
			t.Fatalf("iteration %d delete = %v", iteration, deleteError)
		}
		if admission.err != nil && !hasTaskCode(admission.err, ridu.TaskErrorNotFound) {
			t.Fatalf("iteration %d enqueue = %v", iteration, admission.err)
		}
		if admission.err == nil {
			if _, err := task.Result(ctx, application, admission.receipt.ID); !hasTaskCode(err, ridu.TaskErrorNotFound) {
				t.Fatalf("iteration %d left orphan task: %v", iteration, err)
			}
		}
	}
}

func TestPostgresDurableTaskRejectsUnboundedRawCallsAndDismissesTerminalTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config := postgresTaskConfig()
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	valid := store.Task{
		Slug: "raw-postgres-task", Queue: "default", Input: json.RawMessage(`{}`), RunAt: time.Now().Add(-time.Second),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
	}
	oversized := json.RawMessage(`"` + strings.Repeat("x", store.MaxTaskPayloadBytes) + `"`)
	invalid := valid
	invalid.Input = oversized
	if _, err := backend.EnqueueTask(ctx, invalid); err == nil {
		t.Fatal("oversized raw task input was accepted")
	}
	invalid = valid
	invalid.Retention = store.MaxTaskRetention + time.Millisecond
	if _, err := backend.EnqueueTask(ctx, invalid); err == nil {
		t.Fatal("unbounded raw task retention was accepted")
	}
	poisoned := valid
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
		t.Fatalf("caller-owned lifecycle leaked through admission: %#v", queued)
	}
	claimed, err := backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != queued.ID {
		t.Fatalf("raw task claim = %#v, %v", claimed, err)
	}
	if err := backend.CompleteTask(ctx, queued.ID, claimed[0].LeaseToken, oversized); err == nil {
		t.Fatal("oversized raw task output was accepted")
	}
	if err := backend.FailTask(ctx, store.TaskFailure{
		ID: queued.ID, LeaseToken: claimed[0].LeaseToken, Code: "provider_failed", Message: strings.Repeat("x", store.MaxTaskErrorBytes+1),
	}); err == nil {
		t.Fatal("oversized raw task failure was accepted")
	}
	if err := backend.CompleteTask(ctx, queued.ID, claimed[0].LeaseToken, json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}

	document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := store.DocumentReference{CollectionID: application.Manifest().Snapshot().Collections[0].ID, DocumentID: document.ID}
	targeted := valid
	targeted.Target = &target
	targeted, err = backend.EnqueueTask(ctx, targeted)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = backend.ClaimTasks(ctx, store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != targeted.ID {
		t.Fatalf("targeted task claim = %#v, %v", claimed, err)
	}
	if err := backend.FailTask(ctx, store.TaskFailure{ID: targeted.ID, LeaseToken: claimed[0].LeaseToken, Code: "terminal", Message: "terminal"}); err != nil {
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
}

func postgresTaskConfig(definitions ...ridu.TaskDefinition) ridu.Config {
	return ridu.Config{
		Name: "PostgreSQL durable tasks", Tasks: definitions,
		Collections: []ridu.Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title", field.Required())}}},
	}
}

func hasTaskCode(err error, code ridu.TaskErrorCode) bool {
	var taskError *ridu.TaskError
	return errors.As(err, &taskError) && taskError.Code == code
}
