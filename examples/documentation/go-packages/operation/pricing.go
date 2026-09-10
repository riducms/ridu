package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

var Pricing = field.Group("pricing", field.Fields{
	field.Number("price").Required().Min(0),
	field.Number("salePrice").Min(0),
}).Validate(validatePricing)

func validatePricing(
	_ operation.ValidationContext,
	value operation.Value[store.Value],
) ([]operation.Issue, error) {
	group, present := value.Get()
	if !present {
		return nil, nil
	}
	// A Group's value is an object containing its child fields.
	price, hasPrice := group.Get("price").NumberValue()
	sale, hasSale := group.Get("salePrice").NumberValue()
	if !hasPrice || !hasSale || sale < price {
		return nil, nil
	}
	return []operation.Issue{{
		Code:    "sale_price_too_high",
		Message: "Set a sale price lower than the regular price",
		// Start inside pricing and place the message on salePrice.
		Target: operation.At("salePrice"),
	}}, nil
}
