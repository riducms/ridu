package mongodb

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	mongoTaskCollectionName            = "z_ridu_tasks"
	mongoTaskConcurrencyCollectionName = "z_ridu_task_concurrency"
	mongoTaskCodecVersion              = int32(1)
	mongoTaskIDPrefix                  = "task"
	mongoTaskLeasePrefix               = "lease"
)

var mongoTaskExactKeys = []string{
	"_id", "codec", "slug", "queue", "concurrencyKey", "concurrencyID",
	"input", "output", "state", "runAt", "attempts", "maxAttempts",
	"retryDelay", "maxRetryDelay", "backoff", "timeout", "retention",
	"leaseToken", "leaseExpiresAt", "target", "requestedBy", "lastErrorCode",
	"lastError", "createdAt", "updatedAt", "completedAt", "retainUntil",
}

var mongoTaskConcurrencyExactKeys = []string{
	"_id", "codec", "queue", "key", "task", "token", "expiresAt", "updatedAt",
	"target", "requestedBy",
}

type mongoTaskConcurrencyGuard struct {
	ID          string
	Queue       string
	Key         string
	TaskID      string
	Token       string
	ExpiresAt   time.Time
	UpdatedAt   time.Time
	Target      *store.DocumentReference
	RequestedBy *store.DocumentReference
}

func newMongoTaskIdentifier(prefix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate MongoDB %s ID: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(random[:]), nil
}

func validMongoTaskIdentifier(value, prefix string) bool {
	wantPrefix := prefix + "_"
	if !strings.HasPrefix(value, wantPrefix) || len(value) != len(wantPrefix)+24 {
		return false
	}
	raw := value[len(wantPrefix):]
	decoded, err := hex.DecodeString(raw)
	return err == nil && hex.EncodeToString(decoded) == raw
}

func mongoTaskConcurrencyID(queue, key string) string {
	if key == "" {
		return ""
	}
	return mongoSystemRecordID("task_concurrency", queue, key)
}

func normalizeMongoTaskAdmission(task store.Task) (store.Task, error) {
	if err := store.ValidateTaskAdmission(task); err != nil {
		return store.Task{}, err
	}
	runAt, _, err := normalizeMongoSystemTime(task.RunAt, "task run time")
	if err != nil {
		return store.Task{}, err
	}
	task.RunAt = runAt
	task.State = store.TaskStateQueued
	task.Attempts = 0
	task.Output = nil
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.LastErrorCode = ""
	task.LastError = ""
	task.CreatedAt = time.Time{}
	task.UpdatedAt = time.Time{}
	task.CompletedAt = nil
	task.RetainUntil = nil
	task.Input = append(json.RawMessage(nil), task.Input...)
	task.Target = cloneMongoTaskReference(task.Target)
	task.RequestedBy = cloneMongoTaskReference(task.RequestedBy)
	return task, nil
}

func validateMongoStoredTask(task store.Task) error {
	if !validMongoTaskIdentifier(task.ID, mongoTaskIDPrefix) {
		return fmt.Errorf("stored MongoDB task has an invalid ID")
	}
	if err := store.ValidateTaskAdmission(task); err != nil {
		return fmt.Errorf("stored MongoDB task admission: %w", err)
	}
	if _, err := encodeTime(task.RunAt); err != nil {
		return fmt.Errorf("stored MongoDB task has invalid runAt")
	}
	if task.Attempts < 0 {
		return fmt.Errorf("stored MongoDB task has invalid attempts")
	}
	if _, err := encodeTime(task.CreatedAt); err != nil || task.CreatedAt.IsZero() {
		return fmt.Errorf("stored MongoDB task has invalid createdAt")
	}
	if _, err := encodeTime(task.UpdatedAt); err != nil || task.UpdatedAt.IsZero() {
		return fmt.Errorf("stored MongoDB task has invalid updatedAt")
	}
	if task.UpdatedAt.Before(task.CreatedAt) {
		return fmt.Errorf("stored MongoDB task has invalid update chronology")
	}
	if task.LastErrorCode == "" && task.LastError != "" {
		return fmt.Errorf("stored MongoDB task has an invalid error pair")
	}
	if task.LastErrorCode != "" {
		if err := store.ValidateTaskFailure(store.TaskFailure{Code: task.LastErrorCode, Message: task.LastError}); err != nil {
			return fmt.Errorf("stored MongoDB task failure: %w", err)
		}
	}
	if task.ConcurrencyKey == "" {
		// The persisted concurrencyID is checked separately during decode.
	} else if !validMongoTaskConcurrencyIdentity(task.Queue, task.ConcurrencyKey) {
		return fmt.Errorf("stored MongoDB task has invalid concurrency identity")
	}

	running := task.State == store.TaskStateRunning
	if running != (task.LeaseToken != "" && task.LeaseExpiresAt != nil) {
		return fmt.Errorf("stored MongoDB task has an invalid lease lifecycle")
	}
	if !running && (task.LeaseToken != "" || task.LeaseExpiresAt != nil) {
		return fmt.Errorf("stored MongoDB task has an invalid lease lifecycle")
	}
	if task.LeaseToken != "" && !validMongoTaskIdentifier(task.LeaseToken, mongoTaskLeasePrefix) {
		return fmt.Errorf("stored MongoDB task has an invalid lease token")
	}
	if task.LeaseExpiresAt != nil {
		if _, err := encodeTime(*task.LeaseExpiresAt); err != nil {
			return fmt.Errorf("stored MongoDB task has invalid leaseExpiresAt")
		}
		if !task.LeaseExpiresAt.After(task.UpdatedAt) {
			return fmt.Errorf("stored MongoDB task has invalid lease chronology")
		}
	}

	terminal := task.State == store.TaskStateSucceeded || task.State == store.TaskStateFailed || task.State == store.TaskStateCanceled
	if terminal != (task.CompletedAt != nil && task.RetainUntil != nil) {
		return fmt.Errorf("stored MongoDB task has an invalid terminal lifecycle")
	}
	if !terminal && (task.CompletedAt != nil || task.RetainUntil != nil) {
		return fmt.Errorf("stored MongoDB task has an invalid terminal lifecycle")
	}
	for name, value := range map[string]*time.Time{
		"completedAt": task.CompletedAt,
		"retainUntil": task.RetainUntil,
	} {
		if value != nil {
			if _, err := encodeTime(*value); err != nil {
				return fmt.Errorf("stored MongoDB task has invalid %s", name)
			}
		}
	}
	if terminal {
		if !task.CompletedAt.Equal(task.UpdatedAt) || !task.RetainUntil.After(*task.CompletedAt) ||
			task.RetainUntil.Sub(*task.CompletedAt) != task.Retention {
			return fmt.Errorf("stored MongoDB task has invalid terminal chronology")
		}
	}
	if task.State == store.TaskStateSucceeded {
		if err := store.ValidateTaskPayload(task.Output); err != nil {
			return fmt.Errorf("stored MongoDB task output: %w", err)
		}
		if task.LastErrorCode != "" || task.LastError != "" {
			return fmt.Errorf("stored MongoDB succeeded task has failure state")
		}
	} else if len(task.Output) != 0 {
		return fmt.Errorf("stored MongoDB task has output outside succeeded state")
	}
	if task.State == store.TaskStateFailed && task.LastErrorCode == "" {
		return fmt.Errorf("stored MongoDB failed task has no failure")
	}
	switch task.State {
	case store.TaskStateQueued, store.TaskStateRunning, store.TaskStateSucceeded, store.TaskStateFailed, store.TaskStateCanceled:
	default:
		return fmt.Errorf("stored MongoDB task has an invalid state")
	}
	return nil
}

func encodeMongoTask(task store.Task) (bson.D, error) {
	if err := validateMongoStoredTask(task); err != nil {
		return nil, err
	}
	runAt, _ := encodeTime(task.RunAt)
	createdAt, _ := encodeTime(task.CreatedAt)
	updatedAt, _ := encodeTime(task.UpdatedAt)
	return bson.D{
		{Key: "_id", Value: task.ID},
		{Key: "codec", Value: mongoTaskCodecVersion},
		{Key: "slug", Value: task.Slug},
		{Key: "queue", Value: task.Queue},
		{Key: "concurrencyKey", Value: task.ConcurrencyKey},
		{Key: "concurrencyID", Value: mongoTaskOptionalString(mongoTaskConcurrencyID(task.Queue, task.ConcurrencyKey))},
		{Key: "input", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), task.Input...)}},
		{Key: "output", Value: mongoTaskOptionalPayload(task.Output)},
		{Key: "state", Value: string(task.State)},
		{Key: "runAt", Value: runAt},
		{Key: "attempts", Value: int64(task.Attempts)},
		{Key: "maxAttempts", Value: int64(task.MaxAttempts)},
		{Key: "retryDelay", Value: int64(task.RetryDelay)},
		{Key: "maxRetryDelay", Value: int64(task.MaxRetryDelay)},
		{Key: "backoff", Value: string(task.Backoff)},
		{Key: "timeout", Value: int64(task.Timeout)},
		{Key: "retention", Value: int64(task.Retention)},
		{Key: "leaseToken", Value: task.LeaseToken},
		{Key: "leaseExpiresAt", Value: mongoTaskOptionalTime(task.LeaseExpiresAt)},
		{Key: "target", Value: encodeMongoTaskReference(task.Target)},
		{Key: "requestedBy", Value: encodeMongoTaskReference(task.RequestedBy)},
		{Key: "lastErrorCode", Value: task.LastErrorCode},
		{Key: "lastError", Value: task.LastError},
		{Key: "createdAt", Value: createdAt},
		{Key: "updatedAt", Value: updatedAt},
		{Key: "completedAt", Value: mongoTaskOptionalTime(task.CompletedAt)},
		{Key: "retainUntil", Value: mongoTaskOptionalTime(task.RetainUntil)},
	}, nil
}

func decodeMongoTask(raw bson.Raw) (store.Task, error) {
	if err := requireExactKeys(raw, "MongoDB task", mongoTaskExactKeys...); err != nil {
		return store.Task{}, err
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != mongoTaskCodecVersion {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an unsupported codec version")
	}
	var task store.Task
	if task.ID, ok = raw.Lookup("_id").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid ID")
	}
	if task.Slug, ok = raw.Lookup("slug").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid slug")
	}
	if task.Queue, ok = raw.Lookup("queue").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid queue")
	}
	if task.ConcurrencyKey, ok = raw.Lookup("concurrencyKey").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid concurrency key")
	}
	concurrencyID, err := decodeMongoTaskOptionalString(raw.Lookup("concurrencyID"), "concurrencyID")
	if err != nil {
		return store.Task{}, err
	}
	if concurrencyID != mongoTaskConcurrencyID(task.Queue, task.ConcurrencyKey) {
		return store.Task{}, mongoSystemIdentityCollision("task concurrency identity")
	}
	inputSubtype, input, ok := raw.Lookup("input").BinaryOK()
	if !ok || inputSubtype != 0 || !json.Valid(input) {
		return store.Task{}, fmt.Errorf("stored MongoDB task has invalid input")
	}
	task.Input = append(json.RawMessage(nil), input...)
	task.Output, err = decodeMongoTaskOptionalPayload(raw.Lookup("output"), "output")
	if err != nil {
		return store.Task{}, err
	}
	state, ok := raw.Lookup("state").StringValueOK()
	if !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid state")
	}
	task.State = store.TaskState(state)
	if task.RunAt, err = decodeMongoTaskTime(raw.Lookup("runAt"), "runAt"); err != nil {
		return store.Task{}, err
	}
	attempts, ok := raw.Lookup("attempts").Int64OK()
	if !ok || int64(int(attempts)) != attempts {
		return store.Task{}, fmt.Errorf("stored MongoDB task has invalid attempts")
	}
	task.Attempts = int(attempts)
	maxAttempts, ok := raw.Lookup("maxAttempts").Int64OK()
	if !ok || int64(int(maxAttempts)) != maxAttempts {
		return store.Task{}, fmt.Errorf("stored MongoDB task has invalid maxAttempts")
	}
	task.MaxAttempts = int(maxAttempts)
	if task.RetryDelay, err = decodeMongoTaskDuration(raw.Lookup("retryDelay"), "retryDelay"); err != nil {
		return store.Task{}, err
	}
	if task.MaxRetryDelay, err = decodeMongoTaskDuration(raw.Lookup("maxRetryDelay"), "maxRetryDelay"); err != nil {
		return store.Task{}, err
	}
	backoff, ok := raw.Lookup("backoff").StringValueOK()
	if !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid backoff")
	}
	task.Backoff = store.TaskBackoff(backoff)
	if task.Timeout, err = decodeMongoTaskDuration(raw.Lookup("timeout"), "timeout"); err != nil {
		return store.Task{}, err
	}
	if task.Retention, err = decodeMongoTaskDuration(raw.Lookup("retention"), "retention"); err != nil {
		return store.Task{}, err
	}
	if task.LeaseToken, ok = raw.Lookup("leaseToken").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid lease token")
	}
	if task.LeaseExpiresAt, err = decodeMongoTaskOptionalTime(raw.Lookup("leaseExpiresAt"), "leaseExpiresAt"); err != nil {
		return store.Task{}, err
	}
	if task.Target, err = decodeMongoTaskReference(raw.Lookup("target"), "target"); err != nil {
		return store.Task{}, err
	}
	if task.RequestedBy, err = decodeMongoTaskReference(raw.Lookup("requestedBy"), "requestedBy"); err != nil {
		return store.Task{}, err
	}
	if task.LastErrorCode, ok = raw.Lookup("lastErrorCode").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid lastErrorCode")
	}
	if task.LastError, ok = raw.Lookup("lastError").StringValueOK(); !ok {
		return store.Task{}, fmt.Errorf("stored MongoDB task has an invalid lastError")
	}
	if task.CreatedAt, err = decodeMongoTaskTime(raw.Lookup("createdAt"), "createdAt"); err != nil {
		return store.Task{}, err
	}
	if task.UpdatedAt, err = decodeMongoTaskTime(raw.Lookup("updatedAt"), "updatedAt"); err != nil {
		return store.Task{}, err
	}
	if task.CompletedAt, err = decodeMongoTaskOptionalTime(raw.Lookup("completedAt"), "completedAt"); err != nil {
		return store.Task{}, err
	}
	if task.RetainUntil, err = decodeMongoTaskOptionalTime(raw.Lookup("retainUntil"), "retainUntil"); err != nil {
		return store.Task{}, err
	}
	if err := validateMongoStoredTask(task); err != nil {
		return store.Task{}, err
	}
	return task, nil
}

func validateMongoTaskConcurrencyGuard(guard mongoTaskConcurrencyGuard) error {
	if guard.ID != mongoTaskConcurrencyID(guard.Queue, guard.Key) {
		return mongoSystemIdentityCollision("task concurrency guard")
	}
	if !validMongoTaskConcurrencyIdentity(guard.Queue, guard.Key) {
		return fmt.Errorf("stored MongoDB task concurrency guard has invalid identity")
	}
	if !validMongoTaskIdentifier(guard.TaskID, mongoTaskIDPrefix) || !validMongoTaskIdentifier(guard.Token, mongoTaskLeasePrefix) {
		return fmt.Errorf("stored MongoDB task concurrency guard has invalid ownership")
	}
	for name, value := range map[string]time.Time{"expiresAt": guard.ExpiresAt, "updatedAt": guard.UpdatedAt} {
		if value.IsZero() {
			return fmt.Errorf("stored MongoDB task concurrency guard has invalid %s", name)
		}
		if _, err := encodeTime(value); err != nil {
			return fmt.Errorf("stored MongoDB task concurrency guard has invalid %s", name)
		}
	}
	if !guard.ExpiresAt.After(guard.UpdatedAt) {
		return fmt.Errorf("stored MongoDB task concurrency guard has invalid lease chronology")
	}
	for name, reference := range map[string]*store.DocumentReference{"target": guard.Target, "requestedBy": guard.RequestedBy} {
		if reference != nil {
			if err := store.ValidateTaskReference(*reference); err != nil {
				return fmt.Errorf("stored MongoDB task concurrency guard %s: %w", name, err)
			}
		}
	}
	return nil
}

func encodeMongoTaskConcurrencyGuard(guard mongoTaskConcurrencyGuard) (bson.D, error) {
	if err := validateMongoTaskConcurrencyGuard(guard); err != nil {
		return nil, err
	}
	expiresAt, _ := encodeTime(guard.ExpiresAt)
	updatedAt, _ := encodeTime(guard.UpdatedAt)
	return bson.D{
		{Key: "_id", Value: guard.ID},
		{Key: "codec", Value: mongoTaskCodecVersion},
		{Key: "queue", Value: guard.Queue},
		{Key: "key", Value: guard.Key},
		{Key: "task", Value: guard.TaskID},
		{Key: "token", Value: guard.Token},
		{Key: "expiresAt", Value: expiresAt},
		{Key: "updatedAt", Value: updatedAt},
		{Key: "target", Value: encodeMongoTaskReference(guard.Target)},
		{Key: "requestedBy", Value: encodeMongoTaskReference(guard.RequestedBy)},
	}, nil
}

func decodeMongoTaskConcurrencyGuard(raw bson.Raw) (mongoTaskConcurrencyGuard, error) {
	if err := requireExactKeys(raw, "MongoDB task concurrency guard", mongoTaskConcurrencyExactKeys...); err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != mongoTaskCodecVersion {
		return mongoTaskConcurrencyGuard{}, fmt.Errorf("stored MongoDB task concurrency guard has an unsupported codec version")
	}
	var guard mongoTaskConcurrencyGuard
	var err error
	if guard.ID, ok = raw.Lookup("_id").StringValueOK(); !ok {
		return mongoTaskConcurrencyGuard{}, fmt.Errorf("stored MongoDB task concurrency guard has an invalid ID")
	}
	if guard.Queue, ok = raw.Lookup("queue").StringValueOK(); !ok {
		return mongoTaskConcurrencyGuard{}, fmt.Errorf("stored MongoDB task concurrency guard has an invalid queue")
	}
	if guard.Key, ok = raw.Lookup("key").StringValueOK(); !ok {
		return mongoTaskConcurrencyGuard{}, fmt.Errorf("stored MongoDB task concurrency guard has an invalid key")
	}
	if guard.TaskID, ok = raw.Lookup("task").StringValueOK(); !ok {
		return mongoTaskConcurrencyGuard{}, fmt.Errorf("stored MongoDB task concurrency guard has an invalid task")
	}
	if guard.Token, ok = raw.Lookup("token").StringValueOK(); !ok {
		return mongoTaskConcurrencyGuard{}, fmt.Errorf("stored MongoDB task concurrency guard has an invalid token")
	}
	if guard.ExpiresAt, err = decodeMongoTaskTime(raw.Lookup("expiresAt"), "task concurrency expiresAt"); err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	if guard.UpdatedAt, err = decodeMongoTaskTime(raw.Lookup("updatedAt"), "task concurrency updatedAt"); err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	if guard.Target, err = decodeMongoTaskReference(raw.Lookup("target"), "task concurrency target"); err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	if guard.RequestedBy, err = decodeMongoTaskReference(raw.Lookup("requestedBy"), "task concurrency requestedBy"); err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	if err := validateMongoTaskConcurrencyGuard(guard); err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	return guard, nil
}

func validMongoTaskConcurrencyIdentity(queue, key string) bool {
	return key != "" && len(queue) <= 64 && schema.IsValidCollectionSlug(queue) &&
		len(key) <= store.MaxTaskConcurrencyKeyBytes && utf8.ValidString(key) &&
		!stringsContainNUL(key) && strings.TrimSpace(key) == key
}

func encodeMongoTaskReference(reference *store.DocumentReference) any {
	if reference == nil {
		return nil
	}
	return bson.D{
		{Key: "collection", Value: string(reference.CollectionID)},
		{Key: "document", Value: reference.DocumentID},
	}
}

func decodeMongoTaskReference(value bson.RawValue, scope string) (*store.DocumentReference, error) {
	if value.Type == bson.TypeNull {
		return nil, nil
	}
	raw, ok := value.DocumentOK()
	if !ok {
		return nil, fmt.Errorf("stored MongoDB task has invalid %s", scope)
	}
	if err := requireExactKeys(raw, "MongoDB task "+scope, "collection", "document"); err != nil {
		return nil, err
	}
	collection, ok := raw.Lookup("collection").StringValueOK()
	if !ok {
		return nil, fmt.Errorf("stored MongoDB task has invalid %s collection", scope)
	}
	document, ok := raw.Lookup("document").StringValueOK()
	if !ok {
		return nil, fmt.Errorf("stored MongoDB task has invalid %s document", scope)
	}
	reference := &store.DocumentReference{CollectionID: schema.StableID(collection), DocumentID: document}
	if err := store.ValidateTaskReference(*reference); err != nil {
		return nil, fmt.Errorf("stored MongoDB task %s: %w", scope, err)
	}
	return reference, nil
}

func cloneMongoTaskReference(reference *store.DocumentReference) *store.DocumentReference {
	if reference == nil {
		return nil
	}
	clone := *reference
	return &clone
}

func cloneMongoTask(task store.Task) store.Task {
	task.Input = append(json.RawMessage(nil), task.Input...)
	task.Output = append(json.RawMessage(nil), task.Output...)
	task.Target = cloneMongoTaskReference(task.Target)
	task.RequestedBy = cloneMongoTaskReference(task.RequestedBy)
	if task.LeaseExpiresAt != nil {
		value := *task.LeaseExpiresAt
		task.LeaseExpiresAt = &value
	}
	if task.CompletedAt != nil {
		value := *task.CompletedAt
		task.CompletedAt = &value
	}
	if task.RetainUntil != nil {
		value := *task.RetainUntil
		task.RetainUntil = &value
	}
	return task
}

func mongoTaskOptionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func decodeMongoTaskOptionalString(value bson.RawValue, scope string) (string, error) {
	if value.Type == bson.TypeNull {
		return "", nil
	}
	result, ok := value.StringValueOK()
	if !ok {
		return "", fmt.Errorf("stored MongoDB task has invalid %s", scope)
	}
	return result, nil
}

func mongoTaskOptionalPayload(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return bson.Binary{Subtype: 0, Data: append([]byte(nil), value...)}
}

func decodeMongoTaskOptionalPayload(value bson.RawValue, scope string) (json.RawMessage, error) {
	if value.Type == bson.TypeNull {
		return nil, nil
	}
	subtype, payload, ok := value.BinaryOK()
	if !ok || subtype != 0 || !json.Valid(payload) {
		return nil, fmt.Errorf("stored MongoDB task has invalid %s", scope)
	}
	return append(json.RawMessage(nil), payload...), nil
}

func mongoTaskOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	encoded, _ := encodeTime(*value)
	return encoded
}

func decodeMongoTaskTime(value bson.RawValue, scope string) (time.Time, error) {
	nanoseconds, ok := value.Int64OK()
	if !ok {
		return time.Time{}, fmt.Errorf("stored MongoDB task has invalid %s", scope)
	}
	return decodeTime(nanoseconds), nil
}

func decodeMongoTaskOptionalTime(value bson.RawValue, scope string) (*time.Time, error) {
	if value.Type == bson.TypeNull {
		return nil, nil
	}
	decoded, err := decodeMongoTaskTime(value, scope)
	if err != nil {
		return nil, err
	}
	return &decoded, nil
}

func decodeMongoTaskDuration(value bson.RawValue, scope string) (time.Duration, error) {
	nanoseconds, ok := value.Int64OK()
	if !ok {
		return 0, fmt.Errorf("stored MongoDB task has invalid %s", scope)
	}
	return time.Duration(nanoseconds), nil
}

// mongoTaskServerNowNanosExpression converts MongoDB's authoritative
// millisecond Date into the adapter's existing signed Unix-nanosecond envelope.
func mongoTaskServerNowNanosExpression() bson.D {
	return bson.D{{Key: "$multiply", Value: bson.A{
		bson.D{{Key: "$toLong", Value: "$$NOW"}},
		int64(time.Millisecond),
	}}}
}

func mongoTaskServerDeadlineExpression(duration time.Duration) bson.D {
	return bson.D{{Key: "$add", Value: bson.A{mongoTaskServerNowNanosExpression(), int64(duration)}}}
}

func mongoTaskLiteral(value any) bson.D {
	return bson.D{{Key: "$literal", Value: value}}
}
