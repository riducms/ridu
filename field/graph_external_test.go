package field_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func graphRule(operation.ValidationContext, operation.Value[string]) ([]operation.Issue, error) {
	return []operation.Issue{{Code: "retained_rule", Message: "Retained field-local rule."}}, nil
}

func graphNormalize(_ operation.WriteContext, value operation.Value[string]) (operation.Change[string], error) {
	text, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	return operation.Replace(operation.Present(text + "-normalized")), nil
}

func graphAllow(operation.AccessContext) (bool, error) { return true, nil }
func graphDeny(operation.AccessContext) (bool, error)  { return false, nil }

func assertGraphTextBehavior(t *testing.T, node field.Node, validators, hooks int) field.TextField {
	t.Helper()
	text, err := field.AsText(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(text.Validators()) != validators || len(text.HookPolicy().BeforeChange) != hooks {
		t.Fatalf("field %s lost policy lists: %#v", text.Name(), field.Snapshot(text).BehaviorSummary())
	}
	if got, err := text.Validators()[0](operation.ValidationContext{Context: context.Background()}, operation.Present("code")); err != nil || len(got) != 1 || got[0].Code != "retained_rule" {
		t.Fatalf("field %s lost validator callback: %v, %v", text.Name(), got, err)
	}
	change, err := text.HookPolicy().BeforeChange[0](operation.WriteContext{}, operation.Present("code"))
	if err != nil {
		t.Fatal(err)
	}
	replacement, replaced := change.Replacement()
	if got, present := replacement.Get(); !replaced || !present || got != "code-normalized" {
		t.Fatalf("field %s lost transforming hook", text.Name())
	}
	return text
}

func TestProductionGraphConcreteCopyReuseAndPolicyOwnership(t *testing.T) {
	borrowedHooks := field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}
	labels := map[string]string{"en": "Original"}
	extensions := map[string]store.Value{"plugin.hint": store.String("original")}
	var base field.TextField = field.Text("code").Required().Validate(graphRule).Hooks(borrowedHooks).
		Access(field.Access{Create: graphAllow, Read: graphAllow, Update: graphAllow}).
		Admin(field.Admin{LabelTranslations: labels, Extensions: extensions, Editor: field.Component("app:Code", store.Object(store.Values{"style": store.String("mono")}))}).
		Private("plugin.secret", store.Object(store.Values{"marker": store.String("retained")}))
	var a field.TextField = base.Label("A").MaxLength(120)
	var b field.TextField = base.Label("B").MaxLength(80)
	borrowedHooks.BeforeChange[0] = nil
	labels["en"] = "Changed"
	extensions["plugin.hint"] = store.Null()
	if field.Snapshot(base).Label() != "" || field.Snapshot(a).Label() != "A" || field.Snapshot(b).Label() != "B" {
		t.Fatal("scalar refinement mutated the base or sibling copy")
	}
	if !field.Snapshot(base).Required() || !field.Snapshot(a).Required() || !field.Snapshot(b).Required() {
		t.Fatal("refinement lost requiredness")
	}
	if _, set := field.Snapshot(base).MaxLength(); set {
		t.Fatal("refinement changed the base constraint")
	}
	for _, node := range (field.Fields{base, a, b}) {
		text := assertGraphTextBehavior(t, node, 1, 1)
		readers := text.Validators()
		readers[0] = nil
		returnedHooks := text.HookPolicy()
		returnedHooks.BeforeChange[0] = nil
		assertGraphTextBehavior(t, text, 1, 1)
		definition := field.Snapshot(text)
		admin := definition.AdminPolicy()
		if admin.LabelTranslations["en"] != "Original" || admin.Extensions["plugin.hint"].Kind() != store.ValueString {
			t.Fatal("policy input map changed an immutable field")
		}
		private, exists := definition.Private("plugin.secret")
		data, _ := private.CopyObject()
		if marker, _ := data["marker"].StringValue(); !exists || marker != "retained" {
			t.Fatal("copy lost private owner metadata")
		}
		data["marker"] = store.String("escaped")
		again, _ := definition.Private("plugin.secret")
		data, _ = again.CopyObject()
		if marker, _ := data["marker"].StringValue(); marker != "retained" {
			t.Fatal("private attachment exposed mutable state")
		}
	}
	// The ordinary concrete chain remains assignable until deliberately erased.
	var priority field.NumberField = field.Number("priority").Required().Label("Priority").Min(1)
	if minimum, set := field.Snapshot(priority).Min(); !set || minimum != 1 {
		t.Fatal("concrete number refinement lost its constraint")
	}
}

func TestProductionGraphOwnsPointerInputsAndNestedReuse(t *testing.T) {
	base := field.Text("code").Validate(graphRule).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}).Localized()
	pointer := base
	children := field.Fields{&pointer}
	group := field.Group("group", children)
	array := field.Array("rows", children)
	blockChildren := field.Fields{base}
	caseLabels := map[string]string{"en": "Card"}
	blocks := field.Blocks("content", field.Block{Slug: "card", Labels: field.BlockLabels{SingularTranslations: caseLabels}, Fields: blockChildren})
	pointer = base.Rename("mutated")
	children[0] = field.Text("replaced")
	blockChildren[0] = field.Text("replaced")
	caseLabels["en"] = "Changed"
	for _, node := range (field.Fields{group, array}) {
		definition := field.Snapshot(node)
		nested := definition.Fields()
		if len(nested) != 1 || nested[0].Name() != "code" || !field.Snapshot(nested[0]).Localized() {
			t.Fatalf("nested placement did not own pointer/slice/localization: %#v", nested)
		}
		assertGraphTextBehavior(t, nested[0], 1, 1)
		nested[0] = field.Snapshot(field.Text("escaped"))
		if definition.Fields()[0].Name() != "code" {
			t.Fatal("nested reader exposed mutable children")
		}
	}
	cases := field.Snapshot(blocks).Blocks()
	if len(cases) != 1 || cases[0].Labels.SingularTranslations["en"] != "Card" || cases[0].Fields[0].Name() != "code" {
		t.Fatal("Blocks aliases mutable case inputs")
	}
	assertGraphTextBehavior(t, cases[0].Fields[0], 1, 1)
	cases[0].Fields[0] = field.Snapshot(field.Text("escaped"))
	cases[0].Labels.SingularTranslations["en"] = "Escaped"
	again := field.Snapshot(blocks).Blocks()
	if again[0].Fields[0].Name() != "code" || again[0].Labels.SingularTranslations["en"] != "Card" {
		t.Fatal("Blocks reader exposed mutable case state")
	}
}

func TestProductionGraphEscapedAdminDraftAndPolicyReplacement(t *testing.T) {
	base := field.Text("code").Validate(graphRule).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}).
		Access(field.Access{Create: graphAllow, Read: graphAllow, Update: graphAllow}).
		Admin(field.Admin{Description: "Initial", LabelTranslations: map[string]string{"en": "Code"}, Extensions: map[string]store.Value{"plugin.hint": store.String("kept")}})
	var escaped *field.Admin
	var borrowed map[string]string
	a := base.EditAdmin(func(admin *field.Admin) {
		escaped = admin
		admin.Description = "Edited"
		borrowed = map[string]string{"en": "Edited label"}
		admin.LabelTranslations = borrowed
	})
	escaped.Description = "Escaped"
	escaped.Extensions["plugin.hint"] = store.Null()
	borrowed["en"] = "Escaped label"
	admin := field.Snapshot(a).AdminPolicy()
	if admin.Description != "Edited" || admin.LabelTranslations["en"] != "Edited label" || admin.Extensions["plugin.hint"].Kind() != store.ValueString {
		t.Fatal("escaped edit draft mutated the committed field")
	}
	if field.Snapshot(base).AdminPolicy().Description != "Initial" {
		t.Fatal("edit mutated the source field")
	}
	replaced := a.Admin(field.Admin{Description: "Replacement"}).Hooks(field.Hooks[string]{}).Access(field.Access{Read: graphAllow})
	if got := field.Snapshot(replaced).AdminPolicy(); got.Description != "Replacement" || len(got.LabelTranslations) != 0 || len(got.Extensions) != 0 {
		t.Fatal("whole admin policy replacement retained omitted members")
	}
	if len(replaced.HookPolicy().BeforeChange) != 0 || len(replaced.Validators()) != 1 {
		t.Fatal("whole hook replacement affected unrelated validators")
	}
	if access := field.Snapshot(replaced).AccessPolicy(); access.Create != nil || access.Update != nil || access.Read == nil {
		t.Fatal("whole access replacement retained omitted operations")
	}
	if got := field.Snapshot(base).BehaviorSummary(); !reflect.DeepEqual(got.Hooks, map[string]int{"beforeChange": 1}) {
		t.Fatalf("replacement changed base summary: %#v", got)
	}
}

func TestProductionGraphExplicitEditsPreserveBehaviorAndRejectIncompatibleKinds(t *testing.T) {
	base := field.Text("code").Validate(graphRule).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}).
		Access(field.Access{Create: graphAllow, Read: graphAllow, Update: graphAllow}).
		Admin(field.Admin{Description: "Original", Editor: field.Component("app:Code", store.Object(store.Values{"style": store.String("mono")})), Extensions: map[string]store.Value{"plugin.hint": store.String("retained")}}).
		Private("plugin.secret", store.String("private retained"))
	graph := field.Fields{field.Group("meta", field.Fields{base, field.Number("priority")})}
	var escapedRoot, escapedChildren *field.ChildrenDraft
	edited, err := graph.Edit(func(root *field.ChildrenDraft) error {
		escapedRoot = root
		return root.EditChildren("meta", func(children *field.ChildrenDraft) error {
			escapedChildren = children
			if err := children.EditText("code", func(text field.TextField) field.TextField {
				return text.MaxLength(120).EditAdmin(func(admin *field.Admin) { admin.Description = "Edited" }).
					Validate(graphRule).AppendHooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}}).
					RestrictAccess(field.Access{Update: graphDeny})
			}); err != nil {
				return err
			}
			if err := children.Insert(1, field.Text("inserted")); err != nil {
				return err
			}
			return children.Replace("priority", field.Text("priority"))
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := escapedChildren.Replace("code", field.Text("code").Label("Escaped")); err != nil {
		t.Fatal(err)
	}
	if err := escapedRoot.Replace("meta", field.Text("meta")); err != nil {
		t.Fatal(err)
	}
	children := field.Snapshot(edited[0]).Fields()
	if len(children) != 3 || children[1].Name() != "inserted" || children[2].Kind() != field.KindText {
		t.Fatal("explicit insertion/replacement did not preserve committed child order")
	}
	text := assertGraphTextBehavior(t, children[0], 2, 2)
	definition := field.Snapshot(text)
	if private, exists := definition.Private("plugin.secret"); !exists || private.Kind() != store.ValueString {
		t.Fatal("graph edit dropped private owner attachment")
	}
	admin := definition.AdminPolicy()
	if admin.Description != "Edited" || admin.Editor.Key != "app:Code" || admin.Extensions["plugin.hint"].Kind() != store.ValueString {
		t.Fatal("targeted edit lost unmentioned presentation attachments")
	}
	access := definition.AccessPolicy()
	if allowed, err := access.Create(operation.AccessContext{}); !allowed || err != nil {
		t.Fatal("update restriction replaced create access")
	}
	if allowed, err := access.Read(operation.AccessContext{}); !allowed || err != nil {
		t.Fatal("update restriction replaced read access")
	}
	if allowed, err := access.Update(operation.AccessContext{}); allowed || err != nil {
		t.Fatal("update restriction failed")
	}
	assertGraphTextBehavior(t, field.Snapshot(graph[0]).Fields()[0], 1, 1)
	if field.Snapshot(graph[0]).Fields()[1].Kind() != field.KindNumber {
		t.Fatal("explicit replacement mutated the input graph")
	}

	rolledBack, err := graph.Edit(func(root *field.ChildrenDraft) error {
		return root.EditChildren("meta", func(children *field.ChildrenDraft) error {
			if err := children.Insert(0, field.Text("temporary")); err != nil {
				return err
			}
			return children.EditText("priority", func(text field.TextField) field.TextField { return text.Label("Invalid") })
		})
	})
	var editError *field.EditError
	if !errors.As(err, &editError) || editError.Code != "incompatible_kind" || editError.Name != "priority" || editError.Expected != "TextField" || editError.Actual != "NumberField" {
		t.Fatalf("incompatible edit diagnostic = %#v, %v", editError, err)
	}
	if len(field.Snapshot(rolledBack[0]).Fields()) != 2 {
		t.Fatal("failed edit published partial child insertion")
	}
}

func TestProductionGraphAllBranchEditsDetachEscapedDrafts(t *testing.T) {
	code := field.Text("code").Validate(graphRule).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{graphNormalize}})
	plugin := field.Plugin("rich", "example.rich", nil).EmbeddedTrees(field.EmbeddedTree{
		Key: "document", Root: []string{"root"}, Children: "children", Tag: "type",
		Cases: []field.EmbeddedTreeCase{{TagValue: "embedded", Payload: "payload", Discriminator: "blockType", Identity: "id", Types: []field.Block{
			{Slug: "card", Fields: field.Fields{code}},
		}}},
	}).Private("example.rich", store.String("retained"))
	graph := field.Fields{
		field.Group("group", field.Fields{code}), field.Array("rows", field.Fields{code}),
		field.Blocks("content", field.Block{Slug: "card", Fields: field.Fields{code}}),
		field.NamedTab("tab", "Named", field.Fields{code}),
		field.UnnamedTab("Unnamed", field.Fields{code}), plugin,
	}
	for index, input := range graph {
		t.Run(string(field.Snapshot(input).Boundary())+input.Name(), func(t *testing.T) {
			branches := field.Snapshot(input).Branches()
			if len(branches) != 1 {
				t.Fatalf("fixture %d exposes %d branches", index, len(branches))
			}
			var escaped *field.ChildrenDraft
			edited, err := (field.Fields{input}).Edit(func(root *field.ChildrenDraft) error {
				return root.EditBranch(input.Name(), branches[0].Selector, func(draft *field.ChildrenDraft) error {
					escaped = draft
					return draft.EditText("code", func(text field.TextField) field.TextField { return text.Label("Edited") })
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := escaped.Insert(1, field.Text("escaped")); err != nil {
				t.Fatal(err)
			}
			committed := field.Snapshot(edited[0]).Branches()[0]
			if len(committed.Fields) != 1 || field.Snapshot(committed.Fields[0]).Label() != "Edited" {
				t.Fatal("escaped branch draft changed committed graph")
			}
			assertGraphTextBehavior(t, committed.Fields[0], 1, 1)
			committed.Fields[0] = field.Text("escaped")
			if field.Snapshot(edited[0]).Branches()[0].Fields[0].Name() != "code" {
				t.Fatal("branch reader exposed mutable children")
			}
			if field.Snapshot(field.Snapshot(input).Branches()[0].Fields[0]).Label() != "" {
				t.Fatal("branch edit mutated the source graph")
			}
		})
	}
}

func TestProductionGraphTraversalAndExplicitSymbolicRenames(t *testing.T) {
	dependent := field.Text("code").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("kind"), "link")})
	graph := field.Fields{
		field.Text("kind"), dependent,
		field.Group("nested", field.Fields{field.Text("kind"), dependent}),
		field.Row(field.Fields{dependent.Rename("wrapped")}),
		field.Text("rootBound").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("kind"), "link")}),
	}
	edited, err := graph.Edit(func(root *field.ChildrenDraft) error { return root.RenameRoot("kind", "type") })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"code": "type", "nested.code": "kind", "wrapped": "type", "rootBound": "type"}
	actual := map[string]string{}
	if err := edited.Walk(func(visit field.Visit) error {
		d := field.Snapshot(visit.Node)
		if e := d.AdminPolicy().VisibleWhen; !e.IsZero() {
			name := d.Name()
			if len(visit.Branches) > 0 && visit.Branches[0].Boundary == field.StoredObject {
				name = "nested." + name
			}
			actual[name] = e.Reference().Path()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("rename crossed data scope or skipped layout: %#v; want %#v", actual, want)
	}
	if field.Snapshot(graph[1]).AdminPolicy().VisibleWhen.Reference().Path() != "kind" {
		t.Fatal("symbolic rename mutated original graph")
	}
}

func TestProductionGraphReplacementInputsAndReadHookViewsAreDetached(t *testing.T) {
	output := field.OutputTransform[string](func(_ operation.ReadContext, value operation.Value[string]) (operation.Change[string], error) {
		return operation.Replace(value), nil
	})
	validators := []field.Validator[string]{graphRule}
	reads := field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{output}}
	base := field.Text("code").Validate(graphRule)
	replaced := base.ReplaceValidators(validators...).ReadHooks(reads)
	appended := replaced.AppendReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{output}})
	validators[0] = nil
	reads.AfterRead[0] = nil
	if len(base.Validators()) != 1 || len(base.ReadHookPolicy().AfterRead) != 0 {
		t.Fatal("policy replacement changed the base field")
	}
	if len(replaced.ReadHookPolicy().AfterRead) != 1 || len(appended.ReadHookPolicy().AfterRead) != 2 {
		t.Fatal("read hook append mutated an earlier field")
	}
	if _, err := replaced.Validators()[0](operation.ValidationContext{}, operation.Empty[string]()); err != nil {
		t.Fatal(err)
	}
	view := appended.ReadHookPolicy()
	view.AfterRead[0] = nil
	view.AfterRead[1] = nil
	for _, callback := range appended.ReadHookPolicy().AfterRead {
		if callback == nil {
			t.Fatal("read hook input or returned slice aliases the field")
		}
		if _, err := callback(operation.ReadContext{}, operation.Present("value")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProductionGraphExplicitInsertConflictAndStructuralReferenceErrors(t *testing.T) {
	graph := field.Fields{field.Text("code")}
	for _, edit := range []func(*field.ChildrenDraft) error{
		func(draft *field.ChildrenDraft) error { return draft.Insert(0, field.Text("code")) },
		func(draft *field.ChildrenDraft) error { return draft.Insert(-1, field.Text("other")) },
		func(draft *field.ChildrenDraft) error {
			return draft.Insert(0, field.Text("other").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("owner..code"), "x")}))
		},
	} {
		result, err := graph.Edit(edit)
		if err == nil {
			t.Fatal("invalid explicit edit was accepted")
		}
		if len(result) != 1 || result[0].Name() != "code" {
			t.Fatal("rejected edit changed graph")
		}
	}
}

func TestVisibilityRenameRebindsWholeConditionTreesAndDottedPaths(t *testing.T) {
	visibility := field.All(
		field.Equal(field.Sibling("settings.kind"), "link"),
		field.Any(
			field.NotEqual(field.Root("settings.kind"), "private"),
			field.Not(field.OneOf(field.Sibling("settings.kind"), "disabled", "hidden")),
		),
	)
	dependent := field.Text("code").Admin(field.Admin{VisibleWhen: visibility})
	graph := field.Fields{
		field.Group("settings", field.Fields{field.Text("kind")}),
		dependent,
		field.Group("nested", field.Fields{
			field.Group("settings", field.Fields{field.Text("kind")}), dependent,
		}),
	}
	edited, err := graph.Edit(func(draft *field.ChildrenDraft) error {
		return draft.RenameRoot("settings", "preferences")
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := func(condition field.Condition) []string {
		return []string{
			condition.Conditions()[0].Reference().Path(),
			condition.Conditions()[1].Conditions()[0].Reference().Path(),
			condition.Conditions()[1].Conditions()[1].Conditions()[0].Reference().Path(),
		}
	}
	rootCondition := field.Snapshot(edited[1]).AdminPolicy().VisibleWhen
	if got := paths(rootCondition); !reflect.DeepEqual(got, []string{"preferences.kind", "preferences.kind", "preferences.kind"}) {
		t.Fatalf("root condition paths = %v", got)
	}
	nestedCondition := field.Snapshot(field.Snapshot(edited[2]).Fields()[1]).AdminPolicy().VisibleWhen
	if got := paths(nestedCondition); !reflect.DeepEqual(got, []string{"settings.kind", "preferences.kind", "settings.kind"}) {
		t.Fatalf("nested condition crossed sibling scope = %v", got)
	}
	if got := paths(visibility); !reflect.DeepEqual(got, []string{"settings.kind", "settings.kind", "settings.kind"}) {
		t.Fatalf("rename mutated original condition = %v", got)
	}
	if rootCondition.Conditions()[1].Conditions()[0].Operator() != field.ConditionNotEquals || rootCondition.Conditions()[1].Conditions()[1].Conditions()[0].Operator() != field.ConditionOneOf {
		t.Fatal("rename changed predicate operators")
	}
}

func TestCheckedEditsDiagnoseConcreteCardinalityAndTargetShapes(t *testing.T) {
	cases := []struct {
		name             string
		check            func() error
		expected, actual string
	}{
		{"layout", func() error { _, err := field.AsText(field.Row(field.Fields{field.Text("title")})); return err }, "TextField", "LayoutField"},
		{"select", func() error {
			_, err := field.AsSelect(field.MultiSelect("tags", "a"))
			return err
		}, "SelectField", "MultiSelectField"},
		{"multi-select", func() error { _, err := field.AsMultiSelect(field.Select("tag", "a")); return err }, "MultiSelectField", "SelectField"},
		{"relationship cardinality", func() error { _, err := field.AsRelationship(field.Relationships("authors", "users")); return err }, "RelationshipField", "RelationshipsField"},
		{"relationship targets", func() error {
			_, err := field.AsRelationship(field.PolymorphicRelationship("owner", "users", "teams"))
			return err
		}, "RelationshipField", "PolymorphicRelationshipField"},
		{"polymorphic cardinality", func() error {
			_, err := field.AsPolymorphicRelationship(field.PolymorphicRelationships("owners", "users", "teams"))
			return err
		}, "PolymorphicRelationshipField", "PolymorphicRelationshipsField"},
		{"uploads", func() error { _, err := field.AsUpload(field.Uploads("media", "files")); return err }, "UploadField", "UploadsField"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := test.check()
			var editError *field.EditError
			if !errors.As(err, &editError) || editError.Expected != test.expected || editError.Actual != test.actual {
				t.Fatalf("shape diagnostic = %#v (%v)", editError, err)
			}
			if !strings.Contains(err.Error(), "expected "+test.expected+", got "+test.actual) {
				t.Fatal(err)
			}
		})
	}
}
