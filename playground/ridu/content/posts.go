package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: []field.Definition{
		field.Text("title", field.Label("let's win today interesting"), field.Required()),
		field.Select(
			"status",
			field.OneOf("draft", "published"),
			field.Default("draft"),
		),
		field.Relationship("author", field.To("users")),
		richtext.Field("content"),
	},
}
