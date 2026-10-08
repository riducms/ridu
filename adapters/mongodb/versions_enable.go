package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/enableversions"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ReviewDevelopmentVersions counts the documents, trashed ones included, that
// each resource starting to keep versions between before and after already
// stores in this database. It changes nothing.
func (backend *Store) ReviewDevelopmentVersions(ctx context.Context, before, after schema.Manifest) ([]enableversions.Report, error) {
	if err := backend.prepareIndexOperation(ctx); err != nil {
		return nil, err
	}
	enabled := ridumigration.VersionsEnabled(before.Snapshot(), after.Snapshot(), nil)
	reports := make([]enableversions.Report, 0, len(enabled))
	for _, resource := range enabled {
		documents, err := backend.countMongoStoredDocuments(ctx, resource.ID)
		if err != nil {
			return nil, err
		}
		reports = append(reports, enableversions.Report{Resource: resource, Documents: documents})
	}
	return reports, nil
}

// requireMongoDevelopmentVersionsEmpty refuses development synchronization
// that would enable versions on a resource which already stores documents;
// only a migration records what they become.
func (backend *Store) requireMongoDevelopmentVersionsEmpty(ctx context.Context, enabling []schema.Collection) error {
	for _, resource := range enabling {
		documents, err := backend.countMongoStoredDocuments(ctx, resource.ID)
		if err != nil {
			return err
		}
		if documents != 0 {
			return enableversions.StoredDocumentsError(resource, documents)
		}
	}
	return nil
}

// countMongoStoredDocuments counts the working documents of a resource,
// trashed ones included. A collection MongoDB has not created stores none.
func (backend *Store) countMongoStoredDocuments(ctx context.Context, resource schema.StableID) (int64, error) {
	documents, err := backend.database.Collection(physicalCollectionName(resource)).CountDocuments(ctx, bson.D{})
	return documents, translateMongoError(ctx, err)
}

// enableMongoVersions turns the documents resource stores into versioned
// documents inside the migration's transaction, as existing says. Raw updates
// set only a working document's status and revision, so it keeps its
// incarnation, fence and values, and nothing decodes it under the versioned
// schema before it fits. A published document gets a live head and every
// document a first version holding the snapshot SaveVersion would store.
// References already cover the one head, and the artifact rebuilds unique
// reservations after this phase.
func (backend *Store) enableMongoVersions(ctx context.Context, transaction *documentTransaction, resource schema.Collection, existing ridumigration.ExistingDocuments) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	documents, err := backend.countMongoStoredDocuments(sessionContext, resource.ID)
	if err != nil || documents == 0 {
		return err
	}
	if existing == ridumigration.ExistingRequireEmpty {
		return enableversions.NotEmptyError(resource, documents)
	}
	if err := enableversions.Validate(resource, existing); err != nil {
		return err
	}
	working := backend.database.Collection(physicalCollectionName(resource.ID))
	published := backend.database.Collection(physicalPublishedCollectionName(resource.ID))
	versions := backend.database.Collection(physicalVersionCollectionName(resource.ID))
	cursor, err := working.Find(sessionContext, bson.D{}, options.Find().SetSort(bson.D{{Key: mongoIDPath, Value: mongoAscendingDirection}}))
	if err != nil {
		return translateMongoError(ctx, err)
	}
	defer cursor.Close(sessionContext)
	for cursor.Next(sessionContext) {
		stored, err := decodeDocument(cursor.Current)
		if err != nil {
			return err
		}
		if stored.Status != "" {
			return fmt.Errorf("stored MongoDB document %q of %s already has version metadata", stored.ID, enableversions.Describe(resource))
		}
		document := enableversions.Convert(resource, stored, existing)
		result, err := working.UpdateOne(sessionContext, bson.D{
			{Key: mongoIDPath, Value: document.ID},
			{Key: mongoStatusPath, Value: ""},
			{Key: mongoRevisionPath, Value: int64(stored.Revision)},
		}, bson.D{{Key: "$set", Value: bson.D{
			{Key: mongoStatusPath, Value: string(document.Status)},
			{Key: mongoRevisionPath, Value: int64(document.Revision)},
		}}})
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if result.MatchedCount != 1 {
			return fmt.Errorf("stored MongoDB document %q changed while versions were enabled: %w", document.ID, store.ErrConflict)
		}
		// The live head and the version snapshot are the document as an
		// ordinary save encodes it, with no pending draft changes.
		snapshot, err := encodeDocument(document)
		if err != nil {
			return fmt.Errorf("encode MongoDB document %q: %w", document.ID, err)
		}
		if document.Status == store.StatusPublished {
			if _, err := published.InsertOne(sessionContext, snapshot); err != nil {
				return translateMongoError(ctx, err)
			}
		}
		createdAt, err := encodeTime(document.UpdatedAt)
		if err != nil {
			return fmt.Errorf("encode MongoDB version timestamp: %w", err)
		}
		if _, err := versions.InsertOne(sessionContext, bson.D{
			{Key: "_id", Value: versionID(document.ID, document.Revision)},
			{Key: mongoVersionOwnerPath, Value: document.ID},
			{Key: mongoVersionRevisionPath, Value: int64(document.Revision)},
			{Key: mongoVersionStatusPath, Value: string(document.Status)},
			{Key: mongoVersionCreatedAtPath, Value: createdAt},
			{Key: mongoVersionSnapshotPath, Value: snapshot},
		}); err != nil {
			return translateMongoError(ctx, err)
		}
	}
	return translateMongoError(ctx, cursor.Err())
}
