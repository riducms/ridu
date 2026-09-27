package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func countWords(ctx ridu.HookContext) error {
	switch ctx.Operation {
	case operation.Create, operation.Duplicate, operation.Update,
		operation.Publish, operation.Unpublish:
	default:
		return nil // BeforeOperation also runs for reads and deletes.
	}
	body, sent := ctx.Data["body"].StringValue()
	if !sent {
		return nil // This update does not change the body.
	}
	// Every BeforeChange hook has run, so this is the final body.
	words := len(strings.Fields(body))
	ctx.Data["wordCount"] = store.Number(float64(words))
	return nil
}
