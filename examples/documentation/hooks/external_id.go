package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func unlinkCopy(
	_ operation.Context,
	_ operation.Value[store.Value],
) (operation.Change[store.Value], error) {
	// The copy is a new product that Stripe does not know about yet.
	return operation.Clear[store.Value](), nil
}

var StripeProductID = field.Text("stripeProductId").Unique().Hooks(
	field.Hooks[string]{
		BeforeDuplicate: []field.RawTransform{unlinkCopy},
	},
)
