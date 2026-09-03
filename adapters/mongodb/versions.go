package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	mongoVersionOwnerPath     = "owner"
	mongoVersionRevisionPath  = "revision"
	mongoVersionStatusPath    = "status"
	mongoVersionCreatedAtPath = "createdAt"
	mongoVersionSnapshotPath  = "snapshot"
)

func versionID(documentID string, revision int) string {
	return fmt.Sprintf("%s:%d", documentID, revision)
}

func (transaction *documentTransaction) versionCollection(collection schema.Collection) *mongo.Collection {
	return transaction.store.database.Collection(physicalVersionCollectionName(collection.ID))
}

// SaveVersion stores one canonical document snapshot in the caller's open
// transaction. Re-saving the same revision replaces its snapshot while
// preserving the original version timestamp, matching the store contract used
// by the other official adapters.
func (transaction *documentTransaction) SaveVersion(ctx context.Context, collection schema.Collection, document store.Document, maximum int) (store.Version, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Version{}, err
	}
	defer leave()
	if err := validateCollectionEnvelope(collection); err != nil {
		return store.Version{}, err
	}
	if err := transaction.store.requireVerifiedIndexes(collection); err != nil {
		return store.Version{}, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(collection); err != nil {
		return store.Version{}, err
	}
	if collection.Versions == nil {
		return store.Version{}, fmt.Errorf("MongoDB versions require a version-enabled collection")
	}
	if maximum < 0 {
		return store.Version{}, fmt.Errorf("MongoDB version retention cannot be negative")
	}
	if err := store.ValidateDocumentID(document.ID); err != nil {
		return store.Version{}, err
	}
	if err := validateMongoVersionMetadata(collection, document.Status, document.Revision); err != nil {
		return store.Version{}, err
	}
	if err := validateCompleteValues(collection, document.Values); err != nil {
		return store.Version{}, err
	}
	snapshot, err := encodeDocument(document)
	if err != nil {
		return store.Version{}, fmt.Errorf("encode MongoDB version snapshot: %w", err)
	}
	createdAt, err := encodeTime(transaction.store.now().UTC())
	if err != nil {
		return store.Version{}, fmt.Errorf("encode MongoDB version timestamp: %w", err)
	}
	id := versionID(document.ID, document.Revision)
	filter := bson.D{
		{Key: "_id", Value: id},
		{Key: mongoVersionOwnerPath, Value: document.ID},
		{Key: mongoVersionRevisionPath, Value: int64(document.Revision)},
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: mongoVersionStatusPath, Value: string(document.Status)},
			{Key: mongoVersionSnapshotPath, Value: snapshot},
		}},
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "_id", Value: id},
			{Key: mongoVersionOwnerPath, Value: document.ID},
			{Key: mongoVersionRevisionPath, Value: int64(document.Revision)},
			{Key: mongoVersionCreatedAtPath, Value: createdAt},
		}},
	}
	raw, err := transaction.versionCollection(collection).FindOneAndUpdate(
		sessionContext,
		filter,
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Raw()
	if err != nil {
		return store.Version{}, translateMongoError(ctx, err)
	}
	version, err := decodeMongoVersion(raw, collection)
	if err != nil {
		return store.Version{}, err
	}
	if maximum > 0 {
		if err := transaction.pruneVersions(ctx, sessionContext, collection, document.ID, maximum); err != nil {
			return store.Version{}, err
		}
	}
	return cloneMongoVersion(version), nil
}

func (transaction *documentTransaction) pruneVersions(ctx, sessionContext context.Context, collection schema.Collection, documentID string, maximum int) error {
	filter := bson.D{{Key: mongoVersionOwnerPath, Value: documentID}}
	findOptions := options.Find().
		SetSort(bson.D{{Key: mongoVersionRevisionPath, Value: -1}}).
		SetLimit(int64(maximum + 1))
	cursor, err := transaction.versionCollection(collection).Find(sessionContext, filter, findOptions)
	if err != nil {
		return translateMongoError(ctx, err)
	}
	candidates := make([]store.Version, 0, maximum+1)
	for cursor.Next(sessionContext) {
		version, decodeErr := decodeMongoVersion(cursor.Current, collection)
		if decodeErr != nil {
			_ = cursor.Close(sessionContext)
			return fmt.Errorf("validate MongoDB version retention candidate: %w", decodeErr)
		}
		if version.DocumentID != documentID {
			_ = cursor.Close(sessionContext)
			return fmt.Errorf("stored MongoDB version retention candidate belongs to an unexpected document")
		}
		candidates = append(candidates, version)
	}
	if err := cursor.Err(); err != nil {
		_ = cursor.Close(sessionContext)
		return translateMongoError(ctx, err)
	}
	if err := cursor.Close(sessionContext); err != nil {
		return translateMongoError(ctx, err)
	}
	if len(candidates) <= maximum {
		return nil
	}
	cutoff := candidates[maximum].Revision
	_, err = transaction.versionCollection(collection).DeleteMany(sessionContext, bson.D{
		{Key: mongoVersionOwnerPath, Value: documentID},
		{Key: mongoVersionRevisionPath, Value: bson.D{{Key: "$lte", Value: int64(cutoff)}}},
	})
	return translateMongoError(ctx, err)
}

// ListVersions returns newest-first retained snapshots and applies Access to
// the stored snapshot inside the MongoDB query, never to only the current row.
func (transaction *documentTransaction) ListVersions(ctx context.Context, request store.VersionRequest) ([]store.Version, error) {
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	defer leave()
	if err := validateVersionRequest(request); err != nil {
		return nil, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return nil, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(request.Collection); err != nil {
		return nil, err
	}
	predicates := []bson.D{{{Key: mongoVersionOwnerPath, Value: request.DocumentID}}}
	if request.Access != nil {
		compiled, err := compileMongoAccessNode(
			request.Collection,
			*request.Access,
			"version access",
			mongoPredicateScope{
				localeChain:   request.LocaleChain,
				storagePrefix: mongoVersionSnapshotPath + ".",
			},
			request.AllLocales,
			request.Locales,
		)
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, compiled)
	}
	cursor, err := transaction.versionCollection(request.Collection).Find(
		sessionContext,
		mongoAnd(predicates),
		options.Find().SetSort(bson.D{{Key: mongoVersionRevisionPath, Value: -1}}),
	)
	if err != nil {
		return nil, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)
	versions := make([]store.Version, 0)
	for cursor.Next(sessionContext) {
		version, err := decodeMongoVersionForLocales(cursor.Current, request.Collection, request.Locales)
		if err != nil {
			return nil, err
		}
		if version.DocumentID != request.DocumentID {
			return nil, fmt.Errorf("stored MongoDB version belongs to an unexpected document")
		}
		versions = append(versions, cloneMongoVersion(version))
	}
	if err := cursor.Err(); err != nil {
		return nil, translateMongoError(ctx, err)
	}
	return versions, nil
}

func validateVersionRequest(request store.VersionRequest) error {
	if err := validateCollectionEnvelope(request.Collection); err != nil {
		return err
	}
	if request.Collection.Versions == nil {
		return fmt.Errorf("MongoDB versions require a version-enabled collection")
	}
	if err := store.ValidateDocumentID(request.DocumentID); err != nil {
		return err
	}
	return nil
}

// FindVersion resolves one exact retained revision. Operation-engine version
// reads use ListVersions so their access predicate remains server-side.
func (transaction *documentTransaction) FindVersion(ctx context.Context, collection schema.Collection, documentID string, revision int) (store.Version, error) {
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Version{}, err
	}
	defer leave()
	if err := validateCollectionEnvelope(collection); err != nil {
		return store.Version{}, err
	}
	if err := transaction.store.requireVerifiedIndexes(collection); err != nil {
		return store.Version{}, err
	}
	if err := transaction.store.requireVerifiedVersionIndexes(collection); err != nil {
		return store.Version{}, err
	}
	if collection.Versions == nil {
		return store.Version{}, fmt.Errorf("MongoDB versions require a version-enabled collection")
	}
	if err := store.ValidateDocumentID(documentID); err != nil {
		return store.Version{}, err
	}
	if revision < 1 {
		return store.Version{}, fmt.Errorf("MongoDB version revision must be positive")
	}
	raw, err := transaction.versionCollection(collection).FindOne(sessionContext, bson.D{
		{Key: "_id", Value: versionID(documentID, revision)},
		{Key: mongoVersionOwnerPath, Value: documentID},
		{Key: mongoVersionRevisionPath, Value: int64(revision)},
	}).Raw()
	if err != nil {
		return store.Version{}, translateMongoError(ctx, err)
	}
	version, err := decodeMongoVersion(raw, collection)
	if err != nil {
		return store.Version{}, err
	}
	return cloneMongoVersion(version), nil
}

func decodeMongoVersion(raw bson.Raw, collection schema.Collection) (store.Version, error) {
	if err := requireExactKeys(raw, "MongoDB version", "_id", mongoVersionOwnerPath, mongoVersionRevisionPath, mongoVersionStatusPath, mongoVersionCreatedAtPath, mongoVersionSnapshotPath); err != nil {
		return store.Version{}, err
	}
	id, valid := raw.Lookup("_id").StringValueOK()
	if !valid || stringsContainNUL(id) {
		return store.Version{}, fmt.Errorf("stored MongoDB version has an invalid ID")
	}
	documentID, valid := raw.Lookup(mongoVersionOwnerPath).StringValueOK()
	if !valid {
		return store.Version{}, fmt.Errorf("stored MongoDB version has an invalid document ID")
	}
	if err := store.ValidateDocumentID(documentID); err != nil {
		return store.Version{}, fmt.Errorf("stored MongoDB version document ID: %w", err)
	}
	revision64, valid := raw.Lookup(mongoVersionRevisionPath).Int64OK()
	if !valid || revision64 < 1 || int64(int(revision64)) != revision64 {
		return store.Version{}, fmt.Errorf("stored MongoDB version has an invalid revision")
	}
	revision := int(revision64)
	if id != versionID(documentID, revision) {
		return store.Version{}, fmt.Errorf("stored MongoDB version ID does not match its document and revision")
	}
	statusText, valid := raw.Lookup(mongoVersionStatusPath).StringValueOK()
	if !valid {
		return store.Version{}, fmt.Errorf("stored MongoDB version has an invalid status")
	}
	status := store.Status(statusText)
	if err := validateMongoVersionMetadata(collection, status, revision); err != nil {
		return store.Version{}, fmt.Errorf("stored MongoDB version metadata: %w", err)
	}
	createdAtNanos, valid := raw.Lookup(mongoVersionCreatedAtPath).Int64OK()
	if !valid {
		return store.Version{}, fmt.Errorf("stored MongoDB version has an invalid createdAt")
	}
	snapshotRaw, valid := raw.Lookup(mongoVersionSnapshotPath).DocumentOK()
	if !valid {
		return store.Version{}, fmt.Errorf("stored MongoDB version has an invalid snapshot")
	}
	snapshot, err := decodeCollectionDocument(snapshotRaw, collection)
	if err != nil {
		return store.Version{}, fmt.Errorf("decode stored MongoDB version snapshot: %w", err)
	}
	if snapshot.ID != documentID || snapshot.Revision != revision || snapshot.Status != status {
		return store.Version{}, fmt.Errorf("stored MongoDB version metadata does not match its snapshot")
	}
	return store.Version{
		ID: id, DocumentID: documentID, Revision: revision, Status: status,
		Snapshot: snapshot, CreatedAt: decodeTime(createdAtNanos),
	}, nil
}

func decodeMongoVersionForLocales(raw bson.Raw, collection schema.Collection, locales []schema.LocaleCode) (store.Version, error) {
	version, err := decodeMongoVersion(raw, collection)
	if err != nil {
		return store.Version{}, err
	}
	if len(locales) == 0 {
		return version, nil
	}
	if err := validateCompleteValuesForLocales(collection, version.Snapshot.Values, locales); err != nil {
		return store.Version{}, fmt.Errorf("stored MongoDB version snapshot does not match configured locales for collection %q: %w", collection.ID, err)
	}
	return version, nil
}

func cloneMongoVersion(version store.Version) store.Version {
	version.Snapshot = store.CloneDocument(version.Snapshot)
	return version
}

var _ store.VersionTransaction = (*documentTransaction)(nil)
