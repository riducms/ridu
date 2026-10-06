package mongodb

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Open transactions of one Store get distinct slots, starting from the
// Store's first slot, until every slot is held; then they share the least
// used one.
func TestMongoFenceSlotsGiveEachOpenTransactionItsOwnSlot(t *testing.T) {
	first := mongoReferenceFenceSlots - 2
	slots := mongoFenceSlots{first: first}
	for index := range mongoReferenceFenceSlots {
		want := (first + index) % mongoReferenceFenceSlots
		if slot := slots.acquire(); slot != want {
			t.Fatalf("slot %d = %d, want %d", index, slot, want)
		}
	}
	if slot := slots.acquire(); slot != first {
		t.Fatalf("slot with every slot held = %d, want the first slot shared", slot)
	}
	slots.release(5)
	if slot := slots.acquire(); slot != 5 {
		t.Fatalf("slot after one was released = %d, want the released slot", slot)
	}
	if slot := slots.acquire(); slot != first+1 {
		t.Fatalf("next shared slot = %d, want the least used after the first", slot)
	}
}

func TestMongoFenceIDsAreDistinctPerDocumentAndSlot(t *testing.T) {
	seen := map[string]bool{}
	for _, target := range []mongoFenceTarget{
		{collectionID: "posts", documentID: "a"},
		{collectionID: "posts", documentID: "b"},
		{collectionID: "pages", documentID: "a"},
	} {
		for slot := range 3 {
			id := mongoReferenceFenceID(target, slot)
			if seen[id] {
				t.Fatalf("fence ID %q repeats for %#v slot %d", id, target, slot)
			}
			seen[id] = true
		}
	}
}

// A restart compares a relocked document with the locked read apart from
// its fence, which every lock increments.
func TestMongoSameDocumentExceptFence(t *testing.T) {
	document := func(fence int64, name string) bson.Raw {
		raw, err := bson.Marshal(bson.D{
			{Key: "_id", Value: "a"},
			{Key: "meta", Value: bson.D{{Key: "revision", Value: int64(1)}, {Key: "fence", Value: fence}}},
			{Key: "values", Value: bson.D{{Key: "name", Value: name}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if !mongoSameDocumentExceptFence(document(1, "a"), document(7, "a")) {
		t.Fatal("documents that differ only by fence are different")
	}
	if mongoSameDocumentExceptFence(document(1, "a"), document(1, "b")) {
		t.Fatal("documents with different values are the same")
	}
}
