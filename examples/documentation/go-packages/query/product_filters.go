package content

import "github.com/riducms/ridu/query"

func AffordableProducts(maxPrice float64) (query.Expression, error) {
	price, err := query.NewPath("price")
	if err != nil {
		return nil, err
	}
	inStock, err := query.NewPath("inStock")
	if err != nil {
		return nil, err
	}

	// Describe both requirements; this does not read the database yet.
	return query.And(
		query.LessThanEqual(price, query.Number(maxPrice)),
		query.Equal(inStock, query.Boolean(true)),
	)
}
