package operation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestTransactionUploadObjectLocksAreDeduplicatedAcrossNestedOperations(t *testing.T) {
	locker := &recordingUploadObjectLocker{}
	state := &transactionState{}
	if err := state.lockUploadObjects(context.Background(), locker, []string{"object-b", "object-a", "object-a"}); err != nil {
		t.Fatal(err)
	}
	if err := state.lockUploadObjects(context.Background(), locker, []string{"object-c", "object-b"}); err != nil {
		t.Fatal(err)
	}
	if err := state.lockUploadObjects(context.Background(), locker, []string{"object-a"}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"object-a", "object-b"}, {"object-c"}}
	if !reflect.DeepEqual(locker.calls, want) {
		t.Fatalf("lock calls = %#v, want %#v", locker.calls, want)
	}
	state.releaseUploadObjectLocks()
	if locker.releases != len(want) {
		t.Fatalf("release calls = %d, want %d", locker.releases, len(want))
	}
}

type recordingUploadObjectLocker struct {
	calls    [][]string
	releases int
}

func (locker *recordingUploadObjectLocker) LockUploadObjects(_ context.Context, keys []string) (func(), error) {
	locker.calls = append(locker.calls, append([]string(nil), keys...))
	return func() { locker.releases++ }, nil
}

func TestTransactionMutationFailureRollsBackDocumentCreate(t *testing.T) {
	backend := teststore.New()
	engine, err := New(Config{
		Store: backend,
		Collections: []Collection{{
			Schema: schema.Collection{
				ID: "collection-users", Slug: "users",
				Labels: schema.CollectionLabels{Singular: "User", Plural: "Users"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Execute(context.Background(), Request{
		Operation: operation.Create, Collection: "users",
		TransactionMutation: func(context.Context, store.Transaction, schema.Collection, store.Document) error {
			return errors.New("credential write failed")
		},
	})
	var operationError *Error
	if !errors.As(err, &operationError) || operationError.Code != "store_failed" {
		t.Fatalf("mutation error = %v", err)
	}
	result, err := engine.Execute(context.Background(), Request{Operation: operation.Read, Collection: "users", Page: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Page == nil || result.Page.Total != 0 {
		t.Fatalf("documents after rollback = %#v", result.Page)
	}
}

func TestReadOnlyOperationUsesSnapshotTransaction(t *testing.T) {
	backend := &transactionIntentRecordingStore{Store: teststore.New()}
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts",
		Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	engine, err := New(Config{Store: backend, Collections: []Collection{{Schema: collection}}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", ImportID: "post_1"}); err != nil {
		t.Fatal(err)
	}
	if backend.writeBegins != 1 || backend.snapshotBegins != 0 {
		t.Fatalf("create begins = write:%d snapshot:%d, want write:1 snapshot:0", backend.writeBegins, backend.snapshotBegins)
	}
	if _, err := engine.Execute(context.Background(), Request{Operation: operation.Read, Collection: "posts", ID: "post_1"}); err != nil {
		t.Fatal(err)
	}
	if backend.writeBegins != 1 || backend.snapshotBegins != 1 {
		t.Fatalf("read begins = write:%d snapshot:%d, want write:1 snapshot:1", backend.writeBegins, backend.snapshotBegins)
	}
	if _, err := engine.ExecuteBatch(context.Background(), []Request{
		{Operation: operation.Read, Collection: "posts", ID: "post_1"},
		{Operation: operation.Read, Collection: "posts", Page: 1, Limit: 10},
	}); err != nil {
		t.Fatal(err)
	}
	if backend.writeBegins != 1 || backend.snapshotBegins != 2 {
		t.Fatalf("read batch begins = write:%d snapshot:%d, want write:1 snapshot:2", backend.writeBegins, backend.snapshotBegins)
	}
}

func TestReadOnlyOperationRejectsNestedMutationBeforeAdmission(t *testing.T) {
	backend := &transactionIntentRecordingStore{Store: teststore.New()}
	joinPath, err := query.NewPath("assets")
	if err != nil {
		t.Fatal(err)
	}
	relationshipPath, err := query.NewPath("post")
	if err != nil {
		t.Fatal(err)
	}
	resource := &TransactionResource{
		Commit: func() { t.Fatal("rejected resource committed") },
		Rollback: func(context.Context) error {
			t.Fatal("rejected resource rolled back")
			return nil
		},
		Unknown: func() { t.Fatal("rejected resource received an unknown outcome") },
	}
	var engine *Engine
	var nestedError error
	var nestedJoinError error
	assetHookRan := false
	transactionMutationRan := false
	configured, err := New(Config{
		Store: backend,
		Collections: []Collection{
			{
				Schema: schema.Collection{
					ID: "collection-posts", Slug: "posts",
					Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
					Fields: []schema.Field{{
						ID: "posts-assets", Name: "assets", Path: joinPath,
						Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation,
						Join: &schema.JoinField{CollectionID: "collection-assets", CollectionSlug: "assets", On: relationshipPath},
					}},
				},
				Hooks: Hooks{BeforeOperation: []Hook{func(hookContext Context) error {
					if hookContext.Operation != operation.Read {
						return nil
					}
					_, nestedJoinError = engine.MutateJoin(hookContext.Context, JoinMutationRequest{
						Collection: "posts", ID: "post_1", Field: "assets", Additions: []string{"asset_1"},
					})
					_, nestedError = engine.Execute(hookContext.Context, Request{
						Operation: operation.Create, Collection: "assets", ImportID: "asset_1", TransactionResource: resource,
						TransactionMutation: func(context.Context, store.Transaction, schema.Collection, store.Document) error {
							transactionMutationRan = true
							return nil
						},
					})
					return errors.Join(nestedJoinError, nestedError)
				}}},
			},
			{
				Schema: schema.Collection{
					ID: "collection-assets", Slug: "assets",
					Labels: schema.CollectionLabels{Singular: "Asset", Plural: "Assets"},
				},
				Hooks: Hooks{BeforeOperation: []Hook{func(Context) error {
					assetHookRan = true
					return nil
				}}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine = configured

	_, err = engine.Execute(context.Background(), Request{Operation: operation.Read, Collection: "posts", Page: 1, Limit: 10})
	var operationError *Error
	if !errors.As(nestedError, &operationError) || operationError.Code != "transaction_read_only" || operationError.Status != 409 {
		t.Fatalf("nested mutation error = %#v, %v", operationError, nestedError)
	}
	operationError = nil
	if !errors.As(nestedJoinError, &operationError) || operationError.Code != "transaction_read_only" || operationError.Status != 409 {
		t.Fatalf("nested join mutation error = %#v, %v", operationError, nestedJoinError)
	}
	if !errors.Is(err, errMutationInReadOnlyTransaction) {
		t.Fatalf("outer read error = %v, want read-only transaction rejection", err)
	}
	if assetHookRan || transactionMutationRan || resource.Claimed() {
		t.Fatalf("rejected mutation effects = asset hook:%t transaction mutation:%t resource claimed:%t", assetHookRan, transactionMutationRan, resource.Claimed())
	}
	if backend.writeBegins != 0 || backend.snapshotBegins != 1 {
		t.Fatalf("nested mutation begins = write:%d snapshot:%d, want write:0 snapshot:1", backend.writeBegins, backend.snapshotBegins)
	}
}

type transactionIntentRecordingStore struct {
	store.Store
	writeBegins    int
	snapshotBegins int
}

func (backend *transactionIntentRecordingStore) Begin(ctx context.Context) (store.Transaction, error) {
	backend.writeBegins++
	return backend.Store.Begin(ctx)
}

func (backend *transactionIntentRecordingStore) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	backend.snapshotBegins++
	return backend.Store.(store.SnapshotStore).BeginSnapshot(ctx)
}

func TestPermanentDeleteFenceSpansCommitAndLifecycleInvalidation(t *testing.T) {
	backend := teststore.New()
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	var began bool
	var released bool
	engine, err := New(Config{
		Store:       backend,
		Collections: []Collection{{Schema: collection}},
		BeginPermanentDeleteFence: func(_ context.Context, deletes []PermanentDelete) func(bool) {
			began = true
			if len(deletes) != 1 || deletes[0].Collection.ID != collection.ID || deletes[0].DocumentID != "post_1" {
				t.Fatalf("fenced deletes = %#v", deletes)
			}
			return func(committed bool) {
				if !committed {
					t.Fatal("permanent-delete fence released before a successful commit")
				}
				transaction, beginErr := backend.Begin(context.Background())
				if beginErr != nil {
					t.Fatal(beginErr)
				}
				_, findErr := transaction.Find(context.Background(), store.Request{Collection: collection, ID: "post_1"})
				_ = transaction.Rollback(context.Background())
				if !errors.Is(findErr, store.ErrNotFound) {
					t.Fatalf("document at lifecycle invalidation = %v, want not found", findErr)
				}
				released = true
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), Request{
		Operation: operation.Create, Collection: "posts", ImportID: "post_1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), Request{
		Operation: operation.Delete, Collection: "posts", ID: "post_1",
	}); err != nil {
		t.Fatal(err)
	}
	if !began || !released {
		t.Fatalf("fence lifecycle began=%v released=%v", began, released)
	}
}

func TestPermanentDeleteCleanupIsCoalescedAfterUserHooks(t *testing.T) {
	backend := teststore.New()
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	var order []string
	cleanupCalls := 0
	engine, err := New(Config{
		Store: backend,
		Collections: []Collection{{
			Schema: collection,
			Hooks: Hooks{AfterCommit: []Hook{func(ctx Context) error {
				if ctx.Operation == operation.Delete {
					order = append(order, "hook:"+ctx.ID)
				}
				return nil
			}}},
		}},
		CleanupPermanentDeletes: func(_ context.Context, deletes []PermanentDelete) error {
			cleanupCalls++
			order = append(order, "cleanup")
			if len(deletes) != 2 || deletes[0].Original.ID != "post_1" || deletes[1].Original.ID != "post_2" {
				t.Fatalf("coalesced deletes = %#v", deletes)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"post_1", "post_2"} {
		if _, err := engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", ImportID: id}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = engine.ExecuteBatch(context.Background(), []Request{
		{Operation: operation.Delete, Collection: "posts", ID: "post_1"},
		{Operation: operation.Delete, Collection: "posts", ID: "post_2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupCalls != 1 || !reflect.DeepEqual(order, []string{"hook:post_1", "hook:post_2", "cleanup"}) {
		t.Fatalf("after-commit order = %v, cleanup calls = %d", order, cleanupCalls)
	}
}

func TestPermanentDeleteCleanupContinuesAfterDirectAndDispatcherPanics(t *testing.T) {
	const panicSecret = "after-commit-panic-secret"
	for _, test := range []struct {
		name       string
		dispatcher bool
	}{
		{name: "direct"},
		{name: "dispatcher", dispatcher: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := teststore.New()
			collection := schema.Collection{
				ID: "collection-posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			}
			laterRan := false
			cleanupRan := false
			firstDispatch := true
			config := Config{
				Store: backend,
				Collections: []Collection{{
					Schema: collection,
					Hooks: Hooks{AfterCommit: []Hook{
						func(ctx Context) error {
							if ctx.Operation == operation.Delete && !test.dispatcher {
								panic(panicSecret)
							}
							return nil
						},
						func(ctx Context) error {
							if ctx.Operation == operation.Delete {
								laterRan = true
							}
							return nil
						},
					}},
				}},
				CleanupPermanentDeletes: func(context.Context, []PermanentDelete) error {
					cleanupRan = true
					return nil
				},
			}
			if test.dispatcher {
				config.DispatchAfterCommit = func(ctx Context, hook Hook) error {
					if ctx.Operation == operation.Delete && firstDispatch {
						firstDispatch = false
						panic(panicSecret)
					}
					return hook(ctx)
				}
			}
			engine, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", ImportID: "post_1"}); err != nil {
				t.Fatal(err)
			}
			_, err = engine.Execute(context.Background(), Request{Operation: operation.Delete, Collection: "posts", ID: "post_1"})
			var operationError *Error
			if !errors.As(err, &operationError) || operationError.Code != "hook_failed" || !operationError.Committed {
				t.Fatalf("delete error = %#v, %v", operationError, err)
			}
			if strings.Contains(fmt.Sprintf("%v", err), panicSecret) || strings.Contains(fmt.Sprintf("%v", operationError.Cause), panicSecret) {
				t.Fatal("committed error exposed the recovered panic value")
			}
			if !laterRan || !cleanupRan {
				t.Fatalf("later hook ran=%v cleanup ran=%v", laterRan, cleanupRan)
			}
			transaction, beginErr := backend.Begin(context.Background())
			if beginErr != nil {
				t.Fatal(beginErr)
			}
			_, findErr := transaction.Find(context.Background(), store.Request{Collection: collection, ID: "post_1"})
			_ = transaction.Rollback(context.Background())
			if !errors.Is(findErr, store.ErrNotFound) {
				t.Fatalf("committed delete was not durable: %v", findErr)
			}
		})
	}
}

func TestCommitConflictsKeepStableOperationAndBatchSemantics(t *testing.T) {
	backend := &conflictCommitStore{Store: teststore.New()}
	engine, err := New(Config{
		Store: backend,
		Collections: []Collection{{Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts",
			Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertConflict := func(label string, err error) {
		t.Helper()
		var operationError *Error
		if !errors.As(err, &operationError) || operationError.Code != "conflict" || operationError.Status != 409 {
			t.Fatalf("%s commit error = %#v, %v", label, operationError, err)
		}
		if !operationError.CommitAttempted {
			t.Fatalf("%s commit error did not preserve the uncertain commit outcome", label)
		}
	}
	_, err = engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", ImportID: "post_1"})
	assertConflict("single", err)
	_, err = engine.ExecuteBatch(context.Background(), []Request{{Operation: operation.Create, Collection: "posts", ImportID: "post_2"}})
	assertConflict("batch", err)
}

type conflictCommitStore struct{ store.Store }

func (backend *conflictCommitStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &conflictCommitTransaction{Transaction: transaction}, nil
}

type conflictCommitTransaction struct{ store.Transaction }

func (*conflictCommitTransaction) Commit(context.Context) error { return store.ErrConflict }

func TestTransactionResourceFinalizesBeforeAfterCommit(t *testing.T) {
	backend := teststore.New()
	commits := 0
	rollbacks := 0
	unknowns := 0
	resource := &TransactionResource{
		Commit: func() { commits++ },
		Rollback: func(context.Context) error {
			rollbacks++
			return nil
		},
		Unknown: func() { unknowns++ },
	}
	engine, err := New(Config{
		Store: backend,
		Collections: []Collection{{
			Schema: schema.Collection{
				ID: "collection-posts", Slug: "posts",
				Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			},
			Hooks: Hooks{AfterCommit: []Hook{func(Context) error {
				if commits != 1 || rollbacks != 0 || unknowns != 0 {
					t.Fatalf("resource outcome at after-commit = commit:%d rollback:%d unknown:%d", commits, rollbacks, unknowns)
				}
				return nil
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), Request{
		Operation: operation.Create, Collection: "posts", ImportID: "post_1", TransactionResource: resource,
	}); err != nil {
		t.Fatal(err)
	}
	if !resource.Claimed() || commits != 1 || rollbacks != 0 || unknowns != 0 {
		t.Fatalf("resource outcome = claimed:%v commit:%d rollback:%d unknown:%d", resource.Claimed(), commits, rollbacks, unknowns)
	}
}

func TestTransactionResourceRollsBackAfterDefiniteOperationFailure(t *testing.T) {
	backend := teststore.New()
	commits := 0
	rollbacks := 0
	unknowns := 0
	resource := &TransactionResource{
		Commit: func() { commits++ },
		Rollback: func(context.Context) error {
			rollbacks++
			return nil
		},
		Unknown: func() { unknowns++ },
	}
	engine, err := New(Config{
		Store: backend,
		Collections: []Collection{{Schema: schema.Collection{
			ID: "collection-posts", Slug: "posts",
			Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Execute(context.Background(), Request{
		Operation: operation.Create, Collection: "posts", ImportID: "post_1", TransactionResource: resource,
		TransactionMutation: func(context.Context, store.Transaction, schema.Collection, store.Document) error {
			return errors.New("credential write failed")
		},
	})
	if err == nil {
		t.Fatal("operation unexpectedly succeeded")
	}
	if !resource.Claimed() || commits != 0 || rollbacks != 1 || unknowns != 0 {
		t.Fatalf("resource outcome = claimed:%v commit:%d rollback:%d unknown:%d", resource.Claimed(), commits, rollbacks, unknowns)
	}
}

func TestTransactionResourceIsRetainedWhenCommitOutcomeIsUnknown(t *testing.T) {
	base := teststore.New()
	backend := &appliedCommitErrorStore{Store: base, commitError: errors.New("connection lost after commit")}
	commits := 0
	rollbacks := 0
	unknowns := 0
	resource := &TransactionResource{
		Commit: func() { commits++ },
		Rollback: func(context.Context) error {
			rollbacks++
			return nil
		},
		Unknown: func() { unknowns++ },
	}
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts",
		Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	engine, err := New(Config{Store: backend, Collections: []Collection{{Schema: collection}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Execute(context.Background(), Request{
		Operation: operation.Create, Collection: "posts", ImportID: "post_1", TransactionResource: resource,
	})
	var operationError *Error
	if !errors.As(err, &operationError) || !operationError.CommitAttempted {
		t.Fatalf("commit error = %#v, %v", operationError, err)
	}
	if !resource.Claimed() || commits != 0 || rollbacks != 0 || unknowns != 1 {
		t.Fatalf("resource outcome = claimed:%v commit:%d rollback:%d unknown:%d", resource.Claimed(), commits, rollbacks, unknowns)
	}
	transaction, err := base.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Find(context.Background(), store.Request{Collection: collection, ID: "post_1"}); err != nil {
		t.Fatalf("server-side committed document was not retained: %v", err)
	}
}

func TestSwallowedNestedFailurePoisonsOuterTransactionAndRollsBackResource(t *testing.T) {
	backend := teststore.New()
	commits := 0
	rollbacks := 0
	unknowns := 0
	resource := &TransactionResource{
		Commit: func() { commits++ },
		Rollback: func(context.Context) error {
			rollbacks++
			return nil
		},
		Unknown: func() { unknowns++ },
	}
	var engine *Engine
	configured, err := New(Config{
		Store: backend,
		Collections: []Collection{
			{
				Schema: schema.Collection{
					ID: "collection-posts", Slug: "posts",
					Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
				},
				Hooks: Hooks{BeforeOperation: []Hook{func(hookContext Context) error {
					_, _ = engine.Execute(hookContext.Context, Request{
						Operation: operation.Create, Collection: "assets", ImportID: "asset_1", TransactionResource: resource,
						TransactionMutation: func(context.Context, store.Transaction, schema.Collection, store.Document) error {
							return errors.New("nested mutation failed after its row write")
						},
					})
					return nil
				}}},
			},
			{Schema: schema.Collection{
				ID: "collection-assets", Slug: "assets",
				Labels: schema.CollectionLabels{Singular: "Asset", Plural: "Assets"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine = configured
	_, err = engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", ImportID: "post_1"})
	var operationError *Error
	if !errors.As(err, &operationError) || operationError.Code != "store_failed" || operationError.CommitAttempted {
		t.Fatalf("outer transaction error = %#v, %v", operationError, err)
	}
	if !resource.Claimed() || commits != 0 || rollbacks != 1 || unknowns != 0 {
		t.Fatalf("resource outcome = claimed:%v commit:%d rollback:%d unknown:%d", resource.Claimed(), commits, rollbacks, unknowns)
	}
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	for _, target := range []struct {
		collection schema.Collection
		id         string
	}{
		{collection: configured.collections["posts"].Schema, id: "post_1"},
		{collection: configured.collections["assets"].Schema, id: "asset_1"},
	} {
		if _, findError := transaction.Find(context.Background(), store.Request{Collection: target.collection, ID: target.id}); !errors.Is(findError, store.ErrNotFound) {
			t.Fatalf("%s after poisoned rollback = %v, want not found", target.id, findError)
		}
	}
}

func TestTransactionResourceIsNotClaimedBeforeTransactionAdmission(t *testing.T) {
	resource := &TransactionResource{
		Commit: func() { t.Fatal("unclaimed resource committed") },
		Rollback: func(context.Context) error {
			t.Fatal("unclaimed resource rolled back by the engine")
			return nil
		},
		Unknown: func() { t.Fatal("unclaimed resource received unknown outcome") },
	}
	engine, err := New(Config{Store: teststore.New()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), Request{
		Operation: operation.Create, Collection: "missing", TransactionResource: resource,
	}); err == nil {
		t.Fatal("unknown collection unexpectedly succeeded")
	}
	if resource.Claimed() {
		t.Fatal("resource was claimed before request admission")
	}
}

func TestRestoreVersionReadRollsBackNestedTransactionResource(t *testing.T) {
	backend := teststore.New()
	commits := 0
	rollbacks := 0
	unknowns := 0
	resource := &TransactionResource{
		Commit: func() { commits++ },
		Rollback: func(context.Context) error {
			rollbacks++
			return nil
		},
		Unknown: func() { unknowns++ },
	}
	posts := schema.Collection{
		ID: "collection-posts", Slug: "posts",
		Labels:       schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Capabilities: schema.Capabilities{Versions: true},
		Versions:     &schema.VersionSettings{MaxPerDocument: 10},
	}
	assets := schema.Collection{
		ID: "collection-assets", Slug: "assets",
		Labels: schema.CollectionLabels{Singular: "Asset", Plural: "Assets"},
	}
	var engine *Engine
	configured, err := New(Config{
		Store: backend,
		Collections: []Collection{
			{
				Schema: posts,
				Access: map[operation.Kind]Access{operation.ReadVersions: func(accessContext Context) (Decision, error) {
					if _, nestedError := engine.Execute(accessContext.Context, Request{
						Operation: operation.Create, Collection: "assets", ImportID: "asset_1", TransactionResource: resource,
					}); nestedError != nil {
						return Decision{}, nestedError
					}
					return Decision{}, errors.New("version access failed after nested write")
				}},
			},
			{Schema: assets},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine = configured
	created, err := engine.Execute(context.Background(), Request{Operation: operation.Create, Collection: "posts", ImportID: "post_1"})
	if err != nil || created.Document == nil {
		t.Fatalf("create versioned document = %#v, %v", created.Document, err)
	}
	_, err = engine.Restore(context.Background(), "posts", "post_1", 1, created.Document.Revision, false, nil)
	var operationError *Error
	if !errors.As(err, &operationError) || operationError.Code != "access_failed" {
		t.Fatalf("restore access error = %#v, %v", operationError, err)
	}
	if !resource.Claimed() || commits != 0 || rollbacks != 1 || unknowns != 0 {
		t.Fatalf("resource outcome = claimed:%v commit:%d rollback:%d unknown:%d", resource.Claimed(), commits, rollbacks, unknowns)
	}
	transaction, err := backend.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, findError := transaction.Find(context.Background(), store.Request{Collection: assets, ID: "asset_1"}); !errors.Is(findError, store.ErrNotFound) {
		t.Fatalf("nested restore resource row after rollback = %v, want not found", findError)
	}
}

type appliedCommitErrorStore struct {
	store.Store
	commitError error
}

func (backend *appliedCommitErrorStore) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &appliedCommitErrorTransaction{Transaction: transaction, commitError: backend.commitError}, nil
}

type appliedCommitErrorTransaction struct {
	store.Transaction
	commitError error
}

func (transaction *appliedCommitErrorTransaction) Commit(ctx context.Context) error {
	if err := transaction.Transaction.Commit(ctx); err != nil {
		return err
	}
	return transaction.commitError
}
