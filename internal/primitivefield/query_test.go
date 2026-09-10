package primitivefield

import (
	"math"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestNumberListMembershipRequiresFiniteNumbers(t *testing.T) {
	path, _ := query.ParsePath("sizes")
	field := schema.Field{Type: schema.FieldTypeNumberList}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		node := query.In(path, query.Number(value)).Node()
		if err := ValidateComparison(field, *node.Comparison); err == nil || !strings.Contains(err.Error(), "finite numbers") {
			t.Fatalf("error=%v", err)
		}
	}
	node := query.In(path, query.Number(0), query.Number(-1)).Node()
	if err := ValidateComparison(field, *node.Comparison); err != nil {
		t.Fatal(err)
	}
}
