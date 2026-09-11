package mongodb

import (
	"context"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBUploadDocumentsReferencesPopulationVersionsAndTargetedLookup(t *testing.T) {
	backend := mongoIntegrationStore(t)
	media, posts, manifest := mongoUploadTestSchema(t)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	seed := mongoBegin(t, backend, false)
	original, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: "versioned-media", Status: store.StatusDraft,
		Values: mongoUploadValues("old-original", "$old-variant"),
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	versions := seed.(store.VersionTransaction)
	if _, err := versions.SaveVersion(t.Context(), media, original, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	current, err := seed.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: media, ID: original.ID},
		Values: store.Values{
			"objectKey": store.String("current-original"),
			"sizes": store.Object(store.Values{"$current": store.Object(store.Values{
				"objectKey": store.String("$current-variant"),
			})}),
		},
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(t.Context(), media, current, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := seed.Trash(t.Context(), store.Request{Collection: media, ID: original.ID}); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	targetValues := mongoUploadValues("active-original", "active-variant")
	targetValues["sizes"] = store.Object(store.Values{"thumb": store.Object(store.Values{
		"objectKey": store.String("active-variant"),
	})})
	target, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: "active-media", Status: store.StatusPublished,
		Values: targetValues,
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(t.Context(), media, target, 10); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	otherTarget, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: "other-media", Status: store.StatusPublished,
		Values: mongoUploadValues("other-original", "other-variant"),
	})
	if err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	deliveryOriginal := "ridu/mongodb-upload-test/objects/11111111111111111111111111111111/original.txt"
	deliveryVariant := "ridu/mongodb-upload-test/objects/11111111111111111111111111111111/sizes/thumb.jpg"
	deliveryValues := mongoUploadValues(deliveryOriginal, deliveryVariant)
	deliveryValues["sizes"] = store.Object(store.Values{"thumb": store.Object(store.Values{
		"objectKey": store.String(deliveryVariant),
	})})
	if _, err := seed.Create(t.Context(), store.CreateRequest{
		Collection: media, ID: "delivery-media", Status: store.StatusPublished, Values: deliveryValues,
	}); err != nil {
		mongoRollback(t, seed)
		t.Fatal(err)
	}
	mongoCommit(t, seed)

	lookupKeys := []string{"missing", "active-original", "active-variant", "current-original", "$current-variant", "old-original", "$old-variant"}
	snapshot := mongoBegin(t, backend, true)
	referenced, err := snapshot.(store.UploadReferenceTransaction).ReferencedUploadObjects(t.Context(), store.UploadReferenceRequest{
		Collections: []schema.Collection{media}, ObjectKeys: lookupKeys,
	})
	if err != nil {
		mongoRollback(t, snapshot)
		t.Fatal(err)
	}
	if !reflect.DeepEqual(referenced, []string{"$current-variant", "$old-variant", "active-original", "active-variant", "current-original", "old-original"}) {
		mongoRollback(t, snapshot)
		t.Fatalf("referenced upload objects = %#v", referenced)
	}
	if !reflect.DeepEqual(lookupKeys, []string{"missing", "active-original", "active-variant", "current-original", "$current-variant", "old-original", "$old-variant"}) {
		mongoRollback(t, snapshot)
		t.Fatalf("lookup mutated caller keys: %#v", lookupKeys)
	}
	mongoRollback(t, snapshot)

	stable := mongoBegin(t, backend, true)
	stableReferences := stable.(store.UploadReferenceTransaction)
	if result, err := stableReferences.ReferencedUploadObjects(t.Context(), store.UploadReferenceRequest{
		Collections: []schema.Collection{media}, ObjectKeys: []string{"peer-new-object"},
	}); err != nil || len(result) != 0 {
		mongoRollback(t, stable)
		t.Fatalf("establish upload-reference snapshot = %#v, %v", result, err)
	}
	peerMutation := mongoBegin(t, backend, false)
	if _, err := peerMutation.Update(t.Context(), store.UpdateRequest{
		Request: store.Request{Collection: media, ID: target.ID},
		Values:  store.Values{"objectKey": store.String("peer-new-object")},
	}); err != nil {
		mongoRollback(t, peerMutation)
		mongoRollback(t, stable)
		t.Fatal(err)
	}
	mongoCommit(t, peerMutation)
	if result, err := stableReferences.ReferencedUploadObjects(t.Context(), store.UploadReferenceRequest{
		Collections: []schema.Collection{media}, ObjectKeys: []string{"peer-new-object"},
	}); err != nil || len(result) != 0 {
		mongoRollback(t, stable)
		t.Fatalf("stable upload-reference snapshot observed peer mutation = %#v, %v", result, err)
	}
	mongoRollback(t, stable)
	fresh := mongoBegin(t, backend, true)
	if result, err := fresh.(store.UploadReferenceTransaction).ReferencedUploadObjects(t.Context(), store.UploadReferenceRequest{
		Collections: []schema.Collection{media}, ObjectKeys: []string{"peer-new-object"},
	}); err != nil || !reflect.DeepEqual(result, []string{"peer-new-object"}) {
		mongoRollback(t, fresh)
		t.Fatalf("fresh upload-reference snapshot = %#v, %v", result, err)
	}
	mongoRollback(t, fresh)

	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range []struct {
		key  string
		body string
	}{
		{key: deliveryOriginal, body: "original"},
		{key: deliveryVariant, body: "variant"},
	} {
		if err := storageBackend.Put(t.Context(), object.key, strings.NewReader(object.body), int64(len(object.body)), "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	config := mongoUploadTestConfig()
	config.Storage = storageBackend
	config.StorageNamespace = "mongodb-upload-test"
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	objectKeyPath, _ := query.NewPath("objectKey")
	thumbObjectKeyPath, _ := query.NewPath("sizes", "thumb", "objectKey")
	for _, candidate := range []struct {
		name string
		path query.Path
		key  string
	}{
		{name: "original", path: objectKeyPath, key: "peer-new-object"},
		{name: "configured image size", path: thumbObjectKeyPath, key: "active-variant"},
	} {
		page, err := application.Local().List(t.Context(), "media", ridu.ListOptions{
			Where: query.Equal(candidate.path, query.String(candidate.key)), Limit: 10,
		})
		if err != nil {
			t.Fatalf("list upload by %s object key: %v", candidate.name, err)
		}
		if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != target.ID {
			t.Fatalf("upload %s object-key result = %#v", candidate.name, page)
		}
	}
	for _, candidate := range []struct {
		name string
		key  string
	}{
		{name: "original", key: deliveryOriginal},
		{name: "configured image size", key: deliveryVariant},
	} {
		reader, object, err := application.OpenUpload(t.Context(), "media", candidate.key, nil)
		if err != nil {
			t.Fatalf("open upload by %s object key: %v", candidate.name, err)
		}
		if object.Key != candidate.key {
			_ = reader.Close()
			t.Fatalf("opened upload %s object = %#v", candidate.name, object)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close upload %s object: %v", candidate.name, err)
		}
	}
	post, err := application.Local().Create(t.Context(), "posts", store.Values{
		"visibility": store.String("public"), "hero": store.String(target.ID),
		"content": store.Object(store.Values{
			"gallery": store.List(store.String(target.ID), store.String(target.ID)),
		}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("create upload references: %v", err)
	}
	heroPath, _ := query.NewPath("hero")
	galleryPath, _ := query.NewPath("content", "gallery")
	populated, err := application.Local().Find(t.Context(), "posts", post.ID, ridu.FindOptions{Populate: []query.Population{
		{Path: heroPath}, {Path: galleryPath},
	}})
	if err != nil {
		t.Fatalf("populate upload references: %v", err)
	}
	hero, valid := populated.Values["hero"].CopyDocument()
	if !valid || hero.ID != target.ID {
		t.Fatalf("populated upload = %#v", populated.Values["hero"])
	}
	content, _ := populated.Values["content"].CopyObject()
	gallery, valid := content["gallery"].CopyList()
	if !valid || len(gallery) != 2 {
		t.Fatalf("populated upload gallery = %#v", content["gallery"])
	}
	for _, item := range gallery {
		asset, populated := item.CopyDocument()
		if !populated || asset.ID != target.ID {
			t.Fatalf("populated duplicate upload = %#v", item)
		}
	}
	if _, err := application.Local().Create(t.Context(), "posts", store.Values{
		"visibility": store.String("private"), "hero": store.String(target.ID),
		"content": store.Object(store.Values{"gallery": store.List(store.String(target.ID))}),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("create access-filtered upload owner: %v", err)
	}
	corruptOwner, err := application.Local().Create(t.Context(), "posts", store.Values{
		"visibility": store.String("public"), "hero": store.String(otherTarget.ID),
		"content": store.Object(store.Values{"gallery": store.List(store.String(otherTarget.ID))}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("create corruptible upload owner: %v", err)
	}
	matching, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{
		Where: query.Equal(heroPath, query.String(target.ID)), Limit: 10,
	})
	if err != nil {
		t.Fatalf("list singular upload with access composition: %v", err)
	}
	if matching.Total != 1 || len(matching.Documents) != 1 || matching.Documents[0].ID != post.ID {
		t.Fatalf("singular upload filter/access result = %#v", matching)
	}
	if _, err := backend.database.Collection(physicalCollectionName(posts.ID)).UpdateOne(
		t.Context(),
		bson.D{{Key: "_id", Value: corruptOwner.ID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "values.content.gallery", Value: "not-a-list"}}}},
	); err != nil {
		t.Fatal(err)
	}
	corruptMatch, err := application.Local().List(t.Context(), "posts", ridu.ListOptions{
		Where: query.Equal(heroPath, query.String(otherTarget.ID)), Limit: 10,
	})
	if err != nil {
		t.Fatalf("list over corrupt nested upload owner: %v", err)
	}
	if corruptMatch.Total != 0 || len(corruptMatch.Documents) != 0 {
		t.Fatalf("corrupt nested has-many upload entered list result: %#v", corruptMatch)
	}

	if _, err := backend.database.Collection(physicalCollectionName(media.ID)).InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: "corrupt-upload"},
		{Key: "values", Value: bson.D{{Key: "objectKey", Value: "corrupt-object"}}},
	}); err != nil {
		t.Fatal(err)
	}
	corruptSnapshot := mongoBegin(t, backend, true)
	_, err = corruptSnapshot.(store.UploadReferenceTransaction).ReferencedUploadObjects(t.Context(), store.UploadReferenceRequest{
		Collections: []schema.Collection{media}, ObjectKeys: []string{"corrupt-object"},
	})
	if err == nil || !strings.Contains(err.Error(), "decode matching") {
		mongoRollback(t, corruptSnapshot)
		t.Fatalf("matching corrupt upload lookup error = %v", err)
	}
	mongoRollback(t, corruptSnapshot)

}

func TestMongoDBUploadObjectLocksRemainHeldReleaseAndFollowTransactionLifetime(t *testing.T) {
	backend := mongoIntegrationStore(t)
	media, _, manifest := mongoUploadTestSchema(t)
	if err := backend.SyncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	peer := mongoUploadPeerStore(t, backend.database.Name())
	if err := peer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []*Store{backend, peer} {
		candidate.uploadLockRetry = 10 * time.Millisecond
		candidate.uploadLockWait = 180 * time.Millisecond
	}
	abortOwner := strings.Repeat("1", 32)
	abortFence := strings.Repeat("2", 32)
	abortRecord := func(key string) bson.D {
		return encodeMongoUploadLock(key, abortOwner, abortFence)
	}
	callbackFailure := errors.New("force upload-lock callback rollback")
	if err := backend.runMongoUploadLockTransaction(t.Context(), func(sessionContext context.Context) error {
		if _, err := backend.database.Collection(mongoUploadLockCollectionName).InsertOne(sessionContext, abortRecord("callback-abort")); err != nil {
			return err
		}
		return callbackFailure
	}); !errors.Is(err, callbackFailure) {
		t.Fatalf("upload-lock callback failure = %v", err)
	}
	if count, err := backend.database.Collection(mongoUploadLockCollectionName).CountDocuments(t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID("callback-abort")}}); err != nil || count != 0 {
		t.Fatalf("callback-failed upload-lock transaction rows = %d, %v", count, err)
	}
	cancelledTransactionContext, cancelTransaction := context.WithCancel(t.Context())
	if err := backend.runMongoUploadLockTransaction(cancelledTransactionContext, func(sessionContext context.Context) error {
		if _, err := backend.database.Collection(mongoUploadLockCollectionName).InsertOne(sessionContext, abortRecord("cancelled-abort")); err != nil {
			return err
		}
		cancelTransaction()
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled upload-lock transaction = %v", err)
	}
	if count, err := backend.database.Collection(mongoUploadLockCollectionName).CountDocuments(t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID("cancelled-abort")}}); err != nil || count != 0 {
		t.Fatalf("cancelled upload-lock transaction rows = %d, %v", count, err)
	}

	release, err := backend.LockUploadObjects(t.Context(), []string{"object-b", "object-a", "object-a"})
	if err != nil {
		t.Fatal(err)
	}
	// Stay held beyond the former focused-test lease window. The non-expiring
	// row must not become available without exact release.
	time.Sleep(700 * time.Millisecond)
	started := time.Now()
	if contenderRelease, contenderErr := peer.LockUploadObjects(context.Background(), []string{"object-a"}); !errors.Is(contenderErr, context.DeadlineExceeded) {
		if contenderRelease != nil {
			contenderRelease()
		}
		release()
		t.Fatalf("held upload lock contender = %v, want bounded deadline", contenderErr)
	}
	if time.Since(started) > time.Second {
		release()
		t.Fatal("upload lock private wait bound was not enforced")
	}

	// Failed multi-key acquisition must not commit its earlier free key.
	if partialRelease, partialErr := peer.LockUploadObjects(context.Background(), []string{"free-object", "object-a"}); !errors.Is(partialErr, context.DeadlineExceeded) {
		if partialRelease != nil {
			partialRelease()
		}
		release()
		t.Fatalf("partially blocked acquisition = %v", partialErr)
	}
	freeRelease, err := peer.LockUploadObjects(t.Context(), []string{"free-object"})
	if err != nil {
		release()
		t.Fatalf("failed multi-key acquisition retained a partial lock: %v", err)
	}
	freeRelease()

	release()
	retryRelease, err := peer.LockUploadObjects(t.Context(), []string{"object-a"})
	if err != nil {
		t.Fatalf("upload lock did not release: %v", err)
	}
	retryRelease()

	concurrentRelease, err := backend.LockUploadObjects(t.Context(), []string{"concurrent-release"})
	if err != nil {
		t.Fatal(err)
	}
	const releaseCallers = 32
	var releaseWait sync.WaitGroup
	releaseWait.Add(releaseCallers)
	for range releaseCallers {
		go func() {
			defer releaseWait.Done()
			concurrentRelease()
		}()
	}
	releaseWait.Wait()
	afterConcurrentRelease, err := peer.LockUploadObjects(t.Context(), []string{"concurrent-release"})
	if err != nil {
		t.Fatalf("concurrent upload-lock release did not delete exact row: %v", err)
	}
	afterConcurrentRelease()

	singleReleaseHandle, err := backend.lockUploadObjects(t.Context(), []string{"single-release-retry"})
	if err != nil {
		t.Fatal(err)
	}
	singleReleaseDelete := singleReleaseHandle.deleteExact
	var singleReleaseAttempts atomic.Int32
	firstSingleReleaseDelete := make(chan struct{})
	allowSingleReleaseRetry := make(chan struct{})
	singleReleaseHandle.deleteExact = func() error {
		if singleReleaseAttempts.Add(1) == 1 {
			close(firstSingleReleaseDelete)
			<-allowSingleReleaseRetry
			return errors.New("injected first store-scoped lock delete failure")
		}
		return singleReleaseDelete()
	}
	singleReleaseDone := make(chan struct{})
	go func() {
		singleReleaseHandle.run()
		close(singleReleaseDone)
	}()
	select {
	case <-firstSingleReleaseDelete:
	case <-time.After(time.Second):
		close(allowSingleReleaseRetry)
		<-singleReleaseDone
		t.Fatal("single store-scoped release did not reach injected deletion")
	}
	if heldDuringSingleRelease, heldErr := peer.LockUploadObjects(context.Background(), []string{"single-release-retry"}); !errors.Is(heldErr, context.DeadlineExceeded) {
		if heldDuringSingleRelease != nil {
			heldDuringSingleRelease()
		}
		close(allowSingleReleaseRetry)
		<-singleReleaseDone
		t.Fatalf("failed store-scoped release admitted a peer before retry: %v", heldErr)
	}
	close(allowSingleReleaseRetry)
	select {
	case <-singleReleaseDone:
	case <-time.After(time.Second):
		t.Fatal("single store-scoped release did not complete its retry")
	}
	if singleReleaseAttempts.Load() != 2 {
		t.Fatalf("single store-scoped release delete attempts = %d", singleReleaseAttempts.Load())
	}
	afterSingleRelease, err := peer.LockUploadObjects(t.Context(), []string{"single-release-retry"})
	if err != nil {
		t.Fatalf("single store-scoped release retry stranded lock: %v", err)
	}
	afterSingleRelease()

	// A cancelled acquisition context does not own the successful lock; the
	// release closure remains usable after context expiry.
	acquireContext, cancelAcquire := context.WithCancel(context.Background())
	cancelSafeRelease, err := backend.LockUploadObjects(acquireContext, []string{"cancel-safe"})
	if err != nil {
		cancelAcquire()
		t.Fatal(err)
	}
	cancelAcquire()
	cancelSafeRelease()
	cancelRetry, err := peer.LockUploadObjects(t.Context(), []string{"cancel-safe"})
	if err != nil {
		t.Fatalf("release after acquisition context cancellation: %v", err)
	}
	cancelRetry()

	// Simulate a crash- or commit-unknown acquisition by committing a row
	// without returning a release handle. It never becomes available
	// automatically. Exact manual cleanup after quiescence permits a successor,
	// while repeating stale cleanup cannot delete that successor.
	oldOwner := strings.Repeat("c", 32)
	oldFence := strings.Repeat("d", 32)
	if err := backend.acquireMongoUploadLockSet(t.Context(), []string{"abandoned"}, oldOwner, oldFence); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if abandonedRelease, abandonedErr := peer.LockUploadObjects(context.Background(), []string{"abandoned"}); !errors.Is(abandonedErr, context.DeadlineExceeded) {
		if abandonedRelease != nil {
			abandonedRelease()
		}
		t.Fatalf("abandoned upload lock was taken over automatically: %v", abandonedErr)
	}
	if err := backend.deleteMongoUploadLockSet([]string{"abandoned"}, oldOwner, oldFence); err != nil {
		t.Fatal(err)
	}
	successorRelease, err := peer.LockUploadObjects(t.Context(), []string{"abandoned"})
	if err != nil {
		t.Fatalf("manual abandoned-row cleanup did not permit acquisition: %v", err)
	}
	if err := backend.deleteMongoUploadLockSet([]string{"abandoned"}, oldOwner, oldFence); err != nil {
		t.Fatal(err)
	}
	if staleContender, staleErr := backend.LockUploadObjects(context.Background(), []string{"abandoned"}); !errors.Is(staleErr, context.DeadlineExceeded) {
		if staleContender != nil {
			staleContender()
		}
		successorRelease()
		t.Fatalf("stale owner cleanup removed successor: %v", staleErr)
	}
	successorRelease()

	transaction := mongoBegin(t, backend, false)
	transactionLocker := transaction.(store.UploadObjectLocker)
	earlyRelease, err := transactionLocker.LockUploadObjects(t.Context(), []string{"transaction-object", "transaction-object"})
	if err != nil {
		mongoRollback(t, transaction)
		t.Fatal(err)
	}
	if _, err := transactionLocker.LockUploadObjects(t.Context(), []string{"transaction-object"}); err != nil {
		mongoRollback(t, transaction)
		t.Fatalf("transaction upload lock did not reenter: %v", err)
	}
	earlyRelease()
	if blockedRelease, blockedErr := peer.LockUploadObjects(context.Background(), []string{"transaction-object"}); !errors.Is(blockedErr, context.DeadlineExceeded) {
		if blockedRelease != nil {
			blockedRelease()
		}
		mongoRollback(t, transaction)
		t.Fatalf("transaction release function unlocked early: %v", blockedErr)
	}
	mongoRollback(t, transaction)
	afterRollback, err := peer.LockUploadObjects(t.Context(), []string{"transaction-object"})
	if err != nil {
		t.Fatalf("transaction rollback did not release upload lock: %v", err)
	}
	afterRollback()

	committed := mongoBegin(t, backend, false).(*documentTransaction)
	if _, err := committed.LockUploadObjects(t.Context(), []string{"commit-object"}); err != nil {
		mongoRollback(t, committed)
		t.Fatal(err)
	}
	mongoCommit(t, committed)
	afterCommit, err := peer.LockUploadObjects(t.Context(), []string{"commit-object"})
	if err != nil {
		t.Fatalf("transaction commit did not release upload lock: %v", err)
	}
	afterCommit()

	snapshotTiming := mongoBegin(t, backend, false).(*documentTransaction)
	if _, err := snapshotTiming.Find(t.Context(), store.Request{Collection: media, ID: "missing-snapshot-anchor"}); !errors.Is(err, store.ErrNotFound) {
		mongoRollback(t, snapshotTiming)
		t.Fatalf("establish transaction snapshot before upload lock = %v", err)
	}
	if _, err := snapshotTiming.LockUploadObjects(t.Context(), []string{"post-snapshot-lock"}); err != nil {
		mongoRollback(t, snapshotTiming)
		t.Fatal(err)
	}
	if err := snapshotTiming.Commit(t.Context()); err != nil {
		t.Fatalf("commit transaction that acquired upload lock after its snapshot: %v", err)
	}
	afterSnapshotCommit, err := peer.LockUploadObjects(t.Context(), []string{"post-snapshot-lock"})
	if err != nil {
		t.Fatalf("post-snapshot transaction commit stranded upload lock: %v", err)
	}
	afterSnapshotCommit()

	cancelled := mongoBegin(t, backend, false)
	if _, err := cancelled.(store.UploadObjectLocker).LockUploadObjects(t.Context(), []string{"cancelled-commit-object"}); err != nil {
		mongoRollback(t, cancelled)
		t.Fatal(err)
	}
	cancelledContext, cancelCommit := context.WithCancel(context.Background())
	cancelCommit()
	if err := cancelled.Commit(cancelledContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-commit cancellation error = %v", err)
	}
	afterCancelledCommit, err := peer.LockUploadObjects(t.Context(), []string{"cancelled-commit-object"})
	if err != nil {
		t.Fatalf("pre-commit cancellation did not release upload lock: %v", err)
	}
	afterCancelledCommit()

	rollbackFault := mongoBegin(t, backend, false).(*documentTransaction)
	if _, err := rollbackFault.LockUploadObjects(t.Context(), []string{"rollback-release-fault"}); err != nil {
		mongoRollback(t, rollbackFault)
		t.Fatal(err)
	}
	rollbackHandle := rollbackFault.uploadLockReleases[0]
	rollbackDelete := rollbackHandle.deleteExact
	var rollbackDeleteAttempts atomic.Int32
	firstRollbackDelete := make(chan struct{})
	allowRollbackRetry := make(chan struct{})
	rollbackHandle.deleteExact = func() error {
		if rollbackDeleteAttempts.Add(1) == 1 {
			close(firstRollbackDelete)
			<-allowRollbackRetry
			return errors.New("injected first rollback lock delete failure")
		}
		return rollbackDelete()
	}
	rollbackResult := make(chan error, 1)
	go func() {
		rollbackResult <- rollbackFault.Rollback(context.Background())
	}()
	select {
	case <-firstRollbackDelete:
	case <-time.After(time.Second):
		close(allowRollbackRetry)
		<-rollbackResult
		t.Fatal("transaction rollback did not reach injected lock deletion")
	}
	if heldDuringRetry, heldErr := peer.LockUploadObjects(context.Background(), []string{"rollback-release-fault"}); !errors.Is(heldErr, context.DeadlineExceeded) {
		if heldDuringRetry != nil {
			heldDuringRetry()
		}
		close(allowRollbackRetry)
		t.Fatalf("failed rollback cleanup admitted a peer before retry: %v", heldErr)
	}
	close(allowRollbackRetry)
	if err := <-rollbackResult; err != nil {
		t.Fatalf("transaction rollback release retry: %v", err)
	}
	if rollbackDeleteAttempts.Load() != 2 {
		t.Fatalf("transaction rollback delete attempts = %d", rollbackDeleteAttempts.Load())
	}
	afterRollbackFault, err := peer.LockUploadObjects(t.Context(), []string{"rollback-release-fault"})
	if err != nil {
		t.Fatalf("transaction rollback retry did not release lock: %v", err)
	}
	afterRollbackFault()

	commitFault := mongoBegin(t, backend, false).(*documentTransaction)
	if _, err := commitFault.LockUploadObjects(t.Context(), []string{"commit-release-fault"}); err != nil {
		mongoRollback(t, commitFault)
		t.Fatal(err)
	}
	commitHandle := commitFault.uploadLockReleases[0]
	commitDelete := commitHandle.deleteExact
	var commitDeleteAttempts atomic.Int32
	firstCommitDelete := make(chan struct{})
	allowCommitRetry := make(chan struct{})
	commitHandle.deleteExact = func() error {
		if commitDeleteAttempts.Add(1) == 1 {
			close(firstCommitDelete)
			<-allowCommitRetry
			return errors.New("injected first post-commit lock delete failure")
		}
		return commitDelete()
	}
	commitResult := make(chan error, 1)
	go func() {
		commitResult <- commitFault.Commit(context.Background())
	}()
	select {
	case <-firstCommitDelete:
	case <-time.After(time.Second):
		close(allowCommitRetry)
		<-commitResult
		t.Fatal("transaction commit did not reach injected lock deletion")
	}
	if heldDuringCommitRetry, heldErr := peer.LockUploadObjects(context.Background(), []string{"commit-release-fault"}); !errors.Is(heldErr, context.DeadlineExceeded) {
		if heldDuringCommitRetry != nil {
			heldDuringCommitRetry()
		}
		close(allowCommitRetry)
		t.Fatalf("failed commit cleanup admitted a peer before retry: %v", heldErr)
	}
	close(allowCommitRetry)
	if err := <-commitResult; err != nil {
		t.Fatalf("transaction commit release retry: %v", err)
	}
	if commitFault.state != transactionCommitted || commitFault.uploadLockCleanupErr != nil || commitDeleteAttempts.Load() != 2 {
		t.Fatalf("transaction commit cleanup state = %s, delete attempts = %d", commitFault.state, commitDeleteAttempts.Load())
	}
	afterCommitFault, err := peer.LockUploadObjects(t.Context(), []string{"commit-release-fault"})
	if err != nil {
		t.Fatalf("known commit cleanup retry stranded upload lock: %v", err)
	}
	afterCommitFault()

	// Finishing an explicitly unknown content commit must not issue a new
	// out-of-transaction delete. This simulated aborted outcome therefore leaves
	// the separately committed row held until manual reconciliation.
	unknownTransaction := mongoBegin(t, backend, false).(*documentTransaction)
	if _, err := unknownTransaction.LockUploadObjects(t.Context(), []string{"unknown-commit-object"}); err != nil {
		mongoRollback(t, unknownTransaction)
		t.Fatal(err)
	}
	unknownRaw, err := backend.database.Collection(mongoUploadLockCollectionName).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID("unknown-commit-object")}},
	).Raw()
	if err != nil {
		mongoRollback(t, unknownTransaction)
		t.Fatal(err)
	}
	unknownRecord, err := decodeMongoUploadLock(unknownRaw)
	if err != nil {
		mongoRollback(t, unknownTransaction)
		t.Fatal(err)
	}
	unknownTransaction.finishUnknownCommit(t.Context())
	if unknownTransaction.state != transactionCommitUnknown || unknownTransaction.uploadLockReleases != nil || unknownTransaction.uploadLockedKeys != nil {
		t.Fatalf("unknown commit transaction state = %s, releases=%#v keys=%#v", unknownTransaction.state, unknownTransaction.uploadLockReleases, unknownTransaction.uploadLockedKeys)
	}
	if unknownContender, unknownErr := peer.LockUploadObjects(context.Background(), []string{"unknown-commit-object"}); !errors.Is(unknownErr, context.DeadlineExceeded) {
		if unknownContender != nil {
			unknownContender()
		}
		t.Fatalf("unknown content commit released upload lock: %v", unknownErr)
	}
	if err := backend.deleteMongoUploadLockSet([]string{"unknown-commit-object"}, unknownRecord.Owner, unknownRecord.Fence); err != nil {
		t.Fatal(err)
	}
	afterUnknownReconciliation, err := peer.LockUploadObjects(t.Context(), []string{"unknown-commit-object"})
	if err != nil {
		t.Fatalf("manual unknown-commit reconciliation did not permit acquisition: %v", err)
	}
	afterUnknownReconciliation()

	snapshot := mongoBegin(t, backend, true)
	if _, err := snapshot.(store.UploadObjectLocker).LockUploadObjects(t.Context(), []string{"snapshot"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		mongoRollback(t, snapshot)
		t.Fatalf("snapshot upload lock error = %v", err)
	}
	mongoRollback(t, snapshot)

	activeCorruptKey := "corrupt-codec"
	activeOwner := strings.Repeat("e", 32)
	activeFence := strings.Repeat("f", 32)
	if _, err := backend.database.Collection(mongoUploadLockCollectionName).InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: mongoUploadLockID(activeCorruptKey)}, {Key: "codec", Value: int32(2)},
		{Key: "key", Value: activeCorruptKey}, {Key: "owner", Value: activeOwner}, {Key: "fence", Value: activeFence},
	}); err != nil {
		t.Fatal(err)
	}
	if corruptRelease, corruptErr := backend.LockUploadObjects(t.Context(), []string{activeCorruptKey}); corruptErr == nil {
		if corruptRelease != nil {
			corruptRelease()
		}
		t.Fatal("corrupt upload-lock codec row was accepted")
	}
	corruptRaw, err := backend.database.Collection(mongoUploadLockCollectionName).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID(activeCorruptKey)}},
	).Raw()
	if err != nil {
		t.Fatal(err)
	}
	if err := requireExactKeys(corruptRaw, "corrupt MongoDB upload lock", "_id", "codec", "key", "owner", "fence"); err != nil {
		t.Fatal(err)
	}
	codec, valid := corruptRaw.Lookup("codec").Int32OK()
	key, keyValid := corruptRaw.Lookup("key").StringValueOK()
	owner, ownerValid := corruptRaw.Lookup("owner").StringValueOK()
	fence, fenceValid := corruptRaw.Lookup("fence").StringValueOK()
	if !valid || codec != 2 || !keyValid || key != activeCorruptKey ||
		!ownerValid || owner != activeOwner || !fenceValid || fence != activeFence {
		t.Fatalf("failed acquisition mutated corrupt upload-lock row: %v", corruptRaw)
	}
	if err := backend.deleteMongoUploadLockSet([]string{activeCorruptKey}, activeOwner, activeFence); err != nil {
		t.Fatal(err)
	}

	closeBlockerRelease, err := backend.LockUploadObjects(t.Context(), []string{"close-blocker"})
	if err != nil {
		t.Fatal(err)
	}
	closeRacePeer := mongoUploadPeerStore(t, backend.database.Name())
	if err := closeRacePeer.VerifyIndexes(t.Context(), manifest); err != nil {
		closeBlockerRelease()
		t.Fatal(err)
	}
	closeRacePeer.uploadLockRetry = 10 * time.Millisecond
	closeRacePeer.uploadLockWait = 5 * time.Second
	type lockAttempt struct {
		release func()
		err     error
	}
	attemptResult := make(chan lockAttempt, 1)
	go func() {
		release, err := closeRacePeer.LockUploadObjects(context.Background(), []string{"close-race-free", "close-blocker"})
		attemptResult <- lockAttempt{release: release, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	const closeCallers = 4
	closeStart := make(chan struct{})
	closeResults := make(chan error, closeCallers)
	for range closeCallers {
		go func() {
			<-closeStart
			closeResults <- closeRacePeer.Close()
		}()
	}
	close(closeStart)
	for range closeCallers {
		select {
		case closeErr := <-closeResults:
			if closeErr != nil {
				closeBlockerRelease()
				t.Fatalf("concurrent MongoDB Close = %v", closeErr)
			}
		case <-time.After(3 * time.Second):
			closeBlockerRelease()
			t.Fatal("concurrent MongoDB Close did not wait for upload-lock shutdown")
		}
	}
	select {
	case attempt := <-attemptResult:
		if attempt.release != nil {
			attempt.release()
			closeBlockerRelease()
			t.Fatal("closing upload-lock acquisition unexpectedly succeeded")
		}
		if attempt.err == nil {
			closeBlockerRelease()
			t.Fatal("closing upload-lock acquisition returned no error")
		}
	case <-time.After(time.Second):
		closeBlockerRelease()
		t.Fatal("concurrent Close returned before its upload-lock acquisition exited")
	}
	if count, err := backend.database.Collection(mongoUploadLockCollectionName).CountDocuments(t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID("close-race-free")}}); err != nil || count != 0 {
		closeBlockerRelease()
		t.Fatalf("closing multi-key acquisition left a partial row = %d, %v", count, err)
	}
	closeBlockerRelease()

	// A successfully returned lock linearizes before Close and therefore stays
	// held. Release after Close is safe but cannot use a disconnected Store;
	// exact operator cleanup is required before another process may acquire it.
	closingPeer := mongoUploadPeerStore(t, backend.database.Name())
	if err := closingPeer.VerifyIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	closingPeer.uploadLockRetry = 10 * time.Millisecond
	closingPeer.uploadLockWait = 180 * time.Millisecond
	closeRelease, err := closingPeer.LockUploadObjects(t.Context(), []string{"close-held"})
	if err != nil {
		t.Fatal(err)
	}
	closeRecordRaw, err := backend.database.Collection(mongoUploadLockCollectionName).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID("close-held")}},
	).Raw()
	if err != nil {
		t.Fatal(err)
	}
	closeRecord, err := decodeMongoUploadLock(closeRecordRaw)
	if err != nil {
		t.Fatal(err)
	}
	closeRetryHandle, err := closingPeer.lockUploadObjects(t.Context(), []string{"close-release-retry"})
	if err != nil {
		t.Fatal(err)
	}
	closeRetryRaw, err := backend.database.Collection(mongoUploadLockCollectionName).FindOne(
		t.Context(), bson.D{{Key: "_id", Value: mongoUploadLockID("close-release-retry")}},
	).Raw()
	if err != nil {
		t.Fatal(err)
	}
	closeRetryRecord, err := decodeMongoUploadLock(closeRetryRaw)
	if err != nil {
		t.Fatal(err)
	}
	var closeRetryAttempts atomic.Int32
	closeRetryReached := make(chan struct{})
	closeRetryHandle.deleteExact = func() error {
		if closeRetryAttempts.Add(1) == 2 {
			close(closeRetryReached)
		}
		return errors.New("injected persistent store-scoped lock delete failure")
	}
	closeRetryDone := make(chan struct{})
	go func() {
		closeRetryHandle.run()
		close(closeRetryDone)
	}()
	select {
	case <-closeRetryReached:
	case <-time.After(time.Second):
		t.Fatal("store-scoped release did not retry before Close")
	}
	if err := closingPeer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closeRetryDone:
	case <-time.After(time.Second):
		t.Fatal("Store.Close did not stop a retrying store-scoped release")
	}
	closeRetryHandle.mu.Lock()
	closeRetryComplete := closeRetryHandle.complete
	closeRetryHandle.mu.Unlock()
	if closeRetryComplete {
		t.Fatal("Store.Close marked a failed store-scoped release complete")
	}
	closeRelease()
	if closedContender, closedErr := backend.LockUploadObjects(context.Background(), []string{"close-held"}); !errors.Is(closedErr, context.DeadlineExceeded) {
		if closedContender != nil {
			closedContender()
		}
		t.Fatalf("Store.Close weakened a successfully returned upload lock: %v", closedErr)
	}
	if err := backend.deleteMongoUploadLockSet([]string{"close-held"}, closeRecord.Owner, closeRecord.Fence); err != nil {
		t.Fatal(err)
	}
	if closedRetryContender, closedRetryErr := backend.LockUploadObjects(context.Background(), []string{"close-release-retry"}); !errors.Is(closedRetryErr, context.DeadlineExceeded) {
		if closedRetryContender != nil {
			closedRetryContender()
		}
		t.Fatalf("Store.Close weakened a retrying upload lock: %v", closedRetryErr)
	}
	if err := backend.deleteMongoUploadLockSet([]string{"close-release-retry"}, closeRetryRecord.Owner, closeRetryRecord.Fence); err != nil {
		t.Fatal(err)
	}
	recovered, err := backend.LockUploadObjects(t.Context(), []string{"close-held"})
	if err != nil {
		t.Fatalf("exact manual cleanup did not recover a closed-owner lock: %v", err)
	}
	recovered()
}

func mongoUploadPeerStore(t *testing.T, databaseName string) *Store {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	if databaseURL == "" {
		t.Skip("set RIDU_MONGODB_URL to run MongoDB integration tests")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	parsed.Path = "/" + databaseName
	backend, err := OpenWithConfig(t.Context(), Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("open MongoDB peer store: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close MongoDB peer store: %v", err)
		}
	})
	return backend
}
