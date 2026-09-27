package content

import (
	"errors"

	"github.com/riducms/ridu"
)

func keepFeatured(ctx ridu.HookContext) error {
	// Original is the saved article that is about to be deleted.
	featured, _ := ctx.Original.Values["featured"].BooleanValue()
	if featured {
		return errors.New("remove the article from the homepage first")
	}
	return nil
}
