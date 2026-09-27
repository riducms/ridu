package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func fillExcerpt(ctx ridu.HookContext) error {
	if ctx.Operation != operation.Create {
		return nil
	}
	if excerpt, _ := ctx.Data["excerpt"].StringValue(); excerpt != "" {
		return nil // The author wrote one.
	}
	body, _ := ctx.Data["body"].StringValue()
	words := strings.Fields(body)
	if len(words) > 30 {
		words = words[:30]
	}
	// Required() is checked after this hook, so the excerpt passes.
	ctx.Data["excerpt"] = store.String(strings.Join(words, " "))
	return nil
}
