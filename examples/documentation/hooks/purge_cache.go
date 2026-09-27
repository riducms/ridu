package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
)

// purgeCache stands in for your CDN client's purge call.
var purgeCache = func(ctx context.Context, path string) error {
	return nil
}

func purgeArticleCache(ctx ridu.HookContext) error {
	if ctx.Operation == operation.Read {
		return nil // AfterCommit also runs after reads.
	}
	slug, _ := ctx.Document.Values["slug"].StringValue()
	// The article is already saved. An error here is reported to the
	// caller, but it cannot undo the save.
	return purgeCache(ctx.Context, "/articles/"+slug)
}
