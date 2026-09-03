package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
)

const (
	MaxTaskPayloadBytes        = 1 << 20
	MaxTaskErrorBytes          = 4096
	MaxTaskBatch               = 500
	MaxTaskAttempts            = 100
	MaxTaskConcurrencyKeyBytes = 256
	MaxTaskReferenceIDBytes    = MaxDocumentIDBytes
	MaxTaskRetryDelay          = 30 * 24 * time.Hour
	MaxTaskTimeout             = 24 * time.Hour
	MinTaskRetention           = time.Hour
	MaxTaskRetention           = 365 * 24 * time.Hour
	MaxTaskLeaseDuration       = 24 * time.Hour
)

// ValidateTaskAdmission is the adapter-neutral validation boundary for a new
// durable record. Store-owned lifecycle fields are intentionally ignored
// because EnqueueTask initializes them atomically.
func ValidateTaskAdmission(task Task) error {
	if !validTaskIdentifier(task.Slug, 128) {
		return fmt.Errorf("task slug must be lowercase kebab-case and at most 128 bytes")
	}
	if !validTaskIdentifier(task.Queue, 64) {
		return fmt.Errorf("task queue must be lowercase kebab-case and at most 64 bytes")
	}
	if task.ConcurrencyKey != "" && !validTaskText(task.ConcurrencyKey, MaxTaskConcurrencyKeyBytes, true) {
		return fmt.Errorf("task concurrency key must be valid trimmed UTF-8 and at most %d bytes", MaxTaskConcurrencyKeyBytes)
	}
	if err := ValidateTaskPayload(task.Input); err != nil {
		return fmt.Errorf("task input: %w", err)
	}
	if task.RunAt.IsZero() {
		return fmt.Errorf("task run time is required")
	}
	if task.MaxAttempts < 1 || task.MaxAttempts > MaxTaskAttempts {
		return fmt.Errorf("task max attempts must be between 1 and %d", MaxTaskAttempts)
	}
	if !wholeTaskMilliseconds(task.RetryDelay) || !wholeTaskMilliseconds(task.MaxRetryDelay) ||
		task.RetryDelay < time.Millisecond || task.MaxRetryDelay < task.RetryDelay || task.MaxRetryDelay > MaxTaskRetryDelay {
		return fmt.Errorf("task retry delays must be whole milliseconds between one millisecond and 30 days")
	}
	if task.Backoff != TaskBackoffFixed && task.Backoff != TaskBackoffLinear && task.Backoff != TaskBackoffExponential {
		return fmt.Errorf("task backoff must be fixed, linear, or exponential")
	}
	if !wholeTaskMilliseconds(task.Timeout) || task.Timeout < time.Millisecond || task.Timeout > MaxTaskTimeout {
		return fmt.Errorf("task timeout must be whole milliseconds between one millisecond and 24 hours")
	}
	if !wholeTaskMilliseconds(task.Retention) || task.Retention < MinTaskRetention || task.Retention > MaxTaskRetention {
		return fmt.Errorf("task retention must be whole milliseconds between one hour and one year")
	}
	for _, reference := range []*DocumentReference{task.Target, task.RequestedBy} {
		if reference != nil {
			if err := ValidateTaskReference(*reference); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateTaskPayload bounds one persisted JSON input or output.
func ValidateTaskPayload(payload json.RawMessage) error {
	if len(payload) == 0 || len(payload) > MaxTaskPayloadBytes {
		return fmt.Errorf("JSON payload must contain between 1 and %d bytes", MaxTaskPayloadBytes)
	}
	if !json.Valid(payload) {
		return fmt.Errorf("JSON payload is malformed")
	}
	return nil
}

// ValidateTaskClaim bounds one worker admission request.
func ValidateTaskClaim(request TaskClaim) error {
	if request.Limit < 1 || request.Limit > MaxTaskBatch {
		return fmt.Errorf("task claim limit must be between 1 and %d", MaxTaskBatch)
	}
	if err := ValidateTaskLeaseDuration(request.LeaseDuration); err != nil {
		return err
	}
	if len(request.Queues) > MaxTaskBatch || len(request.Slugs) > MaxTaskBatch {
		return fmt.Errorf("task claim filters must contain at most %d values", MaxTaskBatch)
	}
	for _, queue := range request.Queues {
		if !validTaskIdentifier(queue, 64) {
			return fmt.Errorf("task claim contains an invalid queue")
		}
	}
	for _, slug := range request.Slugs {
		if !validTaskIdentifier(slug, 128) {
			return fmt.Errorf("task claim contains an invalid slug")
		}
	}
	return nil
}

// ValidateTaskList bounds one adapter-neutral inspection request.
func ValidateTaskList(request TaskList) error {
	if request.Limit < 1 || request.Limit > MaxTaskBatch {
		return fmt.Errorf("task list limit must be between 1 and %d", MaxTaskBatch)
	}
	if request.Slug != "" && !validTaskIdentifier(request.Slug, 128) {
		return fmt.Errorf("task list contains an invalid slug")
	}
	if request.Target != nil {
		if err := ValidateTaskReference(*request.Target); err != nil {
			return err
		}
	}
	if len(request.States) > 5 {
		return fmt.Errorf("task list contains too many states")
	}
	for _, state := range request.States {
		if !validTaskState(state) {
			return fmt.Errorf("task list contains an invalid state")
		}
	}
	return nil
}

// ValidateTaskReference bounds a document reference admitted to task state.
func ValidateTaskReference(reference DocumentReference) error {
	if !schema.IsValidStableID(string(reference.CollectionID)) ||
		!validTaskText(reference.DocumentID, MaxTaskReferenceIDBytes, false) || reference.DocumentID == "" {
		return fmt.Errorf("task document references require valid collection and document IDs")
	}
	return nil
}

// ValidateTaskBatch bounds a prune or other adapter batch.
func ValidateTaskBatch(limit int) error {
	if limit < 1 || limit > MaxTaskBatch {
		return fmt.Errorf("task batch limit must be between 1 and %d", MaxTaskBatch)
	}
	return nil
}

// ValidateTaskLeaseDuration prevents a malformed worker setting from creating
// a zero, truncated, or operationally unbounded lease.
func ValidateTaskLeaseDuration(duration time.Duration) error {
	if !wholeTaskMilliseconds(duration) || duration < time.Millisecond || duration > MaxTaskLeaseDuration {
		return fmt.Errorf("task lease must be whole milliseconds between one millisecond and 24 hours")
	}
	return nil
}

// ValidateTaskFailure bounds values persisted for one failed attempt.
func ValidateTaskFailure(failure TaskFailure) error {
	if !validTaskFailureCode(failure.Code) {
		return fmt.Errorf("task failure code is malformed")
	}
	if !validTaskText(failure.Message, MaxTaskErrorBytes, false) {
		return fmt.Errorf("task failure message must be valid UTF-8 and at most %d bytes", MaxTaskErrorBytes)
	}
	if failure.RetryAfter != nil && (!wholeTaskMilliseconds(*failure.RetryAfter) || *failure.RetryAfter < time.Millisecond || *failure.RetryAfter > MaxTaskRetryDelay) {
		return fmt.Errorf("task retry delay must be whole milliseconds between one millisecond and 30 days")
	}
	return nil
}

// ValidateTaskRelease bounds one cooperative worker release.
func ValidateTaskRelease(delay time.Duration, code, message string) error {
	if !wholeTaskMilliseconds(delay) || delay < 0 || delay > MaxTaskRetryDelay {
		return fmt.Errorf("task release delay must be whole milliseconds between zero and 30 days")
	}
	return ValidateTaskFailure(TaskFailure{Code: code, Message: message})
}

func validTaskIdentifier(value string, max int) bool {
	return value != "" && len(value) <= max && schema.IsValidCollectionSlug(value)
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

func validTaskText(value string, max int, trimmed bool) bool {
	return len(value) <= max && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00') && (!trimmed || strings.TrimSpace(value) == value)
}

func wholeTaskMilliseconds(duration time.Duration) bool {
	return duration%time.Millisecond == 0
}

func validTaskState(state TaskState) bool {
	switch state {
	case TaskStateQueued, TaskStateRunning, TaskStateSucceeded, TaskStateFailed, TaskStateCanceled:
		return true
	default:
		return false
	}
}
