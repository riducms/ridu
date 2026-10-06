package mongodb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// MongoDB has no row lock. A transaction that writes a document another open
// transaction wrote, or one committed after its snapshot, fails with
// WriteConflict and the server aborts it, so the adapter builds its locks from
// writes, called fences:
//
//   - An exclusive lock (LockMutation, and any write to an existing document)
//     increments the fence on the document's working head and every one of
//     the document's shared fences.
//   - A shared lock (LockReference) increments one shared fence: a small
//     record keyed by the document and a slot. Each open transaction of a
//     Store uses its own slot, so saves that reference one popular target,
//     such as an image used by many pages, write different records and do not
//     conflict, as PostgreSQL's FOR SHARE lets them proceed together.
//
// An exclusive lock still conflicts with every shared lock. It increments the
// shared fences its snapshot sees, and a transaction that creates a shared
// fence also increments the working-head fence, so a shared fence created
// after that snapshot conflicts on the working head instead. The shared
// fences outlive the lock, so later references increment their existing
// records rather than each creating one and taking turns on the working head;
// a document keeps at most one per slot, and they are removed with it.
// Slots are only an in-process optimization: two replicas, or more open
// transactions than slots, can share a slot, and then take turns on it like
// any other conflicting lock.
//
// The operation engine, which owns hooks, cannot replay a transaction. Before
// a transaction's first content write, however, the adapter can: it has only
// read and held fences. On a conflict it aborts the server transaction,
// waits, starts another on a newer snapshot, retakes the locks it held in
// their original order, and proves each locked document is byte-for-byte
// unchanged apart from its fence, so every document the engine already
// received remains exactly what it locked. A changed document is a genuine
// conflict. The engine's earlier unlocked reads keep their older snapshot,
// which is the read-committed visibility a PostgreSQL write transaction gives
// each statement.
//
// A Store's own transactions also wait in its mongoLockQueue (lock_queue.go):
// a conflict with one of them waits for that transaction instead of sleeping,
// and a waiting exclusive lock holds back the shared locks of younger
// transactions, so that a stream of references cannot starve it. A conflict
// with another Store sleeps a jittered backoff before each retry.
const (
	mongoReferenceFenceCollectionName = "z_ridu_reference_fences"
	// mongoReferenceFenceSlots bounds the shared fences of one document.
	mongoReferenceFenceSlots = 32

	mongoLockWaitBudget = 10 * time.Second
	mongoLockMinBackoff = 2 * time.Millisecond
	mongoLockMaxBackoff = 100 * time.Millisecond
)

type mongoLockMode uint8

const (
	mongoLockShared mongoLockMode = iota + 1
	mongoLockExclusive
)

// mongoFenceTarget is a lockable document. Its working and live heads share
// one lock.
type mongoFenceTarget struct {
	collectionID schema.StableID
	documentID   string
}

// mongoHeldLock is one lock this transaction holds for the operation engine.
type mongoHeldLock struct {
	mode   mongoLockMode
	target mongoFenceTarget
	// working holds the working head, whose fence orders exclusive locks.
	working *mongo.Collection
	// collection and predicate select the document the engine read, in its
	// working or live head; document is the read, kept for a restart.
	collection *mongo.Collection
	predicate  bson.D
	document   bson.Raw
}

// lockDocument reads the document lock.predicate selects under the lock's
// mode and returns it. Before the transaction's first content write, a lock
// that conflicts with a lock of this Store's lock queue, or a transient
// conflict in the database, waits and retries on a restarted transaction
// until the lock wait budget or ctx ends; the conflict is then reported. It
// returns mongo.ErrNoDocuments, unlocked, when nothing matches.
func (transaction *documentTransaction) lockDocument(ctx context.Context, lock mongoHeldLock) (bson.Raw, error) {
	deadline := time.Now().Add(mongoLockWaitBudget)
	backoff := mongoLockMinBackoff
	queue := &transaction.store.lockQueue
	if transaction.canRestart() && lock.mode == mongoLockExclusive && queue.enqueue(transaction, lock.target) {
		defer queue.dequeue(transaction, lock.target)
	}
	// conflict is the last conflict this lock met; nil while it has only
	// waited in the queue.
	var conflict error
	for {
		if transaction.canRestart() {
			changed, expires, ready := queue.turn(transaction, lock.target, lock.mode, time.Now())
			if !ready {
				// A waiter gives up its fences, so nothing waits for it in
				// the database.
				transaction.suspend(ctx)
				if !mongoAwaitTurn(ctx, changed, expires, deadline) {
					return nil, mongoLockWaitError(ctx, conflict)
				}
				continue
			}
		}
		if transaction.suspended {
			if err := transaction.restart(ctx); err != nil {
				if !transaction.restartable(err) {
					return nil, err
				}
				conflict = err
				transaction.suspend(ctx)
				if !mongoWaitToRetry(ctx, &backoff, deadline) {
					return nil, mongoLockWaitError(ctx, conflict)
				}
				continue
			}
		}
		raw, err := transaction.takeLock(ctx, lock)
		if err == nil {
			lock.document = append(bson.Raw(nil), raw...)
			transaction.locks = append(transaction.locks, lock)
			return raw, nil
		}
		if !transaction.restartable(err) {
			return nil, err
		}
		conflict = err
		transaction.suspend(ctx)
		// A holder in this Store's queue is waited for at the top of the
		// loop; any other conflict sleeps first.
		if _, _, ready := queue.turn(transaction, lock.target, lock.mode, time.Now()); ready {
			if !mongoWaitToRetry(ctx, &backoff, deadline) {
				return nil, mongoLockWaitError(ctx, conflict)
			}
		}
	}
}

// mongoLockWaitError reports a lock whose wait ended without it: the caller's
// context error, the last database conflict, or a conflict with a lock of
// this Store that outlasted the wait budget.
func mongoLockWaitError(ctx context.Context, conflict error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if conflict != nil {
		return conflict
	}
	return mongoConfirmedTransactionConflict{}
}

// takeLock takes one lock in the current server transaction and returns the
// locked read.
func (transaction *documentTransaction) takeLock(ctx context.Context, lock mongoHeldLock) (bson.Raw, error) {
	if lock.mode == mongoLockExclusive {
		raw, err := lock.collection.FindOneAndUpdate(
			ctx,
			lock.predicate,
			bson.D{{Key: "$inc", Value: bson.D{{Key: mongoFencePath, Value: int64(1)}}}},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		).Raw()
		if err != nil {
			return nil, err
		}
		if lock.collection.Name() != lock.working.Name() {
			if err := transaction.fenceWorkingHead(ctx, lock.working, lock.target); err != nil {
				return nil, err
			}
		}
		if err := transaction.fenceSharedFences(ctx, lock.target); err != nil {
			return nil, err
		}
		transaction.holdExclusive(lock.target)
		return raw, nil
	}
	raw, err := lock.collection.FindOne(ctx, lock.predicate).Raw()
	if err != nil {
		return nil, err
	}
	if _, held := transaction.exclusive[lock.target]; held {
		return raw, nil
	}
	if err := transaction.takeSharedFence(ctx, lock.working, lock.target); err != nil {
		return nil, err
	}
	transaction.queueHold(lock.target, mongoLockShared)
	return raw, nil
}

// takeSharedFence increments this transaction's shared fence on target. The
// first transaction to use a slot creates its record and, so that an
// exclusive lock whose snapshot cannot see that record still conflicts with
// it, increments the working-head fence as well.
func (transaction *documentTransaction) takeSharedFence(ctx context.Context, working *mongo.Collection, target mongoFenceTarget) error {
	slot := transaction.fenceSlot()
	id := mongoReferenceFenceID(target, slot)
	fences := transaction.store.referenceFenceCollection()
	result, err := fences.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 1 {
		return nil
	}
	if err := transaction.fenceWorkingHead(ctx, working, target); err != nil {
		return err
	}
	_, err = fences.InsertOne(ctx, bson.D{
		{Key: "_id", Value: id},
		{Key: "collection", Value: string(target.collectionID)},
		{Key: "document", Value: target.documentID},
		{Key: "slot", Value: int32(slot)},
		{Key: "fence", Value: int64(1)},
	})
	return err
}

// fenceWorkingHead increments the fence on target's working head.
func (transaction *documentTransaction) fenceWorkingHead(ctx context.Context, working *mongo.Collection, target mongoFenceTarget) error {
	result, err := working.UpdateOne(ctx,
		mongoAnd([]bson.D{{{Key: mongoIDPath, Value: target.documentID}}, mongoTypeGuard(mongoFencePath, "long")}),
		bson.D{{Key: "$inc", Value: bson.D{{Key: mongoFencePath, Value: int64(1)}}}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 1 {
		return nil
	}
	raw, err := working.FindOne(ctx, bson.D{{Key: mongoIDPath, Value: target.documentID}}).Raw()
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return mongoLockStateError{fmt.Errorf("stored MongoDB document %q of collection %q has a live head without a working head", target.documentID, target.collectionID)}
	case err != nil:
		return err
	}
	if _, decodeErr := decodeDocument(raw); decodeErr != nil {
		return mongoLockStateError{decodeErr}
	}
	return mongoLockStateError{fmt.Errorf("stored MongoDB document %q of collection %q has invalid fence metadata", target.documentID, target.collectionID)}
}

// fenceSharedFences increments every shared fence of target, which conflicts
// with every transaction holding one.
func (transaction *documentTransaction) fenceSharedFences(ctx context.Context, target mongoFenceTarget) error {
	_, err := transaction.store.referenceFenceCollection().UpdateMany(ctx, mongoReferenceFenceFilter(target),
		bson.D{{Key: "$inc", Value: bson.D{{Key: "fence", Value: int64(1)}}}})
	return err
}

// deleteSharedFences removes the shared fences of a deleted document, which
// conflicts with every transaction holding one.
func (transaction *documentTransaction) deleteSharedFences(ctx context.Context, target mongoFenceTarget) error {
	_, err := transaction.store.referenceFenceCollection().DeleteMany(ctx, mongoReferenceFenceFilter(target))
	return err
}

func mongoReferenceFenceFilter(target mongoFenceTarget) bson.D {
	return bson.D{{Key: "collection", Value: string(target.collectionID)}, {Key: "document", Value: target.documentID}}
}

// lockForWrite takes the exclusive lock on a document this transaction is
// about to write, unless it already holds it. The write itself must follow in
// the same transaction.
func (transaction *documentTransaction) lockForWrite(ctx context.Context, collection schema.Collection, documentID string) error {
	target := mongoFenceTarget{collectionID: collection.ID, documentID: documentID}
	if _, held := transaction.exclusive[target]; held {
		return nil
	}
	if _, err := transaction.collection(collection).UpdateOne(ctx,
		mongoAnd([]bson.D{{{Key: mongoIDPath, Value: documentID}}, mongoTypeGuard(mongoFencePath, "long")}),
		bson.D{{Key: "$inc", Value: bson.D{{Key: mongoFencePath, Value: int64(1)}}}}); err != nil {
		return translateMongoError(ctx, err)
	}
	if err := transaction.fenceSharedFences(ctx, target); err != nil {
		return translateMongoError(ctx, err)
	}
	transaction.holdExclusive(target)
	return nil
}

func (transaction *documentTransaction) holdExclusive(target mongoFenceTarget) {
	if transaction.exclusive == nil {
		transaction.exclusive = make(map[mongoFenceTarget]struct{})
	}
	transaction.exclusive[target] = struct{}{}
	transaction.queueHold(target, mongoLockExclusive)
}

// queueHold records a fence the current server transaction holds in the
// Store's lock queue. The hold waits while the transaction is suspended and
// is released when it ends.
func (transaction *documentTransaction) queueHold(target mongoFenceTarget, mode mongoLockMode) {
	if !transaction.operation || transaction.store == nil {
		return
	}
	if transaction.store.lockQueue.hold(transaction, target, mode, transaction.serverStarted.Add(mongoStatementTimeLimit)) {
		transaction.queueHolds = append(transaction.queueHolds, target)
	}
}

// releaseQueuedHolds removes the transaction's holds from the Store's lock
// queue once it has ended.
func (transaction *documentTransaction) releaseQueuedHolds() {
	if len(transaction.queueHolds) != 0 {
		transaction.store.lockQueue.release(transaction, transaction.queueHolds)
		transaction.queueHolds = transaction.queueHolds[:0]
	}
}

// releaseLocks returns the fence slot and the queued holds once the
// transaction has ended.
func (transaction *documentTransaction) releaseLocks() {
	transaction.releaseFenceSlot()
	transaction.releaseQueuedHolds()
}

// canRestart reports whether the transaction is still before its first
// content write, where a restart can replace its server transaction.
func (transaction *documentTransaction) canRestart() bool {
	return transaction.operation && !transaction.wrote && !transaction.readOnly
}

// restartable reports a transient transaction error that the transaction
// can still absorb by restarting.
func (transaction *documentTransaction) restartable(err error) bool {
	return transaction.canRestart() && hasMongoErrorLabel(err, transientTransactionErrorLabel)
}

// suspend aborts the server transaction, giving up every fence it holds,
// until restart. Its holds in the Store's lock queue wait meanwhile, and no
// statement may run in the transaction; see enterFor.
func (transaction *documentTransaction) suspend(ctx context.Context) {
	if transaction.suspended {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
	// After a conflict the server already aborted the transaction; this
	// only resets the session's transaction state.
	_ = transaction.session.AbortTransaction(mongo.NewSessionContext(cleanupContext, transaction.session))
	cancel()
	transaction.suspended = true
	clear(transaction.exclusive)
	if len(transaction.queueHolds) != 0 {
		transaction.store.lockQueue.wait(transaction, transaction.queueHolds)
	}
}

// restart replaces the server transaction and retakes every held lock on the
// new snapshot. A locked document that changed meanwhile, or that no longer
// matches its lock predicate, is ErrConflict.
func (transaction *documentTransaction) restart(ctx context.Context) error {
	transaction.suspend(ctx)
	if err := transaction.session.StartTransaction(options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetReadPreference(readpref.Primary()).
		SetWriteConcern(writeconcern.Majority())); err != nil {
		return translateMongoError(ctx, err)
	}
	transaction.suspended = false
	transaction.serverStarted = time.Now()
	for _, lock := range transaction.locks {
		raw, err := transaction.takeLock(ctx, lock)
		switch {
		case errors.Is(err, mongo.ErrNoDocuments):
			return store.ErrConflict
		case err != nil:
			if transaction.restartable(err) {
				return err
			}
			return translateLockError(ctx, err)
		}
		if !mongoSameDocumentExceptFence(lock.document, raw) {
			return store.ErrConflict
		}
	}
	return nil
}

// mongoLockStateError is stored state a lock refused, diagnosed by the
// adapter rather than reported by the driver.
type mongoLockStateError struct{ err error }

func (err mongoLockStateError) Error() string { return err.err.Error() }
func (err mongoLockStateError) Unwrap() error { return err.err }

// translateLockError translates a lock failure, keeping the adapter's own
// diagnosis of stored state.
func translateLockError(ctx context.Context, err error) error {
	var state mongoLockStateError
	if errors.As(err, &state) {
		return state.err
	}
	return translateMongoError(ctx, err)
}

// mongoWaitToRetry sleeps a jittered exponential backoff, reporting false
// without sleeping past deadline or ctx.
func mongoWaitToRetry(ctx context.Context, backoff *time.Duration, deadline time.Time) bool {
	wait := *backoff/2 + rand.N(*backoff/2+1)
	if time.Now().Add(wait).After(deadline) {
		return false
	}
	*backoff = min(*backoff*2, mongoLockMaxBackoff)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// mongoSameDocumentExceptFence compares two stored documents apart from the
// lock fence, which every lock acquisition increments.
func mongoSameDocumentExceptFence(left, right bson.Raw) bool {
	leftElements, leftErr := left.Elements()
	rightElements, rightErr := right.Elements()
	if leftErr != nil || rightErr != nil || len(leftElements) != len(rightElements) {
		return false
	}
	for index := range leftElements {
		leftElement, rightElement := leftElements[index], rightElements[index]
		if leftElement.Key() != rightElement.Key() {
			return false
		}
		if leftElement.Key() != "meta" {
			if !bytes.Equal(leftElement, rightElement) {
				return false
			}
			continue
		}
		leftMeta, leftOK := leftElement.Value().DocumentOK()
		rightMeta, rightOK := rightElement.Value().DocumentOK()
		if !leftOK || !rightOK || !mongoSameMetadataExceptFence(leftMeta, rightMeta) {
			return false
		}
	}
	return true
}

func mongoSameMetadataExceptFence(left, right bson.Raw) bool {
	leftElements, leftErr := left.Elements()
	rightElements, rightErr := right.Elements()
	if leftErr != nil || rightErr != nil || len(leftElements) != len(rightElements) {
		return false
	}
	for index := range leftElements {
		if leftElements[index].Key() != rightElements[index].Key() {
			return false
		}
		if leftElements[index].Key() != "fence" && !bytes.Equal(leftElements[index], rightElements[index]) {
			return false
		}
	}
	return true
}

// mongoFenceSlots assigns each open transaction of a Store that takes a shared
// lock its own slot while one is free.
type mongoFenceSlots struct {
	mu sync.Mutex
	// first is where the search starts, chosen at random per Store so that
	// replicas tend to use different slots.
	first   int
	holders [mongoReferenceFenceSlots]int
}

// acquire returns the first least-used slot from first.
func (slots *mongoFenceSlots) acquire() int {
	slots.mu.Lock()
	defer slots.mu.Unlock()
	best := slots.first
	for offset := range mongoReferenceFenceSlots {
		slot := (slots.first + offset) % mongoReferenceFenceSlots
		if slots.holders[slot] < slots.holders[best] {
			best = slot
		}
		if slots.holders[best] == 0 {
			break
		}
	}
	slots.holders[best]++
	return best
}

func (slots *mongoFenceSlots) release(slot int) {
	slots.mu.Lock()
	slots.holders[slot]--
	slots.mu.Unlock()
}

// fenceSlot returns this transaction's slot, assigning one on first use.
func (transaction *documentTransaction) fenceSlot() int {
	if !transaction.slotHeld {
		transaction.slot, transaction.slotHeld = transaction.store.fenceSlots.acquire(), true
	}
	return transaction.slot
}

// releaseFenceSlot returns the slot once the transaction has ended.
func (transaction *documentTransaction) releaseFenceSlot() {
	if transaction.slotHeld {
		transaction.store.fenceSlots.release(transaction.slot)
		transaction.slotHeld = false
	}
}

func mongoReferenceFenceID(target mongoFenceTarget, slot int) string {
	return mongoSystemRecordID("reference_fence", string(target.collectionID), target.documentID, strconv.Itoa(slot))
}

func (backend *Store) referenceFenceCollection() *mongo.Collection {
	return backend.database.Collection(mongoReferenceFenceCollectionName)
}
