package mongodb

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// mongoQueueTransactions returns transactions begun in order by queue.
func mongoQueueTransactions(queue *mongoLockQueue, count int) []*documentTransaction {
	transactions := make([]*documentTransaction, count)
	for index := range transactions {
		transactions[index] = &documentTransaction{age: queue.begin()}
	}
	return transactions
}

// A waiting exclusive request waits only for the holders it found and older
// transactions: a younger shared request waits behind it, and it is ready
// once those holders leave.
func TestMongoLockQueuePrefersAWaitingExclusiveRequest(t *testing.T) {
	var queue mongoLockQueue
	target := mongoFenceTarget{collectionID: "media", documentID: "a"}
	now := time.Now()
	expires := now.Add(time.Minute)
	transactions := mongoQueueTransactions(&queue, 4)
	older, holder, writer, later := transactions[0], transactions[1], transactions[2], transactions[3]

	if _, _, ready := queue.turn(holder, target, mongoLockShared, now); !ready {
		t.Fatal("shared request on an unlocked document is not ready")
	}
	queue.hold(holder, target, mongoLockShared, expires)
	if !queue.enqueue(writer, target) {
		t.Fatal("exclusive request was not queued")
	}
	changed, _, ready := queue.turn(writer, target, mongoLockExclusive, now)
	if ready {
		t.Fatal("exclusive request is ready while a shared holder remains")
	}
	if _, _, ready := queue.turn(later, target, mongoLockShared, now); ready {
		t.Fatal("younger shared request passed a waiting exclusive request")
	}
	if _, _, ready := queue.turn(older, target, mongoLockShared, now); !ready {
		t.Fatal("a shared request older than the waiting exclusive request waits behind it")
	}
	if _, _, ready := queue.turn(holder, target, mongoLockShared, now); !ready {
		t.Fatal("a holder's repeated shared request waits behind a request that waits for it")
	}

	queue.release(holder, []mongoFenceTarget{target})
	select {
	case <-changed:
	default:
		t.Fatal("releasing the holder did not wake the exclusive request")
	}
	if _, _, ready := queue.turn(writer, target, mongoLockExclusive, now); !ready {
		t.Fatal("exclusive request is not ready after the holder left")
	}
	queue.hold(writer, target, mongoLockExclusive, expires)
	queue.dequeue(writer, target)
	if _, _, ready := queue.turn(later, target, mongoLockShared, now); ready {
		t.Fatal("shared request is ready while an exclusive holder remains")
	}
	queue.release(writer, []mongoFenceTarget{target})
	if _, _, ready := queue.turn(later, target, mongoLockShared, now); !ready {
		t.Fatal("shared request is not ready after the exclusive holder ended")
	}
	if len(queue.entries) != 0 {
		t.Fatalf("queue keeps %d entries after every holder and request left", len(queue.entries))
	}
}

// A holder that waits for another lock gave up its fences: older
// transactions no longer wait for it, but younger ones still do, so they
// cannot change a document it already read. A transaction that starts
// waiting wakes the requests waiting for it.
func TestMongoLockQueueWaitingHoldersBlockOnlyYoungerTransactions(t *testing.T) {
	var queue mongoLockQueue
	target := mongoFenceTarget{collectionID: "media", documentID: "a"}
	now := time.Now()
	transactions := mongoQueueTransactions(&queue, 3)
	older, holder, younger := transactions[0], transactions[1], transactions[2]
	queue.hold(holder, target, mongoLockShared, now.Add(time.Minute))
	changed, _, ready := queue.turn(older, target, mongoLockExclusive, now)
	if ready {
		t.Fatal("exclusive request is ready while a running holder holds the document")
	}
	queue.wait(holder, []mongoFenceTarget{target})
	select {
	case <-changed:
	default:
		t.Fatal("a holder that started waiting did not wake the requests waiting for it")
	}
	if _, _, ready := queue.turn(older, target, mongoLockExclusive, now); !ready {
		t.Fatal("an older exclusive request waits for a younger waiting holder")
	}
	if _, _, ready := queue.turn(younger, target, mongoLockExclusive, now); ready {
		t.Fatal("a younger exclusive request passed an older waiting holder")
	}
	queue.hold(holder, target, mongoLockShared, now.Add(time.Minute))
	if _, _, ready := queue.turn(older, target, mongoLockExclusive, now); ready {
		t.Fatal("a holder that retook its fence no longer blocks older requests")
	}
}

// Shared holders do not wait for each other, and a hold whose server
// transaction MongoDB has already aborted no longer blocks anyone.
func TestMongoLockQueueSharedHoldersAndLapsedHolds(t *testing.T) {
	var queue mongoLockQueue
	target := mongoFenceTarget{collectionID: "media", documentID: "a"}
	now := time.Now()
	transactions := mongoQueueTransactions(&queue, 3)
	first, second, writer := transactions[0], transactions[1], transactions[2]
	queue.hold(first, target, mongoLockShared, now.Add(time.Second))
	if _, _, ready := queue.turn(second, target, mongoLockShared, now); !ready {
		t.Fatal("shared request waits for a shared holder")
	}
	_, expires, ready := queue.turn(writer, target, mongoLockExclusive, now)
	if ready || !expires.Equal(now.Add(time.Second)) {
		t.Fatalf("exclusive request against a shared holder: ready=%t expires=%v", ready, expires)
	}
	if _, _, ready := queue.turn(writer, target, mongoLockExclusive, now.Add(time.Second)); !ready {
		t.Fatal("a lapsed hold still blocks an exclusive request")
	}
}

// The queue tracks a bounded number of documents; beyond it, holds and
// requests are left to the database.
func TestMongoLockQueueIsBounded(t *testing.T) {
	var queue mongoLockQueue
	holder := &documentTransaction{}
	expires := time.Now().Add(time.Minute)
	for index := range mongoLockQueueEntries {
		if !queue.hold(holder, mongoFenceTarget{collectionID: "media", documentID: strconv.Itoa(index)}, mongoLockShared, expires) {
			t.Fatalf("hold %d was not recorded below the bound", index)
		}
	}
	extra := mongoFenceTarget{collectionID: "media", documentID: "extra"}
	if queue.hold(holder, extra, mongoLockShared, expires) || queue.enqueue(holder, extra) {
		t.Fatal("the queue recorded a document beyond its bound")
	}
	if _, _, ready := queue.turn(&documentTransaction{}, extra, mongoLockExclusive, time.Now()); !ready {
		t.Fatal("an untracked document waits in the queue")
	}
	if len(queue.entries) != mongoLockQueueEntries {
		t.Fatalf("queue tracks %d documents, want %d", len(queue.entries), mongoLockQueueEntries)
	}
}

func TestMongoAwaitTurnRespectsContextAndDeadline(t *testing.T) {
	never := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if mongoAwaitTurn(ctx, never, time.Time{}, time.Now().Add(time.Minute)) {
		t.Fatal("wait continued after its context was canceled")
	}
	started := time.Now()
	if mongoAwaitTurn(t.Context(), never, time.Time{}, started.Add(20*time.Millisecond)) {
		t.Fatal("wait continued past its deadline")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("wait overran its deadline by %v", elapsed)
	}
	if !mongoAwaitTurn(t.Context(), never, started.Add(10*time.Millisecond), time.Now().Add(time.Minute)) {
		t.Fatal("wait did not end when the blocking hold lapsed")
	}
	closed := make(chan struct{})
	close(closed)
	if !mongoAwaitTurn(t.Context(), closed, time.Time{}, time.Now().Add(time.Minute)) {
		t.Fatal("wait did not end when its turn changed")
	}
}
