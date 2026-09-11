package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var AuthorName = field.Text("authorName").DefaultFrom(initialAuthorName)

func initialAuthorName(
	ctx operation.Context) (operation.Value[string], error) {
	// Anonymous requests have no signed-in user to copy a name from.
	if ctx.Actor.ID == "" {
		return operation.Empty[string](), nil
	}
	name, present := ctx.Actor.Data.String("displayName")
	if !present || name == "" {
		// Leave this optional field empty when the account has no name.
		return operation.Empty[string](), nil
	}
	return operation.Present(name), nil
}
