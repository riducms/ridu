package field_test

import (
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestVirtualTakesItsValueTypeFromTheResolver(t *testing.T) {
	text := field.Virtual("label", func(operation.Context) (operation.Value[string], error) {
		return operation.Present("Ada"), nil
	})
	for _, test := range []struct {
		node field.Node
		want field.ValueType
	}{
		{text, field.ValueString},
		{field.Virtual("score", func(operation.Context) (operation.Value[float64], error) {
			return operation.Present(4.5), nil
		}), field.ValueNumber},
		{field.Virtual("featured", func(operation.Context) (operation.Value[bool], error) {
			return operation.Empty[bool](), nil
		}), field.ValueBoolean},
		{field.Virtual("meta", func(operation.Context) (operation.Value[store.Value], error) {
			return operation.Present(store.Object(store.Values{})), nil
		}), field.ValueJSON},
	} {
		if got := field.Snapshot(test.node).ValueType(); got != test.want {
			t.Errorf("%T value type = %q, want %q", test.node, got, test.want)
		}
	}
	if _, err := field.AsVirtual[string](text); err != nil {
		t.Fatalf("AsVirtual[string]: %v", err)
	}
	if _, err := field.AsVirtual[float64](text); err == nil {
		t.Fatal("AsVirtual[float64] accepted a string virtual field")
	}
	if _, err := field.AsVirtual[string](field.Text("title")); err == nil {
		t.Fatal("AsVirtual accepted a text field")
	}
	// Renaming and response transforms keep the resolver's type.
	renamed := text.Rename("displayName").AfterRead(func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
		name, _ := value.Get()
		return operation.Set(name + "!"), nil
	})
	if renamed.Resolver() == nil || len(renamed.AfterReadHooks()) != 1 {
		t.Fatal("typed virtual field lost its resolver or response transform")
	}
}
