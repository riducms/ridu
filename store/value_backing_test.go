package store_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestSameBackingUsesImmutableSourcesWithoutComparingContainers(t *testing.T) {
	object := store.Object(store.Values{"title": store.String("same")})
	list := store.List(object)
	document := store.Populated(store.Document{ID: "same", Values: store.Values{"title": store.String("same")}})
	for _, value := range []store.Value{object, list, document} {
		retained := value
		if !value.SameBacking(retained) {
			t.Fatal("copied immutable Value lost its backing identity")
		}
	}
	if object.SameBacking(store.Object(store.Values{"title": store.String("same")})) || list.SameBacking(store.List(object)) || document.SameBacking(store.Populated(store.Document{ID: "same", Values: store.Values{"title": store.String("same")}})) {
		t.Fatal("separate containers were compared by contents")
	}
	updated, _ := list.WithListItem(0, object)
	item, _ := updated.ListItem(0)
	if list.SameBacking(updated) || !item.SameBacking(object) {
		t.Fatal("persistent replacement did not distinguish the root from shared children")
	}
	decoded := object
	if err := json.Unmarshal([]byte(`{"title":"same"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if object.SameBacking(decoded) {
		t.Fatal("decoded container reused an unrelated source identity")
	}
}

func TestSameBackingDistinguishesKindsAndExactScalarBits(t *testing.T) {
	for _, test := range []struct {
		name        string
		left, right store.Value
		want        bool
	}{
		{"null", store.Null(), store.Null(), true},
		{"zero is not null", store.Value{}, store.Null(), false},
		{"string", store.String("same"), store.String("same"), true},
		{"changed string", store.String("before"), store.String("after"), false},
		{"number", store.Number(12), store.Number(12), true},
		{"signed zero", store.Number(0), store.Number(math.Copysign(0, -1)), false},
		{"same NaN bits", store.Number(math.Float64frombits(0x7ff8000000000001)), store.Number(math.Float64frombits(0x7ff8000000000001)), true},
		{"different NaN bits", store.Number(math.Float64frombits(0x7ff8000000000001)), store.Number(math.Float64frombits(0x7ff8000000000002)), false},
		{"boolean", store.Boolean(true), store.Boolean(true), true},
		{"changed boolean", store.Boolean(true), store.Boolean(false), false},
		{"different kinds", store.String("12"), store.Number(12), false},
		{"empty lists", store.List(), store.List(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.left.SameBacking(test.right); got != test.want {
				t.Fatalf("SameBacking = %v, want %v", got, test.want)
			}
		})
	}
}
