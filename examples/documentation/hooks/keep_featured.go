package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/operation"
)

func keepFeatured(ctx ridu.HookContext) error {
	// Original is the saved article that is about to be deleted.
	featured, _ := ctx.Original.Values["featured"].BooleanValue()
	if featured {
		// Reject shows this message to the editor and stops the delete.
		return ridu.Reject(
			"Remove the article from the homepage first.",
			operation.Issue{
				Code:    "featured",
				Message: "Untick Featured, then delete the article.",
				Target:  operation.At("featured"),
			},
		)
	}
	return nil
}
