package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func FindAffordableProducts(
	ctx context.Context,
	local *ridu.LocalAPI,
	maxPrice float64,
) (store.Page, error) {
	// List executes the filter and also applies collection read access.
	return local.List(ctx, "products", ridu.ListOptions{
		Where: AffordableProducts(maxPrice),
		Page:  1,
		Limit: 20,
	})
}
