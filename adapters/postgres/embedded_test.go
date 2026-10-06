package postgres

import (
	"encoding/json"
	"fmt"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"testing"
)

func TestEmbeddedReferenceMigrationUsesDeclaredPayloads(t *testing.T) {
	relation := schema.Field{ID: "block-card-author", Name: "author", Path: query.Field("author"), Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: "authors", CollectionSlug: "authors"}}}}
	tree := schema.EmbeddedTree{Version: 1, Key: "parts", Root: []string{"document"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "attributes", Identity: "uid", Discriminator: "variant", BlockReferences: []string{"card"}}}}
	card := schema.BlockType{Slug: "card", TypeName: "Card", Fields: []schema.Field{relation}}
	before := schema.Snapshot{Blocks: []schema.BlockType{card}, Collections: []schema.Collection{{ID: "pages", Fields: []schema.Field{{
		ID: "canvas", Name: "canvas", Path: query.Field("canvas"), Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "canvas", EmbeddedTrees: []schema.EmbeddedTree{tree}},
	}}}}}
	if err := schema.BindBlockReferences(&before); err != nil {
		t.Fatal(err)
	}
	field := before.Collections[0].Fields[0]
	reference := map[string]any{"relationTo": "authors", "id": "one"}
	ordinary := map[string]any{"relationTo": "authors", "id": "untouched"}
	value := map[string]any{"document": map[string]any{"kind": "widget", "attributes": map[string]any{"uid": "one", "variant": "card", "author": reference}}, "opaque": ordinary}
	if err := validateEmbeddedJSON(field, value, false); err != nil {
		t.Fatal(err)
	}
	if changed, err := rewriteFieldCollectionReferences(value, field, false, "authors", "people"); err != nil || !changed {
		t.Fatal("reference was not rewritten")
	}
	payload := value["document"].(map[string]any)["attributes"].(map[string]any)
	if payload["author"].(map[string]any)["relationTo"] != "people" {
		t.Fatal(payload)
	}
	if value["opaque"].(map[string]any)["relationTo"] != "authors" {
		t.Fatal("opaque JSON traversed")
	}
	if !fieldContainsCollectionReferences(field) {
		t.Fatal("schema discovery missed embedded reference")
	}
	after := schema.Snapshot{Collections: []schema.Collection{{ID: "pages", Fields: []schema.Field{{ID: "canvas", Name: "canvas", Type: schema.FieldTypePlugin}}}}}
	if !referenceIndexTopologyChanged(before, after) {
		t.Fatal("reference topology missed removed embedded schema")
	}
}

func TestEmbeddedReferenceRenameComposesEnvelopeAndPayloads(t *testing.T) {
	for _, rootArray := range []bool{false, true} {
		for _, localized := range []bool{false, true} {
			t.Run(fmt.Sprintf("array=%v/localized=%v", rootArray, localized), func(t *testing.T) {
				snapshot := postgresEmbeddedEvolutionSnapshot()
				postgresEmbeddedBlock(t, &snapshot, "card").Fields = []schema.Field{
					{ID: "block-card-target", Name: "target", Path: query.Field("target"), Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{Polymorphic: true}},
					{ID: "block-card-raw", Name: "raw", Path: query.Field("raw"), Type: schema.FieldTypeJSON},
				}
				field := schema.NewManifest(snapshot).Snapshot().Collections[0].Fields[0]
				field.Localized = localized
				field.Plugin.ReferenceKeys = []string{"relationTo"}
				if rootArray {
					field.Plugin.EmbeddedTrees[0].Root = []string{}
				}
				data := `[{"kind":"link","relationTo":"authors"},{"kind":"widget","data":{"schema":"card","uid":"a","target":{"relationTo":"authors","id":"one"},"raw":{"relationTo":"authors","kind":"widget","data":{"schema":"retired"}}}}]`
				if !rootArray {
					data = `{"outline":` + data + `,"metadata":{"relationTo":"authors"}}`
				}
				if localized {
					data = `{"en":` + data + `,"fr":` + data + `}`
				}
				var value any
				if err := json.Unmarshal([]byte(data), &value); err != nil {
					t.Fatal(err)
				}
				if err := validateEmbeddedJSON(field, value, false); err != nil {
					t.Fatal(err)
				}
				changed, err := rewriteFieldCollectionReferences(value, field, false, "authors", "people")
				if err != nil || !changed {
					t.Fatalf("rewrite: %v, %v", changed, err)
				}
				check := func(value any) {
					if !rootArray {
						object := value.(map[string]any)
						if object["metadata"].(map[string]any)["relationTo"] != "people" {
							t.Fatal("nonstructural envelope reference skipped")
						}
						value = object["outline"]
					}
					nodes := value.([]any)
					if nodes[0].(map[string]any)["relationTo"] != "people" {
						t.Fatal("envelope reference skipped")
					}
					payload := nodes[1].(map[string]any)["data"].(map[string]any)
					if payload["target"].(map[string]any)["relationTo"] != "people" {
						t.Fatal("payload relationship skipped")
					}
					if payload["raw"].(map[string]any)["relationTo"] != "authors" {
						t.Fatal("opaque payload JSON rewritten")
					}
				}
				if localized {
					for _, candidate := range value.(map[string]any) {
						check(candidate)
					}
				} else {
					check(value)
				}
				if changed, err := rewriteFieldCollectionReferences(value, field, false, "authors", "people"); err != nil || changed {
					t.Fatalf("rewrite not idempotent: %v, %v", changed, err)
				}
			})
		}
	}
}

func TestEmbeddedReferenceRenamePropagatesLimits(t *testing.T) {
	field := postgresEmbeddedEvolutionFixture().Snapshot().Collections[0].Fields[0]
	field.Plugin.ReferenceKeys = []string{"relationTo"}
	var deep any = map[string]any{"relationTo": "authors"}
	for i := 0; i < embedded.MaxDepth; i++ {
		deep = map[string]any{"nested": deep}
	}
	value := map[string]any{"outline": []any{map[string]any{"kind": "link", "relationTo": "authors"}}, "metadata": deep}
	original, _ := json.Marshal(value)
	if changed, err := rewriteFieldCollectionReferences(value, field, false, "authors", "people"); err == nil || changed {
		t.Fatalf("limit silently swallowed: %v, %v", changed, err)
	}
	result, _ := json.Marshal(value)
	if string(original) != string(result) {
		t.Fatal("failed rewrite mutated its input")
	}
}
