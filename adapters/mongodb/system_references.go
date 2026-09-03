package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (backend *Store) prepareSystemOperation(ctx context.Context, capability string) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB %s context is required", capability)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if backend == nil || backend.database == nil || backend.client == nil {
		return fmt.Errorf("MongoDB database is unavailable")
	}
	backend.closeMu.Lock()
	closed := backend.closed
	backend.closeMu.Unlock()
	if closed {
		return fmt.Errorf("MongoDB database is closed")
	}
	return nil
}

func rollbackSystemTransaction(transaction *documentTransaction) {
	if transaction == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	_ = transaction.Rollback(ctx)
}

func (backend *Store) runSystemTransaction(
	ctx context.Context,
	operation func(*documentTransaction) error,
) error {
	backoff := time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		transaction, err := backend.begin(ctx, false)
		if err != nil {
			return err
		}
		err = operation(transaction)
		if err != nil {
			rollbackSystemTransaction(transaction)
		} else {
			err = transaction.Commit(ctx)
		}
		if err == nil {
			return nil
		}
		if !isMongoConfirmedTransactionConflict(err) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 25*time.Millisecond {
			backoff *= 2
			if backoff > 25*time.Millisecond {
				backoff = 25 * time.Millisecond
			}
		}
	}
}

func normalizeMongoSystemTime(value time.Time, scope string) (time.Time, int64, error) {
	value = value.UTC()
	encoded, err := encodeTime(value)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("MongoDB %s: %w", scope, err)
	}
	return decodeTime(encoded), encoded, nil
}

// mongoSystemRecordID gives focused framework collections compact stable
// primary keys without making their logical identities depend on BSON field
// ordering. Length-prefixing keeps component boundaries unambiguous; callers
// must still compare the decoded logical identity so even a hash collision
// fails closed instead of reading, overwriting, or deleting another row.
func mongoSystemRecordID(namespace string, components ...string) string {
	digest := sha256.New()
	writeMongoSystemIdentityPart(digest, namespace)
	for _, component := range components {
		writeMongoSystemIdentityPart(digest, component)
	}
	return "z_" + namespace + "_" + hex.EncodeToString(digest.Sum(nil))
}

type mongoIdentityDigest interface {
	Write([]byte) (int, error)
}

func writeMongoSystemIdentityPart(digest mongoIdentityDigest, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}

func validateMongoSystemReference(reference store.DocumentReference, scope string) error {
	if !schema.IsValidStableID(string(reference.CollectionID)) {
		return fmt.Errorf("MongoDB %s collection ID is invalid", scope)
	}
	if err := store.ValidateDocumentID(reference.DocumentID); err != nil {
		return fmt.Errorf("MongoDB %s document ID: %w", scope, err)
	}
	return nil
}

func validateMongoSystemString(value, scope string) error {
	if !utf8.ValidString(value) || stringsContainNUL(value) {
		return fmt.Errorf("MongoDB %s must be valid UTF-8 without NUL", scope)
	}
	return nil
}

type mongoActiveDocumentReference struct {
	reference    store.DocumentReference
	physicalName string
	incarnation  string
}

func orderMongoSystemReferences(references ...store.DocumentReference) ([]mongoActiveDocumentReference, error) {
	unique := make(map[string]mongoActiveDocumentReference, len(references))
	for _, reference := range references {
		if err := validateMongoSystemReference(reference, "system-state reference"); err != nil {
			return nil, err
		}
		physicalName := physicalCollectionName(reference.CollectionID)
		key := physicalName + "\x00" + string(reference.CollectionID) + "\x00" + reference.DocumentID
		unique[key] = mongoActiveDocumentReference{reference: reference, physicalName: physicalName}
	}
	ordered := make([]mongoActiveDocumentReference, 0, len(unique))
	for _, reference := range unique {
		ordered = append(ordered, reference)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].physicalName != ordered[right].physicalName {
			return ordered[left].physicalName < ordered[right].physicalName
		}
		if ordered[left].reference.DocumentID != ordered[right].reference.DocumentID {
			return ordered[left].reference.DocumentID < ordered[right].reference.DocumentID
		}
		return ordered[left].reference.CollectionID < ordered[right].reference.CollectionID
	})
	return ordered, nil
}

// captureActiveDocumentReferences records the immutable incarnation of every
// active system-state owner before the first transaction attempt. Retries keep
// these exact tokens, so ordinary write contention can serialize without ever
// attaching state to a document that was deleted and recreated with the same
// public ID.
func (backend *Store) captureActiveDocumentReferences(
	ctx context.Context,
	references ...store.DocumentReference,
) ([]mongoActiveDocumentReference, error) {
	ordered, err := orderMongoSystemReferences(references...)
	if err != nil {
		return nil, err
	}
	for _, item := range ordered {
		if err := backend.requireVerifiedResourceID(item.reference.CollectionID); err != nil {
			return nil, err
		}
	}
	for index := range ordered {
		item := &ordered[index]
		raw, findErr := backend.database.Collection(item.physicalName).FindOne(
			ctx,
			bson.D{{Key: mongoIDPath, Value: item.reference.DocumentID}},
		).Raw()
		if findErr != nil {
			return nil, translateMongoError(ctx, findErr)
		}
		document, decodeErr := decodeDocument(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if document.DeletedAt != nil {
			return nil, store.ErrNotFound
		}
		item.incarnation, decodeErr = decodeDocumentIncarnation(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
	}
	return ordered, nil
}

// fenceActiveDocumentReferences joins auxiliary state to the same immutable
// content incarnations captured before the first transaction attempt. Every
// active reference is fenced in deterministic physical order inside each
// attempt. A changed, trashed, or deleted incarnation is a semantic conflict,
// while a server-confirmed write conflict remains eligible for retry.
func (transaction *documentTransaction) fenceActiveDocumentReferences(
	ctx context.Context,
	references []mongoActiveDocumentReference,
) error {
	for _, item := range references {
		if err := transaction.store.requireVerifiedResourceID(item.reference.CollectionID); err != nil {
			return err
		}
	}
	for _, item := range references {
		collection := transaction.store.database.Collection(item.physicalName)
		predicate := mongoAnd([]bson.D{
			{{Key: mongoIDPath, Value: item.reference.DocumentID}},
			mongoExactNullPredicate(mongoDeletedAtPath),
			mongoTypeGuard(mongoFencePath, "long"),
			mongoTypeGuard(mongoIncarnationPath, "string"),
			{{Key: mongoIncarnationPath, Value: item.incarnation}},
		})
		raw, err := collection.FindOneAndUpdate(
			ctx,
			predicate,
			bson.D{{Key: "$inc", Value: bson.D{{Key: mongoFencePath, Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err == nil {
			incarnation, decodeErr := decodeDocumentIncarnation(raw)
			if decodeErr != nil {
				return decodeErr
			}
			if incarnation != item.incarnation {
				return fmt.Errorf("MongoDB system-state reference changed incarnation: %w", store.ErrConflict)
			}
			continue
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return translateMongoError(ctx, err)
		}

		// Do not let corrupt lifecycle metadata masquerade as a changed
		// incarnation. Once capture succeeds, every lifecycle change is a
		// semantic conflict rather than a fresh not-found admission decision.
		candidate, probeErr := collection.FindOne(ctx, bson.D{{Key: mongoIDPath, Value: item.reference.DocumentID}}).Raw()
		switch {
		case probeErr == nil:
			if _, decodeErr := decodeDocumentIncarnation(candidate); decodeErr != nil {
				return decodeErr
			}
			return fmt.Errorf("MongoDB system-state reference changed incarnation: %w", store.ErrConflict)
		case errors.Is(probeErr, mongo.ErrNoDocuments):
			return fmt.Errorf("MongoDB system-state reference was deleted: %w", store.ErrConflict)
		default:
			return translateMongoError(ctx, probeErr)
		}
	}
	return nil
}
