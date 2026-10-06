package mongodb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type mongoFenceFixture struct {
	backend  *Store
	manifest schema.Manifest
	targets  schema.Collection
	owners   schema.Collection
	all      map[schema.StableID]schema.Collection
	first    string
	second   string
}

func newMongoFenceFixture(t *testing.T) *mongoFenceFixture {
	t.Helper()
	backend := mongoIntegrationStore(t)
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "MongoDB fences",
		Collections: []ridu.Collection{
			{Slug: "targets", Fields: field.Fields{field.Text("name")}},
			{Slug: "owners", Fields: field.Fields{
				field.Text("code").Unique(),
				field.Relationship("target", "targets").OnDelete(field.ReferenceDeleteRestrict),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	fixture := &mongoFenceFixture{backend: backend, manifest: manifest, all: map[schema.StableID]schema.Collection{}}
	for _, collection := range manifest.Snapshot().Collections {
		fixture.all[collection.ID] = collection
		switch collection.Slug {
		case "targets":
			fixture.targets = collection
		case "owners":
			fixture.owners = collection
		}
	}
	write := mongoBegin(t, backend, false)
	for _, id := range []string{"first", "second"} {
		if _, err := write.Create(t.Context(), store.CreateRequest{Collection: fixture.targets, ID: id, Values: store.Values{"name": store.String(id)}}); err != nil {
			mongoRollback(t, write)
			t.Fatal(err)
		}
	}
	mongoCommit(t, write)
	fixture.first, fixture.second = "first", "second"
	return fixture
}

// replica opens a second Store on the fixture's database, as another Ridu
// replica would, with its slots starting at first.
func (fixture *mongoFenceFixture) replica(t *testing.T, first int) *Store {
	t.Helper()
	parsed, err := url.Parse(strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL")))
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	parsed.Path = "/" + fixture.backend.database.Name()
	replica, err := OpenWithConfig(t.Context(), Config{
		DatabaseURL: parsed.String(), AllowInsecureTransport: true,
		ConnectTimeout: 10 * time.Second, ServerSelectionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("open MongoDB replica store: %v", err)
	}
	t.Cleanup(func() {
		if err := replica.Close(); err != nil {
			t.Errorf("close MongoDB replica store: %v", err)
		}
	})
	if err := replica.syncIndexes(t.Context(), fixture.manifest); err != nil {
		t.Fatal(err)
	}
	replica.fenceSlots.first = first
	return replica
}

// warm creates every shared fence of a target, as earlier saves would have.
func (fixture *mongoFenceFixture) warm(t *testing.T, id string) {
	t.Helper()
	records := make([]any, 0, mongoReferenceFenceSlots)
	for slot := range mongoReferenceFenceSlots {
		target := mongoFenceTarget{collectionID: fixture.targets.ID, documentID: id}
		records = append(records, bson.D{
			{Key: "_id", Value: mongoReferenceFenceID(target, slot)},
			{Key: "collection", Value: string(target.collectionID)},
			{Key: "document", Value: target.documentID},
			{Key: "slot", Value: int32(slot)},
			{Key: "fence", Value: int64(0)},
		})
	}
	if _, err := fixture.backend.referenceFenceCollection().InsertMany(t.Context(), records); err != nil {
		t.Fatal(err)
	}
}

func (fixture *mongoFenceFixture) sharedFences(t *testing.T, id string) int64 {
	t.Helper()
	count, err := fixture.backend.referenceFenceCollection().CountDocuments(t.Context(), bson.D{
		{Key: "collection", Value: string(fixture.targets.ID)}, {Key: "document", Value: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// sharedFenceValues returns the committed fence of each of a target's shared
// fence records, in ascending order.
func (fixture *mongoFenceFixture) sharedFenceValues(t *testing.T, id string) []int64 {
	t.Helper()
	cursor, err := fixture.backend.referenceFenceCollection().Find(t.Context(), mongoReferenceFenceFilter(mongoFenceTarget{collectionID: fixture.targets.ID, documentID: id}))
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Fence int64 `bson:"fence"`
	}
	if err := cursor.All(t.Context(), &records); err != nil {
		t.Fatal(err)
	}
	values := make([]int64, 0, len(records))
	for _, record := range records {
		values = append(values, record.Fence)
	}
	slices.Sort(values)
	return values
}

func (fixture *mongoFenceFixture) lock(ctx context.Context, transaction store.Transaction, id string, mode store.LockMode) error {
	_, err := transaction.Find(ctx, store.Request{Collection: fixture.targets, Collections: fixture.all, ID: id, Lock: mode})
	return err
}

func (fixture *mongoFenceFixture) createOwner(ctx context.Context, transaction store.Transaction, code, target string) error {
	_, err := transaction.Create(ctx, store.CreateRequest{Collection: fixture.owners, Values: store.Values{
		"code": store.String(code), "target": store.String(target),
	}})
	return err
}

// reference locks a target as relationship validation does and creates an
// owner that references it.
func (fixture *mongoFenceFixture) reference(ctx context.Context, transaction store.Transaction, code, target string) error {
	if err := fixture.lock(ctx, transaction, target, store.LockReference); err != nil {
		return err
	}
	return fixture.createOwner(ctx, transaction, code, target)
}

// remove locks a target exclusively and hard deletes it under its owners'
// restrict rule, as the operation engine does.
func (fixture *mongoFenceFixture) remove(ctx context.Context, transaction store.Transaction, id string) error {
	if err := fixture.lock(ctx, transaction, id, store.LockMutation); err != nil {
		return err
	}
	if err := transaction.ApplyReferenceDelete(ctx, store.ReferenceDeleteRequest{
		Target:      store.DocumentReference{CollectionID: fixture.targets.ID, DocumentID: id},
		Collections: fixture.all,
	}); err != nil {
		return err
	}
	_, err := transaction.Delete(ctx, store.Request{Collection: fixture.targets, ID: id})
	return err
}

func mongoAwait(t *testing.T, result <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(within):
		t.Fatalf("operation did not finish within %v", within)
		return nil
	}
}

func mongoStillWaiting(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("operation finished while its fence was held: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
}

// Saves that reference one target hold shared locks together, as on
// PostgreSQL, instead of taking turns on the target.
func TestMongoDBReferenceLocksShareATarget(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	open := make([]store.Transaction, 0, 4)
	defer func() {
		for _, transaction := range open {
			mongoRollback(t, transaction)
		}
	}()
	for index := range 4 {
		transaction := mongoBegin(t, fixture.backend, false)
		open = append(open, transaction)
		started := time.Now()
		if err := fixture.reference(ctx, transaction, fmt.Sprintf("shared-%d", index), fixture.first); err != nil {
			t.Fatalf("reference %d while %d others hold the target = %v", index, index, err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("reference %d waited %v for the other holders", index, elapsed)
		}
	}
	for _, transaction := range open {
		mongoCommit(t, transaction)
	}
	open = nil
}

// The first save to use a slot creates its fence and, so that an exclusive
// lock cannot miss it, fences the working head too: concurrent first users
// take turns instead of failing.
func TestMongoDBNewSharedFencesTakeTurns(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	ctx := t.Context()
	holder := mongoBegin(t, fixture.backend, false)
	if err := fixture.reference(ctx, holder, "holder", fixture.first); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	waiter := mongoBegin(t, fixture.backend, false)
	result := make(chan error, 1)
	go func() { result <- fixture.reference(ctx, waiter, "waiter", fixture.first) }()
	mongoStillWaiting(t, result)
	mongoCommit(t, holder)
	if err := mongoAwait(t, result, 5*time.Second); err != nil {
		mongoRollback(t, waiter)
		t.Fatalf("second new shared fence = %v, want success after the first committed", err)
	}
	mongoCommit(t, waiter)
	if fences := fixture.sharedFences(t, fixture.first); fences != 2 {
		t.Fatalf("shared fences = %d, want one per slot used", fences)
	}
}

// An exclusive lock waits for every shared holder and then sees what they
// committed, so a restrict delete cannot pass a new reference.
func TestMongoDBExclusiveLockWaitsForSharedHolders(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	holders := []store.Transaction{mongoBegin(t, fixture.backend, false), mongoBegin(t, fixture.backend, false)}
	for index, holder := range holders {
		if err := fixture.reference(ctx, holder, fmt.Sprintf("holder-%d", index), fixture.first); err != nil {
			mongoRollback(t, holders[0])
			mongoRollback(t, holders[1])
			t.Fatal(err)
		}
	}
	deletion := mongoBegin(t, fixture.backend, false)
	result := make(chan error, 1)
	go func() { result <- fixture.remove(ctx, deletion, fixture.first) }()
	mongoStillWaiting(t, result)
	mongoCommit(t, holders[0])
	mongoStillWaiting(t, result)
	mongoCommit(t, holders[1])
	err := mongoAwait(t, result, 5*time.Second)
	mongoRollback(t, deletion)
	if !errors.Is(err, store.ErrDeleteRestricted) {
		t.Fatalf("delete after referencing saves committed = %v, want ErrDeleteRestricted", err)
	}
}

// A shared lock waits for an exclusive holder and then reads what it
// committed.
func TestMongoDBSharedLockWaitsForAnExclusiveHolder(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	writer := mongoBegin(t, fixture.backend, false)
	current, err := writer.Find(ctx, store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.first}.CurrentRequest())
	if err != nil {
		mongoRollback(t, writer)
		t.Fatal(err)
	}
	if fences := fixture.sharedFences(t, fixture.first); fences != mongoReferenceFenceSlots {
		mongoRollback(t, writer)
		t.Fatalf("committed shared fences while an exclusive lock is open = %d", fences)
	}
	if _, err := writer.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.first},
		Values:  store.Values{"name": store.String("renamed")}, Current: &current,
	}); err != nil {
		mongoRollback(t, writer)
		t.Fatal(err)
	}
	reader := mongoBegin(t, fixture.backend, false)
	defer mongoRollback(t, reader)
	result := make(chan error, 1)
	var read store.Document
	go func() {
		var err error
		read, err = reader.Find(ctx, store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.first, Lock: store.LockReference})
		result <- err
	}()
	mongoStillWaiting(t, result)
	mongoCommit(t, writer)
	if err := mongoAwait(t, result, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if name, _ := read.Values["name"].StringValue(); name != "renamed" {
		t.Fatalf("shared lock after the exclusive holder committed read %q", name)
	}
	// The update incremented every shared fence instead of deleting it, so
	// later references reuse their records.
	if fences := fixture.sharedFenceValues(t, fixture.first); len(fences) != mongoReferenceFenceSlots || fences[0] != 1 || fences[len(fences)-1] != 1 {
		t.Fatalf("committed shared fences after an exclusive lock = %v, want every slot incremented once", fences)
	}
}

// An exclusive lock whose snapshot predates a reference still excludes it:
// through the shared fence when its snapshot saw the fence, and through the
// working head when the referencing save created the fence.
func TestMongoDBExclusiveLockFromAnOlderSnapshotSeesNewReferences(t *testing.T) {
	for _, warmed := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing-fence=%t", warmed), func(t *testing.T) {
			fixture := newMongoFenceFixture(t)
			if warmed {
				fixture.warm(t, fixture.first)
			}
			ctx := t.Context()
			deletion := mongoBegin(t, fixture.backend, false)
			defer mongoRollback(t, deletion)
			// An unlocked read fixes the deletion's snapshot.
			if _, err := deletion.Find(ctx, store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.second}); err != nil {
				t.Fatal(err)
			}
			reference := mongoBegin(t, fixture.backend, false)
			if err := fixture.reference(ctx, reference, "late", fixture.first); err != nil {
				mongoRollback(t, reference)
				t.Fatal(err)
			}
			mongoCommit(t, reference)
			if err := fixture.remove(ctx, deletion, fixture.first); !errors.Is(err, store.ErrDeleteRestricted) {
				t.Fatalf("delete from a snapshot older than a committed reference = %v, want ErrDeleteRestricted", err)
			}
		})
	}
}

// Replicas coordinate only through the database. Distinct slots share a
// target; a shared slot takes turns; a delete on one replica waits for, and
// then sees, a reference committed on another.
func TestMongoDBReplicasCoordinateThroughFences(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	fixture.backend.fenceSlots.first = 0
	distinct := fixture.replica(t, 1)
	same := fixture.replica(t, 0)

	primary := mongoBegin(t, fixture.backend, false)
	if err := fixture.reference(ctx, primary, "primary", fixture.first); err != nil {
		mongoRollback(t, primary)
		t.Fatal(err)
	}
	other := mongoBegin(t, distinct, false)
	started := time.Now()
	if err := fixture.reference(ctx, other, "distinct-slot", fixture.first); err != nil || time.Since(started) > time.Second {
		mongoRollback(t, primary)
		mongoRollback(t, other)
		t.Fatalf("reference on a replica with another slot = %v after %v, want no wait", err, time.Since(started))
	}
	sharing := mongoBegin(t, same, false)
	shared := make(chan error, 1)
	go func() { shared <- fixture.reference(ctx, sharing, "same-slot", fixture.first) }()
	mongoStillWaiting(t, shared)

	deletion := mongoBegin(t, distinct, false)
	defer mongoRollback(t, deletion)
	deleted := make(chan error, 1)
	go func() { deleted <- fixture.remove(ctx, deletion, fixture.first) }()
	mongoStillWaiting(t, deleted)

	mongoCommit(t, primary)
	if err := mongoAwait(t, shared, 5*time.Second); err != nil {
		mongoRollback(t, other)
		mongoRollback(t, sharing)
		t.Fatalf("reference sharing a slot with another replica = %v, want success after it committed", err)
	}
	mongoCommit(t, other)
	mongoCommit(t, sharing)
	if err := mongoAwait(t, deleted, 5*time.Second); !errors.Is(err, store.ErrDeleteRestricted) {
		t.Fatalf("delete on a replica racing references from both = %v, want ErrDeleteRestricted", err)
	}
}

// Concurrent saves on two replicas race a delete of the target they
// reference. Whatever the interleaving, no committed owner references a
// deleted target: a restrict delete fails while one does, and a nullify
// delete clears every reference it races.
func TestMongoDBReplicaDeletesRaceNewReferences(t *testing.T) {
	for _, action := range []field.ReferenceDeleteAction{field.ReferenceDeleteRestrict, field.ReferenceDeleteNullify} {
		t.Run(string(action), func(t *testing.T) {
			config := ridu.Config{
				Name: "MongoDB replica reference race",
				Collections: []ridu.Collection{
					{Slug: "targets", Fields: field.Fields{field.Text("name")}},
					{Slug: "owners", Fields: field.Fields{field.Text("code"), field.Relationship("target", "targets").OnDelete(action)}},
				},
			}
			primary := mongoIntegrationStore(t)
			first, err := ridu.New(config, primary)
			if err != nil {
				t.Fatal(err)
			}
			if err := primary.syncIndexes(t.Context(), first.Manifest()); err != nil {
				t.Fatal(err)
			}
			fixture := &mongoFenceFixture{backend: primary, manifest: first.Manifest()}
			replica := fixture.replica(t, (primary.fenceSlots.first+mongoReferenceFenceSlots/2)%mongoReferenceFenceSlots)
			second, err := ridu.New(config, replica)
			if err != nil {
				t.Fatal(err)
			}
			applications := []*ridu.App{first, second}

			for round := range 3 {
				target, err := first.Local().Create(t.Context(), "targets", store.Values{"name": store.String("popular")}, ridu.MutationOptions{})
				if err != nil {
					t.Fatal(err)
				}
				var wait sync.WaitGroup
				failures := make(chan error, 64)
				for writer := range 6 {
					wait.Add(1)
					go func() {
						defer wait.Done()
						application := applications[writer%2]
						for index := range 4 {
							_, err := application.Local().Create(t.Context(), "owners", store.Values{
								"code": store.String(fmt.Sprintf("%d-%d-%d", round, writer, index)), "target": store.String(target.ID),
							}, ridu.MutationOptions{})
							if err != nil && !mongoOperationCode(err, "validation") {
								failures <- fmt.Errorf("create owner on replica %d: %w", writer%2, err)
							}
						}
					}()
				}
				wait.Add(1)
				go func() {
					defer wait.Done()
					time.Sleep(time.Duration(round*15) * time.Millisecond)
					_, err := applications[(round+1)%2].Local().Delete(t.Context(), "targets", target.ID, ridu.MutationOptions{})
					if err != nil && !mongoOperationCode(err, "delete_restricted") {
						failures <- fmt.Errorf("delete target: %w", err)
					}
				}()
				wait.Wait()
				close(failures)
				for failure := range failures {
					t.Error(failure)
				}

				_, findErr := first.Local().Find(t.Context(), "targets", target.ID, ridu.FindOptions{})
				targetExists := findErr == nil
				if findErr != nil && !mongoOperationCode(findErr, "not_found") {
					t.Fatal(findErr)
				}
				owners, err := second.Local().List(t.Context(), "owners", ridu.ListOptions{Limit: 100})
				if err != nil {
					t.Fatal(err)
				}
				referencing := 0
				for _, owner := range owners.Documents {
					if id, _ := owner.Values["target"].StringValue(); id == target.ID {
						referencing++
					}
				}
				if !targetExists && referencing != 0 {
					t.Fatalf("round %d: %d owners reference the deleted target", round, referencing)
				}
				if action == field.ReferenceDeleteNullify && targetExists {
					t.Fatalf("round %d: the nullify delete did not complete", round)
				}
			}
		})
	}
}

// A restart retakes earlier locks and refuses one whose document changed
// while no fence protected it: the engine already holds its old content.
func TestMongoDBRestartRejectsADocumentChangedSinceItWasLocked(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	ctx := t.Context()
	holder := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(ctx, holder, fixture.second, store.LockMutation); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	waiter := mongoBegin(t, fixture.backend, false)
	defer mongoRollback(t, waiter)
	if err := fixture.lock(ctx, waiter, fixture.first, store.LockMutation); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	// This write waits for the waiter's fence on the first target and lands
	// when the waiter gives up its fences to wait for the holder below.
	changed := make(chan error, 1)
	go func() {
		_, err := fixture.backend.database.Collection(physicalCollectionName(fixture.targets.ID)).UpdateOne(ctx,
			bson.D{{Key: "_id", Value: fixture.first}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "values.name", Value: "changed"}}}})
		changed <- err
	}()
	mongoStillWaiting(t, changed)
	locked := make(chan error, 1)
	go func() { locked <- fixture.lock(ctx, waiter, fixture.second, store.LockReference) }()
	if err := mongoAwait(t, changed, 5*time.Second); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	mongoStillWaiting(t, locked)
	// Once the holder ends, the waiter restarts and finds the change.
	mongoCommit(t, holder)
	if err := mongoAwait(t, locked, 5*time.Second); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("restart over a changed locked document = %v, want ErrConflict", err)
	}
}

// After a content write the server transaction cannot be replayed, so a
// conflicting lock fails at once instead of waiting.
func TestMongoDBFenceConflictAfterAWriteIsNotRetried(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	ctx := t.Context()
	holder := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(ctx, holder, fixture.second, store.LockMutation); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	defer mongoRollback(t, holder)
	writer := mongoBegin(t, fixture.backend, false)
	defer mongoRollback(t, writer)
	if err := fixture.createOwner(ctx, writer, "written", fixture.first); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err := fixture.lock(ctx, writer, fixture.second, store.LockReference)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("fence after a write = %v, want ErrConflict", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("fence after a write waited %v before conflicting", elapsed)
	}
}

// A unique value another transaction holds is a genuine conflict: it is
// reported, committed or not, and never retried.
func TestMongoDBUniqueConflictsAreNotRetried(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	ctx := t.Context()
	first := mongoBegin(t, fixture.backend, false)
	if err := fixture.createOwner(ctx, first, "shared", fixture.first); err != nil {
		mongoRollback(t, first)
		t.Fatal(err)
	}
	concurrent := mongoBegin(t, fixture.backend, false)
	started := time.Now()
	err := fixture.createOwner(ctx, concurrent, "shared", fixture.second)
	mongoRollback(t, concurrent)
	if !errors.Is(err, store.ErrConflict) || time.Since(started) > time.Second {
		mongoRollback(t, first)
		t.Fatalf("concurrent unique value = %v after %v, want an immediate ErrConflict", err, time.Since(started))
	}
	mongoCommit(t, first)
	later := mongoBegin(t, fixture.backend, false)
	err = fixture.createOwner(ctx, later, "shared", fixture.second)
	mongoRollback(t, later)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("committed unique value = %v, want ErrConflict", err)
	}
}

// Shared fences outlive the updates that increment them, a document never
// has more than one per slot, and a hard delete removes them.
func TestMongoDBSharedFencesStayBoundedAndLeaveWithTheirDocument(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	ctx := t.Context()
	for index := range mongoReferenceFenceSlots + 8 {
		// With no other transaction open, a reference uses the Store's first
		// slot; moving it walks every slot and then reuses them.
		fixture.backend.fenceSlots.mu.Lock()
		fixture.backend.fenceSlots.first = index % mongoReferenceFenceSlots
		fixture.backend.fenceSlots.mu.Unlock()
		reference := mongoBegin(t, fixture.backend, false)
		if err := fixture.lock(ctx, reference, fixture.first, store.LockReference); err != nil {
			mongoRollback(t, reference)
			t.Fatal(err)
		}
		mongoCommit(t, reference)
		if index%5 == 4 {
			update := mongoBegin(t, fixture.backend, false)
			if err := fixture.rename(ctx, update, fixture.first, fmt.Sprintf("edit %d", index)); err != nil {
				mongoRollback(t, update)
				t.Fatal(err)
			}
			mongoCommit(t, update)
		}
		if fences, want := fixture.sharedFences(t, fixture.first), int64(min(index+1, mongoReferenceFenceSlots)); fences != want {
			t.Fatalf("shared fences after %d references = %d, want %d", index+1, fences, want)
		}
	}
	deletion := mongoBegin(t, fixture.backend, false)
	if err := fixture.remove(ctx, deletion, fixture.first); err != nil {
		mongoRollback(t, deletion)
		t.Fatal(err)
	}
	mongoCommit(t, deletion)
	if fences := fixture.sharedFences(t, fixture.first); fences != 0 {
		t.Fatalf("shared fences of a hard-deleted document = %d", fences)
	}
	if fences := fixture.sharedFences(t, fixture.second); fences != 0 {
		t.Fatalf("shared fences of a document never referenced = %d", fences)
	}
}

// A collection's shared fences leave with its identity: a rename removes
// those of the old identity, which saves recreate under the new one, and a
// retirement removes those of the retired collection.
func TestMongoDBMigrationsRemoveSharedFences(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	owner := mongoFenceTarget{collectionID: fixture.owners.ID, documentID: "owner"}
	if _, err := fixture.backend.referenceFenceCollection().InsertOne(t.Context(), bson.D{
		{Key: "_id", Value: mongoReferenceFenceID(owner, 0)}, {Key: "collection", Value: string(owner.collectionID)},
		{Key: "document", Value: owner.documentID}, {Key: "slot", Value: int32(0)}, {Key: "fence", Value: int64(0)},
	}); err != nil {
		t.Fatal(err)
	}
	ownerFences := func() int64 {
		count, err := fixture.backend.referenceFenceCollection().CountDocuments(t.Context(), mongoReferenceFenceFilter(owner))
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	migrate := func(step func(*documentTransaction) error) {
		t.Helper()
		transaction, err := fixture.backend.begin(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if err := step(transaction); err != nil {
			mongoRollback(t, transaction)
			t.Fatal(err)
		}
		mongoCommit(t, transaction)
	}
	migrate(func(transaction *documentTransaction) error {
		return fixture.backend.rewriteMongoMigrationFrameworkState(t.Context(), transaction, fixture.targets.ID, fixture.targets.ID+"_renamed")
	})
	if fences, owners := fixture.sharedFences(t, fixture.first), ownerFences(); fences != 0 || owners != 1 {
		t.Fatalf("shared fences after renaming targets: targets %d, owners %d; want 0 and 1", fences, owners)
	}
	migrate(func(transaction *documentTransaction) error {
		return fixture.backend.retireMongoMigrationResources(t.Context(), transaction, []schema.StableID{fixture.owners.ID})
	})
	if owners := ownerFences(); owners != 0 {
		t.Fatalf("shared fences of a retired collection = %d", owners)
	}
}

// Deleting a document removes its shared fences with it.
func TestMongoDBDeleteRemovesSharedFences(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.second)
	deletion := mongoBegin(t, fixture.backend, false)
	if err := fixture.remove(t.Context(), deletion, fixture.second); err != nil {
		mongoRollback(t, deletion)
		t.Fatal(err)
	}
	mongoCommit(t, deletion)
	if fences := fixture.sharedFences(t, fixture.second); fences != 0 {
		t.Fatalf("shared fences of a deleted document = %d", fences)
	}
}

// awaitQueuedExclusive waits until an exclusive request for the target waits
// in backend's lock queue.
func (fixture *mongoFenceFixture) awaitQueuedExclusive(t *testing.T, backend *Store, id string) {
	t.Helper()
	target := mongoFenceTarget{collectionID: fixture.targets.ID, documentID: id}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		backend.lockQueue.mu.Lock()
		entry := backend.lockQueue.entries[target]
		queued := entry != nil && len(entry.exclusive) != 0
		backend.lockQueue.mu.Unlock()
		if queued {
			return
		}
	}
	t.Fatalf("no exclusive request for %q waits in the lock queue", id)
}

func mongoQueuedDocuments(backend *Store) int {
	backend.lockQueue.mu.Lock()
	defer backend.lockQueue.mu.Unlock()
	return len(backend.lockQueue.entries)
}

// rename locks a target exclusively and renames it, as an update does.
func (fixture *mongoFenceFixture) rename(ctx context.Context, transaction store.Transaction, id, name string) error {
	request := store.Request{Collection: fixture.targets, Collections: fixture.all, ID: id}
	locked := request
	locked.Lock = store.LockMutation
	if _, err := transaction.Find(ctx, locked); err != nil {
		return err
	}
	current, err := transaction.Find(ctx, request.CurrentRequest())
	if err != nil {
		return err
	}
	_, err = transaction.Update(ctx, store.UpdateRequest{Request: request, Values: store.Values{"name": store.String(name)}, Current: &current})
	return err
}

// A waiting exclusive lock waits only for the shared holders it found:
// within one Store, a later reference waits behind it, as PostgreSQL queues
// a FOR SHARE request behind a waiting FOR UPDATE, and then reads what it
// committed.
func TestMongoDBWaitingExclusiveLockHoldsBackLaterReferences(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	holder := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(ctx, holder, fixture.first, store.LockReference); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	writer := mongoBegin(t, fixture.backend, false)
	renamed := make(chan error, 1)
	go func() { renamed <- fixture.rename(ctx, writer, fixture.first, "renamed") }()
	fixture.awaitQueuedExclusive(t, fixture.backend, fixture.first)

	later := mongoBegin(t, fixture.backend, false)
	defer mongoRollback(t, later)
	var read store.Document
	referenced := make(chan error, 1)
	go func() {
		var err error
		read, err = later.Find(ctx, store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.first, Lock: store.LockReference})
		referenced <- err
	}()
	mongoStillWaiting(t, renamed)
	mongoStillWaiting(t, referenced)

	mongoCommit(t, holder)
	if err := mongoAwait(t, renamed, 5*time.Second); err != nil {
		mongoRollback(t, writer)
		t.Fatalf("update after its shared holder committed = %v", err)
	}
	mongoStillWaiting(t, referenced)
	mongoCommit(t, writer)
	if err := mongoAwait(t, referenced, 5*time.Second); err != nil {
		t.Fatalf("reference queued behind an exclusive lock = %v", err)
	}
	if name, _ := read.Values["name"].StringValue(); name != "renamed" {
		t.Fatalf("reference queued behind an exclusive lock read %q, want the committed rename", name)
	}
}

// Saves that keep referencing a target cannot starve an update of it: each
// update waits only for the references it found.
func TestMongoDBContinuousReferencesDoNotStarveAnExclusiveLock(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	stop := make(chan struct{})
	var references sync.WaitGroup
	failures := make(chan error, 8)
	for worker := range 8 {
		references.Add(1)
		go func() {
			defer references.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				transaction, err := fixture.backend.Begin(t.Context())
				if err != nil {
					failures <- err
					return
				}
				if err := fixture.lock(t.Context(), transaction, fixture.first, store.LockReference); err != nil {
					_ = transaction.Rollback(context.Background())
					failures <- fmt.Errorf("reference by worker %d: %w", worker, err)
					return
				}
				// Each reference overlaps the other workers'.
				time.Sleep(50 * time.Millisecond)
				if err := transaction.Commit(t.Context()); err != nil {
					failures <- fmt.Errorf("commit reference by worker %d: %w", worker, err)
					return
				}
			}
		}()
	}
	defer func() {
		close(stop)
		references.Wait()
		close(failures)
		for failure := range failures {
			t.Error(failure)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	for edit := range 5 {
		transaction := mongoBegin(t, fixture.backend, false)
		started := time.Now()
		if err := fixture.rename(t.Context(), transaction, fixture.first, fmt.Sprintf("edit %d", edit)); err != nil {
			mongoRollback(t, transaction)
			t.Fatalf("edit %d under continuous references = %v after %v", edit, err, time.Since(started))
		}
		mongoCommit(t, transaction)
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("edit %d under continuous references waited %v", edit, elapsed)
		}
	}
}

// A lock wait ends with its context, cancelled or past its deadline, and
// leaves the queue: a reference queued behind it no longer waits, and the
// transaction can only roll back.
func TestMongoDBLockWaitEndsWithItsContext(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	holder := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(t.Context(), holder, fixture.first, store.LockReference); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	for _, ending := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(ending.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			if ending == context.DeadlineExceeded {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 750*time.Millisecond)
			}
			defer cancel()
			writer := mongoBegin(t, fixture.backend, false)
			locked := make(chan error, 1)
			go func() { locked <- fixture.lock(ctx, writer, fixture.first, store.LockMutation) }()
			fixture.awaitQueuedExclusive(t, fixture.backend, fixture.first)
			later := mongoBegin(t, fixture.backend, false)
			defer mongoRollback(t, later)
			referenced := make(chan error, 1)
			go func() { referenced <- fixture.lock(t.Context(), later, fixture.first, store.LockReference) }()
			mongoStillWaiting(t, referenced)

			started := time.Now()
			if ending == context.Canceled {
				cancel()
			}
			if err := mongoAwait(t, locked, 5*time.Second); !errors.Is(err, ending) || time.Since(started) > 2*time.Second {
				t.Fatalf("lock wait after its context ended = %v after %v, want %v", err, time.Since(started), ending)
			}
			if err := mongoAwait(t, referenced, 5*time.Second); err != nil {
				t.Fatalf("reference queued behind an abandoned exclusive lock = %v", err)
			}
			if _, err := writer.Find(t.Context(), store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.second}); err == nil {
				t.Fatal("a transaction whose lock wait failed ran another statement")
			}
			mongoRollback(t, writer)
		})
	}
	mongoCommit(t, holder)
	if queued := mongoQueuedDocuments(fixture.backend); queued != 0 {
		t.Fatalf("lock queue keeps %d documents after every transaction ended", queued)
	}
}

// Exclusive locks waiting on two targets for younger references that each
// need the other target do not deadlock: each reference gives way, giving up
// its fences, and restarts unchanged once the older locks are released.
func TestMongoDBQueuedLocksDoNotDeadlock(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	fixture.warm(t, fixture.second)
	ctx := t.Context()
	targets := []string{fixture.first, fixture.second}
	locks := []store.Transaction{mongoBegin(t, fixture.backend, false), mongoBegin(t, fixture.backend, false)}
	saves := []store.Transaction{mongoBegin(t, fixture.backend, false), mongoBegin(t, fixture.backend, false)}
	for index, save := range saves {
		if err := fixture.lock(ctx, save, targets[index], store.LockReference); err != nil {
			t.Fatal(err)
		}
	}
	locked := make([]chan error, 2)
	for index := range locks {
		locked[index] = make(chan error, 1)
		go func() { locked[index] <- fixture.lock(ctx, locks[index], targets[index], store.LockMutation) }()
		fixture.awaitQueuedExclusive(t, fixture.backend, targets[index])
	}
	// Each save commits once it holds both targets: the first save to use a
	// new fence on a target waits for the other to commit.
	referenced := make([]chan error, 2)
	for index, save := range saves {
		referenced[index] = make(chan error, 1)
		go func() {
			if err := fixture.lock(ctx, save, targets[1-index], store.LockReference); err != nil {
				referenced[index] <- err
				return
			}
			referenced[index] <- save.Commit(ctx)
		}()
	}
	for index := range locks {
		if err := mongoAwait(t, locked[index], 5*time.Second); err != nil {
			t.Fatalf("exclusive lock %d = %v", index, err)
		}
		mongoCommit(t, locks[index])
	}
	for index := range saves {
		if err := mongoAwait(t, referenced[index], 5*time.Second); err != nil {
			t.Fatalf("crossed reference %d = %v", index, err)
		}
	}
	if queued := mongoQueuedDocuments(fixture.backend); queued != 0 {
		t.Fatalf("lock queue keeps %d documents after every transaction ended", queued)
	}
}

// A save that waits for a lock gives up its fences, but a younger update of
// the same Store still waits for it instead of changing a document the save
// already read, so the save resumes unchanged rather than failing.
func TestMongoDBWaitingReferenceKeepsYoungerUpdatesOut(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	writer := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(ctx, writer, fixture.second, store.LockMutation); err != nil {
		mongoRollback(t, writer)
		t.Fatal(err)
	}
	save := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(ctx, save, fixture.first, store.LockReference); err != nil {
		mongoRollback(t, writer)
		mongoRollback(t, save)
		t.Fatal(err)
	}
	saved := make(chan error, 1)
	go func() {
		if err := fixture.lock(ctx, save, fixture.second, store.LockReference); err != nil {
			saved <- err
			return
		}
		saved <- save.Commit(ctx)
	}()
	mongoStillWaiting(t, saved)

	editor := mongoBegin(t, fixture.backend, false)
	renamed := make(chan error, 1)
	go func() { renamed <- fixture.rename(ctx, editor, fixture.first, "renamed") }()
	mongoStillWaiting(t, renamed)
	mongoCommit(t, writer)
	if err := mongoAwait(t, saved, 5*time.Second); err != nil {
		mongoRollback(t, editor)
		t.Fatalf("waiting save after an update of a document it read was queued = %v", err)
	}
	if err := mongoAwait(t, renamed, 5*time.Second); err != nil {
		mongoRollback(t, editor)
		t.Fatalf("update queued behind a waiting save = %v", err)
	}
	mongoCommit(t, editor)
}

// Each Store queues only its own transactions. Another Store's reference is
// found in the database, where an exclusive lock restarts and retries until
// it commits, while this Store's later references wait behind that lock.
func TestMongoDBLockQueuesArePerStore(t *testing.T) {
	fixture := newMongoFenceFixture(t)
	fixture.warm(t, fixture.first)
	ctx := t.Context()
	replica := fixture.replica(t, (fixture.backend.fenceSlots.first+mongoReferenceFenceSlots/2)%mongoReferenceFenceSlots)
	holder := mongoBegin(t, fixture.backend, false)
	if err := fixture.lock(ctx, holder, fixture.first, store.LockReference); err != nil {
		mongoRollback(t, holder)
		t.Fatal(err)
	}
	writer := mongoBegin(t, replica, false)
	renamed := make(chan error, 1)
	go func() { renamed <- fixture.rename(ctx, writer, fixture.first, "renamed") }()
	fixture.awaitQueuedExclusive(t, replica, fixture.first)
	if queued := mongoQueuedDocuments(fixture.backend); queued != 1 {
		t.Fatalf("the holder's Store queues %d documents, want only its own hold", queued)
	}

	later := mongoBegin(t, replica, false)
	defer mongoRollback(t, later)
	var read store.Document
	referenced := make(chan error, 1)
	go func() {
		var err error
		read, err = later.Find(ctx, store.Request{Collection: fixture.targets, Collections: fixture.all, ID: fixture.first, Lock: store.LockReference})
		referenced <- err
	}()
	mongoStillWaiting(t, renamed)
	mongoStillWaiting(t, referenced)
	mongoCommit(t, holder)
	if err := mongoAwait(t, renamed, 5*time.Second); err != nil {
		mongoRollback(t, writer)
		t.Fatalf("update after another Store's reference committed = %v", err)
	}
	mongoCommit(t, writer)
	if err := mongoAwait(t, referenced, 5*time.Second); err != nil {
		t.Fatalf("reference queued behind the replica's update = %v", err)
	}
	if name, _ := read.Values["name"].StringValue(); name != "renamed" {
		t.Fatalf("reference queued behind the replica's update read %q", name)
	}
}
