package mongodb

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func findMongoAuthSessionByToken(ctx context.Context, collection *mongo.Collection, tokenHash string) (mongoAuthSession, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoAuthSessionID(tokenHash)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthSession{}, false, nil
	}
	if err != nil {
		return mongoAuthSession{}, false, translateMongoError(ctx, err)
	}
	session, err := decodeMongoAuthSession(raw)
	if err != nil {
		return mongoAuthSession{}, false, err
	}
	if session.Session.TokenHash != tokenHash {
		return mongoAuthSession{}, false, mongoSystemIdentityCollision("auth session")
	}
	return session, true, nil
}

func findMongoAuthSessionByPublicID(ctx context.Context, collection *mongo.Collection, id string) (mongoAuthSession, bool, error) {
	raw, err := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthSession{}, false, nil
	}
	if err != nil {
		return mongoAuthSession{}, false, translateMongoError(ctx, err)
	}
	session, err := decodeMongoAuthSession(raw)
	if err != nil {
		return mongoAuthSession{}, false, err
	}
	if session.Session.ID != id {
		return mongoAuthSession{}, false, mongoSystemIdentityCollision("auth session")
	}
	return session, true, nil
}

func (transaction *documentTransaction) requireMongoAuthCredentialIncarnation(ctx context.Context, collectionID schema.StableID, userID, incarnation string) (mongoAuthCredential, error) {
	credential, found, err := findMongoAuthCredentialByOwner(ctx, transaction.authCredentialCollection(), collectionID, userID)
	if err != nil {
		return mongoAuthCredential{}, err
	}
	if !found || credential.UserIncarnation != incarnation {
		return mongoAuthCredential{}, store.ErrNotFound
	}
	return credential, nil
}

func (backend *Store) CreateSession(ctx context.Context, session store.AuthSession, expectedPasswordHash []byte) error {
	if err := backend.prepareAuthOperation(ctx, "authentication session creation"); err != nil {
		return err
	}
	if err := validateMongoAuthSession(mongoAuthSession{Session: session, UserIncarnation: zeroMongoDocumentIncarnation}); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: session.CollectionID, DocumentID: session.UserID}
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
		credential, err := transaction.requireMongoAuthCredentialIncarnation(sessionContext, session.CollectionID, session.UserID, incarnation)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(credential.PasswordHash, expectedPasswordHash) != 1 {
			return store.ErrConflict
		}
		if _, found, err := findMongoAuthSessionByToken(sessionContext, transaction.authSessionCollection(), session.TokenHash); err != nil {
			return err
		} else if found {
			return store.ErrConflict
		}
		if _, found, err := findMongoAuthSessionByPublicID(sessionContext, transaction.authSessionCollection(), session.ID); err != nil {
			return err
		} else if found {
			return store.ErrConflict
		}
		encoded, err := encodeMongoAuthSession(mongoAuthSession{Session: session, UserIncarnation: incarnation})
		if err != nil {
			return err
		}
		_, err = transaction.authSessionCollection().InsertOne(sessionContext, encoded)
		return translateMongoError(ctx, err)
	})
}

// zeroMongoDocumentIncarnation is used only to validate caller-owned auth
// records before the real owner incarnation is captured. It is never stored.
const zeroMongoDocumentIncarnation = "00000000000000000000000000000000"

func (backend *Store) RotateSession(ctx context.Context, currentHash string, replacement store.AuthSession, now time.Time) error {
	if err := backend.prepareAuthOperation(ctx, "authentication session rotation"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(currentHash, "current auth session token digest"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(replacement.TokenHash, "replacement auth session token digest"); err != nil {
		return err
	}
	if err := validateMongoAuthOwner(replacement.CollectionID, replacement.UserID, "auth session owner"); err != nil {
		return err
	}
	now, nowNanos, err := normalizeMongoSystemTime(now, "authentication session timestamp")
	if err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: replacement.CollectionID, DocumentID: replacement.UserID}
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
		current, found, err := findMongoAuthSessionByToken(sessionContext, transaction.authSessionCollection(), currentHash)
		if err != nil {
			return err
		}
		if !found || current.UserIncarnation != incarnation || current.Session.CollectionID != replacement.CollectionID ||
			current.Session.UserID != replacement.UserID || !current.Session.ExpiresAt.After(now) {
			return store.ErrNotFound
		}
		if replacement.TokenHash != currentHash {
			if _, found, err := findMongoAuthSessionByToken(sessionContext, transaction.authSessionCollection(), replacement.TokenHash); err != nil {
				return err
			} else if found {
				return store.ErrConflict
			}
			deleteResult, err := transaction.authSessionCollection().DeleteOne(sessionContext, bson.D{{Key: "_id", Value: currentHash}, {Key: "tokenHash", Value: currentHash}, {Key: "userIncarnation", Value: incarnation}, {Key: "fence", Value: current.Fence}})
			if err != nil {
				return translateMongoError(ctx, err)
			}
			if deleteResult.DeletedCount != 1 {
				return mongoConfirmedTransactionConflict{}
			}
			current.Session.TokenHash = replacement.TokenHash
			current.Session.LastSeenAt = now
			current.Fence++
			encoded, err := encodeMongoAuthSession(current)
			if err != nil {
				return err
			}
			_, err = transaction.authSessionCollection().InsertOne(sessionContext, encoded)
			return translateMongoError(ctx, err)
		}
		raw, err := transaction.authSessionCollection().FindOneAndUpdate(
			sessionContext,
			bson.D{{Key: "_id", Value: currentHash}, {Key: "tokenHash", Value: currentHash}, {Key: "userIncarnation", Value: incarnation}, {Key: "fence", Value: current.Fence}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "lastSeenAt", Value: nowNanos}}}, {Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		_, err = decodeMongoAuthSession(raw)
		return err
	})
}

func (backend *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if err := backend.prepareAuthOperation(ctx, "authentication session deletion"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(tokenHash, "auth session token digest"); err != nil {
		return err
	}
	session, found, err := findMongoAuthSessionByToken(ctx, backend.authSessionCollection(), tokenHash)
	if err != nil || !found {
		return err
	}
	result, err := backend.authSessionCollection().DeleteOne(ctx, bson.D{{Key: "_id", Value: tokenHash}, {Key: "tokenHash", Value: session.Session.TokenHash}})
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if result.DeletedCount > 1 {
		return fmt.Errorf("MongoDB auth session deletion exceeded one record")
	}
	return nil
}

func (backend *Store) DeleteUserSession(ctx context.Context, collectionID schema.StableID, userID, sessionID string) error {
	if err := backend.prepareAuthOperation(ctx, "authentication owned-session deletion"); err != nil {
		return err
	}
	if err := validateMongoAuthOwner(collectionID, userID, "auth session owner"); err != nil {
		return err
	}
	if err := validateMongoAuthSecretIdentity(sessionID, "auth session ID"); err != nil {
		return err
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		sessionContext, leave, err := transaction.enter(ctx, true)
		if err != nil {
			return err
		}
		defer leave()
		session, found, err := findMongoAuthSessionByPublicID(sessionContext, transaction.authSessionCollection(), sessionID)
		if err != nil || !found {
			return err
		}
		if session.Session.CollectionID != collectionID || session.Session.UserID != userID {
			return nil
		}
		result, err := transaction.authSessionCollection().DeleteOne(sessionContext, bson.D{
			{Key: "_id", Value: session.Session.TokenHash},
			{Key: "id", Value: sessionID},
			{Key: "collection", Value: string(collectionID)},
			{Key: "user", Value: userID},
			{Key: "userIncarnation", Value: session.UserIncarnation},
			{Key: "fence", Value: session.Fence},
		})
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if result.DeletedCount != 1 {
			return mongoConfirmedTransactionConflict{}
		}
		return nil
	})
}

func (backend *Store) DeleteUserSessions(ctx context.Context, collectionID schema.StableID, userID string) error {
	if err := backend.prepareAuthOperation(ctx, "authentication owned-session deletion"); err != nil {
		return err
	}
	if err := validateMongoAuthOwner(collectionID, userID, "auth session owner"); err != nil {
		return err
	}
	return backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		sessionContext, leave, err := transaction.enter(ctx, true)
		if err != nil {
			return err
		}
		defer leave()
		_, err = transaction.authSessionCollection().DeleteMany(sessionContext, mongoAuthOwnerFilter(collectionID, userID))
		return translateMongoError(ctx, err)
	})
}

func (backend *Store) FindSession(ctx context.Context, tokenHash string, now time.Time) (store.AuthSession, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication session lookup"); err != nil {
		return store.AuthSession{}, err
	}
	if err := validateMongoAuthSecretIdentity(tokenHash, "auth session token digest"); err != nil {
		return store.AuthSession{}, err
	}
	now, _, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return store.AuthSession{}, err
	}
	var result store.AuthSession
	err = backend.runMongoAuthSnapshot(ctx, func(sessionContext context.Context) error {
		session, found, err := findMongoAuthSessionByToken(sessionContext, backend.authSessionCollection(), tokenHash)
		if err != nil {
			return err
		}
		if !found || !session.Session.ExpiresAt.After(now) {
			return store.ErrNotFound
		}
		if err := backend.rejectMongoAuthIncarnationMismatch(sessionContext, session.Session.CollectionID, session.Session.UserID, session.UserIncarnation); err != nil {
			return err
		}
		result = cloneMongoAuthSession(session.Session)
		return nil
	})
	return result, err
}

func (backend *Store) ListSessions(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) ([]store.AuthSession, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication session listing"); err != nil {
		return nil, err
	}
	if err := validateMongoAuthOwner(collectionID, userID, "auth session owner"); err != nil {
		return nil, err
	}
	now, nowNanos, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return nil, err
	}
	var result []store.AuthSession
	err = backend.runMongoAuthSnapshot(ctx, func(sessionContext context.Context) error {
		incarnation, err := backend.activeMongoAuthOwnerIncarnation(sessionContext, collectionID, userID)
		if err != nil {
			return err
		}
		cursor, err := backend.authSessionCollection().Find(
			sessionContext,
			bson.D{{Key: "collection", Value: string(collectionID)}, {Key: "user", Value: userID}, {Key: "userIncarnation", Value: incarnation}, {Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: nowNanos}}}},
			options.Find().SetSort(bson.D{{Key: "lastSeenAt", Value: int32(-1)}, {Key: "createdAt", Value: int32(-1)}, {Key: "_id", Value: int32(1)}}),
		)
		if err != nil {
			return translateMongoError(ctx, err)
		}
		defer closeMongoSystemCursor(cursor)
		for cursor.Next(sessionContext) {
			session, decodeErr := decodeMongoAuthSession(cursor.Current)
			if decodeErr != nil {
				return decodeErr
			}
			if !session.Session.ExpiresAt.After(now) || session.UserIncarnation != incarnation {
				return fmt.Errorf("stored MongoDB auth session violated the scoped listing predicate")
			}
			result = append(result, cloneMongoAuthSession(session.Session))
		}
		return translateMongoError(ctx, cursor.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (backend *Store) rejectMongoAuthIncarnationMismatch(ctx context.Context, collectionID schema.StableID, userID, expected string) error {
	if err := backend.requireVerifiedResourceID(collectionID); err != nil {
		return err
	}
	raw, err := backend.database.Collection(physicalCollectionName(collectionID)).FindOne(ctx, bson.D{{Key: "_id", Value: userID}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Preserve an orphan long enough for the core to perform its access-free
		// physical absence check and bounded best-effort cleanup.
		return nil
	}
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if _, err := decodeDocument(raw); err != nil {
		return err
	}
	incarnation, err := decodeDocumentIncarnation(raw)
	if err != nil {
		return err
	}
	if incarnation != expected {
		return store.ErrNotFound
	}
	return nil
}

func (backend *Store) activeMongoAuthOwnerIncarnation(ctx context.Context, collectionID schema.StableID, userID string) (string, error) {
	if err := backend.requireVerifiedResourceID(collectionID); err != nil {
		return "", err
	}
	raw, err := backend.database.Collection(physicalCollectionName(collectionID)).FindOne(ctx, bson.D{{Key: "_id", Value: userID}}).Raw()
	if err != nil {
		return "", translateMongoError(ctx, err)
	}
	document, err := decodeDocument(raw)
	if err != nil {
		return "", err
	}
	if document.DeletedAt != nil {
		return "", store.ErrNotFound
	}
	return decodeDocumentIncarnation(raw)
}
