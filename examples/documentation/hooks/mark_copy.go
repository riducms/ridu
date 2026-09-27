package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func markCopy(ctx ridu.HookContext) error {
	// Data holds the values copied from the original article.
	if title, ok := ctx.Data["title"].StringValue(); ok {
		ctx.Data["title"] = store.String(title + " (copy)")
	}
	// The copy should not replace the original on the homepage.
	ctx.Data["featured"] = store.Boolean(false)
	return nil
}
