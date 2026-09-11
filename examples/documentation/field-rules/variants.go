package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Variants = field.Array("variants", field.Fields{
	field.Text("sku").Required().Validate(validateSKU),
	field.Checkbox("locked"),
})

func validateSKU(
	ctx operation.Context,
	value operation.Value[string],
) ([]operation.Issue, error) {
	current, present := value.Get()
	// Prior follows this row's key, even after the row moves.
	previous, existed := ctx.Prior.String("sku")
	// Use this row's current checkbox: clearing it unlocks the SKU.
	locked, _ := ctx.Siblings.Get("locked").BooleanValue()
	// A new row has no previous SKU to protect.
	if locked && existed && (!present || current != previous) {
		return []operation.Issue{{
			Code:    "sku_locked",
			Message: "A locked variant's SKU cannot change",
		}}, nil
	}
	return nil, nil
}
