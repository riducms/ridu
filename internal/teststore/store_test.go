package teststore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type cancelAfterChecks struct {
	checks int
	after  int
}

func TestProjectionDistinguishesOmittedAndMetadataOnlySelections(t *testing.T) {
	document := store.Document{ID: "post-1", Values: store.Values{"title": store.String("Visible")}}
	if projected := project(document, nil); len(projected.Values) != 1 {
		t.Fatalf("omitted projection values = %#v, want all", projected.Values)
	}
	if projected := project(document, []query.Path{}); len(projected.Values) != 0 {
		t.Fatalf("metadata-only projection values = %#v, want none", projected.Values)
	}
}

func TestSnapshotCommitDoesNotReplaceConcurrentWrite(t *testing.T) {
	ctx := context.Background()
	backend := New()
	collection := schema.Collection{ID: "posts"}
	snapshot, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	releaseSnapshot := make(chan struct{})
	defer close(releaseSnapshot)
	snapshotCommitted := make(chan error, 1)
	go func() {
		<-releaseSnapshot
		snapshotCommitted <- snapshot.Commit(ctx)
	}()

	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "post-b"}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	releaseSnapshot <- struct{}{}
	if err := <-snapshotCommitted; err != nil {
		t.Fatal(err)
	}

	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	if _, err := read.Find(ctx, store.Request{Collection: collection, ID: "post-b"}); err != nil {
		t.Fatalf("write committed after snapshot began did not survive snapshot commit: %v", err)
	}
}

func TestSnapshotRejectsMutationEntryPoints(t *testing.T) {
	ctx := context.Background()
	backend := New()
	snapshot, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "posts"}
	versioned := snapshot.(store.VersionTransaction)
	auth := snapshot.(store.AuthTransaction)
	bootstrap := snapshot.(store.AuthBootstrapTransaction)
	unlock := snapshot.(store.AuthUnlockTransaction)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{name: "create", run: func() error {
			_, err := snapshot.Create(ctx, store.CreateRequest{Collection: collection, ID: "post-1"})
			return err
		}},
		{name: "update", run: func() error {
			_, err := snapshot.Update(ctx, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "post-1"}})
			return err
		}},
		{name: "trash", run: func() error {
			_, err := snapshot.Trash(ctx, store.Request{Collection: collection, ID: "post-1"})
			return err
		}},
		{name: "restore", run: func() error {
			_, err := snapshot.Restore(ctx, store.Request{Collection: collection, ID: "post-1"})
			return err
		}},
		{name: "delete", run: func() error {
			_, err := snapshot.Delete(ctx, store.Request{Collection: collection, ID: "post-1"})
			return err
		}},
		{name: "apply-reference-delete", run: func() error {
			return snapshot.ApplyReferenceDelete(ctx, store.ReferenceDeleteRequest{})
		}},
		{name: "delete-document-state", run: func() error {
			return snapshot.DeleteDocumentState(ctx, store.DocumentReference{})
		}},
		{name: "save-version", run: func() error {
			_, err := versioned.SaveVersion(ctx, collection, store.Document{ID: "post-1"}, 1)
			return err
		}},
		{name: "create-auth-credential", run: func() error {
			return auth.CreateAuthCredential(ctx, collection, "post-1", []byte("hash"), true)
		}},
		{name: "create-first-auth-credential", run: func() error {
			return bootstrap.CreateFirstAuthCredential(ctx, collection, "post-1", []byte("hash"), true)
		}},
		{name: "force-unlock-auth", run: func() error {
			return unlock.ForceUnlockAuth(ctx, collection.ID, "post-1")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil || err.Error() != "snapshot transaction is read-only" {
				t.Fatalf("snapshot mutation error = %v, want read-only rejection", err)
			}
		})
	}
	if err := snapshot.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestListWindowBoundsMaterializationAndReportsOverflow(t *testing.T) {
	backend := New()
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())

	rankPath, err := query.NewPath("rank")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "jobs", Fields: []schema.Field{{
		ID: "jobs-rank", Name: "rank", Path: rankPath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Unique: true, Index: true,
	}}}
	for index := 8; index >= 0; index-- {
		id := string(rune('a' + index))
		if _, err := transaction.Create(context.Background(), store.CreateRequest{
			Collection: collection,
			ID:         id,
			Values:     store.Values{"rank": store.String(id)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	eventOffset := len(backend.Events())
	windowTransaction, supportsWindows := transaction.(store.WindowTransaction)
	if !supportsWindows {
		t.Fatal("test store transaction does not implement store.WindowTransaction")
	}
	window, err := windowTransaction.ListWindow(context.Background(), store.Request{
		Collection: collection,
		Limit:      3,
		IndexWindow: &store.IndexWindow{
			Path: rankPath, LowerBound: "a", UpperBound: "z",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !window.HasMore {
		t.Fatal("window did not report its overflow sentinel")
	}
	if len(window.Documents) != 3 {
		t.Fatalf("window documents = %d, want 3", len(window.Documents))
	}
	for index, expected := range []string{"a", "b", "c"} {
		if actual := window.Documents[index].ID; actual != expected {
			t.Fatalf("window document %d = %q, want %q", index, actual, expected)
		}
	}
	events := backend.Events()[eventOffset:]
	materialized := 0
	for _, event := range events {
		if event == "list" {
			t.Fatalf("window delegated to the full-count list path: %#v", events)
		}
		if event == "list-window-materialize" {
			materialized++
		}
	}
	if materialized != 3 {
		t.Fatalf("window materialized %d documents, want the requested bound 3; events = %#v", materialized, events)
	}
}

func (ctx *cancelAfterChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *cancelAfterChecks) Done() <-chan struct{}       { return nil }
func (ctx *cancelAfterChecks) Value(any) any               { return nil }
func (ctx *cancelAfterChecks) Err() error {
	ctx.checks++
	if ctx.checks > ctx.after {
		return context.Canceled
	}
	return nil
}

func TestFilteredSelectionObservesCancellationDuringItsBoundedScan(t *testing.T) {
	backend := New()
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "posts"}
	for index := 0; index < 10; index++ {
		if _, err := transaction.Create(context.Background(), store.CreateRequest{
			Collection: collection,
			ID:         string(rune('a' + index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	canceling := &cancelAfterChecks{after: 4}
	if _, err := transaction.ResolveFilteredSelection(canceling, store.FilteredSelectionRequest{
		Collection: collection,
		Limit:      3,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("filtered selection error = %v, want context canceled", err)
	}
}

func TestCanceledBootstrapCommitReleasesFirstUserFence(t *testing.T) {
	backend := New()
	collection := schema.Collection{ID: "users"}
	first, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Create(context.Background(), store.CreateRequest{Collection: collection, ID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := first.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(context.Background(), collection, "first", []byte("first"), true); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := first.Commit(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled bootstrap commit = %v", err)
	}
	defer first.Rollback(context.Background())

	second, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(context.Background())
	if _, err := second.Create(context.Background(), store.CreateRequest{Collection: collection, ID: "second"}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- second.(store.AuthBootstrapTransaction).CreateFirstAuthCredential(context.Background(), collection, "second", []byte("second"), true)
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("bootstrap after canceled commit = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bootstrap fence remained locked after canceled commit")
	}
}

func TestSaveVersionReplacesTheSameRevision(t *testing.T) {
	backend := New()
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	versions := transaction.(store.VersionTransaction)
	collection := schema.Collection{ID: "posts", Versions: &schema.VersionSettings{MaxPerDocument: 10}}
	document := store.Document{
		ID: "post-1", Revision: 1, Status: store.StatusDraft,
		Values: store.Values{"title": store.String("First")},
	}
	first, err := versions.SaveVersion(context.Background(), collection, document, 10)
	if err != nil {
		t.Fatal(err)
	}
	document.Values["title"] = store.String("Replacement")
	second, err := versions.SaveVersion(context.Background(), collection, document, 10)
	if err != nil {
		t.Fatal(err)
	}
	items, err := versions.ListVersions(context.Background(), store.VersionRequest{Collection: collection, DocumentID: document.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "post-1:1" {
		t.Fatalf("versions = %#v", items)
	}
	title, _ := items[0].Snapshot.Values["title"].StringValue()
	if title != "Replacement" || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replacement = %q, created %s then %s", title, first.CreatedAt, second.CreatedAt)
	}
}

func TestUpdateHonorsTheRequestedDeletionScope(t *testing.T) {
	backend := New()
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	titlePath, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "posts", Fields: []schema.Field{{
		ID: "posts-title", Name: "title", Path: titlePath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
	}}}
	document, err := transaction.Create(context.Background(), store.CreateRequest{
		Collection: collection, ID: "post-1", Values: store.Values{"title": store.String("Original")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Trash(context.Background(), store.Request{Collection: collection, ID: document.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Update(context.Background(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: document.ID, Deletion: store.DeletionActive},
		Values:  store.Values{"title": store.String("Must not update trash")},
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("active update of trashed row = %v, want store.ErrNotFound", err)
	}
	updated, err := transaction.Update(context.Background(), store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: document.ID, Deletion: store.DeletionTrash},
		Values:  store.Values{"title": store.String("Trash-scoped update")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := updated.Values["title"].StringValue(); title != "Trash-scoped update" {
		t.Fatalf("trash-scoped update title = %q", title)
	}
}
