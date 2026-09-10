package field_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestPrimitiveListLiveFactoryRefinementsPreserveBehavior(t *testing.T) {
	calls := 0
	textLive := func(operation.LiveValidationContext, operation.Value[[]string]) ([]operation.Issue, error) {
		calls++
		return nil, nil
	}
	numberLive := func(operation.LiveValidationContext, operation.Value[[]float64]) ([]operation.Issue, error) {
		calls++
		return nil, nil
	}
	text := field.TextList("points").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
		return operation.Present([]string{"Oak"}), nil
	}).
		Validate(func(operation.ValidationContext, operation.Value[[]string]) ([]operation.Issue, error) {
			return nil, nil
		}).
		Hooks(field.Hooks[[]string]{BeforeChange: []field.Transform[[]string]{func(operation.WriteContext, operation.Value[[]string]) (operation.Change[[]string], error) {
			return operation.Keep[[]string](), nil
		}}}).
		Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return true, nil }}).Private("catalog", store.String("text factory")).LiveValidate(textLive)
	numbers := field.NumberList("sizes").Default(0, 8, 8).Validate(func(operation.ValidationContext, operation.Value[[]float64]) ([]operation.Issue, error) {
		return nil, nil
	}).
		Hooks(field.Hooks[[]float64]{BeforeChange: []field.Transform[[]float64]{func(operation.WriteContext, operation.Value[[]float64]) (operation.Change[[]float64], error) {
			return operation.Keep[[]float64](), nil
		}}}).
		Access(field.Access{Update: func(operation.AccessContext) (bool, error) { return true, nil }}).Private("catalog", store.String("number factory")).LiveValidate(numberLive)
	factory := field.Group("options", field.Fields{text, numbers})
	refined, err := field.EditChild(factory, "points", field.AsTextList, func(f field.TextListField) field.TextListField { return f.MaxLength(32).LiveValidate(textLive) })
	if err != nil {
		t.Fatal(err)
	}
	refined, err = refined.EditChildren(func(children *field.ChildrenDraft) error {
		return children.EditNumberList("sizes", func(f field.NumberListField) field.NumberListField { return f.Max(100).LiveValidate(numberLive) })
	})
	if err != nil {
		t.Fatal(err)
	}
	reused := (field.Fields{refined.Rename("details"), field.Array("variants", refined.Children()), field.Blocks("content", field.Block{Slug: "card", Fields: refined.Children()})}).WithProvenance("app:catalog")
	for _, node := range reused {
		for _, branch := range field.Snapshot(node).Branches() {
			points, err := field.AsTextList(branch.Fields[0])
			if err != nil {
				t.Fatal(err)
			}
			sizes, err := field.AsNumberList(branch.Fields[1])
			if err != nil {
				t.Fatal(err)
			}
			if len(points.LiveValidators()) != 2 || len(points.Validators()) != 1 || points.DefaultCallback() == nil || len(points.HookPolicy().BeforeChange) != 1 || points.AccessPolicy().Read == nil {
				t.Fatal("text policy lost during reuse")
			}
			if len(sizes.LiveValidators()) != 2 || len(sizes.Validators()) != 1 || len(sizes.HookPolicy().BeforeChange) != 1 || sizes.AccessPolicy().Update == nil {
				t.Fatal("number policy lost during reuse")
			}
			if _, ok := field.Snapshot(points).Private("catalog"); !ok {
				t.Fatal("text private attachment lost")
			}
			if _, ok := field.Snapshot(sizes).Private("catalog"); !ok {
				t.Fatal("number private attachment lost")
			}
			if len(points.ReplaceLiveValidators(textLive).LiveValidators()) != 1 || len(sizes.ReplaceLiveValidators(numberLive).LiveValidators()) != 1 {
				t.Fatal("replace appended advisory policy")
			}
			if len(points.ReplaceLiveValidators().LiveValidators()) != 0 || len(sizes.ReplaceLiveValidators().LiveValidators()) != 0 {
				t.Fatal("empty replace did not disable")
			}
			if len(points.ReplaceLiveValidators().Validators()) != 1 || len(sizes.ReplaceLiveValidators().Validators()) != 1 {
				t.Fatal("disabling live altered authoritative validation")
			}
			textSlice := points.LiveValidators()
			numberSlice := sizes.LiveValidators()
			textSlice[0] = nil
			numberSlice[0] = nil
			if points.LiveValidators()[0] == nil || sizes.LiveValidators()[0] == nil {
				t.Fatal("callback inspection exposed mutable slices")
			}
		}
	}
	if len(text.LiveValidators()) != 1 || len(numbers.LiveValidators()) != 1 || calls != 0 {
		t.Fatal("factory construction edited or evaluated the source")
	}
	for _, node := range (field.Fields{text.LiveValidate(nil), numbers.LiveValidate(nil)}) {
		issues := field.Snapshot(node).Issues()
		if len(issues) != 1 || issues[0].Code != "nil_field_callback" {
			t.Fatalf("nil policy diagnostics=%#v", issues)
		}
	}
	if len(field.Snapshot(text.LiveValidate(nil).ReplaceLiveValidators(textLive)).Issues()) != 0 || len(field.Snapshot(numbers.LiveValidate(nil).ReplaceLiveValidators(numberLive)).Issues()) != 0 {
		t.Fatal("superseded nil policy survived")
	}
	for _, node := range (field.Fields{text, numbers}) {
		typ := reflect.TypeOf(node)
		save, _ := typ.MethodByName("Validate")
		live, _ := typ.MethodByName("LiveValidate")
		if save.Type.In(1).In(1) != live.Type.In(1).In(1) {
			t.Fatalf("%T changed typed list argument", node)
		}
	}
}
