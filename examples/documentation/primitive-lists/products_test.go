package content

import (
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
)

func TestProductFactoriesKeepConcreteListTypes(t *testing.T) {
	compact, err := CompactPackage("travel")
	if err != nil {
		t.Fatal(err)
	}
	product := Products
	product.Fields = append(append(field.Fields(nil), Products.Fields...), compact)
	manifest, err := core.Resolve(core.Config{Name: "Product lists", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{product}})
	if err != nil {
		t.Fatal(err)
	}
	fields := manifest.Snapshot().Collections[0].Fields
	if fields[4].Nested.ResolvedFields()[0].List.MaxRows != 8 || fields[5].Nested.ResolvedFields()[0].List.MaxRows != 3 {
		t.Fatal("factory refinement mutated its reusable source")
	}
}
