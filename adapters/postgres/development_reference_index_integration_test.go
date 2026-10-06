package postgres

import (
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Updates rewrite reference index rows only when a document's references
// change, so the index must never hold rows the current schema cannot
// interpret. Removing a nested relationship field in development keeps its
// JSON column; synchronization must rebuild the index from the new schema, or
// deleting a formerly referenced document fails on the unknown field.
func TestPostgresDevelopmentSyncRebuildsReferenceIndexForRemovedFields(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	resolve := func(withAuthor bool) schema.Manifest {
		meta := field.Fields{field.Text("note")}
		if withAuthor {
			meta = append(meta, field.Relationship("author", "authors"))
		}
		manifest, err := core.Resolve(core.Config{Name: "Development references", Collections: []core.Collection{
			{Slug: "authors", Fields: field.Fields{field.Text("name")}},
			{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Group("meta", meta)}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before, after := resolve(true), resolve(false)
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	authors, posts := before.Snapshot().Collections[0], before.Snapshot().Collections[1]
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{Collection: authors, ID: "author", Values: store.Values{"name": store.String("Author")}}); err != nil {
		_ = write.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{Collection: posts, ID: "post", Status: store.StatusPublished, Values: store.Values{
		"meta": store.Object(store.Values{"note": store.String("Note"), "author": store.String("author")}),
	}}); err != nil {
		_ = write.Rollback(ctx)
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var indexed int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_document_references WHERE target_document_id = 'author'`).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("working and live index rows before removal = %d, %v", indexed, err)
	}

	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_document_references`).Scan(&indexed); err != nil || indexed != 0 {
		t.Errorf("index rows after removing the only reference field = %d, %v", indexed, err)
	}
	collections := make(map[schema.StableID]schema.Collection)
	for _, collection := range after.Snapshot().Collections {
		collections[collection.ID] = collection
	}
	remove, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer remove.Rollback(ctx)
	if err := remove.ApplyReferenceDelete(ctx, store.ReferenceDeleteRequest{
		Target: store.DocumentReference{CollectionID: authors.ID, DocumentID: "author"}, Collections: collections,
	}); err != nil {
		t.Fatalf("delete of a formerly referenced document = %v", err)
	}
}
