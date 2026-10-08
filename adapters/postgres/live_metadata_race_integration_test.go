package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

// A locked working read waits for a concurrent writer's row lock. In READ
// COMMITTED PostgreSQL then returns the newest working row, but any other
// table read in the same statement keeps the statement's original snapshot.
// The live revision and pending-draft flag must therefore come from a
// statement that starts after the lock is held, or a draft save that waited
// for a first publication (or an unpublication) acts on stale live state.
func TestPostgresLockedWorkingReadSeesLiveStateCommittedDuringLockWait(t *testing.T) {
	ctx := context.Background()
	backend, admin, applicationName := lockWaitTestBackend(t)
	collection := lockWaitDraftCollection(t, ctx, backend)

	create := func(id string, status store.Status) {
		t.Helper()
		transaction, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(ctx)
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: id, Status: status, Values: store.Values{"title": store.String("Created")}}); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// race holds intent's write open, starts second on the same document in
	// another transaction, waits until second blocks on the row lock, and
	// then commits the first write.
	race := func(id string, intent store.WriteIntent, second func(store.Transaction) error) error {
		t.Helper()
		first, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Rollback(ctx)
		if _, err := conformance.LockedUpdate(ctx, first, store.UpdateRequest{
			Request: store.Request{Collection: collection, ID: id}, Intent: intent,
		}); err != nil {
			t.Fatalf("%s: %v", intent, err)
		}
		done := make(chan error, 1)
		go func() {
			done <- func() error {
				transaction, err := backend.Begin(ctx)
				if err != nil {
					return err
				}
				defer transaction.Rollback(ctx)
				if err := second(transaction); err != nil {
					return err
				}
				return transaction.Commit(ctx)
			}()
		}()
		waitForPostgresRowLockWait(t, ctx, admin, applicationName)
		if err := first.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("the waiting write did not finish after the concurrent write committed")
			return nil
		}
	}
	// raceDraftSave saves a draft without an expected revision while intent's
	// write holds the row.
	raceDraftSave := func(id string, intent store.WriteIntent) error {
		t.Helper()
		return race(id, intent, func(transaction store.Transaction) error {
			_, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
				Request: store.Request{Collection: collection, ID: id}, Intent: store.WriteIntentSaveDraft,
				Values: store.Values{"title": store.String("Saved during " + string(intent))},
			})
			return err
		})
	}
	read := func(id string, publishedOnly bool) (store.Document, error) {
		t.Helper()
		transaction, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(ctx)
		return transaction.Find(ctx, store.Request{Collection: collection, ID: id, PublishedOnly: publishedOnly})
	}

	t.Run("first publication", func(t *testing.T) {
		create("first-publication", store.StatusDraft)
		if err := raceDraftSave("first-publication", store.WriteIntentPublish); err != nil {
			t.Fatalf("draft save after a concurrent first publication = %v", err)
		}
		working, err := read("first-publication", false)
		if err != nil {
			t.Fatal(err)
		}
		if working.Status != store.StatusPublished || working.Revision != 3 || working.PublishedRevision != 2 || !working.HasDraftChanges {
			t.Fatalf("working head after a draft save that waited for publication = status %q revision %d live %d pending %t, want published 3 live 2 pending",
				working.Status, working.Revision, working.PublishedRevision, working.HasDraftChanges)
		}
		if live, err := read("first-publication", true); err != nil || live.Revision != 2 {
			t.Fatalf("live head after the concurrent draft save = %#v, %v", live, err)
		}
	})
	t.Run("unpublication", func(t *testing.T) {
		create("unpublication", store.StatusPublished)
		if err := raceDraftSave("unpublication", store.WriteIntentUnpublish); err != nil {
			t.Fatalf("draft save after a concurrent unpublication = %v", err)
		}
		working, err := read("unpublication", false)
		if err != nil {
			t.Fatal(err)
		}
		if working.Status != store.StatusDraft || working.Revision != 3 || working.PublishedRevision != 0 || working.HasDraftChanges {
			t.Fatalf("working head after a draft save that waited for unpublication = status %q revision %d live %d pending %t, want draft 3 without a live head",
				working.Status, working.Revision, working.PublishedRevision, working.HasDraftChanges)
		}
		if _, err := read("unpublication", true); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("live head after unpublication and a draft save = %v, want not found", err)
		}
	})
	// Trash mirrors deletion into the live row. A trash that waited for a first
	// publication must also trash the live row that publication created.
	t.Run("trash", func(t *testing.T) {
		create("trash", store.StatusDraft)
		var trashed store.Document
		if err := race("trash", store.WriteIntentPublish, func(transaction store.Transaction) error {
			var err error
			trashed, err = transaction.Trash(ctx, store.Request{Collection: collection, ID: "trash"})
			return err
		}); err != nil {
			t.Fatalf("trash after a concurrent first publication = %v", err)
		}
		if live, err := read("trash", true); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("active live read after trash = revision %d, %v; the live row escaped trash", live.Revision, err)
		}
		if trashed.DeletedAt == nil || trashed.PublishedRevision != 2 || trashed.HasDraftChanges {
			t.Errorf("trash result = deleted %v live %d pending %t, want trashed with live revision 2", trashed.DeletedAt, trashed.PublishedRevision, trashed.HasDraftChanges)
		}
	})
}

func lockWaitDraftCollection(t *testing.T, ctx context.Context, backend *Store) schema.Collection {
	t.Helper()
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Live state lock waits"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Versions: &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
			Fields: []schema.Field{atlasTextField("posts-title", "title")},
		}},
	})
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	return manifest.Snapshot().Collections[0]
}

// lockWaitTestBackend opens a store in a disposable schema whose sessions carry
// a unique application name, so a test can observe exactly its own lock waits.
func lockWaitTestBackend(t *testing.T) (*Store, *pgxpool.Pool, string) {
	t.Helper()
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	schemaName, applicationName := "ridu_lock_wait_"+suffix, "ridu-lock-wait-"+suffix
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
		admin.Close()
	})
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	backend, err := OpenWithConfig(ctx, PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: true, ApplicationName: applicationName})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	return backend, admin, applicationName
}

// waitForPostgresRowLockWait returns once a session of applicationName waits
// for another transaction's lock.
func waitForPostgresRowLockWait(t *testing.T, ctx context.Context, admin *pgxpool.Pool, applicationName string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_stat_activity
WHERE datname = current_database() AND application_name = $1 AND wait_event_type = 'Lock'
  AND cardinality(pg_blocking_pids(pid)) > 0
)`, applicationName).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the concurrent write did not wait for the row lock")
}
