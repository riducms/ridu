// Package blockreferences supplies paired inline/reference authoring fixtures.
package blockreferences

import (
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
)

func Config(references bool) ridu.Config {
	access := func(c operation.Context) (bool, error) {
		tenant, _ := c.Root.String("tenant")
		visible, _ := c.Siblings.String("visibility")
		return tenant == "open" && visible == "visible", nil
	}
	card := field.Block{Slug: "card", TypeName: "Card", Fields: field.Fields{field.Text("visibility"), field.Text("secret").Access(field.Access{Read: access, Update: access}), field.Group("details", field.Fields{field.Text("caption")}), field.Blocks("children", field.Block{Slug: "note", TypeName: "Note", Fields: field.Fields{field.Text("text")}})}}
	card.Fields = append(card.Fields, field.Text("controlled").Access(field.Access{Update: access}).Validate(
		func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
			if text, _ := value.Get(); text == "invalid" {
				return []operation.Issue{{Code: "controlled_value", Message: "Choose a valid controlled value"}}, nil
			}
			return nil, nil
		},
	))
	blockField := func(name string) field.BlocksField {
		if references {
			return field.Blocks(name).References("card")
		}
		return field.Blocks(name, card)
	}
	rich := richtext.Config{Blocks: []field.Block{card}}
	if references {
		rich = richtext.Config{BlockReferences: []string{"card"}}
	}
	config := ridu.Config{Name: "Block Registry", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("tenant"), blockField("layout"), blockField("sidebar"), richtext.Field("body", rich)}}, {Slug: "articles", Fields: field.Fields{field.Text("tenant"), blockField("layout")}}}, Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("tenant"), blockField("layout")}}}, Plugins: []ridu.Plugin{richtext.New()}}
	if references {
		config.Blocks = []field.Block{card}
	}
	return config
}
