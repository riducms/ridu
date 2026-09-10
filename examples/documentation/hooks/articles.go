package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Articles = ridu.Collection{
	Slug: "articles",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Text("lastEditedBy"),
	},
	Hooks: ridu.CollectionHooks{
		// Run before saving so the editor ID is stored with the article.
		BeforeChange: []ridu.Hook{recordLastEditor},
	},
}
