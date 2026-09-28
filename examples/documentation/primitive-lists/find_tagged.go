package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func FindTaggedProducts(
	ctx context.Context,
	local *ridu.LocalAPI,
) (store.Page, error) {
	// Match either tag anywhere in the list, using exact values.
	tags := query.Field("tags")
	return local.List(ctx, "products", ridu.ListOptions{
		Where: query.In(tags, query.String("sale"), query.String("featured")),
		Page:  1,
		Limit: 20,
	})
}
