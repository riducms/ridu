package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type taskGreetingInput struct {
	Name string `json:"name"`
}

type taskGreetingOutput struct {
	Greeting string `json:"greeting"`
}

type panickingTaskError string

func (err panickingTaskError) Error() string { panic(string(err)) }

type taskGreetingInputTwin struct {
	Name string `json:"name"`
}

type postgresStyleTaskInput struct {
	ID int `json:"id"`
}

func TestTypedTaskRunsThroughDurableRegistryAndPersistsOutput(t *testing.T) {
	var calls atomic.Int32
	task := NewTask("send-greeting", func(ctx TaskContext, input taskGreetingInput) (taskGreetingOutput, error) {
		calls.Add(1)
		if ctx.Context == nil || ctx.Local == nil || ctx.ID == "" || ctx.Attempt != 1 {
			return taskGreetingOutput{}, fmt.Errorf("invalid task context: %#v", ctx)
		}
		return taskGreetingOutput{Greeting: "Hello " + input.Name}, nil
	}, TaskQueue("notifications"))
	application, _ := newTaskTestApp(t, task)

	receipt, err := task.Enqueue(context.Background(), application, taskGreetingInput{Name: "Ada"}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ID == "" || receipt.Queue != "notifications" || receipt.Slug != "send-greeting" {
		t.Fatalf("receipt = %#v", receipt)
	}
	before, err := task.Result(context.Background(), application, receipt.ID)
	if err != nil || before.State != store.TaskStateQueued || before.HasOutput {
		t.Fatalf("queued result = %#v, %v", before, err)
	}
	summary, err := application.RunTasks(context.Background(), 10)
	if err != nil || summary.Claimed != 1 || summary.Succeeded != 1 {
		t.Fatalf("run summary = %#v, %v", summary, err)
	}
	after, err := task.Result(context.Background(), application, receipt.ID)
	if err != nil || after.State != store.TaskStateSucceeded || !after.HasOutput || after.Output.Greeting != "Hello Ada" {
		t.Fatalf("completed result = %#v, %v", after, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestTaskAdmissionReconcilerRunsBeforeEveryClaimCycle(t *testing.T) {
	var reconciliations atomic.Int32
	var handled atomic.Int32
	var task TypedTask[struct{}, struct{}]
	task = NewTask("reconciled-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
		handled.Add(1)
		return struct{}{}, nil
	}, TaskAdmissionReconciler(func(ctx context.Context, application *App) error {
		if reconciliations.Add(1) != 1 {
			return nil
		}
		_, err := task.Enqueue(ctx, application, struct{}{}, TaskEnqueueOptions{})
		return err
	}))
	application, _ := newTaskTestApp(t, task)

	first, err := application.RunTasks(t.Context(), 10)
	if err != nil || first.Claimed != 1 || first.Succeeded != 1 {
		t.Fatalf("first reconciled run = %#v, %v", first, err)
	}
	second, err := application.RunTasks(t.Context(), 10)
	if err != nil || second.Claimed != 0 {
		t.Fatalf("second reconciled run = %#v, %v", second, err)
	}
	if reconciliations.Load() != 2 || handled.Load() != 1 {
		t.Fatalf("reconciliations/handled = %d/%d, want 2/1", reconciliations.Load(), handled.Load())
	}
}

func TestTaskAdmissionReconcilerFailureIsSurfacedWithoutStarvingQueuedTasks(t *testing.T) {
	reconcileError := errors.New("repair unavailable")
	task := NewTask("failing-reconciler", func(_ TaskContext, _ struct{}) (struct{}, error) {
		return struct{}{}, nil
	}, TaskAdmissionReconciler(func(context.Context, *App) error { return reconcileError }))
	application, _ := newTaskTestApp(t, task)
	if _, err := task.Enqueue(t.Context(), application, struct{}{}, TaskEnqueueOptions{}); err != nil {
		t.Fatal(err)
	}

	summary, err := application.RunTasks(t.Context(), 10)
	if !errors.Is(err, reconcileError) || summary.Succeeded != 1 {
		t.Fatalf("run after reconcile failure = %#v, %v", summary, err)
	}
}

func TestTaskRetryClassificationAndTerminalFailureAreDurable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var retryCalls atomic.Int32
		retrying := NewTask("retrying-task", func(_ TaskContext, _ struct{}) (string, error) {
			if retryCalls.Add(1) == 1 {
				return "", RetryTaskAfter("provider_busy", errors.New("provider busy"), time.Millisecond)
			}
			return "done", nil
		}, TaskRetries(3, time.Millisecond, time.Second, TaskBackoffFixed))
		terminal := NewTask("terminal-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
			return struct{}{}, AbortTask("invalid_destination", errors.New("destination is invalid"))
		})
		application, _ := newTaskTestApp(t, retrying, terminal)
		retryReceipt, err := retrying.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
		if err != nil {
			t.Fatal(err)
		}
		terminalReceipt, err := terminal.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := application.RunTasks(context.Background(), 10)
		if err != nil || summary.Retried != 1 || summary.Failed != 1 {
			t.Fatalf("first run = %#v, %v", summary, err)
		}
		failed, err := terminal.Result(context.Background(), application, terminalReceipt.ID)
		if err != nil || failed.State != store.TaskStateFailed || failed.LastErrorCode != "invalid_destination" || failed.LastError != "destination is invalid" {
			t.Fatalf("terminal result = %#v, %v", failed, err)
		}
		time.Sleep(3 * time.Millisecond)
		summary, err = application.RunTasks(context.Background(), 10)
		if err != nil || summary.Succeeded != 1 {
			t.Fatalf("retry run = %#v, %v", summary, err)
		}
		succeeded, err := retrying.Result(context.Background(), application, retryReceipt.ID)
		if err != nil || !succeeded.HasOutput || succeeded.Output != "done" || succeeded.Attempts != 2 {
			t.Fatalf("retry result = %#v, %v", succeeded, err)
		}
	})
}

func TestTaskHandlerPanicIsRecoveredWithoutPersistingOrReturningItsValue(t *testing.T) {
	const secret = "panic-secret-that-must-not-escape"
	panicking := NewTask("panicking-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
		panic(secret)
	}, TaskRetries(1, time.Millisecond, time.Second, TaskBackoffFixed))
	application, _ := newTaskTestApp(t, panicking)
	receipt, err := panicking.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(context.Background(), 1)
	if err == nil || !errors.Is(err, errTaskHandlerPanicked) {
		t.Fatalf("run error = %T %v", err, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("run error exposed panic value: %v", err)
	}
	if summary.Claimed != 1 || summary.Failed != 1 {
		t.Fatalf("run summary = %#v", summary)
	}
	result, err := panicking.Result(context.Background(), application, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != store.TaskStateFailed || result.LastErrorCode != "task_handler_panicked" || result.LastError != errTaskHandlerPanicked.Error() {
		t.Fatalf("panic result = %#v", result)
	}
	if strings.Contains(result.LastError, secret) {
		t.Fatalf("persisted error exposed panic value: %q", result.LastError)
	}
}

func TestTaskHandlerPanickingErrorIsRecoveredWithoutEscapingTheWorker(t *testing.T) {
	const secret = "panicking-error-secret"
	panicking := NewTask("panicking-error-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
		return struct{}{}, panickingTaskError(secret)
	}, TaskRetries(1, time.Millisecond, time.Second, TaskBackoffFixed))
	application, _ := newTaskTestApp(t, panicking)
	receipt, err := panicking.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(context.Background(), 1)
	if err == nil || !errors.Is(err, errTaskHandlerPanicked) || strings.Contains(err.Error(), secret) {
		t.Fatalf("run error = %T %v", err, err)
	}
	if summary.Claimed != 1 || summary.Failed != 1 {
		t.Fatalf("run summary = %#v", summary)
	}
	result, err := panicking.Result(context.Background(), application, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != store.TaskStateFailed || result.LastErrorCode != "task_handler_panicked" || result.LastError != errTaskHandlerPanicked.Error() {
		t.Fatalf("panic result = %#v", result)
	}
}

func TestTaskRuntimePanicIsContainedWithoutPersistingOrReportingThePanicValue(t *testing.T) {
	const secret = "store-runtime-panic-secret"
	definition := NewTask("runtime-panic-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
		return struct{}{}, nil
	})
	backend := &panickingCompleteTaskStore{Store: teststore.New(), panicValue: secret}
	application, err := New(taskTestConfig(definition), backend)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := definition.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(context.Background(), 1)
	if !errors.Is(err, errTaskRuntimePanicked) {
		t.Fatalf("run error = %T %v", err, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("run error exposed panic value: %v", err)
	}
	if summary.Claimed != 1 || summary.Succeeded != 0 || summary.Retried != 0 || summary.Failed != 0 {
		t.Fatalf("run summary = %#v", summary)
	}
	stored, err := backend.FindTask(context.Background(), receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != store.TaskStateRunning || stored.LeaseToken == "" {
		t.Fatalf("panicked terminal mutation must leave the lease recoverable: %#v", stored)
	}
}

func TestTaskBackoffIsDeterministicAndCapped(t *testing.T) {
	tests := []struct {
		name     string
		backoff  store.TaskBackoff
		attempts int
		want     time.Duration
	}{
		{name: "fixed", backoff: store.TaskBackoffFixed, attempts: 4, want: time.Second},
		{name: "linear", backoff: store.TaskBackoffLinear, attempts: 4, want: 4 * time.Second},
		{name: "exponential", backoff: store.TaskBackoffExponential, attempts: 4, want: 8 * time.Second},
		{name: "capped", backoff: store.TaskBackoffExponential, attempts: 20, want: 10 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := taskRetryDelay(store.Task{RetryDelay: time.Second, MaxRetryDelay: 10 * time.Second, Backoff: test.backoff, Attempts: test.attempts}, 0)
			if got != test.want {
				t.Fatalf("delay = %s, want %s", got, test.want)
			}
		})
	}
}

func TestFutureTaskIsNotClaimedEarly(t *testing.T) {
	task := NewTask("future-task", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })
	application, _ := newTaskTestApp(t, task)
	receipt, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{RunAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(context.Background(), 10)
	if err != nil || summary.Claimed != 0 {
		t.Fatalf("early run = %#v, %v", summary, err)
	}
	result, err := task.Result(context.Background(), application, receipt.ID)
	if err != nil || result.State != store.TaskStateQueued {
		t.Fatalf("future result = %#v, %v", result, err)
	}
}

func TestUnknownPersistedTaskIsDeadLetteredWithoutExecutingCode(t *testing.T) {
	var calls atomic.Int32
	known := NewTask("known-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
		calls.Add(1)
		return struct{}{}, nil
	})
	application, backend := newTaskTestApp(t, known)
	now := time.Now().UTC()
	unknown, err := backend.EnqueueTask(context.Background(), store.Task{
		Slug: "removed-task", Queue: "default", Input: []byte(`{}`), RunAt: now.Add(-time.Second),
		MaxAttempts: 3, RetryDelay: time.Second, MaxRetryDelay: time.Minute,
		Backoff: store.TaskBackoffFixed, Timeout: time.Minute, Retention: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(context.Background(), 10)
	if err != nil || summary.Failed != 1 {
		t.Fatalf("unknown run = %#v, %v", summary, err)
	}
	stored, err := backend.FindTask(context.Background(), unknown.ID)
	if err != nil || stored.State != store.TaskStateFailed || stored.LastErrorCode != "task_not_registered" {
		t.Fatalf("unknown result = %#v, %v", stored, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("registered handler ran %d times for an unknown slug", calls.Load())
	}
}

func TestTypedTaskRejectsAnUnregisteredDefinitionAndOversizedInput(t *testing.T) {
	registered := NewTask("registered-task", func(_ TaskContext, input taskGreetingInput) (struct{}, error) { return struct{}{}, nil })
	unregistered := NewTask("unregistered-task", func(_ TaskContext, input taskGreetingInput) (struct{}, error) { return struct{}{}, nil })
	wrongExactType := NewTask("registered-task", func(_ TaskContext, input taskGreetingInputTwin) (struct{}, error) { return struct{}{}, nil })
	application, _ := newTaskTestApp(t, registered)
	_, err := unregistered.Enqueue(context.Background(), application, taskGreetingInput{Name: "Ada"}, TaskEnqueueOptions{})
	assertTaskErrorCode(t, err, TaskErrorNotRegistered)
	_, err = wrongExactType.Enqueue(context.Background(), application, taskGreetingInputTwin{Name: "Ada"}, TaskEnqueueOptions{})
	assertTaskErrorCode(t, err, TaskErrorNotRegistered)
	_, err = registered.Enqueue(context.Background(), application, taskGreetingInput{Name: strings.Repeat("x", MaxTaskPayloadBytes)}, TaskEnqueueOptions{})
	assertTaskErrorCode(t, err, TaskErrorInvalidInput)
}

func TestTypedTaskResultRejectsPersistedOutputOutsideItsExactContract(t *testing.T) {
	task := NewTask("exact-output-task", func(_ TaskContext, _ struct{}) (taskGreetingOutput, error) {
		return taskGreetingOutput{Greeting: "hello"}, nil
	})
	application, backend := newTaskTestApp(t, task)
	receipt, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	if err := backend.CompleteTask(context.Background(), receipt.ID, claimed[0].LeaseToken, json.RawMessage(`{"greeting":"hello","unexpected":true}`)); err != nil {
		t.Fatal(err)
	}
	_, err = task.Result(context.Background(), application, receipt.ID)
	assertTaskErrorCode(t, err, TaskErrorInvalidOutput)
}

func TestScheduledPublishConcurrencyKeyIsBoundedForImportedDocumentIDs(t *testing.T) {
	if got := scheduledPublishConcurrencyKey("posts", "post-1"); got != "posts:post-1" {
		t.Fatalf("ordinary concurrency key = %q", got)
	}
	longID := strings.Repeat("document", 60)
	first := scheduledPublishConcurrencyKey("posts", longID)
	second := scheduledPublishConcurrencyKey("posts", longID)
	other := scheduledPublishConcurrencyKey("posts", longID+"x")
	if len(first) > store.MaxTaskConcurrencyKeyBytes || first != second || first == other || !strings.HasPrefix(first, "sha256-") {
		t.Fatalf("bounded concurrency keys = %q / %q / %q", first, second, other)
	}
}

func TestTaskDocumentReferencesAreAdmittedAndCleanedUpWithHardDelete(t *testing.T) {
	task := NewTask("document-task", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })
	application, _ := newTaskTestApp(t, task)
	target, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("target")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	requester, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("requester")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	targetReference := store.DocumentReference{CollectionID: application.bySlug["posts"].ID, DocumentID: target.ID}
	requesterReference := store.DocumentReference{CollectionID: application.bySlug["posts"].ID, DocumentID: requester.ID}
	receipt, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{Target: &targetReference, RequestedBy: &requesterReference})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", requester.ID, MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = task.Result(context.Background(), application, receipt.ID)
	assertTaskErrorCode(t, err, TaskErrorNotFound)
	missing := store.DocumentReference{CollectionID: targetReference.CollectionID, DocumentID: "missing"}
	_, err = task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{Target: &missing})
	assertTaskErrorCode(t, err, TaskErrorNotFound)
}

func TestTaskCancellationFencesTheActiveLease(t *testing.T) {
	task := NewTask("cancelable-task", func(ctx TaskContext, _ struct{}) (struct{}, error) {
		<-ctx.Context.Done()
		return struct{}{}, ctx.Context.Err()
	})
	application, backend := newTaskTestApp(t, task)
	receipt, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	if err := task.Cancel(context.Background(), application, receipt.ID); err != nil {
		t.Fatal(err)
	}
	if err := backend.CompleteTask(context.Background(), receipt.ID, claimed[0].LeaseToken, []byte(`{}`)); !errors.Is(err, store.ErrTaskLeaseLost) {
		t.Fatalf("stale completion error = %v", err)
	}
	result, err := task.Result(context.Background(), application, receipt.ID)
	if err != nil || result.State != store.TaskStateCanceled {
		t.Fatalf("canceled result = %#v, %v", result, err)
	}
}

func TestRunningHandlerCannotCompleteAfterCancellationBeforeHeartbeat(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	task := NewTask("cancel-race-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
		close(started)
		<-release
		return struct{}{}, nil
	})
	application, _ := newTaskTestApp(t, task)
	receipt, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run := make(chan error, 1)
	go func() {
		_, err := application.runTasks(context.Background(), taskRunOptions{
			limit: 1, leaseDuration: time.Minute, heartbeatInterval: 30 * time.Second,
		})
		run <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("task did not start")
	}
	if err := task.Cancel(context.Background(), application, receipt.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	assertTaskErrorCode(t, <-run, TaskErrorLeaseLost)
	result, err := task.Result(context.Background(), application, receipt.ID)
	if err != nil || result.State != store.TaskStateCanceled || result.LastErrorCode != "" {
		t.Fatalf("canceled result was overwritten = %#v, %v", result, err)
	}
}

func TestTaskHeartbeatPreventsConcurrentCrashRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		var calls atomic.Int32
		task := NewTask("heartbeat-task", func(ctx TaskContext, _ struct{}) (struct{}, error) {
			calls.Add(1)
			close(started)
			select {
			case <-release:
				return struct{}{}, nil
			case <-ctx.Context.Done():
				return struct{}{}, ctx.Context.Err()
			}
		})
		application, backend := newTaskTestApp(t, task)
		if _, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
		type runResult struct {
			summary TaskRunSummary
			err     error
		}
		run := make(chan runResult, 1)
		go func() {
			summary, err := application.runTasks(context.Background(), taskRunOptions{
				limit: 1, leaseDuration: 500 * time.Millisecond, heartbeatInterval: 25 * time.Millisecond,
			})
			run <- runResult{summary: summary, err: err}
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("task did not start")
		}
		time.Sleep(1100 * time.Millisecond)
		claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
		if err != nil || len(claimed) != 0 {
			t.Fatalf("concurrent recovery claim = %#v, %v", claimed, err)
		}
		close(release)
		result := <-run
		if result.err != nil || result.summary.Succeeded != 1 || calls.Load() != 1 {
			t.Fatalf("run = %#v, %v, calls %d", result.summary, result.err, calls.Load())
		}
	})
}

func TestEveryTaskInAClaimedBatchStartsAndHeartbeatsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan int, 2)
		release := make(chan struct{})
		task := NewTask("batch-heartbeat-task", func(ctx TaskContext, input postgresStyleTaskInput) (struct{}, error) {
			started <- input.ID
			select {
			case <-release:
				return struct{}{}, nil
			case <-ctx.Context.Done():
				return struct{}{}, ctx.Context.Err()
			}
		})
		application, backend := newTaskTestApp(t, task)
		for id := 1; id <= 2; id++ {
			if _, err := task.Enqueue(context.Background(), application, postgresStyleTaskInput{ID: id}, TaskEnqueueOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		type runResult struct {
			summary TaskRunSummary
			err     error
		}
		run := make(chan runResult, 1)
		go func() {
			summary, err := application.runTasks(context.Background(), taskRunOptions{
				limit: 2, leaseDuration: 500 * time.Millisecond, heartbeatInterval: 25 * time.Millisecond,
			})
			run <- runResult{summary: summary, err: err}
		}()
		seen := make(map[int]bool)
		for len(seen) != 2 {
			select {
			case id := <-started:
				seen[id] = true
			case <-time.After(time.Second):
				t.Fatalf("only started %#v", seen)
			}
		}
		time.Sleep(1100 * time.Millisecond)
		claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 2, LeaseDuration: time.Minute})
		if err != nil || len(claimed) != 0 {
			t.Fatalf("claimed live batch member = %#v, %v", claimed, err)
		}
		close(release)
		result := <-run
		if result.err != nil || result.summary.Succeeded != 2 {
			t.Fatalf("batch result = %#v, %v", result.summary, result.err)
		}
	})
}

func TestTaskHeartbeatsWhileRegisteredInputValidationRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		task := NewTask("validation-heartbeat-task", func(_ TaskContext, _ struct{}) (struct{}, error) {
			return struct{}{}, nil
		})
		application, backend := newTaskTestApp(t, task)
		if _, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
		runtime := application.taskRegistry[task.TaskSlug()]
		validate := runtime.validate
		started := make(chan struct{})
		runtime.validate = func(input json.RawMessage) (json.RawMessage, error) {
			close(started)
			time.Sleep(1100 * time.Millisecond)
			return validate(input)
		}
		application.taskRegistry[task.TaskSlug()] = runtime
		type runResult struct {
			summary TaskRunSummary
			err     error
		}
		run := make(chan runResult, 1)
		go func() {
			summary, err := application.runTasks(context.Background(), taskRunOptions{
				limit: 1, leaseDuration: 500 * time.Millisecond, heartbeatInterval: 25 * time.Millisecond,
			})
			run <- runResult{summary: summary, err: err}
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("task validation did not start")
		}
		time.Sleep(800 * time.Millisecond)
		claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
		if err != nil || len(claimed) != 0 {
			t.Fatalf("reclaimed task during registered validation = %#v, %v", claimed, err)
		}
		result := <-run
		if result.err != nil || result.summary.Succeeded != 1 {
			t.Fatalf("run = %#v, %v", result.summary, result.err)
		}
	})
}

func TestTaskWorkerCancellationReturnsTheLeaseForImmediateRecovery(t *testing.T) {
	started := make(chan struct{})
	task := NewTask("draining-task", func(ctx TaskContext, _ struct{}) (struct{}, error) {
		close(started)
		<-ctx.Context.Done()
		return struct{}{}, ctx.Context.Err()
	})
	application, backend := newTaskTestApp(t, task)
	receipt, err := task.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	workerContext, cancel := context.WithCancel(context.Background())
	run := make(chan error, 1)
	go func() {
		_, err := application.runTasks(workerContext, taskRunOptions{
			limit: 1, leaseDuration: time.Minute, heartbeatInterval: 10 * time.Millisecond,
		})
		run <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("task did not start")
	}
	cancel()
	if err := <-run; err != nil {
		t.Fatal(err)
	}
	stored, err := backend.FindTask(context.Background(), receipt.ID)
	if err != nil || stored.State != store.TaskStateQueued || stored.LeaseToken != "" || stored.LastErrorCode != "worker_shutdown" {
		t.Fatalf("released task = %#v, %v", stored, err)
	}
	claimed, err := backend.ClaimTasks(context.Background(), store.TaskClaim{Limit: 1, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != receipt.ID {
		t.Fatalf("recovery claim = %#v, %v", claimed, err)
	}
}

func TestTaskWorkerShutdownReleasesWhenHeartbeatObservesParentCancellation(t *testing.T) {
	started := make(chan struct{})
	definition := NewTask("shutdown-heartbeat-race", func(task TaskContext, _ struct{}) (struct{}, error) {
		close(started)
		<-task.Context.Done()
		return struct{}{}, task.Context.Err()
	})
	backend := &shutdownHeartbeatTaskStore{
		Store:            teststore.New(),
		heartbeatStarted: make(chan struct{}),
	}
	application, err := New(taskTestConfig(definition), backend)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := definition.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	workerContext, cancel := context.WithCancel(context.Background())
	run := make(chan error, 1)
	go func() {
		_, err := application.runTasks(workerContext, taskRunOptions{
			limit: 1, leaseDuration: time.Minute, heartbeatInterval: time.Millisecond,
		})
		run <- err
	}()
	for _, signal := range []<-chan struct{}{started, backend.heartbeatStarted} {
		select {
		case <-signal:
		case <-time.After(time.Second):
			t.Fatal("task did not enter the shutdown race")
		}
	}
	cancel()
	if err := <-run; err != nil {
		t.Fatal(err)
	}
	if backend.releases.Load() != 1 {
		t.Fatalf("release calls = %d", backend.releases.Load())
	}
	stored, err := backend.FindTask(context.Background(), receipt.ID)
	if err != nil || stored.State != store.TaskStateQueued || stored.LeaseToken != "" || stored.LastErrorCode != "worker_shutdown" {
		t.Fatalf("released task = %#v, %v", stored, err)
	}
}

func TestTaskOutputAndPersistedErrorsAreBounded(t *testing.T) {
	oversizedOutput := NewTask("oversized-output", func(_ TaskContext, _ struct{}) (string, error) {
		return strings.Repeat("x", MaxTaskPayloadBytes), nil
	})
	oversizedError := NewTask("oversized-error", func(_ TaskContext, _ struct{}) (struct{}, error) {
		return struct{}{}, AbortTask("provider_rejected", errors.New(strings.Repeat("é", maxTaskErrorBytes)))
	})
	application, _ := newTaskTestApp(t, oversizedOutput, oversizedError)
	outputReceipt, err := oversizedOutput.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	errorReceipt, err := oversizedError.Enqueue(context.Background(), application, struct{}{}, TaskEnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := application.RunTasks(context.Background(), 10)
	if err != nil || summary.Failed != 2 {
		t.Fatalf("run = %#v, %v", summary, err)
	}
	outputResult, err := oversizedOutput.Result(context.Background(), application, outputReceipt.ID)
	if err != nil || outputResult.LastErrorCode != "task_output_too_large" {
		t.Fatalf("output result = %#v, %v", outputResult, err)
	}
	errorResult, err := oversizedError.Result(context.Background(), application, errorReceipt.ID)
	if err != nil || errorResult.LastErrorCode != "provider_rejected" || len(errorResult.LastError) > maxTaskErrorBytes || !strings.HasPrefix(errorResult.LastError, "é") {
		t.Fatalf("error result = %#v, %v", errorResult, err)
	}
}

func TestTaskConfigurationRejectsAmbiguousOrUnsafeDefinitions(t *testing.T) {
	nilHandler := NewTask[struct{}, struct{}]("nil-handler", nil)
	valid := NewTask("duplicate-task", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })
	tests := []struct {
		name  string
		tasks []TaskDefinition
		code  string
	}{
		{name: "duplicate", tasks: []TaskDefinition{valid, valid}, code: "duplicate_task_slug"},
		{name: "reserved", tasks: []TaskDefinition{NewTask("ridu-internal", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })}, code: "invalid_task_slug"},
		{name: "nil handler", tasks: []TaskDefinition{nilHandler}, code: "missing_task_handler"},
		{name: "invalid retries", tasks: []TaskDefinition{NewTask("bad-retries", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil }, TaskRetries(0, 0, 0, "unknown"))}, code: "invalid_task_attempts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(taskTestConfig(test.tasks...))
			var validation *schema.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T %v", err, err)
			}
			for _, issue := range validation.Issues {
				if issue.Code == test.code {
					return
				}
			}
			t.Fatalf("issues = %#v, want %s", validation.Issues, test.code)
		})
	}
}

func TestCompiledTaskRegistrationDoesNotEnterTheSchemaManifest(t *testing.T) {
	first := NewTask("first-task", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })
	second := NewTask("second-task", func(_ TaskContext, _ struct{}) (struct{}, error) { return struct{}{}, nil })
	left, err := Resolve(taskTestConfig(first, second))
	if err != nil {
		t.Fatal(err)
	}
	right, err := Resolve(taskTestConfig(second, first))
	if err != nil {
		t.Fatal(err)
	}
	leftBytes, err := left.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := right.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(leftBytes) != string(rightBytes) || strings.Contains(string(leftBytes), "first-task") {
		t.Fatalf("compiled task registry leaked into manifest:\n%s", leftBytes)
	}
	registry, issues := buildTaskRegistry([]TaskDefinition{second, first})
	if len(issues) != 0 || fmt.Sprint(sortedTaskSlugs(registry)) != "[first-task second-task]" {
		t.Fatalf("deterministic registry = %#v, %#v", sortedTaskSlugs(registry), issues)
	}
}

func newTaskTestApp(t *testing.T, definitions ...TaskDefinition) (*App, *teststore.Store) {
	t.Helper()
	backend := teststore.New()
	application, err := New(taskTestConfig(definitions...), backend)
	if err != nil {
		t.Fatal(err)
	}
	return application, backend
}

func taskTestConfig(definitions ...TaskDefinition) Config {
	return Config{
		Name: "durable task test", Tasks: definitions,
		Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
	}
}

func assertTaskErrorCode(t *testing.T, err error, code TaskErrorCode) {
	t.Helper()
	var taskError *TaskError
	if !errors.As(err, &taskError) || taskError.Code != code {
		t.Fatalf("error = %T %v, want task code %s", err, err, code)
	}
}

type shutdownHeartbeatTaskStore struct {
	*teststore.Store
	heartbeatStarted chan struct{}
	heartbeatOnce    sync.Once
	releases         atomic.Int32
}

type panickingCompleteTaskStore struct {
	*teststore.Store
	panicValue any
}

func (backend *panickingCompleteTaskStore) CompleteTask(context.Context, string, string, json.RawMessage) error {
	panic(backend.panicValue)
}

func (backend *shutdownHeartbeatTaskStore) HeartbeatTask(ctx context.Context, _ string, _ string, _ time.Duration) error {
	backend.heartbeatOnce.Do(func() { close(backend.heartbeatStarted) })
	<-ctx.Done()
	return ctx.Err()
}

func (backend *shutdownHeartbeatTaskStore) ReleaseTask(ctx context.Context, id, leaseToken string, delay time.Duration, code, message string) error {
	backend.releases.Add(1)
	return backend.Store.ReleaseTask(ctx, id, leaseToken, delay, code, message)
}
