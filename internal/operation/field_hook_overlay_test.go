package operation

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestFieldHookLeafReplacementRefreshesViewsAndKeepsRetainedViews(t *testing.T) {
	path, err := query.NewPath("value")
	if err != nil {
		t.Fatal(err)
	}
	field := schema.Field{Name: "value", Path: path, Type: schema.FieldTypeJSON}
	fields := []schema.Field{field, {Name: "marker", Type: schema.FieldTypeText}}
	values := store.Values{"value": store.String("original"), "marker": store.String("unchanged")}
	var snapshots []operation.View
	var identities []string
	observe := func(ctx Context, expected store.Value) {
		t.Helper()
		if !ctx.ValuePresent || !reflect.DeepEqual(ctx.Value, expected) {
			t.Fatalf("input=%v present=%v, want %v", ctx.Value, ctx.ValuePresent, expected)
		}
		if ctx.Data != nil {
			t.Fatal("field callback received the operation's working map")
		}
		for _, view := range []store.Value{ctx.Root, ctx.Siblings} {
			value, exists := view.Lookup("value")
			if !exists || !reflect.DeepEqual(value, expected) {
				t.Fatalf("callback view=%v present=%v, want %v", value, exists, expected)
			}
			if marker, _ := view.Get("marker").StringValue(); marker != "unchanged" {
				t.Fatal("unrelated sibling changed")
			}
		}
		snapshots = append(snapshots, operation.ObjectView(ctx.Siblings))
		identities = append(identities, ctx.OccurrenceID)
	}
	binding := FieldBinding{ID: "value", Field: field, Hooks: Hooks{BeforeChange: []Hook{
		func(ctx Context) error {
			observe(ctx, store.String("original"))
			ctx.ReplaceValue(store.String("first"))
			return nil
		},
		func(ctx Context) error {
			observe(ctx, store.String("first"))
			ctx.ReplaceValue(store.Null())
			return nil
		},
		func(ctx Context) error {
			observe(ctx, store.Null())
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
	for i, expected := range []store.Value{store.String("original"), store.String("first"), store.Null()} {
		value, present := snapshots[i].Lookup("value")
		if !present || !reflect.DeepEqual(value, expected) {
			t.Fatalf("retained view %d changed: value=%v present=%v", i, value, present)
		}
	}
	if value, present := values["value"]; !present || value.Kind() != store.ValueNull {
		t.Fatal("final explicit null was lost")
	}
}

func TestCallbackObjectNormalizesScalarsAndSharesCompleteObjects(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.FieldTypeText, Localized: true},
		{Name: "count", Type: schema.FieldTypeNumber},
		{Name: "seo", Type: schema.FieldTypeGroup},
	}
	encoded := func(value store.Value) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	complete := store.Object(store.Values{"title": store.String("a"), "count": store.Number(1)})
	if view := callbackObject(fields, complete, false, ownMember{}); !view.SameBacking(complete) {
		t.Fatal("a complete object was copied")
	}
	partial := store.Object(store.Values{"count": store.Number(1)})
	for _, test := range []struct {
		name          string
		origin        store.Value
		skipLocalized bool
		own           ownMember
		want          string
	}{
		{"missing scalars read as null; containers stay sparse", partial, false, ownMember{}, `{"count":1,"title":null}`},
		{"all-locales roots keep localized members sparse", partial, true, ownMember{}, `{"count":1}`},
		{"own value replaces a stale member", complete, false, ownMember{name: "count", value: store.Number(2), present: true, apply: true}, `{"count":2,"title":"a"}`},
		{"absent own scalar reads as null", complete, false, ownMember{name: "count", apply: true}, `{"count":null,"title":"a"}`},
		{"a non-object origin becomes an object", store.Null(), false, ownMember{}, `{"count":null,"title":null}`},
	} {
		if got := encoded(callbackObject(fields, test.origin, test.skipLocalized, test.own)); got != test.want {
			t.Errorf("%s: got %s, want %s", test.name, got, test.want)
		}
	}
	if encoded(partial) != `{"count":1}` || encoded(complete) != `{"count":1,"title":"a"}` {
		t.Fatal("normalization changed its origin")
	}
}
