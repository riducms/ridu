package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Pages = ridu.Collection{
	Slug: "pages",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Array("links", field.Fields{
			field.Text("label"),
			field.Text("url"),
		}).Admin(field.Admin{
			// Use the label text for screen readers and row actions.
			RowLabelPath: "label",
			RowLabel:     field.Component("app:linkSummary"),
		}),
	},
}
