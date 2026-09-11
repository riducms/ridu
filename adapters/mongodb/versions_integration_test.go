package mongodb

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBVersionTransactionRetentionAccessAndRollback(t *testing.T) {
	backend := mongoIntegrationStore(t)
	now := time.Date(2026, time.August, 30, 15, 0, 0, 0, time.UTC)
	backend.now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	collection := mongoVersionedCollection(true, 2)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	write := mongoBegin(t, backend, false)
	versions := write.(store.VersionTransaction)
	document, err := write.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "versioned-post", Status: store.StatusDraft,
		Values: store.Values{"title": store.String("first"), "rank": store.Number(1), "owner": store.String("owner-a")},
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if document.Revision != 1 || document.Status != store.StatusDraft {
		mongoRollback(t, write)
		t.Fatalf("created version metadata = %s/%d", document.Status, document.Revision)
	}
	firstVersion, err := versions.SaveVersion(t.Context(), collection, document, 2)
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	resaved, err := versions.SaveVersion(t.Context(), collection, document, 2)
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if !resaved.CreatedAt.Equal(firstVersion.CreatedAt) {
		mongoRollback(t, write)
		t.Fatalf("re-saved revision timestamp = %v, want preserved %v", resaved.CreatedAt, firstVersion.CreatedAt)
	}
	published := store.StatusPublished
	titlePath, _ := query.NewPath("title")
	document, err = write.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{
			Collection: collection, ID: document.ID, ExpectedRevision: document.Revision,
			Select: []query.Path{titlePath},
		},
		Values: store.Values{"title": store.String("second"), "owner": store.String("owner-b")},
		Status: &published,
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if len(document.Values) != 3 {
		mongoRollback(t, write)
		t.Fatalf("versioned mutation projected canonical snapshot input: %#v", document.Values)
	}
	if _, err := versions.SaveVersion(t.Context(), collection, document, 2); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	draft := store.StatusDraft
	document, err = write.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: document.ID, ExpectedRevision: document.Revision},
		Values:  store.Values{"title": store.String("third"), "owner": store.String("owner-c")},
		Status:  &draft,
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(t.Context(), collection, document, 2); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := write.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: document.ID, ExpectedRevision: 1},
		Values:  store.Values{"title": store.String("stale")},
	}); !errors.Is(err, store.ErrConflict) {
		mongoRollback(t, write)
		t.Fatalf("stale revision update = %v, want ErrConflict", err)
	}
	mongoCommit(t, write)

	read := mongoBegin(t, backend, true)
	versionRead := read.(store.VersionTransaction)
	items, err := versionRead.ListVersions(t.Context(), store.VersionRequest{Collection: collection, DocumentID: document.ID})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Revision != 3 || items[1].Revision != 2 || items[0].ID != document.ID+":3" {
		mongoRollback(t, read)
		t.Fatalf("retained versions = %#v", items)
	}
	ownerPath, _ := query.NewPath("owner")
	ownerB := query.Equal(ownerPath, query.String("owner-b")).Node()
	visible, err := versionRead.ListVersions(t.Context(), store.VersionRequest{
		Collection: collection, DocumentID: document.ID, Access: &ownerB,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Revision != 2 {
		mongoRollback(t, read)
		t.Fatalf("snapshot-filtered versions = %#v", visible)
	}
	if _, err := versionRead.FindVersion(t.Context(), collection, document.ID, 1); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, read)
		t.Fatalf("pruned version lookup = %v, want ErrNotFound", err)
	}
	second, err := versionRead.FindVersion(t.Context(), collection, document.ID, 2)
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	second.Snapshot.Values["title"] = store.String("caller mutation")
	again, err := versionRead.FindVersion(t.Context(), collection, document.ID, 2)
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if title, _ := again.Snapshot.Values["title"].StringValue(); title != "second" {
		mongoRollback(t, read)
		t.Fatalf("version snapshot was not detached: %q", title)
	}
	mongoCommit(t, read)

	rolledBack := mongoBegin(t, backend, false)
	rolledDocument, err := rolledBack.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: document.ID, ExpectedRevision: document.Revision},
		Values:  store.Values{"title": store.String("rolled back")},
		Status:  &published,
	})
	if err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	if _, err := rolledBack.(store.VersionTransaction).SaveVersion(t.Context(), collection, rolledDocument, 2); err != nil {
		mongoRollback(t, rolledBack)
		t.Fatal(err)
	}
	mongoRollback(t, rolledBack)

	afterRollback := mongoBegin(t, backend, true)
	current, err := afterRollback.Find(t.Context(), store.Request{Collection: collection, ID: document.ID})
	if err != nil {
		mongoRollback(t, afterRollback)
		t.Fatal(err)
	}
	afterItems, err := afterRollback.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{Collection: collection, DocumentID: document.ID})
	if err != nil {
		mongoRollback(t, afterRollback)
		t.Fatal(err)
	}
	if current.Revision != 3 || len(afterItems) != 2 || afterItems[0].Revision != 3 {
		mongoRollback(t, afterRollback)
		t.Fatalf("rollback state = current %#v versions %#v", current, afterItems)
	}
	mongoCommit(t, afterRollback)

	cleanup := mongoBegin(t, backend, false)
	if _, err := cleanup.Delete(t.Context(), store.Request{Collection: collection, ID: document.ID, ExpectedRevision: 2}); !errors.Is(err, store.ErrConflict) {
		mongoRollback(t, cleanup)
		t.Fatalf("stale revision delete = %v, want ErrConflict", err)
	}
	if _, err := cleanup.Delete(t.Context(), store.Request{Collection: collection, ID: document.ID, ExpectedRevision: 3}); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	if err := cleanup.DeleteDocumentState(t.Context(), store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}); err != nil {
		mongoRollback(t, cleanup)
		t.Fatal(err)
	}
	mongoCommit(t, cleanup)

	empty := mongoBegin(t, backend, true)
	items, err = empty.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{Collection: collection, DocumentID: document.ID})
	if err != nil || len(items) != 0 {
		mongoRollback(t, empty)
		t.Fatalf("versions after state cleanup = %#v, %v", items, err)
	}
	mongoCommit(t, empty)
}

func TestMongoDBVersionAccessUsesRepeatedSnapshotPredicates(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoRepeatedCollection()
	collection.Capabilities.Versions = true
	collection.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}

	write := mongoBegin(t, backend, false)
	versions := write.(store.VersionTransaction)
	document, err := write.Create(t.Context(), store.CreateRequest{
		Collection: collection,
		ID:         "repeated-version",
		Status:     store.StatusDraft,
		Values:     mongoRepeatedValues(),
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(t.Context(), collection, document, 10); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	document, err = write.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: document.ID, ExpectedRevision: document.Revision},
		Values: store.Values{"rows": store.List(store.Object(store.Values{
			"_key": store.String("revision-two"), "kind": store.String("revision-two"), "label": store.String("Second revision"),
		}))},
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(t.Context(), collection, document, 10); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	mongoCommit(t, write)

	access := query.Equal(mongoMustPath(t, "rows.kind"), query.String("primary")).Node()
	read := mongoBegin(t, backend, true)
	visible, err := read.(store.VersionTransaction).ListVersions(t.Context(), store.VersionRequest{
		Collection: collection,
		DocumentID: document.ID,
		Access:     &access,
	})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Revision != 1 {
		mongoRollback(t, read)
		t.Fatalf("repeated snapshot access = %#v, want only revision one", visible)
	}
	mongoCommit(t, read)
}

func TestMongoDBOperationEngineVersionsRestoreAccessAndSameIDRecreation(t *testing.T) {
	backend := mongoIntegrationStore(t)
	ownerPath, _ := query.NewPath("owner")
	owned := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal(ownerPath, query.String(ctx.Actor.ID))), nil
	}
	application, err := ridu.New(ridu.Config{Name: "MongoDB versions", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 3},
		Fields: field.Fields{field.Text("title").Required(), field.Text("owner").Required()},
		Access: ridu.CollectionAccess{ReadVersions: owned},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	ownerA := &store.Document{ID: "owner-a"}
	ownerB := &store.Document{ID: "owner-b"}
	created, err := application.Local().Create(t.Context(), "posts", store.Values{
		"title": store.String("first"), "owner": store.String(ownerA.ID),
	}, ridu.MutationOptions{Actor: ownerA})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != store.StatusDraft || created.Revision != 1 {
		t.Fatalf("created metadata = %s/%d", created.Status, created.Revision)
	}
	published, err := application.Local().PublishChanges(t.Context(), "posts", created.ID, store.Values{
		"title": store.String("second"), "owner": store.String(ownerB.ID),
	}, ridu.MutationOptions{Actor: ownerA, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	unpublished, err := application.Local().Unpublish(t.Context(), "posts", created.ID, ridu.MutationOptions{Actor: ownerB, ExpectedRevision: published.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if unpublished.Status != store.StatusDraft || unpublished.Revision != 3 {
		t.Fatalf("unpublished metadata = %s/%d", unpublished.Status, unpublished.Revision)
	}

	ownerAVersions, err := application.Local().Versions(t.Context(), "posts", created.ID, ridu.FindOptions{Actor: ownerA})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerAVersions) != 1 || ownerAVersions[0].Revision != 1 {
		t.Fatalf("original-owner versions = %#v", ownerAVersions)
	}
	ownerBVersions, err := application.Local().Versions(t.Context(), "posts", created.ID, ridu.FindOptions{Actor: ownerB})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerBVersions) != 2 || ownerBVersions[0].Revision != 3 || ownerBVersions[1].Revision != 2 {
		t.Fatalf("new-owner versions = %#v", ownerBVersions)
	}
	if _, err := application.Local().Version(t.Context(), "posts", created.ID, 2, ridu.FindOptions{Actor: ownerA}); err == nil {
		t.Fatal("snapshot access allowed the former owner to read the transferred revision")
	}

	restored, err := application.Local().Restore(t.Context(), "posts", created.ID, 2, ridu.MutationOptions{Actor: ownerB, ExpectedRevision: unpublished.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != store.StatusPublished || restored.Revision != 4 {
		t.Fatalf("restored metadata = %s/%d", restored.Status, restored.Revision)
	}
	if title, _ := restored.Values["title"].StringValue(); title != "second" {
		t.Fatalf("restored title = %q", title)
	}
	if _, err := application.Local().Publish(t.Context(), "posts", created.ID, ridu.MutationOptions{Actor: ownerB, ExpectedRevision: unpublished.Revision}); err == nil {
		t.Fatal("stale publish revision was accepted")
	}
	ownerBVersions, err = application.Local().Versions(t.Context(), "posts", created.ID, ridu.FindOptions{Actor: ownerB})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerBVersions) != 3 || ownerBVersions[0].Revision != 4 || ownerBVersions[2].Revision != 2 {
		t.Fatalf("retained operation-engine versions = %#v", ownerBVersions)
	}

	if _, err := application.Local().Delete(t.Context(), "posts", created.ID, ridu.MutationOptions{Actor: ownerB}); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := application.Local().Versions(t.Context(), "posts", created.ID, ridu.FindOptions{Actor: ownerB})
	if err != nil || len(afterDelete) != 0 {
		t.Fatalf("versions after hard delete = %#v, %v", afterDelete, err)
	}
	recreated, err := application.Local().Import(t.Context(), "posts", store.Values{
		"title": store.String("recreated"), "owner": store.String(ownerA.ID),
	}, ridu.ImportOptions{ID: created.ID, Actor: ownerA})
	if err != nil {
		t.Fatal(err)
	}
	if recreated.Revision != 1 {
		t.Fatalf("recreated revision = %d, want 1", recreated.Revision)
	}
	recreatedVersions, err := application.Local().Versions(t.Context(), "posts", recreated.ID, ridu.FindOptions{Actor: ownerA})
	if err != nil || len(recreatedVersions) != 1 || recreatedVersions[0].Revision != 1 {
		t.Fatalf("same-ID recreated versions = %#v, %v", recreatedVersions, err)
	}
}

func TestMongoDBVersionRetentionRejectsCorruptHighRevisionBeforePruning(t *testing.T) {
	backend := mongoIntegrationStore(t)
	collection := mongoVersionedCollection(true, 2)
	if err := backend.SyncIndexes(t.Context(), mongoIndexTestManifest(collection)); err != nil {
		t.Fatal(err)
	}
	write := mongoBegin(t, backend, false)
	versions := write.(store.VersionTransaction)
	document, err := write.Create(t.Context(), store.CreateRequest{
		Collection: collection, ID: "retention-corrupt", Status: store.StatusDraft,
		Values: store.Values{
			"title": store.String("first"), "rank": store.Number(1), "owner": store.String("owner"),
		},
	})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(t.Context(), collection, document, 10); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	document.Revision = 2
	document.Values["title"] = store.String("second")
	if _, err := versions.SaveVersion(t.Context(), collection, document, 10); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	mongoCommit(t, write)

	snapshot, err := encodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	const corruptRevision = 99
	if _, err := backend.database.Collection(physicalVersionCollectionName(collection.ID)).InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: versionID(document.ID, corruptRevision)},
		{Key: mongoVersionOwnerPath, Value: document.ID},
		{Key: mongoVersionRevisionPath, Value: int64(corruptRevision)},
		{Key: mongoVersionStatusPath, Value: string(document.Status)},
		{Key: mongoVersionCreatedAtPath, Value: time.Now().UTC().UnixNano()},
		// The envelope claims revision 99 while the complete canonical snapshot
		// remains revision 2. Retention must detect this before deleting history.
		{Key: mongoVersionSnapshotPath, Value: snapshot},
	}); err != nil {
		t.Fatal(err)
	}

	attempt := mongoBegin(t, backend, false)
	candidate := store.CloneDocument(document)
	candidate.Revision = 3
	candidate.Values["title"] = store.String("third")
	if _, err := attempt.(store.VersionTransaction).SaveVersion(t.Context(), collection, candidate, 2); err == nil || !strings.Contains(err.Error(), "retention candidate") {
		mongoRollback(t, attempt)
		t.Fatalf("corrupt retention candidate error = %v", err)
	}
	mongoRollback(t, attempt)

	physical := backend.database.Collection(physicalVersionCollectionName(collection.ID))
	goodCount, err := physical.CountDocuments(t.Context(), bson.D{
		{Key: mongoVersionOwnerPath, Value: document.ID},
		{Key: mongoVersionRevisionPath, Value: bson.D{{Key: "$in", Value: bson.A{int64(1), int64(2)}}}},
	})
	if err != nil || goodCount != 2 {
		t.Fatalf("good retained versions after corrupt prune = %d, %v", goodCount, err)
	}
	if count, err := physical.CountDocuments(t.Context(), bson.D{{Key: "_id", Value: versionID(document.ID, 3)}}); err != nil || count != 0 {
		t.Fatalf("rolled-back candidate version count = %d, %v", count, err)
	}
}
