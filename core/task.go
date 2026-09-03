package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	// MaxTaskPayloadBytes bounds both durable input and output. Large files and
	// exports belong in object storage with a small durable task reference.
	MaxTaskPayloadBytes = store.MaxTaskPayloadBytes
	maxTaskErrorBytes   = store.MaxTaskErrorBytes
	maxTaskListLimit    = store.MaxTaskBatch
	builtinPublishTask  = "ridu-schedule-publish"
)

var (
	errTaskCodecPanicked   = errors.New("task JSON codec panicked")
	errTaskHandlerPanicked = errors.New("task handler panicked")
	errTaskRuntimePanicked = errors.New("task runtime panicked")
)

type TaskBackoff = store.TaskBackoff

const (
	TaskBackoffFixed       = store.TaskBackoffFixed
	TaskBackoffLinear      = store.TaskBackoffLinear
	TaskBackoffExponential = store.TaskBackoffExponential
)

// TaskHandler receives typed data and a request-scoped view of the ordinary
// local operation engine. Handlers must honor Context cancellation. They may
// be invoked more than once after a process crash or lease expiry and must
// therefore make external side effects idempotent.
type TaskHandler[Input, Output any] func(TaskContext, Input) (Output, error)

// TaskReconciler repairs durable application state that should have admitted a
// task but may have missed enqueueing it because the process stopped after the
// state commit. It runs before every task claim cycle, including Execute's
// first worker cycle. Implementations must be bounded, idempotent, and safe to
// run concurrently in multiple application instances.
type TaskReconciler func(context.Context, *App) error

// TaskContext describes one leased attempt. Local is the same access,
// validation, hook, and transaction entry point used by HTTP and application
// code; tasks do not receive a privileged document path.
type TaskContext struct {
	Context context.Context
	ID      string
	Attempt int
	Local   *LocalAPI
	actor   *store.Document
}

type taskConfig struct {
	queue         string
	maxAttempts   int
	retryDelay    time.Duration
	maxRetryDelay time.Duration
	backoff       TaskBackoff
	timeout       time.Duration
	retention     time.Duration
	reconcile     TaskReconciler
}

// TaskOption configures persisted execution policy or runtime-only admission
// recovery for one compiled task definition.
type TaskOption func(*taskConfig)

// TaskQueue selects the default worker queue for this task definition.
func TaskQueue(queue string) TaskOption {
	return func(config *taskConfig) { config.queue = queue }
}

// TaskRetries configures total attempts and deterministic retry delay. The
// first attempt counts toward maxAttempts. maxDelay caps linear/exponential
// growth and must be at least delay.
func TaskRetries(maxAttempts int, delay, maxDelay time.Duration, backoff TaskBackoff) TaskOption {
	return func(config *taskConfig) {
		config.maxAttempts = maxAttempts
		config.retryDelay = delay
		config.maxRetryDelay = maxDelay
		config.backoff = backoff
	}
}

// TaskTimeout limits one handler attempt. Cancellation is cooperative, as it
// is for ordinary Go contexts; a handler that ignores Context can delay drain.
func TaskTimeout(timeout time.Duration) TaskOption {
	return func(config *taskConfig) { config.timeout = timeout }
}

// TaskRetention controls how long terminal task status/output remains
// inspectable before bounded worker pruning removes it.
func TaskRetention(retention time.Duration) TaskOption {
	return func(config *taskConfig) { config.retention = retention }
}

// TaskAdmissionReconciler registers recovery for a domain commit-to-enqueue
// gap. It is runtime-only and is never serialized into the schema manifest or
// persisted task records.
func TaskAdmissionReconciler(reconcile TaskReconciler) TaskOption {
	return func(config *taskConfig) { config.reconcile = reconcile }
}

type taskRuntime struct {
	slug             string
	inputType        reflect.Type
	outputType       reflect.Type
	config           taskConfig
	handlerAvailable bool
	validate         func(json.RawMessage) (json.RawMessage, error)
	run              func(TaskContext, json.RawMessage) (json.RawMessage, error)
}

// TaskDefinition is the non-generic configuration boundary used by Config.
// Only values returned by NewTask can implement it, so serialized input can
// never select or supply executable code.
type TaskDefinition interface {
	TaskSlug() string
	taskRuntime() taskRuntime
}

// TypedTask is one compiled, typed task definition. Add it to Config.Tasks,
// then use Enqueue, Result, and Cancel from trusted application code.
type TypedTask[Input, Output any] struct{ runtime taskRuntime }

// NewTask defines a compiled task handler. Validation is deterministic during
// Resolve/New so the constructor remains convenient in ordinary Go config.
func NewTask[Input, Output any](slug string, handler TaskHandler[Input, Output], options ...TaskOption) TypedTask[Input, Output] {
	config := defaultTaskConfig()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	runtime := taskRuntime{
		slug:             slug,
		inputType:        reflect.TypeOf((*Input)(nil)).Elem(),
		outputType:       reflect.TypeOf((*Output)(nil)).Elem(),
		config:           config,
		handlerAvailable: handler != nil,
	}
	runtime.validate = func(raw json.RawMessage) (canonical json.RawMessage, err error) {
		defer func() {
			if recover() != nil {
				canonical = nil
				err = errTaskCodecPanicked
			}
		}()
		var input Input
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return nil, err
		}
		if err := requireJSONEOF(decoder); err != nil {
			return nil, err
		}
		return json.Marshal(input)
	}
	runtime.run = func(ctx TaskContext, raw json.RawMessage) (encoded json.RawMessage, err error) {
		defer func() {
			if recover() != nil {
				encoded = nil
				err = RetryTask("task_handler_panicked", errTaskHandlerPanicked)
			}
		}()
		if handler == nil {
			return nil, AbortTask("task_handler_unavailable", errors.New("task handler is unavailable"))
		}
		var input Input
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return nil, AbortTask("task_input_invalid", err)
		}
		if err := requireJSONEOF(decoder); err != nil {
			return nil, AbortTask("task_input_invalid", err)
		}
		output, err := handler(ctx, input)
		if err != nil {
			return nil, err
		}
		encoded, err = json.Marshal(output)
		if err != nil {
			return nil, AbortTask("task_output_invalid", err)
		}
		if len(encoded) > MaxTaskPayloadBytes {
			return nil, AbortTask("task_output_too_large", fmt.Errorf("task output exceeds %d bytes", MaxTaskPayloadBytes))
		}
		return encoded, nil
	}
	return TypedTask[Input, Output]{runtime: runtime}
}

func (task TypedTask[Input, Output]) TaskSlug() string         { return task.runtime.slug }
func (task TypedTask[Input, Output]) taskRuntime() taskRuntime { return task.runtime }

// TaskEnqueueOptions schedules one future execution and optionally supplies a
// queue, concurrency key, target, and requesting identity. A concurrency key
// serializes active leases within its queue. Target/requester references are
// lifecycle-safe and are removed if either owning document is hard-deleted.
type TaskEnqueueOptions struct {
	RunAt          time.Time
	Queue          string
	ConcurrencyKey string
	Target         *store.DocumentReference
	RequestedBy    *store.DocumentReference
}

// TaskReceipt is the stable typed handle returned after durable admission.
type TaskReceipt[Output any] struct {
	ID    string
	Slug  string
	Queue string
	RunAt time.Time
}

// TaskResult is a typed local status view. HasOutput distinguishes a valid
// zero/null output from a task that has not succeeded.
type TaskResult[Output any] struct {
	ID            string
	Slug          string
	Queue         string
	State         store.TaskState
	Attempts      int
	MaxAttempts   int
	LastErrorCode string
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
	Output        Output
	HasOutput     bool
}

// Enqueue validates and durably stores typed input. The task must be the same
// slug/type contract registered in the running application's Config.Tasks.
func (task TypedTask[Input, Output]) Enqueue(ctx context.Context, application *App, input Input, options TaskEnqueueOptions) (TaskReceipt[Output], error) {
	if application == nil {
		return TaskReceipt[Output]{}, newTaskError(TaskErrorUnavailable, "task runtime is unavailable", nil)
	}
	encoded, err := marshalTaskJSON(input)
	if err != nil {
		return TaskReceipt[Output]{}, newTaskError(TaskErrorInvalidInput, "task input could not be encoded", err)
	}
	queued, err := application.enqueueTask(ctx, task.runtime, encoded, options)
	if err != nil {
		return TaskReceipt[Output]{}, err
	}
	return TaskReceipt[Output]{ID: queued.ID, Slug: queued.Slug, Queue: queued.Queue, RunAt: queued.RunAt}, nil
}

// Result loads and decodes one task created by this typed definition.
func (task TypedTask[Input, Output]) Result(ctx context.Context, application *App, id string) (TaskResult[Output], error) {
	if application == nil || application.tasks == nil {
		return TaskResult[Output]{}, newTaskError(TaskErrorUnavailable, "task store is unavailable", nil)
	}
	record, err := application.tasks.FindTask(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TaskResult[Output]{}, newTaskError(TaskErrorNotFound, "task was not found", err)
		}
		return TaskResult[Output]{}, newTaskError(TaskErrorStoreFailed, "task could not be loaded", err)
	}
	if record.Slug != task.runtime.slug {
		return TaskResult[Output]{}, newTaskError(TaskErrorNotFound, "task was not found", store.ErrNotFound)
	}
	result := TaskResult[Output]{
		ID: record.ID, Slug: record.Slug, Queue: record.Queue, State: record.State,
		Attempts: record.Attempts, MaxAttempts: record.MaxAttempts,
		LastErrorCode: record.LastErrorCode, LastError: record.LastError,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, CompletedAt: cloneTimePointer(record.CompletedAt),
	}
	if record.State == store.TaskStateSucceeded {
		if err := unmarshalTaskJSON(record.Output, &result.Output); err != nil {
			return TaskResult[Output]{}, newTaskError(TaskErrorInvalidOutput, "task output could not be decoded", err)
		}
		result.HasOutput = true
	}
	return result, nil
}

// Cancel atomically prevents a queued task from starting and fences a running
// attempt so its later heartbeat/completion cannot win.
func (task TypedTask[Input, Output]) Cancel(ctx context.Context, application *App, id string) error {
	if application == nil || application.tasks == nil {
		return newTaskError(TaskErrorUnavailable, "task store is unavailable", nil)
	}
	record, err := application.tasks.FindTask(ctx, id)
	if err != nil || record.Slug != task.runtime.slug {
		if err == nil || errors.Is(err, store.ErrNotFound) {
			return newTaskError(TaskErrorNotFound, "task was not found", store.ErrNotFound)
		}
		return newTaskError(TaskErrorStoreFailed, "task could not be loaded", err)
	}
	if err := application.tasks.CancelTask(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return newTaskError(TaskErrorNotFound, "task was not found", err)
		}
		return newTaskError(TaskErrorStoreFailed, "task could not be canceled", err)
	}
	return nil
}

// TaskErrorCode is stable for local callers and persisted task failures.
type TaskErrorCode string

const (
	TaskErrorNotRegistered TaskErrorCode = "task_not_registered"
	TaskErrorUnavailable   TaskErrorCode = "task_unavailable"
	TaskErrorInvalidInput  TaskErrorCode = "task_input_invalid"
	TaskErrorInvalidOutput TaskErrorCode = "task_output_invalid"
	TaskErrorNotFound      TaskErrorCode = "task_not_found"
	TaskErrorStoreFailed   TaskErrorCode = "task_store_failed"
	TaskErrorLeaseLost     TaskErrorCode = "task_lease_lost"
)

// TaskError is a stable local boundary error.
type TaskError struct {
	Code    TaskErrorCode
	Message string
	Cause   error
}

func (err *TaskError) Error() string { return err.Message }
func (err *TaskError) Unwrap() error { return err.Cause }

func newTaskError(code TaskErrorCode, message string, cause error) error {
	return &TaskError{Code: code, Message: message, Cause: cause}
}

type taskHandlerFailure struct {
	code       string
	cause      error
	retry      bool
	retryAfter time.Duration
}

func (failure *taskHandlerFailure) Error() string {
	if failure.cause == nil {
		return failure.code
	}
	return failure.cause.Error()
}
func (failure *taskHandlerFailure) Unwrap() error { return failure.cause }

// RetryTask marks a handler error retryable with the definition's backoff.
func RetryTask(code string, cause error) error {
	return &taskHandlerFailure{code: normalizeTaskFailureCode(code), cause: cause, retry: true}
}

// RetryTaskAfter marks a handler error retryable after at least delay. This is
// useful for provider rate limits; the persisted definition backoff remains
// the fallback when delay is not positive.
func RetryTaskAfter(code string, cause error, delay time.Duration) error {
	return &taskHandlerFailure{code: normalizeTaskFailureCode(code), cause: cause, retry: true, retryAfter: delay}
}

// AbortTask marks a handler error terminal. It is retained as a dead-letter
// result until the task's configured retention expires.
func AbortTask(code string, cause error) error {
	return &taskHandlerFailure{code: normalizeTaskFailureCode(code), cause: cause}
}

// TaskRunSummary describes one bounded claim cycle. Handler failures are
// persisted as retry/dead-letter state and do not become infrastructure errors.
type TaskRunSummary struct {
	Claimed   int
	Succeeded int
	Retried   int
	Failed    int
	Released  int
	Pruned    int
}

type taskRunOptions struct {
	limit             int
	queues            []string
	slugs             []string
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	fallbackActor     *store.Document
	pruneLimit        int
}

// RunTasks claims and executes one bounded batch. Concurrent callers are safe;
// the store lease is the authority and handlers remain at-least-once.
func (application *App) RunTasks(ctx context.Context, limit int) (TaskRunSummary, error) {
	return application.runTasks(ctx, taskRunOptions{limit: limit})
}

func (application *App) runTasks(ctx context.Context, options taskRunOptions) (TaskRunSummary, error) {
	if application.tasks == nil {
		return TaskRunSummary{}, newTaskError(TaskErrorUnavailable, "store does not support durable tasks", nil)
	}
	if options.limit <= 0 {
		options.limit = 20
	}
	if options.limit > maxTaskListLimit {
		options.limit = maxTaskListLimit
	}
	if options.leaseDuration <= 0 {
		options.leaseDuration = 30 * time.Second
	}
	if options.leaseDuration > store.MaxTaskLeaseDuration {
		options.leaseDuration = store.MaxTaskLeaseDuration
	}
	options.leaseDuration = ceilTaskMilliseconds(options.leaseDuration)
	if options.heartbeatInterval <= 0 || options.heartbeatInterval >= options.leaseDuration {
		options.heartbeatInterval = options.leaseDuration / 3
		if options.heartbeatInterval <= 0 {
			options.heartbeatInterval = time.Nanosecond
		}
	}
	reconcileErrors := application.reconcileTaskAdmissions(ctx, options)
	claimed, err := application.tasks.ClaimTasks(ctx, store.TaskClaim{
		Limit: options.limit, Queues: append([]string(nil), options.queues...), Slugs: append([]string(nil), options.slugs...),
		LeaseDuration: options.leaseDuration,
	})
	if err != nil {
		reconcileErrors = append(reconcileErrors, newTaskError(TaskErrorStoreFailed, "durable tasks could not be claimed", err))
		return TaskRunSummary{}, errors.Join(reconcileErrors...)
	}
	summary := TaskRunSummary{Claimed: len(claimed)}
	type executionResult struct {
		outcome store.TaskState
		err     error
	}
	results := make(chan executionResult, len(claimed))
	for _, record := range claimed {
		record := record
		go func() {
			result := executionResult{}
			defer func() {
				if recover() != nil {
					result = executionResult{outcome: store.TaskStateRunning, err: errTaskRuntimePanicked}
				}
				results <- result
			}()
			result.outcome, result.err = application.executeTask(ctx, record, options)
		}()
	}
	executionErrors := reconcileErrors
	for range claimed {
		result := <-results
		switch result.outcome {
		case store.TaskStateSucceeded:
			summary.Succeeded++
		case store.TaskStateQueued:
			summary.Retried++
		case store.TaskStateFailed:
			summary.Failed++
		default:
			if result.err == nil {
				summary.Released++
			}
		}
		if result.err != nil {
			executionErrors = append(executionErrors, result.err)
		}
	}
	if len(executionErrors) != 0 {
		return summary, errors.Join(executionErrors...)
	}
	if options.pruneLimit > 0 {
		if options.pruneLimit > store.MaxTaskBatch {
			options.pruneLimit = store.MaxTaskBatch
		}
		pruned, err := application.tasks.PruneTasks(ctx, options.pruneLimit)
		if err != nil {
			return summary, newTaskError(TaskErrorStoreFailed, "terminal tasks could not be pruned", err)
		}
		summary.Pruned = pruned
	}
	return summary, nil
}

func (application *App) reconcileTaskAdmissions(ctx context.Context, options taskRunOptions) []error {
	var reconcileErrors []error
	for _, slug := range sortedTaskSlugs(application.taskRegistry) {
		runtime := application.taskRegistry[slug]
		if runtime.config.reconcile == nil || !taskRuntimeSelected(runtime, options) {
			continue
		}
		if err := runTaskReconciler(ctx, application, runtime.config.reconcile); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("reconcile task %q admission: %w", slug, err))
		}
	}
	return reconcileErrors
}

func taskRuntimeSelected(runtime taskRuntime, options taskRunOptions) bool {
	if len(options.slugs) != 0 && !slices.Contains(options.slugs, runtime.slug) {
		return false
	}
	return len(options.queues) == 0 || slices.Contains(options.queues, runtime.config.queue)
}

func runTaskReconciler(ctx context.Context, application *App, reconcile TaskReconciler) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("task admission reconciler panic recovered")
		}
	}()
	return reconcile(ctx, application)
}

func (application *App) executeTask(ctx context.Context, record store.Task, options taskRunOptions) (store.TaskState, error) {
	runtime, registered := application.taskRegistry[record.Slug]
	if !registered {
		return store.TaskStateFailed, application.failClaimedTask(ctx, record, "task_not_registered", "task handler is not registered in this application", nil)
	}
	if record.Attempts > record.MaxAttempts {
		return store.TaskStateFailed, application.failClaimedTask(ctx, record, "task_attempts_exhausted", "task attempts were exhausted during lease recovery", nil)
	}
	handlerContext := ctx
	var cancelHandler context.CancelFunc
	if record.Timeout > 0 {
		handlerContext, cancelHandler = context.WithTimeout(ctx, record.Timeout)
	} else {
		handlerContext, cancelHandler = context.WithCancel(ctx)
	}
	defer cancelHandler()
	heartbeatContext, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				cancelHandler()
				heartbeatDone <- errTaskRuntimePanicked
			}
		}()
		ticker := time.NewTicker(options.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatContext.Done():
				heartbeatDone <- nil
				return
			case <-ticker.C:
				if err := application.tasks.HeartbeatTask(heartbeatContext, record.ID, record.LeaseToken, options.leaseDuration); err != nil {
					cancelHandler()
					heartbeatDone <- err
					return
				}
			}
		}
	}()
	heartbeatStopped := false
	stopHeartbeat := func() error {
		if heartbeatStopped {
			return nil
		}
		heartbeatStopped = true
		cancelHeartbeat()
		return <-heartbeatDone
	}
	defer stopHeartbeat()
	heartbeatFailure := func() error {
		if heartbeatStopped {
			return nil
		}
		select {
		case err := <-heartbeatDone:
			heartbeatStopped = true
			return err
		default:
			return nil
		}
	}
	canonical, err := runtime.validate(record.Input)
	if err != nil || len(canonical) > MaxTaskPayloadBytes {
		if ctx.Err() != nil {
			_ = stopHeartbeat()
			return store.TaskStateRunning, application.releaseClaimedTask(record, "worker_shutdown", "worker stopped before task completion")
		}
		if heartbeatError := heartbeatFailure(); heartbeatError != nil {
			return store.TaskStateRunning, newTaskError(TaskErrorLeaseLost, "durable task lease was lost", heartbeatError)
		}
		message := "durable task input is invalid"
		if len(canonical) > MaxTaskPayloadBytes {
			message = fmt.Sprintf("durable task input exceeds %d bytes", MaxTaskPayloadBytes)
		}
		mutationError := application.failClaimedTask(ctx, record, "task_input_invalid", message, nil)
		if errors.Is(err, errTaskCodecPanicked) {
			return store.TaskStateFailed, errors.Join(mutationError, errTaskCodecPanicked)
		}
		return store.TaskStateFailed, mutationError
	}

	var output json.RawMessage
	var handlerError error
	if handlerContext.Err() != nil {
		handlerError = handlerContext.Err()
	} else {
		output, handlerError = runtime.run(TaskContext{Context: handlerContext, ID: record.ID, Attempt: record.Attempts, Local: application.local, actor: cloneDocument(options.fallbackActor)}, canonical)
	}
	if ctx.Err() != nil {
		_ = stopHeartbeat()
		return store.TaskStateRunning, application.releaseClaimedTask(record, "worker_shutdown", "worker stopped before task completion")
	}
	if heartbeatError := heartbeatFailure(); heartbeatError != nil {
		return store.TaskStateRunning, newTaskError(TaskErrorLeaseLost, "durable task lease was lost", heartbeatError)
	}
	if handlerError == nil {
		if len(output) > MaxTaskPayloadBytes {
			handlerError = AbortTask("task_output_too_large", fmt.Errorf("task output exceeds %d bytes", MaxTaskPayloadBytes))
		} else if err := application.tasks.CompleteTask(ctx, record.ID, record.LeaseToken, output); err != nil {
			return store.TaskStateRunning, taskMutationError("durable task could not be completed", err)
		} else {
			return store.TaskStateSucceeded, nil
		}
	}

	code, message, retry, overrideDelay, diagnostic := classifyTaskFailure(handlerError)
	if diagnostic == nil && errors.Is(handlerContext.Err(), context.DeadlineExceeded) {
		code, message, retry = "task_timeout", "task attempt exceeded its timeout", true
	}
	if record.Attempts >= record.MaxAttempts {
		retry = false
	}
	var retryAfter *time.Duration
	if retry {
		delay := taskRetryDelay(record, overrideDelay)
		retryAfter = &delay
	}
	if err := application.tasks.FailTask(ctx, store.TaskFailure{ID: record.ID, LeaseToken: record.LeaseToken, Code: code, Message: message, RetryAfter: retryAfter}); err != nil {
		return store.TaskStateRunning, taskMutationError("durable task failure could not be recorded", err)
	}
	outcome := store.TaskStateFailed
	if retryAfter != nil {
		outcome = store.TaskStateQueued
	}
	if diagnostic != nil {
		return outcome, diagnostic
	}
	return outcome, nil
}

func (application *App) failClaimedTask(ctx context.Context, record store.Task, code, message string, retryAfter *time.Duration) error {
	if err := application.tasks.FailTask(ctx, store.TaskFailure{ID: record.ID, LeaseToken: record.LeaseToken, Code: code, Message: boundedTaskError(message), RetryAfter: retryAfter}); err != nil {
		return taskMutationError("durable task failure could not be recorded", err)
	}
	return nil
}

func (application *App) releaseClaimedTask(record store.Task, code, message string) error {
	releaseContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.tasks.ReleaseTask(releaseContext, record.ID, record.LeaseToken, 0, code, boundedTaskError(message)); err != nil {
		return taskMutationError("durable task could not be released during worker shutdown", err)
	}
	return nil
}

func taskMutationError(message string, err error) error {
	if errors.Is(err, store.ErrTaskLeaseLost) {
		return newTaskError(TaskErrorLeaseLost, "durable task lease was lost", err)
	}
	return newTaskError(TaskErrorStoreFailed, message, err)
}

func (application *App) enqueueTask(ctx context.Context, definition taskRuntime, input json.RawMessage, options TaskEnqueueOptions) (store.Task, error) {
	if application.tasks == nil {
		return store.Task{}, newTaskError(TaskErrorUnavailable, "store does not support durable tasks", nil)
	}
	registered, exists := application.taskRegistry[definition.slug]
	if !exists || registered.inputType != definition.inputType || registered.outputType != definition.outputType {
		return store.Task{}, newTaskError(TaskErrorNotRegistered, "task is not registered in this application", nil)
	}
	canonical, err := registered.validate(input)
	if err != nil {
		return store.Task{}, newTaskError(TaskErrorInvalidInput, "task input does not match its registered type", err)
	}
	if len(canonical) > MaxTaskPayloadBytes {
		return store.Task{}, newTaskError(TaskErrorInvalidInput, fmt.Sprintf("task input exceeds %d bytes", MaxTaskPayloadBytes), nil)
	}
	queue := registered.config.queue
	if options.Queue != "" {
		queue = options.Queue
	}
	if !validTaskIdentifier(queue, 64) {
		return store.Task{}, newTaskError(TaskErrorInvalidInput, "task queue must be lowercase kebab-case and at most 64 bytes", nil)
	}
	if options.ConcurrencyKey != "" && (!utf8.ValidString(options.ConcurrencyKey) || strings.ContainsRune(options.ConcurrencyKey, '\x00') || len(options.ConcurrencyKey) > store.MaxTaskConcurrencyKeyBytes || strings.TrimSpace(options.ConcurrencyKey) != options.ConcurrencyKey) {
		return store.Task{}, newTaskError(TaskErrorInvalidInput, fmt.Sprintf("task concurrency key must be valid trimmed UTF-8 and at most %d bytes", store.MaxTaskConcurrencyKeyBytes), nil)
	}
	for _, reference := range []*store.DocumentReference{options.Target, options.RequestedBy} {
		if reference != nil {
			if err := store.ValidateTaskReference(*reference); err != nil {
				return store.Task{}, newTaskError(TaskErrorInvalidInput, "task document references require valid collection and document IDs", err)
			}
		}
	}
	runAt := options.RunAt.UTC()
	if runAt.IsZero() {
		runAt = time.Now().UTC()
	}
	record := store.Task{
		Slug: definition.slug, Queue: queue, ConcurrencyKey: options.ConcurrencyKey,
		Input: canonical, State: store.TaskStateQueued, RunAt: runAt,
		MaxAttempts: registered.config.maxAttempts, RetryDelay: registered.config.retryDelay,
		MaxRetryDelay: registered.config.maxRetryDelay, Backoff: registered.config.backoff,
		Timeout: registered.config.timeout, Retention: registered.config.retention,
		Target: cloneDocumentReference(options.Target), RequestedBy: cloneDocumentReference(options.RequestedBy),
	}
	queued, err := application.tasks.EnqueueTask(ctx, record)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Task{}, newTaskError(TaskErrorNotFound, "task document reference was not found", err)
		}
		return store.Task{}, newTaskError(TaskErrorStoreFailed, "task could not be enqueued", err)
	}
	return queued, nil
}

func defaultTaskConfig() taskConfig {
	return taskConfig{
		queue: "default", maxAttempts: 3, retryDelay: time.Second,
		maxRetryDelay: time.Hour, backoff: TaskBackoffExponential,
		timeout: 5 * time.Minute, retention: 7 * 24 * time.Hour,
	}
}

func buildTaskRegistry(definitions []TaskDefinition) (map[string]taskRuntime, []schema.Issue) {
	registry := make(map[string]taskRuntime, len(definitions)+1)
	var issues []schema.Issue
	for index, definition := range definitions {
		path := fmt.Sprintf("tasks[%d]", index)
		if isNilTaskDefinition(definition) {
			issues = append(issues, schema.Issue{Code: "nil_task", Path: path, Message: "task definition must not be nil"})
			continue
		}
		runtime := definition.taskRuntime()
		if !validTaskIdentifier(runtime.slug, 128) || strings.HasPrefix(runtime.slug, "ridu-") {
			issues = append(issues, schema.Issue{Code: "invalid_task_slug", Path: path + ".slug", Message: "task slug must be lowercase kebab-case, at most 128 bytes, and must not use the reserved ridu- prefix"})
		}
		if _, exists := registry[runtime.slug]; exists {
			issues = append(issues, schema.Issue{Code: "duplicate_task_slug", Path: path + ".slug", Message: fmt.Sprintf("task %q is already registered", runtime.slug)})
		} else {
			registry[runtime.slug] = runtime
		}
		if !runtime.handlerAvailable || runtime.run == nil || runtime.validate == nil {
			issues = append(issues, schema.Issue{Code: "missing_task_handler", Path: path + ".handler", Message: "task requires a compiled handler"})
		}
		validateTaskConfig(path, runtime.config, &issues)
	}
	return registry, issues
}

func validateTaskConfig(path string, config taskConfig, issues *[]schema.Issue) {
	if !validTaskIdentifier(config.queue, 64) {
		*issues = append(*issues, schema.Issue{Code: "invalid_task_queue", Path: path + ".queue", Message: "task queue must be lowercase kebab-case and at most 64 bytes"})
	}
	if config.maxAttempts < 1 || config.maxAttempts > store.MaxTaskAttempts {
		*issues = append(*issues, schema.Issue{Code: "invalid_task_attempts", Path: path + ".maxAttempts", Message: "task max attempts must be between 1 and 100"})
	}
	if config.retryDelay%time.Millisecond != 0 || config.maxRetryDelay%time.Millisecond != 0 || config.retryDelay < time.Millisecond || config.maxRetryDelay < config.retryDelay || config.maxRetryDelay > store.MaxTaskRetryDelay {
		*issues = append(*issues, schema.Issue{Code: "invalid_task_retry_delay", Path: path + ".retry", Message: "task retry delays must use whole milliseconds and max delay must be between one millisecond and 30 days"})
	}
	if config.backoff != TaskBackoffFixed && config.backoff != TaskBackoffLinear && config.backoff != TaskBackoffExponential {
		*issues = append(*issues, schema.Issue{Code: "invalid_task_backoff", Path: path + ".backoff", Message: "task backoff must be fixed, linear, or exponential"})
	}
	if config.timeout%time.Millisecond != 0 || config.timeout < time.Millisecond || config.timeout > store.MaxTaskTimeout {
		*issues = append(*issues, schema.Issue{Code: "invalid_task_timeout", Path: path + ".timeout", Message: "task timeout must use whole milliseconds between one millisecond and 24 hours"})
	}
	if config.retention%time.Millisecond != 0 || config.retention < store.MinTaskRetention || config.retention > store.MaxTaskRetention {
		*issues = append(*issues, schema.Issue{Code: "invalid_task_retention", Path: path + ".retention", Message: "task retention must use whole milliseconds between one hour and one year"})
	}
}

func isNilTaskDefinition(definition TaskDefinition) bool {
	if definition == nil {
		return true
	}
	value := reflect.ValueOf(definition)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("task input contains multiple JSON values")
		}
		return err
	}
	return nil
}

func marshalTaskJSON(value any) (encoded []byte, err error) {
	defer func() {
		if recover() != nil {
			encoded = nil
			err = errTaskCodecPanicked
		}
	}()
	return json.Marshal(value)
}

func unmarshalTaskJSON(encoded []byte, target any) (err error) {
	defer func() {
		if recover() != nil {
			err = errTaskCodecPanicked
		}
	}()
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func validTaskIdentifier(value string, max int) bool {
	return value != "" && len(value) <= max && schema.IsValidCollectionSlug(value)
}

func normalizeTaskFailureCode(code string) string {
	code = strings.TrimSpace(code)
	if !validTaskFailureCode(code) {
		return "task_failed"
	}
	return code
}

func validTaskFailureCode(code string) bool {
	if code == "" || len(code) > 128 {
		return false
	}
	for index, candidate := range []byte(code) {
		letter := candidate >= 'a' && candidate <= 'z'
		digit := candidate >= '0' && candidate <= '9'
		separator := candidate == '_' || candidate == '-'
		if !letter && !(index > 0 && (digit || separator)) {
			return false
		}
	}
	return true
}

func classifyTaskFailure(err error) (code, message string, retry bool, retryAfter time.Duration, diagnostic error) {
	code, retry = "task_failed", true
	defer func() {
		if recover() != nil {
			code, message, retry, retryAfter = "task_handler_panicked", errTaskHandlerPanicked.Error(), true, 0
			diagnostic = errTaskHandlerPanicked
		}
	}()
	var classified *taskHandlerFailure
	if errors.As(err, &classified) {
		code, retry, retryAfter = classified.code, classified.retry, classified.retryAfter
	}
	if errors.Is(err, errTaskHandlerPanicked) {
		diagnostic = errTaskHandlerPanicked
	}
	if err != nil {
		message = boundedTaskError(err.Error())
	}
	if message == "" {
		message = code
	}
	return
}

func taskRetryDelay(task store.Task, override time.Duration) time.Duration {
	if override > 0 {
		if override > task.MaxRetryDelay {
			return task.MaxRetryDelay
		}
		return min(ceilTaskMilliseconds(override), task.MaxRetryDelay)
	}
	delay := task.RetryDelay
	switch task.Backoff {
	case store.TaskBackoffLinear:
		if task.Attempts > 1 && delay <= time.Duration(math.MaxInt64/int64(task.Attempts)) {
			delay *= time.Duration(task.Attempts)
		}
	case store.TaskBackoffExponential:
		for attempt := 1; attempt < task.Attempts && delay < task.MaxRetryDelay; attempt++ {
			if delay > task.MaxRetryDelay/2 {
				delay = task.MaxRetryDelay
				break
			}
			delay *= 2
		}
	}
	if delay > task.MaxRetryDelay {
		return task.MaxRetryDelay
	}
	return delay
}

func boundedTaskError(message string) string {
	message = strings.ReplaceAll(strings.ToValidUTF8(message, "�"), "\x00", "�")
	if len(message) <= maxTaskErrorBytes {
		return message
	}
	message = message[:maxTaskErrorBytes]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}

func ceilTaskMilliseconds(duration time.Duration) time.Duration {
	if duration <= 0 || duration%time.Millisecond == 0 {
		return duration
	}
	return duration + time.Millisecond - duration%time.Millisecond
}

func cloneDocumentReference(reference *store.DocumentReference) *store.DocumentReference {
	if reference == nil {
		return nil
	}
	cloned := *reference
	return &cloned
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sortedTaskSlugs(registry map[string]taskRuntime) []string {
	result := make([]string, 0, len(registry))
	for slug := range registry {
		result = append(result, slug)
	}
	sort.Strings(result)
	return result
}
