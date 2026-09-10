package core_test

import (
	"bytes"
	"encoding/json"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
	"testing"
)

func TestLocalRowLabelsResolveWithoutPairedPluginAndRoundTrip(t *testing.T) {
	options := store.Values{"title": store.String("Heading")}
	component := field.Component("app:summary", store.Object(options))
	options["title"] = store.String("changed")
	manifest, err := ridu.Resolve(ridu.Config{Name: "Labels", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Array("rows", field.Fields{field.Text("title").Required()}).Admin(field.Admin{RowLabelPath: "title", RowLabel: component}), field.Blocks("blocks", field.Block{Slug: "copy", Fields: field.Fields{field.Text("title")}}).Admin(field.Admin{RowLabel: component})}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range manifest.Snapshot().Collections[0].Fields {
		label := f.Nested.RowLabelComponent
		if label.Reference != "app:summary" || label.Plugin != "" || label.Component != "" || string(label.Config) != `{"title":"Heading"}` {
			t.Fatalf("bad label %#v", label)
		}
		label.Config[2] = 'Y'
	}
	before, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := schema.Parse(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(parsed)
	if err != nil || !bytes.Equal(before, after) || !bytes.Contains(after, []byte(`"title":"Heading"`)) {
		t.Fatal("row label roundtrip or isolation failed", err)
	}
}

func TestLocalRowLabelsUnifiedEmbeddedOccurrencesResolve(t *testing.T) {
	rows := field.Array("rows", field.Fields{field.Text("title")}).Admin(field.Admin{RowLabel: field.Component("app:summary", store.Object(store.Values{"prefix": store.String("Row")}))})
	manifest, err := ridu.Resolve(ridu.Config{Name: "Embedded labels", Plugins: []ridu.Plugin{outline.Plugin{}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		outline.Field("body", field.Block{Slug: "card", Fields: field.Fields{field.Snapshot(rows)}}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	embedded := schema.EmbeddedBlocks(manifest.Snapshot().Collections[0].Fields[0])[0].Blocks.ResolvedTypes()[0].ResolvedFields()
	if len(embedded) != 1 || embedded[0].Nested.RowLabelComponent.Reference != "app:summary" || string(embedded[0].Nested.RowLabelComponent.Config) != `{"prefix":"Row"}` {
		t.Fatalf("embedded row label projection: %#v", embedded)
	}
}
