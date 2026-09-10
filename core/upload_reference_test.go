package core_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestUploadReferencesRequireReadableTargetsAcrossShapesAndMutations(t *testing.T) {
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	visiblePath, err := query.NewPath("visible")
	if err != nil {
		t.Fatal(err)
	}
	restrictUploads := false
	application, err := ridu.New(ridu.Config{
		Name: "Upload reference admission", Storage: storageBackend, StorageNamespace: "upload-reference-admission",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
				Fields: field.Fields{field.Checkbox("visible").Required()},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					if !restrictUploads {
						return ridu.Allow(), nil
					}
					return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
				}},
			},
			{
				Slug: "entries", Versions: true,
				Fields: field.Fields{field.Upload("hero", "media"), field.Uploads("gallery", "media"), field.Upload("localizedHero", "media").Localized(), field.Group("meta", field.Fields{field.Upload("asset", "media")}), field.Array("sections", field.Fields{field.Upload("asset", "media")}), field.Blocks("content", field.Block{Slug: "image", Fields: field.Fields{field.Upload("asset", "media")}})},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
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
	restrictUploads = true

	tests := []struct {
		name    string
		path    string
		values  store.Values
		options []ridu.LocaleOptions
	}{
		{name: "missing singular", path: "hero", values: store.Values{"hero": store.String("missing")}},
		{name: "filtered has many", path: "gallery.1", values: store.Values{"gallery": store.List(store.String(visible.ID), store.String(hidden.ID))}},
		{name: "nested group", path: "meta.asset", values: store.Values{"meta": store.Object(store.Values{"asset": store.String(hidden.ID)})}},
		{name: "nested array", path: "sections.0.asset", values: store.Values{"sections": store.List(store.Object(store.Values{"asset": store.String(hidden.ID)}))}},
		{name: "nested block", path: "content.0.asset", values: store.Values{"content": store.List(store.Object(store.Values{"blockType": store.String("image"), "asset": store.String(hidden.ID)}))}},
		{name: "localized", path: "localizedHero.fr", values: store.Values{"localizedHero": store.String(hidden.ID)}, options: []ridu.LocaleOptions{{Locale: "fr"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.Local().Create(ctx, "entries", test.values, nil, test.options...); !uploadReferenceIssue(err, test.path) {
				t.Fatalf("upload reference error = %v", err)
			}
		})
	}

	accepted, err := application.Local().Create(ctx, "entries", store.Values{
		"hero":          store.String(visible.ID),
		"gallery":       store.List(store.String(visible.ID), store.String(visible.ID)),
		"meta":          store.Object(store.Values{"asset": store.String(visible.ID)}),
		"sections":      store.List(store.Object(store.Values{"asset": store.String(visible.ID)})),
		"content":       store.List(store.Object(store.Values{"blockType": store.String("image"), "asset": store.String(visible.ID)})),
		"localizedHero": store.String(visible.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(ctx, "entries", accepted.ID, store.Values{"hero": store.String(hidden.ID)}, accepted.Revision, nil); !uploadReferenceIssue(err, "hero") {
		t.Fatalf("filtered upload update error = %v", err)
	}
	if _, err := application.Local().PublishChanges(ctx, "entries", accepted.ID, store.Values{"localizedHero": store.String(hidden.ID)}, accepted.Revision, nil, ridu.LocaleOptions{Locale: "fr"}); !uploadReferenceIssue(err, "localizedHero.fr") {
		t.Fatalf("localized upload update error = %v", err)
	}

	// Duplicate carries every persisted locale, so a source that became
	// unreadable after authoring must fail at its concrete locale path.
	restrictUploads = false
	source, err := application.Local().Create(ctx, "entries", store.Values{"localizedHero": store.String(visible.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err = application.Local().PublishChanges(ctx, "entries", source.ID, store.Values{"localizedHero": store.String(hidden.ID)}, source.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	restrictUploads = true
	if _, err := application.Local().Duplicate(ctx, "entries", source.ID, nil, nil); !uploadReferenceIssue(err, "localizedHero.fr") {
		t.Fatalf("localized upload duplicate error = %v", err)
	}

	// Publish and unpublish validate the canonical document rather than only
	// the locale projected into status hooks.
	restrictUploads = false
	statusDocument, err := application.Local().Create(ctx, "entries", store.Values{"localizedHero": store.String(visible.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	statusDocument, err = application.Local().PublishChanges(ctx, "entries", statusDocument.ID, store.Values{"localizedHero": store.String(hidden.ID)}, statusDocument.Revision, nil, ridu.LocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	restrictUploads = true
	if _, err := application.Local().Publish(ctx, "entries", statusDocument.ID, statusDocument.Revision, nil); !uploadReferenceIssue(err, "localizedHero.fr") {
		t.Fatalf("localized upload publish error = %v", err)
	}
	restrictUploads = false
	statusDocument, err = application.Local().Publish(ctx, "entries", statusDocument.ID, statusDocument.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	restrictUploads = true
	if _, err := application.Local().Unpublish(ctx, "entries", statusDocument.ID, statusDocument.Revision, nil); !uploadReferenceIssue(err, "localizedHero.fr") {
		t.Fatalf("localized upload unpublish error = %v", err)
	}

	// Version restore routes its canonical snapshot through the same admission
	// boundary before replacing the current document.
	restrictUploads = false
	restoreDocument, err := application.Local().Create(ctx, "entries", store.Values{"hero": store.String(hidden.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restoreRevision := restoreDocument.Revision
	restoreDocument, err = application.Local().PublishChanges(ctx, "entries", restoreDocument.ID, store.Values{"hero": store.String(visible.ID)}, restoreDocument.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	restrictUploads = true
	if _, err := application.Local().Restore(ctx, "entries", restoreDocument.ID, restoreRevision, restoreDocument.Revision, nil); !uploadReferenceIssue(err, "hero") {
		t.Fatalf("upload restore error = %v", err)
	}
}

func TestAcceptedUploadReferencesRequestReferenceLocks(t *testing.T) {
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &referenceLockRecordingStore{Store: teststore.New()}
	application, err := ridu.New(ridu.Config{
		Name: "Upload reference locks", Storage: storageBackend, StorageNamespace: "upload-reference-locks",
		Collections: []ridu.Collection{
			{Slug: "media", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}},
			{Slug: "entries", Fields: field.Fields{field.Upload("hero", "media")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	media, err := application.Upload(ctx, "media", ridu.UploadInput{Filename: "locked.txt", Reader: strings.NewReader("locked")})
	if err != nil {
		t.Fatal(err)
	}
	backend.reset()
	if _, err := application.Local().Create(ctx, "entries", store.Values{"hero": store.String(media.ID)}, nil); err != nil {
		t.Fatal(err)
	}
	requests := backend.requests()
	if len(requests) != 1 || requests[0].ID != media.ID || requests[0].Lock != store.LockReference || !requests[0].Collection.Capabilities.Upload {
		t.Fatalf("upload reference lock requests = %#v", requests)
	}
}

func uploadReferenceIssue(err error, path string) bool {
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

type referenceLockRecordingStore struct {
	store.Store
	mu    sync.Mutex
	locks []store.Request
}

func (backend *referenceLockRecordingStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &referenceLockRecordingTransaction{Transaction: transaction, record: backend.record}, nil
}

func (backend *referenceLockRecordingStore) LockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	return backend.Store.(store.UploadObjectLocker).LockUploadObjects(ctx, keys)
}

func (backend *referenceLockRecordingStore) record(request store.Request) {
	if request.Lock != store.LockReference {
		return
	}
	backend.mu.Lock()
	backend.locks = append(backend.locks, request)
	backend.mu.Unlock()
}

func (backend *referenceLockRecordingStore) reset() {
	backend.mu.Lock()
	backend.locks = nil
	backend.mu.Unlock()
}

func (backend *referenceLockRecordingStore) requests() []store.Request {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]store.Request(nil), backend.locks...)
}

type referenceLockRecordingTransaction struct {
	store.Transaction
	record func(store.Request)
}

func (transaction *referenceLockRecordingTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	transaction.record(request)
	return transaction.Transaction.Find(ctx, request)
}

var _ store.Store = (*referenceLockRecordingStore)(nil)
var _ store.UploadObjectLocker = (*referenceLockRecordingStore)(nil)
var _ store.Transaction = (*referenceLockRecordingTransaction)(nil)
