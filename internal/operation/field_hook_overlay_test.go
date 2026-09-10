package operation

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestFieldHookLeafRemovalRefreshesPresenceAndRetainedViews(t *testing.T) {
	path, err := query.NewPath("value")
	if err != nil {
		t.Fatal(err)
	}
	field := schema.Field{Name: "value", Path: path, Type: schema.FieldTypeJSON}
	fields := []schema.Field{field, {Name: "marker", Type: schema.FieldTypeText}}
	values := store.Values{"value": store.String("original"), "marker": store.String("unchanged")}
	var snapshots []operation.View
	var identities []string
	observe := func(ctx Context, expected store.Value, present bool) {
		t.Helper()
		if ctx.ValuePresent != present || !reflect.DeepEqual(ctx.Value, expected) {
			t.Fatalf("input=%v present=%v, want %v present=%v", ctx.Value, ctx.ValuePresent, expected, present)
		}
		for _, view := range []store.Values{ctx.Data, ctx.SiblingData} {
			value, exists := view["value"]
			if exists != present || !reflect.DeepEqual(value, expected) {
				t.Fatalf("callback view=%v present=%v, want %v present=%v", value, exists, expected, present)
			}
			if marker, _ := view["marker"].StringValue(); marker != "unchanged" {
				t.Fatal("unrelated sibling changed")
			}
		}
		snapshots = append(snapshots, operation.Snapshot(ctx.SiblingData))
		identities = append(identities, ctx.OccurrenceID)
	}
	binding := FieldBinding{ID: "value", Field: field, Hooks: Hooks{BeforeChange: []Hook{
		func(ctx Context) error {
			observe(ctx, store.String("original"), true)
			delete(ctx.SiblingData, "value")
			return nil
		},
		func(ctx Context) error {
			observe(ctx, store.Value{}, false)
			ctx.SiblingData["value"] = store.Null()
			return nil
		},
		func(ctx Context) error {
			observe(ctx, store.Null(), true)
			return nil
		},
	}}}
	collection := Collection{Schema: schema.Collection{Fields: fields}, Bindings: []FieldBinding{binding}}
	ctx := Context{Context: t.Context(), Operation: operation.Update, Collection: collection.Schema, Data: values}
	if err := runFieldHooks(collection, ctx, func(hooks Hooks) []Hook { return hooks.BeforeChange }); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 3 || identities[0] != identities[1] || identities[1] != identities[2] {
		t.Fatalf("callback identities=%v", identities)
	}
	for i, expected := range []store.Value{store.String("original"), {}, store.Null()} {
		value, present := snapshots[i].Lookup("value")
		if present != (i != 1) || !reflect.DeepEqual(value, expected) {
			t.Fatalf("retained snapshot %d changed: value=%v present=%v", i, value, present)
		}
	}
	if value, present := values["value"]; !present || value.Kind() != store.ValueNull {
		t.Fatal("final explicit null was lost")
	}
}
