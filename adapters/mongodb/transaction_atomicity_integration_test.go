package mongodb

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBCrossCapabilityTransactionAtomicity(t *testing.T) {
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{
		Name:  "MongoDB cross-capability transaction",
		Admin: ridu.AdminConfig{User: "atomic-users"},
		Collections: []ridu.Collection{
			{Slug: "atomic-targets", Fields: field.Fields{field.Text("name").Required()}},
			{
				Slug: "atomic-users", Auth: true, Versions: true,
				Fields: field.Fields{field.Email("email").Required().Unique(), field.Relationship("favorite", "atomic-targets"), field.Text("note")},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(manifest.Snapshot().Collections)
	targets := collections["atomic-targets"]
	users := collections["atomic-users"]

	const userID = "atomic-user"
	oldIdentity := "old-atomic-user@example.test"
	newIdentity := "new-atomic-user@example.test"
	oldHash := []byte("old-atomic-password")
	newHash := []byte("new-atomic-password")

	seed := mongoBegin(t, backend, false)
	oldTarget, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: targets, ID: "old-target", Values: store.Values{"name": store.String("Old target")},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	newTarget, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: targets, ID: "new-target", Values: store.Values{"name": store.String("New target")},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	original, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: users, ID: userID,
		Values: store.Values{
			"email": store.String(oldIdentity), "favorite": store.String(oldTarget.ID),
			"note": store.String("old revision one"),
		},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := seed.(store.VersionTransaction).SaveVersion(t.Context(), users, original, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	original, err = seed.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: users, ID: userID, ExpectedRevision: original.Revision},
		Values:  store.Values{"note": store.String("old revision two")},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := seed.(store.VersionTransaction).SaveVersion(t.Context(), users, original, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if err := seed.(store.AuthTransaction).CreateAuthCredential(t.Context(), users, original.ID, oldHash, true); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	mongoCommit(t, seed)

	preference := store.Preference{
		CollectionID: users.ID, UserID: userID, Key: "atomic-state",
		Value: json.RawMessage(`{"preserved":true}`),
	}
	if _, err := backend.SetPreference(t.Context(), preference); err != nil {
		t.Fatal(err)
	}

	stageReplacement := func() store.Transaction {
		t.Helper()
		transaction := mongoBegin(t, backend, false)
		if _, err := transaction.Delete(t.Context(), store.Request{Collection: users, ID: userID}); err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		if err := transaction.DeleteDocumentState(t.Context(), store.DocumentReference{
			CollectionID: users.ID, DocumentID: userID,
		}); err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		clearedVersions, err := transaction.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{
			Collection: users, DocumentID: userID,
		})
		if err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		if len(clearedVersions) != 0 {
			mongoRollback(t, transaction)
			t.Fatalf("MongoDB permanent state cleanup retained versions before same-ID recreation: %#v", clearedVersions)
		}
		replacement, err := transaction.Create(t.Context(), store.CreateRequest{
			Collection: users, ID: userID,
			Values: store.Values{
				"email": store.String(newIdentity), "favorite": store.String(newTarget.ID),
				"note": store.String("new revision one"),
			},
		})
		if err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		if _, err := transaction.(store.VersionTransaction).SaveVersion(t.Context(), users, replacement, 10); err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		if err := transaction.(store.AuthTransaction).CreateAuthCredential(t.Context(), users, replacement.ID, newHash, false); err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		return transaction
	}

	rolledBack := stageReplacement()
	mongoRollback(t, rolledBack)
	assertMongoAtomicUserState(
		t, backend, users, targets.ID, userID, oldIdentity, oldTarget.ID, "old revision two", oldHash, true, 2,
	)
	assertMongoAtomicPreferenceState(t, backend, preference, true)
	assertMongoAtomicUniqueClaim(t, backend, users, "old-claim-probe", oldIdentity, oldTarget.ID, true)
	assertMongoAtomicUniqueClaim(t, backend, users, "new-claim-probe", newIdentity, newTarget.ID, false)

	committed := stageReplacement()
	mongoCommit(t, committed)
	assertMongoAtomicUserState(
		t, backend, users, targets.ID, userID, newIdentity, newTarget.ID, "new revision one", newHash, false, 1,
	)
	assertMongoAtomicPreferenceState(t, backend, preference, false)
	assertMongoAtomicUniqueClaim(t, backend, users, "new-conflict-probe", newIdentity, newTarget.ID, true)
	assertMongoAtomicUniqueClaim(t, backend, users, "old-released-probe", oldIdentity, oldTarget.ID, false)
}

func assertMongoAtomicUserState(
	t *testing.T,
	backend *Store,
	collection schema.Collection,
	targetCollectionID schema.StableID,
	documentID string,
	identity string,
	targetID string,
	note string,
	passwordHash []byte,
	verified bool,
	expectedVersionCount int,
) {
	t.Helper()
	read := mongoBegin(t, backend, true)
	document, err := read.Find(t.Context(), store.Request{Collection: collection, ID: documentID})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	versions, err := read.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{
		Collection: collection, DocumentID: documentID,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	mongoCommit(t, read)

	documentIdentity, _ := document.Values["email"].StringValue()
	documentTarget, _ := document.Values["favorite"].StringValue()
	documentNote, _ := document.Values["note"].StringValue()
	if document.ID != documentID || documentIdentity != identity || documentTarget != targetID || documentNote != note {
		t.Fatalf("MongoDB atomic document = %#v, want identity %q, target %q, and note %q", document, identity, targetID, note)
	}
	if document.Revision != expectedVersionCount || len(versions) != expectedVersionCount {
		t.Fatalf("MongoDB atomic versions = document revision %d and %#v, want %d retained revisions", document.Revision, versions, expectedVersionCount)
	}
	for index, version := range versions {
		if version.DocumentID != documentID || version.Revision != expectedVersionCount-index {
			t.Fatalf("MongoDB atomic version order = %#v, want descending contiguous history", versions)
		}
	}
	versionIdentity, _ := versions[0].Snapshot.Values["email"].StringValue()
	versionTarget, _ := versions[0].Snapshot.Values["favorite"].StringValue()
	versionNote, _ := versions[0].Snapshot.Values["note"].StringValue()
	if versionIdentity != identity || versionTarget != targetID || versionNote != note {
		t.Fatalf("MongoDB atomic version snapshot = %#v, want identity %q, target %q, and note %q", versions[0], identity, targetID, note)
	}

	count := func(collectionName string, filter bson.D) int64 {
		t.Helper()
		result, err := backend.database.Collection(collectionName).CountDocuments(t.Context(), filter)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	ownerFilter := bson.D{
		{Key: "ownerCollection", Value: string(collection.ID)},
		{Key: "ownerDocument", Value: documentID},
	}
	expectedReferenceFilter := append(append(bson.D(nil), ownerFilter...),
		bson.E{Key: "targetCollection", Value: string(targetCollectionID)},
		bson.E{Key: "targetDocument", Value: targetID},
	)
	if referenceCount := count(mongoReferenceCollectionName, ownerFilter); referenceCount != 1 {
		t.Fatalf("MongoDB atomic reference count = %d, want 1", referenceCount)
	}
	if expectedReferenceCount := count(mongoReferenceCollectionName, expectedReferenceFilter); expectedReferenceCount != 1 {
		t.Fatalf("MongoDB atomic reference target count = %d, want 1", expectedReferenceCount)
	}
	if versionCount := count(physicalVersionCollectionName(collection.ID), bson.D{{Key: mongoVersionOwnerPath, Value: documentID}}); versionCount != int64(expectedVersionCount) {
		t.Fatalf("MongoDB atomic raw version count = %d, want %d", versionCount, expectedVersionCount)
	}
	if credentialCount := count(mongoAuthCredentialCollectionName, mongoAuthOwnerFilter(collection.ID, documentID)); credentialCount != 1 {
		t.Fatalf("MongoDB atomic credential count = %d, want 1", credentialCount)
	}

	credential, err := backend.FindAuthCredential(t.Context(), collection, identity)
	if err != nil || credential.User.ID != documentID || credential.Verified != verified || !bytes.Equal(credential.PasswordHash, passwordHash) {
		t.Fatalf("MongoDB atomic credential = %#v, %v", credential, err)
	}
}

func assertMongoAtomicPreferenceState(t *testing.T, backend *Store, preference store.Preference, wantPresent bool) {
	t.Helper()
	stored, err := backend.GetPreference(t.Context(), preference.CollectionID, preference.UserID, preference.Key)
	if !wantPresent {
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted MongoDB atomic preference = %#v, %v", stored, err)
		}
		return
	}
	if err != nil || !bytes.Equal(stored.Value, preference.Value) {
		t.Fatalf("preserved MongoDB atomic preference = %#v, %v", stored, err)
	}
}

func assertMongoAtomicUniqueClaim(
	t *testing.T,
	backend *Store,
	collection schema.Collection,
	probeID string,
	identity string,
	targetID string,
	wantConflict bool,
) {
	t.Helper()
	transaction := mongoBegin(t, backend, false)
	_, err := transaction.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: probeID,
		Values: store.Values{"email": store.String(identity), "favorite": store.String(targetID)},
	})
	if wantConflict {
		if !errors.Is(err, store.ErrConflict) {
			mongoRollback(t, transaction)
			t.Fatalf("MongoDB unique claim for %q = %v, want ErrConflict", identity, err)
		}
	} else if err != nil {
		mongoRollback(t, transaction)
		t.Fatalf("MongoDB released unique claim for %q = %v", identity, err)
	}
	mongoRollback(t, transaction)
}
