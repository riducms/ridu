package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func suffixCopiedSlug(
	_ operation.Context,
	value operation.Value[store.Value],
) (operation.Change[store.Value], error) {
	raw, _ := value.Get()
	slug, ok := raw.StringValue()
	if !ok || slug == "" {
		return operation.Keep[store.Value](), nil
	}
	// Slugs are unique, so the copy needs its own.
	return operation.Replace(
		operation.Present(store.String(slug + "-copy")),
	), nil
}

var PageSlug = field.Slug("slug", "title").Hooks(field.Hooks[string]{
	BeforeDuplicate: []field.RawTransform{suffixCopiedSlug},
})
