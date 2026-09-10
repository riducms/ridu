package content

import (
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func SellingPoints(name string) field.TextListField {
	return field.TextList(name).MinRows(1).MaxRows(8).MaxLength(120)
}

func Sizes(name string) field.NumberListField {
	return field.NumberList(name).Min(0).MaxRows(20)
}

func Package(name string) field.GroupField {
	return field.Group(name, field.Fields{SellingPoints("points"), Sizes("sizes")})
}

func CompactPackage(name string) (field.GroupField, error) {
	return field.EditChild(Package(name), "points", field.AsTextList,
		func(f field.TextListField) field.TextListField { return f.MaxRows(3) })
}

var Products = core.Collection{
	Slug: "products",
	Fields: field.Fields{
		field.Text("title").Required(),
		SellingPoints("sellingPoints").Label("Selling points"),
		Sizes("availableSizes").Default(8, 10, 12),
		SellingPoints("translatedPoints").Localized().DefaultFrom(
			func(ctx operation.DefaultContext) (operation.Value[[]string], error) {
				if ctx.Locale == "fr" {
					return operation.Present([]string{"Fabriqué à la main"}), nil
				}
				return operation.Present([]string{"Hand finished"}), nil
			}),
		Package("package"),
	},
}
