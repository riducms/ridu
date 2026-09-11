package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/field"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type previewFindGateStore struct {
	*teststore.Store

	mu         sync.Mutex
	collection schema.CollectionSlug
	documentID string
	reached    chan struct{}
	release    chan struct{}
	armed      bool
}

type previewTestTransaction interface {
	store.Transaction
	store.VersionTransaction
}

func (backend *previewFindGateStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	return backend.wrap(transaction, err)
}

func (backend *previewFindGateStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.BeginSnapshot(ctx)
	return backend.wrap(transaction, err)
}

func (backend *previewFindGateStore) wrap(transaction store.Transaction, err error) (store.Transaction, error) {
	if err != nil {
		return nil, err
	}
	versioned, ok := transaction.(previewTestTransaction)
	if !ok {
		return nil, errors.New("preview test store does not support versions")
	}
	return &previewFindGateTransaction{previewTestTransaction: versioned, backend: backend}, nil
}

func (backend *previewFindGateStore) arm(collection schema.CollectionSlug, documentID string) (<-chan struct{}, func()) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.collection = collection
	backend.documentID = documentID
	backend.reached = make(chan struct{})
	backend.release = make(chan struct{})
	backend.armed = true
	var once sync.Once
	return backend.reached, func() { once.Do(func() { close(backend.release) }) }
}

func (backend *previewFindGateStore) claim(request store.Request) chan struct{} {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.armed || request.Collection.Slug != backend.collection || request.ID != backend.documentID {
		return nil
	}
	backend.armed = false
	close(backend.reached)
	return backend.release
}

type previewFindGateTransaction struct {
	previewTestTransaction
	backend *previewFindGateStore
}

func (transaction *previewFindGateTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	document, err := transaction.previewTestTransaction.Find(ctx, request)
	if release := transaction.backend.claim(request); release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return store.Document{}, ctx.Err()
		}
	}
	return document, err
}

type previewCommitErrorStore struct {
	*teststore.Store
	mu       sync.Mutex
	failNext bool
}

func (backend *previewCommitErrorStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	versioned, ok := transaction.(previewTestTransaction)
	if !ok {
		return nil, errors.New("preview test store does not support versions")
	}
	return &previewCommitErrorTransaction{previewTestTransaction: versioned, backend: backend}, nil
}

func (backend *previewCommitErrorStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	return backend.Store.BeginSnapshot(ctx)
}

func (backend *previewCommitErrorStore) failNextCommit() {
	backend.mu.Lock()
	backend.failNext = true
	backend.mu.Unlock()
}

func (backend *previewCommitErrorStore) takeFailure() bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.failNext {
		return false
	}
	backend.failNext = false
	return true
}

type previewCommitErrorTransaction struct {
	previewTestTransaction
	backend *previewCommitErrorStore
}

func (transaction *previewCommitErrorTransaction) Commit(ctx context.Context) error {
	if err := transaction.previewTestTransaction.Commit(ctx); err != nil {
		return err
	}
	if transaction.backend.takeFailure() {
		return errors.New("commit response lost")
	}
	return nil
}

func TestExpiredPreviewTokenIsRejectedAndRemoved(t *testing.T) {
	now := time.Now().UTC()
	for name, expiresAt := range map[string]time.Time{
		"past":           now.Add(-time.Second),
		"exact boundary": now,
	} {
		t.Run(name, func(t *testing.T) {
			raw := "expired-preview-secret-" + name
			digest := tokenDigest(raw)
			application := &App{previewTokens: &previewTokenRegistry{grants: map[string]previewTokenGrant{
				digest: {
					resource: "collection", slug: "posts", documentID: "post_1",
					actorID: "user_1", expiresAt: expiresAt,
				},
			}}}

			if _, err := application.resolvePreviewToken(context.Background(), raw, "collection", "posts", "post_1", now); err == nil {
				t.Fatal("expired preview token was accepted")
			}
			if _, exists := application.previewTokens.grants[digest]; exists {
				t.Fatal("expired preview token was not removed")
			}
		})
	}
}

func TestPreviewTokenBindsExactAuthCollectionAndReloadsActor(t *testing.T) {
	backend := teststore.New()
	application, err := New(Config{
		Name: "preview identity", Admin: AdminConfig{User: "users"},
		Collections: []Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("role")}},
			{Slug: "staff", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("role")}},
			{
				Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
				Admin:  CollectionAdmin{LivePreview: LivePreviewConfig{URL: "https://preview.example.test/posts/{id}"}},
				Fields: field.Fields{field.Text("title")},
				Access: CollectionAccess{
					Create: func(AccessContext) (AccessDecision, error) { return Allow(), nil },
					Read: func(ctx AccessContext) (AccessDecision, error) {
						if ctx.Actor != nil {
							role, _ := ctx.Actor.Values["role"].StringValue()
							if role == "staff" {
								return Allow(), nil
							}
						}
						return Deny(), nil
					},
				},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}

	collections := make(map[schema.CollectionSlug]schema.Collection)
	for _, collection := range application.Manifest().Snapshot().Collections {
		collections[collection.Slug] = collection
	}
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	usersActor, err := transaction.Create(context.Background(), store.CreateRequest{
		Collection: collections["users"], ID: "shared-actor", Values: store.Values{"email": store.String("user@example.test"), "role": store.String("reader")},
	})
	if err != nil {
		t.Fatal(err)
	}
	staffActor, err := transaction.Create(context.Background(), store.CreateRequest{
		Collection: collections["staff"], ID: "shared-actor", Values: store.Values{"email": store.String("staff@example.test"), "role": store.String("staff")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	post, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Scoped draft")}, MutationOptions{Actor: &staffActor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateCollectionPreviewToken(context.Background(), "posts", post.ID, &AuthIdentity{
		Collection: "users", Actor: usersActor,
	}); err == nil {
		t.Fatal("same-ID actor from the wrong auth collection minted a preview token")
	}
	token, err := application.CreateCollectionPreviewToken(context.Background(), "posts", post.ID, &AuthIdentity{
		Collection: "staff", Actor: staffActor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.FindCollectionPreview(context.Background(), token.Token, "posts", post.ID); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := application.FindCollectionPreview(canceled, token.Token, "posts", post.ID); err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("canceled actor reload = %v, want operational error", err)
	}
	application.previewTokens.mu.Lock()
	_, retained := application.previewTokens.grants[tokenDigest(token.Token)]
	application.previewTokens.mu.Unlock()
	if !retained {
		t.Fatal("transient actor reload failure removed the preview grant")
	}
	reusedTarget, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Original target")}, MutationOptions{Actor: &staffActor})
	if err != nil {
		t.Fatal(err)
	}
	reusedToken, err := application.CreateCollectionPreviewToken(context.Background(), "posts", reusedTarget.ID, &AuthIdentity{
		Collection: "staff", Actor: staffActor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", reusedTarget.ID, MutationOptions{Actor: &staffActor}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(context.Background(), "posts", store.Values{"title": store.String("Recreated target")}, ImportOptions{
		ID: reusedTarget.ID, Status: store.StatusDraft, CreatedAt: reusedTarget.CreatedAt, Actor: &staffActor}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.FindCollectionPreview(context.Background(), reusedToken.Token, "posts", reusedTarget.ID); err == nil {
		t.Fatal("preview token survived permanent deletion and target ID reuse")
	}

	usersActor, err = application.Local().Update(context.Background(), "users", usersActor.ID, store.Values{"role": store.String("staff")}, MutationOptions{Actor: &usersActor})
	if err != nil {
		t.Fatal(err)
	}
	staffActor, err = application.Local().Update(context.Background(), "staff", staffActor.ID, store.Values{"role": store.String("revoked")}, MutationOptions{Actor: &staffActor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.FindCollectionPreview(context.Background(), token.Token, "posts", post.ID); err == nil {
		t.Fatal("preview token used a same-ID actor from another collection or stale actor values")
	}

	_, err = application.Local().Delete(context.Background(), "staff", staffActor.ID, MutationOptions{Actor: &staffActor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.FindCollectionPreview(context.Background(), token.Token, "posts", post.ID); err == nil {
		t.Fatal("preview token survived deletion of its exact actor")
	}
	application.previewTokens.mu.Lock()
	_, exists := application.previewTokens.grants[tokenDigest(token.Token)]
	application.previewTokens.mu.Unlock()
	if exists {
		t.Fatal("orphaned preview token was not removed")
	}
	if _, err := application.Local().Import(context.Background(), "staff", store.Values{
		"email": store.String("replacement@example.test"), "role": store.String("staff"),
	}, ImportOptions{ID: staffActor.ID, CreatedAt: staffActor.CreatedAt, Actor: &usersActor}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.FindCollectionPreview(context.Background(), token.Token, "posts", post.ID); err == nil {
		t.Fatal("preview token survived actor deletion and same-generation-shaped import")
	}
}

func TestPreviewTokenActorQuotaAndExplicitRevocation(t *testing.T) {
	backend := teststore.New()
	application, err := New(Config{
		Name: "preview quota", Admin: AdminConfig{User: "users"},
		Collections: []Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
			{
				Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
				Admin:  CollectionAdmin{LivePreview: LivePreviewConfig{URL: "https://preview.example.test/posts/{id}"}},
				Fields: field.Fields{field.Text("title")},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("first@example.test")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("second@example.test")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, MutationOptions{Actor: &first})
	if err != nil {
		t.Fatal(err)
	}
	firstIdentity := &AuthIdentity{Collection: "users", Actor: first}
	secondIdentity := &AuthIdentity{Collection: "users", Actor: second}
	tokens := make([]PreviewToken, 0, maxPreviewTokensPerActor)
	for index := 0; index < maxPreviewTokensPerActor; index++ {
		token, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, firstIdentity)
		if err != nil {
			t.Fatalf("mint %d: %v", index, err)
		}
		tokens = append(tokens, token)
	}
	if _, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, firstIdentity); previewErrorCode(err) != "rate_limited" {
		t.Fatalf("actor quota error = %v", err)
	}
	if _, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, secondIdentity); err != nil {
		t.Fatal("one actor exhausted another actor's preview capacity", err)
	}
	if err := application.RevokePreviewToken(context.Background(), tokens[0].Token, secondIdentity); previewErrorCode(err) != "invalid_preview_token" {
		t.Fatalf("wrong-actor revoke = %v", err)
	}
	if err := application.RevokePreviewToken(context.Background(), tokens[0].Token, firstIdentity); err != nil {
		t.Fatal(err)
	}
	if err := application.RevokePreviewToken(context.Background(), tokens[0].Token, firstIdentity); err != nil {
		t.Fatal("repeated revoke was not idempotent", err)
	}
	if _, err := application.FindCollectionPreview(context.Background(), tokens[0].Token, "posts", target.ID); previewErrorCode(err) != "invalid_preview_token" {
		t.Fatalf("revoked token read = %v", err)
	}
	if _, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, firstIdentity); err != nil {
		t.Fatal("revocation did not release actor capacity", err)
	}
}

func TestPreviewMintFailsClosedAcrossPermanentDeleteAndSameShapedImport(t *testing.T) {
	for _, subject := range []string{"target", "actor"} {
		t.Run(subject, func(t *testing.T) {
			backend := &previewFindGateStore{Store: teststore.New()}
			application, actor, target := newPreviewRaceApplication(t, backend)
			collection, documentID := schema.CollectionSlug("posts"), target.ID
			if subject == "actor" {
				collection, documentID = "users", actor.ID
			}
			reached, release := backend.arm(collection, documentID)
			t.Cleanup(release)
			type mintResult struct {
				token PreviewToken
				err   error
			}
			result := make(chan mintResult, 1)
			go func() {
				token, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, &AuthIdentity{Collection: "users", Actor: actor})
				result <- mintResult{token: token, err: err}
			}()
			select {
			case <-reached:
			case <-time.After(2 * time.Second):
				t.Fatal("preview mint did not reach the validation gate")
			}

			if subject == "target" {
				if _, err := application.Local().Delete(context.Background(), "posts", target.ID, MutationOptions{Actor: &actor}); err != nil {
					t.Fatal(err)
				}
				if _, err := application.Local().Import(context.Background(), "posts", store.CloneValues(target.Values), ImportOptions{
					ID: target.ID, Status: target.Status, CreatedAt: target.CreatedAt, Actor: &actor}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := application.Local().Delete(context.Background(), "users", actor.ID, MutationOptions{}); err != nil {
					t.Fatal(err)
				}
				if _, err := application.Local().Import(context.Background(), "users", store.CloneValues(actor.Values), ImportOptions{
					ID: actor.ID, Status: actor.Status, CreatedAt: actor.CreatedAt,
				}); err != nil {
					t.Fatal(err)
				}
			}
			release()
			minted := <-result
			if previewErrorCode(minted.err) != "conflict" || minted.token.Token != "" {
				t.Fatalf("mint result = token %q, err %v", minted.token.Token, minted.err)
			}
			application.previewTokens.mu.Lock()
			grants := len(application.previewTokens.grants)
			application.previewTokens.mu.Unlock()
			if grants != 0 {
				t.Fatalf("preview grants after failed mint = %d", grants)
			}
		})
	}
}

func TestPreviewGrantIsRevokedWhenPermanentDeleteCommitOutcomeIsUnknown(t *testing.T) {
	backend := &previewCommitErrorStore{Store: teststore.New()}
	application, actor, target := newPreviewRaceApplication(t, backend)
	token, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, &AuthIdentity{Collection: "users", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	backend.failNextCommit()
	if _, err := application.Local().Delete(context.Background(), "posts", target.ID, MutationOptions{Actor: &actor}); err == nil {
		t.Fatal("permanent delete with a lost commit response succeeded")
	}
	if _, err := application.FindCollectionPreview(context.Background(), token.Token, "posts", target.ID); previewErrorCode(err) != "invalid_preview_token" {
		t.Fatalf("preview after uncertain delete commit = %v", err)
	}
	if _, err := application.Local().Find(context.Background(), "posts", target.ID, FindOptions{Actor: &actor}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("document after applied commit = %v, want not found", err)
	}
}

func TestPreviewMintRejectsIdentityAuthenticatedBeforeActorReplacement(t *testing.T) {
	application, actor, target := newPreviewRaceApplication(t, teststore.New())
	identity := &AuthIdentity{
		Collection: "users", Actor: actor,
		PreviewEpoch: application.previewIdentityEpoch(),
	}
	if _, err := application.Local().Delete(context.Background(), "users", actor.ID, MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(context.Background(), "users", store.CloneValues(actor.Values), ImportOptions{
		ID: actor.ID, Status: actor.Status, CreatedAt: actor.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	token, err := application.CreateCollectionPreviewToken(context.Background(), "posts", target.ID, identity)
	if previewErrorCode(err) != "conflict" || token.Token != "" {
		t.Fatalf("pre-delete identity mint = token %q, err %v", token.Token, err)
	}
}

func newPreviewRaceApplication(t *testing.T, backend store.Store) (*App, store.Document, store.Document) {
	t.Helper()
	application, err := New(Config{
		Name: "preview race", Admin: AdminConfig{User: "users"},
		Collections: []Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
			{
				Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
				Admin:  CollectionAdmin{LivePreview: LivePreviewConfig{URL: "https://preview.example.test/posts/{id}"}},
				Fields: field.Fields{field.Text("title")},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String("author@example.test")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Draft")}, MutationOptions{Actor: &actor})
	if err != nil {
		t.Fatal(err)
	}
	return application, actor, target
}

func previewErrorCode(err error) string {
	var operationError *operationengine.Error
	if errors.As(err, &operationError) {
		return operationError.Code
	}
	return ""
}
