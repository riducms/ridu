package mongodb

import (
	"context"
	"errors"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func findMongoAuthTokenByHash(ctx context.Context, collection *mongo.Collection, tokenHash string) (mongoAuthToken, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "tokenHash", Value: tokenHash}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthToken{}, false, nil
	}
	if err != nil {
		return mongoAuthToken{}, false, translateMongoError(ctx, err)
	}
	token, err := decodeMongoAuthToken(raw)
	if err != nil {
		return mongoAuthToken{}, false, err
	}
	if token.Token.TokenHash != tokenHash {
		return mongoAuthToken{}, false, mongoSystemIdentityCollision("auth token")
	}
	return token, true, nil
}

func findMongoAuthTokenByOwnerPurpose(ctx context.Context, collection *mongo.Collection, collectionID schema.StableID, userID string, purpose store.AuthTokenPurpose) (mongoAuthToken, bool, error) {
	id := mongoAuthTokenID(collectionID, userID, purpose)
	raw, err := collection.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthToken{}, false, nil
	}
	if err != nil {
		return mongoAuthToken{}, false, translateMongoError(ctx, err)
	}
	token, err := decodeMongoAuthToken(raw)
	if err != nil {
		return mongoAuthToken{}, false, err
	}
	if token.Token.CollectionID != collectionID || token.Token.UserID != userID || token.Token.Purpose != purpose {
		return mongoAuthToken{}, false, mongoSystemIdentityCollision("auth token")
	}
	return token, true, nil
}

func (backend *Store) CreateAuthToken(ctx context.Context, token store.AuthToken) error {
	if err := backend.prepareAuthOperation(ctx, "authentication token creation"); err != nil {
		return err
	}
	if err := validateMongoAuthToken(mongoAuthToken{Token: token, UserIncarnation: zeroMongoDocumentIncarnation}); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: token.CollectionID, DocumentID: token.UserID}
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
		if existing, found, err := findMongoAuthTokenByOwnerPurpose(sessionContext, transaction.authTokenCollection(), token.CollectionID, token.UserID, token.Purpose); err != nil {
			return err
		} else if found {
			result, err := transaction.authTokenCollection().DeleteOne(sessionContext, bson.D{
				{Key: "_id", Value: mongoAuthTokenID(token.CollectionID, token.UserID, token.Purpose)},
				{Key: "tokenHash", Value: existing.Token.TokenHash},
				{Key: "userIncarnation", Value: existing.UserIncarnation},
			})
			if err != nil {
				return translateMongoError(ctx, err)
			}
			if result.DeletedCount != 1 {
				return mongoConfirmedTransactionConflict{}
			}
		}
		if existing, found, err := findMongoAuthTokenByHash(sessionContext, transaction.authTokenCollection(), token.TokenHash); err != nil {
			return err
		} else if found && (existing.Token.CollectionID != token.CollectionID || existing.Token.UserID != token.UserID || existing.Token.Purpose != token.Purpose) {
			return store.ErrConflict
		}
		encoded, err := encodeMongoAuthToken(mongoAuthToken{Token: token, UserIncarnation: incarnation})
		if err != nil {
			return err
		}
		_, err = transaction.authTokenCollection().InsertOne(sessionContext, encoded)
		return translateMongoError(ctx, err)
	})
}

func (backend *Store) ResetPasswordWithToken(ctx context.Context, collectionID schema.StableID, tokenHash string, hash []byte, now time.Time) (string, error) {
	return backend.consumeMongoAuthToken(ctx, collectionID, tokenHash, now, store.AuthTokenPasswordReset, hash)
}

func (backend *Store) VerifyEmailWithToken(ctx context.Context, collectionID schema.StableID, tokenHash string, now time.Time) (string, error) {
	return backend.consumeMongoAuthToken(ctx, collectionID, tokenHash, now, store.AuthTokenVerifyEmail, nil)
}

func (backend *Store) consumeMongoAuthToken(ctx context.Context, collectionID schema.StableID, tokenHash string, now time.Time, purpose store.AuthTokenPurpose, replacementHash []byte) (string, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication token consumption"); err != nil {
		return "", err
	}
	if !schema.IsValidStableID(string(collectionID)) {
		return "", store.ErrNotFound
	}
	if err := validateMongoAuthSecretIdentity(tokenHash, "auth token digest"); err != nil {
		return "", err
	}
	now, _, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return "", err
	}
	var userID string
	err = backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		userID = ""
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		token, found, err := findMongoAuthTokenByHash(sessionContext, transaction.authTokenCollection(), tokenHash)
		if err != nil {
			return err
		}
		if !found || token.Token.CollectionID != collectionID || token.Token.Purpose != purpose || !token.Token.ExpiresAt.After(now) {
			return store.ErrNotFound
		}
		reference := store.DocumentReference{CollectionID: collectionID, DocumentID: token.Token.UserID}
		incarnation, err := transaction.fenceMongoAuthOwnerInTransaction(sessionContext, nil, reference, token.UserIncarnation)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return store.ErrNotFound
			}
			return err
		}
		credential, err := transaction.requireMongoAuthCredentialIncarnation(sessionContext, collectionID, token.Token.UserID, incarnation)
		if err != nil {
			return err
		}
		deleted, err := transaction.authTokenCollection().DeleteOne(sessionContext, bson.D{
			{Key: "_id", Value: mongoAuthTokenID(collectionID, token.Token.UserID, purpose)},
			{Key: "tokenHash", Value: tokenHash},
			{Key: "userIncarnation", Value: incarnation},
		})
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if deleted.DeletedCount != 1 {
			return mongoConfirmedTransactionConflict{}
		}
		assignments := bson.D{}
		if purpose == store.AuthTokenPasswordReset {
			assignments = bson.D{
				{Key: "passwordHash", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), replacementHash...)}},
				{Key: "failedLoginAttempts", Value: int64(0)},
				{Key: "lockedUntil", Value: nil},
			}
		} else {
			assignments = bson.D{{Key: "verified", Value: true}}
		}
		raw, err := transaction.authCredentialCollection().FindOneAndUpdate(
			sessionContext,
			mongoAuthCredentialMutationFilter(credential),
			bson.D{{Key: "$set", Value: assignments}, {Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if _, err := decodeMongoAuthCredential(raw); err != nil {
			return err
		}
		if purpose == store.AuthTokenPasswordReset {
			if err := transaction.revokeMongoAuthBearers(sessionContext, reference); err != nil {
				return err
			}
		}
		userID = token.Token.UserID
		return nil
	})
	if err != nil {
		return "", err
	}
	return userID, nil
}
