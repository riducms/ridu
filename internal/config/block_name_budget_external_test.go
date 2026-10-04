package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func TestAutomaticBlockNameCountsTowardResolvedPlacementBudget(t *testing.T) {
	for _, test := range []struct {
		name       string
		depth      int
		overBudget bool
	}{
		{name: "nearby valid graph", depth: 14},
		{name: "automatic names exceed budget", depth: 15, overBudget: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			blocks := []field.Block{{Slug: "leaf", Fields: field.Fields{field.Text("title")}}}
			prior := "leaf"
			for level := 1; level <= test.depth; level++ {
				slug := fmt.Sprintf("level-%d", level)
				blocks = append(blocks, field.Block{Slug: slug, Fields: field.Fields{
					field.Blocks("left").References(prior),
					field.Blocks("right").References(prior),
				}})
				prior = slug
			}
			config := ridu.Config{
				Name: "Block name placement budget", Blocks: blocks,
				Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References(prior)}}},
			}
			manifest, err := ridu.Resolve(config)
			if test.overBudget {
				if err == nil || !strings.Contains(err.Error(), "100000") {
					t.Fatalf("Resolve error = %v, want resolved placement budget failure", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := manifest.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := schema.Parse(encoded)
				if err != nil || !parsed.Equal(manifest) {
					t.Fatalf("resolved schema does not parse unchanged: %v", err)
				}
				snapshot := manifest.Snapshot()
				if len(snapshot.Blocks) != test.depth+1 || len(snapshot.Collections[0].Fields[0].Blocks.Types) != 0 {
					t.Fatal("resolved registry or root placement was expanded")
				}
				for index, block := range snapshot.Blocks {
					wantFields := 3
					if index == 0 {
						wantFields = 2
					}
					if len(block.Fields) != wantFields || block.Fields[wantFields-1].Name != "blockName" {
						t.Fatalf("compact block %q fields = %#v", block.Slug, block.Fields)
					}
					for _, child := range block.Fields {
						if child.Blocks != nil && len(child.Blocks.Types) != 0 {
							t.Fatalf("registered child %q was expanded", child.Name)
						}
					}
				}
			}
			for index, block := range blocks {
				wantFields := 2
				if index == 0 {
					wantFields = 1
				}
				if len(block.Fields) != wantFields {
					t.Fatalf("authored block %q was mutated: %#v", block.Slug, block.Fields)
				}
			}
			root := field.Snapshot(config.Collections[0].Fields[0])
			if len(root.Blocks()) != 0 || len(root.BlockReferences()) != 1 || root.BlockReferences()[0] != prior {
				t.Fatal("authored root reference was expanded or changed")
			}
		})
	}
}
