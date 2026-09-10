package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var SiteSettings = ridu.Global{
	Slug: "site-settings",
	Fields: field.Fields{
		field.Text("siteName"),
		field.Text("lastEditedBy"),
	},
	Hooks: ridu.CollectionHooks{
		// The same helper works: global saves use operation.Update.
		BeforeChange: []ridu.Hook{recordLastEditor},
	},
}
