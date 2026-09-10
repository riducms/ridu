package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var SalePrice = field.Number("salePrice").Min(0).
	// Enforce the rule whenever a document is saved.
	Validate(validateSalePrice).
	// Also check unsaved input while the author edits this field.
	LiveValidate(checkSalePrice)

func Catalog() []ridu.Collection {
	return []ridu.Collection{{
		Slug: "products",
		Fields: field.Fields{
			field.Text("name").Required(),
			field.Number("price").Label("Regular price").Required().Min(0),
			SalePrice,
		},
	}}
}

func validateSalePrice(
	ctx operation.ValidationContext,
	value operation.Value[float64],
) ([]operation.Issue, error) {
	return salePriceIssues(ctx.Siblings, value), nil
}

func checkSalePrice(
	ctx operation.LiveValidationContext,
	value operation.Value[float64],
) ([]operation.Issue, error) {
	// Both callbacks use the same rule, so their feedback agrees.
	return salePriceIssues(ctx.Siblings, value), nil
}

func salePriceIssues(
	siblings operation.View,
	value operation.Value[float64],
) []operation.Issue {
	salePrice, hasSalePrice := value.Get()
	price, hasPrice := siblings.Get("price").NumberValue()
	// A sale price is optional. Wait until both numbers are available.
	if !hasSalePrice || !hasPrice || salePrice < price {
		return nil
	}
	// Return a message beside the sale price field.
	return []operation.Issue{{
		Code:    "sale_price",
		Message: "Sale price must be lower than the regular price.",
	}}
}
