package content

import (
	"github.com/riducms/ridu"
	color "github.com/riducms/ridu/examples/documentation/custom-components/color"
	"github.com/riducms/ridu/field"
)

var Brands = ridu.Collection{
	Slug: "brands",
	Fields: field.Fields{
		color.Field("accent").Required(),
	},
}
