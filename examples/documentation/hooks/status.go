package content

import (
	"log"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func logStatusChange(
	ctx operation.Context,
	value operation.Value[string],
) error {
	status, _ := value.Get()
	// Prior holds the values saved before this operation.
	previous, _ := ctx.Prior.String("status")
	if ctx.Operation == operation.Read || status == previous {
		return nil
	}
	// The change is committed, so this never logs a rolled-back save.
	log.Printf("status %q -> %q by user %q",
		previous, status, ctx.Actor.ID)
	return nil
}

var Status = field.Select("status", "draft", "review", "approved").
	Hooks(field.Hooks[string]{
		AfterCommit: []field.Observer[string]{logStatusChange},
	})
