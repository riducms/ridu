package sqlite

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

// commitSQLiteWrite runs write in one write transaction, committing it, or rolling it back and
// returning the error.
func commitSQLiteWrite(t *testing.T, backend *Store, write func(store.Transaction) error) error {
	t.Helper()
	ctx := context.Background()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := write(transaction); err != nil {
		_ = transaction.Rollback(ctx)
		return err
	}
	return transaction.Commit(ctx)
}

// A server still running an earlier manifest while development sync rebuilt the derived tables can
// write rows the current manifest no longer produces. The document's next write must clear them,
// even in a collection that now declares no uniqueness and holds no references.
func TestSQLiteWriteClearsDerivedRowsAnEarlierManifestLeft(t *testing.T) {
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite stale derived rows", Collections: []ridu.Collection{
		{Slug: "targets", Fields: field.Fields{field.Text("name")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	targets := sqliteCollectionBySlug(t, manifest, "targets")
	posts := sqliteCollectionBySlug(t, manifest, "posts")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: targets, ID: "target", Values: store.Values{"name": store.String("Target")}}); err != nil {
			return err
		}
		_, err := transaction.Create(ctx, store.CreateRequest{Collection: posts, ID: "post", Values: store.Values{"title": store.String("Post")}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO ridu_document_references (owner_collection_id, owner_document_id, field_id, target_collection_id, target_document_id, occurrence)
VALUES ('` + string(posts.ID) + `', 'post', 'retired-field', '` + string(targets.ID) + `', 'target', 0)`,
		`INSERT INTO ridu_unique_values (collection_id, index_key, value_key, document_id)
VALUES ('` + string(posts.ID) + `', 'field:retired-field', '"Post"', 'post')`,
	} {
		if _, err := backend.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		_, err := transaction.Delete(ctx, store.Request{Collection: targets, ID: "target"})
		return err
	}); err == nil {
		t.Fatal("the stale reference did not restrict deleting its target")
	}

	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		request := store.Request{Collection: posts, ID: "post"}
		current, err := transaction.Find(ctx, request.CurrentRequest())
		if err != nil {
			return err
		}
		_, err = transaction.Update(ctx, store.UpdateRequest{Request: request, Values: store.Values{"title": store.String("Edited")}, Current: &current})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var references, uniqueValues int
	if err := backend.db.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM ridu_document_references WHERE owner_document_id = 'post'),
  (SELECT count(*) FROM ridu_unique_values WHERE document_id = 'post')`).Scan(&references, &uniqueValues); err != nil {
		t.Fatal(err)
	}
	if references != 0 || uniqueValues != 0 {
		t.Fatalf("after the next write the post owns %d reference and %d unique rows, want none", references, uniqueValues)
	}
	if err := commitSQLiteWrite(t, backend, func(transaction store.Transaction) error {
		_, err := transaction.Delete(ctx, store.Request{Collection: targets, ID: "target"})
		return err
	}); err != nil {
		t.Fatalf("delete after the stale reference was cleared: %v", err)
	}
}
