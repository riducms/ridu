package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoReferenceCollectionName = "z_ridu_document_references"

type mongoReferenceEntry struct {
	ID         string
	Owner      store.DocumentReference
	FieldID    schema.StableID
	Target     store.DocumentReference
	Locale     schema.LocaleCode
	Occurrence int
}

func (transaction *documentTransaction) replaceDocumentReferences(ctx context.Context, collection schema.Collection, document store.Document) error {
	if !mongoCollectionHasRelationships(collection) {
		return nil
	}
	owner := store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}
	if err := transaction.deleteDocumentReferences(ctx, owner); err != nil {
		return err
	}
	entries := referenceindex.Collect(collection, document)
	if len(entries) == 0 {
		return nil
	}
	documents := make([]any, len(entries))
	for index, entry := range entries {
		encoded, err := encodeMongoReferenceEntry(entry)
		if err != nil {
			return err
		}
		documents[index] = encoded
	}
	if _, err := transaction.referenceCollection().InsertMany(ctx, documents, options.InsertMany().SetOrdered(true)); err != nil {
		return translateMongoError(ctx, err)
	}
	return nil
}

func (transaction *documentTransaction) deleteDocumentReferences(ctx context.Context, owner store.DocumentReference) error {
	if _, err := transaction.referenceCollection().DeleteMany(ctx, bson.D{
		{Key: "ownerCollection", Value: string(owner.CollectionID)},
		{Key: "ownerDocument", Value: owner.DocumentID},
	}); err != nil {
		return translateMongoError(ctx, err)
	}
	return nil
}

func (transaction *documentTransaction) deleteDocumentReferenceState(ctx context.Context, reference store.DocumentReference) error {
	filter := bson.D{{Key: "$or", Value: bson.A{
		bson.D{
			{Key: "ownerCollection", Value: string(reference.CollectionID)},
			{Key: "ownerDocument", Value: reference.DocumentID},
		},
		bson.D{
			{Key: "targetCollection", Value: string(reference.CollectionID)},
			{Key: "targetDocument", Value: reference.DocumentID},
		},
	}}}
	if _, err := transaction.referenceCollection().DeleteMany(ctx, filter); err != nil {
		return translateMongoError(ctx, err)
	}
	return nil
}

func (transaction *documentTransaction) referenceCollection() *mongo.Collection {
	return transaction.store.database.Collection(mongoReferenceCollectionName)
}

func encodeMongoReferenceEntry(entry referenceindex.Entry) (bson.D, error) {
	if err := validateMongoReferenceIdentity(entry.Owner, "owner"); err != nil {
		return nil, err
	}
	if err := validateMongoReferenceIdentity(entry.Target, "target"); err != nil {
		return nil, err
	}
	if !schema.IsValidStableID(string(entry.FieldID)) {
		return nil, fmt.Errorf("MongoDB reference field ID is invalid")
	}
	if entry.Locale != "" && !schema.IsValidLocaleCode(string(entry.Locale)) {
		return nil, fmt.Errorf("MongoDB reference locale is invalid")
	}
	if entry.Occurrence < 0 {
		return nil, fmt.Errorf("MongoDB reference occurrence is invalid")
	}
	identity := mongoReferenceIdentity(entry)
	return bson.D{
		{Key: "_id", Value: identity},
		{Key: "codec", Value: int32(1)},
		{Key: "ownerCollection", Value: string(entry.Owner.CollectionID)},
		{Key: "ownerDocument", Value: entry.Owner.DocumentID},
		{Key: "field", Value: string(entry.FieldID)},
		{Key: "targetCollection", Value: string(entry.Target.CollectionID)},
		{Key: "targetDocument", Value: entry.Target.DocumentID},
		{Key: "locale", Value: string(entry.Locale)},
		{Key: "occurrence", Value: int64(entry.Occurrence)},
	}, nil
}

func mongoReferenceIdentity(entry referenceindex.Entry) string {
	encoded := string(entry.Owner.CollectionID) + "\x00" + entry.Owner.DocumentID + "\x00" +
		string(entry.FieldID) + "\x00" + string(entry.Target.CollectionID) + "\x00" +
		entry.Target.DocumentID + "\x00" + string(entry.Locale) + "\x00" + fmt.Sprint(entry.Occurrence)
	digest := sha256.Sum256([]byte(encoded))
	return "r_" + hex.EncodeToString(digest[:])
}

func decodeMongoReferenceEntry(raw bson.Raw) (mongoReferenceEntry, error) {
	if err := requireExactKeys(raw, "MongoDB reference index entry", "_id", "codec", "ownerCollection", "ownerDocument", "field", "targetCollection", "targetDocument", "locale", "occurrence"); err != nil {
		return mongoReferenceEntry{}, err
	}
	stringValue := func(key string) (string, error) {
		value, valid := raw.Lookup(key).StringValueOK()
		if !valid {
			return "", fmt.Errorf("stored MongoDB reference index entry has invalid %s", key)
		}
		return value, nil
	}
	id, err := stringValue("_id")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	codec, valid := raw.Lookup("codec").Int32OK()
	if !valid || codec != 1 {
		return mongoReferenceEntry{}, fmt.Errorf("stored MongoDB reference index entry has an unsupported codec version")
	}
	ownerCollection, err := stringValue("ownerCollection")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	ownerDocument, err := stringValue("ownerDocument")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	fieldID, err := stringValue("field")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	targetCollection, err := stringValue("targetCollection")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	targetDocument, err := stringValue("targetDocument")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	locale, err := stringValue("locale")
	if err != nil {
		return mongoReferenceEntry{}, err
	}
	if locale != "" && !schema.IsValidLocaleCode(locale) {
		return mongoReferenceEntry{}, fmt.Errorf("stored MongoDB reference index entry has invalid localized scope")
	}
	occurrence64, valid := raw.Lookup("occurrence").Int64OK()
	if !valid || occurrence64 < 0 || int64(int(occurrence64)) != occurrence64 {
		return mongoReferenceEntry{}, fmt.Errorf("stored MongoDB reference index entry has invalid occurrence")
	}
	entry := mongoReferenceEntry{
		ID: id,
		Owner: store.DocumentReference{
			CollectionID: schema.StableID(ownerCollection), DocumentID: ownerDocument,
		},
		FieldID: schema.StableID(fieldID),
		Target: store.DocumentReference{
			CollectionID: schema.StableID(targetCollection), DocumentID: targetDocument,
		},
		Locale: schema.LocaleCode(locale), Occurrence: int(occurrence64),
	}
	if err := validateMongoReferenceIdentity(entry.Owner, "stored owner"); err != nil {
		return mongoReferenceEntry{}, err
	}
	if err := validateMongoReferenceIdentity(entry.Target, "stored target"); err != nil {
		return mongoReferenceEntry{}, err
	}
	if !schema.IsValidStableID(string(entry.FieldID)) {
		return mongoReferenceEntry{}, fmt.Errorf("stored MongoDB reference index entry has invalid field ID")
	}
	expected := mongoReferenceIdentity(referenceindex.Entry{
		Owner: entry.Owner, FieldID: entry.FieldID, Target: entry.Target,
		Locale: entry.Locale, Occurrence: entry.Occurrence,
	})
	if entry.ID != expected {
		return mongoReferenceEntry{}, fmt.Errorf("stored MongoDB reference index entry has an invalid identity")
	}
	return entry, nil
}

func validateMongoReferenceIdentity(reference store.DocumentReference, role string) error {
	if !schema.IsValidStableID(string(reference.CollectionID)) {
		return fmt.Errorf("MongoDB reference %s collection ID is invalid", role)
	}
	if err := store.ValidateDocumentID(reference.DocumentID); err != nil {
		return fmt.Errorf("MongoDB reference %s document ID: %w", role, err)
	}
	return nil
}

func (transaction *documentTransaction) incomingReferenceEntries(ctx context.Context, target store.DocumentReference) ([]mongoReferenceEntry, error) {
	cursor, err := transaction.referenceCollection().Find(ctx, bson.D{
		{Key: "targetCollection", Value: string(target.CollectionID)},
		{Key: "targetDocument", Value: target.DocumentID},
	}, options.Find().SetSort(bson.D{
		{Key: "ownerCollection", Value: int32(1)},
		{Key: "ownerDocument", Value: int32(1)},
		{Key: "field", Value: int32(1)},
		{Key: "locale", Value: int32(1)},
		{Key: "occurrence", Value: int32(1)},
	}))
	if err != nil {
		return nil, translateMongoError(ctx, err)
	}
	defer transaction.closeCursor(cursor)
	entries := make([]mongoReferenceEntry, 0)
	for cursor.Next(ctx) {
		entry, decodeErr := decodeMongoReferenceEntry(cursor.Current)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if entry.Target != target {
			return nil, fmt.Errorf("stored MongoDB reference index entry does not match its target lookup")
		}
		entries = append(entries, entry)
	}
	if err := cursor.Err(); err != nil {
		return nil, translateMongoError(ctx, err)
	}
	return entries, nil
}

func mongoReferenceConstraints(entries []mongoReferenceEntry, collections map[schema.StableID]schema.Collection, ignored map[store.DocumentReference]struct{}) ([]store.ReferenceConstraint, error) {
	constraints := make(map[store.ReferenceConstraint]struct{})
	for _, entry := range entries {
		if _, skip := ignored[entry.Owner]; skip {
			continue
		}
		collection, exists := collections[entry.Owner.CollectionID]
		if !exists {
			return nil, fmt.Errorf("reference owner collection %q is unavailable", entry.Owner.CollectionID)
		}
		field, _, exists := referenceindex.FindReferenceField(collection, entry.FieldID)
		reference := mongoReferenceDetails(field)
		if !exists || reference == nil {
			return nil, fmt.Errorf("reference owner field %q is unavailable", entry.FieldID)
		}
		switch reference.OnDelete {
		case schema.ReferenceDeleteNullify:
		case schema.ReferenceDeleteRestrict:
			constraints[store.ReferenceConstraint{OwnerCollectionID: entry.Owner.CollectionID, FieldID: entry.FieldID}] = struct{}{}
		default:
			return nil, fmt.Errorf("reference owner field %q has unsupported delete action %q", entry.FieldID, reference.OnDelete)
		}
	}
	ordered := make([]store.ReferenceConstraint, 0, len(constraints))
	for constraint := range constraints {
		ordered = append(ordered, constraint)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].OwnerCollectionID != ordered[right].OwnerCollectionID {
			return ordered[left].OwnerCollectionID < ordered[right].OwnerCollectionID
		}
		return ordered[left].FieldID < ordered[right].FieldID
	})
	return ordered, nil
}

func (transaction *documentTransaction) applyReferenceDelete(ctx context.Context, request store.ReferenceDeleteRequest) error {
	if err := validateMongoReferenceIdentity(request.Target, "delete target"); err != nil {
		return err
	}
	for _, collection := range request.Collections {
		if err := validateCollectionEnvelope(collection); err != nil {
			return fmt.Errorf("reference delete collection %q: %w", collection.ID, err)
		}
		if err := transaction.store.requireVerifiedIndexes(collection); err != nil {
			return fmt.Errorf("reference delete collection %q: %w", collection.ID, err)
		}
		if mongoCollectionHasRelationships(collection) {
			if err := transaction.store.requireVerifiedReferenceIndexes(collection); err != nil {
				return fmt.Errorf("reference delete collection %q: %w", collection.ID, err)
			}
		}
	}

	ignored := make(map[store.DocumentReference]struct{}, len(request.IgnoreOwners))
	for _, owner := range request.IgnoreOwners {
		if err := validateMongoReferenceIdentity(owner, "ignored owner"); err != nil {
			return err
		}
		if owner == request.Target {
			ignored[owner] = struct{}{}
			continue
		}
		collection, exists := request.Collections[owner.CollectionID]
		if !exists {
			return fmt.Errorf("ignored reference owner collection %q is unavailable", owner.CollectionID)
		}
		raw, err := transaction.collection(collection).FindOne(ctx, bson.D{{Key: mongoIDPath, Value: owner.DocumentID}}).Raw()
		switch {
		case errors.Is(err, mongo.ErrNoDocuments):
			ignored[owner] = struct{}{}
		case err != nil:
			return translateMongoError(ctx, err)
		default:
			if _, decodeErr := decodeCollectionDocument(raw, collection); decodeErr != nil {
				return decodeErr
			}
		}
	}

	entries, err := transaction.incomingReferenceEntries(ctx, request.Target)
	if err != nil {
		return err
	}
	constraints, err := mongoReferenceConstraints(entries, request.Collections, ignored)
	if err != nil {
		return err
	}
	if len(constraints) != 0 {
		return &store.DeleteRestrictedError{Constraints: constraints}
	}

	owners := make(map[store.DocumentReference]struct{}, len(entries))
	for _, entry := range entries {
		if _, skip := ignored[entry.Owner]; !skip {
			owners[entry.Owner] = struct{}{}
		}
	}
	orderedOwners := make([]store.DocumentReference, 0, len(owners))
	for owner := range owners {
		orderedOwners = append(orderedOwners, owner)
	}
	sort.Slice(orderedOwners, func(left, right int) bool {
		if orderedOwners[left].CollectionID != orderedOwners[right].CollectionID {
			return orderedOwners[left].CollectionID < orderedOwners[right].CollectionID
		}
		return orderedOwners[left].DocumentID < orderedOwners[right].DocumentID
	})

	type ownerMutation struct {
		owner      store.DocumentReference
		collection schema.Collection
		values     bson.D
	}
	mutations := make([]ownerMutation, 0, len(orderedOwners))
	staleOwners := make([]store.DocumentReference, 0)
	for _, owner := range orderedOwners {
		collection := request.Collections[owner.CollectionID]
		raw, findErr := transaction.collection(collection).FindOne(ctx, bson.D{{Key: mongoIDPath, Value: owner.DocumentID}}).Raw()
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			staleOwners = append(staleOwners, owner)
			continue
		}
		if findErr != nil {
			return translateMongoError(ctx, findErr)
		}
		document, decodeErr := decodeCollectionDocument(raw, collection)
		if decodeErr != nil {
			return decodeErr
		}
		values, changed := referenceindex.NullifyTarget(collection, document.Values, request.Target)
		if !changed {
			return fmt.Errorf("reference index for owner collection %q is inconsistent with current values", owner.CollectionID)
		}
		if err := validateCompleteValues(collection, values); err != nil {
			return fmt.Errorf("nullified MongoDB owner %q: %w", owner.CollectionID, err)
		}
		encoded, err := encodeValues(values)
		if err != nil {
			return err
		}
		mutations = append(mutations, ownerMutation{owner: owner, collection: collection, values: encoded})
	}

	// No current document changes until every restriction, derived-index row,
	// owner shape, and resulting value has been validated. All writes below
	// remain in the caller's MongoDB transaction.
	for _, owner := range staleOwners {
		if err := transaction.deleteDocumentReferences(ctx, owner); err != nil {
			return err
		}
	}
	for _, mutation := range mutations {
		updatedRaw, err := transaction.collection(mutation.collection).FindOneAndUpdate(
			ctx,
			mongoAnd([]bson.D{
				{{Key: mongoIDPath, Value: mutation.owner.DocumentID}},
				{{Key: mongoCodecPath, Value: int32(1)}},
				mongoTypeGuard(mongoFencePath, "long"),
			}),
			bson.D{{Key: "$set", Value: bson.D{{Key: "values", Value: mutation.values}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return translateMongoError(ctx, err)
		}
		updated, err := decodeCollectionDocument(updatedRaw, mutation.collection)
		if err != nil {
			return err
		}
		if err := transaction.replaceDocumentReferences(ctx, mutation.collection, updated); err != nil {
			return err
		}
	}
	return nil
}
