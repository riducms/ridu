package mongodb

import (
	"context"
	"sync"
	"time"
)

// The fences in fences.go are the only authority over a document lock, but a
// transaction that meets a conflicting fence can only restart and retry, and
// shared locks that keep arriving can then starve an exclusive lock: each
// retry finds another reference holding the document. PostgreSQL queues
// instead. A mongoLockQueue orders the transactions of one Store the same way:
//
//   - A waiting exclusive request makes shared requests of transactions
//     younger than it wait behind it, so it waits only for the holders it
//     found and for older transactions.
//   - A transaction that must wait for another of this Store's transactions
//     waits for that transaction to end or to start waiting itself, rather
//     than sleeping and retrying.
//
// A transaction aborts its server transaction, losing its fences, before it
// waits, so no transaction, of this Store or another, waits in the database
// for a waiting transaction. The queue keeps its holds while it waits, so a
// younger transaction of the Store still waits for it and cannot change a
// document it already read. An older one ignores them: a transaction waits
// for a waiting one only when that one is older, so waiting transactions
// cannot form a cycle, and a running holder either ends or starts waiting. A
// transaction's age is the order in which the Store began it.
//
// The queue never grants a lock: after waiting, a transaction still takes its
// fence in the database. Other Stores, such as other replicas, are invisible
// to it; a conflict with one of them restarts and retries with backoff.
type mongoLockQueue struct {
	mu      sync.Mutex
	entries map[mongoFenceTarget]*mongoLockQueueEntry
	// began is the age of the last transaction the Store began.
	began uint64
}

// mongoLockQueueEntries bounds the documents a queue tracks. Beyond it,
// further holds and requests are not tracked and wait in the database alone.
const mongoLockQueueEntries = 4096

// mongoLockQueueEntry is one document a transaction of the Store holds or
// waits to lock exclusively. It is removed when neither remains.
type mongoLockQueueEntry struct {
	holders map[*documentTransaction]mongoQueuedHold
	// exclusive holds the ages of the waiting exclusive requests.
	exclusive map[*documentTransaction]uint64
	// changed is closed, and replaced, when a holder leaves or starts
	// waiting, or a waiting exclusive request leaves.
	changed chan struct{}
}

type mongoQueuedHold struct {
	mode mongoLockMode
	age  uint64
	// waiting reports a holder that gave up its fences to wait; only younger
	// transactions wait for it.
	waiting bool
	// expires is when MongoDB aborts the holder's server transaction, so a
	// transaction that is never ended stops blocking others.
	expires time.Time
}

// begin returns the age of a transaction the Store begins.
func (queue *mongoLockQueue) begin() uint64 {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.began++
	return queue.began
}

// hold records that transaction's current server transaction holds target in
// at least mode. It reports whether target is newly recorded for
// transaction; it is not when transaction already holds it or the queue is
// full.
func (queue *mongoLockQueue) hold(transaction *documentTransaction, target mongoFenceTarget, mode mongoLockMode, expires time.Time) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	entry := queue.entry(target)
	if entry == nil {
		return false
	}
	current, held := entry.holders[transaction]
	entry.holders[transaction] = mongoQueuedHold{mode: max(mode, current.mode), age: transaction.age, expires: expires}
	return !held
}

// wait marks transaction's holds on targets as waiting: it no longer holds
// their fences, and only younger transactions keep waiting for it.
func (queue *mongoLockQueue) wait(transaction *documentTransaction, targets []mongoFenceTarget) {
	queue.update(transaction, targets, func(entry *mongoLockQueueEntry, hold mongoQueuedHold) {
		hold.waiting = true
		entry.holders[transaction] = hold
	})
}

// release forgets transaction's holds on targets once it has ended.
func (queue *mongoLockQueue) release(transaction *documentTransaction, targets []mongoFenceTarget) {
	queue.update(transaction, targets, func(entry *mongoLockQueueEntry, _ mongoQueuedHold) {
		delete(entry.holders, transaction)
	})
}

// update applies change to transaction's hold on each of targets and wakes
// their waiters.
func (queue *mongoLockQueue) update(transaction *documentTransaction, targets []mongoFenceTarget, change func(*mongoLockQueueEntry, mongoQueuedHold)) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for _, target := range targets {
		if entry := queue.entries[target]; entry != nil {
			if hold, held := entry.holders[transaction]; held {
				change(entry, hold)
				queue.changed(target, entry)
			}
		}
	}
}

// enqueue records transaction's exclusive request for target, which younger
// shared requests wait behind until dequeue. It reports false, recording
// nothing, when the queue is full.
func (queue *mongoLockQueue) enqueue(transaction *documentTransaction, target mongoFenceTarget) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	entry := queue.entry(target)
	if entry == nil {
		return false
	}
	entry.exclusive[transaction] = transaction.age
	return true
}

func (queue *mongoLockQueue) dequeue(transaction *documentTransaction, target mongoFenceTarget) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if entry := queue.entries[target]; entry != nil {
		delete(entry.exclusive, transaction)
		queue.changed(target, entry)
	}
}

// turn reports whether transaction may take its lock on target in mode now.
// It waits for a conflicting hold, unless the holder is younger and waiting
// itself, and a shared request also waits for an older waiting exclusive
// request. When it may not, changed closes when that may differ, and expires,
// when not zero, is when a blocking hold lapses.
func (queue *mongoLockQueue) turn(transaction *documentTransaction, target mongoFenceTarget, mode mongoLockMode, now time.Time) (changed <-chan struct{}, expires time.Time, ready bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	entry := queue.entries[target]
	if entry == nil {
		return nil, time.Time{}, true
	}
	if own, held := entry.holders[transaction]; held && own.mode >= mode {
		return nil, time.Time{}, true
	}
	blocked := false
	for holder, hold := range entry.holders {
		switch {
		case holder == transaction,
			mode == mongoLockShared && hold.mode == mongoLockShared,
			hold.waiting && hold.age > transaction.age,
			!now.Before(hold.expires):
			continue
		}
		blocked = true
		if expires.IsZero() || hold.expires.Before(expires) {
			expires = hold.expires
		}
	}
	if mode == mongoLockShared {
		for waiting, age := range entry.exclusive {
			if waiting != transaction && age < transaction.age {
				blocked = true
				break
			}
		}
	}
	if !blocked {
		return nil, time.Time{}, true
	}
	return entry.changed, expires, false
}

// entry returns target's entry, creating it unless the queue is full.
// queue.mu must be held.
func (queue *mongoLockQueue) entry(target mongoFenceTarget) *mongoLockQueueEntry {
	if entry := queue.entries[target]; entry != nil {
		return entry
	}
	if len(queue.entries) >= mongoLockQueueEntries {
		return nil
	}
	if queue.entries == nil {
		queue.entries = make(map[mongoFenceTarget]*mongoLockQueueEntry)
	}
	entry := &mongoLockQueueEntry{
		holders:   make(map[*documentTransaction]mongoQueuedHold),
		exclusive: make(map[*documentTransaction]uint64),
		changed:   make(chan struct{}),
	}
	queue.entries[target] = entry
	return entry
}

// changed wakes target's waiters and removes the entry once nothing holds
// or waits for target. queue.mu must be held.
func (queue *mongoLockQueue) changed(target mongoFenceTarget, entry *mongoLockQueueEntry) {
	close(entry.changed)
	if len(entry.holders) == 0 && len(entry.exclusive) == 0 {
		delete(queue.entries, target)
		return
	}
	entry.changed = make(chan struct{})
}

// mongoAwaitTurn waits until changed closes or expires passes, reporting
// false without waiting past deadline or ctx.
func mongoAwaitTurn(ctx context.Context, changed <-chan struct{}, expires, deadline time.Time) bool {
	limit := deadline
	if !expires.IsZero() && expires.Before(limit) {
		limit = expires
	}
	timer := time.NewTimer(time.Until(limit))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-changed:
		return true
	case <-timer.C:
		return time.Now().Before(deadline)
	}
}
