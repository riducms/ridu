package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresUploadReferenceAdmissionAndLockBoundary(t *testing.T) {
	ctx := context.Background()
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	visiblePath, err := query.NewPath("visible")
	if err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	var lockedOnce sync.Once
	config := ridu.Config{
		Name: "PostgreSQL upload reference admission", Storage: storageBackend, StorageNamespace: "postgres-upload-reference-admission",
		Collections: []ridu.Collection{
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
				Fields:       field.Fields{field.Checkbox("visible").Required()},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
				}},
			},
			{
				Slug: "entries", Fields: field.Fields{field.Uploads("assets", "media")},
				Hooks: ridu.CollectionHooks{AfterOperation: []ridu.Hook{func(hookContext ridu.HookContext) error {
					if hookContext.Operation != operation.Create {
						return nil
					}
					lockedOnce.Do(func() { close(locked) })
					select {
					case <-release:
						return errors.New("force rollback after reference lock proof")
					case <-hookContext.Context.Done():
						return hookContext.Context.Err()
					}
				}}},
			},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "visible.txt", Reader: strings.NewReader("visible"), Data: store.Values{"visible": store.Boolean(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "hidden.txt", Reader: strings.NewReader("hidden"), Data: store.Values{"visible": store.Boolean(false)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{"assets": store.List(store.String("missing"))}, nil); !hasUploadReferenceIssue(err, "assets.0") {
		t.Fatalf("missing PostgreSQL upload reference error = %v", err)
	}
	if _, err := application.Local().Create(ctx, "entries", store.Values{"assets": store.List(store.String(hidden.ID))}, nil); !hasUploadReferenceIssue(err, "assets.0") {
		t.Fatalf("filtered PostgreSQL upload reference error = %v", err)
	}

	createResult := make(chan error, 1)
	go func() {
		_, createError := application.Local().Create(ctx, "entries", store.Values{"assets": store.List(store.String(visible.ID))}, nil)
		createResult <- createError
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted upload write did not reach the post-write transaction boundary")
	}

	mediaCollection := collectionBySlug(t, manifest.Snapshot().Collections, "media")
	deleter, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deletionContext, cancelDeletion := context.WithTimeout(ctx, 5*time.Second)
	defer cancelDeletion()
	deleteResult := make(chan error, 1)
	go func() {
		_, deleteError := deleter.Delete(deletionContext, store.Request{Collection: mediaCollection, ID: visible.ID})
		deleteResult <- deleteError
	}()
	select {
	case err := <-deleteResult:
		_ = deleter.Rollback(context.Background())
		close(release)
		t.Fatalf("upload target delete completed while the accepted reference transaction was open: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-createResult:
		if !hasOperationCode(err, "hook_failed") {
			_ = deleter.Rollback(context.Background())
			t.Fatalf("reference-lock proof write error = %v", err)
		}
	case <-time.After(3 * time.Second):
		_ = deleter.Rollback(context.Background())
		t.Fatal("reference-lock proof write did not roll back")
	}
	select {
	case err := <-deleteResult:
		if err != nil {
			_ = deleter.Rollback(context.Background())
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		_ = deleter.Rollback(context.Background())
		t.Fatal("upload target delete did not resume after reference transaction rollback")
	}
	if err := deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresHasManyUploadRoundTripsThroughPublishValidation(t *testing.T) {
	ctx := context.Background()
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{
		Name:             "PostgreSQL has-many upload round trip",
		Storage:          storageBackend,
		StorageNamespace: "postgres-has-many-upload-round-trip",
		Collections: []ridu.Collection{
			{
				Slug:         "media",
				Upload:       true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			},
			{
				Slug:          "posts",
				Versions:      true,
				VersionConfig: ridu.VersionConfig{Drafts: true},
				Fields:        field.Fields{field.Text("title").Required(), field.Uploads("gallery", "media")},
			},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "gallery.txt",
		Reader:   strings.NewReader("gallery"),
	})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(ctx, "posts", store.Values{
		"title":   store.String("Has-many upload round trip"),
		"gallery": store.List(store.String(asset.ID)),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	published, err := application.Local().Publish(ctx, "posts", draft.ID, draft.Revision, nil)
	if err != nil {
		t.Fatalf("publish PostgreSQL document after reading has-many upload: %v", err)
	}
	gallery, valid := published.Values["gallery"].CopyList()
	if !valid || len(gallery) != 1 {
		t.Fatalf("published gallery = %#v", published.Values["gallery"])
	}
	assetID, valid := gallery[0].StringValue()
	if !valid || assetID != asset.ID {
		t.Fatalf("published gallery asset = %#v, want %q", gallery[0], asset.ID)
	}
}

func TestPostgresUploadObjectLocksBlockAcrossSessionsAndReenterTransaction(t *testing.T) {
	ctx := context.Background()
	backend, _ := integrationBackend(t, ctx, ridu.Config{
		Name:        "PostgreSQL upload object locks",
		Collections: []ridu.Collection{{Slug: "documents"}},
	})

	release, err := backend.LockUploadObjects(ctx, []string{"object-b", "object-a", "object-a"})
	if err != nil {
		t.Fatal(err)
	}
	blockedContext, cancelBlocked := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelBlocked()
	if blockedRelease, blockedError := backend.LockUploadObjects(blockedContext, []string{"object-a"}); blockedError == nil {
		blockedRelease()
		release()
		t.Fatal("a second PostgreSQL session acquired a held upload object lock")
	}
	release()
	retryRelease, err := backend.LockUploadObjects(ctx, []string{"object-a"})
	if err != nil {
		t.Fatalf("lock did not resume after release: %v", err)
	}
	retryRelease()

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	transactionLocker, ok := transaction.(store.UploadObjectLocker)
	if !ok {
		_ = transaction.Rollback(ctx)
		t.Fatal("PostgreSQL document transaction does not implement upload object locking")
	}
	firstRelease, err := transactionLocker.LockUploadObjects(ctx, []string{"object-c"})
	if err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatal(err)
	}
	reentrantContext, cancelReentrant := context.WithTimeout(ctx, time.Second)
	defer cancelReentrant()
	secondRelease, err := transactionLocker.LockUploadObjects(reentrantContext, []string{"object-c"})
	if err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatalf("transaction-scoped upload object lock was not re-entrant: %v", err)
	}
	secondRelease()
	firstRelease()
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresUploadCompletesWithOneDocumentConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{
		Name: "single connection upload", Storage: storageBackend, StorageNamespace: "single-connection-upload",
		Collections: []ridu.Collection{{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		}},
	}
	backend, manifest := integrationBackendWithPoolConfig(t, ctx, config, postgres.PoolConfig{
		MaxConnections: 1, MaxUploadLockConnections: 1,
	})
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "single.txt", Reader: strings.NewReader("single connection"),
	})
	if err != nil {
		t.Fatalf("upload with one document connection: %v", err)
	}
	key, valid := document.Values["objectKey"].StringValue()
	if !valid || key == "" {
		t.Fatalf("uploaded metadata = %#v", document.Values)
	}
	reader, _, err := application.OpenUpload(ctx, "media", key, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()

	releaseLock, err := backend.LockUploadObjects(ctx, []string{"pool-slot-a"})
	if err != nil {
		t.Fatal(err)
	}
	blockedContext, cancelBlocked := context.WithTimeout(ctx, 100*time.Millisecond)
	if blockedRelease, blockedError := backend.LockUploadObjects(blockedContext, []string{"pool-slot-b"}); blockedError == nil {
		blockedRelease()
		releaseLock()
		cancelBlocked()
		t.Fatal("bounded upload-lock pool admitted more than its configured capacity")
	}
	cancelBlocked()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		releaseLock()
		t.Fatalf("saturated upload-lock pool starved the document pool: %v", err)
	}
	if err := transaction.Rollback(ctx); err != nil {
		releaseLock()
		t.Fatal(err)
	}
	releaseLock()
	retryRelease, err := backend.LockUploadObjects(ctx, []string{"pool-slot-b"})
	if err != nil {
		t.Fatalf("upload-lock capacity did not recover after release: %v", err)
	}
	retryRelease()
}

func hasUploadReferenceIssue(err error, path string) bool {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		return false
	}
	for _, issue := range operationError.Issues {
		if issue.Code == "invalid_upload" && issue.Path == path && issue.Message == "upload target is unavailable" {
			return true
		}
	}
	return false
}

func collectionBySlug(t *testing.T, collections []schema.Collection, slug schema.CollectionSlug) schema.Collection {
	t.Helper()
	for _, collection := range collections {
		if collection.Slug == slug {
			return collection
		}
	}
	t.Fatalf("collection %q was not resolved", slug)
	return schema.Collection{}
}
