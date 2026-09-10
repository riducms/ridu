package core_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func TestEmbeddedMetadataResolutionAndImmutability(t *testing.T) {
	tree := field.EmbeddedTree{Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind", Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []field.Block{field.Block{Slug: "card", Fields: field.Fields{field.Text("title").Required()}}}}}}
	config := func(trees ...field.EmbeddedTree) core.Config {
		return core.Config{Name: "metadata", Plugins: []core.Plugin{outline.Plugin{}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Plugin("body", outline.Key, json.RawMessage(`{}`)).EmbeddedTrees(trees...)}}}}
	}
	cfg := config(tree)
	tree.Root[0] = "mutated"
	manifest, err := core.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	f := snapshot.Collections[0].Fields[0]
	if got := f.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[0].Path.String(); got != "body.widgets.widget.card.title" {
		t.Fatal(got)
	}
	if string(f.Plugin.Config) != "{}" {
		t.Fatal("executable schema leaked into plugin config")
	}
	f.Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[0].Name = "mutated"
	if manifest.Snapshot().Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0].ResolvedFields()[0].Name != "title" {
		t.Fatal("manifest mutable")
	}
	encoded, _ := json.Marshal(manifest.Snapshot())
	if _, err := schema.Parse(encoded); err != nil {
		t.Fatal(err)
	}
	snapshot = manifest.Snapshot()
	snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Version = 99
	encoded, _ = json.Marshal(snapshot)
	if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("unsupported contract accepted: %v", err)
	}
	base := manifest.Snapshot().Collections[0].Fields[0].Plugin.EmbeddedTrees[0]
	tree.Root = []string{"outline"}
	for _, test := range []struct {
		name   string
		mutate func(*field.EmbeddedTree)
		path   string
	}{
		{"root", func(t *field.EmbeddedTree) { t.Root = []string{"outline.*"} }, "root[0]"},
		{"payload overlap", func(t *field.EmbeddedTree) { t.Cases[0].Payload = "items" }, "cases[0]"},
		{"duplicate tags", func(t *field.EmbeddedTree) { t.Cases = append(t.Cases, t.Cases[0]) }, "cases[1]"},
		{"reserved child", func(t *field.EmbeddedTree) {
			t.Cases[0].Types = []field.Block{field.Block{Slug: "card", Fields: field.Fields{field.Text("schema")}}}
		}, "fields"},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := field.EmbeddedTree{Key: base.Key, Root: []string{"outline"}, Children: base.Children, Tag: base.Tag, Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []field.Block{field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}}}}}
			test.mutate(&copy)
			_, err := core.Resolve(config(copy))
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %s: %v", test.path, err)
			}
		})
	}
	other := tree
	other.Key = "other"
	other.Root = []string{"outline", "items"}
	if _, err := core.Resolve(config(tree, other)); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("overlapping roots: %v", err)
	}
}

func TestEmbeddedSchemaDepthBound(t *testing.T) {
	var child field.Node = field.Text("title")
	for range 52 {
		child = field.Group("child", field.Fields{child})
	}
	_, err := core.Resolve(core.Config{Name: "deep", Plugins: []core.Plugin{outline.Plugin{}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{outline.Field("body", field.Block{Slug: "card", Fields: field.Fields{child}})}}}})
	if err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("unbounded schema: %v", err)
	}
}

func TestEmbeddedGenericSelectorsRequireDeclaredPayload(t *testing.T) {
	_, err := core.Resolve(core.Config{Name: "missing payload", Plugins: []core.Plugin{outline.Plugin{}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Plugin("body", outline.Key, json.RawMessage(`{}`))}}}})
	if err == nil || !strings.Contains(err.Error(), "embeddedTypes[0]") {
		t.Fatalf("generic payload selector with no descriptor was admitted: %v", err)
	}
}
