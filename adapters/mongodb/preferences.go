package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoPreferenceCollectionName = "z_ridu_preferences"

func (backend *Store) GetPreference(ctx context.Context, collectionID schema.StableID, userID, key string) (store.Preference, error) {
	if err := backend.prepareSystemOperation(ctx, "preference"); err != nil {
		return store.Preference{}, err
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err != nil {
		return store.Preference{}, err
	}
	identity := store.Preference{CollectionID: collectionID, UserID: userID, Key: key}
	if err := validateMongoPreferenceIdentity(identity); err != nil {
		return store.Preference{}, err
	}
	preference, found, err := findMongoPreferenceByIdentity(ctx, backend.preferenceCollection(), identity)
	if err != nil {
		return store.Preference{}, err
	}
	if !found {
		return store.Preference{}, store.ErrNotFound
	}
	return cloneMongoPreference(preference), nil
}

func (backend *Store) SetPreference(ctx context.Context, preference store.Preference) (store.Preference, error) {
	if err := backend.prepareSystemOperation(ctx, "preference"); err != nil {
		return store.Preference{}, err
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err != nil {
		return store.Preference{}, err
	}
	if err := validateMongoPreference(preference); err != nil {
		return store.Preference{}, err
	}
	references, err := backend.captureActiveDocumentReferences(ctx, store.DocumentReference{
		CollectionID: preference.CollectionID,
		DocumentID:   preference.UserID,
	})
	if err != nil {
		return store.Preference{}, err
	}
	var stored store.Preference
	if err := backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		var operationErr error
		stored, operationErr = transaction.setPreference(ctx, preference, references)
		return operationErr
	}); err != nil {
		return store.Preference{}, err
	}
	return cloneMongoPreference(stored), nil
}

func (transaction *documentTransaction) setPreference(
	ctx context.Context,
	preference store.Preference,
	references []mongoActiveDocumentReference,
) (store.Preference, error) {
	sessionContext, leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Preference{}, err
	}
	defer leave()
	if err := transaction.fenceActiveDocumentReferences(sessionContext, references); err != nil {
		return store.Preference{}, err
	}

	id := mongoPreferenceID(preference)
	if _, _, err := findMongoPreferenceByIdentity(sessionContext, transaction.preferenceCollection(), preference); err != nil {
		return store.Preference{}, err
	}

	updatedAt, updatedAtNanos, err := normalizeMongoSystemTime(transaction.store.now(), "preference updatedAt")
	if err != nil {
		return store.Preference{}, err
	}
	identityFilter := mongoPreferenceIdentityFilter(id, preference)
	update := bson.D{
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "codec", Value: int32(1)},
			{Key: "collection", Value: string(preference.CollectionID)},
			{Key: "user", Value: preference.UserID},
			{Key: "key", Value: preference.Key},
		}},
		{Key: "$set", Value: bson.D{
			{Key: "value", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), preference.Value...)}},
			{Key: "updatedAt", Value: updatedAtNanos},
		}},
	}
	raw, err := transaction.preferenceCollection().FindOneAndUpdate(
		sessionContext,
		identityFilter,
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Raw()
	if err != nil {
		return store.Preference{}, translateMongoError(ctx, err)
	}
	stored, err := decodeMongoPreference(raw)
	if err != nil {
		return store.Preference{}, err
	}
	if !sameMongoPreferenceIdentity(stored, preference) {
		return store.Preference{}, mongoSystemIdentityCollision("preference")
	}
	if !stored.UpdatedAt.Equal(updatedAt) {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference returned an inconsistent timestamp")
	}
	return stored, nil
}

func (backend *Store) DeletePreference(ctx context.Context, collectionID schema.StableID, userID, key string) error {
	if err := backend.prepareSystemOperation(ctx, "preference"); err != nil {
		return err
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err != nil {
		return err
	}
	identity := store.Preference{CollectionID: collectionID, UserID: userID, Key: key}
	if err := validateMongoPreferenceIdentity(identity); err != nil {
		return err
	}
	id := mongoPreferenceID(identity)
	_, found, err := findMongoPreferenceByIdentity(ctx, backend.preferenceCollection(), identity)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	_, err = backend.preferenceCollection().DeleteOne(ctx, mongoPreferenceIdentityFilter(id, identity))
	return translateMongoError(ctx, err)
}

func (backend *Store) DeletePreferences(ctx context.Context, collectionID schema.StableID, userID string) error {
	if err := backend.prepareSystemOperation(ctx, "preference"); err != nil {
		return err
	}
	if err := backend.requireVerifiedPreferenceIndexes(); err != nil {
		return err
	}
	identity := store.Preference{CollectionID: collectionID, UserID: userID}
	if err := validateMongoPreferenceOwner(identity); err != nil {
		return err
	}
	_, err := backend.preferenceCollection().DeleteMany(ctx, bson.D{
		{Key: "collection", Value: string(collectionID)},
		{Key: "user", Value: userID},
	})
	return translateMongoError(ctx, err)
}

func validateMongoPreference(preference store.Preference) error {
	if err := validateMongoPreferenceIdentity(preference); err != nil {
		return err
	}
	if !json.Valid(preference.Value) {
		return fmt.Errorf("preference value must be valid JSON")
	}
	return nil
}

func validateMongoPreferenceIdentity(preference store.Preference) error {
	if err := validateMongoPreferenceOwner(preference); err != nil {
		return err
	}
	return validateMongoSystemString(preference.Key, "preference key")
}

func validateMongoPreferenceOwner(preference store.Preference) error {
	return validateMongoSystemReference(store.DocumentReference{
		CollectionID: preference.CollectionID,
		DocumentID:   preference.UserID,
	}, "preference owner")
}

func mongoPreferenceID(preference store.Preference) string {
	return mongoSystemRecordID("preference", string(preference.CollectionID), preference.UserID, preference.Key)
}

func mongoPreferenceLogicalIdentityFilter(preference store.Preference) bson.D {
	return bson.D{
		{Key: "collection", Value: string(preference.CollectionID)},
		{Key: "user", Value: preference.UserID},
		{Key: "key", Value: preference.Key},
	}
}

func mongoPreferenceIdentityFilter(id string, preference store.Preference) bson.D {
	filter := bson.D{{Key: "_id", Value: id}}
	return append(filter, mongoPreferenceLogicalIdentityFilter(preference)...)
}

// findMongoPreferenceByIdentity checks both identities protected by the
// verified collection indexes. The logical probe exposes a row whose _id was
// written out of band, while the fallback _id probe still detects a hash-key
// collision with a different logical owner. A genuine concurrent insert is
// left to the transaction conflict retry path.
func findMongoPreferenceByIdentity(
	ctx context.Context,
	collection *mongo.Collection,
	identity store.Preference,
) (store.Preference, bool, error) {
	raw, err := collection.FindOne(ctx, mongoPreferenceLogicalIdentityFilter(identity)).Raw()
	if err == nil {
		preference, decodeErr := decodeMongoPreference(raw)
		if decodeErr != nil {
			return store.Preference{}, false, decodeErr
		}
		if !sameMongoPreferenceIdentity(preference, identity) {
			return store.Preference{}, false, mongoSystemIdentityCollision("preference")
		}
		return preference, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return store.Preference{}, false, translateMongoError(ctx, err)
	}

	raw, err = collection.FindOne(ctx, bson.D{{Key: "_id", Value: mongoPreferenceID(identity)}}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.Preference{}, false, nil
	}
	if err != nil {
		return store.Preference{}, false, translateMongoError(ctx, err)
	}
	preference, err := decodeMongoPreference(raw)
	if err != nil {
		return store.Preference{}, false, err
	}
	if !sameMongoPreferenceIdentity(preference, identity) {
		return store.Preference{}, false, mongoSystemIdentityCollision("preference")
	}
	return preference, true, nil
}

func encodeMongoPreference(preference store.Preference) (bson.D, error) {
	if err := validateMongoPreference(preference); err != nil {
		return nil, err
	}
	updatedAt, err := encodeTime(preference.UpdatedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB preference updatedAt: %w", err)
	}
	return bson.D{
		{Key: "_id", Value: mongoPreferenceID(preference)},
		{Key: "codec", Value: int32(1)},
		{Key: "collection", Value: string(preference.CollectionID)},
		{Key: "user", Value: preference.UserID},
		{Key: "key", Value: preference.Key},
		{Key: "value", Value: bson.Binary{Subtype: 0, Data: append([]byte(nil), preference.Value...)}},
		{Key: "updatedAt", Value: updatedAt},
	}, nil
}

func decodeMongoPreference(raw bson.Raw) (store.Preference, error) {
	if err := requireExactKeys(raw, "MongoDB preference", "_id", "codec", "collection", "user", "key", "value", "updatedAt"); err != nil {
		return store.Preference{}, err
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != 1 {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference has an unsupported codec version")
	}
	collectionID, ok := raw.Lookup("collection").StringValueOK()
	if !ok {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference has an invalid collection")
	}
	userID, ok := raw.Lookup("user").StringValueOK()
	if !ok {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference has an invalid user")
	}
	key, ok := raw.Lookup("key").StringValueOK()
	if !ok {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference has an invalid key")
	}
	preference := store.Preference{CollectionID: schema.StableID(collectionID), UserID: userID, Key: key}
	if err := validateMongoPreferenceIdentity(preference); err != nil {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference identity: %w", err)
	}
	id, ok := raw.Lookup("_id").StringValueOK()
	if !ok || id != mongoPreferenceID(preference) {
		return store.Preference{}, mongoSystemIdentityCollision("preference")
	}
	subtype, value, ok := raw.Lookup("value").BinaryOK()
	if !ok || subtype != 0 || !json.Valid(value) {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference has an invalid JSON value")
	}
	updatedAt, ok := raw.Lookup("updatedAt").Int64OK()
	if !ok {
		return store.Preference{}, fmt.Errorf("stored MongoDB preference has an invalid updatedAt")
	}
	preference.Value = append(json.RawMessage(nil), value...)
	preference.UpdatedAt = decodeTime(updatedAt)
	return preference, nil
}

func sameMongoPreferenceIdentity(left, right store.Preference) bool {
	return left.CollectionID == right.CollectionID && left.UserID == right.UserID && left.Key == right.Key
}

func cloneMongoPreference(preference store.Preference) store.Preference {
	preference.Value = append(json.RawMessage(nil), preference.Value...)
	return preference
}

func (backend *Store) preferenceCollection() *mongo.Collection {
	return backend.database.Collection(mongoPreferenceCollectionName)
}

func (transaction *documentTransaction) preferenceCollection() *mongo.Collection {
	return transaction.store.preferenceCollection()
}

func (transaction *documentTransaction) deletePreferenceState(ctx context.Context, reference store.DocumentReference) error {
	_, err := transaction.preferenceCollection().DeleteMany(ctx, bson.D{
		{Key: "collection", Value: string(reference.CollectionID)},
		{Key: "user", Value: reference.DocumentID},
	})
	return translateMongoError(ctx, err)
}

func mongoSystemIdentityCollision(scope string) error {
	return fmt.Errorf("stored MongoDB %s has an invalid deterministic ID or logical identity collision: %w", scope, store.ErrConflict)
}

var _ store.PreferenceStore = (*Store)(nil)
