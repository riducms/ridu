package operation

import (
	"testing"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestEmbeddedHookOriginalScopeCannotCrossVariantReplacement(t *testing.T) {
	path := func(raw string) query.Path {
		value, err := query.ParsePath(raw)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	fields := []schema.Field{{Name: "body", Path: path("body"), Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{EmbeddedTrees: []schema.EmbeddedTree{{Version: 1, Key: "widgets", Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []schema.BlockType{
		{Slug: "card", Fields: []schema.Field{{Name: "title", Path: path("body.widgets.widget.card.title"), Type: schema.FieldTypeText}}},
		{Slug: "note", Fields: []schema.Field{{Name: "title", Path: path("body.widgets.widget.note.title"), Type: schema.FieldTypeText}}},
	}}}}}}}}
	data := func(kind string) store.Values {
		return store.Values{"body": store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(store.Values{"schema": store.String(kind), "uid": store.String("same"), "title": store.String(kind)})})}
	}
	calls := 0
	binding := FieldBinding{ID: "note-title", Field: fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[1].ResolvedFields()[0], Hooks: Hooks{BeforeChange: []Hook{func(ctx Context) error {
		calls++
		if ctx.OriginalSiblingData != nil || ctx.OriginalValue.Kind() != "" {
			t.Fatal("new variant inherited previous variant payload")
		}
		return nil
	}}}}
	err := runFieldHooks(Collection{Schema: schema.Collection{Fields: fields}, Bindings: []FieldBinding{binding}}, Context{Context: t.Context(), Operation: operation.Update, Collection: schema.Collection{Fields: fields}, Data: data("note"), Original: &store.Document{Values: data("card")}}, func(hooks Hooks) []Hook { return hooks.BeforeChange }, false, false)
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
