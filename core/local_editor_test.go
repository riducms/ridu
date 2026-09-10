package core_test

import (
	"bytes"
	"encoding/json"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func TestLocalEditorResolutionAndRoundTrip(t *testing.T) {
	options := store.Values{"palette": store.List(store.String("red"))}
	editor := field.Component("app:color", store.Object(options))
	options["palette"] = store.List(store.String("changed"))
	manifest, err := ridu.Resolve(ridu.Config{Name: "Editors", Collections: []ridu.Collection{{Slug: "brands", Fields: field.Fields{field.Text("accent").Required().Admin(field.Admin{Editor: editor}), field.Array("rows", field.Fields{field.Number("score").Admin(field.Admin{Editor: field.Component("app:score")})})}}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	accent := snapshot.Collections[0].Fields[0]
	if accent.Type != schema.FieldTypeText || !accent.Required || accent.Admin.Editor.Reference != "app:color" || string(accent.Admin.Editor.Config) != `{"palette":["red"]}` {
		t.Fatalf("editor changed field semantics or config: %#v", accent)
	}
	accent.Admin.Editor.Config[2] = 'X'
	if string(manifest.Snapshot().Collections[0].Fields[0].Admin.Editor.Config) != `{"palette":["red"]}` {
		t.Fatal("manifest editor config is mutable")
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded schema.Manifest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(decoded)
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatalf("editor manifest round trip: %v", err)
	}
}

func TestLocalEditorRejectsMalformedDeclarations(t *testing.T) {
	for name, editor := range map[string]field.ComponentRef{
		"namespace":            field.Component("seo:color"),
		"path":                 field.Component("app:../color"),
		"scalar config":        field.Component("app:color", store.Boolean(true)),
		"null config":          field.Component("app:color", store.Null()),
		"explicit zero config": field.Component("app:color", store.Value{}),
		"multiple configs":     field.Component("app:color", store.Object(store.Values{}), store.Object(store.Values{})),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ridu.Resolve(ridu.Config{Name: "Invalid editor", Collections: []ridu.Collection{{Slug: "brands", Fields: field.Fields{field.Text("accent").Admin(field.Admin{Editor: editor})}}}})
			if err == nil {
				t.Fatal("invalid editor resolved")
			}
		})
	}
}

func TestLocalEditorUnifiedEmbeddedOccurrencesResolve(t *testing.T) {
	editor := field.Text("title").Admin(field.Admin{Editor: field.Component("app:text", store.Object(store.Values{"capture": store.Boolean(true)}))})
	manifest, err := ridu.Resolve(ridu.Config{Name: "Embedded editor", Plugins: []ridu.Plugin{outline.Plugin{}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{outline.Field("body", field.Block{Slug: "card", Fields: field.Fields{field.Snapshot(editor)}})}}}})
	if err != nil {
		t.Fatal(err)
	}
	embedded := schema.EmbeddedBlocks(manifest.Snapshot().Collections[0].Fields[0])[0].Blocks.ResolvedTypes()[0].ResolvedFields()
	if len(embedded) != 1 || embedded[0].Admin.Editor == nil || embedded[0].Admin.Editor.Reference != "app:text" || string(embedded[0].Admin.Editor.Config) != `{"capture":true}` {
		t.Fatalf("embedded editor projection: %#v", embedded)
	}
}

func TestLocalEditorAbsentConfigurationStaysAbsentInDeterministicManifest(t *testing.T) {
	config := ridu.Config{Name: "Optional settings", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		field.Text("simple").Admin(field.Admin{Editor: field.Component("app:simple")}),
		field.Text("configured").Admin(field.Admin{Editor: field.Component("app:configured", store.Object(store.Values{}))}),
	}}}}
	first, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	fields := first.Snapshot().Collections[0].Fields
	if len(fields[0].Admin.Editor.Config) != 0 || string(fields[1].Admin.Editor.Config) != "{}" {
		t.Fatalf("lost omitted versus supplied configuration: %#v", fields)
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("nondeterministic optional config: %v", err)
	}
	if !bytes.Contains(a, []byte(`"editor":{"reference":"app:simple"}`)) || !bytes.Contains(a, []byte(`"editor":{"reference":"app:configured","config":{}}`)) {
		t.Fatalf("unexpected optional config serialization: %s", a)
	}
}
