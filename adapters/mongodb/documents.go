package mongodb

import (
	"context"
	"errors"
	"fmt"
	"math"

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
	if err := validateCollectionEnvelope(request.Collection); err != nil {
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
	if err := validateCompleteValuesForLocales(request.Collection, values, request.Locales); err != nil {
		return store.Document{}, err
	}
	status := request.Status
	revision := 0
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
	if err := transaction.replaceDocumentReferences(sessionContext, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return store.CloneDocument(document), nil
}

func (transaction *documentTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	sessionContext, leave, err := transaction.enter(ctx, request.Lock == store.LockMutation || request.Lock == store.LockReference)
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
	if err := validateRequestEnvelope(validatedRequest); err != nil {
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
	if request.Lock == store.LockMutation || request.Lock == store.LockReference {
		basePredicate := predicate
		predicate = mongoAnd([]bson.D{basePredicate, mongoTypeGuard(mongoFencePath, "long")})
		raw, err = transaction.collection(request.Collection).FindOneAndUpdate(
			sessionContext,
			predicate,
			bson.D{{Key: "$inc", Value: bson.D{{Key: mongoFencePath, Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if errors.Is(err, mongo.ErrNoDocuments) {
			// A corrupt fence must not masquerade as an absent document. Probe
			// with the same ID, filter, access, and deletion predicate so an
			// unauthorized or otherwise non-matching document remains hidden.
			candidate, probeErr := transaction.collection(request.Collection).FindOne(sessionContext, basePredicate).Raw()
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
		raw, err = transaction.collection(request.Collection).FindOne(sessionContext, predicate).Raw()
	}
	if err != nil {
		return store.Document{}, translateMongoError(ctx, err)
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
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
	if err := validateRequestEnvelope(request); err != nil {
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
	total64, err := transaction.collection(request.Collection).CountDocuments(sessionContext, predicate)
	if err != nil {
		return store.Page{}, translateMongoError(ctx, err)
	}
	if total64 > int64(math.MaxInt) {
		return store.Page{}, fmt.Errorf("MongoDB list total exceeds the platform integer range")
	}
	total := int(total64)
	page, limit, start, end := store.ListPageBounds(request.Page, request.Limit, total)
	result := store.Page{Page: page, Limit: limit, Total: total}
	if start == end {
		result.Documents = []store.Document{}
		return result, nil
	}
	var cursor *mongo.Cursor
	if len(order.computed) == 0 {
		findOptions := options.Find().SetSort(order.order).SetSkip(int64(start)).SetLimit(int64(end - start))
		cursor, err = transaction.collection(request.Collection).Find(sessionContext, predicate, findOptions)
	} else {
		pipeline := mongo.Pipeline{
			bson.D{{Key: "$match", Value: predicate}},
			bson.D{{Key: "$set", Value: order.computed}},
			bson.D{{Key: "$sort", Value: order.order}},
			bson.D{{Key: "$skip", Value: int64(start)}},
			bson.D{{Key: "$limit", Value: int64(end - start)}},
			bson.D{{Key: "$unset", Value: order.temporary}},
		}
		cursor, err = transaction.collection(request.Collection).Aggregate(sessionContext, pipeline)
	}
	if err != nil {
		return store.Page{}, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)
	result.Documents = make([]store.Document, 0, end-start)
	for cursor.Next(sessionContext) {
		document, err := decodeCollectionDocumentForLocales(cursor.Current, request.Collection, request.Locales)
		if err != nil {
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
	if err := validateRequestEnvelope(documentRequest); err != nil {
		return store.FilteredSelection{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.FilteredSelection{}, err
	}
	predicate, err := decoderFreeRequestPredicate(documentRequest, false)
	if err != nil {
		return store.FilteredSelection{}, err
	}
	findOptions := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetProjection(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(request.Limit + 1))
	cursor, err := transaction.collection(request.Collection).Find(sessionContext, predicate, findOptions)
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
	if err := validateRequestEnvelope(request.Request); err != nil {
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
		if request.Status != nil {
			return store.Document{}, fmt.Errorf("MongoDB document status requires a versioned collection")
		}
	} else if request.Status != nil {
		if err := validateMongoVersionMetadata(request.Collection, *request.Status, 1); err != nil {
			return store.Document{}, err
		}
	}
	values := store.CloneValues(request.Values)
	canonicalizeMongoAuthIdentity(request.Collection, values)
	if err := validatePatchValuesForLocales(request.Collection, values, request.Locales); err != nil {
		return store.Document{}, err
	}
	predicate, err := requestPredicate(request.Request, true)
	if err != nil {
		return store.Document{}, err
	}
	updatedAt, err := encodeTime(transaction.store.now().UTC())
	if err != nil {
		return store.Document{}, err
	}
	assignments := bson.D{{Key: "meta.updatedAt", Value: mongoLiteral(updatedAt)}}
	if request.Status != nil {
		assignments = append(assignments, bson.E{Key: mongoStatusPath, Value: mongoLiteral(string(*request.Status))})
	}
	if request.ReplaceValues {
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
	if request.Collection.Versions != nil {
		assignments = append(assignments, bson.E{Key: mongoRevisionPath, Value: bson.D{{Key: "$add", Value: bson.A{"$" + mongoRevisionPath, int64(1)}}}})
	}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: assignments}}}
	result := transaction.collection(request.Collection).FindOneAndUpdate(
		sessionContext, predicate, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
	)
	raw, err := result.Raw()
	if err := transaction.mutationResultError(ctx, sessionContext, request.Request, err); err != nil {
		return store.Document{}, err
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
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
	if err := validateRequestEnvelope(request); err != nil {
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
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "meta.updatedAt", Value: now},
		{Key: "meta.deletedAt", Value: deletedAt},
	}}}
	raw, err := transaction.collection(request.Collection).FindOneAndUpdate(
		sessionContext, predicate, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Raw()
	if err := transaction.mutationResultError(ctx, sessionContext, request, err); err != nil {
		return store.Document{}, err
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
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
	if err := validateRequestEnvelope(request); err != nil {
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
	if err := transaction.mutationResultError(ctx, sessionContext, request, err); err != nil {
		return store.Document{}, err
	}
	document, err := decodeCollectionDocumentForLocales(raw, request.Collection, request.Locales)
	if err != nil {
		return store.Document{}, err
	}
	if mongoCollectionHasRelationships(request.Collection) {
		if err := transaction.deleteDocumentReferences(sessionContext, store.DocumentReference{CollectionID: request.Collection.ID, DocumentID: document.ID}); err != nil {
			return store.Document{}, err
		}
	}
	return projectDocument(document, request.Select), nil
}

func (transaction *documentTransaction) mutationResultError(ctx, sessionContext context.Context, request store.Request, mutationError error) error {
	if mutationError == nil {
		return nil
	}
	if !errors.Is(mutationError, mongo.ErrNoDocuments) || request.ExpectedRevision <= 0 {
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
	_, err = transaction.store.database.Collection(physicalVersionCollectionName(reference.CollectionID)).DeleteMany(
		sessionContext,
		bson.D{{Key: mongoVersionOwnerPath, Value: reference.DocumentID}},
	)
	return translateMongoError(ctx, err)
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
