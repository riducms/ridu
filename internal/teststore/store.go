// Package teststore provides a strict transactional fake for operation and
// adapter conformance tests. It is not a production in-memory database.
package teststore

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type Store struct {
	mu              sync.Mutex
	authBootstrapMu sync.Mutex
	documents       map[string]map[string]store.Document
	nextID          int
	events          []string
	now             func() time.Time
	passwords       map[string][]byte
	credentials     map[string]store.AuthCredential
	sessions        map[string]session
	authTokens      map[string]store.AuthToken
	apiKeys         map[string]store.AuthAPIKey
	rateLimits      map[string]rateLimit
	versions        map[string]map[string][]store.Version
	tasks           map[string]store.Task
	preferences     map[string]store.Preference
	documentLocks   map[string]store.DocumentLock
	references      map[string]referenceindex.Entry
	uploadLockMu    sync.Mutex
	uploadLocks     map[string]chan struct{}
}

type session struct {
	record store.AuthSession
}

type rateLimit struct {
	attempts  int
	expiresAt time.Time
}

func New() *Store {
	return &Store{
		documents: make(map[string]map[string]store.Document), now: time.Now,
		passwords: make(map[string][]byte), credentials: make(map[string]store.AuthCredential), sessions: make(map[string]session), authTokens: make(map[string]store.AuthToken), apiKeys: make(map[string]store.AuthAPIKey), rateLimits: make(map[string]rateLimit),
		versions:      make(map[string]map[string][]store.Version),
		tasks:         make(map[string]store.Task),
		preferences:   make(map[string]store.Preference),
		documentLocks: make(map[string]store.DocumentLock),
		references:    make(map[string]referenceindex.Entry),
		uploadLocks:   make(map[string]chan struct{}),
	}
}

func (backend *Store) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	unique := make(map[string]struct{}, len(objectKeys))
	for _, key := range objectKeys {
		if key == "" {
			return nil, fmt.Errorf("upload object lock key is empty")
		}
		unique[key] = struct{}{}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	acquired := make([]chan struct{}, 0, len(keys))
	for _, key := range keys {
		backend.uploadLockMu.Lock()
		lock := backend.uploadLocks[key]
		if lock == nil {
			lock = make(chan struct{}, 1)
			lock <- struct{}{}
			backend.uploadLocks[key] = lock
		}
		backend.uploadLockMu.Unlock()
		select {
		case <-lock:
			acquired = append(acquired, lock)
		case <-ctx.Done():
			for index := len(acquired) - 1; index >= 0; index-- {
				acquired[index] <- struct{}{}
			}
			return nil, ctx.Err()
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for index := len(acquired) - 1; index >= 0; index-- {
				acquired[index] <- struct{}{}
			}
		})
	}, nil
}

func documentLockKey(collectionID schema.StableID, documentID string) string {
	return string(collectionID) + "\x00" + documentID
}

func (backend *Store) FindDocumentLock(_ context.Context, collectionID schema.StableID, documentID string, now time.Time) (store.DocumentLock, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := documentLockKey(collectionID, documentID)
	lock, exists := backend.documentLocks[key]
	if !exists || !lock.ExpiresAt.After(now) {
		delete(backend.documentLocks, key)
		return store.DocumentLock{}, store.ErrNotFound
	}
	return lock, nil
}

func (backend *Store) AcquireDocumentLock(_ context.Context, candidate store.DocumentLock, now time.Time, takeover bool) (store.DocumentLock, bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.activeDocument(candidate.CollectionID, candidate.DocumentID) || !backend.activeDocument(candidate.OwnerCollectionID, candidate.OwnerID) {
		return store.DocumentLock{}, false, store.ErrNotFound
	}
	key := documentLockKey(candidate.CollectionID, candidate.DocumentID)
	current, exists := backend.documentLocks[key]
	sameOwner := exists && current.OwnerCollectionID == candidate.OwnerCollectionID && current.OwnerID == candidate.OwnerID
	if exists && current.ExpiresAt.After(now) && !sameOwner && !takeover {
		return current, false, nil
	}
	if sameOwner {
		candidate.CreatedAt = current.CreatedAt
	}
	backend.documentLocks[key] = candidate
	return candidate, true, nil
}

func (backend *Store) ReleaseDocumentLock(_ context.Context, collectionID schema.StableID, documentID string, ownerCollectionID schema.StableID, ownerID string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := documentLockKey(collectionID, documentID)
	current, exists := backend.documentLocks[key]
	if !exists {
		return nil
	}
	if current.OwnerCollectionID != ownerCollectionID || current.OwnerID != ownerID {
		return nil
	}
	delete(backend.documentLocks, key)
	return nil
}

func preferenceKey(collectionID schema.StableID, userID, key string) string {
	return string(collectionID) + "\x00" + userID + "\x00" + key
}

func (backend *Store) GetPreference(_ context.Context, collectionID schema.StableID, userID, key string) (store.Preference, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	preference, exists := backend.preferences[preferenceKey(collectionID, userID, key)]
	if !exists {
		return store.Preference{}, store.ErrNotFound
	}
	preference.Value = append(json.RawMessage(nil), preference.Value...)
	return preference, nil
}

func (backend *Store) SetPreference(_ context.Context, preference store.Preference) (store.Preference, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.activeDocument(preference.CollectionID, preference.UserID) {
		return store.Preference{}, store.ErrNotFound
	}
	preference.Value = append(json.RawMessage(nil), preference.Value...)
	preference.UpdatedAt = backend.now().UTC()
	backend.preferences[preferenceKey(preference.CollectionID, preference.UserID, preference.Key)] = preference
	return preference, nil
}

func (backend *Store) DeletePreference(_ context.Context, collectionID schema.StableID, userID, key string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	delete(backend.preferences, preferenceKey(collectionID, userID, key))
	return nil
}

func (backend *Store) DeletePreferences(_ context.Context, collectionID schema.StableID, userID string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	prefix := string(collectionID) + "\x00" + userID + "\x00"
	for key := range backend.preferences {
		if strings.HasPrefix(key, prefix) {
			delete(backend.preferences, key)
		}
	}
	return nil
}

func (backend *Store) Ping(ctx context.Context) error { return ctx.Err() }
func (backend *Store) Ready(ctx context.Context, _ schema.Manifest) error {
	return ctx.Err()
}

func (backend *Store) EnqueueTask(ctx context.Context, task store.Task) (store.Task, error) {
	if err := ctx.Err(); err != nil {
		return store.Task{}, err
	}
	if err := store.ValidateTaskAdmission(task); err != nil {
		return store.Task{}, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for _, reference := range []*store.DocumentReference{task.Target, task.RequestedBy} {
		if reference != nil && !backend.activeDocument(reference.CollectionID, reference.DocumentID) {
			return store.Task{}, store.ErrNotFound
		}
	}
	backend.nextID++
	now := backend.now().UTC()
	task.ID = fmt.Sprintf("task_%d", backend.nextID)
	task.State = store.TaskStateQueued
	task.Attempts = 0
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.Output = nil
	task.LastErrorCode = ""
	task.LastError = ""
	task.RunAt = task.RunAt.UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	task.CompletedAt = nil
	task.RetainUntil = nil
	task.Input = append(json.RawMessage(nil), task.Input...)
	task.Target = cloneTaskReference(task.Target)
	task.RequestedBy = cloneTaskReference(task.RequestedBy)
	backend.tasks[task.ID] = task
	return cloneTask(task), nil
}

func (backend *Store) FindTask(ctx context.Context, id string) (store.Task, error) {
	if err := ctx.Err(); err != nil {
		return store.Task{}, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	task, exists := backend.tasks[id]
	if !exists {
		return store.Task{}, store.ErrNotFound
	}
	return cloneTask(task), nil
}

func (backend *Store) ListTasks(ctx context.Context, request store.TaskList) ([]store.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := store.ValidateTaskList(request); err != nil {
		return nil, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	limit := request.Limit
	states := make(map[store.TaskState]struct{}, len(request.States))
	for _, state := range request.States {
		states[state] = struct{}{}
	}
	result := make([]store.Task, 0)
	for _, task := range backend.tasks {
		if request.Slug != "" && task.Slug != request.Slug {
			continue
		}
		if request.Target != nil && (task.Target == nil || *task.Target != *request.Target) {
			continue
		}
		if len(states) != 0 {
			if _, allowed := states[task.State]; !allowed {
				continue
			}
		}
		result = append(result, cloneTask(task))
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].RunAt.Equal(result[right].RunAt) {
			if result[left].CreatedAt.Equal(result[right].CreatedAt) {
				return result[left].ID < result[right].ID
			}
			return result[left].CreatedAt.Before(result[right].CreatedAt)
		}
		return result[left].RunAt.Before(result[right].RunAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (backend *Store) CancelTask(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.cancelTask(id, "", nil)
}

func (backend *Store) DismissTaskForTarget(ctx context.Context, id, slug string, target store.DocumentReference) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ValidateTaskList(store.TaskList{Slug: slug, Target: &target, Limit: 1}); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	task, exists := backend.tasks[id]
	if !exists || task.Slug != slug || task.Target == nil || *task.Target != target || !dismissibleTaskState(task.State) {
		return store.ErrNotFound
	}
	delete(backend.tasks, id)
	return nil
}

func dismissibleTaskState(state store.TaskState) bool {
	switch state {
	case store.TaskStateQueued, store.TaskStateRunning, store.TaskStateFailed, store.TaskStateCanceled:
		return true
	default:
		return false
	}
}

func (backend *Store) cancelTask(id, slug string, target *store.DocumentReference) error {
	task, exists := backend.tasks[id]
	if !exists || slug != "" && task.Slug != slug || target != nil && (task.Target == nil || *task.Target != *target) {
		return store.ErrNotFound
	}
	if task.State == store.TaskStateSucceeded || task.State == store.TaskStateFailed || task.State == store.TaskStateCanceled {
		return store.ErrNotFound
	}
	now := backend.now().UTC()
	retainUntil := now.Add(task.Retention)
	task.State = store.TaskStateCanceled
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.UpdatedAt = now
	task.CompletedAt = &now
	task.RetainUntil = &retainUntil
	backend.tasks[id] = task
	return nil
}

func (backend *Store) ClaimTasks(ctx context.Context, request store.TaskClaim) ([]store.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := store.ValidateTaskClaim(request); err != nil {
		return nil, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	now := backend.now().UTC()
	queues := stringSet(request.Queues)
	slugs := stringSet(request.Slugs)
	candidates := make([]store.Task, 0)
	for _, task := range backend.tasks {
		eligible := task.State == store.TaskStateQueued && !task.RunAt.After(now)
		eligible = eligible || task.State == store.TaskStateRunning && task.LeaseExpiresAt != nil && !task.LeaseExpiresAt.After(now)
		if !eligible || len(queues) != 0 && !queues[task.Queue] || len(slugs) != 0 && !slugs[task.Slug] {
			continue
		}
		candidates = append(candidates, task)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].RunAt.Equal(candidates[right].RunAt) {
			if candidates[left].CreatedAt.Equal(candidates[right].CreatedAt) {
				return candidates[left].ID < candidates[right].ID
			}
			return candidates[left].CreatedAt.Before(candidates[right].CreatedAt)
		}
		return candidates[left].RunAt.Before(candidates[right].RunAt)
	})
	activeKeys := make(map[string]bool)
	for _, task := range backend.tasks {
		if task.State == store.TaskStateRunning && task.LeaseExpiresAt != nil && task.LeaseExpiresAt.After(now) && task.ConcurrencyKey != "" {
			activeKeys[task.Queue+"\x00"+task.ConcurrencyKey] = true
		}
	}
	claimed := make([]store.Task, 0, request.Limit)
	for _, candidate := range candidates {
		if len(claimed) >= request.Limit {
			break
		}
		concurrency := ""
		if candidate.ConcurrencyKey != "" {
			concurrency = candidate.Queue + "\x00" + candidate.ConcurrencyKey
			if activeKeys[concurrency] {
				continue
			}
		}
		backend.nextID++
		task := backend.tasks[candidate.ID]
		expiresAt := now.Add(request.LeaseDuration)
		task.State = store.TaskStateRunning
		task.Attempts++
		task.LeaseToken = fmt.Sprintf("lease_%d", backend.nextID)
		task.LeaseExpiresAt = &expiresAt
		task.UpdatedAt = now
		backend.tasks[task.ID] = task
		if concurrency != "" {
			activeKeys[concurrency] = true
		}
		claimed = append(claimed, cloneTask(task))
	}
	return claimed, nil
}

func (backend *Store) HeartbeatTask(ctx context.Context, id, leaseToken string, leaseDuration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ValidateTaskLeaseDuration(leaseDuration); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	task, exists := backend.tasks[id]
	now := backend.now().UTC()
	if !activeTaskLease(task, exists, leaseToken, now) {
		return store.ErrTaskLeaseLost
	}
	expiresAt := now.Add(leaseDuration)
	task.LeaseExpiresAt = cloneTime(&expiresAt)
	task.UpdatedAt = now
	backend.tasks[id] = task
	return nil
}

func (backend *Store) CompleteTask(ctx context.Context, id, leaseToken string, output json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ValidateTaskPayload(output); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	task, exists := backend.tasks[id]
	now := backend.now().UTC()
	if !activeTaskLease(task, exists, leaseToken, now) {
		return store.ErrTaskLeaseLost
	}
	retainUntil := now.Add(task.Retention)
	task.State = store.TaskStateSucceeded
	task.Output = append(json.RawMessage(nil), output...)
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.LastErrorCode = ""
	task.LastError = ""
	task.UpdatedAt = now
	task.CompletedAt = &now
	task.RetainUntil = &retainUntil
	backend.tasks[id] = task
	return nil
}

func (backend *Store) FailTask(ctx context.Context, failure store.TaskFailure) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ValidateTaskFailure(failure); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	task, exists := backend.tasks[failure.ID]
	now := backend.now().UTC()
	if !activeTaskLease(task, exists, failure.LeaseToken, now) {
		return store.ErrTaskLeaseLost
	}
	task.LastErrorCode = failure.Code
	task.LastError = failure.Message
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.UpdatedAt = now
	if failure.RetryAfter != nil {
		task.State = store.TaskStateQueued
		task.RunAt = now.Add(*failure.RetryAfter)
		task.CompletedAt = nil
		task.RetainUntil = nil
	} else {
		retainUntil := now.Add(task.Retention)
		task.State = store.TaskStateFailed
		task.CompletedAt = &now
		task.RetainUntil = &retainUntil
	}
	backend.tasks[task.ID] = task
	return nil
}

func (backend *Store) ReleaseTask(ctx context.Context, id, leaseToken string, delay time.Duration, code, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ValidateTaskRelease(delay, code, message); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	task, exists := backend.tasks[id]
	now := backend.now().UTC()
	if !activeTaskLease(task, exists, leaseToken, now) {
		return store.ErrTaskLeaseLost
	}
	task.State = store.TaskStateQueued
	task.RunAt = now.Add(delay)
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.LastErrorCode = code
	task.LastError = message
	task.UpdatedAt = now
	backend.tasks[id] = task
	return nil
}

func (backend *Store) PruneTasks(ctx context.Context, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := store.ValidateTaskBatch(limit); err != nil {
		return 0, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	now := backend.now().UTC()
	ids := make([]string, 0)
	for id, task := range backend.tasks {
		if task.RetainUntil != nil && !task.RetainUntil.After(now) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	for _, id := range ids {
		delete(backend.tasks, id)
	}
	return len(ids), nil
}

func activeTaskLease(task store.Task, exists bool, leaseToken string, now time.Time) bool {
	return exists && task.State == store.TaskStateRunning && task.LeaseToken == leaseToken &&
		task.LeaseExpiresAt != nil && task.LeaseExpiresAt.After(now)
}

func cloneTask(task store.Task) store.Task {
	task.Input = append(json.RawMessage(nil), task.Input...)
	task.Output = append(json.RawMessage(nil), task.Output...)
	task.Target = cloneTaskReference(task.Target)
	task.RequestedBy = cloneTaskReference(task.RequestedBy)
	task.LeaseExpiresAt = cloneTime(task.LeaseExpiresAt)
	task.CompletedAt = cloneTime(task.CompletedAt)
	task.RetainUntil = cloneTime(task.RetainUntil)
	return task
}

func cloneTaskReference(reference *store.DocumentReference) *store.DocumentReference {
	if reference == nil {
		return nil
	}
	cloned := *reference
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

var _ store.TaskStore = (*Store)(nil)

func (backend *Store) Events() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]string(nil), backend.events...)
}

func (backend *Store) Begin(ctx context.Context) (store.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.events = append(backend.events, "begin")
	return &transaction{
		store: backend, documents: cloneCollections(backend.documents), versions: cloneVersions(backend.versions),
		references: cloneReferenceEntries(backend.references),
		passwords:  make(map[string][]byte), credentials: make(map[string]store.AuthCredential),
		credentialUpdates: make(map[string]store.AuthCredential),
	}, nil
}

func (backend *Store) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	started, err := backend.Begin(ctx)
	if err != nil {
		return nil, err
	}
	started.(*transaction).readOnly = true
	return started, nil
}

func (backend *Store) SetPasswordHash(_ context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if _, exists := backend.documents[string(collection.ID)][userID]; !exists {
		return store.ErrNotFound
	}
	key := string(collection.ID) + ":" + userID
	credential, exists := backend.credentials[key]
	if !exists {
		credential.Verified = initiallyVerified
	}
	backend.replacePasswordHash(key, hash, credential, true)
	return nil
}

func (backend *Store) ChangePasswordHash(_ context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := string(collection.ID) + ":" + userID
	credential, exists := backend.credentials[key]
	if !exists || !backend.activeDocument(collection.ID, userID) {
		return store.ErrNotFound
	}
	if subtle.ConstantTimeCompare(backend.passwords[key], expectedPasswordHash) != 1 {
		return store.ErrConflict
	}
	backend.replacePasswordHash(key, hash, credential, true)
	return nil
}

func (backend *Store) UpgradePasswordHash(_ context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := string(collection.ID) + ":" + userID
	credential, exists := backend.credentials[key]
	if !exists || !backend.activeDocument(collection.ID, userID) {
		return store.ErrNotFound
	}
	if subtle.ConstantTimeCompare(backend.passwords[key], expectedPasswordHash) != 1 {
		return store.ErrConflict
	}
	backend.replacePasswordHash(key, hash, credential, false)
	return nil
}

func (backend *Store) replacePasswordHash(key string, hash []byte, credential store.AuthCredential, revokeSessions bool) {
	backend.passwords[key] = append([]byte(nil), hash...)
	credential.FailedLoginAttempts = 0
	credential.LockedUntil = time.Time{}
	backend.credentials[key] = credential
	if revokeSessions {
		collectionID, userID, _ := strings.Cut(key, ":")
		for tokenHash, current := range backend.sessions {
			if string(current.record.CollectionID) == collectionID && current.record.UserID == userID {
				delete(backend.sessions, tokenHash)
			}
		}
		for id, apiKey := range backend.apiKeys {
			if string(apiKey.CollectionID) == collectionID && apiKey.UserID == userID {
				delete(backend.apiKeys, id)
			}
		}
	}
}

func (backend *Store) FindAuthCredential(_ context.Context, collection schema.Collection, identity string) (store.AuthCredential, error) {
	identity = store.CanonicalAuthIdentity(identity)
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for _, document := range backend.documents[string(collection.ID)] {
		if document.DeletedAt != nil {
			continue
		}
		value, exists := document.Values[collection.Auth.IdentityField]
		text, valid := value.StringValue()
		if exists && valid && text == identity {
			hash, exists := backend.passwords[string(collection.ID)+":"+document.ID]
			if !exists {
				return store.AuthCredential{}, store.ErrNotFound
			}
			credential := backend.credentials[string(collection.ID)+":"+document.ID]
			credential.User = store.CloneDocument(document)
			credential.PasswordHash = append([]byte(nil), hash...)
			return credential, nil
		}
	}
	return store.AuthCredential{}, store.ErrNotFound
}

func (backend *Store) RecordFailedLogin(_ context.Context, collectionID schema.StableID, userID string, now time.Time, maximum int, lockDuration time.Duration) (store.AuthCredential, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := string(collectionID) + ":" + userID
	credential, exists := backend.credentials[key]
	if !exists {
		if _, hashExists := backend.passwords[key]; !hashExists {
			return store.AuthCredential{}, store.ErrNotFound
		}
	}
	if credential.LockedUntil.After(now) {
		return credential, nil
	}
	if !credential.LockedUntil.IsZero() {
		credential.FailedLoginAttempts = 0
		credential.LockedUntil = time.Time{}
	}
	credential.FailedLoginAttempts++
	if credential.FailedLoginAttempts >= maximum {
		credential.LockedUntil = now.Add(lockDuration)
	}
	backend.credentials[key] = credential
	return credential, nil
}

func (backend *Store) ResetLoginAttempts(_ context.Context, collectionID schema.StableID, userID string, now time.Time) (bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := string(collectionID) + ":" + userID
	credential, exists := backend.credentials[key]
	if !exists {
		if _, hashExists := backend.passwords[key]; !hashExists {
			return false, store.ErrNotFound
		}
	}
	if credential.LockedUntil.After(now) {
		return false, nil
	}
	credential.FailedLoginAttempts = 0
	credential.LockedUntil = time.Time{}
	backend.credentials[key] = credential
	return true, nil
}

func (backend *Store) ForceUnlock(_ context.Context, collectionID schema.StableID, userID string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := string(collectionID) + ":" + userID
	credential, exists := backend.credentials[key]
	if !exists {
		if _, hashExists := backend.passwords[key]; !hashExists {
			return store.ErrNotFound
		}
	}
	credential.FailedLoginAttempts = 0
	credential.LockedUntil = time.Time{}
	backend.credentials[key] = credential
	return nil
}

func (backend *Store) CreateSession(_ context.Context, record store.AuthSession, expectedPasswordHash []byte) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.activeDocument(record.CollectionID, record.UserID) {
		return store.ErrNotFound
	}
	key := string(record.CollectionID) + ":" + record.UserID
	_, exists := backend.credentials[key]
	if !exists {
		return store.ErrNotFound
	}
	if subtle.ConstantTimeCompare(backend.passwords[key], expectedPasswordHash) != 1 {
		return store.ErrConflict
	}
	backend.sessions[record.TokenHash] = session{record: cloneAuthSession(record)}
	return nil
}

func (backend *Store) RotateSession(_ context.Context, currentHash string, replacement store.AuthSession, now time.Time) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	current, exists := backend.sessions[currentHash]
	if !exists || !current.record.ExpiresAt.After(now) {
		return store.ErrNotFound
	}
	if current.record.CollectionID != replacement.CollectionID || current.record.UserID != replacement.UserID || !backend.activeDocument(current.record.CollectionID, current.record.UserID) {
		return store.ErrNotFound
	}
	delete(backend.sessions, currentHash)
	record := current.record
	record.TokenHash = replacement.TokenHash
	record.LastSeenAt = now
	backend.sessions[record.TokenHash] = session{record: cloneAuthSession(record)}
	return nil
}

func (backend *Store) DeleteSession(_ context.Context, tokenHash string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	delete(backend.sessions, tokenHash)
	return nil
}

func (backend *Store) DeleteUserSession(_ context.Context, collectionID schema.StableID, userID, sessionID string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for tokenHash, current := range backend.sessions {
		if current.record.CollectionID == collectionID && current.record.UserID == userID && current.record.ID == sessionID {
			delete(backend.sessions, tokenHash)
		}
	}
	return nil
}

func (backend *Store) DeleteUserSessions(_ context.Context, collectionID schema.StableID, userID string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for tokenHash, current := range backend.sessions {
		if current.record.CollectionID == collectionID && current.record.UserID == userID {
			delete(backend.sessions, tokenHash)
		}
	}
	return nil
}

func (backend *Store) FindSession(_ context.Context, tokenHash string, now time.Time) (store.AuthSession, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	session, exists := backend.sessions[tokenHash]
	if !exists || !session.record.ExpiresAt.After(now) {
		return store.AuthSession{}, store.ErrNotFound
	}
	return cloneAuthSession(session.record), nil
}

func (backend *Store) ListSessions(_ context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthSession, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	var result []store.AuthSession
	for _, current := range backend.sessions {
		if current.record.CollectionID == collectionID && current.record.UserID == userID && current.record.ExpiresAt.After(now) {
			result = append(result, cloneAuthSession(current.record))
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].LastSeenAt.Equal(result[right].LastSeenAt) {
			return result[left].CreatedAt.After(result[right].CreatedAt)
		}
		return result[left].LastSeenAt.After(result[right].LastSeenAt)
	})
	return result, nil
}

func cloneAuthSession(value store.AuthSession) store.AuthSession { return value }

func (backend *Store) CreateAuthToken(_ context.Context, token store.AuthToken) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.activeDocument(token.CollectionID, token.UserID) {
		return store.ErrNotFound
	}
	for hash, current := range backend.authTokens {
		if current.CollectionID == token.CollectionID && current.UserID == token.UserID && current.Purpose == token.Purpose {
			delete(backend.authTokens, hash)
		}
	}
	backend.authTokens[token.TokenHash] = token
	return nil
}

func (backend *Store) ResetPasswordWithToken(_ context.Context, collectionID schema.StableID, tokenHash string, hash []byte, now time.Time) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	token, exists := backend.authTokens[tokenHash]
	if !exists || token.CollectionID != collectionID || token.Purpose != store.AuthTokenPasswordReset || !token.ExpiresAt.After(now) {
		return "", store.ErrNotFound
	}
	key := string(collectionID) + ":" + token.UserID
	credential, exists := backend.credentials[key]
	if !exists {
		return "", store.ErrNotFound
	}
	delete(backend.authTokens, tokenHash)
	backend.passwords[key] = append([]byte(nil), hash...)
	credential.FailedLoginAttempts = 0
	credential.LockedUntil = time.Time{}
	backend.credentials[key] = credential
	for sessionHash, current := range backend.sessions {
		if current.record.CollectionID == collectionID && current.record.UserID == token.UserID {
			delete(backend.sessions, sessionHash)
		}
	}
	for id, key := range backend.apiKeys {
		if key.CollectionID == collectionID && key.UserID == token.UserID {
			delete(backend.apiKeys, id)
		}
	}
	return token.UserID, nil
}

func (backend *Store) VerifyEmailWithToken(_ context.Context, collectionID schema.StableID, tokenHash string, now time.Time) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	token, exists := backend.authTokens[tokenHash]
	if !exists || token.CollectionID != collectionID || token.Purpose != store.AuthTokenVerifyEmail || !token.ExpiresAt.After(now) {
		return "", store.ErrNotFound
	}
	key := string(collectionID) + ":" + token.UserID
	credential, exists := backend.credentials[key]
	if !exists {
		return "", store.ErrNotFound
	}
	delete(backend.authTokens, tokenHash)
	credential.Verified = true
	backend.credentials[key] = credential
	return token.UserID, nil
}

func (backend *Store) CreateAPIKey(_ context.Context, key store.AuthAPIKey, sessionTokenHash string, now time.Time) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.activeDocument(key.CollectionID, key.UserID) {
		return store.ErrNotFound
	}
	if _, exists := backend.credentials[string(key.CollectionID)+":"+key.UserID]; !exists {
		return store.ErrNotFound
	}
	current, exists := backend.sessions[sessionTokenHash]
	if !exists || current.record.CollectionID != key.CollectionID || current.record.UserID != key.UserID || !current.record.ExpiresAt.After(now) {
		return store.ErrNotFound
	}
	backend.apiKeys[key.ID] = key
	return nil
}

func (backend *Store) FindAPIKey(_ context.Context, id string, now time.Time) (store.AuthAPIKey, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key, exists := backend.apiKeys[id]
	if !exists || !key.ExpiresAt.IsZero() && !key.ExpiresAt.After(now) {
		return store.AuthAPIKey{}, store.ErrNotFound
	}
	return key, nil
}

func (backend *Store) TouchAPIKey(_ context.Context, id string, now time.Time) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key, exists := backend.apiKeys[id]
	if !exists || !key.ExpiresAt.IsZero() && !key.ExpiresAt.After(now) {
		return store.ErrNotFound
	}
	key.LastUsedAt = now
	backend.apiKeys[id] = key
	return nil
}

func (backend *Store) ListAPIKeys(_ context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthAPIKey, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	var keys []store.AuthAPIKey
	for _, key := range backend.apiKeys {
		if key.CollectionID == collectionID && key.UserID == userID && (key.ExpiresAt.IsZero() || key.ExpiresAt.After(now)) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left].CreatedAt.After(keys[right].CreatedAt) })
	return keys, nil
}

func (backend *Store) DeleteAPIKey(_ context.Context, collectionID schema.StableID, userID, id string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key, exists := backend.apiKeys[id]
	if exists && key.CollectionID == collectionID && key.UserID == userID {
		delete(backend.apiKeys, id)
	}
	return nil
}

func (backend *Store) PruneExpiredAuth(ctx context.Context, limit int) (store.AuthPruneResult, error) {
	if err := ctx.Err(); err != nil {
		return store.AuthPruneResult{}, err
	}
	if err := store.ValidateAuthPruneBatch(limit); err != nil {
		return store.AuthPruneResult{}, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	now := backend.now().UTC()

	type candidate struct {
		id        string
		expiresAt time.Time
	}
	sessions := make([]candidate, 0)
	for tokenHash, current := range backend.sessions {
		if !current.record.ExpiresAt.After(now) {
			sessions = append(sessions, candidate{id: tokenHash, expiresAt: current.record.ExpiresAt})
		}
	}
	sort.Slice(sessions, func(left, right int) bool {
		if sessions[left].expiresAt.Equal(sessions[right].expiresAt) {
			return sessions[left].id < sessions[right].id
		}
		return sessions[left].expiresAt.Before(sessions[right].expiresAt)
	})
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	for _, current := range sessions {
		delete(backend.sessions, current.id)
	}

	apiKeys := make([]candidate, 0)
	for id, key := range backend.apiKeys {
		if !key.ExpiresAt.IsZero() && !key.ExpiresAt.After(now) {
			apiKeys = append(apiKeys, candidate{id: id, expiresAt: key.ExpiresAt})
		}
	}
	sort.Slice(apiKeys, func(left, right int) bool {
		if apiKeys[left].expiresAt.Equal(apiKeys[right].expiresAt) {
			return apiKeys[left].id < apiKeys[right].id
		}
		return apiKeys[left].expiresAt.Before(apiKeys[right].expiresAt)
	})
	if len(apiKeys) > limit {
		apiKeys = apiKeys[:limit]
	}
	for _, current := range apiKeys {
		delete(backend.apiKeys, current.id)
	}
	return store.AuthPruneResult{Sessions: len(sessions), APIKeys: len(apiKeys)}, nil
}

func (backend *Store) AllowAuthAttempt(_ context.Context, keyHash string, now time.Time, window time.Duration, maximum int) (bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	bucket := backend.rateLimits[keyHash]
	if bucket.expiresAt.IsZero() || !bucket.expiresAt.After(now) {
		bucket = rateLimit{expiresAt: now.Add(window)}
	}
	bucket.attempts++
	backend.rateLimits[keyHash] = bucket
	return bucket.attempts <= maximum, nil
}

type transaction struct {
	store               *Store
	documents           map[string]map[string]store.Document
	done                bool
	readOnly            bool
	versions            map[string]map[string][]store.Version
	passwords           map[string][]byte
	credentials         map[string]store.AuthCredential
	credentialUpdates   map[string]store.AuthCredential
	deletedState        map[store.DocumentReference]struct{}
	references          map[string]referenceindex.Entry
	authBootstrapLocked bool
}

func (transaction *transaction) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	if err := transaction.ready(ctx); err != nil {
		return nil, err
	}
	return transaction.store.LockUploadObjects(ctx, objectKeys)
}

func (transaction *transaction) CreateAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	if err := transaction.writable(ctx); err != nil {
		return err
	}
	return transaction.createAuthCredential(collection, userID, hash, initiallyVerified)
}

func (transaction *transaction) CreateFirstAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	if err := transaction.writable(ctx); err != nil {
		return err
	}
	if !transaction.authBootstrapLocked {
		transaction.store.authBootstrapMu.Lock()
		transaction.authBootstrapLocked = true
	}
	transaction.store.mu.Lock()
	for _, document := range transaction.store.documents[string(collection.ID)] {
		if document.DeletedAt == nil {
			transaction.store.mu.Unlock()
			return store.ErrAuthInitialized
		}
	}
	transaction.store.mu.Unlock()
	active := 0
	for _, document := range transaction.collection(string(collection.ID)) {
		if document.DeletedAt == nil {
			active++
			if document.ID != userID {
				return store.ErrAuthInitialized
			}
		}
	}
	if active != 1 {
		return store.ErrAuthInitialized
	}
	return transaction.createAuthCredential(collection, userID, hash, initiallyVerified)
}

func (transaction *transaction) createAuthCredential(collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	if _, exists := transaction.collection(string(collection.ID))[userID]; !exists {
		return store.ErrNotFound
	}
	key := string(collection.ID) + ":" + userID
	if _, exists := transaction.passwords[key]; exists {
		return store.ErrConflict
	}
	transaction.passwords[key] = append([]byte(nil), hash...)
	transaction.credentials[key] = store.AuthCredential{Verified: initiallyVerified}
	transaction.event("create-auth-credential")
	return nil
}

func (transaction *transaction) ForceUnlockAuth(ctx context.Context, collectionID schema.StableID, userID string) error {
	if err := transaction.writable(ctx); err != nil {
		return err
	}
	key := string(collectionID) + ":" + userID
	transaction.store.mu.Lock()
	credential, exists := transaction.store.credentials[key]
	_, hashExists := transaction.store.passwords[key]
	transaction.store.mu.Unlock()
	if !exists && !hashExists {
		return store.ErrNotFound
	}
	credential.FailedLoginAttempts = 0
	credential.LockedUntil = time.Time{}
	transaction.credentialUpdates[key] = credential
	transaction.event("force-unlock-auth")
	return nil
}

func (transaction *transaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	if err := transaction.writable(ctx); err != nil {
		return store.Document{}, err
	}
	transaction.store.mu.Lock()
	id := request.ID
	if id == "" {
		transaction.store.nextID++
		id = fmt.Sprintf("%s_%d", request.Collection.ID, transaction.store.nextID)
	}
	if err := store.ValidateDocumentID(id); err != nil {
		transaction.store.mu.Unlock()
		return store.Document{}, err
	}
	now := transaction.store.now().UTC()
	createdAt, updatedAt := request.CreatedAt, request.UpdatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	transaction.store.events = append(transaction.store.events, "create")
	transaction.store.mu.Unlock()
	status := request.Status
	revision := 0
	if request.Collection.Versions != nil {
		if status == "" {
			status = store.StatusDraft
			if !request.Collection.Versions.Drafts {
				status = store.StatusPublished
			}
		}
		revision = 1
	}
	values := store.CloneValues(request.Values)
	canonicalizeTestAuthIdentity(request.Collection, values)
	document := store.Document{ID: id, CreatedAt: createdAt, UpdatedAt: updatedAt, Status: status, Revision: revision, Values: values}
	collection := transaction.collection(string(request.Collection.ID))
	if _, exists := collection[id]; exists {
		return store.Document{}, store.ErrConflict
	}
	if uniqueConflict(collection, request.Collection, document, "") {
		return store.Document{}, store.ErrConflict
	}
	collection[id] = document
	if err := transaction.replaceReferenceEntries(request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return store.CloneDocument(document), nil
}

func (transaction *transaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.ready(ctx); err != nil {
		return store.Document{}, err
	}
	if request.Lock != store.LockNone && request.Lock != store.LockReference && request.Lock != store.LockMutation {
		return store.Document{}, fmt.Errorf("unsupported document lock mode %q", request.Lock)
	}
	transaction.event("find")
	document, exists := transaction.collection(string(request.Collection.ID))[request.ID]
	if !exists || !matchesDeletion(document, request.Deletion) || request.PublishedOnly && request.Collection.Versions != nil && document.Status != store.StatusPublished || !matchesRequest(document, request) {
		return store.Document{}, store.ErrNotFound
	}
	return transaction.prepare(document, request)
}

func (transaction *transaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Page{}, err
	}
	if err := transaction.ready(ctx); err != nil {
		return store.Page{}, err
	}
	transaction.event("list")
	var documents []store.Document
	for _, document := range transaction.collection(string(request.Collection.ID)) {
		if matchesDeletion(document, request.Deletion) && (!request.PublishedOnly || request.Collection.Versions == nil || document.Status == store.StatusPublished) && matchesRequest(document, request) {
			documents = append(documents, store.CloneDocument(document))
		}
	}
	sorts := stableSort(request.Sort)
	sortRequest := request
	// All-locales responses retain locale maps, but ordering has one stable
	// scalar meaning: the request's primary/default locale, matching PostgreSQL.
	sortRequest.AllLocales = false
	sort.SliceStable(documents, func(left, right int) bool {
		return compareDocuments(localizedForRequest(documents[left], sortRequest), localizedForRequest(documents[right], sortRequest), sorts) < 0
	})
	page, limit, start, end := store.ListPageBounds(request.Page, request.Limit, len(documents))
	if len(request.Populate) != 0 && request.PopulationBudget == nil {
		request.PopulationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
	}
	selected := make([]store.Document, end-start)
	for index, document := range documents[start:end] {
		prepared, err := transaction.prepare(document, request)
		if err != nil {
			return store.Page{}, err
		}
		selected[index] = prepared
	}
	return store.Page{Documents: selected, Page: page, Limit: limit, Total: len(documents)}, nil
}

func (transaction *transaction) Distinct(ctx context.Context, request store.DistinctRequest) (store.DistinctPage, error) {
	if err := primitivefield.ValidateRequest(store.Request{Collection: request.Collection, Filter: request.Filter, Access: request.Access}); err != nil {
		return store.DistinctPage{}, err
	}
	if err := transaction.ready(ctx); err != nil {
		return store.DistinctPage{}, err
	}
	if err := store.ValidateDistinctRequest(request); err != nil {
		return store.DistinctPage{}, err
	}
	transaction.event("distinct")
	documentRequest := store.Request{
		Collection: request.Collection, Filter: request.Filter, Access: request.Access,
		PublishedOnly: request.PublishedOnly, Deletion: request.Deletion,
		Locales: request.Locales, LocaleChain: request.LocaleChain,
	}
	unique := make(map[string]store.Value)
	for _, document := range transaction.collection(string(request.Collection.ID)) {
		if err := ctx.Err(); err != nil {
			return store.DistinctPage{}, err
		}
		if !matchesDeletion(document, request.Deletion) || request.PublishedOnly && request.Collection.Versions != nil && document.Status != store.StatusPublished || !matchesRequest(document, documentRequest) {
			continue
		}
		document = localizedForRequest(document, documentRequest)
		value, exists := documentValue(document, request.Field.Segments())
		if !exists {
			value = store.Null()
		}
		key, err := distinctValueKey(value)
		if err != nil {
			return store.DistinctPage{}, err
		}
		unique[key] = value
	}
	values := make([]store.Value, 0, len(unique))
	for _, value := range unique {
		values = append(values, value)
	}
	sort.Slice(values, func(left, right int) bool { return compareDistinctValues(values[left], values[right]) < 0 })
	page, limit, start, end := store.ListPageBounds(request.Page, request.Limit, len(values))
	return store.DistinctPage{Values: append([]store.Value(nil), values[start:end]...), Page: page, Limit: limit, Total: len(values)}, nil
}

func distinctValueKey(value store.Value) (string, error) {
	switch value.Kind() {
	case store.ValueNull:
		return "null", nil
	case store.ValueString:
		text, _ := value.StringValue()
		return "string:" + text, nil
	case store.ValueNumber:
		number, _ := value.NumberValue()
		if number == 0 {
			number = 0 // Collapse negative zero to ordinary numeric equality.
		}
		return "number:" + strconv.FormatFloat(number, 'g', -1, 64), nil
	case store.ValueBoolean:
		boolean, _ := value.BooleanValue()
		return "boolean:" + strconv.FormatBool(boolean), nil
	default:
		return "", fmt.Errorf("distinct value has unsupported kind %q", value.Kind())
	}
}

func compareDistinctValues(left, right store.Value) int {
	if left.Kind() == store.ValueNull {
		if right.Kind() == store.ValueNull {
			return 0
		}
		return -1
	}
	if right.Kind() == store.ValueNull {
		return 1
	}
	if leftNumber, ok := left.NumberValue(); ok {
		rightNumber, _ := right.NumberValue()
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	}
	if leftBoolean, ok := left.BooleanValue(); ok {
		rightBoolean, _ := right.BooleanValue()
		if leftBoolean == rightBoolean {
			return 0
		}
		if leftBoolean {
			return 1
		}
		return -1
	}
	leftText, _ := left.StringValue()
	rightText, _ := right.StringValue()
	return strings.Compare(leftText, rightText)
}

func (transaction *transaction) ListWindow(ctx context.Context, request store.Request) (store.Window, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Window{}, err
	}
	if err := transaction.ready(ctx); err != nil {
		return store.Window{}, err
	}
	if err := store.ValidateListWindowRequest(request); err != nil {
		return store.Window{}, err
	}
	transaction.event("list-window")
	// This strict non-production fake has no physical indexes and therefore
	// scans its already-snapshotted map. Unlike List it never clones or sorts the
	// full match set: retained candidates and prepared documents remain bounded
	// by Limit+1 and Limit respectively.
	capacity := request.Limit + 1
	candidates := make([]store.Document, 0, capacity)
	for _, document := range transaction.collection(string(request.Collection.ID)) {
		if err := ctx.Err(); err != nil {
			return store.Window{}, err
		}
		if !matchesDeletion(document, request.Deletion) {
			continue
		}
		value, exists := documentValue(document, request.IndexWindow.Path.Segments())
		key, valid := value.StringValue()
		if !exists || !valid || key < request.IndexWindow.LowerBound || key >= request.IndexWindow.UpperBound {
			continue
		}
		index := sort.Search(len(candidates), func(index int) bool {
			candidate, _ := documentValue(candidates[index], request.IndexWindow.Path.Segments())
			candidateKey, _ := candidate.StringValue()
			return candidateKey >= key
		})
		if len(candidates) == capacity && index == capacity {
			continue
		}
		candidates = append(candidates, store.Document{})
		copy(candidates[index+1:], candidates[index:])
		candidates[index] = document
		if len(candidates) > capacity {
			candidates = candidates[:capacity]
		}
	}
	hasMore := len(candidates) > request.Limit
	if hasMore {
		candidates = candidates[:request.Limit]
	}
	if len(request.Populate) != 0 && request.PopulationBudget == nil {
		request.PopulationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
	}
	documents := make([]store.Document, len(candidates))
	for index, document := range candidates {
		prepared, err := transaction.prepare(document, request)
		if err != nil {
			return store.Window{}, err
		}
		documents[index] = prepared
		transaction.event("list-window-materialize")
	}
	return store.Window{Documents: documents, HasMore: hasMore}, nil
}

func (transaction *transaction) ReferencedUploadObjects(ctx context.Context, request store.UploadReferenceRequest) ([]string, error) {
	if err := transaction.ready(ctx); err != nil {
		return nil, err
	}
	if len(request.ObjectKeys) == 0 || len(request.ObjectKeys) > store.MaxUploadReferenceCandidates {
		return nil, fmt.Errorf("upload reference lookup requires between 1 and %d object keys", store.MaxUploadReferenceCandidates)
	}
	candidates := make(map[string]struct{}, len(request.ObjectKeys))
	for _, key := range request.ObjectKeys {
		if key == "" {
			return nil, fmt.Errorf("upload reference lookup contains an empty object key")
		}
		if _, duplicate := candidates[key]; duplicate {
			return nil, fmt.Errorf("upload reference lookup contains duplicate object key %q", key)
		}
		candidates[key] = struct{}{}
	}
	referenced := make(map[string]struct{}, len(candidates))
	for _, collection := range request.Collections {
		if collection.Upload == nil {
			return nil, fmt.Errorf("upload reference lookup collection %q is not upload-enabled", collection.ID)
		}
		for _, document := range transaction.collection(string(collection.ID)) {
			collectUploadObjectReferences(document.Values, candidates, referenced)
		}
		for _, versions := range transaction.versions[string(collection.ID)] {
			for _, version := range versions {
				collectUploadObjectReferences(version.Snapshot.Values, candidates, referenced)
			}
		}
	}
	result := make([]string, 0, len(referenced))
	for key := range referenced {
		result = append(result, key)
	}
	sort.Strings(result)
	transaction.event("upload-object-references")
	return result, nil
}

func collectUploadObjectReferences(values store.Values, candidates, referenced map[string]struct{}) {
	if key, valid := values["objectKey"].StringValue(); valid {
		if _, wanted := candidates[key]; wanted {
			referenced[key] = struct{}{}
		}
	}
	for _, encoded := range values["sizes"].Entries() {
		key, valid := encoded.Get("objectKey").StringValue()
		if !valid {
			continue
		}
		if _, wanted := candidates[key]; wanted {
			referenced[key] = struct{}{}
		}
	}
}

func (transaction *transaction) ResolveFilteredSelection(ctx context.Context, request store.FilteredSelectionRequest) (store.FilteredSelection, error) {
	if err := transaction.ready(ctx); err != nil {
		return store.FilteredSelection{}, err
	}
	transaction.event("resolve-filtered-selection")
	limit := request.Limit
	if limit < 1 {
		return store.FilteredSelection{}, fmt.Errorf("filtered selection limit must be positive")
	}
	capacity := limit + 1
	ids := make([]string, 0, min(capacity, len(transaction.collection(string(request.Collection.ID)))))
	for id, document := range transaction.collection(string(request.Collection.ID)) {
		if err := ctx.Err(); err != nil {
			return store.FilteredSelection{}, err
		}
		storeRequest := store.Request{Collection: request.Collection, Filter: request.Filter, Access: request.Access, Deletion: request.Deletion, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales}
		if matchesDeletion(document, request.Deletion) && matchesRequest(document, storeRequest) {
			index := sort.SearchStrings(ids, id)
			if len(ids) == capacity && index == capacity {
				continue
			}
			ids = append(ids, "")
			copy(ids[index+1:], ids[index:])
			ids[index] = id
			if len(ids) > capacity {
				ids = ids[:capacity]
			}
		}
	}
	overflow := len(ids) > limit
	if overflow {
		ids = ids[:limit]
	}
	return store.FilteredSelection{IDs: ids, Overflow: overflow}, nil
}

func stableSort(sorts []query.Sort) []query.Sort {
	result := append([]query.Sort(nil), sorts...)
	for _, term := range result {
		if term.Path.String() == "id" {
			return result
		}
	}
	id, _ := query.NewPath("id")
	result = append(result, query.Sort{Path: id, Direction: query.Ascending})
	return result
}

func compareDocuments(left, right store.Document, sorts []query.Sort) int {
	for _, term := range sorts {
		if leftTime, timestamp := documentTimestamp(left, term.Path); timestamp {
			rightTime, _ := documentTimestamp(right, term.Path)
			comparison := compareTimes(leftTime, rightTime)
			if comparison != 0 {
				if term.Direction == query.Descending {
					return -comparison
				}
				return comparison
			}
			continue
		}
		leftValue, _ := documentValue(left, term.Path.Segments())
		rightValue, _ := documentValue(right, term.Path.Segments())
		comparison := 0
		leftNumber, leftIsNumber := leftValue.NumberValue()
		rightNumber, rightIsNumber := rightValue.NumberValue()
		if leftIsNumber && rightIsNumber {
			if leftNumber < rightNumber {
				comparison = -1
			} else if leftNumber > rightNumber {
				comparison = 1
			}
		} else if leftBoolean, leftIsBoolean := leftValue.BooleanValue(); leftIsBoolean {
			if rightBoolean, rightIsBoolean := rightValue.BooleanValue(); rightIsBoolean && leftBoolean != rightBoolean {
				if leftBoolean {
					comparison = 1
				} else {
					comparison = -1
				}
			}
		} else {
			leftText, _ := leftValue.StringValue()
			rightText, _ := rightValue.StringValue()
			if leftText < rightText {
				comparison = -1
			} else if leftText > rightText {
				comparison = 1
			}
		}
		if comparison != 0 {
			if term.Direction == query.Descending {
				return -comparison
			}
			return comparison
		}
	}
	return 0
}

func (transaction *transaction) prepare(document store.Document, request store.Request) (store.Document, error) {
	document = store.CloneDocument(document)
	populationBudget := request.PopulationBudget
	if len(request.Populate) != 0 && populationBudget == nil {
		populationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
		request.PopulationBudget = populationBudget
	}
	for _, population := range request.Populate {
		field, found := populationwalk.FieldAtPath(request.Collection.Fields, population.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			continue
		}
		var populationError error
		mapped, _ := populationwalk.MapAtPath(request.Collection.Fields, document.Values, population.Path, populationwalk.LocaleSelection{
			All: request.AllLocales, Chain: request.LocaleChain,
		}, func(_ schema.Field, value store.Value) store.Value {
			if populationError != nil {
				return value
			}
			return populateValue(value, relationship, func(collectionID schema.StableID, id string) (store.Document, bool) {
				target, found := transaction.collection(string(collectionID))[id]
				targetSchema := request.Collections[collectionID]
				found = found && target.DeletedAt == nil && (!request.PublishedOnly || targetSchema.Versions == nil || target.Status == store.StatusPublished)
				if access := request.PopulationAccess[collectionID]; found && access != nil {
					found = matchesRequest(target, store.Request{
						Collection: targetSchema, Access: access, Locales: request.Locales,
						LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
					})
				}
				if found {
					if population.Depth > 1 {
						var err error
						target, err = transaction.prepare(target, store.Request{Collection: targetSchema, Collections: request.Collections, Populate: populationwalk.DepthPopulations(targetSchema, population.Depth-1), PopulationAccess: request.PopulationAccess, PopulationBudget: populationBudget, PublishedOnly: request.PublishedOnly, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales})
						if err != nil {
							populationError = err
							return store.Document{}, false
						}
					}
					target = localizedForRequest(target, store.Request{Collection: targetSchema, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales})
					target = project(target, population.Select)
					if err := populationBudget.ConsumeDocument(target); err != nil {
						populationError = err
						return store.Document{}, false
					}
				}
				return target, found
			})
		})
		if populationError != nil {
			return store.Document{}, populationError
		}
		document.Values = mapped
	}
	return project(document, request.Select), nil
}

func populateValue(value store.Value, relationship *schema.RelationshipField, lookup func(schema.StableID, string) (store.Document, bool)) store.Value {
	if relationship.HasMany {
		if value.Kind() != store.ValueList {
			return value
		}
		populated := make([]store.Value, 0, value.Len())
		for item := range value.Elements() {
			populated = append(populated, populateReference(item, relationship, lookup))
		}
		return store.List(populated...)
	}
	return populateReference(value, relationship, lookup)
}

func populateReference(value store.Value, relationship *schema.RelationshipField, lookup func(schema.StableID, string) (store.Document, bool)) store.Value {
	if !relationship.Polymorphic {
		id, valid := value.StringValue()
		if !valid {
			return value
		}
		if document, found := lookup(relationship.CollectionID, id); found {
			return store.Populated(document)
		}
		return value
	}
	object, valid := value.CopyObject()
	if !valid {
		return value
	}
	slug, _ := object["relationTo"].StringValue()
	id, _ := object["id"].StringValue()
	for _, target := range relationship.Targets {
		if string(target.CollectionSlug) == slug {
			if document, found := lookup(target.CollectionID, id); found {
				object["id"] = store.Populated(document)
			}
			break
		}
	}
	return store.Object(object)
}

func project(document store.Document, selection []query.Path) store.Document {
	if selection == nil {
		return store.CloneDocument(document)
	}
	values := make(store.Values)
	for _, path := range selection {
		segments := path.Segments()
		if len(segments) == 1 {
			if value, exists := document.Values[segments[0]]; exists {
				values[segments[0]] = value
			}
		}
	}
	document.Values = values
	return store.CloneDocument(document)
}

func (transaction *transaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request.Request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.writable(ctx); err != nil {
		return store.Document{}, err
	}
	transaction.event("update")
	collection := transaction.collection(string(request.Collection.ID))
	document, exists := collection[request.ID]
	if !exists || !matchesDeletion(document, request.Deletion) || !matchesRequest(document, request.Request) {
		return store.Document{}, store.ErrNotFound
	}
	if request.ExpectedRevision > 0 && document.Revision != request.ExpectedRevision {
		return store.Document{}, store.ErrConflict
	}
	if request.ReplaceValues {
		document.Values = store.CloneValues(request.Values)
	} else {
		document.Values = localization.MergeStoragePatch(request.Collection.Fields, document.Values, request.Values)
	}
	canonicalizeTestAuthIdentity(request.Collection, document.Values)
	document.UpdatedAt = transaction.store.now().UTC()
	if request.Status != nil {
		document.Status = *request.Status
	}
	if request.Collection.Versions != nil {
		document.Revision++
	}
	if uniqueConflict(collection, request.Collection, document, document.ID) {
		return store.Document{}, store.ErrConflict
	}
	collection[document.ID] = document
	if err := transaction.replaceReferenceEntries(request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return store.CloneDocument(document), nil
}

func (transaction *transaction) SaveVersion(ctx context.Context, collection schema.Collection, document store.Document, maximum int) (store.Version, error) {
	if err := transaction.writable(ctx); err != nil {
		return store.Version{}, err
	}
	byDocument := transaction.versions[string(collection.ID)]
	if byDocument == nil {
		byDocument = make(map[string][]store.Version)
		transaction.versions[string(collection.ID)] = byDocument
	}
	version := store.Version{ID: fmt.Sprintf("%s:%d", document.ID, document.Revision), DocumentID: document.ID, Revision: document.Revision, Status: document.Status, Snapshot: store.CloneDocument(document), CreatedAt: transaction.store.now().UTC()}
	items := byDocument[document.ID]
	replaced := false
	for index, existing := range items {
		if existing.Revision != document.Revision {
			continue
		}
		version.CreatedAt = existing.CreatedAt
		items[index] = version
		replaced = true
		break
	}
	if !replaced {
		items = append(items, version)
	}
	if maximum > 0 && len(items) > maximum {
		items = append([]store.Version(nil), items[len(items)-maximum:]...)
	}
	byDocument[document.ID] = items
	return cloneVersion(version), nil
}

func (transaction *transaction) ListVersions(ctx context.Context, request store.VersionRequest) ([]store.Version, error) {
	if err := primitivefield.ValidateNode(request.Collection.Fields, request.Access); err != nil {
		return nil, err
	}
	if err := transaction.ready(ctx); err != nil {
		return nil, err
	}
	items := transaction.versions[string(request.Collection.ID)][request.DocumentID]
	result := make([]store.Version, 0, len(items))
	for index := len(items) - 1; index >= 0; index-- {
		storeRequest := store.Request{
			Collection: request.Collection, Access: request.Access, Locales: request.Locales,
			LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
		}
		if matchesRequest(items[index].Snapshot, storeRequest) {
			result = append(result, cloneVersion(items[index]))
		}
	}
	return result, nil
}

func (transaction *transaction) FindVersion(ctx context.Context, collection schema.Collection, documentID string, revision int) (store.Version, error) {
	if err := transaction.ready(ctx); err != nil {
		return store.Version{}, err
	}
	for _, version := range transaction.versions[string(collection.ID)][documentID] {
		if version.Revision == revision {
			return cloneVersion(version), nil
		}
	}
	return store.Version{}, store.ErrNotFound
}

func (transaction *transaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.writable(ctx); err != nil {
		return store.Document{}, err
	}
	transaction.event("delete")
	collection := transaction.collection(string(request.Collection.ID))
	document, exists := collection[request.ID]
	if !exists || !matchesDeletion(document, request.Deletion) || !matchesRequest(document, request) {
		return store.Document{}, store.ErrNotFound
	}
	delete(collection, request.ID)
	transaction.deleteReferenceEntries(store.DocumentReference{CollectionID: request.Collection.ID, DocumentID: request.ID})
	return store.CloneDocument(document), nil
}

func (transaction *transaction) ApplyReferenceDelete(ctx context.Context, request store.ReferenceDeleteRequest) error {
	if err := transaction.writable(ctx); err != nil {
		return err
	}
	if request.Target.CollectionID == "" || request.Target.DocumentID == "" {
		return fmt.Errorf("reference delete requires a target collection and document ID")
	}
	ignored := make(map[store.DocumentReference]struct{}, len(request.IgnoreOwners))
	for _, owner := range request.IgnoreOwners {
		if owner == request.Target {
			ignored[owner] = struct{}{}
			continue
		}
		if _, exists := request.Collections[owner.CollectionID]; !exists {
			return fmt.Errorf("ignored reference owner collection %q is unavailable", owner.CollectionID)
		}
		if _, stillExists := transaction.collection(string(owner.CollectionID))[owner.DocumentID]; !stillExists {
			ignored[owner] = struct{}{}
		}
	}
	entries := make([]referenceindex.Entry, 0)
	for _, entry := range transaction.references {
		if entry.Target != request.Target {
			continue
		}
		if _, skip := ignored[entry.Owner]; skip {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool {
		return referenceEntryKey(entries[left]) < referenceEntryKey(entries[right])
	})

	constraints := make(map[store.ReferenceConstraint]struct{})
	for _, entry := range entries {
		collection, exists := request.Collections[entry.Owner.CollectionID]
		if !exists {
			return fmt.Errorf("reference owner collection %q is unavailable", entry.Owner.CollectionID)
		}
		field, _, exists := referenceindex.FindReferenceField(collection, entry.FieldID)
		if !exists {
			return fmt.Errorf("reference owner field %q is unavailable", entry.FieldID)
		}
		action := schema.ReferenceDeleteNullify
		if field.Relationship != nil {
			action = field.Relationship.OnDelete
		} else if field.Upload != nil {
			action = field.Upload.OnDelete
		}
		if action == schema.ReferenceDeleteRestrict {
			constraints[store.ReferenceConstraint{OwnerCollectionID: entry.Owner.CollectionID, FieldID: entry.FieldID}] = struct{}{}
		} else if action != schema.ReferenceDeleteNullify {
			return fmt.Errorf("reference owner field %q has unsupported delete action %q", entry.FieldID, action)
		}
	}
	if len(constraints) != 0 {
		ordered := make([]store.ReferenceConstraint, 0, len(constraints))
		for constraint := range constraints {
			ordered = append(ordered, constraint)
		}
		sort.Slice(ordered, func(left, right int) bool {
			if ordered[left].OwnerCollectionID != ordered[right].OwnerCollectionID {
				return ordered[left].OwnerCollectionID < ordered[right].OwnerCollectionID
			}
			return ordered[left].FieldID < ordered[right].FieldID
		})
		return &store.DeleteRestrictedError{Constraints: ordered}
	}

	owners := make(map[store.DocumentReference]struct{}, len(entries))
	for _, entry := range entries {
		owners[entry.Owner] = struct{}{}
	}
	orderedOwners := make([]store.DocumentReference, 0, len(owners))
	for owner := range owners {
		orderedOwners = append(orderedOwners, owner)
	}
	sort.Slice(orderedOwners, func(left, right int) bool {
		if orderedOwners[left].CollectionID != orderedOwners[right].CollectionID {
			return orderedOwners[left].CollectionID < orderedOwners[right].CollectionID
		}
		return orderedOwners[left].DocumentID < orderedOwners[right].DocumentID
	})
	for _, owner := range orderedOwners {
		collection := request.Collections[owner.CollectionID]
		document, exists := transaction.collection(string(owner.CollectionID))[owner.DocumentID]
		if !exists {
			// A stale derived row must not let hard delete mutate or disclose an
			// unrelated owner. Remove it and continue; the target remains safe.
			transaction.deleteReferenceEntries(owner)
			continue
		}
		values, changed, referenceErr := referenceindex.NullifyTarget(collection, document.Values, request.Target)
		if referenceErr != nil {
			return referenceErr
		}
		if !changed {
			return fmt.Errorf("reference index for owner collection %q is inconsistent with current values", owner.CollectionID)
		}
		document.Values = values
		transaction.collection(string(owner.CollectionID))[owner.DocumentID] = document
		if err := transaction.replaceReferenceEntries(collection, document); err != nil {
			return err
		}
	}
	transaction.event("apply-reference-delete")
	return nil
}

func (transaction *transaction) replaceReferenceEntries(collection schema.Collection, document store.Document) error {
	owner := store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}
	transaction.deleteReferenceEntries(owner)
	entries, referenceErr := referenceindex.Collect(collection, document)
	if referenceErr != nil {
		return referenceErr
	}
	for _, entry := range entries {
		transaction.references[referenceEntryKey(entry)] = entry
	}
	return nil
}

func (transaction *transaction) deleteReferenceEntries(owner store.DocumentReference) {
	for key, entry := range transaction.references {
		if entry.Owner == owner {
			delete(transaction.references, key)
		}
	}
}

func referenceEntryKey(entry referenceindex.Entry) string {
	return string(entry.Owner.CollectionID) + "\x00" + entry.Owner.DocumentID + "\x00" + string(entry.FieldID) + "\x00" +
		string(entry.Target.CollectionID) + "\x00" + entry.Target.DocumentID + "\x00" + string(entry.Locale) + fmt.Sprintf("\x00%09d", entry.Occurrence)
}

func cloneReferenceEntries(source map[string]referenceindex.Entry) map[string]referenceindex.Entry {
	cloned := make(map[string]referenceindex.Entry, len(source))
	for key, entry := range source {
		cloned[key] = entry
	}
	return cloned
}

func (transaction *transaction) DeleteDocumentState(ctx context.Context, reference store.DocumentReference) error {
	if err := transaction.writable(ctx); err != nil {
		return err
	}
	if reference.CollectionID == "" || reference.DocumentID == "" {
		return fmt.Errorf("document state cleanup requires collection and document IDs")
	}
	if byDocument := transaction.versions[string(reference.CollectionID)]; byDocument != nil {
		delete(byDocument, reference.DocumentID)
	}
	if transaction.deletedState == nil {
		transaction.deletedState = make(map[store.DocumentReference]struct{})
	}
	authKey := string(reference.CollectionID) + ":" + reference.DocumentID
	delete(transaction.passwords, authKey)
	delete(transaction.credentials, authKey)
	transaction.deletedState[reference] = struct{}{}
	transaction.event("delete-document-state")
	return nil
}

func (transaction *transaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.writable(ctx); err != nil {
		return store.Document{}, err
	}
	transaction.event("trash")
	collection := transaction.collection(string(request.Collection.ID))
	document, exists := collection[request.ID]
	if !exists || document.DeletedAt != nil || !matchesRequest(document, request) {
		return store.Document{}, store.ErrNotFound
	}
	now := transaction.store.now().UTC()
	document.DeletedAt = &now
	document.UpdatedAt = now
	collection[request.ID] = document
	return store.CloneDocument(document), nil
}

func (transaction *transaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.writable(ctx); err != nil {
		return store.Document{}, err
	}
	transaction.event("restore")
	collection := transaction.collection(string(request.Collection.ID))
	document, exists := collection[request.ID]
	if !exists || document.DeletedAt == nil || !matchesRequest(document, request) {
		return store.Document{}, store.ErrNotFound
	}
	document.DeletedAt = nil
	document.UpdatedAt = transaction.store.now().UTC()
	if uniqueConflict(collection, request.Collection, document, document.ID) {
		return store.Document{}, store.ErrConflict
	}
	collection[request.ID] = document
	return store.CloneDocument(document), nil
}

func (transaction *transaction) Commit(ctx context.Context) error {
	defer transaction.releaseAuthBootstrap()
	if err := transaction.ready(ctx); err != nil {
		return err
	}
	if transaction.readOnly {
		transaction.event("commit")
		transaction.done = true
		return nil
	}
	transaction.store.mu.Lock()
	defer transaction.store.mu.Unlock()
	for key := range transaction.passwords {
		_, exists := transaction.store.passwords[key]
		if exists && !transaction.deletesAuthKey(key) {
			return store.ErrConflict
		}
	}
	for key := range transaction.credentialUpdates {
		if _, exists := transaction.store.passwords[key]; !exists {
			return store.ErrNotFound
		}
	}
	transaction.store.documents = cloneCollections(transaction.documents)
	transaction.store.versions = cloneVersions(transaction.versions)
	transaction.store.references = cloneReferenceEntries(transaction.references)
	for reference := range transaction.deletedState {
		transaction.store.deleteDocumentState(reference)
	}
	for key, hash := range transaction.passwords {
		transaction.store.passwords[key] = append([]byte(nil), hash...)
		transaction.store.credentials[key] = transaction.credentials[key]
	}
	for key, credential := range transaction.credentialUpdates {
		transaction.store.credentials[key] = credential
	}
	transaction.store.events = append(transaction.store.events, "commit")
	transaction.done = true
	return nil
}

func (transaction *transaction) deletesAuthKey(key string) bool {
	for reference := range transaction.deletedState {
		if string(reference.CollectionID)+":"+reference.DocumentID == key {
			return true
		}
	}
	return false
}

func (backend *Store) deleteDocumentState(reference store.DocumentReference) {
	authKey := string(reference.CollectionID) + ":" + reference.DocumentID
	delete(backend.passwords, authKey)
	delete(backend.credentials, authKey)
	for tokenHash, current := range backend.sessions {
		if current.record.CollectionID == reference.CollectionID && current.record.UserID == reference.DocumentID {
			delete(backend.sessions, tokenHash)
		}
	}
	for tokenHash, current := range backend.authTokens {
		if current.CollectionID == reference.CollectionID && current.UserID == reference.DocumentID {
			delete(backend.authTokens, tokenHash)
		}
	}
	for id, current := range backend.apiKeys {
		if current.CollectionID == reference.CollectionID && current.UserID == reference.DocumentID {
			delete(backend.apiKeys, id)
		}
	}
	for id, task := range backend.tasks {
		isTarget := task.Target != nil && *task.Target == reference
		isRequester := task.RequestedBy != nil && *task.RequestedBy == reference
		if isTarget || isRequester {
			delete(backend.tasks, id)
		}
	}
	for key, preference := range backend.preferences {
		if preference.CollectionID == reference.CollectionID && preference.UserID == reference.DocumentID {
			delete(backend.preferences, key)
		}
	}
	for key, lock := range backend.documentLocks {
		isTarget := lock.CollectionID == reference.CollectionID && lock.DocumentID == reference.DocumentID
		isOwner := lock.OwnerCollectionID == reference.CollectionID && lock.OwnerID == reference.DocumentID
		if isTarget || isOwner {
			delete(backend.documentLocks, key)
		}
	}
}

func (backend *Store) activeDocument(collectionID schema.StableID, documentID string) bool {
	document, exists := backend.documents[string(collectionID)][documentID]
	return exists && document.DeletedAt == nil
}

func cloneVersion(version store.Version) store.Version {
	version.Snapshot = store.CloneDocument(version.Snapshot)
	return version
}

func cloneVersions(source map[string]map[string][]store.Version) map[string]map[string][]store.Version {
	cloned := make(map[string]map[string][]store.Version, len(source))
	for collection, documents := range source {
		cloned[collection] = make(map[string][]store.Version, len(documents))
		for documentID, versions := range documents {
			items := make([]store.Version, len(versions))
			for index, version := range versions {
				items[index] = cloneVersion(version)
			}
			cloned[collection][documentID] = items
		}
	}
	return cloned
}

func (transaction *transaction) Rollback(context.Context) error {
	defer transaction.releaseAuthBootstrap()
	if transaction.done {
		return fmt.Errorf("transaction already completed")
	}
	transaction.store.mu.Lock()
	transaction.store.events = append(transaction.store.events, "rollback")
	transaction.store.mu.Unlock()
	transaction.done = true
	return nil
}

func (transaction *transaction) releaseAuthBootstrap() {
	if !transaction.authBootstrapLocked {
		return
	}
	transaction.authBootstrapLocked = false
	transaction.store.authBootstrapMu.Unlock()
}

func (transaction *transaction) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if transaction.done {
		return fmt.Errorf("transaction already completed")
	}
	return nil
}

func (transaction *transaction) writable(ctx context.Context) error {
	if err := transaction.ready(ctx); err != nil {
		return err
	}
	if transaction.readOnly {
		return fmt.Errorf("snapshot transaction is read-only")
	}
	return nil
}

func (transaction *transaction) event(name string) {
	transaction.store.mu.Lock()
	transaction.store.events = append(transaction.store.events, name)
	transaction.store.mu.Unlock()
}

func (transaction *transaction) collection(id string) map[string]store.Document {
	collection := transaction.documents[id]
	if collection == nil {
		collection = make(map[string]store.Document)
		transaction.documents[id] = collection
	}
	return collection
}

func cloneCollections(source map[string]map[string]store.Document) map[string]map[string]store.Document {
	cloned := make(map[string]map[string]store.Document, len(source))
	for collectionID, documents := range source {
		cloned[collectionID] = make(map[string]store.Document, len(documents))
		for id, document := range documents {
			cloned[collectionID][id] = store.CloneDocument(document)
		}
	}
	return cloned
}

func uniqueConflict(documents map[string]store.Document, collection schema.Collection, candidate store.Document, except string) bool {
	for _, field := range collection.Fields {
		if !field.Unique {
			continue
		}
		candidateValue, exists := candidate.Values[field.Name]
		if !exists || candidateValue.Kind() == store.ValueNull {
			continue
		}
		for id, document := range documents {
			if id == except || document.DeletedAt != nil {
				continue
			}
			value, exists := document.Values[field.Name]
			if exists && ((!field.Localized && uniqueFieldValuesEqual(collection, field, value, candidateValue)) || field.Localized && localizedValuesConflict(value, candidateValue)) {
				return true
			}
		}
	}
	return compoundUniqueConflict(documents, collection, candidate, except)
}

func uniqueFieldValuesEqual(collection schema.Collection, field schema.Field, left, right store.Value) bool {
	if collection.Auth != nil && field.Name == collection.Auth.IdentityField {
		leftText, leftOK := left.StringValue()
		rightText, rightOK := right.StringValue()
		return leftOK && rightOK && leftText == rightText
	}
	return equalValue(left, right)
}

func canonicalizeTestAuthIdentity(collection schema.Collection, values store.Values) {
	if collection.Auth == nil || values == nil {
		return
	}
	identity, exists := values[collection.Auth.IdentityField]
	if !exists {
		return
	}
	text, valid := identity.StringValue()
	if valid {
		values[collection.Auth.IdentityField] = store.String(store.CanonicalAuthIdentity(text))
	}
}

func compoundUniqueConflict(documents map[string]store.Document, collection schema.Collection, candidate store.Document, except string) bool {
	for _, index := range collection.Indexes {
		if !index.Unique {
			continue
		}
		chains := make([][]schema.Field, len(index.Fields))
		localized := false
		locales := make(map[string]struct{})
		for fieldIndex, path := range index.Fields {
			chains[fieldIndex] = indexedFieldChain(collection.Fields, path.Segments())
			localized = localized || indexedChainLocalized(chains[fieldIndex])
			collectIndexedLocales(candidate.Values, chains[fieldIndex], 0, locales)
		}
		if !localized {
			locales[""] = struct{}{}
		}
		for locale := range locales {
			candidateTuple, comparable := indexedTuple(candidate, chains, locale)
			if !comparable {
				continue
			}
			for id, document := range documents {
				if id == except || document.DeletedAt != nil {
					continue
				}
				tuple, comparable := indexedTuple(document, chains, locale)
				if !comparable {
					continue
				}
				equal := true
				for valueIndex := range tuple {
					equal = equal && equalValue(tuple[valueIndex], candidateTuple[valueIndex])
				}
				if equal {
					return true
				}
			}
		}
	}
	return false
}

func indexedFieldChain(fields []schema.Field, segments []string) []schema.Field {
	if len(segments) == 0 {
		return nil
	}
	for _, candidate := range fields {
		if candidate.Name != segments[0] {
			continue
		}
		chain := []schema.Field{candidate}
		if len(segments) == 1 {
			return chain
		}
		if candidate.Nested == nil {
			return nil
		}
		return append(chain, indexedFieldChain(candidate.Nested.ResolvedFields(), segments[1:])...)
	}
	return nil
}

func indexedChainLocalized(chain []schema.Field) bool {
	for _, candidate := range chain {
		if candidate.Localized {
			return true
		}
	}
	return false
}

func collectIndexedLocales(values store.Values, chain []schema.Field, position int, locales map[string]struct{}) {
	if position >= len(chain) {
		return
	}
	collectIndexedLocaleValue(values[chain[position].Name], chain, position, locales)
}

func collectIndexedLocaleValue(value store.Value, chain []schema.Field, position int, locales map[string]struct{}) {
	if value.Kind() == store.ValueNull {
		return
	}
	if chain[position].Localized {
		for locale, localizedValue := range value.Entries() {
			locales[locale] = struct{}{}
			if position+1 < len(chain) {
				collectIndexedLocaleValue(localizedValue.Get(chain[position+1].Name), chain, position+1, locales)
			}
		}
		return
	}
	if position+1 < len(chain) {
		collectIndexedLocaleValue(value.Get(chain[position+1].Name), chain, position+1, locales)
	}
}

func indexedTuple(document store.Document, chains [][]schema.Field, locale string) ([]store.Value, bool) {
	tuple := make([]store.Value, len(chains))
	for index, chain := range chains {
		value, exists := indexedValue(document.Values, chain, locale)
		if !exists || value.Kind() == store.ValueNull {
			return nil, false
		}
		tuple[index] = value
	}
	return tuple, true
}

func indexedValue(values store.Values, chain []schema.Field, locale string) (store.Value, bool) {
	if len(chain) == 0 {
		return store.Value{}, false
	}
	value, exists := values[chain[0].Name]
	for position, candidate := range chain {
		if position != 0 {
			value, exists = value.Lookup(candidate.Name)
		}
		if !exists {
			return store.Value{}, false
		}
		if candidate.Localized {
			value, exists = value.Lookup(locale)
			if !exists {
				return store.Value{}, false
			}
		}
	}
	return value, true
}

func localizedValuesConflict(left, right store.Value) bool {
	if left.Kind() != store.ValueObject || right.Kind() != store.ValueObject {
		return equalValue(left, right)
	}
	for locale, rightValue := range right.Entries() {
		if rightValue.Kind() == store.ValueNull {
			continue
		}
		if leftValue, exists := left.Lookup(locale); exists && leftValue.Kind() != store.ValueNull && equalValue(leftValue, rightValue) {
			return true
		}
	}
	return false
}

func localizedForRequest(document store.Document, request store.Request) store.Document {
	selection := localization.Selection{
		Configured: append([]schema.LocaleCode(nil), request.Locales...),
		Chain:      append([]schema.LocaleCode(nil), request.LocaleChain...),
		All:        request.AllLocales,
	}
	if len(selection.Chain) != 0 {
		selection.Locale = selection.Chain[0]
	}
	return localization.ProjectDocument(document, request.Collection.Fields, selection)
}

func matchesDeletion(document store.Document, mode store.DeletionMode) bool {
	switch mode {
	case store.DeletionAll:
		return true
	case store.DeletionTrash:
		return document.DeletedAt != nil
	default:
		return document.DeletedAt == nil
	}
}

func matchesRequest(document store.Document, request store.Request) bool {
	filterRequest := request
	filterRequest.AllLocales = false
	if !matchesAll(document, filterRequest, request.Filter) {
		return false
	}
	if request.Access == nil {
		return true
	}
	if !request.AllLocales || len(request.Locales) == 0 {
		return matchesAll(document, request, request.Access)
	}
	for _, locale := range request.Locales {
		localeRequest := request
		localeRequest.AllLocales = false
		localeRequest.LocaleChain = []schema.LocaleCode{locale}
		if !matchesAll(document, localeRequest, request.Access) {
			return false
		}
	}
	return true
}

func matchesAll(document store.Document, request store.Request, nodes ...*query.Node) bool {
	cache := make(map[string][]store.Value)
	values := func(path query.Path) []store.Value {
		if values, found := cache[path.String()]; found {
			return values
		}
		values := queryDocumentValues(document, request, path)
		cache[path.String()] = values
		return values
	}
	for _, node := range nodes {
		if node != nil && !matchesPrimitiveQuery(document, request.Collection.Fields, *node, values) {
			return false
		}
	}
	return true
}

// queryDocumentValues resolves schema paths against canonical stored values.
// Runtime objects must not reinterpret a block discriminator as an ordinary
// child name: a public variant path could otherwise reach a private variant.
func queryDocumentValues(document store.Document, request store.Request, path query.Path) []store.Value {
	segments := path.Segments()
	if len(segments) == 1 && (segments[0] == "id" || segments[0] == "_status" || segments[0] == "_revision") {
		return documentValues(document, segments)
	}
	if len(segments) == 0 {
		return nil
	}
	root, present := document.Values[segments[0]]
	if !present {
		return nil
	}
	var values []store.Value
	locales := populationwalk.LocaleSelection{All: request.AllLocales, Chain: request.LocaleChain}
	visit := func(prefix query.Path, remainder []string) {
		populationwalk.VisitAtPath(request.Collection.Fields, store.Values{segments[0]: root}, prefix, locales, func(_ schema.Field, value store.Value) {
			if len(remainder) == 0 {
				values = append(values, value)
			} else {
				values = append(values, valuesBelow(value, remainder)...)
			}
		})
	}
	if _, found := populationwalk.FieldAtPath(request.Collection.Fields, path); found {
		visit(path, nil)
		return values
	}
	// Opaque JSON/plugins support data keys without authored child
	// fields. Structured fields and embedded plugin trees require canonical
	// schema paths instead of accepting their serialized wire shape.
	for length := len(segments) - 1; length > 0; length-- {
		prefix, err := query.NewPath(segments[:length]...)
		if err != nil {
			continue
		}
		if field, found := populationwalk.FieldAtPath(request.Collection.Fields, prefix); found && (field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin && !embedded.HasFields(field)) {
			visit(prefix, segments[length:])
			return values
		}
	}
	return nil
}

func matches(document store.Document, node query.Node) bool {
	return matchesWithValues(document, node, func(path query.Path) []store.Value {
		return documentValues(document, path.Segments())
	})
}

func matchesWithValues(document store.Document, node query.Node, atPath func(query.Path) []store.Value) bool {
	switch node.Kind {
	case query.ExpressionComparison:
		if node.Comparison == nil {
			return false
		}
		if timestamp, system := documentTimestamp(document, node.Comparison.Path); system {
			return matchesTimestamp(timestamp, node.Comparison.Operator, node.Comparison.Value)
		}
		values := atPath(node.Comparison.Path)
		if node.Comparison.Operator == query.OperatorExists {
			want, _ := node.Comparison.Value.BooleanValue()
			present := false
			for _, value := range values {
				present = present || value.Kind() != store.ValueNull
			}
			return present == want
		}
		if len(values) == 0 {
			return compare(store.Value{}, false, node.Comparison.Operator, node.Comparison.Value)
		}
		if node.Comparison.Operator == query.OperatorNotEqual {
			for _, value := range values {
				if compare(value, true, query.OperatorEqual, node.Comparison.Value) {
					return false
				}
			}
			return true
		}
		for _, value := range values {
			if compare(value, true, node.Comparison.Operator, node.Comparison.Value) {
				return true
			}
		}
		return false
	case query.ExpressionAnd:
		for _, child := range node.Children {
			if !matchesWithValues(document, child, atPath) {
				return false
			}
		}
		return true
	case query.ExpressionOr:
		for _, child := range node.Children {
			if matchesWithValues(document, child, atPath) {
				return true
			}
		}
		return false
	case query.ExpressionNot:
		return len(node.Children) == 1 && !matchesWithValues(document, node.Children[0], atPath)
	default:
		return false
	}
}

func documentTimestamp(document store.Document, path query.Path) (time.Time, bool) {
	switch path.String() {
	case "createdAt":
		return document.CreatedAt, true
	case "updatedAt":
		return document.UpdatedAt, true
	default:
		return time.Time{}, false
	}
}

func matchesTimestamp(actual time.Time, operator query.Operator, expected query.Value) bool {
	switch operator {
	case query.OperatorExists:
		want, _ := expected.BooleanValue()
		return want
	case query.OperatorIn:
		for _, item := range expected.Values() {
			if matchesTimestamp(actual, query.OperatorEqual, item) {
				return true
			}
		}
		return false
	}
	if expected.Kind() == query.ValueNull {
		return operator == query.OperatorNotEqual
	}
	text, ok := expected.StringValue()
	if !ok {
		return false
	}
	want, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return false
	}
	comparison := compareTimes(actual, want)
	switch operator {
	case query.OperatorEqual:
		return comparison == 0
	case query.OperatorNotEqual:
		return comparison != 0
	case query.OperatorGreaterThan:
		return comparison > 0
	case query.OperatorGreaterThanEqual:
		return comparison >= 0
	case query.OperatorLessThan:
		return comparison < 0
	case query.OperatorLessThanEqual:
		return comparison <= 0
	default:
		return false
	}
}

func compareTimes(left, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func documentValues(document store.Document, segments []string) []store.Value {
	if len(segments) == 1 && segments[0] == "id" {
		return []store.Value{store.String(document.ID)}
	}
	if len(segments) == 1 && segments[0] == "_status" && document.Status != "" {
		return []store.Value{store.String(string(document.Status))}
	}
	return valuesAtSegments(document.Values, segments)
}

func valuesAtSegments(values store.Values, segments []string) []store.Value {
	if len(segments) == 0 {
		return nil
	}
	value, exists := values[segments[0]]
	if !exists {
		return nil
	}
	if len(segments) == 1 {
		return []store.Value{value}
	}
	return valuesBelow(value, segments[1:])
}

func valuesBelow(value store.Value, segments []string) []store.Value {
	if value.Kind() == store.ValueObject {
		if blockType, exists := value.Lookup("blockType"); exists {
			kind, _ := blockType.StringValue()
			if kind == segments[0] {
				segments = segments[1:]
			}
		}
		if len(segments) == 0 {
			return nil
		}
		child, exists := value.Lookup(segments[0])
		if !exists {
			return nil
		}
		if len(segments) == 1 {
			return []store.Value{child}
		}
		return valuesBelow(child, segments[1:])
	}
	if value.Kind() == store.ValueList {
		var result []store.Value
		for item := range value.Elements() {
			result = append(result, valuesBelow(item, segments)...)
		}
		return result
	}
	return nil
}

func documentValue(document store.Document, segments []string) (store.Value, bool) {
	if len(segments) == 0 {
		return store.Value{}, false
	}
	if len(segments) == 1 && segments[0] == "id" {
		return store.String(document.ID), true
	}
	if len(segments) == 1 && segments[0] == "_status" && document.Status != "" {
		return store.String(string(document.Status)), true
	}
	value, exists := document.Values[segments[0]]
	for _, segment := range segments[1:] {
		if !exists {
			return store.Value{}, false
		}
		value, exists = value.Lookup(segment)
	}
	return value, exists
}

func compare(actual store.Value, exists bool, operator query.Operator, expected query.Value) bool {
	if operator == query.OperatorExists {
		want, _ := expected.BooleanValue()
		present := exists && actual.Kind() != store.ValueNull
		return present == want
	}
	if operator == query.OperatorIn {
		for _, item := range expected.Values() {
			if compare(actual, exists, query.OperatorEqual, item) {
				return true
			}
		}
		return false
	}
	if operator == query.OperatorContains || operator == query.OperatorLike {
		expectedText, expectedOK := expected.StringValue()
		if !exists || !expectedOK {
			return false
		}
		if actual.Kind() == store.ValueList {
			if operator == query.OperatorLike {
				return false
			}
			for value := range actual.Elements() {
				text, valid := value.StringValue()
				if valid && text == expectedText {
					return true
				}
			}
			return false
		}
		actualText, actualOK := actual.StringValue()
		if !actualOK {
			return false
		}
		actualText = strings.ToLower(actualText)
		if operator == query.OperatorContains {
			return strings.Contains(actualText, strings.ToLower(expectedText))
		}
		for _, word := range strings.Fields(strings.ToLower(expectedText)) {
			if !strings.Contains(actualText, word) {
				return false
			}
		}
		return true
	}
	if operator == query.OperatorGreaterThan || operator == query.OperatorGreaterThanEqual || operator == query.OperatorLessThan || operator == query.OperatorLessThanEqual {
		comparison, comparable := compareOrdered(actual, expected)
		if !exists || !comparable {
			return false
		}
		switch operator {
		case query.OperatorGreaterThan:
			return comparison > 0
		case query.OperatorGreaterThanEqual:
			return comparison >= 0
		case query.OperatorLessThan:
			return comparison < 0
		default:
			return comparison <= 0
		}
	}
	equal := false
	switch expected.Kind() {
	case query.ValueNull:
		equal = !exists || actual.Kind() == store.ValueNull
	case query.ValueString:
		expectedText, _ := expected.StringValue()
		actualText, valid := actual.StringValue()
		equal = exists && valid && actualText == expectedText
	case query.ValueNumber:
		expectedNumber, _ := expected.NumberValue()
		actualNumber, valid := actual.NumberValue()
		equal = exists && valid && actualNumber == expectedNumber
	case query.ValueBoolean:
		expectedBoolean, _ := expected.BooleanValue()
		actualBoolean, valid := actual.BooleanValue()
		equal = exists && valid && actualBoolean == expectedBoolean
	}
	if operator == query.OperatorNotEqual {
		return !equal
	}
	return equal
}

func compareOrdered(actual store.Value, expected query.Value) (int, bool) {
	switch expected.Kind() {
	case query.ValueString:
		left, valid := actual.StringValue()
		right, _ := expected.StringValue()
		if !valid {
			return 0, false
		}
		return strings.Compare(left, right), true
	case query.ValueNumber:
		left, valid := actual.NumberValue()
		right, _ := expected.NumberValue()
		if !valid {
			return 0, false
		}
		if left < right {
			return -1, true
		}
		if left > right {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func equalValue(left, right store.Value) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	switch left.Kind() {
	case store.ValueNull:
		return true
	case store.ValueString:
		leftText, _ := left.StringValue()
		rightText, _ := right.StringValue()
		return leftText == rightText
	case store.ValueNumber:
		leftNumber, _ := left.NumberValue()
		rightNumber, _ := right.NumberValue()
		return leftNumber == rightNumber
	case store.ValueBoolean:
		leftBoolean, _ := left.BooleanValue()
		rightBoolean, _ := right.BooleanValue()
		return leftBoolean == rightBoolean
	default:
		return false
	}
}

var _ store.AuthStore = (*Store)(nil)
var _ store.AuthTransaction = (*transaction)(nil)
var _ store.AuthUnlockTransaction = (*transaction)(nil)
var _ store.DistinctTransaction = (*transaction)(nil)

func matchesPrimitiveQuery(document store.Document, fields []schema.Field, node query.Node, values func(query.Path) []store.Value) bool {
	if node.Kind == query.ExpressionComparison && node.Comparison != nil && node.Comparison.Operator == query.OperatorIn {
		if field, ok := populationwalk.FieldAtPath(fields, node.Comparison.Path); ok && primitivefield.IsList(field) {
			for _, value := range values(node.Comparison.Path) {
				if primitivefield.Membership(value, node.Comparison.Value) {
					return true
				}
			}
			return false
		}
	}
	switch node.Kind {
	case query.ExpressionAnd:
		for _, child := range node.Children {
			if !matchesPrimitiveQuery(document, fields, child, values) {
				return false
			}
		}
		return true
	case query.ExpressionOr:
		for _, child := range node.Children {
			if matchesPrimitiveQuery(document, fields, child, values) {
				return true
			}
		}
		return false
	case query.ExpressionNot:
		return len(node.Children) == 1 && !matchesPrimitiveQuery(document, fields, node.Children[0], values)
	default:
		return matchesWithValues(document, node, values)
	}
}
