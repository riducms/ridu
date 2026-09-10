package mongodb

import (
	"encoding/json"
	"fmt"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"strings"
	"testing"
)

func mongoEmbeddedFixture() schema.Field {
	path, _ := query.ParsePath("canvas")
	childPath, _ := query.ParsePath("canvas.parts.widget.card.title")
	child := schema.Field{ID: "title", Name: "title", Path: childPath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Required: true}
	tree := schema.EmbeddedTree{Version: 1, Key: "parts", Root: []string{"document"}, Children: "items", Tag: "kind", Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "attributes", Identity: "uid", Discriminator: "variant", Types: []schema.BlockType{{Slug: "card", Fields: []schema.Field{child}}}}}}
	return schema.Field{ID: "canvas", Name: "canvas", Path: path, Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin, Plugin: &schema.PluginField{Key: "canvas", EmbeddedTrees: []schema.EmbeddedTree{tree}}}
}
func TestMongoEmbeddedAdmissionAndEvolution(t *testing.T) {
	field := mongoEmbeddedFixture()
	if err := validateMongoFieldEnvelope([]schema.Field{field}, nil); err != nil {
		t.Fatal(err)
	}
	payload := store.Values{"uid": store.String("one"), "variant": store.String("card"), "title": store.String("Title")}
	value := func() store.Values {
		return store.Values{"canvas": store.Object(store.Values{"document": store.Object(store.Values{"kind": store.String("widget"), "attributes": store.Object(payload)})})}
	}
	collection := schema.Collection{ID: "pages", Fields: []schema.Field{field}}
	if err := validateCompleteValues(collection, value()); err != nil {
		t.Fatal(err)
	}
	delete(payload, "title")
	if err := validateCompleteValues(collection, value()); err == nil || !strings.Contains(err.Error(), "canvas.document.attributes.title") {
		t.Fatalf("missing required child: %v", err)
	}
	payload["title"] = store.String("Title")
	payload["variant"] = store.String("unknown")
	if err := validateCompleteValues(collection, value()); err == nil || !strings.Contains(err.Error(), "canvas.document.attributes.variant") {
		t.Fatalf("unknown variant: %v", err)
	}
	next := mongoEmbeddedFixture()
	next.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].Fields = append(next.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields(), schema.Field{ID: "caption", Name: "caption", Type: schema.FieldTypeText})
	if err := validateMongoDBAdditiveFields("pages", []schema.Field{field}, []schema.Field{next}); err != nil {
		t.Fatal(err)
	}
	next.Plugin.EmbeddedTrees[0].Root = []string{"different"}
	if err := validateMongoDBAdditiveFields("pages", []schema.Field{field}, []schema.Field{next}); err == nil || !strings.Contains(err.Error(), "data migration") {
		t.Fatalf("envelope change: %v", err)
	}
}

func TestMongoEmbeddedReferenceRenameComposesEnvelopeAndPayloads(t *testing.T) {
	for _, rootArray := range []bool{false, true} {
		for _, localized := range []bool{false, true} {
			t.Run(fmt.Sprintf("array=%v/localized=%v", rootArray, localized), func(t *testing.T) {
				field := mongoEmbeddedFixture()
				field.Localized = localized
				field.Plugin.ReferenceKeys = []string{"relationTo"}
				field.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].Fields = []schema.Field{
					{Name: "target", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{Polymorphic: true}},
					{Name: "raw", Type: schema.FieldTypeJSON},
				}
				if rootArray {
					field.Plugin.EmbeddedTrees[0].Root = []string{}
				}
				data := `[{"kind":"link","relationTo":"authors"},{"kind":"widget","attributes":{"variant":"card","uid":"a","target":{"relationTo":"authors","id":"one"},"raw":{"relationTo":"authors","kind":"widget","attributes":{"variant":"retired"}}}}]`
				if !rootArray {
					data = `{"document":` + data + `,"metadata":{"relationTo":"authors"}}`
				}
				if localized {
					data = `{"en":` + data + `,"fr":` + data + `}`
				}
				var value store.Value
				if err := json.Unmarshal([]byte(data), &value); err != nil {
					t.Fatal(err)
				}
				values := store.Values{field.Name: value}
				if changed, err := rewriteMongoCollectionReferences([]schema.Field{field}, values, "authors", "people"); err != nil || !changed {
					t.Fatalf("rewrite: %v, %v", changed, err)
				}
				check := func(value store.Value) {
					if !rootArray {
						object, _ := value.CopyObject()
						metadata, _ := object["metadata"].CopyObject()
						if got, _ := metadata["relationTo"].StringValue(); got != "people" {
							t.Fatal("nonstructural envelope reference skipped")
						}
						value = object["document"]
					}
					nodes, _ := value.CopyList()
					link, _ := nodes[0].CopyObject()
					if got, _ := link["relationTo"].StringValue(); got != "people" {
						t.Fatal("envelope reference skipped")
					}
					node, _ := nodes[1].CopyObject()
					payload, _ := node["attributes"].CopyObject()
					target, _ := payload["target"].CopyObject()
					if got, _ := target["relationTo"].StringValue(); got != "people" {
						t.Fatal("payload relationship skipped")
					}
					raw, _ := payload["raw"].CopyObject()
					if got, _ := raw["relationTo"].StringValue(); got != "authors" {
						t.Fatal("opaque payload JSON rewritten")
					}
				}
				if localized {
					locales, _ := values[field.Name].CopyObject()
					for _, candidate := range locales {
						check(candidate)
					}
				} else {
					check(values[field.Name])
				}
				if changed, err := rewriteMongoCollectionReferences([]schema.Field{field}, values, "authors", "people"); err != nil || changed {
					t.Fatalf("rewrite not idempotent: %v, %v", changed, err)
				}
			})
		}
	}
}

func TestMongoEmbeddedReferenceRenamePropagatesLimits(t *testing.T) {
	field := mongoEmbeddedFixture()
	field.Plugin.ReferenceKeys = []string{"relationTo"}
	deep := store.Object(store.Values{"relationTo": store.String("authors")})
	for i := 0; i < embedded.MaxDepth; i++ {
		deep = store.Object(store.Values{"nested": deep})
	}
	value := store.Object(store.Values{"document": store.Object(store.Values{"kind": store.String("link"), "relationTo": store.String("authors")}), "metadata": deep})
	values := store.Values{field.Name: value}
	original, _ := json.Marshal(values)
	if changed, err := rewriteMongoCollectionReferences([]schema.Field{field}, values, "authors", "people"); err == nil || changed {
		t.Fatalf("limit silently swallowed: %v, %v", changed, err)
	}
	result, _ := json.Marshal(values)
	if string(original) != string(result) {
		t.Fatal("failed rewrite mutated its input")
	}
}
