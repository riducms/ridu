package content

import (
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestSubtitleReturnsKeepReplaceAndClear(t *testing.T) {
	for _, test := range []struct {
		name        string
		input       operation.Value[string]
		wantReplace bool
		wantPresent bool
		wantValue   string
	}{
		{name: "empty stays empty", input: operation.Empty[string]()},
		{name: "spaces clear the field", input: operation.Present(" \t "), wantReplace: true},
		{name: "text is trimmed", input: operation.Present("  Hello  "), wantReplace: true, wantPresent: true, wantValue: "Hello"},
	} {
		t.Run(test.name, func(t *testing.T) {
			change, err := cleanSubtitle(operation.Context{}, test.input)
			if err != nil {
				t.Fatal(err)
			}
			replacement, replace := change.Replacement()
			value, present := replacement.Get()
			if replace != test.wantReplace || present != test.wantPresent || value != test.wantValue {
				t.Fatalf("replace=%v present=%v value=%q", replace, present, value)
			}
		})
	}
}

func TestZeroValuesRemainPresent(t *testing.T) {
	if value, present := operation.Present(false).Get(); !present || value {
		t.Fatal("an explicit false must remain present")
	}
	if value, present := operation.Present(0.0).Get(); !present || value != 0 {
		t.Fatal("an explicit zero must remain present")
	}
	if value, present := operation.Present("").Get(); !present || value != "" {
		t.Fatal("an explicit empty string must remain present in the wrapper")
	}
	if _, present := operation.Empty[bool]().Get(); present {
		t.Fatal("an empty bool must differ from Present(false)")
	}
}

func TestExamplesSaveClearAndTargetValidationMessages(t *testing.T) {
	app, err := ridu.New(ridu.Config{
		Name: "Operation examples",
		Collections: []ridu.Collection{{
			Slug:   "products",
			Fields: field.Fields{Subtitle, Pricing},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{
		"subtitle": store.String("  Hello  "),
		"pricing": store.Object(store.Values{
			"price": store.Number(10), "salePrice": store.Number(0),
		}),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if subtitle, _ := created.Values["subtitle"].StringValue(); subtitle != "Hello" {
		t.Fatalf("subtitle = %q", subtitle)
	}
	pricing, _ := created.Values["pricing"].CopyObject()
	if sale, present := pricing["salePrice"].NumberValue(); !present || sale != 0 {
		t.Fatal("a zero sale price must not become missing")
	}

	unchanged, err := app.Local().Update(t.Context(), "products", created.ID, store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if subtitle, _ := unchanged.Values["subtitle"].StringValue(); subtitle != "Hello" {
		t.Fatalf("omitted update changed subtitle to %q", subtitle)
	}
	cleared, err := app.Local().Update(t.Context(), "products", created.ID, store.Values{
		"subtitle": store.String("  "),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := cleared.Values["subtitle"]; !exists || value.Kind() != store.ValueNull {
		t.Fatal("Replace(Empty) must clear the value to null, not remove its property")
	}

	_, err = app.Local().Update(t.Context(), "products", created.ID, store.Values{
		"pricing": store.Object(store.Values{
			"price": store.Number(10), "salePrice": store.Number(12),
		}),
	}, ridu.MutationOptions{})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || len(failure.Issues) != 1 {
		t.Fatalf("expected one validation issue: %v", err)
	}
	issue := failure.Issues[0]
	if issue.Code != "sale_price_too_high" || issue.Path != "pricing.salePrice" {
		t.Fatalf("wrong issue target: %+v", issue)
	}
	stored, err := app.Local().Find(t.Context(), "products", created.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pricing, _ = stored.Values["pricing"].CopyObject()
	if sale, _ := pricing["salePrice"].NumberValue(); sale != 0 {
		t.Fatal("rejected sale price reached storage")
	}
}

func TestRawHookDistinguishesOmittedAndNull(t *testing.T) {
	var inputs []operation.Value[store.Value]
	app, err := ridu.New(ridu.Config{
		Name: "Raw presence",
		Collections: []ridu.Collection{{
			Slug: "products",
			Fields: field.Fields{field.Number("score").Hooks(field.Hooks[float64]{
				BeforeValidate: []field.RawTransform{func(_ operation.Context, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
					inputs = append(inputs, value)
					return operation.Keep[store.Value](), nil
				}},
			})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Local().Update(t.Context(), "products", created.ID, store.Values{"score": store.Null()}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("raw callbacks = %d", len(inputs))
	}
	if _, present := inputs[0].Get(); present {
		t.Fatal("omitted input must be empty")
	}
	if value, present := inputs[1].Get(); !present || value.Kind() != store.ValueNull {
		t.Fatal("explicit null must be present before validation")
	}
}
