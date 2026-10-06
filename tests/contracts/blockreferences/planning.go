package blockreferences

import (
	"fmt"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
)

// PlanningChange is a block-definition schema change that migration planning
// applies at every placement of the definitions it touches.
type PlanningChange struct {
	Name   string
	Before ridu.Config
	After  ridu.Config
}

// PlanningChanges returns the block-definition changes that cost tests plan
// on LayeredConfig(layers, width), whose placements grow exponentially with
// layers while its definitions grow linearly: a text field added to every
// block, a leaf's field renamed, a leaf's array field made required, and a
// new block with a relationship added to the lowest container layer.
func PlanningChanges(layers, width int) []PlanningChange {
	edit := func(change func(blocks []field.Block) []field.Block) ridu.Config {
		config := LayeredConfig(layers, width)
		blocks := make([]field.Block, len(config.Blocks))
		for index, block := range config.Blocks {
			block.Fields = append(field.Fields(nil), block.Fields...)
			blocks[index] = block
		}
		config.Blocks = change(blocks)
		return config
	}
	return []PlanningChange{
		{Name: "add-field", Before: LayeredConfig(layers, width), After: edit(func(blocks []field.Block) []field.Block {
			for index := range blocks {
				blocks[index].Fields = append(blocks[index].Fields, field.Text("note"))
			}
			return blocks
		})},
		{Name: "rename-field", Before: LayeredConfig(layers, width), After: edit(func(blocks []field.Block) []field.Block {
			blocks[0].Fields[0] = field.Text("headline").Required()
			return blocks
		})},
		{Name: "require-nested", Before: LayeredConfig(layers, width), After: edit(func(blocks []field.Block) []field.Block {
			blocks[0].Fields[4] = field.Array("links", field.Fields{field.Text("label").Required()})
			return blocks
		})},
		{Name: "add-block", Before: LayeredConfig(layers, width), After: edit(func(blocks []field.Block) []field.Block {
			extra := field.Block{Slug: "extra", Fields: field.Fields{field.Text("label"), field.Relationship("link", "media")}}
			leaves := make([]string, width)
			for index := range leaves {
				leaves[index] = fmt.Sprintf("leaf-%d", index)
			}
			for index, block := range blocks {
				if block.Slug == "layer-1-0" {
					blocks[index].Fields[1] = field.Blocks("children").References(append(leaves, extra.Slug)...)
				}
			}
			return append(blocks, extra)
		})},
	}
}
