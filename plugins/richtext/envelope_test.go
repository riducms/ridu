package richtext_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
)

func TestPortableEnvelopeAdmissionHasExactPaths(t *testing.T) {
	app, err := ridu.New(blockConfig(richtext.Field("body")), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, raw, path string }{
		{"root-kind", `{"version":1,"root":{"type":"paragraph","children":[]}}`, "body.root.type"},
		{"document-extra", `{"version":1,"extension":"historical","root":{"type":"root","children":[]}}`, "body.extension"},
		{"unknown-mode", `{"version":1,"root":{"type":"root","children":[{"type":"text","text":"Keep me","mode":"unsupported-mode"}]}}`, "body.root.children.0.mode"},
		{"unknown-format", `{"version":1,"root":{"type":"root","children":[{"type":"paragraph","children":[],"format":"unexpected-alignment"}]}}`, "body.root.children.0.format"},
		{"unknown-direction", `{"version":1,"root":{"type":"root","children":[],"direction":"sideways"}}`, "body.root.direction"},
		{"node-extra", `{"version":1,"root":{"type":"root","children":[{"type":"text","text":"Keep me","extension":"historical"}]}}`, "body.root.children.0.extension"},
		{"wrong-node-property", `{"version":1,"root":{"type":"root","children":[{"type":"text","text":"Keep me","caption":"historical"}]}}`, "body.root.children.0.caption"},
		{"root-version", `{"version":1,"root":{"type":"root","version":2,"children":[]}}`, "body.root.version"},
		{"text-version", `{"version":1,"root":{"type":"root","children":[{"type":"text","version":2,"text":"Keep me"}]}}`, "body.root.children.0.version"},
		{"null-version", `{"version":1,"root":{"type":"root","children":[{"type":"text","version":null,"text":"Keep me"}]}}`, "body.root.children.0.version"},
		{"text-format", `{"version":1,"root":{"type":"root","children":[{"type":"text","format":"bold","text":"Keep me"}]}}`, "body.root.children.0.format"},
		{"leaf-children", `{"version":1,"root":{"type":"root","children":[{"type":"text","text":"Keep me","children":[]}]}}`, "body.root.children.0.children"},
		{"nested-root", `{"version":1,"root":{"type":"root","children":[{"type":"root","children":[]}]}}`, "body.root.children.0.type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var value store.Value
			if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
				t.Fatal(err)
			}
			_, err := app.Local().Create(context.Background(), "pages", store.Values{"body": value}, ridu.MutationOptions{})
			encoded, _ := json.Marshal(err)
			if err == nil || !strings.Contains(string(encoded), test.path) {
				t.Fatalf("expected exact path %s: %v %s", test.path, err, encoded)
			}
		})
	}
}

func TestPortableLexicalPropertiesSurviveAdmissionAndTypedCodec(t *testing.T) {
	app, err := ridu.New(blockConfig(richtext.Field("body")), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"version":1,"root":{"type":"root","version":1,"direction":null,"format":"","indent":0,"children":[{"type":"paragraph","version":1,"textFormat":1,"textStyle":"color: blue","children":[]},{"type":"code","version":1,"language":"go","theme":"night","textFormat":1,"textStyle":"color: blue","children":[{"type":"text","version":1,"text":"hello","format":1,"detail":0,"mode":"normal","style":""}]},{"type":"link","version":1,"url":"/hello","target":null,"rel":null,"title":null,"children":[]}]}}`
	var value store.Value
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(context.Background(), "pages", store.Values{"body": value}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(created.Values["body"])
	var typed richtext.Document[struct{}]
	if err := json.Unmarshal(data, &typed); err != nil {
		t.Fatal(err)
	}
	if typed.Root.Children[0].TextStyle != "color: blue" || typed.Root.Children[1].Theme != "night" {
		t.Fatal("portable properties were dropped")
	}
	for _, raw := range []string{`{"type":"text","version":2,"text":"x"}`, `{"type":"text","version":null,"text":"x"}`, `{"type":"text","version":0,"text":"x"}`} {
		var node richtext.Node[struct{}]
		if err := json.Unmarshal([]byte(raw), &node); err == nil {
			t.Fatalf("invalid node version decoded: %s", raw)
		}
	}
}
