package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var NavigationFields = field.Fields{
	field.Group("navigation", field.Fields{
		field.Text("label").DefaultFrom(initialLinkLabel),
	}),
	field.Array("links", field.Fields{
		field.Text("url"),
		field.Text("label").DefaultFrom(initialLinkLabel),
	}),
}

func initialLinkLabel(
	ctx operation.DefaultContext,
) (operation.Value[string], error) {
	// Siblings reads this group or row, not another row in the array.
	url, present := ctx.Siblings.String("url")
	if present && url == "/about" {
		return operation.Present("About us"), nil
	}
	return operation.Present("New link"), nil
}
