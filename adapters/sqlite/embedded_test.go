package sqlite

import (
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"strings"
	"testing"
)

func embeddedMigrationField(children ...schema.Field) schema.Field {
	path, _ := query.ParsePath("canvas")
	tree := schema.EmbeddedTree{Version: 1, Key: "parts", Root: []string{"document"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "attributes", Identity: "uid", Discriminator: "variant", Types: []schema.BlockType{{Slug: "card", Fields: children}}}}}
	return schema.Field{ID: "canvas", Name: "canvas", Path: path, Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "canvas", EmbeddedTrees: []schema.EmbeddedTree{tree}}}
}
func TestEmbeddedAdditiveEvolutionAndRollback(t *testing.T) {
	before := embeddedMigrationField(schema.Field{ID: "title", Name: "title", Type: schema.FieldTypeText})
	after := embeddedMigrationField(schema.Field{ID: "title", Name: "title", Type: schema.FieldTypeText}, schema.Field{ID: "caption", Name: "caption", Type: schema.FieldTypeText})
	if err := validateSQLiteAdditiveFields("pages", []schema.Field{before}, []schema.Field{after}); err != nil {
		t.Fatal(err)
	}
	after.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[1].Required = true
	if err := validateSQLiteAdditiveFields("pages", []schema.Field{before}, []schema.Field{after}); err == nil || !strings.Contains(err.Error(), "caption") {
		t.Fatalf("required addition: %v", err)
	}
	after.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[1].Required = false
	payload := store.Object(store.Values{"uid": store.String("one"), "variant": store.String("card"), "title": store.String("Title"), "caption": store.String("Caption")})
	value := store.Object(store.Values{"document": store.Object(store.Values{"kind": store.String("widget"), "attributes": payload})})
	updated, changed := scrubSQLiteRollbackFieldValue(after, before, value)
	if !changed {
		t.Fatal("rollback missed embedded addition")
	}
	root, _ := updated.CopyObject()
	document, _ := root["document"].CopyObject()
	attributes, _ := document["attributes"].CopyObject()
	if _, exists := attributes["caption"]; exists {
		t.Fatal("rollback retained removed child")
	}
	if got, _ := attributes["title"].StringValue(); got != "Title" {
		t.Fatal("rollback changed retained child")
	}
	after.Plugin.EmbeddedTrees[0].Children = "different"
	if err := validateSQLiteAdditiveFields("pages", []schema.Field{before}, []schema.Field{after}); err == nil || !strings.Contains(err.Error(), "data migration") {
		t.Fatalf("envelope change: %v", err)
	}
}
