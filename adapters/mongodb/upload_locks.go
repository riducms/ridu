package mongodb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	mongoUploadLockCollectionName = "z_ridu_upload_object_locks"
	defaultUploadLockRetry        = 25 * time.Millisecond
	defaultUploadLockWait         = 10 * time.Second
)

type mongoUploadLockRecord struct {
	ID    string
	Key   string
	Owner string
	Fence string
}

type mongoUploadLockBusy struct{}

func (mongoUploadLockBusy) Error() string { return "MongoDB upload object lock is busy" }

// mongoUploadLockCommitUnknown marks an acquisition whose commit was sent but
// could not be confirmed. Exact cleanup is unsafe in that state: the delete
// could observe no row before a late commit makes the non-expiring row visible.
// The row therefore stays fail-closed for operator reconciliation.
type mongoUploadLockCommitUnknown struct {
	cause error
}

func (mongoUploadLockCommitUnknown) Error() string {
	return "MongoDB upload lock acquisition outcome is unknown; quiesce upload work and reconcile private lock rows before retrying"
}

func (failure mongoUploadLockCommitUnknown) Unwrap() error {
	return failure.cause
}

func isMongoUploadLockCommitUnknown(err error) bool {
	var unknown mongoUploadLockCommitUnknown
	return errors.As(err, &unknown)
}

type mongoUploadLockRelease struct {
	mu          sync.Mutex
	complete    bool
	deleteExact func() error
	waitRetry   func() bool
}

func newMongoUploadLockRelease(deleteExact func() error, waitRetry func() bool) *mongoUploadLockRelease {
	return &mongoUploadLockRelease{deleteExact: deleteExact, waitRetry: waitRetry}
}

func (release *mongoUploadLockRelease) run() {
	_ = release.deleteUntilComplete()
}

func (release *mongoUploadLockRelease) deleteUntilComplete() error {
	release.mu.Lock()
	defer release.mu.Unlock()
	if release.complete {
		return nil
	}
	for {
		err := release.deleteExact()
		if err == nil {
			release.complete = true
			return nil
		}
		if release.waitRetry == nil || !release.waitRetry() {
			return err
		}
	}
}

// LockUploadObjects acquires one separately committed exact-owner lock set.
// Acquisition is all-or-none, and rows remain held until release. Graceful
// shutdown must drain callers before Store.Close; crash-abandoned rows require
// explicit operator recovery.
func (backend *Store) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	release, err := backend.lockUploadObjects(ctx, objectKeys)
	if err != nil {
		return nil, err
	}
	return release.run, nil
}

func (backend *Store) lockUploadObjects(ctx context.Context, objectKeys []string) (*mongoUploadLockRelease, error) {
	keys, err := normalizedMongoUploadLockKeys(objectKeys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return &mongoUploadLockRelease{complete: true}, nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB upload object lock context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if backend == nil || backend.client == nil || backend.database == nil {
		return nil, fmt.Errorf("MongoDB database is unavailable")
	}
	if err := backend.requireVerifiedUploadLockIndexes(); err != nil {
		return nil, err
	}
	lockContext := ctx
	cancelWait := func() {}
	if backend.uploadLockWait > 0 {
		lockContext, cancelWait = context.WithTimeout(ctx, backend.uploadLockWait)
	}
	defer cancelWait()
	lockContext, cancelOperation, finishOperation, err := backend.beginMongoUploadLockAcquisition(lockContext)
	if err != nil {
		return nil, err
	}
	defer cancelOperation()
	defer finishOperation()
	owner, err := newMongoUploadLockToken()
	if err != nil {
		return nil, err
	}
	fence, err := newMongoUploadLockToken()
	if err != nil {
		return nil, err
	}
	retry := backend.mongoUploadLockRetryInterval()
	for {
		err = backend.acquireMongoUploadLockSet(lockContext, keys, owner, fence)
		if err == nil {
			break
		}
		if isMongoUploadLockCommitUnknown(err) {
			// A late commit can make this non-expiring row visible after an
			// immediate delete observed nothing. Retain the row fail-closed and
			// require quiesced operator reconciliation.
			return nil, err
		}
		// A known failed transaction cannot own the requested set. Exact-owner
		// cleanup is still defensive against an earlier whole-transaction retry.
		if cleanupErr := backend.cleanupMongoUploadLockSet(keys, owner, fence); cleanupErr != nil {
			return nil, cleanupErr
		}
		var busy mongoUploadLockBusy
		if !errors.As(err, &busy) {
			return nil, err
		}
		timer := time.NewTimer(retry)
		select {
		case <-lockContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, lockContext.Err()
		case <-timer.C:
		}
	}

	if err := lockContext.Err(); err != nil {
		// Close may cancel the admitted acquisition immediately after commit.
		// Exact cleanup still runs while the acquisition is counted, so the
		// method never returns a successful lock that Close already won.
		if cleanupErr := backend.cleanupMongoUploadLockSet(keys, owner, fence); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, err
	}
	release := newMongoUploadLockRelease(func() error {
		return backend.deleteMongoUploadLockSet(keys, owner, fence)
	}, backend.waitForMongoUploadLockCleanupRetry)
	return release, nil
}

func normalizedMongoUploadLockKeys(objectKeys []string) ([]string, error) {
	unique := make(map[string]struct{}, len(objectKeys))
	for _, key := range objectKeys {
		if key == "" || !utf8.ValidString(key) || stringsContainNUL(key) {
			return nil, fmt.Errorf("upload object lock key is invalid")
		}
		unique[key] = struct{}{}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func newMongoUploadLockToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate MongoDB upload lock ownership: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func mongoUploadLockID(key string) string {
	digest := sha256.Sum256([]byte("ridu-upload-object-lock\x00" + key))
	return "z_upload_lock_" + hex.EncodeToString(digest[:])
}

func (backend *Store) mongoUploadLockRetryInterval() time.Duration {
	retry := backend.uploadLockRetry
	if retry <= 0 {
		retry = defaultUploadLockRetry
	}
	return retry
}

func (backend *Store) acquireMongoUploadLockSet(ctx context.Context, keys []string, owner, fence string) error {
	err := backend.runMongoUploadLockTransaction(ctx, func(sessionContext context.Context) error {
		for _, key := range keys {
			id := mongoUploadLockID(key)
			collection := backend.database.Collection(mongoUploadLockCollectionName)
			existing, findErr := collection.FindOne(sessionContext, bson.D{{Key: "_id", Value: id}}).Raw()
			switch {
			case errors.Is(findErr, mongo.ErrNoDocuments):
				if _, insertErr := collection.InsertOne(sessionContext, encodeMongoUploadLock(key, owner, fence)); insertErr != nil {
					return insertErr
				}
			case findErr != nil:
				return findErr
			default:
				record, decodeErr := decodeMongoUploadLock(existing)
				if decodeErr != nil {
					return decodeErr
				}
				if record.Key != key {
					return fmt.Errorf("MongoDB upload lock identity collision: %w", store.ErrConflict)
				}
				return mongoUploadLockBusy{}
			}
		}
		return nil
	})
	if err == nil {
		return nil
	}
	if isMongoUploadLockCommitUnknown(err) {
		return err
	}
	var busy mongoUploadLockBusy
	if errors.As(err, &busy) || errors.Is(err, store.ErrConflict) {
		return err
	}
	if mongo.IsDuplicateKeyError(err) {
		for _, key := range keys {
			raw, findErr := backend.database.Collection(mongoUploadLockCollectionName).FindOne(ctx, bson.D{{Key: "_id", Value: mongoUploadLockID(key)}}).Raw()
			if errors.Is(findErr, mongo.ErrNoDocuments) {
				continue
			}
			if findErr != nil {
				return translateMongoError(ctx, findErr)
			}
			record, decodeErr := decodeMongoUploadLock(raw)
			if decodeErr != nil {
				return decodeErr
			}
			if record.Key != key {
				return fmt.Errorf("MongoDB upload lock identity collision: %w", store.ErrConflict)
			}
		}
		return mongoUploadLockBusy{}
	}
	if hasMongoErrorLabel(err, transientTransactionErrorLabel) {
		return mongoUploadLockBusy{}
	}
	translated := translateMongoError(ctx, err)
	if isMongoConfirmedTransactionConflict(translated) {
		return mongoUploadLockBusy{}
	}
	return translated
}

// runMongoUploadLockTransaction is deliberately local to upload locks. It
// keeps every wire operation on the bounded caller context instead of using
// mongo.Session.WithTransaction, whose commit and abort paths detach caller
// cancellation. Whole-transaction retries remain owned by acquisition; only
// an unknown commit result retries that same commit.
func (backend *Store) runMongoUploadLockTransaction(ctx context.Context, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session, err := backend.client.StartSession()
	if err != nil {
		return err
	}
	defer func() {
		cleanupContext, cancel := mongoUploadLockCleanupContext(ctx)
		defer cancel()
		if session.TransactionRunning() {
			_ = session.AbortTransaction(mongo.NewSessionContext(cleanupContext, session))
		}
		session.EndSession(cleanupContext)
	}()
	transactionOptions := options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetReadPreference(readpref.Primary()).
		SetWriteConcern(writeconcern.Majority())
	if err := session.StartTransaction(transactionOptions); err != nil {
		return err
	}
	sessionContext := mongo.NewSessionContext(ctx, session)
	if err := operation(sessionContext); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	backoff := 10 * time.Millisecond
	for {
		backend.uploadLockLifecycleMu.Lock()
		lifecycleClosed := backend.uploadLockLifecycleCtx == nil || backend.uploadLockLifecycleCtx.Err() != nil
		backend.uploadLockLifecycleMu.Unlock()
		if lifecycleClosed {
			return fmt.Errorf("MongoDB database is closed")
		}
		err := session.CommitTransaction(sessionContext)
		if err == nil {
			return nil
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return mongoUploadLockCommitUnknown{cause: contextErr}
		}
		if hasMongoErrorLabel(err, unknownTransactionCommitResultLabel) {
			if isMaxTimeMSExpiredError(err) {
				return mongoUploadLockCommitUnknown{}
			}
		} else if hasMongoErrorLabel(err, transientTransactionErrorLabel) {
			return err
		} else if mongo.IsTimeout(err) {
			return mongoUploadLockCommitUnknown{}
		} else {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return mongoUploadLockCommitUnknown{cause: ctx.Err()}
		case <-timer.C:
		}
		if backoff < 250*time.Millisecond {
			backoff = min(backoff*2, 250*time.Millisecond)
		}
	}
}

func mongoUploadLockCleanupContext(context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultCloseTimeout)
}

func encodeMongoUploadLock(key, owner, fence string) bson.D {
	return bson.D{
		{Key: "_id", Value: mongoUploadLockID(key)},
		{Key: "codec", Value: int32(1)},
		{Key: "key", Value: key},
		{Key: "owner", Value: owner},
		{Key: "fence", Value: fence},
	}
}

func decodeMongoUploadLock(raw bson.Raw) (mongoUploadLockRecord, error) {
	if err := requireExactKeys(raw, "MongoDB upload lock", "_id", "codec", "key", "owner", "fence"); err != nil {
		return mongoUploadLockRecord{}, err
	}
	id, valid := raw.Lookup("_id").StringValueOK()
	if !valid {
		return mongoUploadLockRecord{}, fmt.Errorf("stored MongoDB upload lock has an invalid identity")
	}
	codec, valid := raw.Lookup("codec").Int32OK()
	if !valid || codec != 1 {
		return mongoUploadLockRecord{}, fmt.Errorf("stored MongoDB upload lock has an unsupported codec version")
	}
	key, valid := raw.Lookup("key").StringValueOK()
	if !valid || key == "" || !utf8.ValidString(key) || stringsContainNUL(key) {
		return mongoUploadLockRecord{}, fmt.Errorf("stored MongoDB upload lock has an invalid key")
	}
	if id != mongoUploadLockID(key) {
		return mongoUploadLockRecord{}, fmt.Errorf("stored MongoDB upload lock has an invalid deterministic identity: %w", store.ErrConflict)
	}
	owner, valid := raw.Lookup("owner").StringValueOK()
	if !valid || !validMongoUploadLockToken(owner) {
		return mongoUploadLockRecord{}, fmt.Errorf("stored MongoDB upload lock has invalid ownership")
	}
	fence, valid := raw.Lookup("fence").StringValueOK()
	if !valid || !validMongoUploadLockToken(fence) {
		return mongoUploadLockRecord{}, fmt.Errorf("stored MongoDB upload lock has an invalid fence")
	}
	return mongoUploadLockRecord{ID: id, Key: key, Owner: owner, Fence: fence}, nil
}

func validMongoUploadLockToken(value string) bool {
	if len(value) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func (backend *Store) deleteMongoUploadLockSet(keys []string, owner, fence string) error {
	finishOperation, err := backend.beginMongoUploadLockCleanup()
	if err != nil {
		return err
	}
	defer finishOperation()
	return backend.cleanupMongoUploadLockSet(keys, owner, fence)
}

func (backend *Store) cleanupMongoUploadLockSet(keys []string, owner, fence string) error {
	cleanupContext, cancelCleanup := mongoUploadLockCleanupContext(context.Background())
	defer cancelCleanup()
	return backend.deleteMongoUploadLockSetInContext(cleanupContext, keys, owner, fence)
}

func (backend *Store) deleteMongoUploadLockSetInContext(ctx context.Context, keys []string, owner, fence string) error {
	ids := make(bson.A, len(keys))
	for index, key := range keys {
		ids[index] = mongoUploadLockID(key)
	}
	if _, err := backend.database.Collection(mongoUploadLockCollectionName).DeleteMany(ctx, bson.D{
		{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}},
		{Key: "owner", Value: owner},
		{Key: "fence", Value: fence},
	}); err != nil {
		return fmt.Errorf("MongoDB upload lock cleanup failed")
	}
	return nil
}

func (backend *Store) beginMongoUploadLockAcquisition(ctx context.Context) (context.Context, context.CancelFunc, func(), error) {
	backend.closeMu.Lock()
	defer backend.closeMu.Unlock()
	if backend.closed {
		return nil, nil, nil, fmt.Errorf("MongoDB database is closed")
	}
	return backend.beginMongoUploadLockOperation(ctx)
}

func (backend *Store) beginMongoUploadLockOperation(ctx context.Context) (context.Context, context.CancelFunc, func(), error) {
	backend.uploadLockLifecycleMu.Lock()
	defer backend.uploadLockLifecycleMu.Unlock()
	if backend.uploadLockLifecycleCtx == nil || backend.uploadLockLifecycleCtx.Err() != nil {
		return nil, nil, nil, fmt.Errorf("MongoDB database is closed")
	}
	operationContext, cancel := context.WithCancel(ctx)
	stopLifecycle := context.AfterFunc(backend.uploadLockLifecycleCtx, cancel)
	backend.uploadLockOperations.Add(1)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			stopLifecycle()
			cancel()
			backend.uploadLockOperations.Done()
		})
	}
	return operationContext, cancel, finish, nil
}

func (backend *Store) beginMongoUploadLockCleanup() (func(), error) {
	backend.uploadLockLifecycleMu.Lock()
	defer backend.uploadLockLifecycleMu.Unlock()
	if backend.uploadLockLifecycleCtx == nil || backend.uploadLockLifecycleCtx.Err() != nil {
		return nil, fmt.Errorf("MongoDB database is closed")
	}
	backend.uploadLockOperations.Add(1)
	var once sync.Once
	return func() {
		once.Do(backend.uploadLockOperations.Done)
	}, nil
}

func (backend *Store) stopMongoUploadLockOperations() {
	backend.uploadLockLifecycleMu.Lock()
	if backend.uploadLockLifecycleCancel != nil {
		backend.uploadLockLifecycleCancel()
	}
	backend.uploadLockLifecycleMu.Unlock()
	backend.uploadLockOperations.Wait()
}

func (backend *Store) waitForMongoUploadLockCleanupRetry() bool {
	if backend == nil {
		return false
	}
	backend.uploadLockLifecycleMu.Lock()
	lifecycle := backend.uploadLockLifecycleCtx
	backend.uploadLockLifecycleMu.Unlock()
	if lifecycle == nil || lifecycle.Err() != nil {
		return false
	}
	timer := time.NewTimer(backend.mongoUploadLockRetryInterval())
	defer timer.Stop()
	select {
	case <-lifecycle.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Transaction-scoped upload locks use the Store lock collection so they are
// visible before object-storage work. Returned releases are deliberately
// inert; terminal commit/rollback owns every acquired handle.
func (transaction *documentTransaction) LockUploadObjects(ctx context.Context, objectKeys []string) (func(), error) {
	keys, err := normalizedMongoUploadLockKeys(objectKeys)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB upload object lock context is required")
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if transaction.session == nil || transaction.state != transactionOpen {
		return nil, fmt.Errorf("MongoDB transaction is not open (state %s)", transaction.state)
	}
	if transaction.readOnly {
		return nil, fmt.Errorf("MongoDB snapshot transaction is read-only")
	}
	if len(keys) == 0 {
		return func() {}, nil
	}
	if transaction.uploadLockedKeys == nil {
		transaction.uploadLockedKeys = make(map[string]struct{})
	}
	newKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, held := transaction.uploadLockedKeys[key]; !held {
			newKeys = append(newKeys, key)
		}
	}
	if len(newKeys) == 0 {
		return func() {}, nil
	}
	release, err := transaction.store.lockUploadObjects(ctx, newKeys)
	if err != nil {
		return nil, err
	}
	transaction.uploadLockReleases = append(transaction.uploadLockReleases, release)
	for _, key := range newKeys {
		transaction.uploadLockedKeys[key] = struct{}{}
	}
	return func() {}, nil
}

func (transaction *documentTransaction) releaseUploadObjectLocks() error {
	pending := append([]*mongoUploadLockRelease(nil), transaction.uploadLockReleases...)
	failed := make([]*mongoUploadLockRelease, 0, len(pending))
	for index := len(pending) - 1; index >= 0; index-- {
		if err := pending[index].deleteUntilComplete(); err != nil {
			failed = append(failed, pending[index])
		}
	}
	if len(failed) != 0 {
		transaction.uploadLockReleases = failed
		return fmt.Errorf("MongoDB transaction upload lock cleanup stopped with the database lifecycle; manual reconciliation is required")
	}
	transaction.uploadLockReleases = nil
	transaction.uploadLockedKeys = nil
	return nil
}

// abandonUploadObjectLocks deliberately drops the local release handles
// without deleting their separately committed rows. When the content commit
// is explicitly unknown, releasing those rows could admit conflicting object
// work before the caller has reconciled the document outcome.
func (transaction *documentTransaction) abandonUploadObjectLocks() {
	transaction.uploadLockReleases = nil
	transaction.uploadLockedKeys = nil
}

var _ store.UploadObjectLocker = (*Store)(nil)
var _ store.UploadObjectLocker = (*documentTransaction)(nil)
