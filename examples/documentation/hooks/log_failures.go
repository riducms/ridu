package content

import (
	"log"

	"github.com/riducms/ridu"
)

func logFailure(ctx ridu.HookContext) error {
	log.Printf(
		"Ridu %s failed (collection=%s global=%s): %v",
		ctx.Operation, ctx.CollectionID, ctx.GlobalID, ctx.Error,
	)
	// Finish logging; the original failure still reaches the caller.
	return nil
}
