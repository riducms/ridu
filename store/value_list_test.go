package store_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestValueListItemUpdatesPreserveCanonicalSnapshots(t *testing.T) {
	for _, size := range []int{0, 1, 31, 32, 33, 1024, 1025} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			items := make([]store.Value, size)
			for index := range items {
				items[index] = store.Number(float64(index))
			}
			original := store.List(items...)
			updated := original
			type snapshot struct {
				value store.Value
				want  store.Value
			}
			snapshots := []snapshot{{value: original, want: store.List(items...)}}
			for _, index := range []int{0, size / 2, size - 1} {
				if index < 0 || index >= size {
					continue
				}
				replacement := store.Object(store.Values{"number": store.Number(float64(-index - 1))})
				var valid bool
				updated, valid = updated.WithListItem(index, replacement)
				if !valid {
					t.Fatalf("valid index %d rejected", index)
				}
				items[index] = replacement
				if item, valid := updated.ListItem(index); !valid || !reflect.DeepEqual(item, replacement) {
					t.Fatalf("updated item %d=%v, valid=%v", index, item, valid)
				}
				snapshots = append(snapshots, snapshot{value: updated, want: store.List(items...)})
			}
			// The constructor's input and each accessor remain detached, including
			// lists large enough to have more than one branch level.
			if size != 0 {
				items[0] = store.Null()
				extracted, _ := updated.CopyList()
				extracted[0] = store.Null()
			}
			for index, retained := range snapshots {
				if !reflect.DeepEqual(retained.value, retained.want) {
					t.Fatalf("retained snapshot %d differs from reconstruction", index)
				}
				encoded, err := json.Marshal(retained.value)
				if err != nil {
					t.Fatal(err)
				}
				var decoded store.Value
				if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, retained.value) {
					t.Fatalf("snapshot %d JSON round trip differs: %v", index, err)
				}
			}
			for _, index := range []int{0, size / 2, size - 1} {
				if index >= 0 && index < size {
					updated, _ = updated.WithListItem(index, store.Number(float64(index)))
				}
			}
			if !reflect.DeepEqual(updated, original) {
				t.Fatal("restoring items did not restore structural equality")
			}
			items, valid := original.CopyList()
			if !valid || items == nil || len(items) != size {
				t.Fatalf("list accessor size=%d, nil=%v, valid=%v", len(items), items == nil, valid)
			}
		})
	}
}

func TestValueListItemAccessRejectsInvalidIndicesWithoutChanges(t *testing.T) {
	for _, value := range []store.Value{store.Value{}, store.Null(), store.String("text"), store.Object(store.Values{}), store.List(), store.List(store.String("one"))} {
		for _, index := range []int{-1, 1, math.MaxInt} {
			if _, valid := value.ListItem(index); valid {
				t.Fatalf("%s accepted index %d", value.Kind(), index)
			}
			if updated, valid := value.WithListItem(index, store.Null()); valid || !reflect.DeepEqual(updated, value) {
				t.Fatalf("%s changed at invalid index %d", value.Kind(), index)
			}
		}
		if value.Kind() != store.ValueList {
			if items, valid := value.CopyList(); valid || items == nil || len(items) != 0 {
				t.Fatalf("%s non-list accessor=%v, valid=%v", value.Kind(), items, valid)
			}
			if _, valid := value.ListItem(0); valid {
				t.Fatalf("%s accepted item access", value.Kind())
			}
		}
	}
}

func TestValueListJSONMatchesSliceEncoding(t *testing.T) {
	for _, test := range []struct {
		name  string
		items []store.Value
	}{
		{name: "empty", items: []store.Value{}},
		{name: "mixed", items: []store.Value{store.Null(), store.Boolean(true), store.Number(1.5), store.String("<tag>\n\u2028"), store.Object(store.Values{"key": store.List()}), store.List(store.String("nested"))}},
		{name: "unknown", items: []store.Value{{}}},
		{name: "nan", items: []store.Value{store.Number(math.NaN())}},
		{name: "infinity", items: []store.Value{store.List(store.Number(math.Inf(1)))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			want, wantError := json.Marshal(test.items)
			got, err := store.List(test.items...).MarshalJSON()
			if !bytes.Equal(got, want) || (err == nil) != (wantError == nil) {
				t.Fatalf("JSON=%s error=%v, want %s error=%v", got, err, want, wantError)
			}
			if err != nil && (reflect.TypeOf(err) != reflect.TypeOf(wantError) || err.Error() != wantError.Error()) {
				t.Fatalf("error=%T %v, want %T %v", err, err, wantError, wantError)
			}
		})
	}
	if got := encodedValue(t, store.List()); got != "[]" {
		t.Fatalf("empty list JSON=%s", got)
	}
}

func TestValueListSupportsConcurrentPersistentUpdates(t *testing.T) {
	items := make([]store.Value, 1025)
	for index := range items {
		items[index] = store.Number(float64(index))
	}
	original := store.List(items...)
	var workers sync.WaitGroup
	for _, index := range []int{0, 1, 31, 32, 33, 511, 1023, 1024} {
		workers.Go(func() {
			for attempt := range 20 {
				updated, valid := original.WithListItem(index, store.String(fmt.Sprint(attempt)))
				if !valid {
					t.Errorf("valid index %d rejected", index)
					return
				}
				item, _ := updated.ListItem(index)
				if text, _ := item.StringValue(); text != fmt.Sprint(attempt) {
					t.Errorf("updated item %d=%v", index, item)
				}
				extracted, _ := updated.CopyList()
				extracted[index] = store.Null()
				if item, _ := original.ListItem(index); !reflect.DeepEqual(item, items[index]) {
					t.Errorf("original item %d changed", index)
				}
			}
		})
	}
	workers.Wait()
	if !reflect.DeepEqual(original, store.List(items...)) {
		t.Fatal("concurrent updates changed original")
	}
}
