package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func openDeleteTestStore(t *testing.T) (*Store, schema.Collection, schema.Collection) {
	t.Helper()
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite delete", Collections: []ridu.Collection{
		{Slug: "people", Fields: field.Fields{field.Text("name"), field.Relationship("friend", "people")}},
		{Slug: "notes", Versions: true, Fields: field.Fields{field.Text("body")}},
	}})
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
	return backend, sqliteCollectionBySlug(t, manifest, "people"), sqliteCollectionBySlug(t, manifest, "notes")
}

func ownedReferenceCount(t *testing.T, backend *Store, collection schema.Collection, id string) int {
	t.Helper()
	var count int
	if err := backend.db.QueryRowContext(context.Background(), `SELECT count(*) FROM ridu_document_references
WHERE owner_collection_id = ? AND owner_document_id = ?`, string(collection.ID), id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSQLiteDeleteThatTheRequestCannotSeeLeavesTheDocumentAndItsReferences(t *testing.T) {
	ctx := context.Background()
	backend, people, _ := openDeleteTestStore(t)
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: people, ID: "bea", Values: store.Values{"name": store.String("Bea")}}); err != nil {
			return err
		}
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: people, ID: "ada", Values: store.Values{"name": store.String("Ada"), "friend": store.String("bea")}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if count := ownedReferenceCount(t, backend, people, "ada"); count != 1 {
		t.Fatalf("references owned before delete = %d", count)
	}
	hidden := query.Equal("name", "Someone else").Node()
	err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		if _, err := transaction.Delete(ctx, store.Request{Collection: people, ID: "ada", Access: &hidden}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("delete hidden by access = %v, want not found", err)
		}
		// The rejected delete must not have removed anything this transaction then commits.
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	if _, err := read.Find(ctx, store.Request{Collection: people, ID: "ada"}); err != nil {
		t.Fatalf("document after rejected delete: %v", err)
	}
	if count := ownedReferenceCount(t, backend, people, "ada"); count != 1 {
		t.Fatalf("references owned after rejected delete = %d, want 1", count)
	}
}

func TestSQLiteDeleteRemovesASelfReferencingDocument(t *testing.T) {
	ctx := context.Background()
	backend, people, _ := openDeleteTestStore(t)
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: people, ID: "cy", Values: store.Values{"name": store.String("Cy"), "friend": store.String("cy")}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var deleted store.Document
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		var err error
		deleted, err = transaction.Delete(ctx, store.Request{Collection: people, ID: "cy"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if name, _ := deleted.Values["name"].StringValue(); deleted.ID != "cy" || name != "Cy" {
		t.Fatalf("deleted document = %#v", deleted)
	}
	if count := ownedReferenceCount(t, backend, people, "cy"); count != 0 {
		t.Fatalf("references owned after delete = %d", count)
	}
}

func TestSQLiteDeleteReportsExpectedRevisionConflictsAndMissingDocuments(t *testing.T) {
	ctx := context.Background()
	backend, _, notes := openDeleteTestStore(t)
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: notes, ID: "note", Values: store.Values{"body": store.String("Draft")}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		_, err := transaction.Delete(ctx, store.Request{Collection: notes, ID: "note", ExpectedRevision: 2})
		return err
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete at another revision = %v, want conflict", err)
	}
	err = commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		_, err := transaction.Delete(ctx, store.Request{Collection: notes, ID: "missing", ExpectedRevision: 1})
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete of a missing document = %v, want not found", err)
	}
	var deleted store.Document
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		var err error
		deleted, err = transaction.Delete(ctx, store.Request{Collection: notes, ID: "note", ExpectedRevision: 1})
		return err
	}); err != nil {
		t.Fatalf("delete at the current revision: %v", err)
	}
	if deleted.ID != "note" || deleted.Revision != 1 {
		t.Fatalf("deleted document = %#v", deleted)
	}
}
