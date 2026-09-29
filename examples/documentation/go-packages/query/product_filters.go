package content

import "github.com/riducms/ridu/query"

func AffordableProducts(maxPrice float64) query.Expression {
	// Describe both requirements; this does not read the database yet.
	return query.And(
		query.LessThanEqual("price", maxPrice),
		query.Equal("inStock", true),
	)
}
