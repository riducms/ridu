package field

import (
	"errors"
	"reflect"
	"testing"

	"example.com/ridu-gate1/operation"
	"github.com/riducms/ridu/store"
)

func issue(code string) Validator[string] {
	return func(operation.ValidationContext, operation.Value[string]) ([]operation.Issue, error) {
		return []operation.Issue{{Code: code, Message: code}}, nil
	}
}

func write(text string) Transform[string] {
	return func(operation.WriteContext, operation.Value[string]) (operation.Change[string], error) {
		return operation.Replace(operation.Present(text)), nil
	}
}

func raw(text string) RawTransform {
	return func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error) {
		return operation.Replace(operation.Present(store.String(text))), nil
	}
}

func output(text string) OutputTransform[string] {
	return func(operation.ReadContext, operation.Value[string]) (operation.Change[string], error) {
		return operation.Replace(operation.Present(text)), nil
	}
}

func validatorCodes(t *testing.T, f TextField) []string {
	t.Helper()
	var result []string
	for _, validator := range f.Validators() {
		issues, err := validator(operation.ValidationContext{}, operation.Present("sample"))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, issues[0].Code)
	}
	return result
}

func writeOutputs(t *testing.T, f TextField) []string {
	t.Helper()
	var result []string
	for _, hook := range f.HookPolicy().BeforeChange {
		change, err := hook(operation.WriteContext{}, operation.Present("sample"))
		if err != nil {
			t.Fatal(err)
		}
		value, replaced := change.Replacement()
		text, present := value.Get()
		if !replaced || !present {
			t.Fatal("expected a present replacement")
		}
		result = append(result, text)
	}
	return result
}

func TestImmutableCallsAndConcreteMethodSets(t *testing.T) {
	original := Text("title")
	var refined TextField = original.Required().Label("Title").MaxLength(120)
	original.Required()
	original.Label("Ignored")
	original.MaxLength(50)
	original.Validate(issue("ignored"))
	original.Hooks(Hooks[string]{BeforeChange: []Transform[string]{write("ignored")}})
	original.EditAdmin(func(a *Admin) { a.Description = "ignored" })
	if original.IsRequired() || original.LabelText() != "" || original.LengthLimit() != 0 || len(original.Validators()) != 0 || len(original.HookPolicy().BeforeChange) != 0 || original.AdminPolicy().Description != "" {
		t.Fatal("ignored immutable return mutated original")
	}
	if !refined.IsRequired() || refined.LabelText() != "Title" || refined.LengthLimit() != 120 {
		t.Fatal("concrete chain lost settings")
	}
	var number NumberField = Number("priority").Required().Label("Priority").Min(0)
	var group GroupField = Group("meta", Fields{refined}).Label("Metadata").Required()
	var relationship RelationshipField = Relationship("owner", "users").Required().Label("Owner").Access(Access{}).Hooks(Hooks[operation.ID]{})
	if number.Kind() != NumberKind || group.Kind() != GroupKind || relationship.Target() != "users" {
		t.Fatal("facade method sets")
	}
}

func TestPolicyInputsOutputsAndAppendBranchesOwnSlices(t *testing.T) {
	hooks := Hooks[string]{BeforeValidate: []RawTransform{raw("raw")}, BeforeChange: make([]Transform[string], 1, 8)}
	hooks.BeforeChange[0] = write("first")
	reads := ReadHooks[string]{AfterRead: []OutputTransform[string]{output("read")}}
	original := Text("title").Hooks(hooks).ReadHooks(reads).Validate(issue("first"))
	hooks.BeforeValidate[0], hooks.BeforeChange[0] = raw("corrupt"), write("corrupt")
	reads.AfterRead[0] = output("corrupt")
	borrowed := original.HookPolicy()
	borrowed.BeforeValidate[0], borrowed.BeforeChange[0] = raw("corrupt"), write("corrupt")
	original.ReadHookPolicy().AfterRead[0] = output("corrupt")
	original.Validators()[0] = issue("corrupt")
	left := original.AppendHooks(Hooks[string]{BeforeChange: []Transform[string]{write("left")}}).Validate(issue("left"))
	right := original.AppendHooks(Hooks[string]{BeforeChange: []Transform[string]{write("right")}}).Validate(issue("right"))
	for _, tc := range []struct {
		f    TextField
		want []string
	}{{original, []string{"first"}}, {left, []string{"first", "left"}}, {right, []string{"first", "right"}}} {
		if got := validatorCodes(t, tc.f); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("validators %v, want %v", got, tc.want)
		}
		if got := writeOutputs(t, tc.f); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("hooks %v, want %v", got, tc.want)
		}
	}
	change, err := original.HookPolicy().BeforeValidate[0](operation.WriteContext{}, operation.Value[store.Value]{})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := change.Replacement()
	finite, _ := v.Get()
	text, _ := finite.StringValue()
	if text != "raw" {
		t.Fatal("raw hooks alias caller")
	}
	read, err := original.ReadHookPolicy().AfterRead[0](operation.ReadContext{}, operation.Value[string]{})
	if err != nil {
		t.Fatal(err)
	}
	rv, _ := read.Replacement()
	text, _ = rv.Get()
	if text != "read" {
		t.Fatal("output hooks alias caller")
	}
	// Whole-group replacement intentionally clears unspecified phases.
	replaced := original.Hooks(Hooks[string]{BeforeChange: []Transform[string]{write("replacement")}})
	if len(replaced.HookPolicy().BeforeValidate) != 0 || len(original.HookPolicy().BeforeValidate) != 1 {
		t.Fatal("whole hook replacement became a merge")
	}
	if got := writeOutputs(t, replaced); !reflect.DeepEqual(got, []string{"replacement"}) {
		t.Fatal(got)
	}
	if len(original.Hooks(Hooks[string]{}).HookPolicy().BeforeChange) != 0 {
		t.Fatal("zero group must clear")
	}
}

func TestAdminOwnershipAndExplicitZeroIntent(t *testing.T) {
	props := store.Values{"swatches": store.List(store.String("blue"))}
	admin := Admin{
		Description: "help", ReadOnly: true,
		Labels:      map[string]string{"en": "Title"},
		Extensions:  map[string]store.Value{"unfamiliar:plugin": store.Object(props)},
		Editor:      Component("app:color", store.Object(props)),
		VisibleWhen: Eq(Sibling("kind"), store.String("external")),
	}
	original := Text("title").Admin(admin)
	admin.Labels["en"] = "corrupt"
	admin.Extensions["unfamiliar:plugin"] = store.Null()
	props["swatches"] = store.Null()
	var retained *Admin
	borrowedLabels := map[string]string{"en": "Edited"}
	edited := original.EditAdmin(func(a *Admin) {
		retained = a
		a.Description = ""
		a.ReadOnly = false
		a.Labels = borrowedLabels
	})
	retained.Description = "corrupt"
	retained.Extensions["unfamiliar:plugin"] = store.Null()
	borrowedLabels["en"] = "corrupt"
	snapshot := edited.AdminPolicy()
	snapshot.Labels["en"] = "corrupt"
	snapshot.Extensions["unfamiliar:plugin"] = store.Null()
	config, _ := snapshot.Editor.Config.CopyObject()
	config["swatches"] = store.Null()
	for _, f := range []TextField{original, edited} {
		a := f.AdminPolicy()
		if a.Editor.Key != "app:color" || a.VisibleWhen.Reference() != Sibling("kind") || a.Extensions["unfamiliar:plugin"].Kind() != store.ValueObject {
			t.Fatal("unmentioned settings lost or aliased")
		}
		config, _ := a.Editor.Config.CopyObject()
		if config["swatches"].Kind() != store.ValueList {
			t.Fatal("editor config aliases mutable input/output")
		}
	}
	if a := original.AdminPolicy(); a.Labels["en"] != "Title" || a.Description != "help" || !a.ReadOnly {
		t.Fatal("original changed")
	}
	if a := edited.AdminPolicy(); a.Labels["en"] != "Edited" || a.Description != "" || a.ReadOnly {
		t.Fatal("explicit zero edit not preserved")
	}
	replaced := original.Admin(Admin{Description: "replacement"}).AdminPolicy()
	if replaced.ReadOnly || replaced.Editor.Key != "" || replaced.Labels != nil || replaced.Extensions != nil {
		t.Fatal("whole Admin replacement inferred partial intent")
	}
}

func TestRestrictAccessConjoinsSpecifiedPolicyOnly(t *testing.T) {
	var calls []string
	allow := func(name string) AccessRule {
		return func(operation.AccessContext) (bool, error) { calls = append(calls, name); return true, nil }
	}
	base := Text("title").Access(Access{Create: allow("create"), Read: allow("read"), Update: allow("update")})
	deny := func(operation.AccessContext) (bool, error) { calls = append(calls, "restriction"); return false, nil }
	restricted := base.RestrictAccess(Access{Update: deny})
	for _, fn := range []AccessRule{restricted.AccessPolicy().Create, restricted.AccessPolicy().Read} {
		if ok, err := fn(operation.AccessContext{}); !ok || err != nil {
			t.Fatal("unrelated policy changed")
		}
	}
	if ok, err := restricted.AccessPolicy().Update(operation.AccessContext{}); ok || err != nil {
		t.Fatal("AND restriction lost")
	}
	if !reflect.DeepEqual(calls, []string{"create", "read", "update", "restriction"}) {
		t.Fatal(calls)
	}
	if ok, _ := base.AccessPolicy().Update(operation.AccessContext{}); !ok {
		t.Fatal("restriction mutated base")
	}
	calls = nil
	blocked := Text("x").Access(Access{Update: deny}).RestrictAccess(Access{Update: allow("never")})
	if ok, _ := blocked.AccessPolicy().Update(operation.AccessContext{}); ok || !reflect.DeepEqual(calls, []string{"restriction"}) {
		t.Fatal("AND did not short circuit")
	}
	failure := errors.New("lookup failure")
	broken := base.Access(Access{Update: func(operation.AccessContext) (bool, error) { return false, failure }}).RestrictAccess(Access{Update: allow("never")})
	if _, err := broken.AccessPolicy().Update(operation.AccessContext{}); !errors.Is(err, failure) {
		t.Fatal("operational error swallowed")
	}
	if replaced := base.Access(Access{Update: deny}).AccessPolicy(); replaced.Read != nil || replaced.Create != nil {
		t.Fatal("whole Access group did not replace")
	}
}

func TestChildrenSnapshotsRetainedDraftAndFailedEdit(t *testing.T) {
	text := Text("title").Validate(issue("owned"))
	children := Fields{&text, Number("priority")}
	group := Group("meta", children)
	text = Text("changed") // A *TextField input must have been frozen to a value.
	children[0] = Text("corrupt")
	group.Children()[0] = Text("corrupt")
	if group.Children()[0].Name() != "title" {
		t.Fatal("group aliases mutable children or pointer facade")
	}
	var retained *ChildrenDraft
	edited, err := group.EditChildren(func(d *ChildrenDraft) error {
		retained = d
		return d.EditText("title", func(f TextField) TextField { return f.Label("Edited") })
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := retained.Replace("title", Number("title")); err != nil {
		t.Fatal(err)
	}
	if edited.Children()[0].Kind() != TextKind || group.Children()[0].(TextField).LabelText() != "" {
		t.Fatal("draft or edited group aliases original")
	}
	failed, err := edited.EditChildren(func(d *ChildrenDraft) error {
		if err := d.Insert(0, Text("new")); err != nil {
			return err
		}
		return d.EditText("priority", func(f TextField) TextField { t.Fatal("incompatible callback invoked"); return f })
	})
	var incompatible *EditError
	if !errors.As(err, &incompatible) || incompatible.Code != "incompatible_kind" || incompatible.Actual != NumberKind {
		t.Fatalf("incompatible edit: %v", err)
	}
	if len(failed.Children()) != 2 || failed.Children()[0].Name() != "title" || len(edited.Children()) != 2 {
		t.Fatal("failed edit leaked partial changes")
	}
	fields := Fields{group}
	var nested *ChildrenDraft
	result, err := fields.Edit(func(d *ChildrenDraft) error {
		return d.EditChildren("meta", func(child *ChildrenDraft) error {
			nested = child
			return child.Insert(2, Text("extra"))
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := nested.Replace("title", Number("title")); err != nil {
		t.Fatal(err)
	}
	if result[0].Children()[0].Kind() != TextKind || len(fields[0].Children()) != 2 {
		t.Fatal("nested draft leaked after publish")
	}
}

func TestEditsPreserveInvisibleAttachmentsAndClosureOwnership(t *testing.T) {
	// Only the attachment owner knows this structure; public editing never reads
	// it. Its finite payload is immutable, but its closure capture is author-owned.
	capture := "before"
	text := Text("label").Validate(issue("original")).Hooks(Hooks[string]{BeforeChange: []Transform[string]{write("original")}})
	text.private = map[string]privateAttachment{"owner:secret": {value: store.String("private"), callback: func() string { return capture }}}
	group := Group("link", Fields{text})
	group.private = map[string]privateAttachment{"owner:group": {value: store.String("group"), callback: func() string { return capture }}}
	refined, err := group.Rename("footer").Label("Footer").EditAdmin(func(a *Admin) { a.Description = "footer help" }).EditChildren(func(d *ChildrenDraft) error {
		return d.EditText("label", func(f TextField) TextField {
			return f.Required().MaxLength(120).EditAdmin(func(a *Admin) { a.ReadOnly = true }).
				Validate(issue("added")).AppendHooks(Hooks[string]{BeforeChange: []Transform[string]{write("added")}}).
				RestrictAccess(Access{Update: func(operation.AccessContext) (bool, error) { return false, nil }})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	child := refined.Children()[0].(TextField)
	for _, attachment := range []privateAttachment{child.private["owner:secret"], refined.private["owner:group"]} {
		if attachment.value.Kind() != store.ValueString || attachment.callback == nil || attachment.callback() != "before" {
			t.Fatal("private attachment lost")
		}
	}
	if got := validatorCodes(t, child); !reflect.DeepEqual(got, []string{"original", "added"}) {
		t.Fatal(got)
	}
	if got := writeOutputs(t, child); !reflect.DeepEqual(got, []string{"original", "added"}) {
		t.Fatal(got)
	}
	capture = "after"
	if child.private["owner:secret"].callback() != "after" || text.private["owner:secret"].callback() != "after" {
		t.Fatal("closure captures must remain author-owned")
	}
	if len(text.Validators()) != 1 || text.IsRequired() || text.AdminPolicy().ReadOnly {
		t.Fatal("child refinement mutated original")
	}
}
