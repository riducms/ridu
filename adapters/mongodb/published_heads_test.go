package mongodb

import (
	"reflect"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestMongoHeadReservationsUnionWorkingAndPublishedUniqueness(t *testing.T) {
	collection := mongoIndexTestCollection(t)
	collection.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 1}
	collection.Capabilities.Versions = true
	working := store.Document{ID: "first", Status: store.StatusPublished, Revision: 2, Values: store.Values{
		"code": store.String("new"), "tenant": store.String("tenant"),
		"seo": store.Object(store.Values{"slug": store.String("new-slug")}),
	}}
	live := store.CloneDocument(working)
	live.Revision = 1
	live.Values["code"] = store.String("old")
	live.Values["seo"] = store.Object(store.Values{"slug": store.String("old-slug")})
	keys, err := mongoHeadReservationKeys(collection, nil, working, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 4 {
		t.Fatalf("reservation count = %d, want 4 for two independent unique declarations across both heads", len(keys))
	}
	other := store.CloneDocument(working)
	other.ID = "other"
	other.Values["code"] = store.String("old")
	other.Values["seo"] = store.Object(store.Values{"slug": store.String("other-slug")})
	otherKeys, err := mongoHeadReservationKeys(collection, nil, other)
	if err != nil {
		t.Fatal(err)
	}
	shared := false
	for _, candidate := range otherKeys {
		for _, existing := range keys {
			shared = shared || candidate == existing
		}
	}
	if !shared {
		t.Fatal("another document did not collide with a published-only unique value")
	}
	deleted := time.Now()
	working.DeletedAt, live.DeletedAt = &deleted, &deleted
	trashedKeys, err := mongoHeadReservationKeys(collection, nil, working, live)
	if err != nil || len(trashedKeys) != 0 {
		t.Fatalf("trashed reservations = %v, %v", trashedKeys, err)
	}
}

func mustReadMongoArtifacts(t *testing.T, directory string) []migrationartifact.File {
	t.Helper()
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestMongoPublishedHeadQueriesAndUniqueReservationsIntegration(t *testing.T) {
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{Name: "Mongo dual heads", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("slug").Required().Unique()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	collection := mongoDBSemanticLiveCollection(t, manifest, "posts")
	transaction, err := backend.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "first", Status: store.StatusPublished, Values: store.Values{"slug": store.String("old")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(t.Context(), transaction, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: 1},
		Values:  store.Values{"slug": store.String("implicit")},
	}); err == nil {
		t.Fatal("draft-capable default update accepted an implicit publication")
	}
	staged, err := conformance.LockedUpdate(t.Context(), transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: 1}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"slug": store.String("new")}})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Revision != 2 || staged.PublishedRevision != 1 || !staged.HasDraftChanges {
		t.Fatalf("staged metadata = %#v", staged)
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	read, err := backend.BeginSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	live, err := read.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, PublishedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if slug, _ := live.Values["slug"].StringValue(); slug != "old" || live.Revision != 1 || live.HasDraftChanges {
		t.Fatalf("live projection = %#v", live)
	}
	filter := query.Equal("slug", "new").Node()
	page, err := read.List(t.Context(), store.Request{Collection: collection, PublishedOnly: true, Filter: &filter})
	if err != nil || *page.Total != 0 {
		t.Fatalf("public pending-value page = %#v, %v", page, err)
	}
	filter = query.Equal("slug", "old").Node()
	page, err = read.List(t.Context(), store.Request{Collection: collection, PublishedOnly: true, Filter: &filter})
	if err != nil || *page.Total != 1 {
		t.Fatalf("public live-value page = %#v, %v", page, err)
	}
	if err := read.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"old", "new"} {
		conflict, err := backend.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = conflict.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "other-" + slug, Status: store.StatusDraft, Values: store.Values{"slug": store.String(slug)}})
		if err == nil {
			_ = conflict.Rollback(t.Context())
			t.Fatalf("unique value %q was not reserved across both heads", slug)
		}
		_ = conflict.Rollback(t.Context())
	}
	if err := backend.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatalf("published-head readiness after mutations: %v", err)
	}
}

func TestMongoPublishedOnlyReferenceAndUploadRemainProtected(t *testing.T) {
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{Name: "Mongo live dependencies", Collections: []ridu.Collection{
		{
			Slug: "media", Upload: true, Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true},
			UploadConfig:  ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			Fields:        field.Fields{field.Text("alt")},
		},
		{
			Slug: "owners", Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields:        field.Fields{field.Text("title").Required(), field.Upload("hero", "media").OnDelete(field.ReferenceDeleteNullify)},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	collections := mongoCollectionsBySlug(manifest.Snapshot().Collections)
	media, owners := collections["media"], collections["owners"]
	write := mongoBegin(t, backend, false)
	asset, err := write.Create(t.Context(), store.CreateRequest{Collection: media, ID: "asset", Status: store.StatusPublished, Values: mongoUploadValues("live-object", "live-variant")})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(t.Context(), write, store.UpdateRequest{
		Request: store.Request{Collection: media, ID: asset.ID, ExpectedRevision: 1},
		Intent:  store.WriteIntentSaveDraft, ReplaceValues: true, Values: mongoUploadValues("working-object", "working-variant"),
	}); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	owner, err := write.Create(t.Context(), store.CreateRequest{Collection: owners, ID: "owner", Status: store.StatusPublished, Values: store.Values{"title": store.String("Owner"), "hero": store.String(asset.ID)}})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(t.Context(), write, store.UpdateRequest{
		Request: store.Request{Collection: owners, ID: owner.ID, ExpectedRevision: 1},
		Intent:  store.WriteIntentSaveDraft, ReplaceValues: true, Values: store.Values{"title": store.String("Owner draft")},
	}); err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	mongoCommit(t, write)

	read := mongoBegin(t, backend, true)
	keys, err := read.(store.UploadReferenceTransaction).ReferencedUploadObjects(t.Context(), store.UploadReferenceRequest{
		Collections: []schema.Collection{media}, ObjectKeys: []string{"live-object", "live-variant", "working-object", "working-variant"},
	})
	if err != nil || !reflect.DeepEqual(keys, []string{"live-object", "live-variant", "working-object", "working-variant"}) {
		mongoRollback(t, read)
		t.Fatalf("both active upload heads must protect their objects: %#v, %v", keys, err)
	}
	mongoRollback(t, read)

	deleteReferences := mongoBegin(t, backend, false)
	if err := deleteReferences.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{
		Target:      store.DocumentReference{CollectionID: media.ID, DocumentID: asset.ID},
		Collections: map[schema.StableID]schema.Collection{media.ID: media, owners.ID: owners},
	}); err != nil {
		mongoRollback(t, deleteReferences)
		t.Fatal(err)
	}
	mongoCommit(t, deleteReferences)

	read = mongoBegin(t, backend, true)
	live, err := read.Find(t.Context(), store.Request{Collection: owners, ID: owner.ID, PublishedOnly: true})
	if err != nil {
		mongoRollback(t, read)
		t.Fatal(err)
	}
	if hero := live.Values["hero"]; !hero.IsZero() && hero.Kind() != store.ValueNull {
		mongoRollback(t, read)
		t.Fatalf("published-only reference survived target deletion planning: %#v", live.Values)
	}
	working, err := read.Find(t.Context(), store.Request{Collection: owners, ID: owner.ID})
	if err != nil || working.Revision != 2 || !working.HasDraftChanges {
		mongoRollback(t, read)
		t.Fatalf("working owner changed beyond reference nullification: %#v, %v", working, err)
	}
	mongoRollback(t, read)
}

func TestMongoVersionedWithoutDraftsOmitsAuthoringHeadMetadata(t *testing.T) {
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{Name: "Mongo versioned without drafts", Collections: []ridu.Collection{{
		Slug: "pages", Versions: true, Fields: field.Fields{field.Text("title")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	collection := mongoCollectionsBySlug(manifest.Snapshot().Collections)["pages"]
	write := mongoBegin(t, backend, false)
	created, err := write.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "page", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if created.PublishedRevision != 0 || created.HasDraftChanges {
		mongoRollback(t, write)
		t.Fatalf("non-draft create leaked authoring metadata: %#v", created)
	}
	updated, err := conformance.LockedUpdate(t.Context(), write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: 1}, Values: store.Values{"title": store.String("Updated")}})
	if err != nil {
		mongoRollback(t, write)
		t.Fatal(err)
	}
	if updated.PublishedRevision != 0 || updated.HasDraftChanges {
		mongoRollback(t, write)
		t.Fatalf("non-draft update leaked authoring metadata: %#v", updated)
	}
	mongoCommit(t, write)
	read := mongoBegin(t, backend, true)
	for _, publishedOnly := range []bool{false, true} {
		found, err := read.Find(t.Context(), store.Request{Collection: collection, ID: created.ID, PublishedOnly: publishedOnly})
		if err != nil || found.PublishedRevision != 0 || found.HasDraftChanges {
			mongoRollback(t, read)
			t.Fatalf("non-draft read publishedOnly=%t leaked authoring metadata: %#v, %v", publishedOnly, found, err)
		}
	}
	mongoRollback(t, read)
}

func TestMongoMigrationPreservesCrossHeadUniqueReservations(t *testing.T) {
	for _, rename := range []bool{false, true} {
		name := "add unique declaration"
		if rename {
			name = "rename unique field"
		}
		t.Run(name, func(t *testing.T) {
			backend := mongoIntegrationStore(t)
			resolve := func(fieldName string, unique bool) schema.Manifest {
				t.Helper()
				title := field.Text(fieldName)
				if unique {
					title = title.Unique()
				}
				manifest, err := ridu.Resolve(ridu.Config{Name: "Mongo unique migration", Collections: []ridu.Collection{{
					Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{title},
				}}})
				if err != nil {
					t.Fatal(err)
				}
				return manifest
			}
			before := resolve("slug", rename)
			afterField := "slug"
			options := ArtifactOptions{}
			if rename {
				afterField = "permalink"
				options.Renames = []migration.Rename{{CollectionBefore: "pages", CollectionAfter: "pages", FieldBefore: "slug", FieldAfter: "permalink"}}
			}
			after := resolve(afterField, true)
			directory := t.TempDir()
			if _, err := CreateArtifact(t.Context(), directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := backend.ApplyArtifacts(t.Context(), directory); err != nil {
				t.Fatal(err)
			}
			collection := mongoCollectionsBySlug(before.Snapshot().Collections)["pages"]
			write := mongoBegin(t, backend, false)
			created, err := write.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "first", Status: store.StatusPublished, Values: store.Values{"slug": store.String("live")}})
			if err != nil {
				mongoRollback(t, write)
				t.Fatal(err)
			}
			if _, err := conformance.LockedUpdate(t.Context(), write, store.UpdateRequest{
				Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: 1},
				Intent:  store.WriteIntentSaveDraft, Values: store.Values{"slug": store.String("working")},
			}); err != nil {
				mongoRollback(t, write)
				t.Fatal(err)
			}
			mongoCommit(t, write)
			if _, err := CreateArtifact(t.Context(), directory, "unique-change", after, time.Unix(2, 0), options); err != nil {
				t.Fatal(err)
			}
			if !rename {
				files := mustReadMongoArtifacts(t, directory)
				found := false
				for _, phase := range files[1].Artifact.Phases {
					for _, step := range phase.Steps {
						found = found || step.Kind == migration.StepMongoDBRebuildHeadReservations
					}
				}
				if !found {
					t.Fatal("unique-index addition omitted the explicit reservation rebuild")
				}
			}
			if err := backend.ApplyArtifactsWithOptions(t.Context(), directory, RunnerOptions{AllowMaintenance: true}); err != nil {
				t.Fatal(err)
			}
			collection = mongoCollectionsBySlug(after.Snapshot().Collections)["pages"]
			for _, value := range []string{"live", "working"} {
				conflict := mongoBegin(t, backend, false)
				_, err := conflict.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "other-" + value, Status: store.StatusDraft, Values: store.Values{afterField: store.String(value)}})
				_ = conflict.Rollback(t.Context())
				if err == nil {
					t.Fatalf("migration left %q from an active head unreserved", value)
				}
			}
			if err := backend.Ready(t.Context(), after); err != nil {
				t.Fatal(err)
			}
		})
	}
}
