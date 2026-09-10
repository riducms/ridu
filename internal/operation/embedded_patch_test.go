package operation

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestRuntimePatchPreservesUntargetedItemsAndSnapshots(t *testing.T) {
	items := make([]store.Value, 65)
	for i := range items {
		items[i] = store.Object(store.Values{"title": store.String("before"), "nested": store.List(store.String("child"))})
	}
	values := store.Values{"items": store.List(items...), "scalar": store.String("unchanged")}
	snapshot := store.CloneValues(values)
	patch := &runtimePatch{}
	patch.add("items.31.title", store.String("first"), false)
	patch.add("items.32.nested.0", store.String("nested edit"), false)
	patch.add("items.64.title", store.Value{}, true)
	// Removing a list element has never meant deleting or shifting its row.
	patch.add("items.0", store.Value{}, true)
	for _, path := range []string{"items.-1", "items.65", "items.invalid", "missing.child", "scalar.child"} {
		patch.add(path, store.Null(), false)
	}
	patch.apply(values)
	want := append([]store.Value(nil), items...)
	want[31] = store.Object(store.Values{"title": store.String("first"), "nested": store.List(store.String("child"))})
	want[32] = store.Object(store.Values{"title": store.String("before"), "nested": store.List(store.String("nested edit"))})
	want[64] = store.Object(store.Values{"nested": store.List(store.String("child"))})
	if !reflect.DeepEqual(values, store.Values{"items": store.List(want...), "scalar": store.String("unchanged")}) {
		t.Fatal("patch changed untargeted items or missed nested replacements")
	}
	if !reflect.DeepEqual(snapshot["items"], store.List(items...)) {
		t.Fatal("patch mutated an earlier snapshot")
	}
}
