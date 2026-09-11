package richtext_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func blockConfig(def field.Node) ridu.Config {
	return ridu.Config{Name: "Rich text blocks", Plugins: []ridu.Plugin{richtext.New()}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{
		def,
	}}}}
}

func TestBlockConfigurationUsesExecutableSchemas(t *testing.T) {
	block := field.Block{Slug: "callout", Fields: field.Fields{
		field.Text("title").Required(),
	}}
	for _, test := range []struct {
		name   string
		config richtext.Config
		want   string
	}{
		{"implicit", richtext.Config{Blocks: []field.Block{block}}, ""},
		{"explicit", richtext.Config{Features: []richtext.Feature{richtext.FeatureBlocks}, Blocks: []field.Block{block}}, ""},
		{"disabled", richtext.Config{Features: []richtext.Feature{}, Blocks: []field.Block{block}}, "explicitly disables"},
		{"bare", richtext.Config{Features: []richtext.Feature{richtext.FeatureBlocks}}, "requires explicit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := richtext.Field("body", test.config)
			if strings.Contains(string(field.Snapshot(definition).PluginConfig()), "callout") || strings.Contains(string(field.Snapshot(definition).PluginConfig()), "title") {
				t.Fatal("executable definitions leaked into settings")
			}
			manifest, err := ridu.Resolve(blockConfig(definition))
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			embedded := manifest.Snapshot().Collections[0].Fields[0].Plugin.EmbeddedTrees
			if len(embedded) != 1 || embedded[0].Cases[0].ResolvedTypes()[0].Slug != "callout" {
				t.Fatal(embedded)
			}
		})
	}
	for _, reserved := range []string{"_key", "blockType"} {
		_, err := ridu.Resolve(blockConfig(richtext.Field("body", richtext.Config{Blocks: []field.Block{field.Block{Slug: "callout", Fields: field.Fields{
			field.Text(reserved),
		}}}})))
		if err == nil || !strings.Contains(err.Error(), reserved) {
			t.Fatalf("reserved %q: %v", reserved, err)
		}
	}
}

func TestBlockEnvelopeValidationHasExactPaths(t *testing.T) {
	app, err := ridu.New(blockConfig(richtext.Field("body", richtext.Config{Blocks: []field.Block{field.Block{Slug: "callout", Fields: field.Fields{
		field.Text("title").Required(),
	}}}})), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		node string
		path string
	}{
		{"legacy", `{"type":"block","version":1,"blockType":"callout","fields":{"blockType":"callout","title":"hello"}}`, "body.root.children.0.blockType"},
		{"children", `{"type":"block","version":1,"children":[],"fields":{"blockType":"callout","title":"hello"}}`, "body.root.children.0.children"},
		{"version", `{"type":"block","version":2,"fields":{"blockType":"callout","title":"hello"}}`, "body.root.children.0.version"},
		{"undeclared", `{"type":"block","version":1,"fields":{"blockType":"missing","title":"hello"}}`, "body.root.children.0.fields.blockType"},
		{"unknown-field", `{"type":"block","version":1,"fields":{"blockType":"callout","title":"hello","extra":true}}`, "body.root.children.0.fields.extra"},
		{"missing-payload", `{"type":"block","version":1}`, "body.root.children.0.fields"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var node store.Value
			if err := json.Unmarshal([]byte(test.node), &node); err != nil {
				t.Fatal(err)
			}
			_, err := app.Local().Create(context.Background(), "pages", store.Values{"body": document(node)}, ridu.MutationOptions{})
			if err == nil {
				t.Fatal("malformed node accepted")
			}
			var validation *schema.ValidationError
			if errors.As(err, &validation) {
				for _, issue := range validation.Issues {
					if issue.Path == test.path {
						return
					}
				}
			}
			// Operation errors wrap the stable issue list; inspect their JSON transport shape.
			encoded, _ := json.Marshal(err)
			if !strings.Contains(string(encoded), test.path) {
				t.Fatalf("missing exact path %s: %v %s", test.path, err, encoded)
			}
		})
	}
}

func TestPlainRichTextRejectsBlocksAndRetainsRecoveryBoundary(t *testing.T) {
	app, err := ridu.New(blockConfig(richtext.Field("body")), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	var node store.Value
	json.Unmarshal([]byte(`{"type":"block","version":1,"fields":{"blockType":"undeclared","_key":"historical","secret":"do not leak"}}`), &node)
	if _, err := app.Local().Create(context.Background(), "pages", store.Values{"body": document(node)}, ridu.MutationOptions{}); err == nil {
		t.Fatal("plain field accepted undeclared payload")
	}
}
