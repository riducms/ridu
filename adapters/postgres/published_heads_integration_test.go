package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresPublishedHeadAndDraftLifecycle(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	title := atlasTextField("posts-title", "title")
	title.Required, title.Unique = true, true
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Published head lifecycle"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Versions: &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}, Fields: []schema.Field{title}}},
	})
	collection := manifest.Snapshot().Collections[0]
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("apply current history: %v", err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("ready after initial migration: %v", err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "legacy", Status: store.StatusPublished, Values: store.Values{"title": store.String("Original title")}}); err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read := func(publishedOnly bool) store.Document {
		t.Helper()
		transaction, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(ctx)
		document, err := transaction.Find(ctx, store.Request{Collection: collection, ID: "legacy", PublishedOnly: publishedOnly})
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	if got := read(true); got.Revision != 1 || postgresTitle(got) != "Original title" {
		t.Fatalf("public snapshot = %#v", got)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: "legacy", ExpectedRevision: 1},
		Values:  store.Values{"title": store.String("Implicit publication")},
	}); err == nil {
		transaction.Rollback(ctx)
		t.Fatal("draft-capable default update accepted an implicit publication")
	}
	staged, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: "legacy", ExpectedRevision: 1},
		Intent:  store.WriteIntentSaveDraft, Values: store.Values{"title": store.String("Pending title")},
	})
	if err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if staged.Revision != 2 || staged.PublishedRevision != 1 || !staged.HasDraftChanges {
		t.Fatalf("staged metadata = %#v", staged)
	}
	if got := read(true); got.Revision != 1 || postgresTitle(got) != "Original title" || got.HasDraftChanges {
		t.Fatalf("published snapshot after draft = %#v", got)
	}
	if got := read(false); got.Revision != 2 || postgresTitle(got) != "Pending title" || !got.HasDraftChanges {
		t.Fatalf("working snapshot after draft = %#v", got)
	}
	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		value string
		want  int
	}{{"Original title", 1}, {"Pending title", 0}} {
		filter := query.Equal(titlePath, candidate.value).Node()
		page, err := transaction.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Filter: &filter, Page: 1, Limit: 10})
		if err != nil || *page.Total != candidate.want {
			transaction.Rollback(ctx)
			t.Fatalf("public filter %q = %#v, %v", candidate.value, page, err)
		}
	}
	transaction.Rollback(ctx)
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "legacy", ExpectedRevision: 2}, Intent: store.WriteIntentPublish})
	if err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(true); got.Revision != 3 || postgresTitle(got) != "Pending title" {
		t.Fatalf("promoted public snapshot = %#v", got)
	}
}

func postgresTitle(document store.Document) string {
	text, _ := document.Values["title"].StringValue()
	return text
}

func TestPostgresPublishedCompoundUniqueAcrossHeads(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	x := atlasTextField("posts-x", "x")
	y := atlasTextField("posts-y", "y")
	xPath, _ := query.NewPath("x")
	yPath, _ := query.NewPath("y")
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Published compound unique"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Versions: &schema.VersionSettings{Drafts: true}, Fields: []schema.Field{x, y},
			Capabilities: schema.Capabilities{Versions: true, Trash: true},
			Indexes:      []schema.CollectionIndex{{Fields: []query.Path{xPath, yPath}, Unique: true}}}},
	})
	collection := manifest.Snapshot().Collections[0]
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	one, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "one", Status: store.StatusPublished,
		Values: store.Values{"x": store.String("original"), "y": store.String("pair")}})
	if err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collection, ID: one.ID, ExpectedRevision: one.Revision},
		Intent: store.WriteIntentSaveDraft, Values: store.Values{"x": store.String("pending"), "y": store.String("pair")}}); err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "two", Status: store.StatusPublished,
		Values: store.Values{"x": store.String("original"), "y": store.String("pair")}})
	transaction.Rollback(ctx)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate live compound tuple = %v, want conflict", err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Trash(ctx, store.Request{Collection: collection, ID: one.ID}); err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "two", Status: store.StatusPublished,
		Values: store.Values{"x": store.String("original"), "y": store.String("pair")}}); err != nil {
		transaction.Rollback(ctx)
		t.Fatalf("trashed live tuple should be available: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transaction.Restore(ctx, store.Request{Collection: collection, ID: one.ID})
	transaction.Rollback(ctx)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("restoring occupied published tuple = %v, want conflict", err)
	}
}

func TestPostgresPublishedHeadTrashMetadata(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Published trash metadata"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{ID: "posts", Slug: "posts", Versions: &schema.VersionSettings{Drafts: true}, Fields: []schema.Field{atlasTextField("posts-title", "title")}}},
	})
	collection := manifest.Snapshot().Collections[0]
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "one", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
	if err != nil {
		transaction.Rollback(ctx)
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, wantDeleted := range []bool{true, false} {
		transaction, err = backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		request := store.Request{Collection: collection, ID: created.ID}
		if wantDeleted {
			_, err = transaction.Trash(ctx, request)
		} else {
			_, err = transaction.Restore(ctx, request)
		}
		if err != nil {
			transaction.Rollback(ctx)
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		transaction, err = backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		live, err := transaction.Find(ctx, store.Request{Collection: collection, ID: created.ID, PublishedOnly: true, Deletion: store.DeletionAll})
		transaction.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if (live.DeletedAt != nil) != wantDeleted || live.Revision != created.Revision || !live.UpdatedAt.Equal(created.UpdatedAt) || postgresTitle(live) != "Live" {
			t.Fatalf("published head after trash=%t: %#v", wantDeleted, live)
		}
	}
}

func TestPostgresPublishedReferencesRespectNullifyAndCascade(t *testing.T) {
	for _, action := range []schema.ReferenceDeleteAction{schema.ReferenceDeleteNullify, schema.ReferenceDeleteCascade} {
		t.Run(string(action), func(t *testing.T) {
			ctx := context.Background()
			backend := migrationArtifactTestBackend(t)
			path, _ := query.NewPath("target")
			manifest := schema.NewManifest(schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "Published reference action"}, Plugins: []schema.Plugin{},
				Collections: []schema.Collection{
					{ID: "targets", Slug: "targets", Fields: []schema.Field{atlasTextField("targets-name", "name")}},
					{ID: "owners", Slug: "owners", Versions: &schema.VersionSettings{Drafts: true}, Fields: []schema.Field{{
						ID: "owners-target", Name: "target", Path: path, Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
						Relationship: &schema.RelationshipField{CollectionID: "targets", CollectionSlug: "targets", OnDelete: action},
					}}},
				},
			})
			collections := map[schema.StableID]schema.Collection{}
			for _, collection := range manifest.Snapshot().Collections {
				collections[collection.ID] = collection
			}
			directory := t.TempDir()
			if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), nil, false); err != nil {
				t.Fatal(err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			transaction, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collections["targets"], ID: "target", Values: store.Values{"name": store.String("Target")}}); err != nil {
				transaction.Rollback(ctx)
				t.Fatal(err)
			}
			owner, err := transaction.Create(ctx, store.CreateRequest{Collection: collections["owners"], ID: "owner", Status: store.StatusPublished, Values: store.Values{"target": store.String("target")}})
			if err != nil {
				transaction.Rollback(ctx)
				t.Fatal(err)
			}
			if err := transaction.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			transaction, err = backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: collections["owners"], ID: owner.ID, ExpectedRevision: owner.Revision}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"target": store.Null()}}); err != nil {
				transaction.Rollback(ctx)
				t.Fatal(err)
			}
			if err := transaction.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			transaction, err = backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			request := store.ReferenceDeleteRequest{Target: store.DocumentReference{CollectionID: "targets", DocumentID: "target"}, Collections: collections}
			cascade, err := transaction.(store.CascadeTransaction).CascadeOwners(ctx, request)
			if err != nil {
				transaction.Rollback(ctx)
				t.Fatal(err)
			}
			if action == schema.ReferenceDeleteCascade {
				if len(cascade) != 1 || cascade[0].DocumentID != owner.ID {
					transaction.Rollback(ctx)
					t.Fatalf("published-only cascade owners = %#v", cascade)
				}
				request.PendingOwners = cascade
			} else if len(cascade) != 0 {
				transaction.Rollback(ctx)
				t.Fatalf("nullify produced cascade owners = %#v", cascade)
			}
			if err := transaction.ApplyReferenceDelete(ctx, request); err != nil {
				transaction.Rollback(ctx)
				t.Fatal(err)
			}
			if action == schema.ReferenceDeleteCascade {
				// The operation engine deletes cascade owners before the target.
				// This transaction only proves the published-only edge is found
				// and accepted once its owner is pending deletion.
				transaction.Rollback(ctx)
				return
			}
			if _, err := transaction.Delete(ctx, store.Request{Collection: collections["targets"], ID: "target"}); err != nil {
				transaction.Rollback(ctx)
				t.Fatal(err)
			}
			if err := transaction.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			transaction, err = backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.Rollback(ctx)
			live, err := transaction.Find(ctx, store.Request{Collection: collections["owners"], ID: owner.ID, PublishedOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if live.Values["target"].Kind() != store.ValueNull {
				t.Fatalf("published reference retained deleted target: %#v", live.Values["target"])
			}
		})
	}
}
