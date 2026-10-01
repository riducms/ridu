package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// HasMigrationHistory includes partial semantic work, which development
// recovery must never mutate outside the immutable runner.
func (backend *Store) HasMigrationHistory(ctx context.Context) (bool, error) {
	for _, name := range []string{mongoMigrationArtifactCollectionName, mongoMigrationStepCollectionName} {
		count, err := backend.mongoMigrationCollection(name).CountDocuments(ctx, bson.D{})
		if err != nil {
			return false, translateMongoError(ctx, err)
		}
		if count != 0 {
			return true, nil
		}
	}
	return false, nil
}

// ReviewDevelopmentFieldKinds reads every current document and snapshot under
// one stable database snapshot, without projecting through the candidate schema.
func (backend *Store) ReviewDevelopmentFieldKinds(ctx context.Context, before, after schema.Manifest) ([]fieldchange.Report, error) {
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	reports := fieldchange.Reports(changes)
	transaction, err := backend.begin(ctx, true)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback(ctx)
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	err = backend.scanMongoFieldKinds(sessionContext, transaction, changes, reports, false)
	leave()
	return reports, err
}

// ClearDevelopmentFieldKinds clears only the reviewed field values from live
// documents and complete retained snapshots, with references in one transaction.
// Immutable applied or incomplete migration history requires the reviewed runner.
func (backend *Store) ClearDevelopmentFieldKinds(ctx context.Context, before, after schema.Manifest, expected []fieldchange.Report) error {
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	if err := fieldchange.ValidateClear(changes); err != nil {
		return err
	}
	normalized, err := normalizeMongoMigrationRunnerOptions(RunnerOptions{})
	if err != nil {
		return err
	}
	return backend.withMongoMigrationLease(ctx, normalized, func(runContext context.Context, lease *mongoMigrationLease) error {
		return lease.transaction(runContext, func(sessionContext context.Context) error {
			// This helper owns no session; every raw write uses the fenced session
			// context provided by lease.transaction, including reference writes.
			transaction := &documentTransaction{store: backend}
			managed, err := backend.HasMigrationHistory(sessionContext)
			if err != nil {
				return err
			}
			if managed {
				return fmt.Errorf("this MongoDB database is managed by ridu migrate; field-kind recovery requires a reviewed migration")
			}
			reports := fieldchange.Reports(changes)
			if err := backend.scanMongoFieldKinds(sessionContext, transaction, changes, reports, false); err != nil {
				return err
			}
			if err := fieldchange.ConfirmCounts(expected, reports); err != nil {
				return err
			}
			reports = fieldchange.Reports(changes)
			if err := backend.scanMongoFieldKinds(sessionContext, transaction, changes, reports, true); err != nil {
				return err
			}
			// lease.transaction pauses its heartbeat while holding the fence.
			// Reject an expired owner before committing the content as well.
			return lease.refreshUnlocked(sessionContext, backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName))
		})
	})
}

func (backend *Store) scanMongoFieldKinds(ctx context.Context, transaction *documentTransaction, changes []fieldchange.Change, reports []fieldchange.Report, clear bool) error {
	for _, resource := range fieldchange.AffectedResources(changes) {
		for _, snapshot := range []bool{false, true} {
			name := physicalCollectionName(resource.ID)
			if snapshot {
				name = physicalVersionCollectionName(resource.ID)
			}
			collection := backend.database.Collection(name)
			if err := scanMongoFieldKindCollection(ctx, transaction, collection, resource, changes, reports, snapshot, clear); err != nil {
				return err
			}
		}
	}
	return nil
}

func scanMongoFieldKindCollection(ctx context.Context, transaction *documentTransaction, collection *mongo.Collection, resource schema.Collection, changes []fieldchange.Change, reports []fieldchange.Report, snapshot, clear bool) error {
	cursor, err := collection.Find(ctx, bson.D{})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		raw := cursor.Current
		documentRaw := raw
		prefix := ""
		if snapshot {
			var ok bool
			documentRaw, ok = raw.Lookup(mongoVersionSnapshotPath).DocumentOK()
			if !ok {
				return fmt.Errorf("stored MongoDB version has an invalid snapshot")
			}
			prefix = mongoVersionSnapshotPath + "."
		}
		document, err := decodeDocument(documentRaw)
		if err != nil {
			return err
		}
		values, found, err := fieldchange.Process(changes, reports, resource.ID, document.Values, snapshot, clear)
		if err != nil {
			return err
		}
		if !clear || !found {
			continue
		}
		encoded, err := encodeValues(values)
		if err != nil {
			return err
		}
		id, ok := raw.Lookup("_id").StringValueOK()
		if !ok {
			return fmt.Errorf("stored field-kind recovery document has an invalid identity")
		}
		if _, err := collection.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}}, bson.D{{Key: "$set", Value: bson.D{{Key: prefix + "values", Value: encoded}}}}); err != nil {
			return translateMongoError(ctx, err)
		}
		if !snapshot {
			document.Values = values
			if err := transaction.replaceDocumentReferences(ctx, resource, document); err != nil {
				return err
			}
		}
	}
	return translateMongoError(ctx, cursor.Err())
}
