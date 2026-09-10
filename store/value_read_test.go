package store_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestValueReadsDistinguishMembershipAndContainerKinds(t *testing.T) {
	value := store.Object(store.Values{
		"null": store.Null(), "false": store.Boolean(false),
		"seo.title": store.String("literal"),
		"seo":       store.Object(store.Values{"title": store.String("nested")}),
	})
	if _, ok := value.Lookup("missing"); ok || value.Get("missing").Kind() != store.ValueNull {
		t.Fatal("missing member acquired a value")
	}
	if child, ok := value.Lookup("null"); !ok || child.Kind() != store.ValueNull {
		t.Fatal("explicit null lost membership")
	}
	if child, ok := value.Get("false").BooleanValue(); !ok || child {
		t.Fatal("false lost its type or value")
	}
	if title, _ := value.Get("seo.title").StringValue(); title != "literal" {
		t.Fatal("lookup interpreted a literal key as a path")
	}
	if title, _ := value.Get("seo").Get("title").StringValue(); title != "nested" {
		t.Fatal("nested lookup missed its child")
	}
	entries := store.Values{}
	for name, child := range value.Entries() {
		entries[name] = child
	}
	if value.Len() != 4 || !reflect.DeepEqual(store.Object(entries), value) {
		t.Fatal("object iteration lost members")
	}
	for _, scalar := range []store.Value{{}, store.Null(), store.String("text"), store.Number(1), store.Boolean(true), store.Populated(store.Document{ID: "related"})} {
		if _, ok := scalar.Lookup("title"); ok || scalar.Get("title").Kind() != store.ValueNull || scalar.Len() != 0 {
			t.Fatalf("%s treated as a container", scalar.Kind())
		}
		for range scalar.Entries() {
			t.Fatalf("%s yielded object members", scalar.Kind())
		}
		for range scalar.Elements() {
			t.Fatalf("%s yielded list elements", scalar.Kind())
		}
	}
}

func TestValueIteratorsRetainSnapshotsAndSupportEarlyStop(t *testing.T) {
	for _, size := range []int{0, 1, 33, 1025} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			items := make([]store.Value, size)
			for i := range items {
				items[i] = store.Object(store.Values{"number": store.Number(float64(i))})
			}
			original := store.List(items...)
			iterator := original.Elements()
			if size > 0 {
				original, _ = original.WithListItem(size-1, store.Null())
			}
			seen := 0
			for range iterator {
				seen++
				break
			}
			if seen != min(1, size) {
				t.Fatal("iterator did not stop early")
			}
			for range 2 {
				retained := slices.Collect(iterator)
				if len(retained) != size || !reflect.DeepEqual(store.List(retained...), store.List(items...)) {
					t.Fatal("reused iterator lost order or followed a later replacement")
				}
			}
			if original.Len() != size {
				t.Fatal("replacement changed list length")
			}
		})
	}
	object := store.Object(store.Values{"nested": store.Object(store.Values{"title": store.String("original")})})
	iterator := object.Entries()
	for range iterator {
		break
	}
	if err := json.Unmarshal([]byte(`{"replaced":true}`), &object); err != nil {
		t.Fatal(err)
	}
	for name, child := range iterator {
		copy, _ := child.CopyObject()
		copy["title"] = store.String("local edit")
		if title, _ := child.Get("title").StringValue(); name != "nested" || title != "original" {
			t.Fatal("iterator or copied map changed the retained child")
		}
	}
}

func TestValueReadersShareSnapshotsAcrossConcurrentEdits(t *testing.T) {
	items := make([]store.Value, 65)
	for i := range items {
		items[i] = store.Object(store.Values{"number": store.Number(float64(i))})
	}
	original := store.List(items...)
	iterator := original.Elements()
	var workers sync.WaitGroup
	for worker := range 4 {
		workers.Go(func() {
			for range 10 {
				updated, _ := original.WithListItem(worker, store.Null())
				i := 0
				for item := range iterator {
					number, _ := item.Get("number").NumberValue()
					if number != float64(i) {
						t.Error("concurrent replacement changed an earlier reader")
						return
					}
					i++
				}
				if item, _ := updated.ListItem(worker); item.Kind() != store.ValueNull || i != len(items) {
					t.Error("replacement or traversal missed an item")
				}
			}
		})
	}
	workers.Wait()
}
