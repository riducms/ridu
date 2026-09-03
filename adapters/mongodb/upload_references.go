package mongodb

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ReferencedUploadObjects performs a bounded, access-independent lookup over
// current/trash documents and every retained immutable version. It probes one
// requested key at a time so both the reply and strict-decoding work remain
// bounded even when one object is referenced by many documents.
func (transaction *documentTransaction) ReferencedUploadObjects(ctx context.Context, request store.UploadReferenceRequest) ([]string, error) {
	sessionContext, leave, err := transaction.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	defer leave()
	keys, collections, err := validateMongoUploadReferenceRequest(request)
	if err != nil {
		return nil, err
	}
	for _, collection := range collections {
		if err := transaction.store.requireVerifiedIndexes(collection); err != nil {
			return nil, fmt.Errorf("upload reference collection %q: %w", collection.ID, err)
		}
		if err := transaction.store.requireVerifiedVersionIndexes(collection); err != nil {
			return nil, fmt.Errorf("upload reference collection %q: %w", collection.ID, err)
		}
	}

	referenced := make([]string, 0, len(keys))
	for _, key := range keys {
		found := false
		for _, collection := range collections {
			raw, findErr := transaction.collection(collection).FindOne(
				sessionContext,
				mongoUploadReferenceFilter("$values", key),
				options.FindOne().SetSort(bson.D{{Key: mongoIDPath, Value: int32(1)}}),
			).Raw()
			switch {
			case findErr == nil:
				document, decodeErr := decodeCollectionDocument(raw, collection)
				if decodeErr != nil {
					return nil, fmt.Errorf("decode matching MongoDB upload document: %w", decodeErr)
				}
				if !mongoUploadValuesReference(document.Values, key) {
					return nil, fmt.Errorf("MongoDB upload document did not confirm its matched object reference")
				}
				found = true
			case !errors.Is(findErr, mongo.ErrNoDocuments):
				return nil, translateMongoError(ctx, findErr)
			}
			if found || collection.Versions == nil {
				if found {
					break
				}
				continue
			}

			versionRaw, findErr := transaction.versionCollection(collection).FindOne(
				sessionContext,
				mongoUploadReferenceFilter("$"+mongoVersionSnapshotPath+".values", key),
				options.FindOne().SetSort(bson.D{{Key: mongoVersionRevisionPath, Value: int32(-1)}, {Key: "_id", Value: int32(1)}}),
			).Raw()
			switch {
			case findErr == nil:
				version, decodeErr := decodeMongoVersion(versionRaw, collection)
				if decodeErr != nil {
					return nil, fmt.Errorf("decode matching MongoDB upload version: %w", decodeErr)
				}
				if !mongoUploadValuesReference(version.Snapshot.Values, key) {
					return nil, fmt.Errorf("MongoDB upload version did not confirm its matched object reference")
				}
				found = true
			case !errors.Is(findErr, mongo.ErrNoDocuments):
				return nil, translateMongoError(ctx, findErr)
			}
			if found {
				break
			}
		}
		if found {
			referenced = append(referenced, key)
		}
	}
	return referenced, nil
}

func validateMongoUploadReferenceRequest(request store.UploadReferenceRequest) ([]string, []schema.Collection, error) {
	if len(request.ObjectKeys) == 0 || len(request.ObjectKeys) > store.MaxUploadReferenceCandidates {
		return nil, nil, fmt.Errorf("upload reference lookup requires between 1 and %d object keys", store.MaxUploadReferenceCandidates)
	}
	seenKeys := make(map[string]struct{}, len(request.ObjectKeys))
	keys := make([]string, 0, len(request.ObjectKeys))
	for _, key := range request.ObjectKeys {
		if key == "" || !utf8.ValidString(key) || stringsContainNUL(key) {
			return nil, nil, fmt.Errorf("upload reference lookup contains an invalid object key")
		}
		if _, duplicate := seenKeys[key]; duplicate {
			return nil, nil, fmt.Errorf("upload reference lookup contains a duplicate object key")
		}
		seenKeys[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	collections := append([]schema.Collection(nil), request.Collections...)
	sort.Slice(collections, func(left, right int) bool { return collections[left].ID < collections[right].ID })
	if len(collections) == 0 {
		return nil, nil, fmt.Errorf("upload reference lookup requires at least one upload collection")
	}
	seenCollections := make(map[schema.StableID]struct{}, len(collections))
	for _, collection := range collections {
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			return nil, nil, fmt.Errorf("upload reference lookup contains duplicate collection %q", collection.ID)
		}
		seenCollections[collection.ID] = struct{}{}
		if err := validateCollectionEnvelope(collection); err != nil {
			return nil, nil, fmt.Errorf("upload reference lookup collection %q: %w", collection.ID, err)
		}
		if collection.Upload == nil {
			return nil, nil, fmt.Errorf("upload reference lookup collection %q is not upload-enabled", collection.ID)
		}
	}
	return keys, collections, nil
}

func mongoUploadReferenceFilter(valuesExpression, key string) bson.D {
	objectKey := valuesExpression + ".objectKey"
	sizes := valuesExpression + ".sizes"
	literalKey := bson.D{{Key: "$literal", Value: key}}
	direct := bson.D{{Key: "$and", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: objectKey}}, "string"}}},
		bson.D{{Key: "$eq", Value: bson.A{objectKey, literalKey}}},
	}}}
	getVariantObjectKey := bson.D{{Key: "$getField", Value: bson.D{
		{Key: "field", Value: bson.D{{Key: "$literal", Value: "objectKey"}}},
		{Key: "input", Value: "$$riduUploadSize.v"},
	}}}
	variantMatches := bson.D{{Key: "$let", Value: bson.D{
		{Key: "vars", Value: bson.D{{Key: "riduUploadObjectKey", Value: getVariantObjectKey}}},
		{Key: "in", Value: bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$$riduUploadObjectKey"}}, "string"}}},
			bson.D{{Key: "$eq", Value: bson.A{"$$riduUploadObjectKey", literalKey}}},
		}}}},
	}}}
	mappedVariants := bson.D{{Key: "$map", Value: bson.D{
		{Key: "input", Value: bson.D{{Key: "$objectToArray", Value: sizes}}},
		{Key: "as", Value: "riduUploadSize"},
		{Key: "in", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$$riduUploadSize.v"}}, "object"}}},
			variantMatches,
			false,
		}}}},
	}}}
	variant := bson.D{{Key: "$cond", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: sizes}}, "object"}}},
		bson.D{{Key: "$anyElementTrue", Value: bson.A{mappedVariants}}},
		false,
	}}}
	return bson.D{{Key: "$expr", Value: bson.D{{Key: "$or", Value: bson.A{direct, variant}}}}}
}

func mongoUploadValuesReference(values store.Values, key string) bool {
	if objectKey, valid := values["objectKey"].StringValue(); valid && objectKey == key {
		return true
	}
	sizes, valid := values["sizes"].ObjectValue()
	if !valid {
		return false
	}
	for _, size := range sizes {
		metadata, valid := size.ObjectValue()
		if !valid {
			continue
		}
		if objectKey, valid := metadata["objectKey"].StringValue(); valid && objectKey == key {
			return true
		}
	}
	return false
}

var _ store.UploadReferenceTransaction = (*documentTransaction)(nil)
