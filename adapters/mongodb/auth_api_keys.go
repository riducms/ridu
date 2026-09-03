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

func findMongoAuthAPIKeyByID(ctx context.Context, collection *mongo.Collection, id string) (mongoAuthAPIKey, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoAuthAPIKeyID(id)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthAPIKey{}, false, nil
	}
	if err != nil {
		return mongoAuthAPIKey{}, false, translateMongoError(ctx, err)
	}
	key, err := decodeMongoAuthAPIKey(raw)
	if err != nil {
		return mongoAuthAPIKey{}, false, err
	}
	if key.Key.ID != id {
		return mongoAuthAPIKey{}, false, mongoSystemIdentityCollision("auth API key")
	}
	return key, true, nil
}

func findMongoAuthAPIKeyByToken(ctx context.Context, collection *mongo.Collection, tokenHash string) (mongoAuthAPIKey, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "tokenHash", Value: tokenHash}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthAPIKey{}, false, nil
	}
	if err != nil {
		return mongoAuthAPIKey{}, false, translateMongoError(ctx, err)
	}
	key, err := decodeMongoAuthAPIKey(raw)
	if err != nil {
		return mongoAuthAPIKey{}, false, err
	}
	if key.Key.TokenHash != tokenHash {
		return mongoAuthAPIKey{}, false, mongoSystemIdentityCollision("auth API key")
	}
	return key, true, nil
}

func (backend *Store) CreateAPIKey(ctx context.Context, key store.AuthAPIKey, sessionTokenHash string, now time.Time) error {
	if err := backend.prepareAuthOperation(ctx, "authentication API-key creation"); err != nil {
		return err
	}
	key.LastUsedAt = time.Time{}
	if err := validateMongoAuthAPIKey(mongoAuthAPIKey{Key: key, UserIncarnation: zeroMongoDocumentIncarnation}); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(sessionTokenHash, "authorizing auth session token digest"); err != nil {
		return err
	}
	now, _, err := normalizeMongoSystemTime(now, "authentication API-key timestamp")
	if err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: key.CollectionID, DocumentID: key.UserID}
	references, err := backend.captureActiveDocumentReferences(ctx, reference)
	if err != nil {
		return err
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
			return err
		}
		incarnation, _ := mongoCapturedAuthIncarnation(references, reference)
		if _, err := transaction.requireMongoAuthCredentialIncarnation(sessionContext, key.CollectionID, key.UserID, incarnation); err != nil {
			return err
		}
		session, found, err := findMongoAuthSessionByToken(sessionContext, transaction.authSessionCollection(), sessionTokenHash)
		if err != nil {
			return err
		}
		if !found || session.UserIncarnation != incarnation || session.Session.CollectionID != key.CollectionID ||
			session.Session.UserID != key.UserID || !session.Session.ExpiresAt.After(now) {
			return store.ErrNotFound
		}
		expiresAt, _ := encodeTime(session.Session.ExpiresAt)
		raw, err := transaction.authSessionCollection().FindOneAndUpdate(
			sessionContext,
			bson.D{
				{Key: "_id", Value: session.Session.TokenHash},
				{Key: "tokenHash", Value: session.Session.TokenHash},
				{Key: "collection", Value: string(key.CollectionID)},
				{Key: "user", Value: key.UserID},
				{Key: "userIncarnation", Value: incarnation},
				{Key: "expiresAt", Value: expiresAt},
				{Key: "fence", Value: session.Fence},
			},
			bson.D{{Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if _, err := decodeMongoAuthSession(raw); err != nil {
			return err
		}
		if _, found, err := findMongoAuthAPIKeyByID(sessionContext, transaction.authAPIKeyCollection(), key.ID); err != nil {
			return err
		} else if found {
			return store.ErrConflict
		}
		if _, found, err := findMongoAuthAPIKeyByToken(sessionContext, transaction.authAPIKeyCollection(), key.TokenHash); err != nil {
			return err
		} else if found {
			return store.ErrConflict
		}
		encoded, err := encodeMongoAuthAPIKey(mongoAuthAPIKey{Key: key, UserIncarnation: incarnation})
		if err != nil {
			return err
		}
		_, err = transaction.authAPIKeyCollection().InsertOne(sessionContext, encoded)
		return translateMongoError(ctx, err)
	})
}

func (backend *Store) FindAPIKey(ctx context.Context, id string, now time.Time) (store.AuthAPIKey, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication API-key lookup"); err != nil {
		return store.AuthAPIKey{}, err
	}
	if err := validateMongoAuthSecretIdentity(id, "auth API-key ID"); err != nil {
		return store.AuthAPIKey{}, err
	}
	now, _, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return store.AuthAPIKey{}, err
	}
	var result store.AuthAPIKey
	err = backend.runMongoAuthSnapshot(ctx, func(sessionContext context.Context) error {
		key, found, err := findMongoAuthAPIKeyByID(sessionContext, backend.authAPIKeyCollection(), id)
		if err != nil {
			return err
		}
		if !found || (!key.Key.ExpiresAt.IsZero() && !key.Key.ExpiresAt.After(now)) {
			return store.ErrNotFound
		}
		if err := backend.rejectMongoAuthIncarnationMismatch(sessionContext, key.Key.CollectionID, key.Key.UserID, key.UserIncarnation); err != nil {
			return err
		}
		result = cloneMongoAuthAPIKey(key.Key)
		return nil
	})
	return result, err
}

func (backend *Store) TouchAPIKey(ctx context.Context, id string, now time.Time) error {
	if err := backend.prepareAuthOperation(ctx, "authentication API-key touch"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(id, "auth API-key ID"); err != nil {
		return err
	}
	now, nowNanos, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return err
	}
	key, found, err := findMongoAuthAPIKeyByID(ctx, backend.authAPIKeyCollection(), id)
	if err != nil {
		return err
	}
	if !found || (!key.Key.ExpiresAt.IsZero() && !key.Key.ExpiresAt.After(now)) {
		return store.ErrNotFound
	}
	reference := store.DocumentReference{CollectionID: key.Key.CollectionID, DocumentID: key.Key.UserID}
	references, err := backend.captureActiveDocumentReferences(ctx, reference)
	if err != nil {
		return err
	}
	incarnation, _ := mongoCapturedAuthIncarnation(references, reference)
	if incarnation != key.UserIncarnation {
		return store.ErrNotFound
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
			return err
		}
		current, found, err := findMongoAuthAPIKeyByID(sessionContext, transaction.authAPIKeyCollection(), id)
		if err != nil {
			return err
		}
		if !found || current.UserIncarnation != incarnation || (!current.Key.ExpiresAt.IsZero() && !current.Key.ExpiresAt.After(now)) {
			return store.ErrNotFound
		}
		raw, err := transaction.authAPIKeyCollection().FindOneAndUpdate(
			sessionContext,
			bson.D{{Key: "_id", Value: id}, {Key: "userIncarnation", Value: incarnation}, {Key: "tokenHash", Value: current.Key.TokenHash}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "lastUsedAt", Value: nowNanos}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		_, err = decodeMongoAuthAPIKey(raw)
		return err
	})
}

func (backend *Store) ListAPIKeys(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthAPIKey, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication API-key listing"); err != nil {
		return nil, err
	}
	if err := validateMongoAuthOwner(collectionID, userID, "auth API-key owner"); err != nil {
		return nil, err
	}
	now, nowNanos, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return nil, err
	}
	var result []store.AuthAPIKey
	err = backend.runMongoAuthSnapshot(ctx, func(sessionContext context.Context) error {
		incarnation, err := backend.activeMongoAuthOwnerIncarnation(sessionContext, collectionID, userID)
		if err != nil {
			return err
		}
		expiry := bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "expiresAt", Value: nil}},
			bson.D{{Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: nowNanos}}}},
		}}}
		predicate := mongoAnd([]bson.D{mongoAuthOwnerFilter(collectionID, userID), {{Key: "userIncarnation", Value: incarnation}}, expiry})
		cursor, err := backend.authAPIKeyCollection().Find(
			sessionContext, predicate,
			options.Find().SetSort(bson.D{{Key: "createdAt", Value: int32(-1)}, {Key: "_id", Value: int32(1)}}),
		)
		if err != nil {
			return translateMongoError(ctx, err)
		}
		defer closeMongoSystemCursor(cursor)
		for cursor.Next(sessionContext) {
			key, decodeErr := decodeMongoAuthAPIKey(cursor.Current)
			if decodeErr != nil {
				return decodeErr
			}
			if key.UserIncarnation != incarnation || (!key.Key.ExpiresAt.IsZero() && !key.Key.ExpiresAt.After(now)) {
				return fmt.Errorf("stored MongoDB auth API key violated the scoped listing predicate")
			}
			result = append(result, cloneMongoAuthAPIKey(key.Key))
		}
		return translateMongoError(ctx, cursor.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (backend *Store) DeleteAPIKey(ctx context.Context, collectionID schema.StableID, userID, id string) error {
	if err := backend.prepareAuthOperation(ctx, "authentication API-key deletion"); err != nil {
		return err
	}
	if err := validateMongoAuthOwner(collectionID, userID, "auth API-key owner"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(id, "auth API-key ID"); err != nil {
		return err
	}
	key, found, err := findMongoAuthAPIKeyByID(ctx, backend.authAPIKeyCollection(), id)
	if err != nil || !found {
		return err
	}
	if key.Key.CollectionID != collectionID || key.Key.UserID != userID {
		return nil
	}
	_, err = backend.authAPIKeyCollection().DeleteOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "collection", Value: string(collectionID)}, {Key: "user", Value: userID}, {Key: "tokenHash", Value: key.Key.TokenHash}})
	return translateMongoError(ctx, err)
}
