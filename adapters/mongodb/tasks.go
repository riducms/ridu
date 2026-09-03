package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (backend *Store) EnqueueTask(ctx context.Context, task store.Task) (store.Task, error) {
	if err := backend.prepareSystemOperation(ctx, "task"); err != nil {
		return store.Task{}, err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return store.Task{}, err
	}
	var err error
	task, err = normalizeMongoTaskAdmission(task)
	if err != nil {
		return store.Task{}, err
	}
	task.ID, err = newMongoTaskIdentifier(mongoTaskIDPrefix)
	if err != nil {
		return store.Task{}, err
	}
	references, err := backend.captureMongoTaskReferences(ctx, task)
	if err != nil {
		return store.Task{}, err
	}

	var stored store.Task
	if err := backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		var operationErr error
		stored, operationErr = transaction.enqueueTask(ctx, task, references)
		return operationErr
	}); err != nil {
		return store.Task{}, err
	}
	return cloneMongoTask(stored), nil
}

func (backend *Store) FindTask(ctx context.Context, id string) (store.Task, error) {
	if err := backend.prepareSystemOperation(ctx, "task"); err != nil {
		return store.Task{}, err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return store.Task{}, err
	}
	raw, err := backend.taskCollection().FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Raw()
	if err != nil {
		return store.Task{}, translateMongoError(ctx, err)
	}
	task, err := decodeMongoTask(raw)
	if err != nil {
		return store.Task{}, err
	}
	return cloneMongoTask(task), nil
}

func (backend *Store) ListTasks(ctx context.Context, request store.TaskList) ([]store.Task, error) {
	if err := backend.prepareSystemOperation(ctx, "task"); err != nil {
		return nil, err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return nil, err
	}
	if err := store.ValidateTaskList(request); err != nil {
		return nil, err
	}
	predicates := make(bson.D, 0, 3)
	if request.Slug != "" {
		predicates = append(predicates, bson.E{Key: "slug", Value: request.Slug})
	}
	if request.Target != nil {
		predicates = append(predicates,
			bson.E{Key: "target.collection", Value: string(request.Target.CollectionID)},
			bson.E{Key: "target.document", Value: request.Target.DocumentID},
		)
	}
	if len(request.States) != 0 {
		states := make(bson.A, len(request.States))
		for index, state := range request.States {
			states[index] = string(state)
		}
		predicates = append(predicates, bson.E{Key: "state", Value: bson.D{{Key: "$in", Value: states}}})
	}
	cursor, err := backend.taskCollection().Find(
		ctx,
		predicates,
		options.Find().SetSort(mongoTaskOrder()).SetLimit(int64(request.Limit)),
	)
	if err != nil {
		return nil, translateMongoError(ctx, err)
	}
	defer closeMongoSystemCursor(cursor)
	result := make([]store.Task, 0, request.Limit)
	for cursor.Next(ctx) {
		task, decodeErr := decodeMongoTask(cursor.Current)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, cloneMongoTask(task))
	}
	if err := cursor.Err(); err != nil {
		return nil, translateMongoError(ctx, err)
	}
	return result, nil
}

func (transaction *documentTransaction) enqueueTask(
	ctx context.Context,
	task store.Task,
	references []mongoActiveDocumentReference,
) (store.Task, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Task{}, err
	}
	defer leave()
	if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
		return store.Task{}, err
	}

	collection := transaction.taskCollection()
	if raw, findErr := collection.FindOne(sessionContext, bson.D{{Key: "_id", Value: task.ID}}).Raw(); findErr == nil {
		if _, decodeErr := decodeMongoTask(raw); decodeErr != nil {
			return store.Task{}, decodeErr
		}
		return store.Task{}, fmt.Errorf("MongoDB generated task ID collided with stored state: %w", store.ErrConflict)
	} else if !errors.Is(findErr, mongo.ErrNoDocuments) {
		return store.Task{}, translateMongoError(ctx, findErr)
	}

	assignments, err := mongoTaskAdmissionAssignments(task)
	if err != nil {
		return store.Task{}, err
	}
	raw, err := collection.FindOneAndUpdate(
		sessionContext,
		bson.D{{Key: "_id", Value: task.ID}},
		mongo.Pipeline{bson.D{{Key: "$set", Value: assignments}}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Raw()
	if err != nil {
		return store.Task{}, translateMongoError(ctx, err)
	}
	stored, err := decodeMongoTask(raw)
	if err != nil {
		return store.Task{}, err
	}
	if !sameMongoTaskAdmission(stored, task) {
		return store.Task{}, mongoSystemIdentityCollision("task")
	}
	return stored, nil
}

func mongoTaskAdmissionAssignments(task store.Task) (bson.D, error) {
	runAt, err := encodeTime(task.RunAt)
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB task runAt: %w", err)
	}
	return bson.D{
		{Key: "codec", Value: mongoTaskCodecVersion},
		{Key: "slug", Value: mongoTaskLiteral(task.Slug)},
		{Key: "queue", Value: mongoTaskLiteral(task.Queue)},
		{Key: "concurrencyKey", Value: mongoTaskLiteral(task.ConcurrencyKey)},
		{Key: "concurrencyID", Value: mongoTaskLiteral(mongoTaskOptionalString(mongoTaskConcurrencyID(task.Queue, task.ConcurrencyKey)))},
		{Key: "input", Value: mongoTaskLiteral(bson.Binary{Subtype: 0, Data: append([]byte(nil), task.Input...)})},
		{Key: "output", Value: nil},
		{Key: "state", Value: string(store.TaskStateQueued)},
		{Key: "runAt", Value: runAt},
		{Key: "attempts", Value: int64(0)},
		{Key: "maxAttempts", Value: int64(task.MaxAttempts)},
		{Key: "retryDelay", Value: int64(task.RetryDelay)},
		{Key: "maxRetryDelay", Value: int64(task.MaxRetryDelay)},
		{Key: "backoff", Value: string(task.Backoff)},
		{Key: "timeout", Value: int64(task.Timeout)},
		{Key: "retention", Value: int64(task.Retention)},
		{Key: "leaseToken", Value: ""},
		{Key: "leaseExpiresAt", Value: nil},
		{Key: "target", Value: mongoTaskLiteral(encodeMongoTaskReference(task.Target))},
		{Key: "requestedBy", Value: mongoTaskLiteral(encodeMongoTaskReference(task.RequestedBy))},
		{Key: "lastErrorCode", Value: ""},
		{Key: "lastError", Value: ""},
		{Key: "createdAt", Value: mongoTaskServerNowNanosExpression()},
		{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
		{Key: "completedAt", Value: nil},
		{Key: "retainUntil", Value: nil},
	}, nil
}

func sameMongoTaskAdmission(stored, requested store.Task) bool {
	return stored.ID == requested.ID && stored.Slug == requested.Slug && stored.Queue == requested.Queue &&
		stored.ConcurrencyKey == requested.ConcurrencyKey && string(stored.Input) == string(requested.Input) &&
		stored.RunAt.Equal(requested.RunAt) && stored.MaxAttempts == requested.MaxAttempts &&
		stored.RetryDelay == requested.RetryDelay && stored.MaxRetryDelay == requested.MaxRetryDelay &&
		stored.Backoff == requested.Backoff && stored.Timeout == requested.Timeout &&
		stored.Retention == requested.Retention && sameMongoTaskReference(stored.Target, requested.Target) &&
		sameMongoTaskReference(stored.RequestedBy, requested.RequestedBy)
}

func (backend *Store) captureMongoTaskReferences(ctx context.Context, task store.Task) ([]mongoActiveDocumentReference, error) {
	references := make([]store.DocumentReference, 0, 2)
	if task.Target != nil {
		references = append(references, *task.Target)
	}
	if task.RequestedBy != nil {
		references = append(references, *task.RequestedBy)
	}
	return backend.captureActiveDocumentReferences(ctx, references...)
}

func sameMongoTaskReference(left, right *store.DocumentReference) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.CollectionID == right.CollectionID && left.DocumentID == right.DocumentID
}

func mongoTaskOrder() bson.D {
	return bson.D{
		{Key: "runAt", Value: int32(1)},
		{Key: "createdAt", Value: int32(1)},
		{Key: "_id", Value: int32(1)},
	}
}

func closeMongoSystemCursor(cursor *mongo.Cursor) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	_ = cursor.Close(ctx)
}

func (backend *Store) taskCollection() *mongo.Collection {
	return backend.database.Collection(mongoTaskCollectionName)
}

func (backend *Store) taskConcurrencyCollection() *mongo.Collection {
	return backend.database.Collection(mongoTaskConcurrencyCollectionName)
}

func (transaction *documentTransaction) taskCollection() *mongo.Collection {
	return transaction.store.taskCollection()
}

func (transaction *documentTransaction) taskConcurrencyCollection() *mongo.Collection {
	return transaction.store.taskConcurrencyCollection()
}

func findMongoTaskConcurrencyGuardByIdentity(
	ctx context.Context,
	collection *mongo.Collection,
	queue, key string,
) (mongoTaskConcurrencyGuard, bool, error) {
	logical := bson.D{{Key: "queue", Value: queue}, {Key: "key", Value: key}}
	raw, err := collection.FindOne(ctx, logical).Raw()
	if err == nil {
		guard, decodeErr := decodeMongoTaskConcurrencyGuard(raw)
		if decodeErr != nil {
			return mongoTaskConcurrencyGuard{}, false, decodeErr
		}
		if guard.Queue != queue || guard.Key != key {
			return mongoTaskConcurrencyGuard{}, false, mongoSystemIdentityCollision("task concurrency guard")
		}
		return guard, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return mongoTaskConcurrencyGuard{}, false, translateMongoError(ctx, err)
	}

	raw, err = collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoTaskConcurrencyID(queue, key)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoTaskConcurrencyGuard{}, false, nil
	}
	if err != nil {
		return mongoTaskConcurrencyGuard{}, false, translateMongoError(ctx, err)
	}
	guard, err := decodeMongoTaskConcurrencyGuard(raw)
	if err != nil {
		return mongoTaskConcurrencyGuard{}, false, err
	}
	if guard.Queue != queue || guard.Key != key {
		return mongoTaskConcurrencyGuard{}, false, mongoSystemIdentityCollision("task concurrency guard")
	}
	return guard, true, nil
}

func mongoTaskConcurrencyIdentityFilter(queue, key string) bson.D {
	return bson.D{
		{Key: "_id", Value: mongoTaskConcurrencyID(queue, key)},
		{Key: "queue", Value: queue},
		{Key: "key", Value: key},
	}
}

func mongoTaskIdentityFilter(task store.Task) bson.D {
	return bson.D{
		{Key: "_id", Value: task.ID},
		{Key: "slug", Value: task.Slug},
		{Key: "queue", Value: task.Queue},
		{Key: "concurrencyKey", Value: task.ConcurrencyKey},
		{Key: "concurrencyID", Value: mongoTaskOptionalString(mongoTaskConcurrencyID(task.Queue, task.ConcurrencyKey))},
		{Key: "createdAt", Value: mustEncodeMongoTaskTime(task.CreatedAt)},
	}
}

func mustEncodeMongoTaskTime(value time.Time) int64 {
	encoded, _ := encodeTime(value)
	return encoded
}

func mongoTaskGuardMatchesTask(guard mongoTaskConcurrencyGuard, task store.Task, token string) bool {
	return guard.Queue == task.Queue && guard.Key == task.ConcurrencyKey && guard.TaskID == task.ID &&
		guard.Token == token && sameMongoTaskReference(guard.Target, task.Target) &&
		sameMongoTaskReference(guard.RequestedBy, task.RequestedBy)
}

func mongoTaskGuardAssignments(task store.Task, token string, leaseDuration time.Duration, includeCodec bool) bson.D {
	assignments := bson.D{
		{Key: "queue", Value: mongoTaskLiteral(task.Queue)},
		{Key: "key", Value: mongoTaskLiteral(task.ConcurrencyKey)},
		{Key: "task", Value: mongoTaskLiteral(task.ID)},
		{Key: "token", Value: mongoTaskLiteral(token)},
		{Key: "expiresAt", Value: mongoTaskServerDeadlineExpression(leaseDuration)},
		{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
		{Key: "target", Value: mongoTaskLiteral(encodeMongoTaskReference(task.Target))},
		{Key: "requestedBy", Value: mongoTaskLiteral(encodeMongoTaskReference(task.RequestedBy))},
	}
	if includeCodec {
		assignments = append(bson.D{{Key: "codec", Value: mongoTaskCodecVersion}}, assignments...)
	}
	return assignments
}

func (transaction *documentTransaction) claimTaskConcurrencyGuard(
	callerContext, sessionContext context.Context,
	task store.Task,
	token string,
	leaseDuration time.Duration,
) (mongoTaskConcurrencyGuard, bool, error) {
	collection := transaction.taskConcurrencyCollection()
	current, found, err := findMongoTaskConcurrencyGuardByIdentity(sessionContext, collection, task.Queue, task.ConcurrencyKey)
	if err != nil {
		return mongoTaskConcurrencyGuard{}, false, err
	}
	filter := mongoTaskConcurrencyIdentityFilter(task.Queue, task.ConcurrencyKey)
	if found {
		filter = append(filter, bson.E{Key: "$expr", Value: bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$expiresAt"}}, "long"}}},
			bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}},
		}}}})
	}
	updateOptions := options.FindOneAndUpdate().SetReturnDocument(options.After)
	if !found {
		updateOptions.SetUpsert(true)
	}
	raw, err := collection.FindOneAndUpdate(
		sessionContext,
		filter,
		mongo.Pipeline{bson.D{{Key: "$set", Value: mongoTaskGuardAssignments(task, token, leaseDuration, !found)}}},
		updateOptions,
	).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) && found {
		return current, false, nil
	}
	if err != nil {
		return mongoTaskConcurrencyGuard{}, false, translateMongoError(callerContext, err)
	}
	guard, err := decodeMongoTaskConcurrencyGuard(raw)
	if err != nil {
		return mongoTaskConcurrencyGuard{}, false, err
	}
	if !mongoTaskGuardMatchesTask(guard, task, token) {
		return mongoTaskConcurrencyGuard{}, false, mongoSystemIdentityCollision("task concurrency guard ownership")
	}
	return guard, true, nil
}

func (transaction *documentTransaction) refreshTaskConcurrencyGuard(
	callerContext, sessionContext context.Context,
	task store.Task,
	token string,
	leaseDuration time.Duration,
) (mongoTaskConcurrencyGuard, error) {
	guard, found, err := findMongoTaskConcurrencyGuardByIdentity(
		sessionContext, transaction.taskConcurrencyCollection(), task.Queue, task.ConcurrencyKey,
	)
	if err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	if !found || !mongoTaskGuardMatchesTask(guard, task, token) {
		return mongoTaskConcurrencyGuard{}, store.ErrTaskLeaseLost
	}
	filter := mongoTaskConcurrencyIdentityFilter(task.Queue, task.ConcurrencyKey)
	filter = append(filter,
		bson.E{Key: "task", Value: task.ID},
		bson.E{Key: "token", Value: token},
		bson.E{Key: "$expr", Value: bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$expiresAt"}}, "long"}}},
			bson.D{{Key: "$gt", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}},
		}}}},
	)
	raw, err := transaction.taskConcurrencyCollection().FindOneAndUpdate(
		sessionContext,
		filter,
		mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
			{Key: "expiresAt", Value: mongoTaskServerDeadlineExpression(leaseDuration)},
			{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
		}}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoTaskConcurrencyGuard{}, store.ErrTaskLeaseLost
	}
	if err != nil {
		return mongoTaskConcurrencyGuard{}, translateMongoError(callerContext, err)
	}
	updated, err := decodeMongoTaskConcurrencyGuard(raw)
	if err != nil {
		return mongoTaskConcurrencyGuard{}, err
	}
	if !mongoTaskGuardMatchesTask(updated, task, token) {
		return mongoTaskConcurrencyGuard{}, mongoSystemIdentityCollision("task concurrency guard ownership")
	}
	return updated, nil
}

func (transaction *documentTransaction) deleteTaskConcurrencyGuard(
	callerContext, sessionContext context.Context,
	task store.Task,
	token string,
	leaseOwned bool,
) error {
	if task.ConcurrencyKey == "" {
		return nil
	}
	guard, found, err := findMongoTaskConcurrencyGuardByIdentity(
		sessionContext, transaction.taskConcurrencyCollection(), task.Queue, task.ConcurrencyKey,
	)
	if err != nil {
		return err
	}
	if !found {
		if leaseOwned {
			return store.ErrTaskLeaseLost
		}
		return fmt.Errorf("MongoDB running task has no concurrency guard: %w", store.ErrConflict)
	}
	if !mongoTaskGuardMatchesTask(guard, task, token) {
		if leaseOwned {
			return store.ErrTaskLeaseLost
		}
		return fmt.Errorf("MongoDB running task concurrency ownership changed: %w", store.ErrConflict)
	}
	filter := mongoTaskConcurrencyIdentityFilter(task.Queue, task.ConcurrencyKey)
	filter = append(filter, bson.E{Key: "task", Value: task.ID}, bson.E{Key: "token", Value: token})
	result, err := transaction.taskConcurrencyCollection().DeleteOne(sessionContext, filter)
	if err != nil {
		return translateMongoError(callerContext, err)
	}
	if result.DeletedCount != 1 {
		if leaseOwned {
			return store.ErrTaskLeaseLost
		}
		return fmt.Errorf("MongoDB running task concurrency guard changed: %w", store.ErrConflict)
	}
	return nil
}

func (backend *Store) ClaimTasks(ctx context.Context, request store.TaskClaim) ([]store.Task, error) {
	if err := backend.prepareSystemOperation(ctx, "task claim"); err != nil {
		return nil, err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return nil, err
	}
	if err := store.ValidateTaskClaim(request); err != nil {
		return nil, err
	}
	tokens := make(map[string]string)
	var claimed []store.Task
	if err := backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		var operationErr error
		claimed, operationErr = transaction.claimTasks(ctx, request, tokens)
		return operationErr
	}); err != nil {
		return nil, err
	}
	result := make([]store.Task, len(claimed))
	for index := range claimed {
		result[index] = cloneMongoTask(claimed[index])
	}
	return result, nil
}

func (transaction *documentTransaction) claimTasks(
	ctx context.Context,
	request store.TaskClaim,
	tokens map[string]string,
) ([]store.Task, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return nil, err
	}
	defer leave()
	candidates, err := transaction.mongoTaskClaimCandidates(ctx, sessionContext, request)
	if err != nil {
		return nil, err
	}
	claimed := make([]store.Task, 0, len(candidates))
	for _, candidate := range candidates {
		token := tokens[candidate.ID]
		if token == "" {
			token, err = newMongoTaskIdentifier(mongoTaskLeasePrefix)
			if err != nil {
				return nil, err
			}
			tokens[candidate.ID] = token
		}

		var guard *mongoTaskConcurrencyGuard
		if candidate.ConcurrencyKey != "" {
			acquiredGuard, acquired, guardErr := transaction.claimTaskConcurrencyGuard(
				ctx, sessionContext, candidate, token, request.LeaseDuration,
			)
			if guardErr != nil {
				return nil, guardErr
			}
			if !acquired {
				continue
			}
			guard = &acquiredGuard
		}

		filter := mongoTaskIdentityFilter(candidate)
		filter = append(filter, bson.E{Key: "$expr", Value: mongoTaskDueExpression()})
		leaseExpiresAt := any(mongoTaskServerDeadlineExpression(request.LeaseDuration))
		updatedAt := any(mongoTaskServerNowNanosExpression())
		if guard != nil {
			leaseExpiresAt = mustEncodeMongoTaskTime(guard.ExpiresAt)
			updatedAt = mustEncodeMongoTaskTime(guard.UpdatedAt)
		}
		raw, updateErr := transaction.taskCollection().FindOneAndUpdate(
			sessionContext,
			filter,
			mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
				{Key: "state", Value: string(store.TaskStateRunning)},
				{Key: "attempts", Value: bson.D{{Key: "$add", Value: bson.A{"$attempts", int64(1)}}}},
				{Key: "leaseToken", Value: mongoTaskLiteral(token)},
				{Key: "leaseExpiresAt", Value: leaseExpiresAt},
				{Key: "updatedAt", Value: updatedAt},
			}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if errors.Is(updateErr, mongo.ErrNoDocuments) {
			return nil, mongoConfirmedTransactionConflict{}
		}
		if updateErr != nil {
			return nil, translateMongoError(ctx, updateErr)
		}
		stored, decodeErr := decodeMongoTask(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if stored.ID != candidate.ID || stored.LeaseToken != token || stored.State != store.TaskStateRunning ||
			stored.Attempts != candidate.Attempts+1 {
			return nil, mongoSystemIdentityCollision("task claim")
		}
		if guard != nil && (!stored.LeaseExpiresAt.Equal(guard.ExpiresAt) || !stored.UpdatedAt.Equal(guard.UpdatedAt)) {
			return nil, fmt.Errorf("stored MongoDB task claim has inconsistent guard timestamps")
		}
		claimed = append(claimed, stored)
	}
	return claimed, nil
}

func (transaction *documentTransaction) mongoTaskClaimCandidates(
	callerContext, sessionContext context.Context,
	request store.TaskClaim,
) ([]store.Task, error) {
	if err := transaction.validateMongoTaskBlockingGuards(callerContext, sessionContext, request); err != nil {
		return nil, err
	}
	pipeline := mongoTaskClaimPipeline(request)
	cursor, err := transaction.taskCollection().Aggregate(sessionContext, pipeline)
	if err != nil {
		return nil, translateMongoError(callerContext, err)
	}
	defer transaction.closeCursor(cursor)
	result := make([]store.Task, 0, request.Limit)
	for cursor.Next(sessionContext) {
		task, decodeErr := decodeMongoTask(cursor.Current)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, task)
	}
	if err := cursor.Err(); err != nil {
		return nil, translateMongoError(callerContext, err)
	}
	return result, nil
}

func (transaction *documentTransaction) validateMongoTaskBlockingGuards(
	callerContext, sessionContext context.Context,
	request store.TaskClaim,
) error {
	cursor, err := transaction.taskCollection().Aggregate(sessionContext, mongoTaskBlockingGuardPipeline(request))
	if err != nil {
		return translateMongoError(callerContext, err)
	}
	defer transaction.closeCursor(cursor)
	for cursor.Next(sessionContext) {
		guard, decodeErr := decodeMongoTaskConcurrencyGuard(cursor.Current)
		if decodeErr != nil {
			return mongoTaskBlockingGuardDrift(decodeErr)
		}
		raw, probeErr := transaction.taskConcurrencyCollection().FindOne(
			sessionContext,
			bson.D{{Key: "queue", Value: guard.Queue}, {Key: "key", Value: guard.Key}},
		).Raw()
		if errors.Is(probeErr, mongo.ErrNoDocuments) {
			return mongoTaskBlockingGuardDrift(fmt.Errorf("logical identity disappeared"))
		}
		if probeErr != nil {
			return translateMongoError(callerContext, probeErr)
		}
		probed, decodeErr := decodeMongoTaskConcurrencyGuard(raw)
		if decodeErr != nil {
			return mongoTaskBlockingGuardDrift(decodeErr)
		}
		if !sameMongoTaskConcurrencyGuard(probed, guard) {
			return mongoTaskBlockingGuardDrift(fmt.Errorf("logical identity changed"))
		}
	}
	if err := cursor.Err(); err != nil {
		return translateMongoError(callerContext, err)
	}
	return nil
}

func mongoTaskBlockingGuardDrift(cause error) error {
	return fmt.Errorf("MongoDB task concurrency guard blocks a due task but is corrupt: %v: %w", cause, store.ErrConflict)
}

func sameMongoTaskConcurrencyGuard(left, right mongoTaskConcurrencyGuard) bool {
	return left.ID == right.ID && left.Queue == right.Queue && left.Key == right.Key &&
		left.TaskID == right.TaskID && left.Token == right.Token &&
		left.ExpiresAt.Equal(right.ExpiresAt) && left.UpdatedAt.Equal(right.UpdatedAt) &&
		sameMongoTaskReference(left.Target, right.Target) && sameMongoTaskReference(left.RequestedBy, right.RequestedBy)
}

func mongoTaskClaimMatch(request store.TaskClaim) bson.D {
	match := bson.D{{Key: "$expr", Value: mongoTaskDueExpression()}}
	if len(request.Queues) != 0 {
		queues := make(bson.A, len(request.Queues))
		for index, queue := range request.Queues {
			queues[index] = queue
		}
		match = append(match, bson.E{Key: "queue", Value: bson.D{{Key: "$in", Value: queues}}})
	}
	if len(request.Slugs) != 0 {
		slugs := make(bson.A, len(request.Slugs))
		for index, slug := range request.Slugs {
			slugs[index] = slug
		}
		match = append(match, bson.E{Key: "slug", Value: bson.D{{Key: "$in", Value: slugs}}})
	}
	return match
}

func mongoTaskActiveGuardLookup() bson.D {
	activeGuardExpression := bson.D{{Key: "$and", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{"$_id", "$$riduConcurrencyID"}}},
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$expiresAt"}}, "long"}}},
		bson.D{{Key: "$gt", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}},
	}}}
	return bson.D{
		{Key: "from", Value: mongoTaskConcurrencyCollectionName},
		{Key: "let", Value: bson.D{{Key: "riduConcurrencyID", Value: "$concurrencyID"}}},
		{Key: "pipeline", Value: mongo.Pipeline{
			bson.D{{Key: "$match", Value: bson.D{{Key: "$expr", Value: activeGuardExpression}}}},
			bson.D{{Key: "$limit", Value: int64(1)}},
		}},
		{Key: "as", Value: "_riduActiveConcurrency"},
	}
}

func mongoTaskBlockingGuardPipeline(request store.TaskClaim) mongo.Pipeline {
	return mongo.Pipeline{
		bson.D{{Key: "$match", Value: mongoTaskClaimMatch(request)}},
		bson.D{{Key: "$lookup", Value: mongoTaskActiveGuardLookup()}},
		bson.D{{Key: "$unwind", Value: "$_riduActiveConcurrency"}},
		bson.D{{Key: "$replaceWith", Value: "$_riduActiveConcurrency"}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$_id"},
			{Key: "guard", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
		}}},
		bson.D{{Key: "$replaceWith", Value: "$guard"}},
	}
}

func mongoTaskClaimPipeline(request store.TaskClaim) mongo.Pipeline {
	return mongo.Pipeline{
		bson.D{{Key: "$match", Value: mongoTaskClaimMatch(request)}},
		bson.D{{Key: "$sort", Value: mongoTaskOrder()}},
		bson.D{{Key: "$lookup", Value: mongoTaskActiveGuardLookup()}},
		bson.D{{Key: "$match", Value: bson.D{{Key: "_riduActiveConcurrency.0", Value: bson.D{{Key: "$exists", Value: false}}}}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$concurrencyID", "$_id"}}}},
			{Key: "task", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
		}}},
		bson.D{{Key: "$replaceWith", Value: "$task"}},
		bson.D{{Key: "$project", Value: bson.D{{Key: "_riduActiveConcurrency", Value: int32(0)}}}},
		bson.D{{Key: "$sort", Value: mongoTaskOrder()}},
		bson.D{{Key: "$limit", Value: int64(request.Limit)}},
	}
}

func mongoTaskDueExpression() bson.D {
	now := mongoTaskServerNowNanosExpression()
	return bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{"$state", string(store.TaskStateQueued)}}},
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$runAt"}}, "long"}}},
			bson.D{{Key: "$lte", Value: bson.A{"$runAt", now}}},
		}}},
		bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{"$state", string(store.TaskStateRunning)}}},
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$leaseExpiresAt"}}, "long"}}},
			bson.D{{Key: "$lte", Value: bson.A{"$leaseExpiresAt", now}}},
		}}},
	}}}
}

func findMongoTaskRecord(ctx context.Context, collection *mongo.Collection, id string) (store.Task, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.Task{}, false, nil
	}
	if err != nil {
		return store.Task{}, false, translateMongoError(ctx, err)
	}
	task, err := decodeMongoTask(raw)
	if err != nil {
		return store.Task{}, false, err
	}
	return task, true, nil
}

func (backend *Store) CancelTask(ctx context.Context, id string) error {
	if err := backend.prepareSystemOperation(ctx, "task cancellation"); err != nil {
		return err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		return transaction.cancelTask(ctx, id)
	})
}

func (transaction *documentTransaction) cancelTask(ctx context.Context, id string) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	task, found, err := findMongoTaskRecord(sessionContext, transaction.taskCollection(), id)
	if err != nil {
		return err
	}
	if !found || (task.State != store.TaskStateQueued && task.State != store.TaskStateRunning) {
		return store.ErrNotFound
	}
	filter := mongoTaskIdentityFilter(task)
	filter = append(filter,
		bson.E{Key: "state", Value: string(task.State)},
		bson.E{Key: "leaseToken", Value: task.LeaseToken},
	)
	raw, err := transaction.taskCollection().FindOneAndUpdate(
		sessionContext,
		filter,
		mongo.Pipeline{bson.D{{Key: "$set", Value: mongoTaskTerminalAssignments(store.TaskStateCanceled, nil)}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoConfirmedTransactionConflict{}
	}
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if _, err := decodeMongoTask(raw); err != nil {
		return err
	}
	if task.State == store.TaskStateRunning {
		if err := transaction.deleteTaskConcurrencyGuard(ctx, sessionContext, task, task.LeaseToken, false); err != nil {
			return err
		}
	}
	return nil
}

func (backend *Store) DismissTaskForTarget(ctx context.Context, id, slug string, target store.DocumentReference) error {
	if err := backend.prepareSystemOperation(ctx, "task dismissal"); err != nil {
		return err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	if err := store.ValidateTaskList(store.TaskList{Slug: slug, Target: &target, Limit: 1}); err != nil {
		return err
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		return transaction.dismissTaskForTarget(ctx, id, slug, target)
	})
}

func (transaction *documentTransaction) dismissTaskForTarget(
	ctx context.Context,
	id, slug string,
	target store.DocumentReference,
) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	task, found, err := findMongoTaskRecord(sessionContext, transaction.taskCollection(), id)
	if err != nil {
		return err
	}
	allowed := task.State == store.TaskStateQueued || task.State == store.TaskStateRunning ||
		task.State == store.TaskStateFailed || task.State == store.TaskStateCanceled
	if !found || !allowed || task.Slug != slug || task.Target == nil ||
		task.Target.CollectionID != target.CollectionID || task.Target.DocumentID != target.DocumentID {
		return store.ErrNotFound
	}
	if task.State == store.TaskStateRunning {
		if err := transaction.deleteTaskConcurrencyGuard(ctx, sessionContext, task, task.LeaseToken, false); err != nil {
			return err
		}
	}
	filter := mongoTaskIdentityFilter(task)
	filter = append(filter,
		bson.E{Key: "state", Value: string(task.State)},
		bson.E{Key: "slug", Value: slug},
		bson.E{Key: "target.collection", Value: string(target.CollectionID)},
		bson.E{Key: "target.document", Value: target.DocumentID},
	)
	result, err := transaction.taskCollection().DeleteOne(sessionContext, filter)
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if result.DeletedCount != 1 {
		return mongoConfirmedTransactionConflict{}
	}
	return nil
}

func (backend *Store) HeartbeatTask(ctx context.Context, id, leaseToken string, leaseDuration time.Duration) error {
	if err := backend.prepareSystemOperation(ctx, "task heartbeat"); err != nil {
		return err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	if err := store.ValidateTaskLeaseDuration(leaseDuration); err != nil {
		return err
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		return transaction.heartbeatTask(ctx, id, leaseToken, leaseDuration)
	})
}

func (transaction *documentTransaction) heartbeatTask(
	ctx context.Context,
	id, leaseToken string,
	leaseDuration time.Duration,
) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	task, found, err := findMongoTaskRecord(sessionContext, transaction.taskCollection(), id)
	if err != nil {
		return err
	}
	if !found || task.State != store.TaskStateRunning || task.LeaseToken != leaseToken {
		return store.ErrTaskLeaseLost
	}
	assignments := bson.D{
		{Key: "leaseExpiresAt", Value: mongoTaskServerDeadlineExpression(leaseDuration)},
		{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
	}
	var guard *mongoTaskConcurrencyGuard
	if task.ConcurrencyKey != "" {
		updatedGuard, guardErr := transaction.refreshTaskConcurrencyGuard(
			ctx, sessionContext, task, leaseToken, leaseDuration,
		)
		if guardErr != nil {
			return guardErr
		}
		guard = &updatedGuard
		assignments = bson.D{
			{Key: "leaseExpiresAt", Value: mustEncodeMongoTaskTime(updatedGuard.ExpiresAt)},
			{Key: "updatedAt", Value: mustEncodeMongoTaskTime(updatedGuard.UpdatedAt)},
		}
	}
	updated, err := transaction.updateOwnedTask(ctx, sessionContext, task, leaseToken, assignments)
	if err != nil {
		return err
	}
	if guard != nil && (!updated.LeaseExpiresAt.Equal(guard.ExpiresAt) || !updated.UpdatedAt.Equal(guard.UpdatedAt)) {
		return fmt.Errorf("stored MongoDB task heartbeat has inconsistent guard timestamps")
	}
	return nil
}

func (backend *Store) CompleteTask(ctx context.Context, id, leaseToken string, output json.RawMessage) error {
	if err := backend.prepareSystemOperation(ctx, "task completion"); err != nil {
		return err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	if err := store.ValidateTaskPayload(output); err != nil {
		return err
	}
	output = append(json.RawMessage(nil), output...)
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		return transaction.finishOwnedTask(ctx, id, leaseToken, mongoTaskTerminalAssignments(store.TaskStateSucceeded, output))
	})
}

func (backend *Store) FailTask(ctx context.Context, failure store.TaskFailure) error {
	if err := backend.prepareSystemOperation(ctx, "task failure"); err != nil {
		return err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	if err := store.ValidateTaskFailure(failure); err != nil {
		return err
	}
	assignments := mongoTaskTerminalAssignments(store.TaskStateFailed, nil)
	if failure.RetryAfter != nil {
		assignments = mongoTaskQueueAssignments(*failure.RetryAfter, failure.Code, failure.Message)
	} else {
		assignments = append(assignments,
			bson.E{Key: "lastErrorCode", Value: mongoTaskLiteral(failure.Code)},
			bson.E{Key: "lastError", Value: mongoTaskLiteral(failure.Message)},
		)
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		return transaction.finishOwnedTask(ctx, failure.ID, failure.LeaseToken, assignments)
	})
}

func (backend *Store) ReleaseTask(
	ctx context.Context,
	id, leaseToken string,
	delay time.Duration,
	code, message string,
) error {
	if err := backend.prepareSystemOperation(ctx, "task release"); err != nil {
		return err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	if err := store.ValidateTaskRelease(delay, code, message); err != nil {
		return err
	}
	assignments := mongoTaskQueueAssignments(delay, code, message)
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		return transaction.finishOwnedTask(ctx, id, leaseToken, assignments)
	})
}

func (transaction *documentTransaction) finishOwnedTask(
	ctx context.Context,
	id, leaseToken string,
	assignments bson.D,
) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	task, found, err := findMongoTaskRecord(sessionContext, transaction.taskCollection(), id)
	if err != nil {
		return err
	}
	if !found || task.State != store.TaskStateRunning || task.LeaseToken != leaseToken {
		return store.ErrTaskLeaseLost
	}
	if _, err := transaction.updateOwnedTask(ctx, sessionContext, task, leaseToken, assignments); err != nil {
		return err
	}
	return transaction.deleteTaskConcurrencyGuard(ctx, sessionContext, task, leaseToken, true)
}

func (transaction *documentTransaction) updateOwnedTask(
	callerContext, sessionContext context.Context,
	task store.Task,
	leaseToken string,
	assignments bson.D,
) (store.Task, error) {
	filter := mongoTaskIdentityFilter(task)
	filter = append(filter,
		bson.E{Key: "state", Value: string(store.TaskStateRunning)},
		bson.E{Key: "leaseToken", Value: leaseToken},
		bson.E{Key: "$expr", Value: bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$leaseExpiresAt"}}, "long"}}},
			bson.D{{Key: "$gt", Value: bson.A{"$leaseExpiresAt", mongoTaskServerNowNanosExpression()}}},
		}}}},
	)
	raw, err := transaction.taskCollection().FindOneAndUpdate(
		sessionContext,
		filter,
		mongo.Pipeline{bson.D{{Key: "$set", Value: assignments}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.Task{}, store.ErrTaskLeaseLost
	}
	if err != nil {
		return store.Task{}, translateMongoError(callerContext, err)
	}
	updated, err := decodeMongoTask(raw)
	if err != nil {
		return store.Task{}, err
	}
	if updated.ID != task.ID {
		return store.Task{}, mongoSystemIdentityCollision("task lifecycle")
	}
	return updated, nil
}

func mongoTaskQueueAssignments(delay time.Duration, code, message string) bson.D {
	return bson.D{
		{Key: "state", Value: string(store.TaskStateQueued)},
		{Key: "runAt", Value: mongoTaskServerDeadlineExpression(delay)},
		{Key: "output", Value: nil},
		{Key: "leaseToken", Value: ""},
		{Key: "leaseExpiresAt", Value: nil},
		{Key: "lastErrorCode", Value: mongoTaskLiteral(code)},
		{Key: "lastError", Value: mongoTaskLiteral(message)},
		{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
		{Key: "completedAt", Value: nil},
		{Key: "retainUntil", Value: nil},
	}
}

func mongoTaskTerminalAssignments(state store.TaskState, output json.RawMessage) bson.D {
	encodedOutput := any(nil)
	if len(output) != 0 {
		encodedOutput = mongoTaskLiteral(bson.Binary{Subtype: 0, Data: append([]byte(nil), output...)})
	}
	assignments := bson.D{
		{Key: "state", Value: string(state)},
		{Key: "output", Value: encodedOutput},
		{Key: "leaseToken", Value: ""},
		{Key: "leaseExpiresAt", Value: nil},
		{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
		{Key: "completedAt", Value: mongoTaskServerNowNanosExpression()},
		{Key: "retainUntil", Value: bson.D{{Key: "$add", Value: bson.A{mongoTaskServerNowNanosExpression(), "$retention"}}}},
	}
	if state == store.TaskStateSucceeded {
		assignments = append(assignments,
			bson.E{Key: "lastErrorCode", Value: ""},
			bson.E{Key: "lastError", Value: ""},
		)
	}
	return assignments
}

func (backend *Store) PruneTasks(ctx context.Context, limit int) (int, error) {
	if err := backend.prepareSystemOperation(ctx, "task pruning"); err != nil {
		return 0, err
	}
	if err := backend.requireVerifiedTaskIndexes(); err != nil {
		return 0, err
	}
	if err := store.ValidateTaskBatch(limit); err != nil {
		return 0, err
	}
	pruned := 0
	if err := backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		var operationErr error
		pruned, operationErr = transaction.pruneTasks(ctx, limit)
		return operationErr
	}); err != nil {
		return 0, err
	}
	return pruned, nil
}

func (transaction *documentTransaction) pruneTasks(ctx context.Context, limit int) (int, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return 0, err
	}
	defer leave()
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "retainUntil", Value: bson.D{{Key: "$type", Value: "long"}}},
			{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$retainUntil", mongoTaskServerNowNanosExpression()}}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "retainUntil", Value: int32(1)}, {Key: "_id", Value: int32(1)}}}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	}
	cursor, err := transaction.taskCollection().Aggregate(sessionContext, pipeline)
	if err != nil {
		return 0, translateMongoError(ctx, err)
	}
	candidates := make([]store.Task, 0, limit)
	for cursor.Next(sessionContext) {
		task, decodeErr := decodeMongoTask(cursor.Current)
		if decodeErr != nil {
			transaction.closeCursor(cursor)
			return 0, decodeErr
		}
		candidates = append(candidates, task)
	}
	if err := cursor.Err(); err != nil {
		transaction.closeCursor(cursor)
		return 0, translateMongoError(ctx, err)
	}
	transaction.closeCursor(cursor)
	for _, candidate := range candidates {
		filter := mongoTaskIdentityFilter(candidate)
		filter = append(filter,
			bson.E{Key: "retainUntil", Value: mustEncodeMongoTaskTime(*candidate.RetainUntil)},
			bson.E{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$retainUntil", mongoTaskServerNowNanosExpression()}}}},
		)
		result, deleteErr := transaction.taskCollection().DeleteOne(sessionContext, filter)
		if deleteErr != nil {
			return 0, translateMongoError(ctx, deleteErr)
		}
		if result.DeletedCount != 1 {
			return 0, mongoConfirmedTransactionConflict{}
		}
	}
	return len(candidates), nil
}

func (transaction *documentTransaction) deleteTaskState(ctx context.Context, reference store.DocumentReference) error {
	predicate := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "target.collection", Value: string(reference.CollectionID)}, {Key: "target.document", Value: reference.DocumentID}},
		bson.D{{Key: "requestedBy.collection", Value: string(reference.CollectionID)}, {Key: "requestedBy.document", Value: reference.DocumentID}},
	}}}
	if _, err := transaction.taskConcurrencyCollection().DeleteMany(ctx, predicate); err != nil {
		return translateMongoError(ctx, err)
	}
	_, err := transaction.taskCollection().DeleteMany(ctx, predicate)
	return translateMongoError(ctx, err)
}

var _ store.TaskStore = (*Store)(nil)
