package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoDocumentLockCollectionName = "z_ridu_document_locks"

func (backend *Store) FindDocumentLock(ctx context.Context, collectionID schema.StableID, documentID string, now time.Time) (store.DocumentLock, error) {
	if err := backend.prepareSystemOperation(ctx, "document lock"); err != nil {
		return store.DocumentLock{}, err
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err != nil {
		return store.DocumentLock{}, err
	}
	identity := store.DocumentLock{CollectionID: collectionID, DocumentID: documentID}
	if err := validateMongoDocumentLockTarget(identity); err != nil {
		return store.DocumentLock{}, err
	}
	now, _, err := normalizeMongoSystemTime(now, "document lock lookup time")
	if err != nil {
		return store.DocumentLock{}, err
	}
	lock, err := backend.findDocumentLockRecord(ctx, identity)
	if err != nil {
		return store.DocumentLock{}, err
	}
	if !lock.ExpiresAt.After(now) {
		return store.DocumentLock{}, store.ErrNotFound
	}
	return lock, nil
}

func (backend *Store) AcquireDocumentLock(ctx context.Context, candidate store.DocumentLock, now time.Time, takeover bool) (store.DocumentLock, bool, error) {
	if err := backend.prepareSystemOperation(ctx, "document lock"); err != nil {
		return store.DocumentLock{}, false, err
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err != nil {
		return store.DocumentLock{}, false, err
	}
	candidate, now, err := normalizeMongoDocumentLock(candidate, now)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	references, err := backend.captureActiveDocumentReferences(
		ctx,
		store.DocumentReference{CollectionID: candidate.CollectionID, DocumentID: candidate.DocumentID},
		store.DocumentReference{CollectionID: candidate.OwnerCollectionID, DocumentID: candidate.OwnerID},
	)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	var lock store.DocumentLock
	var acquired bool
	if err := backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		var operationErr error
		lock, acquired, operationErr = transaction.acquireDocumentLock(ctx, candidate, now, takeover, references)
		return operationErr
	}); err != nil {
		return store.DocumentLock{}, false, err
	}
	return lock, acquired, nil
}

func (backend *Store) findDocumentLockRecord(ctx context.Context, identity store.DocumentLock) (store.DocumentLock, error) {
	lock, found, err := findMongoDocumentLockByIdentity(ctx, backend.documentLockCollection(), identity)
	if err != nil {
		return store.DocumentLock{}, err
	}
	if !found {
		return store.DocumentLock{}, store.ErrNotFound
	}
	return lock, nil
}

func (transaction *documentTransaction) acquireDocumentLock(
	ctx context.Context,
	candidate store.DocumentLock,
	now time.Time,
	takeover bool,
	references []mongoActiveDocumentReference,
) (store.DocumentLock, bool, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	defer leave()
	if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
		return store.DocumentLock{}, false, err
	}

	id := mongoDocumentLockID(candidate)
	current, found, err := findMongoDocumentLockByIdentity(sessionContext, transaction.documentLockCollection(), candidate)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	if !found {
		encoded, encodeErr := encodeMongoDocumentLock(candidate)
		if encodeErr != nil {
			return store.DocumentLock{}, false, encodeErr
		}
		if _, insertErr := transaction.documentLockCollection().InsertOne(sessionContext, encoded); insertErr != nil {
			return store.DocumentLock{}, false, translateMongoError(ctx, insertErr)
		}
		return candidate, true, nil
	}
	sameOwner := current.OwnerCollectionID == candidate.OwnerCollectionID && current.OwnerID == candidate.OwnerID
	if current.ExpiresAt.After(now) && !sameOwner && !takeover {
		return current, false, nil
	}
	if sameOwner {
		candidate.CreatedAt = current.CreatedAt
	}
	replacement, err := encodeMongoDocumentLock(candidate)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	raw, err := transaction.documentLockCollection().FindOneAndReplace(
		sessionContext,
		mongoDocumentLockTargetFilter(id, candidate),
		replacement,
		options.FindOneAndReplace().SetReturnDocument(options.After),
	).Raw()
	if err != nil {
		return store.DocumentLock{}, false, translateMongoError(ctx, err)
	}
	stored, err := decodeMongoDocumentLock(raw)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	if !sameMongoDocumentLockTarget(stored, candidate) {
		return store.DocumentLock{}, false, mongoSystemIdentityCollision("document lock")
	}
	return stored, true, nil
}

func (backend *Store) ReleaseDocumentLock(
	ctx context.Context,
	collectionID schema.StableID,
	documentID string,
	ownerCollectionID schema.StableID,
	ownerID string,
) error {
	if err := backend.prepareSystemOperation(ctx, "document lock"); err != nil {
		return err
	}
	if err := backend.requireVerifiedDocumentLockIndexes(); err != nil {
		return err
	}
	identity := store.DocumentLock{
		CollectionID: collectionID, DocumentID: documentID,
		OwnerCollectionID: ownerCollectionID, OwnerID: ownerID,
	}
	if err := validateMongoDocumentLockIdentity(identity); err != nil {
		return err
	}
	id := mongoDocumentLockID(identity)
	current, found, err := findMongoDocumentLockByIdentity(ctx, backend.documentLockCollection(), identity)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if current.OwnerCollectionID != ownerCollectionID || current.OwnerID != ownerID {
		return nil
	}
	_, err = backend.documentLockCollection().DeleteOne(ctx, mongoDocumentLockOwnerFilter(id, identity))
	return translateMongoError(ctx, err)
}

func normalizeMongoDocumentLock(candidate store.DocumentLock, now time.Time) (store.DocumentLock, time.Time, error) {
	if err := validateMongoDocumentLock(candidate); err != nil {
		return store.DocumentLock{}, time.Time{}, err
	}
	createdAt, _, err := normalizeMongoSystemTime(candidate.CreatedAt, "document lock createdAt")
	if err != nil {
		return store.DocumentLock{}, time.Time{}, err
	}
	updatedAt, _, err := normalizeMongoSystemTime(candidate.UpdatedAt, "document lock updatedAt")
	if err != nil {
		return store.DocumentLock{}, time.Time{}, err
	}
	expiresAt, _, err := normalizeMongoSystemTime(candidate.ExpiresAt, "document lock expiresAt")
	if err != nil {
		return store.DocumentLock{}, time.Time{}, err
	}
	now, _, err = normalizeMongoSystemTime(now, "document lock acquisition time")
	if err != nil {
		return store.DocumentLock{}, time.Time{}, err
	}
	candidate.CreatedAt = createdAt
	candidate.UpdatedAt = updatedAt
	candidate.ExpiresAt = expiresAt
	return candidate, now, nil
}

func validateMongoDocumentLock(lock store.DocumentLock) error {
	if err := validateMongoDocumentLockIdentity(lock); err != nil {
		return err
	}
	return validateMongoSystemString(lock.OwnerLabel, "document lock owner label")
}

func validateMongoDocumentLockIdentity(lock store.DocumentLock) error {
	if err := validateMongoDocumentLockTarget(lock); err != nil {
		return err
	}
	return validateMongoSystemReference(store.DocumentReference{
		CollectionID: lock.OwnerCollectionID,
		DocumentID:   lock.OwnerID,
	}, "document lock owner")
}

func validateMongoDocumentLockTarget(lock store.DocumentLock) error {
	return validateMongoSystemReference(store.DocumentReference{
		CollectionID: lock.CollectionID,
		DocumentID:   lock.DocumentID,
	}, "document lock target")
}

func mongoDocumentLockID(lock store.DocumentLock) string {
	return mongoSystemRecordID("document_lock", string(lock.CollectionID), lock.DocumentID)
}

func mongoDocumentLockLogicalTargetFilter(lock store.DocumentLock) bson.D {
	return bson.D{
		{Key: "collection", Value: string(lock.CollectionID)},
		{Key: "document", Value: lock.DocumentID},
	}
}

func mongoDocumentLockTargetFilter(id string, lock store.DocumentLock) bson.D {
	filter := bson.D{{Key: "_id", Value: id}}
	return append(filter, mongoDocumentLockLogicalTargetFilter(lock)...)
}

func mongoDocumentLockOwnerFilter(id string, lock store.DocumentLock) bson.D {
	filter := mongoDocumentLockTargetFilter(id, lock)
	return append(filter,
		bson.E{Key: "ownerCollection", Value: string(lock.OwnerCollectionID)},
		bson.E{Key: "owner", Value: lock.OwnerID},
	)
}

// findMongoDocumentLockByIdentity validates both the unique logical target and
// its deterministic primary key. This makes permanent out-of-band identity
// drift a semantic collision instead of an endlessly retryable duplicate-key
// insert, while retaining the ordinary transaction retry for a real race.
func findMongoDocumentLockByIdentity(
	ctx context.Context,
	collection *mongo.Collection,
	identity store.DocumentLock,
) (store.DocumentLock, bool, error) {
	raw, err := collection.FindOne(ctx, mongoDocumentLockLogicalTargetFilter(identity)).Raw()
	if err == nil {
		lock, decodeErr := decodeMongoDocumentLock(raw)
		if decodeErr != nil {
			return store.DocumentLock{}, false, decodeErr
		}
		if !sameMongoDocumentLockTarget(lock, identity) {
			return store.DocumentLock{}, false, mongoSystemIdentityCollision("document lock")
		}
		return lock, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return store.DocumentLock{}, false, translateMongoError(ctx, err)
	}

	raw, err = collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoDocumentLockID(identity)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.DocumentLock{}, false, nil
	}
	if err != nil {
		return store.DocumentLock{}, false, translateMongoError(ctx, err)
	}
	lock, err := decodeMongoDocumentLock(raw)
	if err != nil {
		return store.DocumentLock{}, false, err
	}
	if !sameMongoDocumentLockTarget(lock, identity) {
		return store.DocumentLock{}, false, mongoSystemIdentityCollision("document lock")
	}
	return lock, true, nil
}

func encodeMongoDocumentLock(lock store.DocumentLock) (bson.D, error) {
	if err := validateMongoDocumentLock(lock); err != nil {
		return nil, err
	}
	createdAt, err := encodeTime(lock.CreatedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB document lock createdAt: %w", err)
	}
	updatedAt, err := encodeTime(lock.UpdatedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB document lock updatedAt: %w", err)
	}
	expiresAt, err := encodeTime(lock.ExpiresAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB document lock expiresAt: %w", err)
	}
	return bson.D{
		{Key: "_id", Value: mongoDocumentLockID(lock)},
		{Key: "codec", Value: int32(1)},
		{Key: "collection", Value: string(lock.CollectionID)},
		{Key: "document", Value: lock.DocumentID},
		{Key: "ownerCollection", Value: string(lock.OwnerCollectionID)},
		{Key: "owner", Value: lock.OwnerID},
		{Key: "ownerLabel", Value: lock.OwnerLabel},
		{Key: "createdAt", Value: createdAt},
		{Key: "updatedAt", Value: updatedAt},
		{Key: "expiresAt", Value: expiresAt},
	}, nil
}

func decodeMongoDocumentLock(raw bson.Raw) (store.DocumentLock, error) {
	if err := requireExactKeys(
		raw,
		"MongoDB document lock",
		"_id", "codec", "collection", "document", "ownerCollection", "owner", "ownerLabel", "createdAt", "updatedAt", "expiresAt",
	); err != nil {
		return store.DocumentLock{}, err
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != 1 {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an unsupported codec version")
	}
	collectionID, ok := raw.Lookup("collection").StringValueOK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid collection")
	}
	documentID, ok := raw.Lookup("document").StringValueOK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid document")
	}
	ownerCollectionID, ok := raw.Lookup("ownerCollection").StringValueOK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid owner collection")
	}
	ownerID, ok := raw.Lookup("owner").StringValueOK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid owner")
	}
	ownerLabel, ok := raw.Lookup("ownerLabel").StringValueOK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid owner label")
	}
	lock := store.DocumentLock{
		CollectionID: schema.StableID(collectionID), DocumentID: documentID,
		OwnerCollectionID: schema.StableID(ownerCollectionID), OwnerID: ownerID, OwnerLabel: ownerLabel,
	}
	if err := validateMongoDocumentLock(lock); err != nil {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock identity: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoDocumentLockID(lock) {
		return store.DocumentLock{}, mongoSystemIdentityCollision("document lock")
	}
	createdAt, ok := raw.Lookup("createdAt").Int64OK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid createdAt")
	}
	updatedAt, ok := raw.Lookup("updatedAt").Int64OK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid updatedAt")
	}
	expiresAt, ok := raw.Lookup("expiresAt").Int64OK()
	if !ok {
		return store.DocumentLock{}, fmt.Errorf("stored MongoDB document lock has an invalid expiresAt")
	}
	lock.CreatedAt = decodeTime(createdAt)
	lock.UpdatedAt = decodeTime(updatedAt)
	lock.ExpiresAt = decodeTime(expiresAt)
	return lock, nil
}

func sameMongoDocumentLockTarget(left, right store.DocumentLock) bool {
	return left.CollectionID == right.CollectionID && left.DocumentID == right.DocumentID
}

func (backend *Store) documentLockCollection() *mongo.Collection {
	return backend.database.Collection(mongoDocumentLockCollectionName)
}

func (transaction *documentTransaction) documentLockCollection() *mongo.Collection {
	return transaction.store.documentLockCollection()
}

func (transaction *documentTransaction) deleteDocumentLockState(ctx context.Context, reference store.DocumentReference) error {
	_, err := transaction.documentLockCollection().DeleteMany(ctx, bson.D{{Key: "$or", Value: bson.A{
		bson.D{
			{Key: "collection", Value: string(reference.CollectionID)},
			{Key: "document", Value: reference.DocumentID},
		},
		bson.D{
			{Key: "ownerCollection", Value: string(reference.CollectionID)},
			{Key: "owner", Value: reference.DocumentID},
		},
	}}})
	return translateMongoError(ctx, err)
}

var _ store.DocumentLockStore = (*Store)(nil)
