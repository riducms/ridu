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
	Fields: field.Fields{
		field.Text("title").Required().Admin(field.Admin{Editor: field.Component("app:text")}),
		field.Select("status", "draft", "published").Default("draft"),
		field.Relationship("author", "users"),
		richtext.Field("content"),
	},
}
