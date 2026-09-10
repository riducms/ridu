package field_test

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func initialLabel(ctx operation.DefaultContext) (operation.Value[string], error) {
	if ctx.Locale == "fr" {
		return operation.Present("Lire la suite"), nil
	}
	return operation.Present("Read more"), nil
}

func TestDynamicDefaultSupportedConcreteTypes(t *testing.T) {
	stringDefault := field.DefaultFunc[string](initialLabel)
	numberDefault := field.DefaultFunc[float64](func(operation.DefaultContext) (operation.Value[float64], error) {
		return operation.Present(0.0), nil
	})
	boolDefault := field.DefaultFunc[bool](func(operation.DefaultContext) (operation.Value[bool], error) {
		return operation.Present(false), nil
	})
	listDefault := field.DefaultFunc[[]string](func(operation.DefaultContext) (operation.Value[[]string], error) {
		return operation.Present([]string{"news"}), nil
	})

	// These concrete assignments are an external-consumer compile check for
	// useful chaining and the same logical type as each kind's literal Default.
	var text field.TextField = field.Text("text").DefaultFrom(stringDefault).MaxLength(120)
	var code field.CodeField = field.Code("code").DefaultFrom(stringDefault)
	var textarea field.TextareaField = field.Textarea("textarea").DefaultFrom(stringDefault)
	var email field.EmailField = field.Email("email").DefaultFrom(stringDefault)
	var date field.DateField = field.Date("date").DefaultFrom(stringDefault).Format(field.DateTime)
	var number field.NumberField = field.Number("number").DefaultFrom(numberDefault).Min(0)
	var checkbox field.CheckboxField = field.Checkbox("checkbox").DefaultFrom(boolDefault)
	var selectField field.SelectField = field.Select("select", "news").DefaultFrom(stringDefault)
	var radio field.RadioField = field.Radio("radio", "news").DefaultFrom(stringDefault)
	var multi field.MultiSelectField = field.MultiSelect("multi", "news").DefaultFrom(listDefault)

	for _, node := range (field.Fields{text, code, textarea, email, date, number, checkbox, selectField, radio, multi}) {
		view := field.Snapshot(node)
		if !view.HasBehavior() || !view.BehaviorSummary().DynamicDefault {
			t.Errorf("%T lost its executable default attachment", node)
		}
		if _, literal := view.Default(); literal || len(view.SelectDefaults()) != 0 {
			t.Errorf("%T has a literal and dynamic default simultaneously", node)
		}
		if issues := view.Issues(); len(issues) != 0 {
			t.Errorf("%T rejected a callback without evaluating it: %#v", node, issues)
		}
	}
	for _, callback := range []field.DefaultFunc[string]{text.DefaultCallback(), code.DefaultCallback(), textarea.DefaultCallback(), email.DefaultCallback(), date.DefaultCallback(), selectField.DefaultCallback(), radio.DefaultCallback()} {
		value, err := callback(operation.DefaultContext{Locale: "fr"})
		if got, present := value.Get(); err != nil || !present || got != "Lire la suite" {
			t.Fatalf("string default callback = %v, %v", value, err)
		}
	}
	if value, err := number.DefaultCallback()(operation.DefaultContext{}); err != nil {
		t.Fatal(err)
	} else if got, present := value.Get(); !present || got != 0 {
		t.Fatal("numeric zero lost its presence")
	}
	if value, err := checkbox.DefaultCallback()(operation.DefaultContext{}); err != nil {
		t.Fatal(err)
	} else if got, present := value.Get(); !present || got {
		t.Fatal("false lost its presence")
	}
	if value, err := multi.DefaultCallback()(operation.DefaultContext{}); err != nil {
		t.Fatal(err)
	} else if got, present := value.Get(); !present || !slices.Equal(got, []string{"news"}) {
		t.Fatal("multi-select callback lost its logical list type")
	}
}

func TestDynamicDefaultMethodSetDoesNotExpandDefaultSupport(t *testing.T) {
	for _, node := range (field.Fields{
		field.JSON("json"), field.Point("point"),
		field.Relationship("author", "users"), field.Relationships("authors", "users"),
		field.Upload("image", "media"), field.Uploads("images", "media"),
		field.PolymorphicRelationship("owner", "users", "teams"),
		field.PolymorphicRelationships("owners", "users", "teams"),
		field.Group("group", nil), field.Array("rows", nil), field.Blocks("content"),
		field.Row(nil), field.Collapsible("Details", nil),
		field.Join("comments", "comments", "post"),
		field.Plugin("body", "example:richtext", nil),
		field.Virtual("summary", field.ValueString, nil),
	}) {
		for _, method := range []string{"DefaultFrom", "DefaultCallback"} {
			if _, found := reflect.TypeOf(node).MethodByName(method); found {
				t.Errorf("%T exposes unsupported %s", node, method)
			}
		}
	}
}

func TestDynamicDefaultSettersReplaceRatherThanCompose(t *testing.T) {
	base := field.Text("label").Default("Untitled").Validate(graphRule).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}).
		Access(field.Access{Create: graphAllow}).Private("factory", store.String("retained"))
	dynamic := base.DefaultFrom(initialLabel)
	fixed := dynamic.Default("New article")
	if literal, exists := field.Snapshot(base).Default(); !exists || literal.String() != "Untitled" || base.DefaultCallback() != nil {
		t.Fatal("dynamic refinement mutated the literal base")
	}
	if _, exists := field.Snapshot(dynamic).Default(); exists || dynamic.DefaultCallback() == nil {
		t.Fatal("dynamic default retained a literal fallback")
	}
	if literal, exists := field.Snapshot(fixed).Default(); !exists || literal.String() != "New article" || fixed.DefaultCallback() != nil {
		t.Fatal("literal refinement retained a dynamic fallback")
	}
	for _, node := range (field.Fields{base, dynamic, fixed}) {
		text := assertGraphTextBehavior(t, node, 1, 1)
		allowed, err := text.AccessPolicy().Create(operation.AccessContext{})
		if err != nil || !allowed {
			t.Fatal("default replacement lost access")
		}
		if value, exists := field.Snapshot(text).Private("factory"); !exists || value.Kind() != store.ValueString {
			t.Fatal("default replacement lost private attachments")
		}
	}

	listBase := field.MultiSelect("tags", "news", "updates").Default("news")
	listDynamic := listBase.DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
		return operation.Present([]string{"updates"}), nil
	})
	listFixed := listDynamic.Default("updates")
	if !slices.Equal(field.Snapshot(listBase).SelectDefaults(), []string{"news"}) || listBase.DefaultCallback() != nil ||
		len(field.Snapshot(listDynamic).SelectDefaults()) != 0 || listDynamic.DefaultCallback() == nil ||
		!slices.Equal(field.Snapshot(listFixed).SelectDefaults(), []string{"updates"}) || listFixed.DefaultCallback() != nil {
		t.Fatal("multi-select defaults did not replace immutably")
	}
}

func TestDynamicDefaultDiagnosticsFollowTheFinalPolicy(t *testing.T) {
	invalid := field.Text("title").DefaultFrom(nil)
	issues := field.Snapshot(invalid).Issues()
	if len(issues) != 1 || issues[0].Code != "nil_field_callback" || issues[0].Path != "defaultFrom" || !strings.Contains(issues[0].Message, "non-nil") {
		t.Fatalf("nil callback diagnostic = %#v", issues)
	}
	validNumber := func(operation.DefaultContext) (operation.Value[float64], error) {
		return operation.Present(3.0), nil
	}
	validList := func(operation.DefaultContext) (operation.Value[[]string], error) {
		return operation.Empty[[]string](), nil
	}
	for _, node := range (field.Fields{
		invalid.Default("Fixed"), invalid.DefaultFrom(initialLabel),
		field.Number("n").Default(math.NaN()).DefaultFrom(validNumber),
		field.Number("n").DefaultFrom(nil).Default(0),
		field.Number("n").DefaultFrom(validNumber).Default(math.NaN()).DefaultFrom(validNumber),
		field.Text("title").MaxLength(2).Default("Too long").DefaultFrom(initialLabel),
		field.MultiSelect("tags").DefaultFrom(nil).Default(),
		field.MultiSelect("tags").DefaultFrom(nil).DefaultFrom(validList),
	}) {
		if got := field.Snapshot(node).Issues(); len(got) != 0 {
			t.Errorf("%T retained superseded default diagnostics: %#v", node, got)
		}
	}
	if len(field.Snapshot(invalid).Issues()) != 1 {
		t.Fatal("refining a nil callback mutated the original diagnostics")
	}
	if got := field.Snapshot(field.Text("title").MinLength(-1).DefaultFrom(nil).Default("fixed")).Issues(); len(got) != 1 || got[0].Path != "minLength" {
		t.Fatalf("default refinement cleared unrelated diagnostics: %#v", got)
	}
	if got := field.Snapshot(field.Slug("slug", "title").DefaultFrom(initialLabel)).Issues(); len(got) != 1 || got[0].Code != "unsupported_slug_default" {
		t.Fatalf("dynamic default bypassed the slug helper's existing default restriction: %#v", got)
	}
}

func TestDynamicDefaultFactoriesAndGraphEditsPreserveBehaviorWithoutEvaluation(t *testing.T) {
	calls := 0
	initial := func(ctx operation.DefaultContext) (operation.Value[string], error) {
		calls++
		return initialLabel(ctx)
	}
	label := field.Text("label").Required().DefaultFrom(initial).Private("factory", store.String("link"))
	link := field.Group("cta", field.Fields{&label, field.Text("url")})
	refined, err := field.EditChild(link, "label", field.AsText, func(text field.TextField) field.TextField {
		return text.MaxLength(32).EditAdmin(func(admin *field.Admin) { admin.Description = "Button label" })
	})
	if err != nil {
		t.Fatal(err)
	}
	label = label.Default("Changed factory variable")
	roots := field.Fields{refined, field.Array("links", link.Children()), field.Blocks("content", field.Block{Slug: "link", Fields: link.Children()})}
	edited, err := roots.Edit(func(draft *field.ChildrenDraft) error {
		return draft.EditChildren("cta", func(children *field.ChildrenDraft) error {
			return children.EditText("label", func(text field.TextField) field.TextField { return text.Label("Action") })
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range edited.WithProvenance("app:buttons") {
		for _, branch := range field.Snapshot(root).Branches() {
			text, err := field.AsText(branch.Fields[0])
			if err != nil {
				t.Fatal(err)
			}
			view := field.Snapshot(text)
			if len(view.Issues()) != 0 || !view.HasBehavior() || text.DefaultCallback() == nil {
				t.Fatal("reuse lost the private default policy")
			}
			if _, present := view.Default(); present {
				t.Fatal("reassigning an input facade changed an owned child")
			}
			data, err := json.Marshal(view.BehaviorSummary())
			if err != nil || strings.Contains(string(data), "Lire") || strings.Contains(string(data), "Read more") {
				t.Fatal("static behavior evidence contains an evaluated value")
			}
		}
	}
	if calls != 0 {
		t.Fatal("authoring, inspection, or graph edits executed a dynamic default")
	}
	text, _ := field.AsText(field.Snapshot(edited[0]).Fields()[0])
	value, err := text.DefaultCallback()(operation.DefaultContext{Locale: "fr"})
	if got, present := value.Get(); err != nil || !present || got != "Lire la suite" || calls != 1 {
		t.Fatal("graph-edited factory lost its default callback")
	}
	if max, set := field.Snapshot(link.Children()[0]).MaxLength(); set || max != 0 {
		t.Fatal("child refinement mutated the reusable factory")
	}
}

func TestDynamicMultiSelectDefaultResultsAreOwned(t *testing.T) {
	borrowed := []string{"news", "updates"}
	f := field.MultiSelect("tags").DefaultFrom(func(operation.DefaultContext) (operation.Value[[]string], error) {
		return operation.Present(borrowed), nil
	})
	first, err := f.DefaultCallback()(operation.DefaultContext{})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := f.DefaultCallback()(operation.DefaultContext{})
	values, _ := first.Get()
	values[0] = "changed returned value"
	if borrowed[0] != "news" {
		t.Fatal("returned default slice aliases the application's captured input")
	}
	other, _ := second.Get()
	if other[0] != "news" {
		t.Fatal("separate callback results alias each other")
	}
	borrowed[1] = "changed captured value"
	if values[1] != "updates" || other[1] != "updates" {
		t.Fatal("captured mutations changed an already-returned default")
	}
}
