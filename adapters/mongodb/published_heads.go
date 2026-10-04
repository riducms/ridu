package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Rebuild reservations from both active heads using the destination manifest.
// It is deterministic and idempotent, so an interrupted no-transaction
// maintenance step can resume before a new unique index is admitted.
func (backend *Store) rebuildMongoHeadReservations(ctx context.Context, manifest schema.Manifest) error {
	snapshot := manifest.Snapshot()
	var locales []schema.LocaleCode
	if snapshot.Application.Localization != nil {
		locales = snapshot.Application.Localization.LocaleCodes()
	}
	transaction := &documentTransaction{store: backend}
	for _, collection := range append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...) {
		if collection.Versions == nil {
			continue
		}
		working := backend.database.Collection(physicalCollectionName(collection.ID))
		all, err := working.Find(ctx, bson.D{})
		if err != nil {
			return translateMongoError(ctx, err)
		}
		for all.Next(ctx) {
			document, err := decodeCollectionDocument(all.Current, collection)
			if err != nil {
				_ = all.Close(ctx)
				return err
			}
			if err := transaction.replaceHeadReservationsForLocales(ctx, collection, document, locales); err != nil {
				_ = all.Close(ctx)
				return err
			}
		}
		if err := all.Err(); err != nil {
			_ = all.Close(ctx)
			return translateMongoError(ctx, err)
		}
		if err := all.Close(ctx); err != nil {
			return translateMongoError(ctx, err)
		}
	}
	return nil
}

func (backend *Store) verifyMongoPublishedHeadCoverage(ctx context.Context, collection schema.Collection) error {
	working := backend.database.Collection(physicalCollectionName(collection.ID))
	publishedName := physicalPublishedCollectionName(collection.ID)
	published := backend.database.Collection(publishedName)
	workingCount, err := working.CountDocuments(ctx, bson.D{{Key: mongoStatusPath, Value: string(store.StatusPublished)}})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	liveCount, err := published.CountDocuments(ctx, bson.D{{Key: mongoStatusPath, Value: string(store.StatusPublished)}})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	allLiveCount, err := published.CountDocuments(ctx, bson.D{})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if workingCount != liveCount || allLiveCount != liveCount {
		return fmt.Errorf("MongoDB collection %q has incomplete published-head coverage; repair corrupted physical state", collection.ID)
	}
	if workingCount == 0 {
		return nil
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: mongoStatusPath, Value: string(store.StatusPublished)}}}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: publishedName},
			{Key: "localField", Value: "_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "riduLive"},
		}}},
		bson.D{{Key: "$match", Value: bson.D{{Key: "riduLive", Value: bson.D{{Key: "$size", Value: 0}}}}}},
		bson.D{{Key: "$limit", Value: int64(1)}},
	}
	cursor, err := working.Aggregate(ctx, pipeline)
	if err != nil {
		return translateMongoError(ctx, err)
	}
	defer cursor.Close(ctx)
	if cursor.Next(ctx) {
		return fmt.Errorf("MongoDB collection %q has a published document without its live snapshot", collection.ID)
	}
	return translateMongoError(ctx, cursor.Err())
}
