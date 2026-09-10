package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "readingMinutes"},
	},
	Fields: field.Fields{
		field.Text("title").
			Required().
			Admin(field.Admin{
				Editor: field.Component("app:titleCounter"),
			}),
		field.Number("readingMinutes"),
	},
}
