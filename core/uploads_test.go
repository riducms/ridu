package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

type observedReader struct {
	reads int
}

func listAllStorageObjects(ctx context.Context, backend storage.Backend, prefix string) ([]storage.Object, error) {
	var objects []storage.Object
	cursor := ""
	for {
		page, err := backend.List(ctx, storage.ListRequest{Prefix: prefix, Cursor: cursor, Limit: storage.MaxListPageSize})
		if err != nil {
			return nil, err
		}
		objects = append(objects, page.Objects...)
		if page.NextCursor == "" {
			return objects, nil
		}
		cursor = page.NextCursor
	}
}

type missingModifiedAtBackend struct{ storage.Backend }

func (backend missingModifiedAtBackend) List(ctx context.Context, request storage.ListRequest) (storage.ListPage, error) {
	page, err := backend.Backend.List(ctx, request)
	for index := range page.Objects {
		page.Objects[index].ModifiedAt = time.Time{}
	}
	return page, err
}

type failingDeleteBackend struct{ storage.Backend }

type uploadLockOnlyStore struct {
	store.Store
	locker store.UploadObjectLocker
}

func (backend uploadLockOnlyStore) LockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	return backend.locker.LockUploadObjects(ctx, keys)
}

func (backend failingDeleteBackend) Delete(context.Context, string) error {
	return errors.New("object deletion unavailable")
}

func (backend failingDeleteBackend) List(ctx context.Context, request storage.ListRequest) (storage.ListPage, error) {
	page, err := backend.Backend.List(ctx, request)
	for index := range page.Objects {
		page.Objects[index].ModifiedAt = time.Now().Add(-10 * time.Minute)
	}
	return page, err
}

type adversarialListBackend struct {
	storage.Backend
	maliciousKey string
	deleted      []string
}

type blockingListBackend struct {
	storage.Backend
	listed  chan struct{}
	release chan struct{}
	once    sync.Once
}

type blockingDeleteBackend struct {
	storage.Backend
	deleting chan struct{}
	release  chan struct{}
	once     sync.Once
}

type staleListedObjectsBackend struct{ storage.Backend }

func (backend staleListedObjectsBackend) List(ctx context.Context, request storage.ListRequest) (storage.ListPage, error) {
	page, err := backend.Backend.List(ctx, request)
	for index := range page.Objects {
		page.Objects[index].ModifiedAt = time.Now().Add(-10 * time.Minute)
	}
	return page, err
}

type partialVariantFailureBackend struct {
	storage.Backend
	originalPut  chan string
	deleting     chan string
	allowDelete  chan struct{}
	deleteWaited sync.Once
}

type recordingPutBackend struct {
	storage.Backend
	mu   sync.Mutex
	keys []string
}

func (backend *recordingPutBackend) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if err := backend.Backend.Put(ctx, key, reader, size, contentType); err != nil {
		return err
	}
	backend.mu.Lock()
	backend.keys = append(backend.keys, key)
	backend.mu.Unlock()
	return nil
}

func (backend *recordingPutBackend) storedKeys() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]string(nil), backend.keys...)
}

func (backend *partialVariantFailureBackend) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if strings.Contains(key, "/sizes/") {
		return errors.New("variant storage unavailable")
	}
	if err := backend.Backend.Put(ctx, key, reader, size, contentType); err != nil {
		return err
	}
	select {
	case backend.originalPut <- key:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (backend *partialVariantFailureBackend) Delete(ctx context.Context, key string) error {
	var waitError error
	backend.deleteWaited.Do(func() {
		select {
		case backend.deleting <- key:
		case <-ctx.Done():
			waitError = ctx.Err()
			return
		}
		select {
		case <-backend.allowDelete:
		case <-ctx.Done():
			waitError = ctx.Err()
		}
	})
	if waitError != nil {
		return waitError
	}
	return backend.Backend.Delete(ctx, key)
}

type generatedObjectLockStore struct {
	*teststore.Store
	requested chan []string
	acquired  chan []string
}

func newGeneratedObjectLockStore() *generatedObjectLockStore {
	return &generatedObjectLockStore{Store: teststore.New(), requested: make(chan []string, 16), acquired: make(chan []string, 16)}
}

func (backend *generatedObjectLockStore) LockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	return backend.lockUploadObjects(ctx, keys)
}

func (backend *generatedObjectLockStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &generatedObjectLockTransaction{Transaction: transaction, lock: backend.lockUploadObjects}, nil
}

func (backend *generatedObjectLockStore) lockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	requested := append([]string(nil), keys...)
	select {
	case backend.requested <- requested:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release, err := backend.Store.LockUploadObjects(ctx, keys)
	if err != nil {
		return nil, err
	}
	select {
	case backend.acquired <- requested:
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
	return release, nil
}

type generatedObjectLockTransaction struct {
	store.Transaction
	lock func(context.Context, []string) (func(), error)
}

func (transaction *generatedObjectLockTransaction) LockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	return transaction.lock(ctx, keys)
}

type singleSessionObjectLockStore struct {
	*teststore.Store
	mu   sync.Mutex
	held bool
}

type ambiguousCommitStore struct {
	*teststore.Store
	mu       sync.Mutex
	failNext bool
}

func (backend *ambiguousCommitStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &ambiguousCommitTransaction{Transaction: transaction, backend: backend}, nil
}

func (backend *ambiguousCommitStore) arm() {
	backend.mu.Lock()
	backend.failNext = true
	backend.mu.Unlock()
}

func (backend *ambiguousCommitStore) consumeFailure() bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.failNext {
		return false
	}
	backend.failNext = false
	return true
}

type ambiguousCommitTransaction struct {
	store.Transaction
	backend *ambiguousCommitStore
}

func (transaction *ambiguousCommitTransaction) Commit(ctx context.Context) error {
	if err := transaction.Transaction.Commit(ctx); err != nil {
		return err
	}
	if transaction.backend.consumeFailure() {
		return errors.New("commit acknowledgement lost")
	}
	return nil
}

func (backend *singleSessionObjectLockStore) LockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	backend.mu.Lock()
	if backend.held {
		backend.mu.Unlock()
		return nil, errors.New("upload lock session capacity exhausted")
	}
	backend.held = true
	backend.mu.Unlock()
	release, err := backend.Store.LockUploadObjects(ctx, keys)
	if err != nil {
		backend.mu.Lock()
		backend.held = false
		backend.mu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			release()
			backend.mu.Lock()
			backend.held = false
			backend.mu.Unlock()
		})
	}, nil
}

func blockingChangeHook(operation ridu.Operation, entered chan<- struct{}, proceed <-chan struct{}) ridu.Hook {
	return func(ctx ridu.HookContext) error {
		if ctx.Operation != operation {
			return nil
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-proceed:
			return nil
		case <-ctx.Context.Done():
			return ctx.Context.Err()
		}
	}
}

func awaitGeneratedObjectLock(t *testing.T, backend *generatedObjectLockStore, phase string) []string {
	t.Helper()
	select {
	case keys := <-backend.requested:
		return keys
	case <-time.After(time.Second):
		t.Fatalf("%s did not request a generated-object lock", phase)
		return nil
	}
}

func requireSameSingleObjectLock(t *testing.T, preparation, cleanup []string) {
	t.Helper()
	if len(preparation) != 1 || len(cleanup) != 1 || preparation[0] != cleanup[0] {
		t.Fatalf("preparation lock = %v, cleanup lock = %v", preparation, cleanup)
	}
}

func requireAmbiguousCommitError(t *testing.T, err error) {
	t.Helper()
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "store_failed" || !operationError.CommitAttempted {
		t.Fatalf("ambiguous commit error = %v", err)
	}
}

func (backend *blockingDeleteBackend) Delete(ctx context.Context, key string) error {
	backend.once.Do(func() { close(backend.deleting) })
	select {
	case <-backend.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return backend.Backend.Delete(ctx, key)
}

func (backend *blockingListBackend) List(ctx context.Context, request storage.ListRequest) (storage.ListPage, error) {
	page, err := backend.Backend.List(ctx, request)
	if err != nil {
		return storage.ListPage{}, err
	}
	backend.once.Do(func() {
		close(backend.listed)
		select {
		case <-backend.release:
		case <-ctx.Done():
		}
	})
	return page, ctx.Err()
}

func (backend *adversarialListBackend) List(context.Context, storage.ListRequest) (storage.ListPage, error) {
	return storage.ListPage{Objects: []storage.Object{{Key: backend.maliciousKey, ModifiedAt: time.Now().Add(-10 * time.Minute)}}}, nil
}

func (backend *adversarialListBackend) Delete(ctx context.Context, key string) error {
	backend.deleted = append(backend.deleted, key)
	return backend.Backend.Delete(ctx, key)
}

func (reader *observedReader) Read([]byte) (int, error) {
	reader.reads++
	return 0, io.EOF
}

func TestUploadValidationDeliveryAndAfterCommitCleanup(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "uploads", Storage: backend, StorageNamespace: "uploads-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true, Trash: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}, Private: true},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	actor := &store.Document{ID: "editor"}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "../../notes file.txt", Reader: bytes.NewBufferString("hello"), Data: store.Values{"alt": store.String("Notes")}, Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := document.Values["objectKey"].StringValue()
	if _, _, err := application.OpenUpload(context.Background(), "media", key, nil); err == nil {
		t.Fatal("private upload was delivered without an actor")
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", key, actor)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := io.ReadAll(reader)
	reader.Close()
	if string(encoded) != "hello" {
		t.Fatalf("upload body = %q", encoded)
	}
	if _, err := application.Local().Delete(context.Background(), "media", document.ID, actor); err != nil {
		t.Fatal(err)
	}
	if reader, _, err := backend.Open(context.Background(), key); err != nil {
		t.Fatal("soft-deleted upload object was removed", err)
	} else {
		reader.Close()
	}
	if result, err := application.ReconcileUploads(context.Background(), 5*time.Minute); err != nil || result.Candidates != 0 || result.Deleted != 0 {
		t.Fatalf("trash reconciliation = %#v, %v", result, err)
	}
	if reader, _, err := backend.Open(context.Background(), key); err != nil {
		t.Fatal("reconciliation removed an object referenced by trash", err)
	} else {
		reader.Close()
	}
	if _, err := application.Local().DeletePermanent(context.Background(), "media", document.ID, actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.Open(context.Background(), key); err == nil {
		t.Fatal("object remained after permanent document deletion")
	}
}

func TestUploadSpecializedOperationsPreserveExactActorCollection(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staffOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor != nil && ctx.ActorCollection == "staff" {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}
	application, err := ridu.New(ridu.Config{
		Name: "exact upload identity", Admin: ridu.AdminConfig{User: "staff"}, Storage: backend, StorageNamespace: "exact-upload-identity",
		Collections: []ridu.Collection{
			{Slug: "staff", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, Private: true, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
				Access:       ridu.CollectionAccess{Create: staffOnly, Read: staffOnly, Update: staffOnly},
				Fields:       []field.Definition{field.Text("alt")},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	staff, err := application.Local().Create(ctx, "staff", store.Values{"email": store.String("editor@example.test")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := &ridu.AuthIdentity{Collection: "staff", Actor: staff}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Upload(ctx, "media", ridu.UploadInput{Filename: "blank.png", Reader: bytes.NewReader(encoded.Bytes()), Actor: &staff}); !operationCode(err, "access_denied") {
		t.Fatalf("actor-only upload = %v, want trusted-local blank collection to remain denied", err)
	}
	document, err := application.UploadForIdentity(ctx, "media", ridu.UploadInput{Filename: "image.png", Reader: bytes.NewReader(encoded.Bytes())}, identity)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := document.Values["objectKey"].StringValue()
	reader, _, err := application.OpenUploadForIdentity(ctx, "media", key, identity)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	duplicated, err := application.DuplicateForIdentity(ctx, "media", document.ID, store.Values{"alt": store.String("copy")}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if duplicated.ID == document.ID {
		t.Fatal("upload duplicate reused the source document ID")
	}
	updated, err := application.UpdateUploadImageForIdentity(ctx, "media", document.ID, ridu.UpdateUploadImageInput{
		FocalX: 25, FocalY: 75, ExpectedRevision: document.Revision,
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if focalX, _ := updated.Values["focalX"].NumberValue(); focalX != 25 {
		t.Fatalf("focalX = %v", focalX)
	}
}

func TestActiveUploadDeliveryCannotExecuteFromApplicationOrigin(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "active upload", Storage: backend, StorageNamespace: "active-upload-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"text/html"}},
		Fields:       []field.Definition{field.Text("alt")},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{
		Filename: "payload.html", Reader: strings.NewReader(`<!doctype html><script>top.location='/api/schema'</script>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	deliveryURL, _ := document.Values["url"].StringValue()
	response, err := handlerClient(application.Handler(ridu.HandlerOptions{})).Get("http://ridu.test" + deliveryURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Disposition") != "attachment" {
		t.Fatalf("active delivery status=%d disposition=%q", response.StatusCode, response.Header.Get("Content-Disposition"))
	}
	if policy := response.Header.Get("Content-Security-Policy"); !strings.Contains(policy, "sandbox") || !strings.Contains(policy, "default-src 'none'") {
		t.Fatalf("active delivery CSP = %q", policy)
	}
	if response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("active delivery security headers = %#v", response.Header)
	}
	if response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("access-checked upload cache = %q", response.Header.Get("Cache-Control"))
	}
}

func TestRenamedUploadCollectionDeliversPriorNamespacedObjectWithoutRewritingMetadata(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const key = "ridu/upload-rename-test/objects/0123456789abcdef0123456789abcdef/prior.txt"
	if err := backend.Put(context.Background(), key, strings.NewReader("prior"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "upload rename", Storage: backend, StorageNamespace: "upload-rename-test",
		Collections: []ridu.Collection{{
			Slug: "assets", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	storedURL := "/api/uploads/media/" + key
	if _, err := application.Local().Import(context.Background(), "assets", store.Values{
		"filename": store.String("prior.txt"), "mimeType": store.String("text/plain"),
		"filesize": store.Number(5), "objectKey": store.String(key), "url": store.String(storedURL),
	}, ridu.ImportOptions{ID: "prior-asset", Status: store.StatusPublished}, nil); err != nil {
		t.Fatal(err)
	}

	reader, _, err := application.OpenUpload(context.Background(), "media", key, nil)
	if err != nil {
		t.Fatalf("open through prior collection route: %v", err)
	}
	body, _ := io.ReadAll(reader)
	reader.Close()
	if string(body) != "prior" {
		t.Fatalf("prior object body = %q", body)
	}
	response, err := handlerClient(application.Handler(ridu.HandlerOptions{})).Get("http://ridu.test" + storedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("prior stored URL = %d cache=%q: %s", response.StatusCode, response.Header.Get("Cache-Control"), readBody(t, response))
	}
	served, _ := io.ReadAll(response.Body)
	if string(served) != "prior" {
		t.Fatalf("prior stored URL body = %q", served)
	}
}

func TestRenamedUploadURLFallsBackWhenOldSlugIsReused(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const key = "ridu/upload-reused-slug-test/objects/0123456789abcdef0123456789abcdef/prior.txt"
	if err := backend.Put(context.Background(), key, strings.NewReader("prior"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "upload reused slug", Storage: backend, StorageNamespace: "upload-reused-slug-test",
		Collections: []ridu.Collection{
			{Slug: "assets", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}},
			{Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}, Private: true}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	storedURL := "/api/uploads/media/" + key
	if _, err := application.Local().Import(context.Background(), "assets", store.Values{
		"filename": store.String("prior.txt"), "mimeType": store.String("text/plain"),
		"filesize": store.Number(5), "objectKey": store.String(key), "url": store.String(storedURL),
	}, ridu.ImportOptions{ID: "prior-asset", Status: store.StatusPublished}, nil); err != nil {
		t.Fatal(err)
	}

	reader, _, err := application.OpenUpload(context.Background(), "media", key, nil)
	if err != nil {
		t.Fatalf("open through reused private collection hint: %v", err)
	}
	reader.Close()
	response, err := handlerClient(application.Handler(ridu.HandlerOptions{})).Get("http://ridu.test" + storedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("reused-slug stored URL = %d cache=%q: %s", response.StatusCode, response.Header.Get("Cache-Control"), readBody(t, response))
	}
}

func TestRenamedUploadURLFallbackCannotBypassActualOwnerAccess(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const key = "ridu/upload-reused-slug-access-test/objects/0123456789abcdef0123456789abcdef/private.txt"
	if err := backend.Put(context.Background(), key, strings.NewReader("private"), 7, "text/plain"); err != nil {
		t.Fatal(err)
	}
	ownerPath, err := query.NewPath("owner")
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "upload reused slug access", Storage: backend, StorageNamespace: "upload-reused-slug-access-test",
		Collections: []ridu.Collection{
			{
				Slug: "assets", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}, Private: true},
				Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if ctx.Actor == nil {
						return ridu.Deny(), nil
					}
					return ridu.Where(query.Equal(ownerPath, query.String(ctx.Actor.ID))), nil
				}},
				Fields: []field.Definition{field.Text("owner")},
			},
			{Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(context.Background(), "assets", store.Values{
		"filename": store.String("private.txt"), "mimeType": store.String("text/plain"), "filesize": store.Number(7),
		"objectKey": store.String(key), "url": store.String("/api/uploads/media/" + key), "owner": store.String("allowed-viewer"),
	}, ridu.ImportOptions{ID: "private-asset", Status: store.StatusPublished}, nil); err != nil {
		t.Fatal(err)
	}

	for name, actor := range map[string]*store.Document{
		"anonymous":   nil,
		"wrong actor": {ID: "wrong-viewer"},
	} {
		t.Run(name, func(t *testing.T) {
			reader, _, openErr := application.OpenUpload(context.Background(), "media", key, actor)
			if reader != nil {
				reader.Close()
				t.Fatal("denied fallback returned an object reader")
			}
			var operationError *ridu.OperationError
			if !errors.As(openErr, &operationError) || operationError.Code != "not_found" {
				t.Fatalf("denied fallback error = %v", openErr)
			}
		})
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", key, &store.Document{ID: "allowed-viewer"})
	if err != nil {
		t.Fatalf("access-visible renamed owner: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "private" {
		t.Fatalf("access-visible renamed body = %q, %v", body, err)
	}
}

func TestDeniedUploadDoesNotReadOrStoreBytes(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "denied upload", Storage: backend, StorageNamespace: "denied-upload-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt")},
		Access: ridu.CollectionAccess{Create: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Deny(), nil
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	reader := &observedReader{}
	if _, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "denied.txt", Reader: reader}); !operationCode(err, "access_denied") {
		t.Fatalf("denied upload error = %v", err)
	}
	if reader.reads != 0 {
		t.Fatalf("denied upload read source %d times", reader.reads)
	}
	if objects, err := listAllStorageObjects(context.Background(), backend, ""); err != nil || len(objects) != 0 {
		t.Fatalf("denied upload objects = %#v, %v", objects, err)
	}
}

func TestCommittedUploadObjectsSurviveAfterCommitFailure(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failAfterCommit := true
	application, err := ridu.New(ridu.Config{Name: "committed upload", Storage: backend, StorageNamespace: "committed-upload-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt")},
		Hooks: ridu.CollectionHooks{AfterCommit: []ridu.Hook{func(ridu.HookContext) error {
			if failAfterCommit {
				return errors.New("delivery failed")
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "committed.txt", Reader: strings.NewReader("committed")}); !operationCode(err, "hook_failed") {
		t.Fatalf("upload after-commit error = %v", err)
	}
	failAfterCommit = false
	page, err := application.Local().List(context.Background(), "media", ridu.ListOptions{Limit: 10})
	if err != nil || len(page.Documents) != 1 {
		t.Fatalf("committed upload documents = %#v, %v", page.Documents, err)
	}
	key, _ := page.Documents[0].Values["objectKey"].StringValue()
	if reader, _, err := backend.Open(context.Background(), key); err != nil {
		t.Fatal("committed upload object was rolled back", err)
	} else {
		reader.Close()
	}
}

func TestCommittedDuplicateAndRegenerationObjectsSurviveAfterCommitFailure(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failingOperation := ridu.Operation("")
	application, err := ridu.New(ridu.Config{Name: "committed image operations", Storage: backend, StorageNamespace: "committed-image-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{
			MaxFileSize: 4096, MimeTypes: []string{"image/png"},
			ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}},
		},
		Fields: []field.Definition{field.Text("alt")},
		Hooks: ridu.CollectionHooks{AfterCommit: []ridu.Hook{func(ctx ridu.HookContext) error {
			if ctx.Operation == failingOperation {
				return errors.New("delivery failed")
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	original, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "image.png", Reader: bytes.NewReader(encoded.Bytes()), Data: store.Values{"alt": store.String("Image")}})
	if err != nil {
		t.Fatal(err)
	}
	failingOperation = ridu.OperationDuplicate
	if _, err := application.Duplicate(context.Background(), "media", original.ID, store.Values{"alt": store.String("Copy")}, nil); !operationCode(err, "hook_failed") {
		t.Fatalf("duplicate after-commit error = %v", err)
	}
	failingOperation = ""
	page, err := application.Local().List(context.Background(), "media", ridu.ListOptions{Limit: 10})
	if err != nil || len(page.Documents) != 2 {
		t.Fatalf("documents after committed duplicate = %#v, %v", page.Documents, err)
	}
	for _, document := range page.Documents {
		for _, key := range uploadKeysForTest(document.Values) {
			if reader, _, err := backend.Open(context.Background(), key); err != nil {
				t.Fatalf("committed duplicate object %q was rolled back: %v", key, err)
			} else {
				reader.Close()
			}
		}
	}

	failingOperation = ridu.OperationUpdate
	if _, err := application.UpdateUploadImage(context.Background(), "media", original.ID, ridu.UpdateUploadImageInput{FocalX: 25, FocalY: 75, ExpectedRevision: original.Revision}); !operationCode(err, "hook_failed") {
		t.Fatalf("regeneration after-commit error = %v", err)
	}
	failingOperation = ""
	updated, err := application.Local().Find(context.Background(), "media", original.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if focalX, _ := updated.Values["focalX"].NumberValue(); focalX != 25 {
		t.Fatalf("committed focalX = %v", focalX)
	}
	for _, key := range uploadKeysForTest(updated.Values) {
		if reader, _, err := backend.Open(context.Background(), key); err != nil {
			t.Fatalf("committed regeneration object %q was rolled back: %v", key, err)
		} else {
			reader.Close()
		}
	}
}

func TestAmbiguousUploadCommitsRetainGeneratedObjects(t *testing.T) {
	t.Run("upload", func(t *testing.T) {
		storageBackend, err := localstorage.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		documentStore := &ambiguousCommitStore{Store: teststore.New()}
		application, err := ridu.New(ridu.Config{Name: "ambiguous upload", Storage: storageBackend, StorageNamespace: "ambiguous-upload", Collections: []ridu.Collection{{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				if ctx.Operation == ridu.OperationCreate {
					documentStore.arm()
				}
				return nil
			}}},
		}}}, documentStore)
		if err != nil {
			t.Fatal(err)
		}
		_, err = application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "ambiguous.txt", Reader: strings.NewReader("ambiguous")})
		requireAmbiguousCommitError(t, err)
		page, err := application.Local().List(context.Background(), "media", ridu.ListOptions{Limit: 10})
		if err != nil || len(page.Documents) != 1 {
			t.Fatalf("documents after ambiguous upload commit = %#v, %v", page.Documents, err)
		}
		key, _ := page.Documents[0].Values["objectKey"].StringValue()
		reader, _, err := storageBackend.Open(context.Background(), key)
		if err != nil {
			t.Fatalf("ambiguous upload commit deleted generated object %q: %v", key, err)
		}
		reader.Close()
	})

	t.Run("duplicate", func(t *testing.T) {
		storageBackend, err := localstorage.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		documentStore := &ambiguousCommitStore{Store: teststore.New()}
		application, err := ridu.New(ridu.Config{Name: "ambiguous duplicate", Storage: storageBackend, StorageNamespace: "ambiguous-duplicate", Collections: []ridu.Collection{{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				if ctx.Operation == ridu.OperationDuplicate {
					documentStore.arm()
				}
				return nil
			}}},
		}}}, documentStore)
		if err != nil {
			t.Fatal(err)
		}
		source, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "source.txt", Reader: strings.NewReader("source")})
		if err != nil {
			t.Fatal(err)
		}
		_, err = application.Duplicate(context.Background(), "media", source.ID, nil, nil)
		requireAmbiguousCommitError(t, err)
		page, err := application.Local().List(context.Background(), "media", ridu.ListOptions{Limit: 10})
		if err != nil || len(page.Documents) != 2 {
			t.Fatalf("documents after ambiguous duplicate commit = %#v, %v", page.Documents, err)
		}
		for _, document := range page.Documents {
			for _, key := range uploadKeysForTest(document.Values) {
				reader, _, openError := storageBackend.Open(context.Background(), key)
				if openError != nil {
					t.Fatalf("ambiguous duplicate commit deleted object %q: %v", key, openError)
				}
				reader.Close()
			}
		}
	})

	t.Run("image regeneration", func(t *testing.T) {
		storageBackend, err := localstorage.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		documentStore := &ambiguousCommitStore{Store: teststore.New()}
		application, err := ridu.New(ridu.Config{Name: "ambiguous regeneration", Storage: storageBackend, StorageNamespace: "ambiguous-regeneration", Collections: []ridu.Collection{{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				if ctx.Operation == ridu.OperationUpdate {
					documentStore.arm()
				}
				return nil
			}}},
		}}}, documentStore)
		if err != nil {
			t.Fatal(err)
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
			t.Fatal(err)
		}
		source, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "source.png", Reader: bytes.NewReader(encoded.Bytes())})
		if err != nil {
			t.Fatal(err)
		}
		oldKeys := imageSizeKeys(t, source.Values)
		_, err = application.UpdateUploadImage(context.Background(), "media", source.ID, ridu.UpdateUploadImageInput{FocalX: 25, FocalY: 75, ExpectedRevision: source.Revision})
		requireAmbiguousCommitError(t, err)
		updated, err := application.Local().Find(context.Background(), "media", source.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if focalX, _ := updated.Values["focalX"].NumberValue(); focalX != 25 {
			t.Fatalf("focal point after ambiguous regeneration commit = %v", focalX)
		}
		newKeys := imageSizeKeys(t, updated.Values)
		if len(newKeys) != 1 || len(oldKeys) != 1 || newKeys[0] == oldKeys[0] {
			t.Fatalf("variant keys after ambiguous regeneration old=%v new=%v", oldKeys, newKeys)
		}
		reader, _, err := storageBackend.Open(context.Background(), newKeys[0])
		if err != nil {
			t.Fatalf("ambiguous regeneration commit deleted generated variant %q: %v", newKeys[0], err)
		}
		reader.Close()
	})
}

func uploadKeysForTest(values store.Values) []string {
	keys := make([]string, 0, 2)
	if key, valid := values["objectKey"].StringValue(); valid {
		keys = append(keys, key)
	}
	return append(keys, imageSizeKeysFromValues(values)...)
}

func imageSizeKeysFromValues(values store.Values) []string {
	sizes, valid := values["sizes"].ObjectValue()
	if !valid {
		return nil
	}
	keys := make([]string, 0, len(sizes))
	for _, size := range sizes {
		metadata, _ := size.ObjectValue()
		if key, valid := metadata["objectKey"].StringValue(); valid {
			keys = append(keys, key)
		}
	}
	return keys
}

func TestUploadMetadataIsServerOwned(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "server-owned upload", Storage: backend, StorageNamespace: "server-owned-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt")},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	forged := store.Values{"alt": store.String("Forged"), "objectKey": store.String("outside/victim.txt"), "filename": store.String("shared.txt"), "sizes": store.Object(store.Values{
		"thumb": store.Object(store.Values{"objectKey": store.String("outside/victim.txt")}),
	})}
	if _, err := application.Local().Create(context.Background(), "media", forged, nil); !operationCode(err, "bad_operation") {
		t.Fatalf("local forged create error = %v", err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/media", strings.NewReader(`{"alt":"Forged","objectKey":"media/shared.txt"}`), "")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("JSON upload create = %d: %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()

	if err := backend.Put(context.Background(), "outside/victim.txt", strings.NewReader("victim"), 6, "text/plain"); err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "real.txt", Reader: strings.NewReader("real"), Data: forged})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(context.Background(), "media", document.ID, store.Values{"objectKey": store.String("media/other.txt")}, nil); !operationCode(err, "field_access_denied") {
		t.Fatalf("forged metadata update error = %v", err)
	}
	current, err := application.Local().Find(context.Background(), "media", document.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentKey, _ := current.Values["objectKey"].StringValue()
	originalKey, _ := document.Values["objectKey"].StringValue()
	if currentKey != originalKey {
		t.Fatalf("object key changed from %q to %q", originalKey, currentKey)
	}
	if currentKey == "outside/victim.txt" {
		t.Fatal("upload retained the caller-supplied object key")
	}
	duplicate, err := application.Duplicate(context.Background(), "media", document.ID, forged, nil)
	if err != nil {
		t.Fatal(err)
	}
	duplicateKey, _ := duplicate.Values["objectKey"].StringValue()
	if duplicateKey == "outside/victim.txt" || duplicateKey == originalKey {
		t.Fatalf("duplicate object key = %q", duplicateKey)
	}
	if _, err := application.Local().Delete(context.Background(), "media", document.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "media", duplicate.ID, nil); err != nil {
		t.Fatal(err)
	}
	if reader, _, err := backend.Open(context.Background(), "outside/victim.txt"); err != nil {
		t.Fatal("document cleanup deleted a caller-forged object key", err)
	} else {
		reader.Close()
	}
}

func TestUploadImportRejectsVariantMetadataAboveCleanupBoundBeforeLocking(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	documents := newGeneratedObjectLockStore()
	application, err := ridu.New(ridu.Config{Name: "bounded imported variants", Storage: backend, StorageNamespace: "bounded-import-variants", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, documents)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make(store.Values, store.MaxUploadReferenceCandidates)
	for index := range store.MaxUploadReferenceCandidates {
		name := fmt.Sprintf("variant-%d", index)
		key := fmt.Sprintf("ridu/bounded-import-variants/objects/%032x/sizes/%s.txt", index+1, name)
		sizes[name] = store.Object(store.Values{"objectKey": store.String(key)})
	}
	values := store.Values{
		"filename":  store.String("import.txt"),
		"mimeType":  store.String("text/plain"),
		"filesize":  store.Number(1),
		"url":       store.String("/api/uploads/ridu/bounded-import-variants/objects/00000000000000000000000000000001/import.txt"),
		"objectKey": store.String("ridu/bounded-import-variants/objects/00000000000000000000000000000001/import.txt"),
		"sizes":     store.Object(sizes),
	}
	if _, err := application.Local().Import(context.Background(), "media", values, ridu.ImportOptions{ID: "oversized-import", Status: store.StatusPublished}, nil); !operationCode(err, "validation") {
		t.Fatalf("oversized variant import error = %v", err)
	}
	select {
	case keys := <-documents.requested:
		t.Fatalf("oversized import requested object locks before admission: %d keys", len(keys))
	default:
	}
}

func TestDuplicateUploadCopiesObjectsAndDeletesIndependently(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "upload duplicate", Storage: backend, StorageNamespace: "duplicate-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	original, err := application.Upload(context.Background(), "media", ridu.UploadInput{
		Filename: "notes.txt", Reader: bytes.NewBufferString("independent bytes"), Data: store.Values{"alt": store.String("Notes")},
	})
	if err != nil {
		t.Fatal(err)
	}
	deliveryURL, _ := original.Values["url"].StringValue()
	delivery, err := handlerClient(application.Handler(ridu.HandlerOptions{})).Get("http://ridu.test" + deliveryURL)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.StatusCode != http.StatusOK {
		t.Fatalf("namespaced delivery = %d: %s", delivery.StatusCode, readBody(t, delivery))
	}
	delivery.Body.Close()
	if _, err := application.Local().Duplicate(context.Background(), "media", original.ID, nil, nil); err == nil {
		t.Fatal("raw local duplication unexpectedly shared upload storage")
	}
	duplicate, err := application.Duplicate(context.Background(), "media", original.ID, store.Values{"alt": store.String("Copy of Notes")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	originalKey, _ := original.Values["objectKey"].StringValue()
	duplicateKey, _ := duplicate.Values["objectKey"].StringValue()
	if originalKey == duplicateKey || duplicateKey == "" {
		t.Fatalf("duplicate object key = %q, original = %q", duplicateKey, originalKey)
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", duplicateKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := io.ReadAll(reader)
	reader.Close()
	if string(encoded) != "independent bytes" {
		t.Fatalf("duplicated upload body = %q", encoded)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/media/"+original.ID+"/duplicate", strings.NewReader(`{"alt":"REST copy"}`), "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("REST duplicate = %d: %s", response.StatusCode, readBody(t, response))
	}
	var envelope protocol.DocumentEnvelope[map[string]any]
	decodeResponse(t, response, &envelope)
	if envelope.Doc["objectKey"] == originalKey {
		t.Fatalf("REST duplicate reused original key: %#v", envelope.Doc)
	}
	if _, err := application.Local().Delete(context.Background(), "media", original.ID, nil); err != nil {
		t.Fatal(err)
	}
	reader, _, err = application.OpenUpload(context.Background(), "media", duplicateKey, nil)
	if err != nil {
		t.Fatal("duplicate became unreadable after deleting original", err)
	}
	reader.Close()
}

func TestUpdateUploadImageCommitsFocalMetadataAndRetainsVersionedVariants(t *testing.T) {
	storageRoot := t.TempDir()
	backend, err := localstorage.New(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "image editing", Storage: backend, StorageNamespace: "image-editing-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true, Versions: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "image.png", Reader: bytes.NewReader(encoded.Bytes()), Data: store.Values{"alt": store.String("Image")}})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys := imageSizeKeys(t, document.Values)
	updated, err := application.UpdateUploadImage(context.Background(), "media", document.ID, ridu.UpdateUploadImageInput{FocalX: 10, FocalY: 90, CropX: 10, CropY: 20, CropWidth: 70, CropHeight: 60, ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if focalX, _ := updated.Values["focalX"].NumberValue(); focalX != 10 {
		t.Fatalf("focalX = %v", focalX)
	}
	if focalY, _ := updated.Values["focalY"].NumberValue(); focalY != 90 {
		t.Fatalf("focalY = %v", focalY)
	}
	if cropWidth, _ := updated.Values["cropWidth"].NumberValue(); cropWidth != 70 {
		t.Fatalf("cropWidth = %v", cropWidth)
	}
	newKeys := imageSizeKeys(t, updated.Values)
	if len(oldKeys) != 1 || len(newKeys) != 1 || oldKeys[0] == newKeys[0] {
		t.Fatalf("variant keys old=%v new=%v", oldKeys, newKeys)
	}
	if reader, _, err := backend.Open(context.Background(), oldKeys[0]); err != nil {
		t.Fatal("versioned image variant was removed", err)
	} else {
		reader.Close()
	}
	if reader, _, err := backend.Open(context.Background(), newKeys[0]); err != nil {
		t.Fatal("new image variant was not stored", err)
	} else {
		reader.Close()
	}
	if reader, _, err := application.OpenUpload(context.Background(), "media", newKeys[0], nil); err != nil {
		t.Fatal("generated image variant was not deliverable", err)
	} else {
		reader.Close()
	}
	objectsBeforeConflict, err := listAllStorageObjects(context.Background(), backend, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.UpdateUploadImage(context.Background(), "media", document.ID, ridu.UpdateUploadImageInput{FocalX: 50, FocalY: 50, ExpectedRevision: document.Revision}); !operationCode(err, "conflict") {
		t.Fatalf("stale regeneration error = %v", err)
	}
	objectsAfterConflict, err := listAllStorageObjects(context.Background(), backend, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(objectsAfterConflict) != len(objectsBeforeConflict) {
		t.Fatalf("stale regeneration stored objects: before=%d after=%d", len(objectsBeforeConflict), len(objectsAfterConflict))
	}
	restored, err := application.Local().Restore(context.Background(), "media", document.ID, document.Revision, updated.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	restoredKeys := imageSizeKeys(t, restored.Values)
	if len(restoredKeys) != 1 || restoredKeys[0] != oldKeys[0] {
		t.Fatalf("restored variant keys = %v, want %v", restoredKeys, oldKeys)
	}
	if reader, _, err := application.OpenUpload(context.Background(), "media", restoredKeys[0], nil); err != nil {
		t.Fatal("restored version references a missing variant", err)
	} else {
		reader.Close()
	}
	originalKey, _ := document.Values["objectKey"].StringValue()
	if _, err := application.Local().Delete(context.Background(), "media", document.ID, nil); err != nil {
		t.Fatal(err)
	}
	for _, key := range append([]string{originalKey}, oldKeys...) {
		if _, _, err := backend.Open(context.Background(), key); err == nil {
			t.Fatalf("permanent deletion retained current object %q", key)
		}
	}
	for _, key := range newKeys {
		if _, _, err := backend.Open(context.Background(), key); err != nil {
			t.Fatalf("historical object %q was not deferred to reconciliation: %v", key, err)
		}
		old := time.Now().Add(-10 * time.Minute)
		if err := os.Chtimes(filepath.Join(storageRoot, filepath.FromSlash(key)), old, old); err != nil {
			t.Fatal(err)
		}
	}
	report, err := application.ReconcileUploads(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if report.Candidates != len(newKeys) || report.Deleted != 0 {
		t.Fatalf("reconciliation report = %#v", report)
	}
	cleaned, err := application.CleanupUploads(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.Deleted != len(newKeys) {
		t.Fatalf("cleanup result = %#v", cleaned)
	}
}

func TestUploadRestoreRejectsMissingHistoricalObjects(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "restore object validation", Storage: backend, StorageNamespace: "restore-object-validation", Collections: []ridu.Collection{{
		Slug: "media", Upload: true, Versions: true,
		VersionConfig: ridu.VersionConfig{MaxPerDocument: 10},
		UploadConfig:  ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	created, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "source.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys := imageSizeKeys(t, created.Values)
	updated, err := application.UpdateUploadImage(context.Background(), "media", created.ID, ridu.UpdateUploadImageInput{FocalX: 25, FocalY: 75, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	newKeys := imageSizeKeys(t, updated.Values)
	if len(oldKeys) != 1 || len(newKeys) != 1 || oldKeys[0] == newKeys[0] {
		t.Fatalf("variant keys old=%v new=%v", oldKeys, newKeys)
	}
	if err := backend.Delete(context.Background(), oldKeys[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Restore(context.Background(), "media", created.ID, created.Revision, updated.Revision, nil); !operationCode(err, "validation") {
		t.Fatalf("restore with missing historical object = %v", err)
	}
	current, err := application.Local().Find(context.Background(), "media", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentKeys := imageSizeKeys(t, current.Values)
	if len(currentKeys) != 1 || currentKeys[0] != newKeys[0] || current.Revision != updated.Revision {
		t.Fatalf("current document changed after refused restore = revision %d keys %v", current.Revision, currentKeys)
	}
	if reader, _, err := backend.Open(context.Background(), newKeys[0]); err != nil {
		t.Fatalf("current variant was lost after refused restore: %v", err)
	} else {
		reader.Close()
	}
}

func TestUploadRestoreLocksAndRevalidatesKeysAfterVersionPruningCleanup(t *testing.T) {
	storageRoot := t.TempDir()
	backend, err := localstorage.New(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	paused := make(chan struct{})
	resume := make(chan struct{})
	var pauseMu sync.Mutex
	pauseNextUpdate := false
	application, err := ridu.New(ridu.Config{Name: "restore cleanup race", Storage: backend, StorageNamespace: "restore-cleanup-race", Collections: []ridu.Collection{{
		Slug: "media", Upload: true, Versions: true,
		VersionConfig: ridu.VersionConfig{MaxPerDocument: 2},
		UploadConfig:  ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
		Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
			if ctx.Operation != ridu.OperationPublish {
				return nil
			}
			pauseMu.Lock()
			shouldPause := pauseNextUpdate
			pauseNextUpdate = false
			pauseMu.Unlock()
			if shouldPause {
				close(paused)
				select {
				case <-resume:
				case <-ctx.Context.Done():
					return ctx.Context.Err()
				}
			}
			return nil
		}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	created, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "source.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys := imageSizeKeys(t, created.Values)
	if len(oldKeys) != 1 {
		t.Fatalf("initial variant keys = %v", oldKeys)
	}
	if err := os.Chtimes(filepath.Join(storageRoot, filepath.FromSlash(oldKeys[0])), time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, err := application.UpdateUploadImage(context.Background(), "media", created.ID, ridu.UpdateUploadImageInput{FocalX: 20, FocalY: 80, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	pauseMu.Lock()
	pauseNextUpdate = true
	pauseMu.Unlock()
	type restoreResult struct {
		document store.Document
		err      error
	}
	restored := make(chan restoreResult, 1)
	go func() {
		document, restoreError := application.Local().Restore(context.Background(), "media", created.ID, created.Revision, 0, nil)
		restored <- restoreResult{document: document, err: restoreError}
	}()
	select {
	case <-paused:
	case <-time.After(time.Second):
		t.Fatal("restore did not pause between historical read and update admission")
	}
	third, err := application.UpdateUploadImage(context.Background(), "media", created.ID, ridu.UpdateUploadImageInput{FocalX: 40, FocalY: 60, ExpectedRevision: second.Revision})
	if err != nil {
		close(resume)
		t.Fatal(err)
	}
	cleanup, err := application.CleanupUploads(context.Background(), 5*time.Minute)
	if err != nil {
		close(resume)
		t.Fatal(err)
	}
	if cleanup.Candidates != 1 || cleanup.Deleted != 1 {
		close(resume)
		t.Fatalf("cleanup after pruning historical version = %#v", cleanup)
	}
	close(resume)
	var result restoreResult
	select {
	case result = <-restored:
	case <-time.After(time.Second):
		t.Fatal("restore did not resume after cleanup")
	}
	if !operationCode(result.err, "validation") {
		t.Fatalf("restore adopted an object deleted after version pruning: document=%#v error=%v", result.document, result.err)
	}
	current, err := application.Local().Find(context.Background(), "media", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentKeys := imageSizeKeys(t, current.Values)
	thirdKeys := imageSizeKeys(t, third.Values)
	if current.Revision != third.Revision || len(currentKeys) != 1 || len(thirdKeys) != 1 || currentKeys[0] != thirdKeys[0] {
		t.Fatalf("current document changed after raced restore = revision %d/%d keys %v/%v", current.Revision, third.Revision, currentKeys, thirdKeys)
	}
	if _, _, err := backend.Open(context.Background(), oldKeys[0]); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("pruned historical variant after cleanup = %v, want not found", err)
	}
}

func TestUploadStorageNamespaceIsExplicit(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ridu.New(ridu.Config{Name: "display name", Storage: backend, Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
	}}}, teststore.New())
	if err == nil || !strings.Contains(err.Error(), "StorageNamespace") {
		t.Fatalf("missing storage namespace error = %v", err)
	}
}

func TestUploadDocumentStoreRequiresCrossProcessObjectLocking(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ridu.New(ridu.Config{Name: "object locking", Storage: backend, StorageNamespace: "object-locking-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
	}}}, baseOnlyStore{inner: teststore.New()})
	if err == nil || !strings.Contains(err.Error(), "store.UploadObjectLocker") {
		t.Fatalf("missing upload object locker error = %v", err)
	}
}

func TestHardDeleteWithoutSnapshotStoreRetainsUploadObject(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	documents := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "snapshot cleanup required", Storage: backend, StorageNamespace: "snapshot-cleanup-required", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, uploadLockOnlyStore{Store: documents, locker: documents})
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "safe.txt", Reader: strings.NewReader("safe")})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := document.Values["objectKey"].StringValue()
	_, err = application.Local().Delete(context.Background(), "media", document.ID, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "hook_failed" || !operationError.Committed || !strings.Contains(err.Error(), "after commit") {
		t.Fatalf("hard delete without snapshot store = %#v, %v", operationError, err)
	}
	reader, _, err := backend.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("unsafe cleanup deleted object without a stable snapshot: %v", err)
	}
	reader.Close()
	if _, err := application.Local().Find(context.Background(), "media", document.ID, nil); !operationCode(err, "not_found") {
		t.Fatalf("hard-deleted row remained after committed cleanup failure: %v", err)
	}
}

func TestHardDeleteCleanupContinuesAfterPanickingAfterCommitHook(t *testing.T) {
	const panicSecret = "upload-after-commit-panic-secret"
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	laterRan := false
	application, err := ridu.New(ridu.Config{Name: "panic-safe upload cleanup", Storage: backend, StorageNamespace: "panic-safe-upload-cleanup", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Hooks: ridu.CollectionHooks{AfterCommit: []ridu.Hook{
			func(ctx ridu.HookContext) error {
				if ctx.Operation == ridu.OperationDelete {
					panic(panicSecret)
				}
				return nil
			},
			func(ctx ridu.HookContext) error {
				if ctx.Operation == ridu.OperationDelete {
					laterRan = true
				}
				return nil
			},
		}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "safe.txt", Reader: strings.NewReader("safe")})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := document.Values["objectKey"].StringValue()
	_, err = application.Local().Delete(context.Background(), "media", document.ID, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "hook_failed" || !operationError.Committed {
		t.Fatalf("hard delete error = %#v, %v", operationError, err)
	}
	if strings.Contains(fmt.Sprintf("%v", operationError.Cause), panicSecret) {
		t.Fatal("hard-delete response exposed recovered panic value")
	}
	if !laterRan {
		t.Fatal("later after-commit hook did not run after recovered panic")
	}
	if _, _, err := backend.Open(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("framework cleanup did not remove the committed orphan: %v", err)
	}
}

func TestBulkUploadHardDeleteUsesOnlyBoundedReferenceLookups(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	documents := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "bounded hard-delete references", Storage: backend, StorageNamespace: "bounded-hard-delete", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, documents)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, store.MaxUploadReferenceCandidates+1)
	for index := range ids {
		document, uploadError := application.Upload(context.Background(), "media", ridu.UploadInput{
			Filename: fmt.Sprintf("asset-%d.txt", index), Reader: strings.NewReader("asset"),
		})
		if uploadError != nil {
			t.Fatal(uploadError)
		}
		ids[index] = document.ID
	}
	before := documents.Events()
	if _, err := application.Local().BulkDelete(context.Background(), "media", ids, nil); err != nil {
		t.Fatal(err)
	}
	after := documents.Events()
	if got := countEvent(after, "upload-object-references") - countEvent(before, "upload-object-references"); got != 2 {
		t.Fatalf("targeted reference lookups = %d, want 2 bounded batches", got)
	}
	if got := countEvent(after, "list") - countEvent(before, "list"); got != 0 {
		t.Fatalf("hard delete performed %d full document scans", got)
	}
}

func TestUploadCleanupCoalescesCandidateReferenceRechecks(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := staleListedObjectsBackend{Backend: localBackend}
	documents := teststore.New()
	application, err := ridu.New(ridu.Config{Name: "coalesced cleanup references", Storage: backend, StorageNamespace: "coalesced-cleanup", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, documents)
	if err != nil {
		t.Fatal(err)
	}
	const objectCount = store.MaxUploadReferenceCandidates + 1
	for index := 0; index < objectCount; index++ {
		key := fmt.Sprintf("ridu/coalesced-cleanup/objects/%032x/orphan.txt", index+1)
		if err := localBackend.Put(context.Background(), key, strings.NewReader("x"), 1, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	before := documents.Events()
	result, err := application.CleanupUploads(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != objectCount || result.Candidates != objectCount || result.Deleted != objectCount {
		t.Fatalf("cleanup result = %#v", result)
	}
	after := documents.Events()
	// Two candidate-enumeration batches plus two locked destructive rechecks.
	if got := countEvent(after, "upload-object-references") - countEvent(before, "upload-object-references"); got != 4 {
		t.Fatalf("targeted reference lookups = %d, want 4", got)
	}
	if got := countEvent(after, "list") - countEvent(before, "list"); got != 0 {
		t.Fatalf("cleanup performed %d full document scans", got)
	}
}

func TestUnversionedImageRegenerationDefersSupersededVariantsToReconciliation(t *testing.T) {
	storageRoot := t.TempDir()
	backend, err := localstorage.New(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "unversioned image", Storage: backend, StorageNamespace: "unversioned-image-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "image.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys := imageSizeKeys(t, document.Values)
	if _, err := application.UpdateUploadImage(context.Background(), "media", document.ID, ridu.UpdateUploadImageInput{FocalX: 25, FocalY: 50, ExpectedRevision: document.Revision}); err != nil {
		t.Fatal(err)
	}
	for _, key := range oldKeys {
		reader, _, err := backend.Open(context.Background(), key)
		if err != nil {
			t.Fatalf("superseded variant %q was deleted before reconciliation: %v", key, err)
		}
		reader.Close()
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(storageRoot, filepath.FromSlash(key)), old, old); err != nil {
			t.Fatal(err)
		}
	}
	cleaned, err := application.CleanupUploads(context.Background(), 5*time.Minute)
	if err != nil || cleaned.Candidates != len(oldKeys) || cleaned.Deleted != len(oldKeys) {
		t.Fatalf("superseded variant cleanup = %#v, %v", cleaned, err)
	}
	for _, key := range oldKeys {
		if _, _, err := backend.Open(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("reconciliation retained superseded variant %q: %v", key, err)
		}
	}
}

func TestUnversionedImageRegenerationUsesOneUploadLockSession(t *testing.T) {
	storageRoot := t.TempDir()
	backend, err := localstorage.New(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	documentStore := &singleSessionObjectLockStore{Store: teststore.New()}
	application, err := ridu.New(ridu.Config{Name: "single upload lock session", Storage: backend, StorageNamespace: "single-lock-session", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
	}}}, documentStore)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "image.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys := imageSizeKeys(t, document.Values)
	if _, err := application.UpdateUploadImage(context.Background(), "media", document.ID, ridu.UpdateUploadImageInput{FocalX: 25, FocalY: 50, ExpectedRevision: document.Revision}); err != nil {
		t.Fatalf("regeneration exhausted the single upload-lock session: %v", err)
	}
	for _, key := range oldKeys {
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(storageRoot, filepath.FromSlash(key)), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := application.CleanupUploads(context.Background(), 5*time.Minute); err != nil {
		t.Fatalf("single-session reconciliation failed after regeneration: %v", err)
	}
	for _, key := range oldKeys {
		if _, _, err := backend.Open(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("single-session reconciliation retained superseded variant %q: %v", key, err)
		}
	}
}

func TestUploadReconciliationFailsClosedWithoutObjectAge(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := missingModifiedAtBackend{Backend: localBackend}
	application, err := ridu.New(ridu.Config{Name: "missing object age", Storage: backend, StorageNamespace: "missing-age-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "live.txt", Reader: strings.NewReader("live")}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.CleanupUploads(context.Background(), 5*time.Minute); !operationCode(err, "storage_failed") {
		t.Fatalf("missing modification time cleanup error = %v", err)
	}
}

func TestUploadReconciliationIgnoresNonCanonicalAndSupersetListKeys(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &adversarialListBackend{
		Backend:      localBackend,
		maliciousKey: "ridu/reconcile-test/objects/0123456789abcdef0123456789abcdef/../../other/victim",
	}
	application, err := ridu.New(ridu.Config{Name: "reconcile ownership", Storage: backend, StorageNamespace: "reconcile-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.CleanupUploads(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 0 || result.Candidates != 0 || len(backend.deleted) != 0 {
		t.Fatalf("cleanup escaped ownership: result=%#v deleted=%v", result, backend.deleted)
	}
}

func TestFailedUploadRollbackSurfacesStorageFailureAndReportsOrphans(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := failingDeleteBackend{Backend: localBackend}
	application, err := ridu.New(ridu.Config{Name: "rollback failure", Storage: backend, StorageNamespace: "rollback-failure-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Upload(context.Background(), "media", ridu.UploadInput{
		Filename: "local.txt", Reader: strings.NewReader("orphaned"),
	}); !operationCode(err, "storage_failed") {
		t.Fatalf("local rollback failure = %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "rest.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "orphaned over REST"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("data", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://ridu.test/api/collections/media", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := handlerClient(application.Handler(ridu.HandlerOptions{})).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("REST rollback failure = %d: %s", response.StatusCode, readBody(t, response))
	}
	var envelope protocol.ErrorEnvelope
	decodeResponse(t, response, &envelope)
	if envelope.Error.Code != protocol.ErrorInternal {
		t.Fatalf("REST rollback error = %#v", envelope.Error)
	}
	source, err := application.Upload(context.Background(), "media", ridu.UploadInput{
		Filename: "source.txt", Reader: strings.NewReader("source"),
		Data: store.Values{"alt": store.String("Source")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Duplicate(
		context.Background(),
		"media",
		source.ID,
		store.Values{"alt": store.String("")},
		nil,
	); !operationCode(err, "storage_failed") {
		t.Fatalf("local duplicate rollback failure = %v", err)
	}
	response = requestJSON(
		t,
		handlerClient(application.Handler(ridu.HandlerOptions{})),
		http.MethodPost,
		"http://ridu.test/api/collections/media/"+source.ID+"/duplicate",
		strings.NewReader(`{"alt":""}`),
		"",
	)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("REST duplicate rollback failure = %d: %s", response.StatusCode, readBody(t, response))
	}
	decodeResponse(t, response, &envelope)
	if envelope.Error.Code != protocol.ErrorInternal {
		t.Fatalf("REST duplicate rollback error = %#v", envelope.Error)
	}
	report, err := application.ReconcileUploads(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 5 || report.Candidates != 4 || report.Deleted != 0 {
		t.Fatalf("rollback orphan report = %#v", report)
	}
}

func imageSizeKeys(t *testing.T, values store.Values) []string {
	t.Helper()
	sizes, valid := values["sizes"].ObjectValue()
	if !valid {
		t.Fatal("sizes metadata is missing")
	}
	keys := make([]string, 0, len(sizes))
	for _, size := range sizes {
		metadata, _ := size.ObjectValue()
		key, _ := metadata["objectKey"].StringValue()
		keys = append(keys, key)
	}
	return keys
}

func TestRESTUpdatesUploadImageFocalPoint(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "image REST", Storage: backend, StorageNamespace: "image-rest-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "image.png", Reader: bytes.NewReader(encoded.Bytes()), Data: store.Values{"alt": store.String("Image")}})
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/media/"+document.ID+"/image", strings.NewReader(`{"focalX":25,"focalY":75,"cropX":10,"cropY":15,"cropWidth":80,"cropHeight":70}`), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("image update = %d: %s", response.StatusCode, readBody(t, response))
	}
	var body struct {
		Doc map[string]any `json:"doc"`
	}
	decodeResponse(t, response, &body)
	if body.Doc["focalX"] != float64(25) || body.Doc["focalY"] != float64(75) || body.Doc["cropWidth"] != float64(80) {
		t.Fatalf("image update document = %#v", body.Doc)
	}
	invalid := requestJSON(t, client, http.MethodPatch, "http://ridu.test/api/collections/media/"+document.ID+"/image", strings.NewReader(`{"focalX":101,"focalY":50}`), "")
	if invalid.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid focal status = %d", invalid.StatusCode)
	}
}

func TestRESTRemoteUploadRejectsPrivateNetworkTargets(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "remote upload REST", Storage: backend, StorageNamespace: "remote-upload-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(application.Handler(ridu.HandlerOptions{}))
	response := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/media/remote-upload", strings.NewReader(`{"url":"http://127.0.0.1/private.png","data":{"alt":"Private"}}`), "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("private remote upload = %d: %s", response.StatusCode, readBody(t, response))
	}
}

func TestUploadReconciliationReportsBeforeExplicitCleanup(t *testing.T) {
	root := t.TempDir()
	backend, err := localstorage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "reconciliation", Storage: backend, StorageNamespace: "reconciliation-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt")},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "live.txt", Reader: strings.NewReader("live")})
	if err != nil {
		t.Fatal(err)
	}
	liveKey, _ := document.Values["objectKey"].StringValue()
	segments := strings.Split(liveKey, "/")
	if len(segments) < 3 {
		t.Fatalf("namespaced object key = %q", liveKey)
	}
	orphanKey := strings.Join(segments[:2], "/") + "/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/orphan.txt"
	if err := backend.Put(context.Background(), orphanKey, strings.NewReader("orphan"), 6, "text/plain"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(orphanKey)), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := application.ReconcileUploads(context.Background(), time.Minute); !operationCode(err, "bad_request") {
		t.Fatalf("unsafe reconciliation window error = %v", err)
	}
	report, err := application.ReconcileUploads(context.Background(), 5*time.Minute)
	if err != nil || report.Candidates != 1 || report.Deleted != 0 {
		t.Fatalf("reconciliation report = %#v, %v", report, err)
	}
	if reader, _, err := backend.Open(context.Background(), orphanKey); err != nil {
		t.Fatal("report-only reconciliation deleted candidate", err)
	} else {
		reader.Close()
	}
	cleanup, err := application.CleanupUploads(context.Background(), 5*time.Minute)
	if err != nil || cleanup.Candidates != 1 || cleanup.Deleted != 1 {
		t.Fatalf("reconciliation cleanup = %#v, %v", cleanup, err)
	}
	if _, _, err := backend.Open(context.Background(), orphanKey); err == nil {
		t.Fatal("explicit cleanup retained orphan")
	}
	if reader, _, err := backend.Open(context.Background(), liveKey); err != nil {
		t.Fatal("explicit cleanup deleted referenced object", err)
	} else {
		reader.Close()
	}
}

func TestUploadCleanupRechecksReferencesAdmittedAfterCandidateEnumeration(t *testing.T) {
	root := t.TempDir()
	localBackend, err := localstorage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	backend := &blockingListBackend{Backend: localBackend, listed: make(chan struct{}), release: make(chan struct{})}
	application, err := ridu.New(ridu.Config{Name: "reconciliation race", Storage: backend, StorageNamespace: "reconciliation-race", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	key := "ridu/reconciliation-race/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/imported.txt"
	if err := localBackend.Put(context.Background(), key, strings.NewReader("imported"), 8, "text/plain"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(key)), old, old); err != nil {
		t.Fatal(err)
	}
	type cleanupResult struct {
		result ridu.ReconcileResult
		err    error
	}
	completed := make(chan cleanupResult, 1)
	go func() {
		result, cleanupError := application.CleanupUploads(context.Background(), 5*time.Minute)
		completed <- cleanupResult{result: result, err: cleanupError}
	}()
	select {
	case <-backend.listed:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not enumerate storage")
	}
	values := store.Values{
		"filename": store.String("imported.txt"), "mimeType": store.String("text/plain"),
		"filesize": store.Number(8), "url": store.String("/api/uploads/" + key), "objectKey": store.String(key),
	}
	document, err := application.Local().Import(context.Background(), "media", values, ridu.ImportOptions{ID: "imported-media", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	close(backend.release)
	result := <-completed
	if result.err != nil || result.result.Candidates != 1 || result.result.Deleted != 0 {
		t.Fatalf("cleanup race result = %#v, %v", result.result, result.err)
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", key, nil)
	if err != nil {
		t.Fatalf("freshly referenced candidate was deleted: %v", err)
	}
	reader.Close()
	if document.ID != "imported-media" {
		t.Fatalf("imported document = %#v", document)
	}
}

func TestGeneratedUploadPreparationBlocksCleanupUntilDocumentCommit(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageBackend := staleListedObjectsBackend{Backend: localBackend}
	documentStore := newGeneratedObjectLockStore()
	entered := make(chan struct{}, 1)
	proceed := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(proceed) }) }
	defer release()
	application, err := ridu.New(ridu.Config{Name: "generated upload race", Storage: storageBackend, StorageNamespace: "generated-upload-race", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Hooks:        ridu.CollectionHooks{BeforeChange: []ridu.Hook{blockingChangeHook(ridu.OperationCreate, entered, proceed)}},
	}}}, documentStore)
	if err != nil {
		t.Fatal(err)
	}
	type uploadResult struct {
		document store.Document
		err      error
	}
	uploadDone := make(chan uploadResult, 1)
	go func() {
		document, uploadError := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "pending.txt", Reader: strings.NewReader("pending")})
		uploadDone <- uploadResult{document: document, err: uploadError}
	}()
	preparationLock := awaitGeneratedObjectLock(t, documentStore, "upload preparation")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("upload did not reach the pre-commit hook")
	}

	type cleanupResult struct {
		result ridu.ReconcileResult
		err    error
	}
	cleanupDone := make(chan cleanupResult, 1)
	go func() {
		result, cleanupError := application.CleanupUploads(context.Background(), 5*time.Minute)
		cleanupDone <- cleanupResult{result: result, err: cleanupError}
	}()
	cleanupLock := awaitGeneratedObjectLock(t, documentStore, "upload cleanup")
	requireSameSingleObjectLock(t, preparationLock, cleanupLock)
	select {
	case result := <-cleanupDone:
		t.Fatalf("cleanup passed an in-flight upload preparation: %#v, %v", result.result, result.err)
	default:
	}

	release()
	var uploaded uploadResult
	select {
	case uploaded = <-uploadDone:
	case <-time.After(time.Second):
		t.Fatal("upload did not finish after the commit hook resumed")
	}
	if uploaded.err != nil {
		t.Fatal(uploaded.err)
	}
	var cleaned cleanupResult
	select {
	case cleaned = <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not resume after the upload committed")
	}
	if cleaned.err != nil || cleaned.result.Candidates != 1 || cleaned.result.Deleted != 0 {
		t.Fatalf("cleanup result = %#v, %v", cleaned.result, cleaned.err)
	}
	key, _ := uploaded.document.Values["objectKey"].StringValue()
	reader, _, err := application.OpenUpload(context.Background(), "media", key, nil)
	if err != nil {
		t.Fatalf("committed upload object was deleted: %v", err)
	}
	reader.Close()
}

func TestPartialUploadPreparationKeepsImportLockedUntilRollbackDeletesObjects(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageBackend := &partialVariantFailureBackend{
		Backend: localBackend, originalPut: make(chan string, 1), deleting: make(chan string, 1), allowDelete: make(chan struct{}),
	}
	var allowDeleteOnce sync.Once
	allowDelete := func() { allowDeleteOnce.Do(func() { close(storageBackend.allowDelete) }) }
	defer allowDelete()
	documentStore := newGeneratedObjectLockStore()
	application, err := ridu.New(ridu.Config{Name: "partial generated upload", Storage: storageBackend, StorageNamespace: "partial-generated-upload", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
	}}}, documentStore)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	uploadDone := make(chan error, 1)
	go func() {
		_, uploadError := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "partial.png", Reader: bytes.NewReader(encoded.Bytes())})
		uploadDone <- uploadError
	}()
	var originalKey string
	select {
	case originalKey = <-storageBackend.originalPut:
	case <-time.After(time.Second):
		t.Fatal("upload did not store its original object")
	}
	select {
	case deletingKey := <-storageBackend.deleting:
		if deletingKey != originalKey {
			t.Fatalf("rollback first deleted %q, want original %q", deletingKey, originalKey)
		}
	case <-time.After(time.Second):
		t.Fatal("partial upload did not begin rollback cleanup")
	}
	preparationKeys := awaitGeneratedObjectLock(t, documentStore, "partial upload preparation")
	select {
	case <-documentStore.acquired:
	case <-time.After(time.Second):
		t.Fatal("partial upload never acquired its generated-object lock")
	}
	if len(preparationKeys) != 2 || preparationKeys[0] != originalKey {
		t.Fatalf("partial upload lock keys = %v, original = %q", preparationKeys, originalKey)
	}

	values := store.Values{
		"filename": store.String("partial.png"), "mimeType": store.String("image/png"),
		"filesize": store.Number(float64(encoded.Len())), "url": store.String("/api/uploads/media/" + originalKey), "objectKey": store.String(originalKey),
	}
	importDone := make(chan error, 1)
	go func() {
		_, importError := application.Local().Import(context.Background(), "media", values, ridu.ImportOptions{ID: "racing-import", Status: store.StatusPublished}, nil)
		importDone <- importError
	}()
	importKeys := awaitGeneratedObjectLock(t, documentStore, "concurrent import")
	if len(importKeys) != 1 || importKeys[0] != originalKey {
		t.Fatalf("import lock keys = %v, original = %q", importKeys, originalKey)
	}
	select {
	case keys := <-documentStore.acquired:
		t.Fatalf("import acquired %v before partial-upload rollback deletion finished", keys)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case err := <-importDone:
		t.Fatalf("import bypassed partial-upload rollback: %v", err)
	default:
	}

	allowDelete()
	select {
	case keys := <-documentStore.acquired:
		if len(keys) != 1 || keys[0] != originalKey {
			t.Fatalf("import acquired unexpected keys %v", keys)
		}
	case <-time.After(time.Second):
		t.Fatal("import did not acquire the object lock after rollback cleanup")
	}
	if err := <-uploadDone; !operationCode(err, "storage_failed") {
		t.Fatalf("partial upload error = %v", err)
	}
	if err := <-importDone; !operationCode(err, "validation") {
		t.Fatalf("import after rollback error = %v", err)
	}
	if _, err := application.Local().Find(context.Background(), "media", "racing-import", nil); !operationCode(err, "not_found") {
		t.Fatalf("failed import committed a dangling document: %v", err)
	}
	if _, _, err := localBackend.Open(context.Background(), originalKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("partial upload original remained after rollback: %v", err)
	}
}

func TestNestedUploadPreparationRollsBackWithOuterTransaction(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageBackend := &recordingPutBackend{Backend: localBackend}
	outerFailure := errors.New("reject outer document")
	var application *ridu.App
	var nested store.Document
	application, err = ridu.New(ridu.Config{Name: "nested upload rollback", Storage: storageBackend, StorageNamespace: "nested-upload-rollback", Collections: []ridu.Collection{
		{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		},
		{
			Slug: "posts",
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				var uploadError error
				nested, uploadError = application.Upload(ctx.Context, "media", ridu.UploadInput{Filename: "nested.txt", Reader: strings.NewReader("nested")})
				if uploadError != nil {
					return uploadError
				}
				return outerFailure
			}}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", nil, nil); !operationCode(err, "hook_failed") || !errors.Is(err, outerFailure) {
		t.Fatalf("outer transaction error = %v", err)
	}
	keys := storageBackend.storedKeys()
	if len(keys) != 1 || nested.ID == "" {
		t.Fatalf("nested upload = %#v, stored keys = %v", nested, keys)
	}
	if _, err := application.Local().Find(context.Background(), "media", nested.ID, nil); !operationCode(err, "not_found") {
		t.Fatalf("nested upload document survived outer rollback: %v", err)
	}
	if _, _, err := localBackend.Open(context.Background(), keys[0]); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("nested upload object survived outer rollback: %v", err)
	}
}

func TestSwallowedNestedUploadFailurePoisonsOuterTransactionAndCleansPreparation(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageBackend := &recordingPutBackend{Backend: localBackend}
	var application *ridu.App
	var nestedError error
	application, err = ridu.New(ridu.Config{Name: "nested upload failure", Storage: storageBackend, StorageNamespace: "nested-upload-failure", Collections: []ridu.Collection{
		{
			Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
			Fields:       []field.Definition{field.Text("alt", field.Required())},
		},
		{
			Slug: "posts",
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				_, nestedError = application.Upload(ctx.Context, "media", ridu.UploadInput{Filename: "invalid.txt", Reader: strings.NewReader("invalid")})
				return nil
			}}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", nil, nil); !operationCode(err, "validation") {
		t.Fatalf("outer transaction did not preserve swallowed nested failure: %v", err)
	}
	if !operationCode(nestedError, "validation") {
		t.Fatalf("nested upload error = %v", nestedError)
	}
	keys := storageBackend.storedKeys()
	if len(keys) != 1 {
		t.Fatalf("nested failed upload stored keys = %v", keys)
	}
	if _, _, err := localBackend.Open(context.Background(), keys[0]); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("failed nested upload object survived outer rollback: %v", err)
	}
	page, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{Limit: 10})
	if err != nil || page.Total != 0 {
		t.Fatalf("outer document committed after swallowed nested failure: %#v, %v", page, err)
	}
}

func TestGeneratedDuplicatePreparationBlocksCleanupUntilDocumentCommit(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageBackend := staleListedObjectsBackend{Backend: localBackend}
	documentStore := newGeneratedObjectLockStore()
	entered := make(chan struct{}, 1)
	proceed := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(proceed) }) }
	defer release()
	application, err := ridu.New(ridu.Config{Name: "generated duplicate race", Storage: storageBackend, StorageNamespace: "generated-duplicate-race", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
		Hooks:        ridu.CollectionHooks{BeforeChange: []ridu.Hook{blockingChangeHook(ridu.OperationDuplicate, entered, proceed)}},
	}}}, documentStore)
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "source.txt", Reader: strings.NewReader("source")})
	if err != nil {
		t.Fatal(err)
	}
	awaitGeneratedObjectLock(t, documentStore, "source upload")

	type duplicateResult struct {
		document store.Document
		err      error
	}
	duplicateDone := make(chan duplicateResult, 1)
	go func() {
		document, duplicateError := application.Duplicate(context.Background(), "media", source.ID, nil, nil)
		duplicateDone <- duplicateResult{document: document, err: duplicateError}
	}()
	preparationLock := awaitGeneratedObjectLock(t, documentStore, "duplicate preparation")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("duplicate did not reach the pre-commit hook")
	}

	type cleanupResult struct {
		result ridu.ReconcileResult
		err    error
	}
	cleanupDone := make(chan cleanupResult, 1)
	go func() {
		result, cleanupError := application.CleanupUploads(context.Background(), 5*time.Minute)
		cleanupDone <- cleanupResult{result: result, err: cleanupError}
	}()
	cleanupLock := awaitGeneratedObjectLock(t, documentStore, "duplicate cleanup")
	requireSameSingleObjectLock(t, preparationLock, cleanupLock)
	select {
	case result := <-cleanupDone:
		t.Fatalf("cleanup passed an in-flight duplicate preparation: %#v, %v", result.result, result.err)
	default:
	}

	release()
	var duplicated duplicateResult
	select {
	case duplicated = <-duplicateDone:
	case <-time.After(time.Second):
		t.Fatal("duplicate did not finish after the commit hook resumed")
	}
	if duplicated.err != nil {
		t.Fatal(duplicated.err)
	}
	var cleaned cleanupResult
	select {
	case cleaned = <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not resume after the duplicate committed")
	}
	if cleaned.err != nil || cleaned.result.Candidates != 1 || cleaned.result.Deleted != 0 {
		t.Fatalf("cleanup result = %#v, %v", cleaned.result, cleaned.err)
	}
	key, _ := duplicated.document.Values["objectKey"].StringValue()
	reader, _, err := application.OpenUpload(context.Background(), "media", key, nil)
	if err != nil {
		t.Fatalf("committed duplicate object was deleted: %v", err)
	}
	reader.Close()
}

func TestGeneratedImageRegenerationBlocksCleanupUntilUnversionedCommit(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageBackend := staleListedObjectsBackend{Backend: localBackend}
	documentStore := newGeneratedObjectLockStore()
	entered := make(chan struct{}, 1)
	proceed := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(proceed) }) }
	defer release()
	application, err := ridu.New(ridu.Config{Name: "generated image race", Storage: storageBackend, StorageNamespace: "generated-image-race", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
		Hooks:        ridu.CollectionHooks{BeforeChange: []ridu.Hook{blockingChangeHook(ridu.OperationUpdate, entered, proceed)}},
	}}}, documentStore)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	source, err := application.Upload(context.Background(), "media", ridu.UploadInput{Filename: "source.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	awaitGeneratedObjectLock(t, documentStore, "source image upload")

	type updateResult struct {
		document store.Document
		err      error
	}
	updateDone := make(chan updateResult, 1)
	go func() {
		document, updateError := application.UpdateUploadImage(context.Background(), "media", source.ID, ridu.UpdateUploadImageInput{FocalX: 25, FocalY: 75, ExpectedRevision: source.Revision})
		updateDone <- updateResult{document: document, err: updateError}
	}()
	preparationLock := awaitGeneratedObjectLock(t, documentStore, "image regeneration")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("image regeneration did not reach the pre-commit hook")
	}

	type cleanupResult struct {
		result ridu.ReconcileResult
		err    error
	}
	cleanupDone := make(chan cleanupResult, 1)
	go func() {
		result, cleanupError := application.CleanupUploads(context.Background(), 5*time.Minute)
		cleanupDone <- cleanupResult{result: result, err: cleanupError}
	}()
	cleanupLock := awaitGeneratedObjectLock(t, documentStore, "image regeneration cleanup")
	requireSameSingleObjectLock(t, preparationLock, cleanupLock)
	select {
	case result := <-cleanupDone:
		t.Fatalf("cleanup passed in-flight image regeneration: %#v, %v", result.result, result.err)
	default:
	}

	release()
	var updated updateResult
	select {
	case updated = <-updateDone:
	case <-time.After(time.Second):
		t.Fatal("image regeneration did not finish after the commit hook resumed")
	}
	if updated.err != nil {
		t.Fatal(updated.err)
	}
	var cleaned cleanupResult
	select {
	case cleaned = <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not resume after image regeneration committed")
	}
	if cleaned.err != nil || cleaned.result.Candidates != 1 || cleaned.result.Deleted != 0 {
		t.Fatalf("cleanup result = %#v, %v", cleaned.result, cleaned.err)
	}
	newKeys := imageSizeKeys(t, updated.document.Values)
	if len(newKeys) != 1 || newKeys[0] != preparationLock[0] {
		t.Fatalf("regenerated keys = %v, preparation lock = %v", newKeys, preparationLock)
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", newKeys[0], nil)
	if err != nil {
		t.Fatalf("committed regenerated variant was deleted: %v", err)
	}
	reader.Close()
}

func TestDeletingOneImportedUploadRetainsSharedObjectUntilLastReference(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "shared imported upload", Storage: backend, StorageNamespace: "shared-import", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	key := "ridu/shared-import/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/shared.txt"
	if err := backend.Put(context.Background(), key, strings.NewReader("shared"), 6, "text/plain"); err != nil {
		t.Fatal(err)
	}
	values := store.Values{
		"filename": store.String("shared.txt"), "mimeType": store.String("text/plain"),
		"filesize": store.Number(6), "url": store.String("/api/uploads/" + key), "objectKey": store.String(key),
	}
	for _, id := range []string{"media-a", "media-b"} {
		if _, err := application.Local().Import(context.Background(), "media", values, ridu.ImportOptions{ID: id, Status: store.StatusPublished}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := application.Local().Delete(context.Background(), "media", "media-a", nil); err != nil {
		t.Fatal(err)
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", key, nil)
	if err != nil {
		t.Fatalf("shared object was deleted with its first owner: %v", err)
	}
	reader.Close()
	if _, err := application.Local().Delete(context.Background(), "media", "media-b", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.Open(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("last reference cleanup error = %v", err)
	}
}

func TestUploadObjectDeletionSerializesWithConcurrentImportAdmission(t *testing.T) {
	root := t.TempDir()
	localBackend, err := localstorage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	backend := &blockingDeleteBackend{Backend: localBackend, deleting: make(chan struct{}), release: make(chan struct{})}
	application, err := ridu.New(ridu.Config{Name: "serialized upload objects", Storage: backend, StorageNamespace: "serialized-import", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	key := "ridu/serialized-import/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/imported.txt"
	if err := localBackend.Put(context.Background(), key, strings.NewReader("imported"), 8, "text/plain"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(key)), old, old); err != nil {
		t.Fatal(err)
	}
	cleanupDone := make(chan error, 1)
	go func() {
		_, cleanupError := application.CleanupUploads(context.Background(), 5*time.Minute)
		cleanupDone <- cleanupError
	}()
	select {
	case <-backend.deleting:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not reach object deletion")
	}
	values := store.Values{
		"filename": store.String("imported.txt"), "mimeType": store.String("text/plain"),
		"filesize": store.Number(8), "url": store.String("/api/uploads/" + key), "objectKey": store.String(key),
	}
	importDone := make(chan error, 1)
	go func() {
		_, importError := application.Local().Import(context.Background(), "media", values, ridu.ImportOptions{ID: "late-import", Status: store.StatusPublished}, nil)
		importDone <- importError
	}()
	select {
	case err := <-importDone:
		t.Fatalf("import bypassed the destructive object lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(backend.release)
	if err := <-cleanupDone; err != nil {
		t.Fatal(err)
	}
	if err := <-importDone; !operationCode(err, "validation") {
		t.Fatalf("import adopted an object deleted ahead of it: %v", err)
	}
	if _, err := application.Local().Find(context.Background(), "media", "late-import", nil); !operationCode(err, "not_found") {
		t.Fatalf("failed import committed a dangling document: %v", err)
	}
}

func TestUnversionedImageRegenerationRetainsVariantSharedByImportedDocument(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "shared image variant", Storage: backend, StorageNamespace: "shared-image", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	shared := "ridu/shared-image/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/sizes/thumb.png"
	originals := []string{
		"ridu/shared-image/objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/a.png",
		"ridu/shared-image/objects/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/b.png",
	}
	for _, key := range append(append([]string(nil), originals...), shared) {
		if err := backend.Put(context.Background(), key, bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	for index, id := range []string{"media-a", "media-b"} {
		values := store.Values{
			"filename": store.String(filepath.Base(originals[index])), "mimeType": store.String("image/png"),
			"filesize": store.Number(float64(encoded.Len())), "url": store.String("/api/uploads/" + originals[index]), "objectKey": store.String(originals[index]),
			"width": store.Number(4), "height": store.Number(2),
			"sizes": store.Object(store.Values{"thumb": store.Object(store.Values{
				"objectKey": store.String(shared), "url": store.String("/api/uploads/" + shared),
				"width": store.Number(2), "height": store.Number(2), "mimeType": store.String("image/png"), "filesize": store.Number(float64(encoded.Len())),
			})}),
		}
		if _, err := application.Local().Import(context.Background(), "media", values, ridu.ImportOptions{ID: id, Status: store.StatusPublished}, nil); err != nil {
			t.Fatal(err)
		}
	}
	first, err := application.Local().Find(context.Background(), "media", "media-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.UpdateUploadImage(context.Background(), "media", first.ID, ridu.UpdateUploadImageInput{FocalX: 50, FocalY: 50, ExpectedRevision: first.Revision}); err != nil {
		t.Fatal(err)
	}
	reader, _, err := application.OpenUpload(context.Background(), "media", shared, nil)
	if err != nil {
		t.Fatalf("regeneration deleted another document's shared variant: %v", err)
	}
	reader.Close()
}

func TestMultipartUploadUsesCollectionSizeLimit(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{Name: "multipart limit", Storage: backend, StorageNamespace: "multipart-test", Collections: []ridu.Collection{{
		Slug: "media", Upload: true,
		UploadConfig: ridu.UploadConfig{MaxFileSize: 2 << 20, MimeTypes: []string{"text/plain"}},
		Fields:       []field.Definition{field.Text("alt", field.Required())},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "large.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(bytes.Repeat([]byte("a"), 1_200_000)); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("data", `{"alt":"Large text"}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://ridu.test/api/collections/media", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := handlerClient(application.Handler(ridu.HandlerOptions{})).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("1.2 MiB multipart upload = %d: %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
}

func TestUploadImageConfigurationEnforcesProcessingBudgets(t *testing.T) {
	for name, sizes := range map[string][]ridu.ImageSize{
		"variant count": func() []ridu.ImageSize {
			result := make([]ridu.ImageSize, 65)
			for index := range result {
				result[index] = ridu.ImageSize{Name: fmt.Sprintf("size-%d", index), Width: 10, Height: 10}
			}
			return result
		}(),
		"aggregate pixels": {
			{Name: "one", Width: 10_000, Height: 4_000},
			{Name: "two", Width: 10_000, Height: 4_000},
			{Name: "three", Width: 10_000, Height: 4_000},
		},
	} {
		t.Run(name, func(t *testing.T) {
			backend, err := localstorage.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			_, err = ridu.New(ridu.Config{Name: "invalid image budget", Storage: backend, StorageNamespace: "image-budget-test", Collections: []ridu.Collection{{
				Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{ImageSizes: sizes},
			}}}, teststore.New())
			if err == nil || !strings.Contains(err.Error(), "image") {
				t.Fatalf("invalid %s configuration error = %v", name, err)
			}
		})
	}
}
