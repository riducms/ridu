package content

import (
	"context"

	"github.com/riducms/ridu"
)

// purgeCache stands in for your CDN client's purge call.
var purgeCache = func(ctx context.Context, path string) error {
	return nil
}

func purgeArticleCache(ctx ridu.HookContext) error {
	// AfterCommit runs after every committed change, including deletes.
	slug, _ := ctx.Document.Values["slug"].StringValue()
	// The article is already saved. An error here is reported to the
	// caller, but it cannot undo the save.
	return purgeCache(ctx.Context, "/articles/"+slug)
}
