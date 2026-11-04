package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// RunWriteGuards verifies the stored-version guards of the official
// adapters: an update refuses a Current that no longer describes the stored
// row, and saving revision 1 starts a document's version history, so a
// document created with the ID of a removed one never inherits its versions.
func RunWriteGuards(t *testing.T, factory Factory) {
	t.Helper()
	manifest := writeGuardManifest()
	snapshot := manifest.Snapshot()
	notes, posts := snapshot.Collections[0], snapshot.Collections[1]

	t.Run("update rejects a stale Current", func(t *testing.T) {
		backend := factory(t, manifest)
		for _, collection := range []schema.Collection{notes, posts} {
			transaction := begin(t, backend)
			if _, err := transaction.Create(t.Context(), store.CreateRequest{Collection: collection, ID: "one", Values: store.Values{"title": store.String("Created")}}); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
			request := store.Request{Collection: collection, ID: "one"}
			locked := request
			locked.Lock = store.LockMutation
			current, err := transaction.Find(t.Context(), locked)
			if err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
			if _, err := transaction.Update(t.Context(), store.UpdateRequest{Request: request, Values: store.Values{"title": store.String("First write")}, Current: &current}); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
			// current was read before the first write in this transaction.
			if _, err := transaction.Update(t.Context(), store.UpdateRequest{Request: request, Values: store.Values{"body": store.String("Stale write")}, Current: &current}); !errors.Is(err, store.ErrConflict) {
				rollback(t, transaction)
				t.Fatalf("%s update from a Current read before an earlier write = %v, want store.ErrConflict", collection.Slug, err)
			}
			fabricated := store.Document{ID: "one", Revision: current.Revision + 1}
			if _, err := transaction.Update(t.Context(), store.UpdateRequest{Request: request, Values: store.Values{"body": store.String("Fabricated write")}, Current: &fabricated}); !errors.Is(err, store.ErrConflict) {
				rollback(t, transaction)
				t.Fatalf("%s update from a fabricated Current = %v, want store.ErrConflict", collection.Slug, err)
			}
			stored, err := transaction.Find(t.Context(), locked)
			if err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
			if title, _ := stored.Values["title"].StringValue(); title != "First write" || stored.Values["body"].Kind() == store.ValueString {
				rollback(t, transaction)
				t.Fatalf("%s values after rejected writes = %#v", collection.Slug, stored.Values)
			}
			if _, err := transaction.Update(t.Context(), store.UpdateRequest{Request: request, Values: store.Values{"body": store.String("Fresh write")}, Current: &stored}); err != nil {
				rollback(t, transaction)
				t.Fatalf("%s update from the stored version = %v", collection.Slug, err)
			}
			commit(t, transaction)
		}
	})

	// Local.Increment and every read-modify-write in a hook rely on this:
	// a transaction that locks a row with LockMutation reads the value the
	// previous holder committed, so no update is lost. An adapter may make the
	// second locker wait (PostgreSQL, SQLite) or fail fast with ErrConflict
	// (MongoDB); the caller then retries.
	t.Run("a mutation lock serializes read-modify-write", func(t *testing.T) {
		backend := factory(t, manifest)
		transaction := begin(t, backend)
		if _, err := transaction.Create(t.Context(), store.CreateRequest{Collection: notes, ID: "counter", Values: store.Values{"count": store.Number(0)}}); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		commit(t, transaction)
		const writers = 8
		results := make(chan error, writers)
		seen := make(chan float64, writers)
		for range writers {
			go func() {
				for attempt := 0; ; attempt++ {
					value, err := incrementUnderLock(t.Context(), backend, notes, "counter")
					if errors.Is(err, store.ErrConflict) && attempt < 200 {
						time.Sleep(time.Duration(attempt%10+1) * time.Millisecond)
						continue
					}
					if err == nil {
						seen <- value
					}
					results <- err
					return
				}
			}()
		}
		for range writers {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		close(seen)
		values := map[float64]bool{}
		for value := range seen {
			values[value] = true
		}
		transaction = begin(t, backend)
		stored, err := transaction.Find(t.Context(), store.Request{Collection: notes, ID: "counter"})
		rollback(t, transaction)
		if err != nil {
			t.Fatal(err)
		}
		if count, _ := stored.Values["count"].NumberValue(); count != writers || len(values) != writers {
			t.Fatalf("count after %d locked increments = %v, distinct results %v", writers, count, values)
		}
	})

	t.Run("revision 1 starts the version history", func(t *testing.T) {
		backend := factory(t, manifest)
		transaction := begin(t, backend)
		versions := requireVersionTransaction(t, transaction)
		document, err := transaction.Create(t.Context(), store.CreateRequest{Collection: posts, ID: "reused", Values: store.Values{"title": store.String("Old 1")}})
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if _, err := versions.SaveVersion(t.Context(), posts, document, 2); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		for _, title := range []string{"Old 2", "Old 3"} {
			document, err = LockedUpdate(t.Context(), transaction, store.UpdateRequest{Request: store.Request{Collection: posts, ID: "reused"}, Values: store.Values{"title": store.String(title)}})
			if err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
			if _, err := versions.SaveVersion(t.Context(), posts, document, 2); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
		}
		// Remove the row without its document state, as a removal path that
		// leaves history behind would.
		if _, err := transaction.Delete(t.Context(), store.Request{Collection: posts, ID: "reused"}); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		recreated, err := transaction.Create(t.Context(), store.CreateRequest{Collection: posts, ID: "reused", Values: store.Values{"title": store.String("New 1")}})
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if _, err := versions.SaveVersion(t.Context(), posts, recreated, 2); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		history, err := versions.ListVersions(t.Context(), store.VersionRequest{Collection: posts, DocumentID: "reused"})
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if len(history) != 1 || history[0].Revision != 1 {
			rollback(t, transaction)
			t.Fatalf("history of a recreated document = %d versions %v, want only its revision 1", len(history), versionRevisions(history))
		}
		if title, _ := history[0].Snapshot.Values["title"].StringValue(); title != "New 1" {
			rollback(t, transaction)
			t.Fatalf("recreated document's revision 1 snapshot title = %q", title)
		}
		commit(t, transaction)
	})
}

func versionRevisions(versions []store.Version) []int {
	revisions := make([]int, len(versions))
	for index, version := range versions {
		revisions[index] = version.Revision
	}
	return revisions
}

func writeGuardManifest() schema.Manifest {
	fields := func(collection string) []schema.Field {
		return []schema.Field{
			textField(schema.StableID("guards-"+collection+"-title"), "title", false),
			textField(schema.StableID("guards-"+collection+"-body"), "body", false),
			numberField(schema.StableID("guards-"+collection+"-count"), "count"),
		}
	}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Store write guards"},
		Collections: []schema.Collection{
			{
				ID: "guards-notes", Slug: "notes", Labels: schema.CollectionLabels{Singular: "Note", Plural: "Notes"},
				Fields: fields("notes"),
			},
			{
				ID: "guards-posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
				Capabilities: schema.Capabilities{Versions: true}, Versions: &schema.VersionSettings{MaxPerDocument: 2},
				Fields: fields("posts"),
			},
		},
		Plugins: []schema.Plugin{},
	})
}

// incrementUnderLock reads a document's count with LockMutation and writes it
// back one higher in the same transaction.
func incrementUnderLock(ctx context.Context, backend store.Store, collection schema.Collection, id string) (float64, error) {
	transaction, err := backend.Begin(ctx)
	if err != nil {
		return 0, err
	}
	request := store.Request{Collection: collection, ID: id}
	current, err := transaction.Find(ctx, request.CurrentRequest())
	if err != nil {
		_ = transaction.Rollback(ctx)
		return 0, err
	}
	count, _ := current.Values["count"].NumberValue()
	if _, err := transaction.Update(ctx, store.UpdateRequest{Request: request, Values: store.Values{"count": store.Number(count + 1)}, Current: &current}); err != nil {
		_ = transaction.Rollback(ctx)
		return 0, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, err
	}
	return count + 1, nil
}
