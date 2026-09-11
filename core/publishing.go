package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// SchedulePublish queues a publish for one exact authenticated identity. A nil
// identity preserves anonymous scheduling when collection access allows it.
func (application *App) SchedulePublish(ctx context.Context, collection, documentID string, runAt time.Time, expectedRevision int, identity *AuthIdentity) (store.ScheduledPublish, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Versions == nil {
		return store.ScheduledPublish{}, &operationengine.Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if application.tasks == nil {
		return store.ScheduledPublish{}, &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	requestedByCollectionID := schema.StableID("")
	actorCollection := schema.CollectionSlug("")
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return store.ScheduledPublish{}, err
	}
	if identity != nil {
		requestedByCollectionID = application.authBySlug[string(actorCollection)].ID
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, ID: documentID, Actor: actor, ActorCollection: actorCollection})
	if err != nil {
		return store.ScheduledPublish{}, err
	}
	if !capabilities.Operations.Publish {
		return store.ScheduledPublish{}, &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	document, err := application.local.Find(ctx, collection, documentID, FindOptions{Actor: actor, ActorCollection: actorCollection})
	if err != nil {
		return store.ScheduledPublish{}, err
	}
	if expectedRevision == 0 {
		expectedRevision = document.Revision
	}
	job := store.ScheduledPublish{CollectionID: resolved.ID, DocumentID: documentID, ExpectedRevision: expectedRevision, RunAt: runAt.UTC()}
	if requestedByCollectionID != "" {
		job.RequestedByCollectionID = requestedByCollectionID
		job.RequestedByUserID = actor.ID
	}
	return application.schedulePublishTask(ctx, job)
}

// ScheduledPublishes lists scheduled publishes using one exact identity.
func (application *App) ScheduledPublishes(ctx context.Context, collection, documentID string, identity *AuthIdentity) ([]store.ScheduledPublish, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	return application.scheduledPublishes(ctx, collection, documentID, actor, actorCollection)
}

func (application *App) scheduledPublishes(ctx context.Context, collection, documentID string, actor *store.Document, actorCollection schema.CollectionSlug) ([]store.ScheduledPublish, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Versions == nil {
		return nil, &operationengine.Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if application.tasks == nil {
		return nil, &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	if _, err := application.local.Find(ctx, collection, documentID, FindOptions{Actor: actor, ActorCollection: actorCollection}); err != nil {
		return nil, err
	}
	target := store.DocumentReference{CollectionID: resolved.ID, DocumentID: documentID}
	tasks, err := application.tasks.ListTasks(ctx, store.TaskList{
		Slug: builtinPublishTask, Target: &target,
		States: []store.TaskState{store.TaskStateQueued, store.TaskStateRunning, store.TaskStateFailed}, Limit: maxTaskListLimit,
	})
	if err != nil {
		return nil, err
	}
	jobs := make([]store.ScheduledPublish, 0, len(tasks))
	for _, task := range tasks {
		job, err := scheduledPublishFromTask(task)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(left, right int) bool {
		if jobs[left].RunAt.Equal(jobs[right].RunAt) {
			return jobs[left].ID < jobs[right].ID
		}
		return jobs[left].RunAt.Before(jobs[right].RunAt)
	})
	return jobs, nil
}

// CancelScheduledPublish removes a scheduled publish using one exact identity.
func (application *App) CancelScheduledPublish(ctx context.Context, collection, documentID, jobID string, identity *AuthIdentity) error {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	return application.cancelScheduledPublish(ctx, collection, documentID, jobID, actor, actorCollection)
}

func (application *App) cancelScheduledPublish(ctx context.Context, collection, documentID, jobID string, actor *store.Document, actorCollection schema.CollectionSlug) error {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Versions == nil {
		return &operationengine.Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if application.tasks == nil {
		return &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	capabilities, err := application.local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{Collection: collection, ID: documentID, Actor: actor, ActorCollection: actorCollection})
	if err != nil {
		return err
	}
	if !capabilities.Operations.Publish {
		return &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	deleteError := application.tasks.DismissTaskForTarget(ctx, jobID, builtinPublishTask, store.DocumentReference{CollectionID: resolved.ID, DocumentID: documentID})
	if deleteError != nil {
		if errors.Is(deleteError, store.ErrNotFound) {
			return &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publish was not found"}
		}
		return deleteError
	}
	return nil
}

func (application *App) RunScheduledPublishes(ctx context.Context, limit int, actor *store.Document) (int, error) {
	if application.tasks == nil {
		return 0, &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	if limit < 1 {
		limit = 20
	}
	summary, err := application.runTasks(ctx, taskRunOptions{limit: limit, slugs: []string{builtinPublishTask}, fallbackActor: actor})
	return summary.Succeeded, err
}

type scheduledPublishTaskInput struct {
	CollectionID            schema.StableID `json:"collectionID"`
	DocumentID              string          `json:"documentID"`
	ExpectedRevision        int             `json:"expectedRevision"`
	RequestedByCollectionID schema.StableID `json:"requestedByCollectionID,omitempty"`
	RequestedByUserID       string          `json:"requestedByUserID,omitempty"`
}

func scheduledPublishTaskRuntime(application *App) taskRuntime {
	definition := NewTask(builtinPublishTask, func(task TaskContext, input scheduledPublishTaskInput) (struct{}, error) {
		collection, exists := application.collectionByID(input.CollectionID)
		if !exists {
			return struct{}{}, AbortTask("scheduled_publish_collection_unavailable", errors.New("collection is unavailable"))
		}
		actor := task.actor
		actorCollection := schema.CollectionSlug("")
		if input.RequestedByCollectionID != "" || input.RequestedByUserID != "" {
			authCollection, exists := application.authByID[input.RequestedByCollectionID]
			if !exists || input.RequestedByUserID == "" {
				return struct{}{}, AbortTask("scheduled_publish_identity_unavailable", errors.New("requesting identity is unavailable"))
			}
			resolvedActor, err := application.authUser(task.Context, authCollection, input.RequestedByUserID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return struct{}{}, AbortTask("scheduled_publish_identity_unavailable", errors.New("requesting identity is unavailable"))
				}
				return struct{}{}, RetryTask("scheduled_publish_identity_load_failed", err)
			}
			actor = &resolvedActor
			actorCollection = authCollection.Slug
		}
		if _, err := application.local.Publish(task.Context, string(collection.Slug), input.DocumentID, MutationOptions{Actor: actor, ActorCollection: actorCollection, ExpectedRevision: input.ExpectedRevision}); err != nil {
			var operationError *operationengine.Error
			if errors.As(err, &operationError) && operationError.Status < 500 {
				return struct{}{}, AbortTask("scheduled_publish_rejected", err)
			}
			return struct{}{}, RetryTask("scheduled_publish_failed", err)
		}
		return struct{}{}, nil
	}, TaskRetries(10, time.Minute, time.Hour, TaskBackoffExponential), TaskTimeout(5*time.Minute), TaskRetention(30*24*time.Hour))
	return definition.taskRuntime()
}

func (application *App) schedulePublishTask(ctx context.Context, job store.ScheduledPublish) (store.ScheduledPublish, error) {
	input := scheduledPublishTaskInput{
		CollectionID: job.CollectionID, DocumentID: job.DocumentID, ExpectedRevision: job.ExpectedRevision,
		RequestedByCollectionID: job.RequestedByCollectionID, RequestedByUserID: job.RequestedByUserID,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return store.ScheduledPublish{}, err
	}
	target := store.DocumentReference{CollectionID: job.CollectionID, DocumentID: job.DocumentID}
	var requestedBy *store.DocumentReference
	if job.RequestedByCollectionID != "" || job.RequestedByUserID != "" {
		requestedBy = &store.DocumentReference{CollectionID: job.RequestedByCollectionID, DocumentID: job.RequestedByUserID}
	}
	queued, err := application.enqueueTask(ctx, application.taskRegistry[builtinPublishTask], encoded, TaskEnqueueOptions{
		RunAt: job.RunAt, Queue: "scheduled-publish", ConcurrencyKey: scheduledPublishConcurrencyKey(job.CollectionID, job.DocumentID),
		Target: &target, RequestedBy: requestedBy,
	})
	if err != nil {
		return store.ScheduledPublish{}, err
	}
	return scheduledPublishFromTask(queued)
}

func scheduledPublishConcurrencyKey(collectionID schema.StableID, documentID string) string {
	key := string(collectionID) + ":" + documentID
	if len(key) <= store.MaxTaskConcurrencyKeyBytes {
		return key
	}
	digest := sha256.Sum256([]byte(key))
	return "sha256-" + hex.EncodeToString(digest[:])
}

func scheduledPublishFromTask(task store.Task) (store.ScheduledPublish, error) {
	var input scheduledPublishTaskInput
	if err := json.Unmarshal(task.Input, &input); err != nil {
		return store.ScheduledPublish{}, fmt.Errorf("decode scheduled publish task %q: %w", task.ID, err)
	}
	return store.ScheduledPublish{
		ID: task.ID, CollectionID: input.CollectionID, DocumentID: input.DocumentID,
		ExpectedRevision: input.ExpectedRevision, RunAt: task.RunAt, Attempts: task.Attempts,
		RequestedByCollectionID: input.RequestedByCollectionID, RequestedByUserID: input.RequestedByUserID,
		LastError: task.LastError, CreatedAt: task.CreatedAt,
	}, nil
}
