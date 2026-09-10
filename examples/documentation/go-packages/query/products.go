package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
)

func visibleProducts(ridu.AccessContext) (ridu.AccessDecision, error) {
	visible, err := query.NewPath("visible")
	if err != nil {
		return ridu.Deny(), err
	}
	// This filter limits every read, including reads with other filters.
	return ridu.Where(
		query.Equal(visible, query.Boolean(true)),
	), nil
}

var Products = ridu.Collection{
	Slug: "products",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Number("price").Required().Min(0),
		field.Checkbox("inStock").Default(false),
		field.Checkbox("visible").Default(false),
	},
	Access: ridu.CollectionAccess{Read: visibleProducts},
}
