package mongodb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (transaction *documentTransaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	if err := transaction.store.validateCollectionEnvelope(request.Collection); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedReferenceIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	values := store.CloneValues(request.Values)
	canonicalizeMongoAuthIdentity(request.Collection, values)
	if err := validateStoredValuesForLocales(request.Collection, values, request.Locales); err != nil {
		return store.Document{}, err
	}
	status := request.Status
	revision := 0
	if request.Collection.Upload != nil {
		revision = 1
	}
	if request.Collection.Versions == nil {
		if status != "" {
			return store.Document{}, fmt.Errorf("MongoDB document status requires a versioned collection")
		}
	} else {
		if status == "" {
			status = store.StatusPublished
			if request.Collection.Versions.Drafts {
				status = store.StatusDraft
			}
		}
		revision = 1
		if err := validateMongoVersionMetadata(request.Collection, status, revision); err != nil {
			return store.Document{}, err
		}
	}
	id := request.ID
	if id == "" {
		id, err = newDocumentID(request.Collection.ID)
		if err != nil {
			return store.Document{}, err
		}
	}
	if err := store.ValidateDocumentID(id); err != nil {
		return store.Document{}, err
	}
	now := transaction.store.now().UTC()
	createdAt := request.CreatedAt.UTC()
	if request.CreatedAt.IsZero() {
		createdAt = now
	}
	updatedAt := request.UpdatedAt.UTC()
	if request.UpdatedAt.IsZero() {
		updatedAt = createdAt
	}
	document := store.Document{
		ID: id, CreatedAt: createdAt, UpdatedAt: updatedAt,
		Status: status, Revision: revision, Values: values,
	}
	encoded, err := encodeDocument(document)
	if err != nil {
		return store.Document{}, err
	}
	if _, err := transaction.collection(request.Collection).InsertOne(sessionContext, encoded); err != nil {
		return store.Document{}, translateMongoError(ctx, err)
	}
	if request.Collection.Versions != nil && status == store.StatusPublished {
		if _, err := transaction.publishedCollection(request.Collection).InsertOne(sessionContext, encoded); err != nil {
			return store.Document{}, translateMongoError(ctx, err)
		}
		if request.Collection.Versions.Drafts {
			document.PublishedRevision = document.Revision
		}
	}
	if err := transaction.replaceHeadReservations(sessionContext, request.Collection, document, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.replaceDocumentReferences(sessionContext, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return store.CloneDocument(document), nil
}

func (transaction *documentTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	locked := request.Lock == store.LockMutation || request.Lock == store.LockReference
	var sessionContext context.Context
	var leave func()
	var err error
	if locked {
		sessionContext, leave, err = transaction.enterLock(ctx)
	} else {
		sessionContext, leave, err = transaction.enter(ctx, false)
	}
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	switch request.Lock {
	case store.LockNone, store.LockReference, store.LockMutation:
	default:
		return store.Document{}, fmt.Errorf("MongoDB Find does not support document lock mode %q", request.Lock)
	}
	validatedRequest := request
	validatedRequest.Lock = store.LockNone
	if err := transaction.store.validateRequestEnvelope(validatedRequest); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedPopulationIndexes(request); err != nil {
		return store.Document{}, err
	}
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	if err := store.ValidateDocumentID(request.ID); err != nil {
		return store.Document{}, err
	}
	predicate, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	var raw bson.Raw
	if locked {
		lock := mongoHeldLock{
			mode:       mongoLockShared,
			target:     mongoFenceTarget{collectionID: request.Collection.ID, documentID: request.ID},
			working:    transaction.collection(request.Collection),
			collection: transaction.readCollection(request),
			predicate:  predicate,
		}
		if request.Lock == store.LockMutation {
			lock.mode = mongoLockExclusive
			lock.predicate = mongoAnd([]bson.D{predicate, mongoTypeGuard(mongoFencePath, "long")})
		}
		raw, err = transaction.lockDocument(sessionContext, lock)
		if errors.Is(err, mongo.ErrNoDocuments) && lock.mode == mongoLockExclusive {
			// A corrupt fence must not masquerade as an absent document. Probe
			// with the same ID, filter, access, and deletion predicate so an
			// unauthorized or otherwise non-matching document remains hidden.
			candidate, probeErr := transaction.readCollection(request).FindOne(sessionContext, predicate).Raw()
			switch {
			case probeErr == nil:
				if _, decodeErr := decodeCollectionDocumentForLocales(candidate, request.Collection, request.Locales); decodeErr != nil {
					return store.Document{}, decodeErr
				}
			case !errors.Is(probeErr, mongo.ErrNoDocuments):
				return store.Document{}, translateMongoError(ctx, probeErr)
			}
		}
	} else {
		raw, err = transaction.readCollection(request).FindOne(sessionContext, predicate).Raw()
	}
	if err != nil {
		return store.Document{}, translateLockError(ctx, err)
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
		return store.Document{}, err
	}
	if err := transaction.attachPublishedMetadata(sessionContext, request, &document); err != nil {
		return store.Document{}, err
	}
	documents, err := transaction.populate(sessionContext, []store.Document{document}, request)
	if err != nil {
		return store.Document{}, err
	}
	return projectDocument(documents[0], request.Select), nil
}

func (transaction *documentTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Page{}, err
	}
	defer leave()
	if err := transaction.store.validateRequestEnvelope(request); err != nil {
		return store.Page{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Page{}, err
	}
	if err := transaction.store.requireVerifiedPopulationIndexes(request); err != nil {
		return store.Page{}, err
	}
	predicate, err := decoderFreeRequestPredicate(request, false)
	if err != nil {
		return store.Page{}, err
	}
	order, err := requestSort(request)
	if err != nil {
		return store.Page{}, err
	}
	collection := transaction.readCollection(request)
	// A SkipTotal read runs no count. It reads one row past the page so the
	// extra row alone proves that a next page exists.
	var result store.Page
	var start, read int
	if request.SkipTotal {
		var limit int
		result.Page, limit, start = store.UncountedPageBounds(request.Page, request.Limit)
		result.Limit, read = limit, limit+1
		if start == math.MaxInt {
			result.Documents = []store.Document{}
			return result, nil
		}
	} else {
		total64, err := collection.CountDocuments(sessionContext, predicate)
		if err != nil {
			return store.Page{}, translateMongoError(ctx, err)
		}
		if total64 > int64(math.MaxInt) {
			return store.Page{}, fmt.Errorf("MongoDB list total exceeds the platform integer range")
		}
		total := int(total64)
		var end int
		_, _, start, end = store.ListPageBounds(request.Page, request.Limit, total)
		result, read = store.CountedPage(nil, request.Page, request.Limit, total), end-start
		if read == 0 {
			result.Documents = []store.Document{}
			return result, nil
		}
	}
	var cursor *mongo.Cursor
	if len(order.computed) == 0 {
		cursor, err = mongoFind(sessionContext, collection, predicate, mongoFindCommand{sort: order.order, skip: int64(start), limit: int64(read)})
	} else {
		pipeline := mongo.Pipeline{
			bson.D{{Key: "$match", Value: predicate}},
			bson.D{{Key: "$set", Value: order.computed}},
			bson.D{{Key: "$sort", Value: order.order}},
			bson.D{{Key: "$skip", Value: int64(start)}},
			bson.D{{Key: "$limit", Value: int64(read)}},
			bson.D{{Key: "$unset", Value: order.temporary}},
		}
		cursor, err = mongoAggregate(sessionContext, collection, pipeline)
	}
	if err != nil {
		return store.Page{}, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)
	result.Documents = make([]store.Document, 0, min(read, result.Limit))
	for cursor.Next(sessionContext) {
		if len(result.Documents) == result.Limit {
			// Only a SkipTotal read reaches the overflow row; it is never decoded.
			result.HasNextPage = true
			break
		}
		document, err := decodeCollectionDocumentForLocales(cursor.Current, request.Collection, request.Locales)
		if err != nil {
			return store.Page{}, err
		}
		if err := transaction.attachPublishedMetadata(sessionContext, request, &document); err != nil {
			return store.Page{}, err
		}
		result.Documents = append(result.Documents, document)
	}
	if err := cursor.Err(); err != nil {
		return store.Page{}, translateMongoError(ctx, err)
	}
	result.Documents, err = transaction.populate(sessionContext, result.Documents, request)
	if err != nil {
		return store.Page{}, err
	}
	for index := range result.Documents {
		result.Documents[index] = projectDocument(result.Documents[index], request.Select)
	}
	return result, nil
}

func (transaction *documentTransaction) ResolveFilteredSelection(ctx context.Context, request store.FilteredSelectionRequest) (store.FilteredSelection, error) {
	if request.Limit < 1 || request.Limit > store.MaxListWindowDocuments {
		return store.FilteredSelection{}, fmt.Errorf("filtered selection limit must be between 1 and %d", store.MaxListWindowDocuments)
	}
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.FilteredSelection{}, err
	}
	defer leave()
	documentRequest := store.Request{
		Collection: request.Collection, Filter: request.Filter, Access: request.Access,
		Deletion: request.Deletion, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
	}
	if err := transaction.store.validateRequestEnvelope(documentRequest); err != nil {
		return store.FilteredSelection{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.FilteredSelection{}, err
	}
	predicate, err := decoderFreeRequestPredicate(documentRequest, false)
	if err != nil {
		return store.FilteredSelection{}, err
	}
	cursor, err := mongoFind(sessionContext, transaction.collection(request.Collection), predicate, mongoFindCommand{
		sort:       bson.D{{Key: "_id", Value: 1}},
		projection: bson.D{{Key: "_id", Value: 1}},
		limit:      int64(request.Limit + 1),
	})
	if err != nil {
		return store.FilteredSelection{}, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)
	ids := make([]string, 0, request.Limit+1)
	for cursor.Next(sessionContext) {
		id, ok := cursor.Current.Lookup("_id").StringValueOK()
		if !ok {
			return store.FilteredSelection{}, fmt.Errorf("stored MongoDB document has an invalid ID")
		}
		if err := store.ValidateDocumentID(id); err != nil {
			return store.FilteredSelection{}, fmt.Errorf("stored MongoDB document ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := cursor.Err(); err != nil {
		return store.FilteredSelection{}, translateMongoError(ctx, err)
	}
	overflow := len(ids) > request.Limit
	if overflow {
		ids = ids[:request.Limit]
	}
	return store.FilteredSelection{IDs: ids, Overflow: overflow}, nil
}

func (transaction *documentTransaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	if err := transaction.store.validateRequestEnvelope(request.Request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedReferenceIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	if err := store.ValidateDocumentID(request.ID); err != nil {
		return store.Document{}, err
	}
	if request.Collection.Versions == nil {
		if request.Intent != store.WriteIntentDefault {
			return store.Document{}, fmt.Errorf("write intent requires a versioned collection")
		}
	} else if !request.Collection.Versions.Drafts &&
		(request.Intent == store.WriteIntentSaveDraft || request.Intent == store.WriteIntentDiscardDraft) {
		return store.Document{}, fmt.Errorf("draft write intent requires a draft-enabled collection")
	} else if request.Collection.Versions.Drafts && request.Intent == store.WriteIntentDefault {
		return store.Document{}, fmt.Errorf("write intent is required for a draft-enabled collection")
	}
	switch request.Intent {
	case store.WriteIntentDefault, store.WriteIntentSaveDraft, store.WriteIntentPublish, store.WriteIntentUnpublish, store.WriteIntentDiscardDraft:
	default:
		return store.Document{}, fmt.Errorf("unsupported write intent %q", request.Intent)
	}
	values := store.CloneValues(request.Values)
	canonicalizeMongoAuthIdentity(request.Collection, values)
	if err := validateStoredValuesForLocales(request.Collection, values, request.Locales); err != nil {
		return store.Document{}, err
	}
	predicate, err := requestPredicate(request.Request, true)
	if err != nil {
		return store.Document{}, err
	}
	current, err := request.LockedCurrent()
	if err != nil {
		return store.Document{}, err
	}
	if request.ExpectedRevision > 0 && current.Revision != request.ExpectedRevision {
		return store.Document{}, store.ErrConflict
	}
	hasLive := store.HasLiveHead(request.Collection, current)
	liveRevision := current.PublishedRevision
	var live store.Document
	if request.Intent == store.WriteIntentDiscardDraft {
		// Only a discard needs the live content; every other intent derives
		// the live state from the locked working read.
		var found bool
		live, found, err = transaction.publishedHead(sessionContext, request.Collection, request.ID, request.Locales)
		if err != nil {
			return store.Document{}, err
		}
		hasLive = found
		liveRevision = live.Revision
	}
	if (request.Intent == store.WriteIntentUnpublish && !hasLive) || (request.Intent == store.WriteIntentDiscardDraft && (!hasLive || !current.HasDraftChanges)) {
		return store.Document{}, store.ErrConflict
	}
	// The row must still be the stored version Current describes. Every write
	// advances updatedAt, and revision on a revisioned resource, so a stale or
	// caller-built Current matches nothing and becomes a conflict.
	storedUpdatedAt, err := encodeTime(current.UpdatedAt)
	if err != nil {
		// No stored document has an unrepresentable updatedAt.
		return store.Document{}, store.ErrConflict
	}
	guard := []bson.D{predicate, {{Key: "meta.updatedAt", Value: storedUpdatedAt}}}
	if request.Collection.Versions != nil || request.Collection.Upload != nil {
		guard = append(guard, bson.D{{Key: mongoRevisionPath, Value: int64(current.Revision)}})
	}
	predicate = mongoAnd(guard)
	updatedAt, err := encodeTime(nextUpdatedAt(transaction.store.now(), current.UpdatedAt))
	if err != nil {
		return store.Document{}, err
	}
	assignments := bson.D{{Key: "meta.updatedAt", Value: mongoLiteral(updatedAt)}}
	if request.Collection.Versions != nil {
		switch request.Intent {
		case store.WriteIntentSaveDraft:
			status := store.StatusDraft
			if hasLive {
				status = store.StatusPublished
			}
			assignments = append(assignments, bson.E{Key: mongoStatusPath, Value: mongoLiteral(string(status))}, bson.E{Key: "meta.pending", Value: mongoLiteral(hasLive)})
		case store.WriteIntentPublish:
			assignments = append(assignments, bson.E{Key: mongoStatusPath, Value: mongoLiteral(string(store.StatusPublished))}, bson.E{Key: "meta.pending", Value: mongoLiteral(false)})
		case store.WriteIntentUnpublish:
			assignments = append(assignments, bson.E{Key: mongoStatusPath, Value: mongoLiteral(string(store.StatusDraft))}, bson.E{Key: "meta.pending", Value: mongoLiteral(false)})
		case store.WriteIntentDiscardDraft:
			assignments = append(assignments, bson.E{Key: mongoStatusPath, Value: mongoLiteral(string(store.StatusPublished))}, bson.E{Key: "meta.pending", Value: mongoLiteral(false)})
		case store.WriteIntentDefault:
			assignments = append(assignments, bson.E{Key: "meta.pending", Value: mongoLiteral(false)})
		}
	}
	if request.Intent == store.WriteIntentDiscardDraft {
		encodedValues, encodeErr := encodeValues(live.Values)
		if encodeErr != nil {
			return store.Document{}, encodeErr
		}
		assignments = append(assignments, bson.E{Key: "values", Value: mongoLiteral(encodedValues)})
	} else if request.ReplaceValues {
		encodedValues, encodeErr := encodeValues(values)
		if encodeErr != nil {
			return store.Document{}, encodeErr
		}
		assignments = append(assignments, bson.E{Key: "values", Value: mongoLiteral(encodedValues)})
	} else {
		valueAssignments, assignmentErr := mongoPatchAssignments(request.Collection, values)
		if assignmentErr != nil {
			return store.Document{}, assignmentErr
		}
		assignments = append(assignments, valueAssignments...)
	}
	if request.Collection.Versions != nil || request.Collection.Upload != nil {
		assignments = append(assignments, bson.E{Key: mongoRevisionPath, Value: bson.D{{Key: "$add", Value: bson.A{"$" + mongoRevisionPath, int64(1)}}}})
	}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: assignments}}}
	if err := transaction.lockForWrite(sessionContext, request.Collection, request.ID); err != nil {
		return store.Document{}, err
	}
	result := transaction.collection(request.Collection).FindOneAndUpdate(
		sessionContext, predicate, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
	)
	raw, err := result.Raw()
	if err := transaction.mutationResultError(ctx, sessionContext, request.Request, err, true); err != nil {
		return store.Document{}, err
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
		return store.Document{}, err
	}
	if request.Collection.Versions != nil {
		switch request.Intent {
		case store.WriteIntentSaveDraft, store.WriteIntentDiscardDraft:
			// The published snapshot remains unchanged.
		case store.WriteIntentUnpublish:
			if _, err := transaction.publishedCollection(request.Collection).DeleteOne(sessionContext, bson.D{{Key: "_id", Value: document.ID}}); err != nil {
				return store.Document{}, translateMongoError(ctx, err)
			}
		case store.WriteIntentDefault, store.WriteIntentPublish:
			if document.Status == store.StatusPublished {
				published := store.CloneDocument(document)
				published.HasDraftChanges = false
				encoded, encodeErr := encodeDocument(published)
				if encodeErr != nil {
					return store.Document{}, encodeErr
				}
				if _, err := transaction.publishedCollection(request.Collection).ReplaceOne(sessionContext, bson.D{{Key: "_id", Value: document.ID}}, encoded, options.Replace().SetUpsert(true)); err != nil {
					return store.Document{}, translateMongoError(ctx, err)
				}
			} else if _, err := transaction.publishedCollection(request.Collection).DeleteOne(sessionContext, bson.D{{Key: "_id", Value: document.ID}}); err != nil {
				return store.Document{}, translateMongoError(ctx, err)
			}
		}
		// The live state just written in this transaction is the state an
		// authoring read would attach; the pending flag is stored on the row.
		document.PublishedRevision = 0
		if request.Collection.Versions.Drafts && document.Status == store.StatusPublished {
			switch request.Intent {
			case store.WriteIntentSaveDraft, store.WriteIntentDiscardDraft:
				document.PublishedRevision = liveRevision
			default:
				document.PublishedRevision = document.Revision
			}
		}
		if !request.Collection.Versions.Drafts {
			document.HasDraftChanges = false
		}
	}
	if err := transaction.replaceHeadReservations(sessionContext, request.Collection, document, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.replaceDocumentReferences(sessionContext, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	// The operation engine persists the returned canonical document as the
	// version snapshot before applying the caller's response projection. Match
	// the other official adapters by keeping versioned mutation results whole.
	if request.Collection.Versions != nil {
		return store.CloneDocument(document), nil
	}
	return projectDocument(document, request.Select), nil
}

func (transaction *documentTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	if !request.Collection.Capabilities.Trash {
		return store.Document{}, fmt.Errorf("collection %q does not support trash", request.Collection.ID)
	}
	request.Deletion = store.DeletionActive
	return transaction.setTrashed(ctx, request, true)
}

func (transaction *documentTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if !request.Collection.Capabilities.Trash {
		return store.Document{}, fmt.Errorf("collection %q does not support trash", request.Collection.ID)
	}
	request.Deletion = store.DeletionTrash
	return transaction.setTrashed(ctx, request, false)
}

func (transaction *documentTransaction) setTrashed(ctx context.Context, request store.Request, trashed bool) (store.Document, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	if err := transaction.store.validateRequestEnvelope(request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	if err := store.ValidateDocumentID(request.ID); err != nil {
		return store.Document{}, err
	}
	predicate, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	now, err := encodeTime(transaction.store.now().UTC())
	if err != nil {
		return store.Document{}, err
	}
	deletedAt := any(nil)
	if trashed {
		deletedAt = now
	}
	// updatedAt strictly advances, as in Update, even if the clock does not.
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
		{Key: "meta.updatedAt", Value: bson.D{{Key: "$max", Value: bson.A{now, bson.D{{Key: "$add", Value: bson.A{"$meta.updatedAt", int64(1)}}}}}}},
		{Key: "meta.deletedAt", Value: mongoLiteral(deletedAt)},
	}}}}
	if err := transaction.lockForWrite(sessionContext, request.Collection, request.ID); err != nil {
		return store.Document{}, err
	}
	raw, err := transaction.collection(request.Collection).FindOneAndUpdate(
		sessionContext, predicate, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Raw()
	if err := transaction.mutationResultError(ctx, sessionContext, request, err, false); err != nil {
		return store.Document{}, err
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
		return store.Document{}, err
	}
	if request.Collection.Versions != nil {
		if _, err := transaction.publishedCollection(request.Collection).UpdateOne(sessionContext,
			bson.D{{Key: "_id", Value: document.ID}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "meta.updatedAt", Value: now}, {Key: "meta.deletedAt", Value: deletedAt}}}}); err != nil {
			return store.Document{}, translateMongoError(ctx, err)
		}
		if err := transaction.attachPublishedMetadata(sessionContext, request, &document); err != nil {
			return store.Document{}, err
		}
	}
	if err := transaction.replaceHeadReservations(sessionContext, request.Collection, document, request.Locales); err != nil {
		return store.Document{}, err
	}
	return projectDocument(document, request.Select), nil
}

func (transaction *documentTransaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	if err := transaction.store.validateRequestEnvelope(request); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedReferenceIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(request.Collection); err != nil {
		return store.Document{}, err
	}
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	if err := store.ValidateDocumentID(request.ID); err != nil {
		return store.Document{}, err
	}
	predicate, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	raw, err := transaction.collection(request.Collection).FindOneAndDelete(sessionContext, predicate).Raw()
	if err := transaction.mutationResultError(ctx, sessionContext, request, err, false); err != nil {
		return store.Document{}, err
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
		return store.Document{}, err
	}
	if request.Collection.Versions != nil {
		if _, err := transaction.publishedCollection(request.Collection).DeleteOne(sessionContext, bson.D{{Key: "_id", Value: document.ID}}); err != nil {
			return store.Document{}, translateMongoError(ctx, err)
		}
	}
	// A deleted document keeps no shared fences. Deleting them also excludes
	// their holders, as the deleted working head excludes a transaction
	// creating one.
	if err := transaction.deleteSharedFences(sessionContext, mongoFenceTarget{collectionID: request.Collection.ID, documentID: document.ID}); err != nil {
		return store.Document{}, translateMongoError(ctx, err)
	}
	hasRelationships, err := transaction.store.collectionHasRelationships(request.Collection)
	if err != nil {
		return store.Document{}, err
	}
	if hasRelationships {
		if err := transaction.deleteDocumentReferences(sessionContext, store.DocumentReference{CollectionID: request.Collection.ID, DocumentID: document.ID}); err != nil {
			return store.Document{}, err
		}
	}
	return projectDocument(document, request.Select), nil
}

// mutationResultError reports a write that matched no document. A visible
// document that the write did not match has another expected revision or,
// for a guarded write, changed since it was read: that is a conflict.
func (transaction *documentTransaction) mutationResultError(ctx, sessionContext context.Context, request store.Request, mutationError error, guarded bool) error {
	if mutationError == nil {
		return nil
	}
	if !errors.Is(mutationError, mongo.ErrNoDocuments) || request.ExpectedRevision <= 0 && !guarded {
		return translateMongoError(ctx, mutationError)
	}
	probe := request
	probe.ExpectedRevision = 0
	visiblePredicate, err := requestPredicate(probe, true)
	if err != nil {
		return err
	}
	raw, err := transaction.collection(request.Collection).FindOne(sessionContext, visiblePredicate).Raw()
	switch {
	case err == nil:
		if _, decodeErr := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales); decodeErr != nil {
			return decodeErr
		}
		return store.ErrConflict
	case errors.Is(err, mongo.ErrNoDocuments):
		return store.ErrNotFound
	default:
		return translateMongoError(ctx, err)
	}
}

func (transaction *documentTransaction) ApplyReferenceDelete(ctx context.Context, request store.ReferenceDeleteRequest) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	return transaction.applyReferenceDelete(sessionContext, request)
}

// CascadeOwners lists current owners that reference the target through a
// cascade field.
func (transaction *documentTransaction) CascadeOwners(ctx context.Context, request store.ReferenceDeleteRequest) ([]store.DocumentReference, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return nil, err
	}
	defer leave()
	return transaction.cascadeOwners(sessionContext, request)
}

func (transaction *documentTransaction) DeleteDocumentState(ctx context.Context, reference store.DocumentReference) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if reference.CollectionID == "" || !schema.IsValidStableID(string(reference.CollectionID)) || reference.DocumentID == "" {
		return fmt.Errorf("document state cleanup requires collection and document IDs")
	}
	if err := store.ValidateDocumentID(reference.DocumentID); err != nil {
		return err
	}
	if err := transaction.store.requireVerifiedPreferenceIndexes(); err != nil {
		return err
	}
	if err := transaction.store.requireVerifiedDocumentLockIndexes(); err != nil {
		return err
	}
	if err := transaction.store.requireVerifiedTaskIndexes(); err != nil {
		return err
	}
	if err := transaction.store.requireVerifiedAuthIndexes(); err != nil {
		return err
	}
	versioned, references, err := transaction.store.requireVerifiedDocumentStatePlan(reference.CollectionID)
	if err != nil {
		return err
	}
	return transaction.deleteDocumentState(ctx, sessionContext, reference, versioned, references)
}

// deleteDocumentState removes the framework state reference owns or targets.
// Its caller has entered the transaction and established which namespaces
// the resource uses.
func (transaction *documentTransaction) deleteDocumentState(ctx, sessionContext context.Context, reference store.DocumentReference, versioned, references bool) error {
	if references {
		if err := transaction.deleteDocumentReferenceState(sessionContext, reference); err != nil {
			return err
		}
	}
	if err := transaction.deletePreferenceState(sessionContext, reference); err != nil {
		return err
	}
	if err := transaction.deleteDocumentLockState(sessionContext, reference); err != nil {
		return err
	}
	if err := transaction.deleteTaskState(sessionContext, reference); err != nil {
		return err
	}
	if err := transaction.deleteAuthState(sessionContext, reference); err != nil {
		return err
	}
	if !versioned {
		return nil
	}
	// Version state is keyed by stable resource identity plus canonical document
	// ID. DeleteMany is deliberately idempotent so a retried version-enabled
	// cleanup does not need a separate existence probe.
	_, err := transaction.store.database.Collection(physicalVersionCollectionName(reference.CollectionID)).DeleteMany(
		sessionContext,
		bson.D{{Key: mongoVersionOwnerPath, Value: reference.DocumentID}},
	)
	return translateMongoError(ctx, err)
}

// nextUpdatedAt is the updatedAt of a write that replaces a document last
// written at previous. It strictly increases even when the clock does not
// advance, so updatedAt identifies a document's stored version.
func nextUpdatedAt(now, previous time.Time) time.Time {
	now = now.UTC()
	if now.After(previous) {
		return now
	}
	return previous.UTC().Add(time.Nanosecond)
}

func canonicalizeMongoAuthIdentity(collection schema.Collection, values store.Values) {
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

func (transaction *documentTransaction) collection(collection schema.Collection) *mongo.Collection {
	return transaction.store.database.Collection(physicalCollectionName(collection.ID))
}

func (transaction *documentTransaction) publishedCollection(collection schema.Collection) *mongo.Collection {
	return transaction.store.database.Collection(physicalPublishedCollectionName(collection.ID))
}

func (transaction *documentTransaction) publishedHead(ctx context.Context, collection schema.Collection, id string, locales []schema.LocaleCode) (store.Document, bool, error) {
	if collection.Versions == nil {
		return store.Document{}, false, nil
	}
	raw, err := transaction.publishedCollection(collection).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.Document{}, false, nil
	}
	if err != nil {
		return store.Document{}, false, translateMongoError(ctx, err)
	}
	document, err := decodeCollectionDocumentForLocales(raw, collection, locales)
	if err != nil {
		return store.Document{}, false, err
	}
	return document, true, nil
}

func (transaction *documentTransaction) readCollection(request store.Request) *mongo.Collection {
	if request.PublishedOnly && request.Collection.Versions != nil {
		return transaction.publishedCollection(request.Collection)
	}
	return transaction.collection(request.Collection)
}

func (transaction *documentTransaction) attachPublishedMetadata(ctx context.Context, request store.Request, document *store.Document) error {
	if request.PublishedOnly || request.Collection.Versions == nil || !request.Collection.Versions.Drafts {
		document.PublishedRevision = 0
		document.HasDraftChanges = false
		return nil
	}
	raw, err := transaction.publishedCollection(request.Collection).FindOne(ctx, bson.D{{Key: "_id", Value: document.ID}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		if document.Status == store.StatusPublished {
			return fmt.Errorf("stored MongoDB document %q has published status without a published head", document.ID)
		}
		return nil
	}
	if err != nil {
		return translateMongoError(ctx, err)
	}
	live, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
		return err
	}
	if document.Status != store.StatusPublished || live.Status != store.StatusPublished {
		return fmt.Errorf("stored MongoDB document %q has inconsistent publication metadata", document.ID)
	}
	document.PublishedRevision = live.Revision
	return nil
}

func (transaction *documentTransaction) closeCursor(cursor *mongo.Cursor) {
	cleanupContext, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	_ = cursor.Close(mongo.NewSessionContext(cleanupContext, transaction.session))
}

func projectDocument(document store.Document, selection []query.Path) store.Document {
	if selection == nil {
		return store.CloneDocument(document)
	}
	projected := store.CloneDocument(document)
	projected.Values = make(store.Values)
	for _, path := range selection {
		segments := path.Segments()
		if len(segments) != 1 {
			continue
		}
		if value, exists := document.Values[segments[0]]; exists {
			projected.Values[segments[0]] = value
		}
	}
	return projected
}
