package schema

import (
	"bytes"
	"testing"

	"github.com/riducms/ridu/query"
)

func TestAdminProjectionPreservesSharedBlocksAndStorage(t *testing.T) {
	leaf := Field{
		Name: "title", Path: query.Field("title"), Type: FieldTypeText, Required: true,
		Admin: FieldAdmin{Hidden: true}, Text: &TextField{},
	}
	inline := BlockType{Slug: "inline", Admin: &BlockAdmin{RowLabel: "title"}, Fields: []Field{leaf}}
	manifest := NewManifest(Snapshot{
		Version: CurrentVersion, Application: Application{Name: "Projection"},
		Blocks: []BlockType{{
			Slug: "card", Admin: &BlockAdmin{RowLabel: "title"},
			Fields: []Field{{Name: "details", Path: query.Field("details"), Type: FieldTypeGroup, Nested: &NestedField{Fields: []Field{leaf}}}},
		}},
		Collections: []Collection{{
			ID: "posts", Slug: "posts", Admin: CollectionAdmin{Hidden: true},
			Fields: []Field{{Name: "content", Path: query.Field("content"), Type: FieldTypeBlocks, Blocks: &BlocksField{BlockReferences: []string{"card"}}}},
		}},
		Globals: []Global{{
			ID: "settings", Slug: "settings", Admin: CollectionAdmin{Hidden: true},
			Fields: []Field{{Name: "widgets", Path: query.Field("widgets"), Type: FieldTypePlugin, Plugin: &PluginField{EmbeddedTrees: []EmbeddedTree{{
				Cases: []EmbeddedTreeCase{{Types: []BlockType{inline}}, {BlockReferences: []string{"card"}}},
			}}}}},
		}},
	})
	before, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	projected := manifest.WithoutAdminPresentation()
	if !manifest.SameStorage(projected) {
		t.Fatal("admin presentation changed storage identity")
	}
	if manifest.Equal(projected) {
		t.Fatal("admin presentation was retained")
	}
	snapshot := projected.Snapshot()
	placed := snapshot.Collections[0].Fields[0].Blocks.ResolvedTypes()[0]
	if placed.Admin != nil || placed.ResolvedFields()[0].Nested.ResolvedFields()[0].Admin.Hidden {
		t.Fatal("registered block placements retained admin metadata")
	}
	cases := snapshot.Globals[0].Fields[0].Plugin.EmbeddedTrees[0].Cases
	if cases[0].ResolvedTypes()[0].Admin != nil || cases[0].ResolvedTypes()[0].Fields[0].Admin.Hidden ||
		cases[1].ResolvedTypes()[0].ResolvedFields()[0].Nested.ResolvedFields()[0].Admin.Hidden {
		t.Fatal("embedded block cases retained admin metadata")
	}
	snapshot.Blocks[0].Fields[0].Nested.Fields[0].Required = false
	if manifest.SameStorage(NewManifest(snapshot)) {
		t.Fatal("a storage change inside a shared block was ignored")
	}
	after, err := manifest.Bytes()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("projection mutated its source manifest: %v", err)
	}
}
