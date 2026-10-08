package sqlite

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestSQLiteFieldKindClearCoversPublishedOnlyValuesAndLogicalCounts(t *testing.T) {
	ctx := t.Context()
	resolve := func(body field.Node) schema.Manifest {
		t.Helper()
		manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite active-head field recovery", Collections: []ridu.Collection{{
			Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{body},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before, after := resolve(field.Text("body")), resolve(field.Number("body"))
	collection := sqliteCollectionBySlug(t, before, "posts")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	for _, id := range []string{"both-heads", "live-only"} {
		created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: id, Status: store.StatusPublished, Values: store.Values{"body": store.String("old")}})
		if err != nil {
			t.Fatal(err)
		}
		if id == "both-heads" {
			if _, err := write.(store.VersionTransaction).SaveVersion(ctx, collection, created, 10); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{
			Request: store.Request{Collection: collection, ID: id, ExpectedRevision: created.Revision},
			Intent:  store.WriteIntentSaveDraft, Values: store.Values{"body": store.Null()},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	reports, err := backend.ReviewDevelopmentFieldKinds(ctx, before, after)
	if err != nil || len(reports) != 1 || reports[0].Documents != 2 || reports[0].Snapshots != 1 {
		t.Fatalf("logical active-head and history counts = %#v, %v", reports, err)
	}
	incorrect := append([]fieldchange.Report(nil), reports...)
	incorrect[0].Documents--
	if err := backend.ClearDevelopmentFieldKinds(ctx, before, after, incorrect); err == nil {
		t.Fatal("field-kind clear accepted stale reviewed counts")
	}
	if err := backend.ClearDevelopmentFieldKinds(ctx, before, after, reports); err != nil {
		t.Fatal(err)
	}
	afterCollection := sqliteCollectionBySlug(t, after, "posts")
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	for _, id := range []string{"both-heads", "live-only"} {
		for _, publishedOnly := range []bool{false, true} {
			document, err := read.Find(ctx, store.Request{Collection: afterCollection, ID: id, PublishedOnly: publishedOnly})
			if err != nil {
				t.Fatal(err)
			}
			if value, exists := document.Values["body"]; exists && value.Kind() != store.ValueNull {
				t.Fatalf("%s head publishedOnly=%t retained incompatible body: %#v", id, publishedOnly, document.Values)
			}
		}
	}
	versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: afterCollection, DocumentID: "both-heads"})
	if err != nil || len(versions) != 1 {
		t.Fatalf("retained history after clear = %#v, %v", versions, err)
	}
	if value, exists := versions[0].Snapshot.Values["body"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("retained snapshot kept incompatible body: %#v", versions[0].Snapshot.Values)
	}
}

func TestSQLitePublishedHeadSelectsBeforeFilterAndPreservesRevision(t *testing.T) {
	ctx := t.Context()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite published heads", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	collection := sqliteCollectionBySlug(t, manifest, "posts")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	created, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "post-1", Status: store.StatusPublished, Values: store.Values{"title": store.String("live")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision}, Values: store.Values{"title": store.String("implicit publication")}}); err == nil {
		t.Fatal("draft-capable default update accepted an implicit publication")
	}
	staged, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"title": store.String("pending")}})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Revision != 2 || staged.PublishedRevision != 1 || !staged.HasDraftChanges || staged.Status != store.StatusPublished {
		t.Fatalf("staged metadata = %#v", staged)
	}
	live, err := transaction.Find(ctx, store.Request{Collection: collection, ID: created.ID, PublishedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := live.Values["title"].StringValue(); title != "live" || live.Revision != 1 || live.PublishedRevision != 0 || live.HasDraftChanges {
		t.Fatalf("public projection = %#v", live)
	}
	filter := query.Equal("title", "pending").Node()
	page, err := transaction.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Filter: &filter})
	if err != nil {
		t.Fatal(err)
	}
	if *page.Total != 0 {
		t.Fatalf("published pending-title count = %d", *page.Total)
	}
	filter = query.Equal("title", "live").Node()
	page, err = transaction.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Filter: &filter})
	if err != nil || *page.Total != 1 {
		t.Fatalf("published live-title page = %#v, %v", page, err)
	}
	discarded, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: created.ID, ExpectedRevision: staged.Revision}, Intent: store.WriteIntentDiscardDraft})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := discarded.Values["title"].StringValue(); title != "live" || discarded.Revision != 3 || discarded.PublishedRevision != 1 || discarded.HasDraftChanges {
		t.Fatalf("discarded working = %#v", discarded)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLitePopulatedWorkingHeadsIncludePublicationMetadata(t *testing.T) {
	ctx := t.Context()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite populated draft metadata", Collections: []ridu.Collection{{
		Slug: "nodes", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title"), field.Relationship("next", "nodes")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	collection := sqliteCollectionBySlug(t, manifest, "nodes")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct{ id, next string }{{"leaf", ""}, {"middle", "leaf"}, {"root", "middle"}} {
		values := store.Values{"title": store.String(candidate.id)}
		if candidate.next != "" {
			values["next"] = store.String(candidate.next)
		}
		created, createErr := write.Create(ctx, store.CreateRequest{Collection: collection, ID: candidate.id, Status: store.StatusPublished, Values: values})
		if createErr != nil {
			_ = write.Rollback(ctx)
			t.Fatal(createErr)
		}
		if candidate.id != "root" {
			values["title"] = store.String("pending-" + candidate.id)
			if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: candidate.id, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft, Values: values}); err != nil {
				_ = write.Rollback(ctx)
				t.Fatal(err)
			}
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	path, _ := query.NewPath("next")
	for _, depth := range []int{1, 2} {
		read, err := backend.BeginSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		root, err := read.Find(ctx, store.Request{Collection: collection, Collections: map[schema.StableID]schema.Collection{collection.ID: collection}, ID: "root", Populate: []query.Population{{Path: path, Depth: depth}}})
		_ = read.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		middle, ok := root.Values["next"].CopyDocument()
		if !ok || middle.PublishedRevision != 1 || !middle.HasDraftChanges || middle.Revision != 2 {
			t.Fatalf("depth %d middle metadata = %#v", depth, middle)
		}
		if depth == 2 {
			leaf, ok := middle.Values["next"].CopyDocument()
			if !ok || leaf.PublishedRevision != 1 || !leaf.HasDraftChanges || leaf.Revision != 2 {
				t.Fatalf("nested leaf metadata = %#v", leaf)
			}
		}
	}
}

func TestSQLiteMutationResponsesTrackPublishedMetadata(t *testing.T) {
	ctx := t.Context()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite mutation metadata", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	collection := sqliteCollectionBySlug(t, manifest, "posts")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "one", Values: store.Values{"title": store.String("first")}})
	if err != nil {
		t.Fatal(err)
	}
	published, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "one", ExpectedRevision: created.Revision}, Intent: store.WriteIntentPublish})
	if err != nil || published.PublishedRevision != 2 || published.HasDraftChanges {
		t.Fatalf("publish response = %#v, %v", published, err)
	}
	staged, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "one", ExpectedRevision: published.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"title": store.String("pending")}})
	if err != nil || staged.PublishedRevision != 2 || !staged.HasDraftChanges {
		t.Fatalf("draft response = %#v, %v", staged, err)
	}
	trashed, err := write.Trash(ctx, store.Request{Collection: collection, ID: "one"})
	if err != nil || trashed.PublishedRevision != 2 || !trashed.HasDraftChanges || trashed.DeletedAt == nil {
		t.Fatalf("trash response = %#v, %v", trashed, err)
	}
	restored, err := write.Restore(ctx, store.Request{Collection: collection, ID: "one"})
	if err != nil || restored.PublishedRevision != 2 || !restored.HasDraftChanges || restored.DeletedAt != nil {
		t.Fatalf("restore response = %#v, %v", restored, err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLitePublishedAndWorkingHeadsReserveBothUniqueValues(t *testing.T) {
	ctx := t.Context()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite head uniqueness", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("slug").Required().Unique()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	collection := sqliteCollectionBySlug(t, manifest, "posts")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	first, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "first", Status: store.StatusPublished, Values: store.Values{"slug": store.String("old")}})
	if err != nil {
		t.Fatal(err)
	}
	staged, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: first.ID, ExpectedRevision: first.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"slug": store.String("new")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"old", "new"} {
		probe, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := probe.Create(ctx, store.CreateRequest{Collection: collection, ID: "other-" + slug, Status: store.StatusDraft, Values: store.Values{"slug": store.String(slug)}}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("create with reserved %s = %v, want conflict", slug, err)
		}
		_ = probe.Rollback(ctx)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	if _, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: first.ID, ExpectedRevision: staged.Revision}, Intent: store.WriteIntentUnpublish}); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "other-old", Status: store.StatusDraft, Values: store.Values{"slug": store.String("old")}}); err != nil {
		t.Fatalf("old slug was not released on unpublish: %v", err)
	}
}

func TestSQLiteRejectsUnsupportedPlannerHistory(t *testing.T) {
	ctx := t.Context()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite unsupported planner", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	unsupported, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	// 1.2.0 is the previous released planner; its artifacts cannot replay.
	unsupported.Planner.Version = "1.2.0"
	if _, err := migrationartifact.Create(directory, "initial", unsupported, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	want := `_initial.ridu.json uses unsupported planner version "1.2.0"; this Ridu release supports only ridu-sqlite "` + sqlitePlannerVersion + `", so create a new migration history`
	if _, err := CreateArtifact(ctx, directory, "next", manifest, time.Unix(2, 0), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("new artifact over unsupported planner history = %v", err)
	}
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("apply unsupported planner history = %v", err)
	}
}

func TestSQLiteDevelopmentMigrationRejectsMissingPublishedHeadTable(t *testing.T) {
	ctx := t.Context()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite missing live head", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `DROP TABLE ridu_published_documents`); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err == nil {
		t.Fatal("readiness accepted missing live-head table")
	}
	if err := backend.Migrate(ctx, manifest); err == nil {
		t.Fatal("development migration silently restored missing live-head table")
	}
}
