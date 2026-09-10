package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var ArticleTitle = field.Text("title").
	Required().
	Localized().
	DefaultFrom(initialTitle)

func initialTitle(
	ctx operation.DefaultContext,
) (operation.Value[string], error) {
	// Locale is the content language, not the admin interface language.
	if ctx.Locale == "fr" {
		// Present supplies the value; nil means the callback succeeded.
		return operation.Present("Sans titre"), nil
	}
	return operation.Present("Untitled"), nil
}
