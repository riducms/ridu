package field_test

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// ProductFacts is reusable application code, using only public field contracts.
func ProductFacts(name string) field.GroupField {
	return field.Group(name, field.Fields{
		field.TextList("sellingPoints").Label("Selling points").MinRows(1).MaxRows(8).MaxLength(120).
			Default("Solid oak", "Five-year warranty").
			Validate(func(_ operation.Context, value operation.Value[[]string]) ([]operation.Issue, error) {
				items, _ := value.Get()
				if slices.Contains(items, "unverified") {
					return []operation.Issue{{Code: "unverified_claim", Message: "Check the product claims"}}, nil
				}
				return nil, nil
			}).Hooks(field.Hooks[[]string]{BeforeChange: []field.Transform[[]string]{
			func(_ operation.Context, value operation.Value[[]string]) (operation.Change[[]string], error) {
				return operation.Replace(value), nil
			},
		}}).Access(field.Access{Update: func(ctx operation.Context) (bool, error) { return ctx.Actor.ID == "editor", nil }}).
			Private("factory", store.String("product-facts")),
		field.NumberList("availableSizes").Min(0).MaxRows(20).DefaultFrom(
			func(operation.Context) (operation.Value[[]float64], error) {
				return operation.Present([]float64{0, 8, 10}), nil
			}),
	})
}

func TestPrimitiveListConcreteFactoryAndChildRefinement(t *testing.T) {
	base := ProductFacts("facts")
	refined, err := field.EditChild(base, "sellingPoints", field.AsTextList, func(points field.TextListField) field.TextListField {
		return points.MaxLength(32).EditAdmin(func(admin *field.Admin) { admin.Description = "Keep each claim short" })
	})
	if err != nil {
		t.Fatal(err)
	}
	points, err := field.AsTextList(refined.Children()[0])
	if err != nil {
		t.Fatal(err)
	}
	if max, _ := field.Snapshot(base.Children()[0]).MaxLength(); max != 120 {
		t.Fatal("refinement mutated factory")
	}
	if max, _ := field.Snapshot(points).MaxLength(); max != 32 {
		t.Fatal("concrete refinement lost bounds")
	}
	if !reflect.DeepEqual(field.Snapshot(points).BehaviorSummary(), field.Snapshot(base.Children()[0]).BehaviorSummary()) {
		t.Fatal("refinement lost unrelated policies")
	}
	if attachment, ok := field.Snapshot(points).Private("factory"); !ok || attachment.Kind() != store.ValueString {
		t.Fatal("private attachment lost")
	}
	issues, err := points.Validators()[0](operation.Context{}, operation.Present([]string{"unverified"}))
	if err != nil || len(issues) != 1 || issues[0].Code != "unverified_claim" {
		t.Fatalf("typed validation: %v %v", issues, err)
	}
	allowed, err := points.AccessPolicy().Update(operation.Context{Actor: operation.Actor{ID: "editor"}})
	if err != nil || !allowed {
		t.Fatal("access policy lost")
	}
	validators := points.Validators()
	validators[0] = nil
	hooks := points.HookPolicy()
	hooks.BeforeChange[0] = nil
	if points.Validators()[0] == nil || points.HookPolicy().BeforeChange[0] == nil {
		t.Fatal("mutable callback slice alias")
	}

	_, err = field.EditChild(base, "sellingPoints", field.AsText, func(text field.TextField) field.TextField { return text.Required() })
	if err == nil || !strings.Contains(err.Error(), "TextListField") {
		t.Fatalf("wrong-kind diagnostic: %v", err)
	}
	pluginEdit, err := base.EditChildren(func(draft *field.ChildrenDraft) error {
		if err := draft.EditTextList("sellingPoints", func(f field.TextListField) field.TextListField { return f.MaxRows(4) }); err != nil {
			return err
		}
		return draft.EditNumberList("availableSizes", func(f field.NumberListField) field.NumberListField { return f.Max(50) })
	})
	if err != nil {
		t.Fatal(err)
	}
	numbers, err := field.AsNumberList(pluginEdit.Children()[1])
	if err != nil {
		t.Fatal(err)
	}
	value, err := numbers.DefaultCallback()(operation.Context{})
	if got, ok := value.Get(); err != nil || !ok || !slices.Equal(got, []float64{0, 8, 10}) {
		t.Fatalf("number list default: %v %v", got, err)
	}
}

func TestPrimitiveListMethodSetsKeepValueShapesDistinct(t *testing.T) {
	var text field.TextListField = field.TextList("points").Required().Localized().MinLength(0).MaxLength(120).MinRows(1).MaxRows(8)
	var number field.NumberListField = field.NumberList("sizes").Min(0).Max(100).Default(0)
	for _, node := range (field.Fields{text, number}) {
		for _, method := range []string{"Unique", "Index", "Fields", "Options", "EditChildren"} {
			if _, ok := reflect.TypeOf(node).MethodByName(method); ok {
				t.Errorf("%T exposes unsupported %s", node, method)
			}
		}
		if field.Snapshot(node).Category() != field.CategoryScalar {
			t.Fatal("list is not one field occurrence")
		}
	}
	if _, ok := reflect.TypeOf(number).MethodByName("Step"); ok {
		t.Fatal("number list exposes a scalar input increment without numeric stepping controls")
	}
	for _, scalar := range (field.Fields{field.Text("text"), field.Number("number")}) {
		if _, ok := reflect.TypeOf(scalar).MethodByName("MinRows"); ok {
			t.Fatalf("scalar %T acquired list semantics", scalar)
		}
	}
	if _, err := field.AsTextList(field.MultiSelect("choices")); err == nil {
		t.Fatal("selection list accepted as primitive text list")
	}
	if _, err := field.AsNumberList(text); err == nil {
		t.Fatal("text list accepted as number list")
	}
}

func TestPrimitiveListDefaultsSnapshotAndReplacePolicies(t *testing.T) {
	textInput := []string{"", "oak", "oak", " spaced "}
	numberInput := []float64{0, 10, 10, -0.5}
	text := field.TextList("points").Default(textInput...)
	number := field.NumberList("sizes").Default(numberInput...)
	textInput[1], numberInput[1] = "mutated", 999
	for _, test := range []struct {
		node field.Node
		want string
	}{
		{text, `["","oak","oak"," spaced "]`}, {number, `[0,10,10,-0.5]`},
		{field.TextList("empty").Default(), `[]`}, {field.NumberList("empty").Default(), `[]`},
	} {
		value, ok := field.Snapshot(test.node).Default()
		if !ok || value.Kind() != field.DefaultList || value.String() != test.want {
			t.Fatalf("%T literal default = %v", test.node, value)
		}
	}
	dynamic := text.DefaultFrom(func(operation.Context) (operation.Value[[]string], error) {
		return operation.Present([]string{"initial"}), nil
	})
	if _, set := field.Snapshot(dynamic).Default(); set || dynamic.DefaultCallback() == nil {
		t.Fatal("dynamic default did not replace literal")
	}
	if fixed := dynamic.Default("fixed"); fixed.DefaultCallback() != nil {
		t.Fatal("literal default retained dynamic callback")
	}
	if number.DefaultCallback() != nil || text.DefaultCallback() != nil {
		t.Fatal("refinement mutated factory")
	}
	for _, node := range (field.Fields{
		field.NumberList("n").Default(math.NaN()).Default(0),
		field.TextList("t").DefaultFrom(nil).Default(),
		field.NumberList("n").DefaultFrom(nil).DefaultFrom(func(operation.Context) (operation.Value[[]float64], error) {
			return operation.Empty[[]float64](), nil
		}),
	}) {
		if got := field.Snapshot(node).Issues(); len(got) != 0 {
			t.Fatalf("superseded default error: %v", got)
		}
	}
}

func TestPrimitiveListConfigurationDiagnostics(t *testing.T) {
	for name, node := range map[string]field.Node{
		"negative count":         field.TextList("points").MinRows(-1),
		"reversed count":         field.NumberList("sizes").MinRows(3).MaxRows(2),
		"negative text length":   field.TextList("points").MinLength(-1),
		"reversed number bounds": field.NumberList("sizes").Min(5).Max(2),
		"nonfinite bound":        field.NumberList("sizes").Max(math.Inf(1)),
		"nonfinite default":      field.NumberList("sizes").Default(math.NaN()),
		"required empty":         field.TextList("points").Required().Default(),
		"minimum default":        field.TextList("points").MinRows(2).Default("one"),
		"text item":              field.TextList("points").MaxLength(2).Default("valid too long"),
		"number item":            field.NumberList("sizes").Min(0).Default(-1),
		"nil callback":           field.TextList("points").DefaultFrom(nil),
	} {
		t.Run(name, func(t *testing.T) {
			issues := field.Snapshot(node).Issues()
			if len(issues) == 0 || issues[0].Code == "" || issues[0].Path == "" || issues[0].Message == "" {
				t.Fatalf("unhelpful diagnostics: %#v", issues)
			}
		})
	}
	if got := field.Snapshot(field.TextList("points").MinLength(1).MaxLength(1).Default("😀", "é")).Issues(); len(got) != 0 {
		t.Fatalf("text bounds counted bytes: %v", got)
	}
}
