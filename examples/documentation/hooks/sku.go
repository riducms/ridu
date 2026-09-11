package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func uppercaseSKU(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	sku, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	// Replace updates this field; Ridu validates the result again.
	return operation.Replace(
		operation.Present(strings.ToUpper(sku)),
	), nil
}

var SKU = field.Text("sku").Required().Hooks(field.Hooks[string]{
	BeforeChange: []field.Transform[string]{uppercaseSKU},
})
