package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLitePreferencesAndDocumentLocksRequireActiveDocuments(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "support"}, Plugins: []schema.Plugin{},
	}))
	now := time.Date(2026, time.August, 29, 12, 0, 0, 123, time.UTC)
	backend.now = func() time.Time { return now }
	insertSQLiteSupportDocument(t, backend, "users", "user-1", nil, false)
	insertSQLiteSupportDocument(t, backend, "users", "user-2", nil, false)
	insertSQLiteSupportDocument(t, backend, "posts", "post-1", nil, false)

	preference, err := backend.SetPreference(ctx, store.Preference{
		CollectionID: "users", UserID: "user-1", Key: "theme", Value: json.RawMessage(`{"mode":"dark"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if preference.UpdatedAt != now || string(preference.Value) != `{"mode":"dark"}` {
		t.Fatalf("SetPreference() = %#v", preference)
	}
	preference.Value[0] = '['
	stored, err := backend.GetPreference(ctx, "users", "user-1", "theme")
	if err != nil {
		t.Fatal(err)
	}
	if stored.UpdatedAt != now || string(stored.Value) != `{"mode":"dark"}` {
		t.Fatalf("GetPreference() = %#v", stored)
	}
	if err := backend.DeletePreference(ctx, "users", "user-1", "theme"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.GetPreference(ctx, "users", "user-1", "theme"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetPreference() after delete error = %v", err)
	}

	first := store.DocumentLock{
		CollectionID: "posts", DocumentID: "post-1", OwnerCollectionID: "users", OwnerID: "user-1", OwnerLabel: "First",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	acquired, won, err := backend.AcquireDocumentLock(ctx, first, now, false)
	if err != nil || !won || acquired.OwnerID != "user-1" {
		t.Fatalf("first AcquireDocumentLock() = %#v, %v, %v", acquired, won, err)
	}
	second := store.DocumentLock{
		CollectionID: "posts", DocumentID: "post-1", OwnerCollectionID: "users", OwnerID: "user-2", OwnerLabel: "Second",
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second), ExpiresAt: now.Add(2 * time.Minute),
	}
	blocked, won, err := backend.AcquireDocumentLock(ctx, second, now, false)
	if err != nil || won || blocked.OwnerID != "user-1" {
		t.Fatalf("blocked AcquireDocumentLock() = %#v, %v, %v", blocked, won, err)
	}
	taken, won, err := backend.AcquireDocumentLock(ctx, second, now, true)
	if err != nil || !won || taken.OwnerID != "user-2" {
		t.Fatalf("takeover AcquireDocumentLock() = %#v, %v, %v", taken, won, err)
	}
	if err := backend.ReleaseDocumentLock(ctx, "posts", "post-1", "users", "user-1"); err != nil {
		t.Fatal(err)
	}
	if current, err := backend.FindDocumentLock(ctx, "posts", "post-1", now); err != nil || current.OwnerID != "user-2" {
		t.Fatalf("lock after wrong-owner release = %#v, %v", current, err)
	}
	if err := backend.ReleaseDocumentLock(ctx, "posts", "post-1", "users", "user-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindDocumentLock(ctx, "posts", "post-1", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("FindDocumentLock() after release error = %v", err)
	}

	if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_documents SET deleted_at = ? WHERE collection_id = ? AND id = ?`, encodeTime(now), "users", "user-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.SetPreference(ctx, store.Preference{CollectionID: "users", UserID: "user-1", Key: "late", Value: json.RawMessage(`true`)}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preference for trashed owner error = %v", err)
	}
	first.UpdatedAt = now.Add(time.Second)
	if _, _, err := backend.AcquireDocumentLock(ctx, first, now, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("lock for trashed owner error = %v", err)
	}
}

func TestSQLiteUploadObjectLocksAreDeterministicAndTransactionReentrant(t *testing.T) {
	ctx := context.Background()
	backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "locks"}, Plugins: []schema.Plugin{},
	}))
	release, err := backend.LockUploadObjects(ctx, []string{"object-b", "object-a", "object-a"})
	if err != nil {
		t.Fatal(err)
	}
	blockedContext, cancel := context.WithCancel(ctx)
	cancel()
	if blockedRelease, blockedError := backend.LockUploadObjects(blockedContext, []string{"object-a"}); !errors.Is(blockedError, context.Canceled) {
		if blockedRelease != nil {
			blockedRelease()
		}
		release()
		t.Fatalf("blocked LockUploadObjects() error = %v", blockedError)
	}
	release()
	release()
	retryRelease, err := backend.LockUploadObjects(ctx, []string{"object-a"})
	if err != nil {
		t.Fatal(err)
	}
	retryRelease()

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	locker := transaction.(store.UploadObjectLocker)
	firstRelease, err := locker.LockUploadObjects(ctx, []string{"object-c", "object-c"})
	if err != nil {
		t.Fatal(err)
	}
	secondRelease, err := locker.LockUploadObjects(ctx, []string{"object-c"})
	if err != nil {
		t.Fatal(err)
	}
	firstRelease()
	secondRelease()
	if invalidRelease, err := locker.LockUploadObjects(ctx, []string{""}); err == nil {
		invalidRelease()
		t.Fatal("transaction upload lock accepted an empty key")
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteUploadObjectLocksCoordinateIndependentStores(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	release, err := first.LockUploadObjects(ctx, []string{"object"})
	if err != nil {
		t.Fatal(err)
	}
	blockedContext, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if blockedRelease, lockError := second.LockUploadObjects(blockedContext, []string{"object"}); !errors.Is(lockError, context.DeadlineExceeded) {
		if blockedRelease != nil {
			blockedRelease()
		}
		release()
		t.Fatalf("independent Store lock error = %v", lockError)
	}
	release()
	secondRelease, err := second.LockUploadObjects(ctx, []string{"object"})
	if err != nil {
		t.Fatal(err)
	}
	secondRelease()

	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})
	if err := first.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	transaction, err := first.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.(store.UploadObjectLocker).LockUploadObjects(ctx, []string{"adopted"}); err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatal(err)
	}
	blockedContext, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if blockedRelease, lockError := second.LockUploadObjects(blockedContext, []string{"adopted"}); !errors.Is(lockError, context.DeadlineExceeded) {
		if blockedRelease != nil {
			blockedRelease()
		}
		_ = transaction.Rollback(ctx)
		t.Fatalf("transaction-held independent Store lock error = %v", lockError)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	afterRollback, err := second.LockUploadObjects(ctx, []string{"adopted"})
	if err != nil {
		t.Fatal(err)
	}
	afterRollback()
}

func TestSQLiteUploadObjectLocksCanonicalizeSymlinkAliases(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	realPath := filepath.Join(directory, "real.sqlite")
	aliasPath := filepath.Join(directory, "alias.sqlite")
	realStore, err := Open(ctx, realPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = realStore.Close() })
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Skipf("create SQLite path alias: %v", err)
	}
	aliasStore, err := Open(ctx, aliasPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aliasStore.Close() })
	if realStore.lockPath != aliasStore.lockPath {
		t.Fatalf("lock paths differ for one SQLite file: %q != %q", realStore.lockPath, aliasStore.lockPath)
	}

	release, err := realStore.LockUploadObjects(ctx, []string{"object"})
	if err != nil {
		t.Fatal(err)
	}
	blockedContext, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if blockedRelease, lockError := aliasStore.LockUploadObjects(blockedContext, []string{"object"}); !errors.Is(lockError, context.DeadlineExceeded) {
		if blockedRelease != nil {
			blockedRelease()
		}
		release()
		t.Fatalf("symlink-alias lock error = %v, want deadline", lockError)
	}
	release()
	retry, err := aliasStore.LockUploadObjects(ctx, []string{"object"})
	if err != nil {
		t.Fatal(err)
	}
	retry()
}

func TestSQLiteTransactionUploadLockDoesNotWaitWhileHoldingWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lock-order.sqlite")
	preparation, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = preparation.Close() })
	adoption, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adoption.Close() })
	if err := preparation.Migrate(ctx, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})); err != nil {
		t.Fatal(err)
	}

	releasePreparation, err := preparation.LockUploadObjects(ctx, []string{"object"})
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := adoption.Begin(ctx)
	if err != nil {
		releasePreparation()
		t.Fatal(err)
	}
	if release, lockError := transaction.(store.UploadObjectLocker).LockUploadObjects(ctx, []string{"object"}); !errors.Is(lockError, store.ErrConflict) {
		if release != nil {
			release()
		}
		_ = transaction.Rollback(ctx)
		releasePreparation()
		t.Fatalf("transaction upload lock error = %v, want conflict", lockError)
	}
	if err := transaction.Rollback(ctx); err != nil {
		releasePreparation()
		t.Fatal(err)
	}
	releasePreparation()

	retry, err := adoption.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retry.(store.UploadObjectLocker).LockUploadObjects(ctx, []string{"object"}); err != nil {
		_ = retry.Rollback(ctx)
		t.Fatalf("transaction upload lock after release: %v", err)
	}
	if err := retry.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteUploadObjectLockCoordinatesSubprocess(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "subprocess.sqlite")
	backend, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	release, err := backend.LockUploadObjects(ctx, []string{"object"})
	if err != nil {
		t.Fatal(err)
	}
	runSQLiteUploadLockHelper(t, path, "blocked")
	release()
	runSQLiteUploadLockHelper(t, path, "available")
}

func TestSQLiteUploadObjectLockSubprocessHelper(t *testing.T) {
	path := os.Getenv("RIDU_SQLITE_UPLOAD_LOCK_HELPER_PATH")
	mode := os.Getenv("RIDU_SQLITE_UPLOAD_LOCK_HELPER_MODE")
	if path == "" || mode == "" {
		t.Skip("subprocess helper")
	}
	backend, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	release, err := backend.LockUploadObjects(ctx, []string{"object"})
	switch mode {
	case "blocked":
		if release != nil {
			release()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("subprocess lock error = %v, want deadline", err)
		}
	case "available":
		if err != nil {
			t.Fatal(err)
		}
		release()
	default:
		t.Fatalf("unknown subprocess helper mode %q", mode)
	}
}

func runSQLiteUploadLockHelper(t *testing.T, path, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteUploadObjectLockSubprocessHelper$")
	command.Env = append(os.Environ(),
		"RIDU_SQLITE_UPLOAD_LOCK_HELPER_PATH="+path,
		"RIDU_SQLITE_UPLOAD_LOCK_HELPER_MODE="+mode,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("subprocess upload lock helper (%s): %v\n%s", mode, err, output)
	}
}

func TestSQLiteSnapshotRejectsMutationEntryPoints(t *testing.T) {
	ctx := context.Background()
	collection := schema.Collection{ID: "posts", Slug: "posts"}
	backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "snapshot read only"},
		Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
	}))
	snapshot, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	versioned := snapshot.(store.VersionTransaction)
	auth := snapshot.(store.AuthTransaction)
	bootstrap := snapshot.(store.AuthBootstrapTransaction)
	unlock := snapshot.(store.AuthUnlockTransaction)
	uploads := snapshot.(store.UploadObjectLocker)
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
		{name: "upload-object-lock", run: func() error {
			release, err := uploads.LockUploadObjects(ctx, []string{"object-key"})
			if release != nil {
				release()
			}
			return err
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

func TestSQLiteTransactionRejectsUseAfterCompletion(t *testing.T) {
	ctx := context.Background()
	collection := schema.Collection{ID: "posts", Slug: "posts"}
	for _, test := range []struct {
		name   string
		finish func(store.Transaction) error
	}{
		{name: "commit", finish: func(transaction store.Transaction) error { return transaction.Commit(ctx) }},
		{name: "rollback", finish: func(transaction store.Transaction) error { return transaction.Rollback(ctx) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{
				Version: schema.CurrentVersion, Application: schema.Application{Name: "completed transaction"},
				Collections: []schema.Collection{collection}, Plugins: []schema.Plugin{},
			}))
			transaction, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.finish(transaction); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{name: "create", run: func() error {
					_, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "post-1"})
					return err
				}},
				{name: "find", run: func() error {
					_, err := transaction.Find(ctx, store.Request{Collection: collection, ID: "post-1"})
					return err
				}},
				{name: "commit", run: func() error { return transaction.Commit(ctx) }},
				{name: "rollback", run: func() error { return transaction.Rollback(ctx) }},
			} {
				t.Run(operation.name, func(t *testing.T) {
					if err := operation.run(); err == nil || err.Error() != "transaction already completed" {
						t.Fatalf("operation after transaction completion error = %v, want completed rejection", err)
					}
				})
			}
		})
	}
}

func TestSQLiteReferencedUploadObjectsIncludesTrashAndVersions(t *testing.T) {
	ctx := context.Background()
	upload := sqliteSupportUploadCollection("media", true)
	backend := newSQLiteSupportStore(t, schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "references"}, Collections: []schema.Collection{upload}, Plugins: []schema.Plugin{},
	}))
	insertSQLiteSupportDocument(t, backend, "media", "active", store.Values{
		"objectKey": store.String("objects/current"),
		"sizes": store.Object(store.Values{
			"thumb": store.Object(store.Values{"objectKey": store.String("objects/size")}),
		}),
	}, false)
	insertSQLiteSupportDocument(t, backend, "media", "trashed", store.Values{
		"objectKey": store.String("objects/trash"), "sizes": store.Object(store.Values{}),
	}, true)
	insertSQLiteSupportVersion(t, backend, upload, "historic", store.Values{
		"objectKey": store.String("objects/version"),
		"sizes": store.Object(store.Values{
			"card": store.Object(store.Values{"objectKey": store.String("objects/version-size")}),
		}),
	})

	transaction, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lookup := transaction.(store.UploadReferenceTransaction)
	got, err := lookup.ReferencedUploadObjects(ctx, store.UploadReferenceRequest{
		Collections: []schema.Collection{upload},
		ObjectKeys: []string{
			"objects/version-size", "objects/missing", "objects/trash", "objects/current", "objects/version", "objects/size",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"objects/current", "objects/size", "objects/trash", "objects/version", "objects/version-size"}
	if len(got) != len(want) {
		t.Fatalf("ReferencedUploadObjects() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ReferencedUploadObjects() = %#v, want %#v", got, want)
		}
	}
	if _, err := lookup.ReferencedUploadObjects(ctx, store.UploadReferenceRequest{
		Collections: []schema.Collection{upload}, ObjectKeys: []string{"duplicate", "duplicate"},
	}); err == nil {
		t.Fatal("ReferencedUploadObjects() accepted duplicate keys")
	}
	if _, err := lookup.ReferencedUploadObjects(ctx, store.UploadReferenceRequest{
		Collections: []schema.Collection{{ID: "posts"}}, ObjectKeys: []string{"objects/current"},
	}); err == nil {
		t.Fatal("ReferencedUploadObjects() accepted a non-upload collection")
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteVersionsAreScopedByCollection(t *testing.T) {
	ctx := context.Background()
	posts := schema.Collection{
		ID: "posts", Slug: "posts", Capabilities: schema.Capabilities{Versions: true},
		Versions: &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
	}
	pages := posts
	pages.ID = "pages"
	pages.Slug = "pages"
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Collections: []schema.Collection{posts, pages},
	})
	backend := newSQLiteSupportStore(t, manifest)

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	versions := transaction.(store.VersionTransaction)
	var versionIDs []string
	for _, collection := range []schema.Collection{posts, pages} {
		document, createError := transaction.Create(ctx, store.CreateRequest{
			Collection: collection, ID: "same-id", Values: store.Values{},
		})
		if createError != nil {
			_ = transaction.Rollback(ctx)
			t.Fatal(createError)
		}
		version, saveError := versions.SaveVersion(ctx, collection, document, 10)
		if saveError != nil {
			_ = transaction.Rollback(ctx)
			t.Fatalf("SaveVersion(%s): %v", collection.ID, saveError)
		}
		versionIDs = append(versionIDs, version.ID)
	}
	if versionIDs[0] != versionIDs[1] {
		t.Fatalf("version IDs = %#v, want collection-scoped duplicate IDs", versionIDs)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	snapshot, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Rollback(ctx)
	for _, collection := range []schema.Collection{posts, pages} {
		got, err := snapshot.(store.VersionTransaction).FindVersion(ctx, collection, "same-id", 1)
		if err != nil || got.DocumentID != "same-id" {
			t.Fatalf("FindVersion(%s) = %#v, %v", collection.ID, got, err)
		}
	}
}

func newSQLiteSupportStore(t *testing.T, manifest schema.Manifest) *Store {
	t.Helper()
	backend, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	return backend
}

func insertSQLiteSupportDocument(t *testing.T, backend *Store, collectionID, documentID string, values store.Values, trashed bool) {
	t.Helper()
	if values == nil {
		values = store.Values{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	now := encodeTime(time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC))
	var deletedAt any
	if trashed {
		deletedAt = now
	}
	if _, err := backend.db.Exec(`INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json)
VALUES (?, ?, ?, ?, ?, '', 0, ?)`, collectionID, documentID, now, now, deletedAt, string(encoded)); err != nil {
		t.Fatal(err)
	}
}

func insertSQLiteSupportVersion(t *testing.T, backend *Store, collection schema.Collection, documentID string, values store.Values) {
	t.Helper()
	now := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	snapshot, err := json.Marshal(store.Document{
		ID: documentID, CreatedAt: now, UpdatedAt: now, Status: store.StatusDraft, Revision: 1, Values: values,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.Exec(`INSERT INTO ridu_versions
  (id, collection_id, document_id, revision, status, snapshot_json, created_at)
VALUES (?, ?, ?, 1, ?, ?, ?)`, documentID+":1", string(collection.ID), documentID, string(store.StatusDraft), string(snapshot), encodeTime(now)); err != nil {
		t.Fatal(err)
	}
}

func sqliteSupportUploadCollection(id schema.StableID, versioned bool) schema.Collection {
	objectKeyPath, _ := query.NewPath("objectKey")
	sizesPath, _ := query.NewPath("sizes")
	collection := schema.Collection{
		ID: id, Slug: schema.CollectionSlug(id), Capabilities: schema.Capabilities{Upload: true}, Upload: &schema.UploadSettings{},
		Fields: []schema.Field{
			{ID: id + "-object-key", Name: "objectKey", Path: objectKeyPath, Type: schema.FieldTypeText, Category: schema.FieldCategoryUpload},
			{ID: id + "-sizes", Name: "sizes", Path: sizesPath, Type: schema.FieldTypeJSON, Category: schema.FieldCategoryUpload},
		},
	}
	if versioned {
		collection.Capabilities.Versions = true
		collection.Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	}
	return collection
}
