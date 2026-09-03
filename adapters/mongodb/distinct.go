package mongodb

import (
	"context"
	"fmt"
	"math"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Distinct returns one stable, access-filtered page of unique direct scalar
// values. The two aggregate commands share the transaction snapshot, so the
// exact total and selected page cannot observe different committed states.
func (transaction *documentTransaction) Distinct(ctx context.Context, request store.DistinctRequest) (store.DistinctPage, error) {
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.DistinctPage{}, err
	}
	defer leave()
	if err := store.ValidateDistinctRequest(request); err != nil {
		return store.DistinctPage{}, err
	}
	documentRequest := store.Request{
		Collection: request.Collection, Filter: request.Filter, Access: request.Access,
		PublishedOnly: request.PublishedOnly, Deletion: request.Deletion,
		Locales: request.Locales, LocaleChain: request.LocaleChain,
	}
	if err := validateRequestEnvelope(documentRequest); err != nil {
		return store.DistinctPage{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.DistinctPage{}, err
	}
	predicate, err := decoderFreeRequestPredicate(documentRequest, false)
	if err != nil {
		return store.DistinctPage{}, err
	}
	resolved, err := resolveMongoPredicatePath(
		request.Collection,
		request.Field,
		"distinct",
		mongoPredicateScope{localeChain: request.LocaleChain},
	)
	if err != nil {
		return store.DistinctPage{}, err
	}
	groupValue := any("$" + resolved.storagePath)
	if len(resolved.localePaths) != 0 {
		groupValue = mongoLocalizedValueExpression(resolved)
	}
	group := bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: groupValue}}}}
	collection := transaction.collection(request.Collection)
	basePipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: predicate}},
		group,
	}
	total, err := transaction.distinctTotal(ctx, sessionContext, collection, basePipeline)
	if err != nil {
		return store.DistinctPage{}, err
	}
	page, limit, start, end := store.ListPageBounds(request.Page, request.Limit, total)
	result := store.DistinctPage{Values: []store.Value{}, Page: page, Limit: limit, Total: total}
	if start == end {
		return result, nil
	}
	pipeline := append(mongo.Pipeline(nil), basePipeline...)
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: int32(1)}}}},
		bson.D{{Key: "$skip", Value: int64(start)}},
		bson.D{{Key: "$limit", Value: int64(end - start)}},
	)
	cursor, err := collection.Aggregate(sessionContext, pipeline)
	if err != nil {
		return store.DistinctPage{}, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)
	result.Values = make([]store.Value, 0, end-start)
	for cursor.Next(sessionContext) {
		if err := requireExactKeys(cursor.Current, "MongoDB distinct result", "_id"); err != nil {
			return store.DistinctPage{}, err
		}
		value, err := decodeValue(cursor.Current.Lookup("_id"))
		if err != nil {
			return store.DistinctPage{}, fmt.Errorf("stored MongoDB distinct value: %w", err)
		}
		if !mongoDistinctKindMatches(resolved.kind, value) {
			return store.DistinctPage{}, fmt.Errorf("stored MongoDB distinct value does not match the collection field type")
		}
		result.Values = append(result.Values, value)
	}
	if err := cursor.Err(); err != nil {
		return store.DistinctPage{}, translateMongoError(ctx, err)
	}
	return result, nil
}

func (transaction *documentTransaction) distinctTotal(callerContext, sessionContext context.Context, collection *mongo.Collection, base mongo.Pipeline) (int, error) {
	pipeline := append(mongo.Pipeline(nil), base...)
	pipeline = append(pipeline, bson.D{{Key: "$count", Value: "total"}})
	cursor, err := collection.Aggregate(sessionContext, pipeline)
	if err != nil {
		return 0, translateMongoError(callerContext, err)
	}
	defer transaction.closeCursor(cursor)
	if !cursor.Next(sessionContext) {
		if err := cursor.Err(); err != nil {
			return 0, translateMongoError(callerContext, err)
		}
		return 0, nil
	}
	if err := requireExactKeys(cursor.Current, "MongoDB distinct total", "total"); err != nil {
		return 0, err
	}
	total, ok := cursor.Current.Lookup("total").AsInt64OK()
	if !ok || total < 0 || total > int64(math.MaxInt) {
		return 0, fmt.Errorf("stored MongoDB distinct total is outside the platform integer range")
	}
	if cursor.Next(sessionContext) {
		return 0, fmt.Errorf("MongoDB distinct total returned more than one result")
	}
	if err := cursor.Err(); err != nil {
		return 0, translateMongoError(callerContext, err)
	}
	return int(total), nil
}

func mongoDistinctKindMatches(kind mongoScalarKind, value store.Value) bool {
	if value.Kind() == store.ValueNull {
		return true
	}
	switch kind {
	case mongoStringScalar:
		return value.Kind() == store.ValueString
	case mongoNumberScalar, mongoIntegerScalar, mongoTimestampScalar:
		return value.Kind() == store.ValueNumber
	case mongoBooleanScalar:
		return value.Kind() == store.ValueBoolean
	default:
		return false
	}
}

var _ store.DistinctTransaction = (*documentTransaction)(nil)
