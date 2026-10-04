package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// MongoDB's authored unique indexes cover each physical head independently.
// The working document also holds a unique multikey reservation for every
// authored unique tuple in either active head. MongoDB permits duplicate keys
// within one document but rejects the same reservation across document IDs.
func mongoHeadReservationKeys(collection schema.Collection, locales []schema.LocaleCode, heads ...store.Document) ([]string, error) {
	definitions, err := mongoDeclaredIndexesForLocales(collection, locales)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{})
	for _, head := range heads {
		if head.ID == "" || head.DeletedAt != nil {
			continue
		}
		values, err := encodeValues(head.Values)
		if err != nil {
			return nil, err
		}
		for _, definition := range definitions {
			if !definition.unique {
				continue
			}
			tuple := bson.D{}
			complete := true
			for _, key := range definition.keys {
				path := strings.TrimPrefix(key.Key, "values.")
				if path == key.Key {
					return nil, fmt.Errorf("MongoDB unique index %q has an unexpected storage path", definition.name)
				}
				value, exists := mongoHeadValueAtPath(values, strings.Split(path, "."))
				if !exists || value == nil {
					complete = false
					break
				}
				tuple = append(tuple, bson.E{Key: key.Key, Value: value})
			}
			if !complete {
				continue
			}
			encoded, err := bson.Marshal(tuple)
			if err != nil {
				return nil, err
			}
			digest := sha256.New()
			_, _ = digest.Write([]byte(definition.name))
			_, _ = digest.Write([]byte{0})
			_, _ = digest.Write(encoded)
			keys["u_"+hex.EncodeToString(digest.Sum(nil))] = struct{}{}
		}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func mongoHeadValueAtPath(values bson.D, segments []string) (any, bool) {
	for index, segment := range segments {
		found := false
		for _, entry := range values {
			if entry.Key != segment {
				continue
			}
			found = true
			if index == len(segments)-1 {
				return entry.Value, true
			}
			nested, ok := entry.Value.(bson.D)
			if !ok {
				return nil, false
			}
			values = nested
			break
		}
		if !found {
			return nil, false
		}
	}
	return nil, false
}

func (transaction *documentTransaction) replaceHeadReservations(ctx context.Context, collection schema.Collection, document store.Document, locales []schema.LocaleCode) error {
	if collection.Versions == nil {
		return nil
	}
	if len(locales) == 0 {
		plan, err := transaction.store.verifiedIndexPlan(collection)
		if err != nil {
			return err
		}
		locales = plan.locales
	}
	return transaction.replaceHeadReservationsForLocales(ctx, collection, document, locales)
}

// The development recovery path has an explicit before manifest, but may run
// while the adapter's serving index cache is intentionally invalidated.
func (transaction *documentTransaction) replaceHeadReservationsForLocales(ctx context.Context, collection schema.Collection, document store.Document, locales []schema.LocaleCode) error {
	if collection.Versions == nil {
		return nil
	}
	heads := []store.Document{document}
	if live, exists, err := transaction.publishedHead(ctx, collection, document.ID, locales); err != nil {
		return err
	} else if exists {
		heads = append(heads, live)
	}
	keys, err := mongoHeadReservationKeys(collection, locales, heads...)
	if err != nil {
		return err
	}
	update := bson.D{{Key: "$unset", Value: bson.D{{Key: "reservations", Value: ""}}}}
	if len(keys) != 0 {
		array := make(bson.A, len(keys))
		for index, key := range keys {
			array[index] = key
		}
		update = bson.D{{Key: "$set", Value: bson.D{{Key: "reservations", Value: array}}}}
	}
	result, err := transaction.collection(collection).UpdateOne(ctx,
		bson.D{{Key: "_id", Value: document.ID}},
		update)
	if err != nil {
		return translateMongoError(ctx, err)
	}
	if result.MatchedCount != 1 {
		return store.ErrNotFound
	}
	return nil
}
