package field_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestLiveValidationFactoriesPreserveIndependentPolicies(t *testing.T) {
	calls := 0
	check := func(operation.LiveValidationContext, operation.Value[string]) ([]operation.Issue, error) {
		calls++
		return nil, nil
	}
	base := field.Text("sku").Validate(graphRule).DefaultFrom(initialLabel).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}).
		Access(field.Access{Update: graphAllow}).Private("factory", store.String("product")).LiveValidate(check)
	extended := base.LiveValidate(check)
	replaced := extended.ReplaceLiveValidators(check)
	disabled := replaced.ReplaceLiveValidators()
	for i, f := range []field.TextField{base, extended, replaced, disabled} {
		want := []int{1, 2, 1, 0}[i]
		if len(f.LiveValidators()) != want || field.Snapshot(f).BehaviorSummary().LiveValidators != want {
			t.Fatalf("live policy %d = %#v", i, field.Snapshot(f).BehaviorSummary())
		}
		if len(f.Validators()) != 1 || f.DefaultCallback() == nil || len(f.HookPolicy().BeforeChange) != 1 || f.AccessPolicy().Update == nil {
			t.Fatal("live refinement changed save/default/access behavior")
		}
		if _, ok := field.Snapshot(f).Private("factory"); !ok {
			t.Fatal("lost private attachment")
		}
	}
	exposed := base.LiveValidators()
	exposed[0] = nil
	if base.LiveValidators()[0] == nil {
		t.Fatal("inspection aliases callback storage")
	}
	link := field.Group("product", field.Fields{base})
	refined, err := field.EditChild(link, "sku", field.AsText, func(f field.TextField) field.TextField { return f.MaxLength(32) })
	if err != nil {
		t.Fatal(err)
	}
	fields, err := (field.Fields{refined, field.Array("products", link.Children()), field.Blocks("content", field.Block{Slug: "product", Fields: link.Children()})}).Edit(func(d *field.ChildrenDraft) error {
		return d.EditChildren("product", func(c *field.ChildrenDraft) error {
			return c.EditText("sku", func(f field.TextField) field.TextField { return f.Label("Stock code") })
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range fields.WithProvenance("app:catalog") {
		for _, branch := range field.Snapshot(node).Branches() {
			text, err := field.AsText(branch.Fields[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(text.LiveValidators()) != 1 || text.DefaultCallback() == nil || len(text.Validators()) != 1 {
				t.Fatal("reuse dropped field behavior")
			}
		}
	}
	if calls != 0 {
		t.Fatal("authoring or graph edits evaluated advisory code")
	}
	if _, err := base.LiveValidators()[0](operation.LiveValidationContext{}, operation.Present("A-1")); err != nil || calls != 1 {
		t.Fatal("callback not retained")
	}
}

func TestLiveValidationNilPolicyAndMethodSets(t *testing.T) {
	invalid := field.Text("sku").LiveValidate(nil)
	issues := field.Snapshot(invalid).Issues()
	if len(issues) != 1 || issues[0].Code != "nil_field_callback" || !strings.Contains(issues[0].Path, "liveValidators") {
		t.Fatalf("nil diagnostic: %#v", issues)
	}
	if got := field.Snapshot(invalid.ReplaceLiveValidators()).Issues(); len(got) != 0 {
		t.Fatalf("superseded diagnostic: %#v", got)
	}
	for _, node := range (field.Fields{field.Row(nil), field.Collapsible("Details", nil), field.Join("comments", "comments", "post"), field.Virtual("summary", field.ValueString, nil)}) {
		for _, method := range []string{"LiveValidate", "ReplaceLiveValidators", "LiveValidators"} {
			if _, ok := reflect.TypeOf(node).MethodByName(method); ok {
				t.Errorf("%T exposes %s without a writable value", node, method)
			}
		}
	}
	// Every existing writable validation kind must expose the equivalent typed opt-in.
	for _, node := range (field.Fields{field.Text("a"), field.Code("a"), field.Textarea("a"), field.Email("a"), field.Date("a"), field.Number("a"), field.Checkbox("a"), field.Select("a"), field.Radio("a"), field.MultiSelect("a"), field.JSON("a"), field.Point("a"), field.Group("a", nil), field.Array("a", nil), field.Blocks("a"), field.Relationship("a", "users"), field.Relationships("a", "users"), field.Upload("a", "media"), field.Uploads("a", "media"), field.PolymorphicRelationship("a", "users", "teams"), field.PolymorphicRelationships("a", "users", "teams"), field.Plugin("a", "app:data", nil)}) {
		typ := reflect.TypeOf(node)
		save, _ := typ.MethodByName("Validate")
		live, ok := typ.MethodByName("LiveValidate")
		if !ok || save.Type.In(1).In(1) != live.Type.In(1).In(1) {
			t.Errorf("%T does not preserve its write value type", node)
		}
	}
}
