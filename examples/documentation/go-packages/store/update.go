package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func RenamePageLink(
	ctx context.Context,
	app *ridu.App,
	page store.Document,
	label string,
	options ridu.MutationOptions,
) (store.Document, error) {
	links, err := RenameFirstLink(page.Values["links"], label)
	if err != nil {
		return store.Document{}, err
	}

	// Send the updated list. Omitted top-level fields stay unchanged.
	return app.Local().UpdateWithOptions(ctx, "pages", page.ID,
		store.Values{"links": links}, options,
	)
}
