package sqlite

import (
	"github.com/riducms/ridu/internal/schematest"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"strings"
	"testing"
)

// embeddedMigrationField places the card definition, whose fields are
// definition-relative, in an embedded tree case.
func embeddedMigrationField(t *testing.T, children ...schema.Field) schema.Field {
	path, _ := query.ParsePath("canvas")
	tree := schema.EmbeddedTree{Version: 1, Key: "parts", Root: []string{"document"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "attributes", Identity: "uid", Discriminator: "variant", BlockReferences: []string{"card"}}}}
	card := schema.BlockType{Slug: "card", TypeName: "Card", Fields: children}
	return schematest.Bind(t, "pages", []schema.BlockType{card}, schema.Field{ID: "canvas", Name: "canvas", Path: path, Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "canvas", EmbeddedTrees: []schema.EmbeddedTree{tree}}})[0]
}

func embeddedMigrationText(name string, required bool) schema.Field {
	path, _ := query.ParsePath(name)
	return schema.Field{ID: schema.StableID("block-card-" + name), Name: name, Path: path, Type: schema.FieldTypeText, Required: required}
}

// embeddedMigrationTransition validates one collection holding the field
// before and after, with each embedded definition validated once.
func embeddedMigrationTransition(before, after schema.Field) error {
	snapshot := func(field schema.Field) schema.Snapshot {
		return schema.Snapshot{Collections: []schema.Collection{{ID: "pages", Slug: "pages", Fields: []schema.Field{field}}}}
	}
	return sqliteAdditiveRules{}.snapshot(snapshot(before), snapshot(after))
}

func TestEmbeddedAdditiveEvolutionAndRollback(t *testing.T) {
	before := embeddedMigrationField(t, embeddedMigrationText("title", false))
	after := embeddedMigrationField(t, embeddedMigrationText("title", false), embeddedMigrationText("caption", false))
	if err := embeddedMigrationTransition(before, after); err != nil {
		t.Fatal(err)
	}
	required := embeddedMigrationField(t, embeddedMigrationText("title", false), embeddedMigrationText("caption", true))
	if err := embeddedMigrationTransition(before, required); err == nil || !strings.Contains(err.Error(), "caption") {
		t.Fatalf("required addition: %v", err)
	}
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
