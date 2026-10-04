package core_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestRichTextBlocksEngineAcceptance(t *testing.T) {
	richtextblocks.Run(t, memoryRichTextBlocksFactory)
}

func TestRichTextBlockReferencesEngineAcceptance(t *testing.T) {
	richtextblocks.RunReferences(t, memoryRichTextBlocksFactory)
}

func TestBuiltInBlockNameDefaultsAcrossOrdinaryAndEmbeddedBlocks(t *testing.T) {
	block := field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}
	app, err := ridu.New(ridu.Config{
		Name: "Block names", Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
			field.Blocks("layout", block), richtext.Field("body", richtext.Config{Blocks: []field.Block{block}}),
		}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "pages", store.Values{
		"layout": store.List(store.Object(store.Values{"blockType": store.String("card"), "title": store.String("Ordinary")})),
		"body":   richtextblocks.Document(richtextblocks.Block("card", "", store.Values{"title": store.String("Embedded")})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := created.Values["layout"].CopyList()
	ordinary, _ := rows[0].CopyObject()
	if name, ok := ordinary["blockName"].StringValue(); !ok || name != "" {
		t.Fatalf("ordinary block name = %#v", ordinary["blockName"])
	}
	document, _ := created.Values["body"].CopyObject()
	root, _ := document["root"].CopyObject()
	nodes, _ := root["children"].CopyList()
	node, _ := nodes[0].CopyObject()
	embedded, _ := node["fields"].CopyObject()
	if name, ok := embedded["blockName"].StringValue(); !ok || name != "" {
		t.Fatalf("embedded block name = %#v", embedded["blockName"])
	}
}

func BenchmarkRichTextBlocksEnginePerformance(b *testing.B) {
	richtextblocks.RunPerformanceBenchmark(b, "memory", memoryRichTextBlocksBackend)
}

func memoryRichTextBlocksFactory(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
	return memoryRichTextBlocksBackend(t, config)
}

func memoryRichTextBlocksBackend(t testing.TB, config ridu.Config) (store.Store, *ridu.App) {
	t.Helper()
	backend := teststore.New()
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	return backend, app
}
