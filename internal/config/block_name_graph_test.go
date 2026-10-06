package config

import (
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestBlockNameConditionBindsWithinItsDefinition(t *testing.T) {
	for _, placement := range []string{"inline", "registered", "embedded"} {
		for _, authored := range []bool{false, true} {
			name := placement + "/automatic"
			if authored {
				name = placement + "/authored"
			}
			t.Run(name, func(t *testing.T) {
				block := field.Block{Slug: "card", Fields: field.Fields{
					field.Text("caption").Admin(field.Admin{VisibleWhen: field.NotEqual(field.Sibling("blockName"), "")}),
				}}
				if authored {
					block.Fields = append(block.Fields, field.Text("blockName").Default("Draft"))
				}
				originalFieldCount := len(block.Fields)
				input := Input{Name: "Block name conditions", Collections: []Collection{{Slug: "pages"}}}
				switch placement {
				case "inline":
					input.Collections[0].Fields = field.Fields{field.Blocks("layout", block)}
				case "registered":
					input.Blocks = []field.Block{block}
					input.Collections[0].Fields = field.Fields{field.Blocks("layout").References("card")}
				case "embedded":
					input.Plugins = []Plugin{{Key: "outline"}}
					input.Collections[0].Fields = field.Fields{field.Plugin("body", "outline", json.RawMessage(`{}`)).EmbeddedTrees(field.EmbeddedTree{
						Key: "widgets", Root: []string{"outline"}, Children: "items", Tag: "kind",
						Cases: []field.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []field.Block{block}}},
					})}
				}
				_, graph, err := ResolveGraph(input)
				if err != nil {
					t.Fatal(err)
				}
				if len(block.Fields) != originalFieldCount {
					t.Fatal("resolution mutated the authored block definition")
				}

				var caption, blockName *Occurrence
				for _, occurrence := range graph.Occurrences() {
					candidate := occurrence
					switch {
					case candidate.ResourceKind == BlockResource && candidate.Resource == "card" && candidate.ResolvedPath == "caption":
						caption = &candidate
					case candidate.ResourceKind == BlockResource && candidate.Resource == "card" && candidate.ResolvedPath == "blockName":
						if blockName != nil {
							t.Fatal("duplicate blockName occurrence")
						}
						blockName = &candidate
					}
				}
				if caption == nil || blockName == nil || blockName.SchemaID == "" {
					t.Fatalf("missing graph fields or resolved schema ID: caption=%#v blockName=%#v", caption, blockName)
				}
				if len(caption.References) != 1 || caption.References[0].TargetID != blockName.ID || caption.References[0].ResolvedPath != blockName.ResolvedPath {
					t.Fatalf("caption condition did not bind blockName: %#v", caption.References)
				}
			})
		}
	}
}

func TestBlockNameConditionUsesAuthoredLayoutChild(t *testing.T) {
	_, graph, err := ResolveGraph(Input{Name: "Layout block name", Collections: []Collection{{Slug: "pages", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{
			field.Row(field.Fields{field.Text("blockName").Default("Draft")}),
			field.Text("caption").Admin(field.Admin{VisibleWhen: field.NotEqual(field.Sibling("blockName"), "")}),
		}}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	var names int
	for _, occurrence := range graph.Occurrences() {
		if occurrence.ResourceKind == BlockResource && occurrence.Resource == "card" && occurrence.ResolvedPath == "blockName" {
			names++
		}
	}
	if names != 1 {
		t.Fatalf("blockName graph occurrences = %d, want authored child only", names)
	}
}
