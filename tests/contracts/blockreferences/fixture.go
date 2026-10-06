// Package blockreferences supplies paired inline/reference authoring fixtures.
package blockreferences

import (
	"fmt"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
)

// Card is declared once, so the inline and registered forms of the fixture
// select the same definition: a slug names one definition per application,
// and callbacks are identical only when shared from one value. Its slug is
// distinct from the plain "card" blocks of other contracts that share the
// browser fixture.
var Card = func() field.Block {
	access := func(c operation.Context) (bool, error) {
		tenant, _ := c.Root.String("tenant")
		visible, _ := c.Siblings.String("visibility")
		return tenant == "open" && visible == "visible", nil
	}
	return field.Block{Slug: CardSlug, TypeName: "RegistryCard", Labels: field.BlockLabels{Singular: "Card", Plural: "Cards"}, Fields: field.Fields{
		field.Text("visibility"),
		field.Text("secret").Access(field.Access{Read: access, Update: access}),
		field.Group("details", field.Fields{field.Text("caption")}),
		field.Blocks("children", field.Block{Slug: NoteSlug, TypeName: "RegistryNote", Labels: field.BlockLabels{Singular: "Note", Plural: "Notes"}, Fields: field.Fields{field.Text("text")}}),
		field.Text("controlled").Access(field.Access{Update: access}).Validate(
			func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
				if text, _ := value.Get(); text == "invalid" {
					return []operation.Issue{{Code: "controlled_value", Message: "Choose a valid controlled value"}}, nil
				}
				return nil, nil
			},
		),
	}}
}()

// CardSlug and NoteSlug are the stored discriminators of Card and its nested note.
const (
	CardSlug = "registry-card"
	NoteSlug = "registry-note"
)

func Config(references bool) ridu.Config {
	card := Card
	blockField := func(name string) field.BlocksField {
		if references {
			return field.Blocks(name).References(CardSlug)
		}
		return field.Blocks(name, card)
	}
	rich := richtext.Config{Blocks: []field.Block{card}}
	if references {
		rich = richtext.Config{BlockReferences: []string{CardSlug}}
	}
	config := ridu.Config{Name: "Block Registry", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("tenant"), blockField("layout"), blockField("sidebar"), richtext.Field("body", rich)}}, {Slug: "articles", Fields: field.Fields{field.Text("tenant"), blockField("layout")}}}, Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("tenant"), blockField("layout")}}}, Plugins: []ridu.Plugin{richtext.New()}}
	if references {
		config.Blocks = []field.Block{card}
	}
	return config
}

// LayeredConfig registers width leaf blocks and layers of width container
// blocks, each referencing every block of the layer below, used by a pages
// layout that references the top layer. Definitions grow linearly with layers
// while placements, the paths through the reference graph, grow exponentially:
// LayeredConfig(7, 3) has 24 definitions and about 62,000 field placements.
// Block processing that follows definitions stays small for it.
func LayeredConfig(layers, width int) ridu.Config {
	var blocks []field.Block
	var lower []string
	for index := range width {
		slug := fmt.Sprintf("leaf-%d", index)
		blocks = append(blocks, field.Block{Slug: slug, Fields: field.Fields{
			field.Text("heading").Required(), field.Number("count"), field.Select("tone", "quiet", "loud"),
			field.Relationship("media", "media"), field.Array("links", field.Fields{field.Text("label")}),
		}})
		lower = append(lower, slug)
	}
	for layer := 1; layer <= layers; layer++ {
		var current []string
		for index := range width {
			slug := fmt.Sprintf("layer-%d-%d", layer, index)
			blocks = append(blocks, field.Block{Slug: slug, Fields: field.Fields{
				field.Text("title"), field.Blocks("children").References(lower...),
			}})
			current = append(current, slug)
		}
		lower = current
	}
	return ridu.Config{
		Name: "Layered blocks", Blocks: blocks,
		Collections: []ridu.Collection{
			{Slug: "media", Fields: field.Fields{field.Text("title")}},
			{Slug: "pages", Fields: field.Fields{field.Text("title"), field.Blocks("layout").References(lower...)}},
		},
	}
}
