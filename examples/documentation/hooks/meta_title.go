package content

import "github.com/riducms/ridu"

func fillMetaTitle(ctx ridu.HookContext) error {
	values := ctx.Document.Values
	if metaTitle, _ := values["metaTitle"].StringValue(); metaTitle != "" {
		return nil
	}
	// This changes the response only. The stored metaTitle stays
	// empty, so it keeps following the title when the title changes.
	values["metaTitle"] = values["title"]
	return nil
}
