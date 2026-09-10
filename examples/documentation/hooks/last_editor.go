package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func recordLastEditor(ctx ridu.HookContext) error {
	// A nil actor means the request is anonymous.
	if ctx.Actor == nil {
		return nil
	}
	// Limit editor tracking to creates and ordinary updates.
	if ctx.Operation != operation.Create &&
		ctx.Operation != operation.Update {
		return nil
	}
	// Mutate Data entries to include them in the same save.
	ctx.Data["lastEditedBy"] = store.String(ctx.Actor.ID)
	return nil
}
