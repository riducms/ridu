package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Users = ridu.Collection{
	Slug: "users",
	Auth: true,
	Fields: field.Fields{
		field.Email("email").Required().Unique(),
	},
}
