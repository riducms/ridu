package core

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/riducms/ridu/field"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// NaN and infinities cannot arrive in JSON, but the typed lowering must also
// reject them when private engine callers supply store.Values directly.
func TestLivePrimitiveListRejectsNonFiniteEngineValues(t *testing.T) {
	calls := 0
	var current []float64
	app, err := New(Config{Name: "Finite advisory lists", Collections: []Collection{{Slug: "products", Fields: field.Fields{
		field.NumberList("sizes").LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[[]float64]) ([]operation.Issue, error) {
			calls++
			current, _ = value.Get()
			return nil, nil
		}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		value store.Value
	}{
		{"NaN", store.Number(math.NaN())},
		{"positive infinity", store.Number(math.Inf(1))},
		{"negative infinity", store.Number(math.Inf(-1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := store.Values{"sizes": store.List(store.Number(0), test.value)}
			result, err := app.local.engine.LiveValidate(t.Context(), operationengine.LiveValidationRequest{Collection: "products", Data: data, Fields: []string{"sizes"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Evaluations) != 1 || result.Evaluations[0].Status != "skipped" || calls != 0 {
				t.Fatalf("malformed values reached callback: result=%#v calls=%d", result, calls)
			}
			_, err = app.Local().Create(t.Context(), "products", data, nil)
			var failure *OperationError
			if !errors.As(err, &failure) || failure.Status != 422 {
				t.Fatalf("authoritative write accepted nonfinite list: %v", err)
			}
		})
	}
	result, err := app.local.engine.LiveValidate(t.Context(), operationengine.LiveValidationRequest{Collection: "products", Data: store.Values{"sizes": store.List(store.Number(0), store.Number(math.MaxFloat64), store.Number(0))}, Fields: []string{"sizes"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Evaluations) != 1 || result.Evaluations[0].Status != "checked" || calls != 1 || !reflect.DeepEqual(current, []float64{0, math.MaxFloat64, 0}) {
		t.Fatalf("finite whole list lost values: result=%#v calls=%d value=%#v", result, calls, current)
	}
}
