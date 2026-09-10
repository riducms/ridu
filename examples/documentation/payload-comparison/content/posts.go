package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func signedIn(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	// Actor is nil when no user is signed in.
	if ctx.Actor == nil {
		// Denying access is a decision; nil means no execution error.
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

var Posts = ridu.Collection{
	Slug: "posts",
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "author", "updatedAt"},
	},
	Fields: field.Fields{
		field.Text("title").Required().MaxLength(120),
		field.Text("slug").Required().Unique().Index(),
		field.Textarea("summary"),
		// Store the user's ID. Ownership permissions come later.
		field.Relationship("author", "users").Required(),
	},
	Access: ridu.CollectionAccess{
		Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Allow(), nil
		},
		Create: signedIn,
		Update: signedIn,
		Delete: signedIn,
	},
}
