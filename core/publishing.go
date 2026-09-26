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
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// PublicationScheduleOptions controls revision fencing and the saved display timezone.
// TimeZone is optional; it never changes the instant supplied in runAt.
type PublicationScheduleOptions struct {
	// ExpectedRevision rejects the task when the document changes before it runs.
	// Zero captures the current revision when the task is created.
	ExpectedRevision int
	// TimeZone optionally retains the intended display zone without changing runAt.
	TimeZone string
}

// SchedulePublish queues a publish for one exact authenticated identity. A nil
// identity preserves anonymous scheduling when collection access allows it.
func (application *App) SchedulePublish(ctx context.Context, collection, documentID string, runAt time.Time, options PublicationScheduleOptions, identity *AuthIdentity) (store.ScheduledPublication, error) {
	return application.schedulePublication(ctx, collection, documentID, runAt, options, identity, store.PublicationActionPublish)
}

// ScheduleUnpublish queues an unpublish for one exact authenticated identity.
func (application *App) ScheduleUnpublish(ctx context.Context, collection, documentID string, runAt time.Time, options PublicationScheduleOptions, identity *AuthIdentity) (store.ScheduledPublication, error) {
	return application.schedulePublication(ctx, collection, documentID, runAt, options, identity, store.PublicationActionUnpublish)
}

func (application *App) schedulePublication(ctx context.Context, collection, documentID string, runAt time.Time, options PublicationScheduleOptions, identity *AuthIdentity, action store.PublicationAction) (store.ScheduledPublication, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Versions == nil {
		return store.ScheduledPublication{}, &operationengine.Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if application.tasks == nil {
		return store.ScheduledPublication{}, &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	requestedByCollectionID := schema.StableID("")
	actorCollection := schema.CollectionSlug("")
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return store.ScheduledPublication{}, err
	}
	if identity != nil {
		requestedByCollectionID = application.authBySlug[string(actorCollection)].ID
	}
	publicationOperation := operation.Publish
	if action == store.PublicationActionUnpublish {
		publicationOperation = operation.Unpublish
	}
	document, err := application.local.engine.PublicationTarget(ctx, operationengine.CapabilitiesRequest{
		Collection: collection, ID: documentID, Actor: actor, ActorCollection: actorCollection,
	}, publicationOperation)
	if err != nil {
		return store.ScheduledPublication{}, err
	}
	if action == store.PublicationActionUnpublish && document.Status != store.StatusPublished {
		return store.ScheduledPublication{}, &operationengine.Error{Code: "validation", Status: 422, Message: "only a published document can be scheduled for unpublishing"}
	}
	if options.TimeZone != "" && !schema.IsValidTimeZoneID(options.TimeZone) {
		return store.ScheduledPublication{}, &operationengine.Error{Code: "validation", Status: 422, Message: "timeZone must be UTC, a ±HH:mm offset, or a region timezone such as Europe/London"}
	}
	if options.ExpectedRevision == 0 {
		options.ExpectedRevision = document.Revision
	}
	job := store.ScheduledPublication{Action: action, CollectionID: resolved.ID, DocumentID: documentID, ExpectedRevision: options.ExpectedRevision, RunAt: runAt.UTC(), TimeZone: options.TimeZone}
	if requestedByCollectionID != "" {
		job.RequestedByCollectionID = requestedByCollectionID
		job.RequestedByUserID = actor.ID
	}
	return application.schedulePublishTask(ctx, job)
}

// ScheduledPublications lists scheduled publication changes using one exact identity.
func (application *App) ScheduledPublications(ctx context.Context, collection, documentID string, identity *AuthIdentity) ([]store.ScheduledPublication, error) {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	return application.scheduledPublications(ctx, collection, documentID, actor, actorCollection)
}

func (application *App) scheduledPublications(ctx context.Context, collection, documentID string, actor *store.Document, actorCollection schema.CollectionSlug) ([]store.ScheduledPublication, error) {
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
		States: []store.TaskState{store.TaskStateQueued, store.TaskStateFailed}, Limit: maxTaskListLimit,
	})
	if err != nil {
		return nil, err
	}
	jobs := make([]store.ScheduledPublication, 0, len(tasks))
	for _, task := range tasks {
		job, err := scheduledPublicationFromTask(task)
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

// CancelScheduledPublication removes a scheduled publication change using one exact identity.
func (application *App) CancelScheduledPublication(ctx context.Context, collection, documentID, jobID string, identity *AuthIdentity) error {
	actor, actorCollection, err := application.resolveOptionalAuthIdentity(ctx, identity)
	if err != nil {
		return err
	}
	return application.cancelScheduledPublication(ctx, collection, documentID, jobID, actor, actorCollection)
}

func (application *App) cancelScheduledPublication(ctx context.Context, collection, documentID, jobID string, actor *store.Document, actorCollection schema.CollectionSlug) error {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Versions == nil {
		return &operationengine.Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if application.tasks == nil {
		return &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	// Keep task identity private from actors who cannot perform either lifecycle
	// action without evaluating unrelated collection access rules. The selected
	// task action is authorized again with the locked target in the dismissal
	// transaction below; this preflight never grants dismissal.
	publicationKinds := []operation.Kind{operation.Publish}
	if resolved.Versions.Drafts {
		publicationKinds = append(publicationKinds, operation.Unpublish)
	}
	publicationAllowed := false
	var publicationAccessError error
	for _, kind := range publicationKinds {
		_, accessError := application.local.engine.PublicationTarget(ctx, operationengine.CapabilitiesRequest{
			Collection: collection, ID: documentID, Actor: actor, ActorCollection: actorCollection,
		}, kind)
		if accessError == nil {
			publicationAllowed = true
			break
		}
		var operationError *operationengine.Error
		if !errors.As(accessError, &operationError) || operationError.Status >= 500 {
			publicationAccessError = accessError
		}
	}
	if !publicationAllowed {
		if publicationAccessError != nil {
			return publicationAccessError
		}
		return &operationengine.Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	task, err := application.tasks.FindTask(ctx, jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publication was not found"}
		}
		return err
	}
	if task.Slug != builtinPublishTask || task.Target == nil || task.Target.CollectionID != resolved.ID || task.Target.DocumentID != documentID {
		return &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publication was not found"}
	}
	job, err := scheduledPublicationFromTask(task)
	if err != nil {
		return err
	}
	if job.CollectionID != resolved.ID || job.DocumentID != documentID {
		return &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publication was not found"}
	}
	publicationOperation := operation.Publish
	if job.Action == store.PublicationActionUnpublish {
		publicationOperation = operation.Unpublish
	}
	deleteError := application.local.engine.MutatePublicationTarget(ctx, operationengine.CapabilitiesRequest{
		Collection: collection, ID: documentID, Actor: actor, ActorCollection: actorCollection,
	}, publicationOperation, func(ctx context.Context, transaction store.Transaction, _ schema.Collection, _ store.Document) error {
		return transaction.DismissTaskForTarget(ctx, jobID, builtinPublishTask, store.DocumentReference{CollectionID: resolved.ID, DocumentID: documentID})
	})
	if deleteError != nil {
		if errors.Is(deleteError, store.ErrNotFound) {
			return &operationengine.Error{Code: "not_found", Status: 404, Message: "scheduled publication was not found"}
		}
		return deleteError
	}
	return nil
}

func (application *App) RunScheduledPublications(ctx context.Context, limit int, actor *store.Document) (int, error) {
	if application.tasks == nil {
		return 0, &operationengine.Error{Code: "store_failed", Status: 500, Message: "store does not support scheduled publishing"}
	}
	if limit < 1 {
		limit = 20
	}
	summary, err := application.runTasks(ctx, taskRunOptions{limit: limit, slugs: []string{builtinPublishTask}, fallbackActor: actor})
	return summary.Succeeded, err
}

type scheduledPublicationTaskInput struct {
	TimeZone                string                  `json:"timeZone,omitempty"`
	Action                  store.PublicationAction `json:"action"`
	CollectionID            schema.StableID         `json:"collectionID"`
	DocumentID              string                  `json:"documentID"`
	ExpectedRevision        int                     `json:"expectedRevision"`
	RequestedByCollectionID schema.StableID         `json:"requestedByCollectionID,omitempty"`
	RequestedByUserID       string                  `json:"requestedByUserID,omitempty"`
}

func scheduledPublicationTaskRuntime(application *App) taskRuntime {
	definition := NewTask(builtinPublishTask, func(task TaskContext, input scheduledPublicationTaskInput) (struct{}, error) {
		collection, exists := application.collectionByID(input.CollectionID)
		if !exists {
			return struct{}{}, AbortTask("scheduled_publication_collection_unavailable", errors.New("collection is unavailable"))
		}
		actor := task.actor
		actorCollection := schema.CollectionSlug("")
		if input.RequestedByCollectionID != "" || input.RequestedByUserID != "" {
			authCollection, exists := application.authByID[input.RequestedByCollectionID]
			if !exists || input.RequestedByUserID == "" {
				return struct{}{}, AbortTask("scheduled_publication_identity_unavailable", errors.New("requesting identity is unavailable"))
			}
			resolvedActor, err := application.authUser(task.Context, authCollection, input.RequestedByUserID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return struct{}{}, AbortTask("scheduled_publication_identity_unavailable", errors.New("requesting identity is unavailable"))
				}
				return struct{}{}, RetryTask("scheduled_publication_identity_load_failed", err)
			}
			actor = &resolvedActor
			actorCollection = authCollection.Slug
		}
		options := MutationOptions{Actor: actor, ActorCollection: actorCollection, ExpectedRevision: input.ExpectedRevision}
		var err error
		switch input.Action {
		case store.PublicationActionPublish:
			_, err = application.local.Publish(task.Context, string(collection.Slug), input.DocumentID, options)
		case store.PublicationActionUnpublish:
			_, err = application.local.Unpublish(task.Context, string(collection.Slug), input.DocumentID, options)
		default:
			return struct{}{}, AbortTask("scheduled_publication_invalid_action", errors.New("publication action is invalid"))
		}
		if err != nil {
			var operationError *operationengine.Error
			if errors.As(err, &operationError) && operationError.Status < 500 {
				return struct{}{}, AbortTask("scheduled_publication_rejected", err)
			}
			return struct{}{}, RetryTask("scheduled_publication_failed", err)
		}
		return struct{}{}, nil
	}, TaskRetries(10, time.Minute, time.Hour, TaskBackoffExponential), TaskTimeout(5*time.Minute), TaskRetention(30*24*time.Hour))
	return definition.taskRuntime()
}

func (application *App) schedulePublishTask(ctx context.Context, job store.ScheduledPublication) (store.ScheduledPublication, error) {
	input := scheduledPublicationTaskInput{
		Action: job.Action, TimeZone: job.TimeZone,
		CollectionID: job.CollectionID, DocumentID: job.DocumentID, ExpectedRevision: job.ExpectedRevision,
		RequestedByCollectionID: job.RequestedByCollectionID, RequestedByUserID: job.RequestedByUserID,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return store.ScheduledPublication{}, err
	}
	target := store.DocumentReference{CollectionID: job.CollectionID, DocumentID: job.DocumentID}
	var requestedBy *store.DocumentReference
	if job.RequestedByCollectionID != "" || job.RequestedByUserID != "" {
		requestedBy = &store.DocumentReference{CollectionID: job.RequestedByCollectionID, DocumentID: job.RequestedByUserID}
	}
	queued, err := application.enqueueTask(ctx, application.taskRegistry[builtinPublishTask], encoded, TaskEnqueueOptions{
		RunAt: job.RunAt, Queue: "scheduled-publish", ConcurrencyKey: scheduledPublicationConcurrencyKey(job.CollectionID, job.DocumentID),
		Target: &target, RequestedBy: requestedBy,
	})
	if err != nil {
		return store.ScheduledPublication{}, err
	}
	return scheduledPublicationFromTask(queued)
}

func scheduledPublicationConcurrencyKey(collectionID schema.StableID, documentID string) string {
	key := string(collectionID) + ":" + documentID
	if len(key) <= store.MaxTaskConcurrencyKeyBytes {
		return key
	}
	digest := sha256.Sum256([]byte(key))
	return "sha256-" + hex.EncodeToString(digest[:])
}

func scheduledPublicationFromTask(task store.Task) (store.ScheduledPublication, error) {
	var input scheduledPublicationTaskInput
	if err := json.Unmarshal(task.Input, &input); err != nil {
		return store.ScheduledPublication{}, fmt.Errorf("decode scheduled publish task %q: %w", task.ID, err)
	}
	if input.Action != store.PublicationActionPublish && input.Action != store.PublicationActionUnpublish {
		return store.ScheduledPublication{}, fmt.Errorf("decode scheduled publish task %q: invalid publication action %q", task.ID, input.Action)
	}
	if input.TimeZone != "" && !schema.IsValidTimeZoneID(input.TimeZone) {
		return store.ScheduledPublication{}, fmt.Errorf("decode scheduled publish task %q: invalid timezone %q", task.ID, input.TimeZone)
	}
	return store.ScheduledPublication{
		ID: task.ID, Action: input.Action, CollectionID: input.CollectionID, DocumentID: input.DocumentID,
		ExpectedRevision: input.ExpectedRevision, RunAt: task.RunAt, Attempts: task.Attempts, TimeZone: input.TimeZone,
		RequestedByCollectionID: input.RequestedByCollectionID, RequestedByUserID: input.RequestedByUserID,
		LastError: task.LastError, CreatedAt: task.CreatedAt,
	}, nil
}
