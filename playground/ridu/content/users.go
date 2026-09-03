package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Users = ridu.Collection{
	Slug: "users",
	Auth: true,
	Access: ridu.CollectionAccess{
		Admin:  authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: []field.Definition{
		field.Text("email", field.Required(), field.Unique()),
	},
}

func authenticatedOnly(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}
