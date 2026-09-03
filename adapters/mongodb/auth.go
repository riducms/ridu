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

const (
	mongoAuthCredentialCollectionName = "z_ridu_auth_credentials"
	mongoAuthSessionCollectionName    = "z_ridu_auth_sessions"
	mongoAuthTokenCollectionName      = "z_ridu_auth_tokens"
	mongoAuthAPIKeyCollectionName     = "z_ridu_auth_api_keys"
	mongoAuthRateLimitCollectionName  = "z_ridu_auth_rate_limits"
	mongoAuthBootstrapCollectionName  = "z_ridu_auth_bootstrap"
)

func (backend *Store) prepareAuthOperation(ctx context.Context, capability string) error {
	if err := backend.prepareSystemOperation(ctx, capability); err != nil {
		return err
	}
	return backend.requireVerifiedAuthIndexes()
}

func validateMongoAuthCollection(collection schema.Collection) (schema.Field, error) {
	if err := validateCollectionEnvelope(collection); err != nil {
		return schema.Field{}, err
	}
	return validateMongoAuthCollectionMetadata(collection)
}

func validateMongoAuthCollectionMetadata(collection schema.Collection) (schema.Field, error) {
	if collection.Auth == nil || !collection.Capabilities.Auth || collection.Auth.IdentityField == "" {
		return schema.Field{}, fmt.Errorf("auth collection %q has no identity field", collection.Slug)
	}
	for _, candidate := range collection.Fields {
		if candidate.Name != collection.Auth.IdentityField || candidate.Category == schema.FieldCategoryPresentation {
			continue
		}
		if (candidate.Type != schema.FieldTypeText && candidate.Type != schema.FieldTypeEmail) ||
			!candidate.Unique || candidate.Localized {
			return schema.Field{}, fmt.Errorf("auth collection %q has an invalid identity field", collection.Slug)
		}
		return candidate, nil
	}
	return schema.Field{}, fmt.Errorf("auth collection %q has no identity field", collection.Slug)
}

func (backend *Store) authCredentialCollection() *mongo.Collection {
	return backend.database.Collection(mongoAuthCredentialCollectionName)
}

func (backend *Store) authSessionCollection() *mongo.Collection {
	return backend.database.Collection(mongoAuthSessionCollectionName)
}

func (backend *Store) authTokenCollection() *mongo.Collection {
	return backend.database.Collection(mongoAuthTokenCollectionName)
}

func (backend *Store) authAPIKeyCollection() *mongo.Collection {
	return backend.database.Collection(mongoAuthAPIKeyCollectionName)
}

func (backend *Store) authRateLimitCollection() *mongo.Collection {
	return backend.database.Collection(mongoAuthRateLimitCollectionName)
}

func (backend *Store) authBootstrapCollection() *mongo.Collection {
	return backend.database.Collection(mongoAuthBootstrapCollectionName)
}

func (transaction *documentTransaction) authCredentialCollection() *mongo.Collection {
	return transaction.store.authCredentialCollection()
}

func (transaction *documentTransaction) authSessionCollection() *mongo.Collection {
	return transaction.store.authSessionCollection()
}

func (transaction *documentTransaction) authTokenCollection() *mongo.Collection {
	return transaction.store.authTokenCollection()
}

func (transaction *documentTransaction) authAPIKeyCollection() *mongo.Collection {
	return transaction.store.authAPIKeyCollection()
}

func (transaction *documentTransaction) authRateLimitCollection() *mongo.Collection {
	return transaction.store.authRateLimitCollection()
}

func (transaction *documentTransaction) authBootstrapCollection() *mongo.Collection {
	return transaction.store.authBootstrapCollection()
}

func mongoAuthOwnerFilter(collectionID schema.StableID, userID string) bson.D {
	return bson.D{{Key: "collection", Value: string(collectionID)}, {Key: "user", Value: userID}}
}

func findMongoAuthCredentialByOwner(ctx context.Context, collection *mongo.Collection, collectionID schema.StableID, userID string) (mongoAuthCredential, bool, error) {
	logical := mongoAuthOwnerFilter(collectionID, userID)
	raw, err := collection.FindOne(ctx, logical).Raw()
	if err == nil {
		credential, decodeErr := decodeMongoAuthCredential(raw)
		if decodeErr != nil {
			return mongoAuthCredential{}, false, decodeErr
		}
		if credential.CollectionID != collectionID || credential.UserID != userID {
			return mongoAuthCredential{}, false, mongoSystemIdentityCollision("auth credential")
		}
		return credential, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthCredential{}, false, translateMongoError(ctx, err)
	}
	raw, err = collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoAuthCredentialID(collectionID, userID)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoAuthCredential{}, false, nil
	}
	if err != nil {
		return mongoAuthCredential{}, false, translateMongoError(ctx, err)
	}
	credential, err := decodeMongoAuthCredential(raw)
	if err != nil {
		return mongoAuthCredential{}, false, err
	}
	if credential.CollectionID != collectionID || credential.UserID != userID {
		return mongoAuthCredential{}, false, mongoSystemIdentityCollision("auth credential")
	}
	return credential, true, nil
}

func (transaction *documentTransaction) fenceMongoAuthOwnerInTransaction(
	ctx context.Context,
	collection *schema.Collection,
	reference store.DocumentReference,
	expectedIncarnation string,
) (string, error) {
	if err := transaction.store.requireVerifiedResourceID(reference.CollectionID); err != nil {
		return "", err
	}
	physicalName := physicalCollectionName(reference.CollectionID)
	raw, err := transaction.store.database.Collection(physicalName).FindOne(
		ctx,
		bson.D{{Key: mongoIDPath, Value: reference.DocumentID}},
	).Raw()
	if err != nil {
		return "", translateMongoError(ctx, err)
	}
	var document store.Document
	if collection == nil {
		document, err = decodeDocument(raw)
	} else {
		document, err = decodeCollectionDocument(raw, *collection)
	}
	if err != nil {
		return "", err
	}
	if document.DeletedAt != nil {
		return "", store.ErrNotFound
	}
	incarnation, err := decodeDocumentIncarnation(raw)
	if err != nil {
		return "", err
	}
	if expectedIncarnation != "" && incarnation != expectedIncarnation {
		return "", fmt.Errorf("MongoDB auth owner changed incarnation: %w", store.ErrConflict)
	}
	item := mongoActiveDocumentReference{reference: reference, physicalName: physicalName, incarnation: incarnation}
	if err := transaction.fenceActiveDocumentReferences(ctx, []mongoActiveDocumentReference{item}); err != nil {
		return "", err
	}
	return incarnation, nil
}

func mongoCapturedAuthIncarnation(references []mongoActiveDocumentReference, reference store.DocumentReference) (string, error) {
	for _, item := range references {
		if item.reference == reference {
			return item.incarnation, nil
		}
	}
	return "", fmt.Errorf("MongoDB auth owner reference was not captured")
}

func (backend *Store) SetPasswordHash(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	if err := backend.prepareAuthOperation(ctx, "authentication password replacement"); err != nil {
		return err
	}
	if _, err := validateMongoAuthCollection(collection); err != nil {
		return err
	}
	if err := backend.requireVerifiedIndexes(collection); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: collection.ID, DocumentID: userID}
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
		incarnation, err := mongoCapturedAuthIncarnation(references, reference)
		if err != nil {
			return err
		}
		credential, found, err := findMongoAuthCredentialByOwner(sessionContext, transaction.authCredentialCollection(), collection.ID, userID)
		if err != nil {
			return err
		}
		if !found {
			credential = mongoAuthCredential{
				CollectionID: collection.ID, UserID: userID, UserIncarnation: incarnation,
				PasswordHash: append([]byte(nil), hash...), Verified: initiallyVerified,
			}
			encoded, err := encodeMongoAuthCredential(credential)
			if err != nil {
				return err
			}
			if _, err := transaction.authCredentialCollection().InsertOne(sessionContext, encoded); err != nil {
				return translateMongoError(ctx, err)
			}
		} else {
			if credential.UserIncarnation != incarnation {
				return fmt.Errorf("MongoDB auth credential belongs to a different document incarnation: %w", store.ErrConflict)
			}
			filter := mongoAuthCredentialMutationFilter(credential)
			update := bson.D{
				{Key: "$set", Value: bson.D{
					{Key: "passwordHash", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), hash...)}},
					{Key: "failedLoginAttempts", Value: int64(0)},
					{Key: "lockedUntil", Value: nil},
				}},
				{Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}},
			}
			raw, err := transaction.authCredentialCollection().FindOneAndUpdate(
				sessionContext, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
			).Raw()
			if err != nil {
				return translateMongoError(ctx, err)
			}
			if _, err := decodeMongoAuthCredential(raw); err != nil {
				return err
			}
		}
		return transaction.revokeMongoAuthBearers(sessionContext, reference)
	})
}

func (backend *Store) ChangePasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	return backend.compareAndSetPasswordHash(ctx, collection, userID, expectedPasswordHash, hash, true)
}

func (backend *Store) UpgradePasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte) error {
	return backend.compareAndSetPasswordHash(ctx, collection, userID, expectedPasswordHash, hash, false)
}

func (backend *Store) compareAndSetPasswordHash(ctx context.Context, collection schema.Collection, userID string, expectedPasswordHash, hash []byte, revoke bool) error {
	if err := backend.prepareAuthOperation(ctx, "authentication password change"); err != nil {
		return err
	}
	if _, err := validateMongoAuthCollection(collection); err != nil {
		return err
	}
	if err := backend.requireVerifiedIndexes(collection); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: collection.ID, DocumentID: userID}
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
		credential, found, err := findMongoAuthCredentialByOwner(sessionContext, transaction.authCredentialCollection(), collection.ID, userID)
		if err != nil {
			return err
		}
		if !found || credential.UserIncarnation != incarnation {
			return store.ErrNotFound
		}
		if subtle.ConstantTimeCompare(credential.PasswordHash, expectedPasswordHash) != 1 {
			return store.ErrConflict
		}
		update := bson.D{
			{Key: "$set", Value: bson.D{{Key: "passwordHash", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), hash...)}}}},
			{Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}},
		}
		if revoke {
			update[0].Value = append(update[0].Value.(bson.D),
				bson.E{Key: "failedLoginAttempts", Value: int64(0)},
				bson.E{Key: "lockedUntil", Value: nil},
			)
		}
		raw, err := transaction.authCredentialCollection().FindOneAndUpdate(
			sessionContext, mongoAuthCredentialMutationFilter(credential), update,
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if _, err := decodeMongoAuthCredential(raw); err != nil {
			return err
		}
		if revoke {
			return transaction.revokeMongoAuthBearers(sessionContext, reference)
		}
		return nil
	})
}

func mongoAuthCredentialMutationFilter(credential mongoAuthCredential) bson.D {
	return bson.D{
		{Key: "_id", Value: mongoAuthCredentialID(credential.CollectionID, credential.UserID)},
		{Key: "collection", Value: string(credential.CollectionID)},
		{Key: "user", Value: credential.UserID},
		{Key: "userIncarnation", Value: credential.UserIncarnation},
		{Key: "fence", Value: credential.Fence},
	}
}

func (backend *Store) FindAuthCredential(ctx context.Context, collection schema.Collection, identity string) (store.AuthCredential, error) {
	if err := backend.prepareAuthOperation(ctx, "authentication credential lookup"); err != nil {
		return store.AuthCredential{}, err
	}
	identityField, err := validateMongoAuthCollection(collection)
	if err != nil {
		return store.AuthCredential{}, err
	}
	if err := backend.requireVerifiedIndexes(collection); err != nil {
		return store.AuthCredential{}, err
	}
	identity = store.CanonicalAuthIdentity(identity)
	var result store.AuthCredential
	err = backend.runMongoAuthSnapshot(ctx, func(sessionContext context.Context) error {
		envelope, err := mongoCollectionEnvelopePredicate(collection, nil)
		if err != nil {
			return err
		}
		predicate := mongoAnd([]bson.D{
			{{Key: mongoAuthoredValuesPath + identityField.Name, Value: identity}},
			mongoExactNullPredicate(mongoDeletedAtPath),
			envelope,
		})
		raw, err := backend.database.Collection(physicalCollectionName(collection.ID)).FindOne(sessionContext, predicate).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		user, err := decodeCollectionDocument(raw, collection)
		if err != nil {
			return err
		}
		incarnation, err := decodeDocumentIncarnation(raw)
		if err != nil {
			return err
		}
		credential, found, err := findMongoAuthCredentialByOwner(sessionContext, backend.authCredentialCollection(), collection.ID, user.ID)
		if err != nil {
			return err
		}
		if !found || credential.UserIncarnation != incarnation {
			return store.ErrNotFound
		}
		result = store.AuthCredential{
			User: user, PasswordHash: append([]byte(nil), credential.PasswordHash...),
			FailedLoginAttempts: credential.FailedLoginAttempts, Verified: credential.Verified,
		}
		if credential.LockedUntil != nil {
			result.LockedUntil = *credential.LockedUntil
		}
		return nil
	})
	if err != nil {
		return store.AuthCredential{}, err
	}
	return result, nil
}

func (backend *Store) RecordFailedLogin(ctx context.Context, collectionID schema.StableID, userID string, now time.Time, maximum int, lockDuration time.Duration) (store.AuthCredential, error) {
	if maximum < 1 || lockDuration <= 0 {
		return store.AuthCredential{}, fmt.Errorf("MongoDB failed-login policy is invalid")
	}
	now, _, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return store.AuthCredential{}, err
	}
	lockedUntil, lockedNanos, err := normalizeMongoSystemTime(now.Add(lockDuration), "authentication lock deadline")
	if err != nil {
		return store.AuthCredential{}, err
	}
	if err := backend.prepareAuthOperation(ctx, "authentication failed-login update"); err != nil {
		return store.AuthCredential{}, err
	}
	reference := store.DocumentReference{CollectionID: collectionID, DocumentID: userID}
	references, err := backend.captureActiveDocumentReferences(ctx, reference)
	if err != nil {
		return store.AuthCredential{}, err
	}
	var result store.AuthCredential
	err = backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		result = store.AuthCredential{}
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
			return err
		}
		incarnation, _ := mongoCapturedAuthIncarnation(references, reference)
		credential, found, err := findMongoAuthCredentialByOwner(sessionContext, transaction.authCredentialCollection(), collectionID, userID)
		if err != nil {
			return err
		}
		if !found || credential.UserIncarnation != incarnation {
			return store.ErrNotFound
		}
		attempts := credential.FailedLoginAttempts + 1
		var nextLockedUntil any
		if credential.LockedUntil != nil {
			switch {
			case credential.LockedUntil.After(now):
				attempts = credential.FailedLoginAttempts
				nextLockedUntil, _ = encodeTime(*credential.LockedUntil)
			case !credential.LockedUntil.After(now):
				attempts = 1
			}
		}
		if nextLockedUntil == nil && attempts >= maximum {
			nextLockedUntil = lockedNanos
		}
		update := bson.D{
			{Key: "$set", Value: bson.D{{Key: "failedLoginAttempts", Value: int64(attempts)}, {Key: "lockedUntil", Value: nextLockedUntil}}},
			{Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}},
		}
		raw, err := transaction.authCredentialCollection().FindOneAndUpdate(sessionContext, mongoAuthCredentialMutationFilter(credential), update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		stored, err := decodeMongoAuthCredential(raw)
		if err != nil {
			return err
		}
		result.FailedLoginAttempts = stored.FailedLoginAttempts
		if stored.LockedUntil != nil {
			result.LockedUntil = *stored.LockedUntil
		}
		if result.LockedUntil.Equal(lockedUntil) || result.LockedUntil.IsZero() || result.LockedUntil.After(now) {
			return nil
		}
		return fmt.Errorf("stored MongoDB auth credential returned inconsistent lock state")
	})
	return result, err
}

func (backend *Store) ResetLoginAttempts(ctx context.Context, collectionID schema.StableID, userID string, now time.Time) (bool, error) {
	now, _, err := normalizeMongoSystemTime(now, "authentication timestamp")
	if err != nil {
		return false, err
	}
	if err := backend.prepareAuthOperation(ctx, "authentication failed-login reset"); err != nil {
		return false, err
	}
	reference := store.DocumentReference{CollectionID: collectionID, DocumentID: userID}
	references, err := backend.captureActiveDocumentReferences(ctx, reference)
	if err != nil {
		return false, err
	}
	reset := false
	err = backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		reset = false
		sessionContext, leave, enterErr := transaction.enter(ctx, true)
		if enterErr != nil {
			return enterErr
		}
		defer leave()
		if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
			return err
		}
		incarnation, _ := mongoCapturedAuthIncarnation(references, reference)
		credential, found, err := findMongoAuthCredentialByOwner(sessionContext, transaction.authCredentialCollection(), collectionID, userID)
		if err != nil {
			return err
		}
		if !found || credential.UserIncarnation != incarnation {
			return store.ErrNotFound
		}
		if credential.LockedUntil != nil && credential.LockedUntil.After(now) {
			return nil
		}
		raw, err := transaction.authCredentialCollection().FindOneAndUpdate(
			sessionContext, mongoAuthCredentialMutationFilter(credential),
			bson.D{{Key: "$set", Value: bson.D{{Key: "failedLoginAttempts", Value: int64(0)}, {Key: "lockedUntil", Value: nil}}}, {Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		if _, err := decodeMongoAuthCredential(raw); err != nil {
			return err
		}
		reset = true
		return nil
	})
	return reset, err
}

func (backend *Store) ForceUnlock(ctx context.Context, collectionID schema.StableID, userID string) error {
	if err := backend.prepareAuthOperation(ctx, "authentication force unlock"); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: collectionID, DocumentID: userID}
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
		return transaction.forceUnlockMongoAuth(sessionContext, reference, incarnation)
	})
}

func (transaction *documentTransaction) ForceUnlockAuth(ctx context.Context, collectionID schema.StableID, userID string) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if err := transaction.store.requireVerifiedAuthIndexes(); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: collectionID, DocumentID: userID}
	incarnation, err := transaction.fenceMongoAuthOwnerInTransaction(sessionContext, nil, reference, "")
	if err != nil {
		return err
	}
	return transaction.forceUnlockMongoAuth(sessionContext, reference, incarnation)
}

func (transaction *documentTransaction) forceUnlockMongoAuth(ctx context.Context, reference store.DocumentReference, incarnation string) error {
	credential, found, err := findMongoAuthCredentialByOwner(ctx, transaction.authCredentialCollection(), reference.CollectionID, reference.DocumentID)
	if err != nil {
		return err
	}
	if !found || credential.UserIncarnation != incarnation {
		return store.ErrNotFound
	}
	raw, err := transaction.authCredentialCollection().FindOneAndUpdate(
		ctx, mongoAuthCredentialMutationFilter(credential),
		bson.D{{Key: "$set", Value: bson.D{{Key: "failedLoginAttempts", Value: int64(0)}, {Key: "lockedUntil", Value: nil}}}, {Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Raw()
	if err != nil {
		return translateMongoError(ctx, err)
	}
	_, err = decodeMongoAuthCredential(raw)
	return err
}

func (transaction *documentTransaction) CreateAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if err := transaction.store.requireVerifiedAuthIndexes(); err != nil {
		return err
	}
	if err := transaction.store.requireVerifiedIndexes(collection); err != nil {
		return err
	}
	if _, err := validateMongoAuthCollection(collection); err != nil {
		return err
	}
	reference := store.DocumentReference{CollectionID: collection.ID, DocumentID: userID}
	incarnation, err := transaction.fenceMongoAuthOwnerInTransaction(sessionContext, &collection, reference, "")
	if err != nil {
		return err
	}
	return transaction.createMongoAuthCredential(sessionContext, collection.ID, userID, incarnation, hash, initiallyVerified)
}

func (transaction *documentTransaction) createMongoAuthCredential(ctx context.Context, collectionID schema.StableID, userID, incarnation string, hash []byte, initiallyVerified bool) error {
	if _, found, err := findMongoAuthCredentialByOwner(ctx, transaction.authCredentialCollection(), collectionID, userID); err != nil {
		return err
	} else if found {
		return store.ErrConflict
	}
	credential := mongoAuthCredential{
		CollectionID: collectionID, UserID: userID, UserIncarnation: incarnation,
		PasswordHash: append([]byte(nil), hash...), Verified: initiallyVerified,
	}
	encoded, err := encodeMongoAuthCredential(credential)
	if err != nil {
		return err
	}
	_, err = transaction.authCredentialCollection().InsertOne(ctx, encoded)
	return translateMongoError(ctx, err)
}

func (transaction *documentTransaction) CreateFirstAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if err := transaction.store.requireVerifiedAuthIndexes(); err != nil {
		return err
	}
	if err := transaction.store.requireVerifiedIndexes(collection); err != nil {
		return err
	}
	if _, err := validateMongoAuthCollection(collection); err != nil {
		return err
	}

	guardCollection := transaction.authBootstrapCollection()
	guardID := mongoAuthBootstrapID(collection.ID)
	if raw, findErr := guardCollection.FindOne(sessionContext, bson.D{{Key: "_id", Value: guardID}}).Raw(); findErr == nil {
		guard, decodeErr := decodeMongoAuthBootstrapGuard(raw)
		if decodeErr != nil {
			return decodeErr
		}
		if guard.CollectionID != collection.ID {
			return mongoSystemIdentityCollision("auth bootstrap guard")
		}
	} else if !errors.Is(findErr, mongo.ErrNoDocuments) {
		return translateMongoError(ctx, findErr)
	}
	raw, err := guardCollection.FindOneAndUpdate(
		sessionContext,
		bson.D{{Key: "_id", Value: guardID}, {Key: "collection", Value: string(collection.ID)}},
		bson.D{
			{Key: "$setOnInsert", Value: bson.D{{Key: "codec", Value: mongoAuthCodecVersion}, {Key: "collection", Value: string(collection.ID)}}},
			{Key: "$inc", Value: bson.D{{Key: "generation", Value: int64(1)}}},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Raw()
	if err != nil {
		translated := translateMongoError(ctx, err)
		if isMongoConfirmedTransactionConflict(translated) || errors.Is(translated, store.ErrConflict) {
			return store.ErrAuthInitialized
		}
		return translated
	}
	guard, err := decodeMongoAuthBootstrapGuard(raw)
	if err != nil {
		return err
	}
	if guard.CollectionID != collection.ID {
		return mongoSystemIdentityCollision("auth bootstrap guard")
	}

	envelope, err := mongoCollectionEnvelopePredicate(collection, nil)
	if err != nil {
		return err
	}
	collectionStore := transaction.store.database.Collection(physicalCollectionName(collection.ID))
	lifecycleActive := mongoExactNullPredicate(mongoDeletedAtPath)
	active, err := collectionStore.CountDocuments(sessionContext, lifecycleActive)
	if err != nil {
		return translateMongoError(ctx, err)
	}
	validActive, err := collectionStore.CountDocuments(sessionContext, mongoAnd([]bson.D{lifecycleActive, envelope}))
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if active != 1 || validActive != 1 {
		return store.ErrAuthInitialized
	}
	reference := store.DocumentReference{CollectionID: collection.ID, DocumentID: userID}
	incarnation, err := transaction.fenceMongoAuthOwnerInTransaction(sessionContext, &collection, reference, "")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			return store.ErrAuthInitialized
		}
		return err
	}
	if err := transaction.createMongoAuthCredential(sessionContext, collection.ID, userID, incarnation, hash, initiallyVerified); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return store.ErrAuthInitialized
		}
		return err
	}
	transaction.authBootstrapPrepared = true
	return nil
}

func (backend *Store) runMongoAuthSnapshot(ctx context.Context, operation func(context.Context) error) error {
	transaction, err := backend.begin(ctx, true)
	if err != nil {
		return err
	}
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		rollbackSystemTransaction(transaction)
		return err
	}
	err = operation(sessionContext)
	leave()
	if err != nil {
		rollbackSystemTransaction(transaction)
		return err
	}
	return transaction.Commit(ctx)
}

func (transaction *documentTransaction) revokeMongoAuthBearers(ctx context.Context, reference store.DocumentReference) error {
	owner := mongoAuthOwnerFilter(reference.CollectionID, reference.DocumentID)
	if _, err := transaction.authSessionCollection().DeleteMany(ctx, owner); err != nil {
		return translateMongoError(ctx, err)
	}
	_, err := transaction.authAPIKeyCollection().DeleteMany(ctx, owner)
	return translateMongoError(ctx, err)
}

func (transaction *documentTransaction) deleteAuthState(ctx context.Context, reference store.DocumentReference) error {
	owner := mongoAuthOwnerFilter(reference.CollectionID, reference.DocumentID)
	for _, collection := range []*mongo.Collection{
		transaction.authTokenCollection(),
		transaction.authSessionCollection(),
		transaction.authAPIKeyCollection(),
		transaction.authCredentialCollection(),
	} {
		if _, err := collection.DeleteMany(ctx, owner); err != nil {
			return translateMongoError(ctx, err)
		}
	}
	return nil
}

var _ store.AuthStore = (*Store)(nil)
var _ store.AuthMaintenanceStore = (*Store)(nil)
var _ store.AuthUnlockStore = (*Store)(nil)
var _ store.AuthTransaction = (*documentTransaction)(nil)
var _ store.AuthBootstrapTransaction = (*documentTransaction)(nil)
var _ store.AuthUnlockTransaction = (*documentTransaction)(nil)
