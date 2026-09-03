package mongodb

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBUploadReferenceDeletePlansNullifiesVersionsAndRecreation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "MongoDB upload reference deletion",
		Collections: []ridu.Collection{
			{
				Slug: "media", Upload: true, Trash: true, Versions: true,
				UploadConfig: ridu.UploadConfig{
					MaxFileSize: 1024, MimeTypes: []string{"text/plain"},
					ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 16, Height: 16, Fit: "cover"}},
				},
				VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields:        []field.Definition{field.Text("alt")},
			},
			{
				Slug: "owners", Trash: true, Versions: true,
				VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Upload("guard", field.To("media"), field.OnDelete(field.ReferenceDeleteRestrict)),
					field.Upload("hero", field.To("media"), field.OnDelete(field.ReferenceDeleteNullify)),
					field.Group("content", field.Fields(
						field.Upload("gallery", field.ToMany("media"), field.OnDelete(field.ReferenceDeleteNullify)),
					)),
				},
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
	media := collections["media"]
	owners := collections["owners"]
	allCollections := map[schema.StableID]schema.Collection{media.ID: media, owners.ID: owners}
	target := store.DocumentReference{CollectionID: media.ID, DocumentID: "delete-target"}

	seed := mongoBegin(t, backend, false)
	if _, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: target.DocumentID, Status: store.StatusPublished,
		Values: mongoUploadValues("delete-original", "delete-variant"),
	}); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	other, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: "other-target", Status: store.StatusPublished,
		Values: mongoUploadValues("other-delete-original", "other-delete-variant"),
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	active, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: owners, ID: "active-owner", Status: store.StatusPublished,
		Values: store.Values{
			"title": store.String("Active"), "guard": store.String(target.DocumentID), "hero": store.String(target.DocumentID),
			"content": store.Object(store.Values{"gallery": store.List(
				store.String(target.DocumentID), store.String(target.DocumentID), store.String(other.ID),
			)}),
		},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := seed.(store.VersionTransaction).SaveVersion(t.Context(), owners, active, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	trashed, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: owners, ID: "trashed-owner", Status: store.StatusPublished,
		Values: store.Values{
			"title": store.String("Trashed"), "hero": store.String(target.DocumentID),
			"content": store.Object(store.Values{"gallery": store.List(
				store.String(target.DocumentID), store.String(target.DocumentID),
			)}),
		},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := seed.(store.VersionTransaction).SaveVersion(t.Context(), owners, trashed, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	trashed, err = seed.Trash(t.Context(), store.Request{Collection: owners, ID: trashed.ID})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	mongoCommit(t, seed)

	incomplete := mongoBegin(t, backend, false)
	err = incomplete.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{
		Target:      target,
		Collections: map[schema.StableID]schema.Collection{media.ID: media},
	})
	if err == nil || !strings.Contains(err.Error(), "reference owner collection") {
		mongoRollback(t, incomplete)
		t.Fatalf("reference delete with incomplete collection map = %v, want unavailable owner error", err)
	}
	mongoRollback(t, incomplete)

	blocked := mongoBegin(t, backend, false)
	if err := blocked.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{Target: target, Collections: allCollections}); !errors.Is(err, store.ErrDeleteRestricted) {
		mongoRollback(t, blocked)
		t.Fatalf("restricted upload delete = %v, want ErrDeleteRestricted", err)
	}
	unchanged, err := blocked.Find(t.Context(), store.Request{Collection: owners, ID: active.ID})
	if err != nil {
		mongoRollback(t, blocked)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, unchanged, target.DocumentID, []string{target.DocumentID, target.DocumentID, other.ID})
	mongoRollback(t, blocked)

	removeRestriction := mongoBegin(t, backend, false)
	if _, err := removeRestriction.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: owners, ID: active.ID}, Values: store.Values{"guard": store.Null()},
	}); err != nil {
		mongoRollback(t, removeRestriction)
		t.Fatal(err)
	}
	mongoCommit(t, removeRestriction)
	assertMongoUploadTargetReferenceRows(t, backend, target, 6)

	rolledBack := mongoBegin(t, backend, false)
	if err := rolledBack.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{Target: target, Collections: allCollections}); err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	nullifiedActive, err := rolledBack.Find(t.Context(), store.Request{Collection: owners, ID: active.ID})
	if err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, nullifiedActive, "", []string{other.ID})
	nullifiedTrash, err := rolledBack.Find(t.Context(), store.Request{Collection: owners, ID: trashed.ID, Deletion: store.DeletionTrash})
	if err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, nullifiedTrash, "", nil)
	mongoRollback(t, rolledBack)

	afterRollback := mongoBegin(t, backend, true)
	activeAfterRollback, err := afterRollback.Find(t.Context(), store.Request{Collection: owners, ID: active.ID})
	if err != nil {
		mongoRollback(t, afterRollback)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, activeAfterRollback, target.DocumentID, []string{target.DocumentID, target.DocumentID, other.ID})
	trashAfterRollback, err := afterRollback.Find(t.Context(), store.Request{Collection: owners, ID: trashed.ID, Deletion: store.DeletionTrash})
	if err != nil {
		mongoRollback(t, afterRollback)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, trashAfterRollback, target.DocumentID, []string{target.DocumentID, target.DocumentID})
	mongoRollback(t, afterRollback)
	assertMongoUploadTargetReferenceRows(t, backend, target, 6)

	allowed := mongoBegin(t, backend, false)
	if err := allowed.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{Target: target, Collections: allCollections}); err != nil {
		mongoRollback(t, allowed)
		t.Fatal(err)
	}
	if _, err := allowed.Delete(t.Context(), store.Request{Collection: media, ID: target.DocumentID}); err != nil {
		mongoRollback(t, allowed)
		t.Fatal(err)
	}
	if err := allowed.DeleteDocumentState(t.Context(), target); err != nil {
		mongoRollback(t, allowed)
		t.Fatal(err)
	}
	mongoCommit(t, allowed)
	assertMongoUploadTargetReferenceRows(t, backend, target, 0)

	verified := mongoBegin(t, backend, true)
	activeAfterDelete, err := verified.Find(t.Context(), store.Request{Collection: owners, ID: active.ID})
	if err != nil {
		mongoRollback(t, verified)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, activeAfterDelete, "", []string{other.ID})
	trashAfterDelete, err := verified.Find(t.Context(), store.Request{Collection: owners, ID: trashed.ID, Deletion: store.DeletionTrash})
	if err != nil {
		mongoRollback(t, verified)
		t.Fatal(err)
	}
	assertMongoUploadOwnerValues(t, trashAfterDelete, "", nil)
	for _, fixture := range []struct {
		id      string
		gallery []string
	}{
		{id: active.ID, gallery: []string{target.DocumentID, target.DocumentID, other.ID}},
		{id: trashed.ID, gallery: []string{target.DocumentID, target.DocumentID}},
	} {
		versions, err := verified.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{Collection: owners, DocumentID: fixture.id})
		if err != nil {
			mongoRollback(t, verified)
			t.Fatal(err)
		}
		if len(versions) != 1 {
			mongoRollback(t, verified)
			t.Fatalf("owner %q versions = %#v", fixture.id, versions)
		}
		assertMongoUploadOwnerValues(t, versions[0].Snapshot, target.DocumentID, fixture.gallery)
	}
	mongoRollback(t, verified)

	recreate := mongoBegin(t, backend, false)
	if _, err := recreate.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: target.DocumentID, Status: store.StatusPublished,
		Values: mongoUploadValues("recreated-original", "recreated-variant"),
	}); err != nil {
		mongoRollback(t, recreate)
		t.Fatal(err)
	}
	mongoCommit(t, recreate)
	redelete := mongoBegin(t, backend, false)
	if err := redelete.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{Target: target, Collections: allCollections}); err != nil {
		mongoRollback(t, redelete)
		t.Fatalf("same-ID upload recreation inherited stale target rows: %v", err)
	}
	if _, err := redelete.Delete(t.Context(), store.Request{Collection: media, ID: target.DocumentID}); err != nil {
		mongoRollback(t, redelete)
		t.Fatal(err)
	}
	if err := redelete.DeleteDocumentState(t.Context(), target); err != nil {
		mongoRollback(t, redelete)
		t.Fatal(err)
	}
	mongoCommit(t, redelete)
}

func assertMongoUploadOwnerValues(t *testing.T, document store.Document, hero string, gallery []string) {
	t.Helper()
	if hero == "" {
		if document.Values["hero"].Kind() != store.ValueNull {
			t.Fatalf("owner %q hero = %#v, want null", document.ID, document.Values["hero"])
		}
	} else if id, valid := document.Values["hero"].StringValue(); !valid || id != hero {
		t.Fatalf("owner %q hero = %#v, want %q", document.ID, document.Values["hero"], hero)
	}
	content, valid := document.Values["content"].ObjectValue()
	if !valid {
		t.Fatalf("owner %q content = %#v", document.ID, document.Values["content"])
	}
	items, valid := content["gallery"].Values()
	if !valid || len(items) != len(gallery) {
		t.Fatalf("owner %q gallery = %#v, want %#v", document.ID, content["gallery"], gallery)
	}
	for index, expected := range gallery {
		if id, valid := items[index].StringValue(); !valid || id != expected {
			t.Fatalf("owner %q gallery[%d] = %#v, want %q", document.ID, index, items[index], expected)
		}
	}
}

func assertMongoUploadTargetReferenceRows(t *testing.T, backend *Store, target store.DocumentReference, expected int64) {
	t.Helper()
	count, err := backend.database.Collection(mongoReferenceCollectionName).CountDocuments(t.Context(), bson.D{
		{Key: "targetCollection", Value: string(target.CollectionID)},
		{Key: "targetDocument", Value: target.DocumentID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("upload target reference rows = %d, want %d", count, expected)
	}
}
