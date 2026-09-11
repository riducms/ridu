package graphql_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

type graphQLUploadObject struct {
	body        []byte
	contentType string
	modifiedAt  time.Time
}

type graphQLUploadStorage struct {
	mu      sync.Mutex
	objects map[string]graphQLUploadObject
}

func newGraphQLUploadStorage() *graphQLUploadStorage {
	return &graphQLUploadStorage{objects: make(map[string]graphQLUploadObject)}
}

func (backend *graphQLUploadStorage) Put(_ context.Context, key string, reader io.Reader, _ int64, contentType string) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	backend.objects[key] = graphQLUploadObject{body: append([]byte(nil), body...), contentType: contentType, modifiedAt: time.Now().UTC()}
	backend.mu.Unlock()
	return nil
}

func (backend *graphQLUploadStorage) Open(_ context.Context, key string) (io.ReadCloser, storage.Object, error) {
	backend.mu.Lock()
	object, exists := backend.objects[key]
	backend.mu.Unlock()
	if !exists {
		return nil, storage.Object{}, storage.ErrNotFound
	}
	body := append([]byte(nil), object.body...)
	return io.NopCloser(bytes.NewReader(body)), storage.Object{
		Key: key, Size: int64(len(body)), ContentType: object.contentType, ModifiedAt: object.modifiedAt,
	}, nil
}

func (backend *graphQLUploadStorage) Delete(_ context.Context, key string) error {
	backend.mu.Lock()
	delete(backend.objects, key)
	backend.mu.Unlock()
	return nil
}

func (backend *graphQLUploadStorage) List(_ context.Context, request storage.ListRequest) (storage.ListPage, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	result := make([]storage.Object, 0, len(backend.objects))
	for key, object := range backend.objects {
		if !strings.HasPrefix(key, request.Prefix) || key <= request.Cursor {
			continue
		}
		result = append(result, storage.Object{
			Key: key, Size: int64(len(object.body)), ContentType: object.contentType, ModifiedAt: object.modifiedAt,
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Key < result[right].Key })
	page := storage.ListPage{Objects: result}
	if len(page.Objects) > request.Limit {
		page.Objects = page.Objects[:request.Limit]
		page.NextCursor = page.Objects[len(page.Objects)-1].Key
	}
	return page, nil
}

func (backend *graphQLUploadStorage) contains(key string) bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	_, exists := backend.objects[key]
	return exists
}

func (backend *graphQLUploadStorage) count() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return len(backend.objects)
}

type graphQLUploadObservation struct {
	operation       operation.Kind
	actorID         string
	actorCollection string
}

func TestGraphQLUploadMutationsMatchExecutableStorageContract(t *testing.T) {
	backend := newGraphQLUploadStorage()
	var observationsMu sync.Mutex
	var accessObservations, hookObservations []graphQLUploadObservation
	recordAccess := func(ctx ridu.AccessContext) {
		if ctx.Operation != operation.Create && ctx.Operation != operation.Duplicate {
			return
		}
		observation := graphQLUploadObservation{operation: ctx.Operation, actorCollection: string(ctx.ActorCollection)}
		if ctx.Actor != nil {
			observation.actorID = ctx.Actor.ID
		}
		observationsMu.Lock()
		accessObservations = append(accessObservations, observation)
		observationsMu.Unlock()
	}
	recordHook := func(ctx ridu.HookContext) error {
		observation := graphQLUploadObservation{operation: ctx.Operation, actorCollection: string(ctx.ActorCollection)}
		if ctx.Actor != nil {
			observation.actorID = ctx.Actor.ID
		}
		observationsMu.Lock()
		hookObservations = append(hookObservations, observation)
		observationsMu.Unlock()
		return nil
	}
	staffOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		recordAccess(ctx)
		if ctx.Actor != nil && ctx.ActorCollection == "staff" {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}

	application, err := ridu.New(ridu.Config{
		Name: "GraphQL upload execution", Plugins: []ridu.Plugin{graphqlplugin.New()},
		Admin: ridu.AdminConfig{User: "staff"}, Storage: backend, StorageNamespace: "graphql-upload-execution",
		Collections: []ridu.Collection{
			{
				Slug: "staff", Auth: true,
				AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
				Fields: field.Fields{
					field.Email("email").Required().Unique(),
				},
			},
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
				Fields: field.Fields{
					field.Text("caption"),
				},
				Access: ridu.CollectionAccess{Create: staffOnly, Read: staffOnly, Update: staffOnly, Delete: staffOnly},
				Hooks:  ridu.CollectionHooks{BeforeDuplicate: []ridu.Hook{recordHook}, BeforeChange: []ridu.Hook{recordHook}},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	staff, err := application.Local().Import(ctx, "staff", store.Values{"email": store.String("editor@example.test")}, ridu.ImportOptions{ID: "graphql-upload-editor", Status: store.StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "staff", staff.ID, "graphql-upload-password"); err != nil {
		t.Fatal(err)
	}
	session, err := application.Login(ctx, "staff", "editor@example.test", "graphql-upload-password")
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "source.txt", Reader: bytes.NewBufferString("independent upload bytes"),
		Data: store.Values{"caption": store.String("Source")}, Actor: &staff, ActorCollection: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := source.Values["objectKey"].StringValue()
	if sourceKey == "" || !backend.contains(sourceKey) {
		t.Fatalf("source object was not committed: %q", sourceKey)
	}

	manifest := application.Manifest()
	sdl, err := graphqlplugin.GenerateSDL(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, advertised := range []string{"duplicateMedia(", "updateMedia(", "deleteMedia("} {
		if !strings.Contains(sdl, advertised) {
			t.Fatalf("upload SDL omitted executable mutation %q:\n%s", advertised, sdl)
		}
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	objectsBeforeDenied := backend.count()
	denied := graphQL(t, server.URL, fmt.Sprintf(`mutation { duplicateMedia(id: %q) { id } }`, source.ID))
	assertErrorCode(t, denied, "access_denied")
	if backend.count() != objectsBeforeDenied {
		t.Fatalf("denied duplicate changed storage object count: got %d, want %d", backend.count(), objectsBeforeDenied)
	}

	headers := make(http.Header)
	headers.Set("Authorization", "sEsSiOn \t "+session.Token)
	duplicateResult := graphQLWithHeaders(t, server.URL, fmt.Sprintf(`mutation { duplicateMedia(id: %q, data: {caption: "Copy"}) { id caption objectKey } }`, source.ID), headers)
	duplicate := objectAt(t, duplicateResult, "data", "duplicateMedia")
	duplicateID, _ := duplicate["id"].(string)
	duplicateKey, _ := duplicate["objectKey"].(string)
	if duplicateID == "" || duplicateID == source.ID || duplicateKey == "" || duplicateKey == sourceKey || !backend.contains(duplicateKey) {
		t.Fatalf("storage-aware GraphQL duplicate = %#v; source key = %q", duplicate, sourceKey)
	}
	reader, _, err := application.OpenUploadForIdentity(ctx, "media", duplicateKey, &ridu.AuthIdentity{Collection: "staff", Actor: staff})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != "independent upload bytes" {
		t.Fatalf("duplicate body = %q", body)
	}

	updated := graphQLWithToken(t, server.URL, session.Token, fmt.Sprintf(`mutation { updateMedia(id: %q, data: {caption: "Updated copy"}) { id caption objectKey } }`, duplicateID))
	if document := objectAt(t, updated, "data", "updateMedia"); document["caption"] != "Updated copy" || document["objectKey"] != duplicateKey {
		t.Fatalf("executable upload metadata update = %#v", document)
	}
	deletedDuplicate := graphQLWithToken(t, server.URL, session.Token, fmt.Sprintf(`mutation { deleteMedia(id: %q) { id } }`, duplicateID))
	if objectAt(t, deletedDuplicate, "data", "deleteMedia")["id"] != duplicateID {
		t.Fatalf("delete duplicate result = %#v", deletedDuplicate)
	}
	if backend.contains(duplicateKey) || !backend.contains(sourceKey) {
		t.Fatalf("deleting duplicate affected source: duplicate=%t source=%t", backend.contains(duplicateKey), backend.contains(sourceKey))
	}

	secondResult := graphQLWithToken(t, server.URL, session.Token, fmt.Sprintf(`mutation { duplicateMedia(id: %q) { id objectKey } }`, source.ID))
	second := objectAt(t, secondResult, "data", "duplicateMedia")
	secondKey, _ := second["objectKey"].(string)
	if secondKey == "" || secondKey == sourceKey || !backend.contains(secondKey) {
		t.Fatalf("second storage-aware duplicate = %#v", second)
	}
	deletedSource := graphQLWithToken(t, server.URL, session.Token, fmt.Sprintf(`mutation { deleteMedia(id: %q) { id } }`, source.ID))
	if objectAt(t, deletedSource, "data", "deleteMedia")["id"] != source.ID {
		t.Fatalf("delete source result = %#v", deletedSource)
	}
	if backend.contains(sourceKey) || !backend.contains(secondKey) {
		t.Fatalf("deleting source affected duplicate: source=%t duplicate=%t", backend.contains(sourceKey), backend.contains(secondKey))
	}

	observationsMu.Lock()
	defer observationsMu.Unlock()
	assertExactActor := func(kind string, observations []graphQLUploadObservation) {
		t.Helper()
		foundDuplicate := false
		for _, observation := range observations {
			if observation.operation != operation.Duplicate {
				continue
			}
			foundDuplicate = true
			if observation.actorID != staff.ID || observation.actorCollection != "staff" {
				t.Fatalf("%s duplicate actor context = %#v, want %s/staff", kind, observation, staff.ID)
			}
		}
		if !foundDuplicate {
			t.Fatalf("%s did not observe duplicate operation: %#v", kind, observations)
		}
	}
	assertExactActor("access", accessObservations)
	assertExactActor("hook", hookObservations)
}
