package mongodb

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ListWindow performs one count-free, offset-free range read over the direct
// unique text index admitted by store.ValidateListWindowRequest. The unique
// key is the complete ordering, so no secondary sort term is added.
func (transaction *documentTransaction) ListWindow(ctx context.Context, request store.Request) (store.Window, error) {
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Window{}, err
	}
	defer leave()

	path, indexName, predicate, err := mongoListWindowPredicate(request)
	if err != nil {
		return store.Window{}, err
	}
	if err := transaction.store.requireVerifiedIndexesForLocales(request.Collection, request.Locales); err != nil {
		return store.Window{}, err
	}

	cursor, err := transaction.collection(request.Collection).Find(
		sessionContext,
		predicate,
		options.Find().
			SetSort(bson.D{{Key: path, Value: mongoAscendingDirection}}).
			SetHint(indexName).
			SetLimit(int64(request.Limit+1)),
	)
	if err != nil {
		return store.Window{}, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)

	documents := make([]store.Document, 0, request.Limit+1)
	for cursor.Next(sessionContext) {
		document, decodeErr := decodeCollectionDocumentForLocales(cursor.Current, request.Collection, request.Locales)
		if decodeErr != nil {
			return store.Window{}, decodeErr
		}
		documents = append(documents, document)
	}
	if err := cursor.Err(); err != nil {
		return store.Window{}, translateMongoError(ctx, err)
	}

	hasMore := len(documents) > request.Limit
	if hasMore {
		documents = documents[:request.Limit]
	}
	for index := range documents {
		documents[index] = projectDocument(documents[index], request.Select)
	}
	return store.Window{Documents: documents, HasMore: hasMore}, nil
}

func mongoListWindowPredicate(request store.Request) (string, string, bson.D, error) {
	if err := store.ValidateListWindowRequest(request); err != nil {
		return "", "", nil, err
	}
	if err := validateCollectionEnvelope(request.Collection); err != nil {
		return "", "", nil, err
	}
	if _, err := requestProjection(request); err != nil {
		return "", "", nil, err
	}
	resolved, err := resolveMongoPredicatePath(request.Collection, request.IndexWindow.Path, "list window", mongoPredicateScope{})
	if err != nil {
		return "", "", nil, err
	}
	if resolved.kind != mongoStringScalar || len(resolved.objectAncestors) != 0 {
		return "", "", nil, fmt.Errorf("MongoDB list window path %q is not a direct text index", request.IndexWindow.Path.String())
	}
	field, found := mongoFieldNamed(request.Collection.Fields, request.IndexWindow.Path.Segments()[0])
	if !found {
		return "", "", nil, fmt.Errorf("MongoDB list window field %q is unavailable", request.IndexWindow.Path.String())
	}
	indexName := mongoDeclaredIndexName(request.Collection.ID, "field:"+string(field.ID), true)
	predicate := mongoAnd([]bson.D{
		// These type predicates intentionally match the hinted unique index's
		// partial membership exactly. If an out-of-band array reaches the range,
		// strict document decoding fails closed instead of turning the range into
		// an unbounded FETCH filter over otherwise qualifying index keys.
		mongoIndexTypePredicate(mongoDeletedAtPath, "null"),
		mongoIndexTypePredicate(resolved.storagePath, "string"),
		{{Key: resolved.storagePath, Value: bson.D{
			{Key: "$gte", Value: request.IndexWindow.LowerBound},
			{Key: "$lt", Value: request.IndexWindow.UpperBound},
		}}},
	})
	return resolved.storagePath, indexName, predicate, nil
}

var _ store.WindowTransaction = (*documentTransaction)(nil)
