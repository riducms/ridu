package consumer_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"example.com/ridu-gate1/consumer"
	"example.com/ridu-gate1/field"
	"example.com/ridu-gate1/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type lookupProbe struct {
	calls      int
	context    context.Context
	collection schema.CollectionSlug
	id         operation.ID
	err        error
}

func (reader *lookupProbe) FindByID(ctx context.Context, collection schema.CollectionSlug, id operation.ID) (store.Document, error) {
	reader.calls++
	reader.context, reader.collection, reader.id = ctx, collection, id
	return store.Document{ID: string(id), Values: store.Values{"enabled": store.Boolean(true)}}, reader.err
}

func TestTypedCallbacksCompileAndRetainTheirLogicalContracts(t *testing.T) {
	// Direct callback calls below inspect the attached function values only.
	// No operation engine, resolver or callback dispatcher exists in this probe.
	reader := &lookupProbe{}
	ctx := operation.ValidationContext{
		Context:  context.Background(),
		Siblings: operation.Snapshot(store.Values{"supplier": store.String("supplier-1")}),
		Local:    reader,
	}
	issues, err := consumer.SupplierExists(ctx, operation.Present("sku"))
	if err != nil || len(issues) != 0 || reader.calls != 1 || reader.collection != "suppliers" || reader.id != "supplier-1" || reader.context != ctx.Context {
		t.Fatalf("bound lookup = %#v, issues=%v, err=%v", reader, issues, err)
	}
	ctx.Siblings = operation.Snapshot(nil)
	issues, err = consumer.SupplierExists(ctx, operation.Present("sku"))
	if err != nil || len(issues) != 1 || issues[0].Code != "supplier_required" || issues[0].Message == "" || issues[0].Path.String() != "" || reader.calls != 1 {
		t.Fatalf("relative structured issue = %#v, err=%v", issues, err)
	}
	ctx.Siblings = operation.Snapshot(store.Values{"supplier": store.String("supplier-1")})
	reader.err = errors.New("lookup failed")
	if _, err := consumer.SupplierExists(ctx, operation.Present("sku")); !errors.Is(err, reader.err) {
		t.Fatalf("operational error was not preserved: %v", err)
	}

	rawChange, err := consumer.TrimRaw(operation.WriteContext{}, operation.Present(store.String(" title ")))
	raw, replaced := rawChange.Replacement()
	rawValue, present := raw.Get()
	if text, _ := rawValue.StringValue(); err != nil || !replaced || !present || text != "title" {
		t.Fatal("finite raw transform did not retain its typed return")
	}
	malformed, err := consumer.TrimRaw(operation.WriteContext{}, operation.Present(store.Object(store.Values{"wrong": store.Boolean(true)})))
	if _, replaced := malformed.Replacement(); err != nil || replaced {
		t.Fatal("raw transform incorrectly assumed malformed input was a string")
	}
	write, err := consumer.Prefix(operation.WriteContext{}, operation.Present("title"))
	assertStringChange(t, write, err, "link:title")
	output, err := consumer.UppercaseOutput(operation.ReadContext{}, operation.Present("title"))
	assertStringChange(t, output, err, "TITLE")

	owner, err := consumer.NormalizeOwner(operation.WriteContext{}, operation.Present(operation.ID(" user-1 ")))
	ownerValue, replaced := owner.Replacement()
	if id, present := ownerValue.Get(); err != nil || !replaced || !present || id != operation.ID("user-1") {
		t.Fatal("reference write did not use the typed ID")
	}
	original := operation.Populated(store.Document{ID: "user-1", Values: store.Values{"name": store.String("Ada")}})
	read, err := consumer.OwnerOutput(operation.ReadContext{}, operation.Present(original))
	readValue, replaced := read.Replacement()
	reference, present := readValue.Get()
	document, populated := reference.Document()
	if err != nil || !replaced || !present || !populated || reference.ID() != "user-1" {
		t.Fatal("read callback did not retain its distinct populated output type")
	}
	if label, _ := document.Values["label"].StringValue(); label != "Owner user-1" {
		t.Fatalf("populated callback label = %q", label)
	}
	originalDocument, _ := original.Document()
	if _, exists := originalDocument.Values["label"]; exists {
		t.Fatal("read callback mutated the original populated value")
	}
}

func TestFactoryRefinementRetainsBehaviorAndSymbolicReferences(t *testing.T) {
	primary := consumer.Link("primary")
	secondary := consumer.Link("secondary").Label("Footer link").EditAdmin(func(admin *field.Admin) {
		admin.Description = "Footer destination."
		admin.Labels["en"] = "Footer"
	})
	refined, err := secondary.EditChildren(func(children *field.ChildrenDraft) error {
		return children.EditText("href", func(text field.TextField) field.TextField {
			return text.Label("Footer URL").EditAdmin(func(admin *field.Admin) { admin.Labels["en"] = "Footer URL" })
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if primary.LabelText() != "Link" || primary.AdminPolicy().Labels["en"] != "Link" || refined.LabelText() != "Footer link" {
		t.Fatal("independent factory calls shared refinements")
	}
	if len(refined.HookPolicy().BeforeChange) != 1 || refined.AccessPolicy().Read == nil || refined.AccessPolicy().Update == nil {
		t.Fatal("group refinement lost attached behavior")
	}
	allowed, err := refined.AccessPolicy().Update(operation.AccessContext{Actor: operation.Actor{ID: "editor"}})
	if err != nil || !allowed {
		t.Fatal("refined group lost its access callback")
	}
	groupChange, err := refined.HookPolicy().BeforeChange[0](operation.WriteContext{}, operation.Present(store.Object(nil)))
	if _, replaced := groupChange.Replacement(); err != nil || replaced {
		t.Fatal("refined group lost its original hook")
	}
	assertReference(t, refined.AdminPolicy().VisibleWhen.Reference(), field.RootScope, "tenant")

	renamed := refined.Rename("footer")
	nested := field.Group("page", field.Fields{renamed})
	footer := inspectText(t, field.Fields{nested}, []string{"page", "footer"}, "href")
	untouched := inspectText(t, field.Fields{primary}, []string{"primary"}, "href")
	if footer.LabelText() != "Footer URL" || untouched.LabelText() != "Destination" || untouched.AdminPolicy().Labels["en"] != "Destination" {
		t.Fatal("child refinement escaped its factory instance")
	}
	primaryEdited, err := primary.EditAdmin(func(admin *field.Admin) { admin.Labels["en"] = "Header" }).
		EditChildren(func(children *field.ChildrenDraft) error {
			return children.EditText("href", func(text field.TextField) field.TextField {
				return text.EditAdmin(func(admin *field.Admin) { admin.Labels["en"] = "Header URL" })
			})
		})
	if err != nil {
		t.Fatal(err)
	}
	if primaryEdited.AdminPolicy().Labels["en"] != "Header" || refined.AdminPolicy().Labels["en"] != "Footer" {
		t.Fatal("editing the first factory instance changed the second")
	}
	footerAfterPrimaryEdit := inspectText(t, field.Fields{refined}, []string{"secondary"}, "href")
	if footerAfterPrimaryEdit.AdminPolicy().Labels["en"] != "Footer URL" {
		t.Fatal("editing the first factory's child changed the second factory's child")
	}
	assertReference(t, footer.AdminPolicy().VisibleWhen.Reference(), field.SiblingScope, "kind")
	assertReference(t, renamed.AdminPolicy().VisibleWhen.Reference(), field.RootScope, "tenant")
	if len(footer.Validators()) != 1 || len(footer.HookPolicy().BeforeValidate) != 1 || len(footer.HookPolicy().BeforeChange) != 1 || len(footer.ReadHookPolicy().AfterRead) != 1 {
		t.Fatal("copy, rename or nesting discarded child behavior")
	}
}

func TestExternalGraphDecoratorPreservesUnmentionedPolicies(t *testing.T) {
	config := consumer.Configuration()
	decorated, err := consumer.Decorate(config.Fields)
	if err != nil {
		t.Fatal(err)
	}
	text := inspectText(t, decorated, []string{"primary"}, "href")
	original := inspectText(t, config.Fields, []string{"primary"}, "href")
	secondary := inspectText(t, decorated, []string{"secondary"}, "href")
	if text.LengthLimit() != 256 || original.LengthLimit() != 120 || secondary.LengthLimit() != 120 {
		t.Fatal("typed edit changed the original or another factory occurrence")
	}
	admin := text.AdminPolicy()
	if admin.Description != "Refined destination." || admin.Editor.Key != "link-input" || admin.Labels["en"] != "Destination" {
		t.Fatal("detached admin edit lost unmentioned settings")
	}
	if hint, _ := admin.Extensions["consumer:hint"].StringValue(); hint != "preserve" {
		t.Fatal("admin edit lost unrelated extension metadata")
	}
	assertReference(t, admin.VisibleWhen.Reference(), field.SiblingScope, "kind")
	validators, hooks := text.Validators(), text.HookPolicy()
	if len(validators) != 2 || len(hooks.BeforeValidate) != 1 || len(hooks.BeforeChange) != 2 || len(text.ReadHookPolicy().AfterRead) != 1 {
		t.Fatal("explicit append replaced an existing callback or another phase")
	}
	issues, err := validators[0](operation.ValidationContext{}, operation.Present(""))
	if err != nil || len(issues) != 1 || issues[0].Code != "link_blank" {
		t.Fatal("original validator was lost")
	}
	issues, err = validators[1](operation.ValidationContext{}, operation.Present("reserved"))
	if err != nil || len(issues) != 1 || issues[0].Code != "reserved_link" {
		t.Fatal("appended validator was lost or reordered")
	}
	first, err := hooks.BeforeChange[0](operation.WriteContext{}, operation.Present("href"))
	assertStringChange(t, first, err, "link:href")
	second, err := hooks.BeforeChange[1](operation.WriteContext{}, operation.Present("href"))
	assertStringChange(t, second, err, "href:decorated")
	access := text.AccessPolicy()
	for _, rule := range []field.AccessRule{access.Create, access.Read} {
		if allowed, err := rule(operation.AccessContext{}); err != nil || !allowed {
			t.Fatal("restricting update replaced create/read access")
		}
	}
	if allowed, err := access.Update(operation.AccessContext{}); err != nil || allowed {
		t.Fatal("explicit update restriction was not retained")
	}
	if allowed, err := original.AccessPolicy().Update(operation.AccessContext{}); err != nil || !allowed {
		t.Fatal("restriction mutated original access")
	}
	var names []string
	if err := decorated.Walk(func(node field.Node) error {
		names = append(names, node.Name())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"title", "priority", "meta", "description", "tenant", "sku", "primary", "kind", "href", "weight", "secondary", "kind", "href"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("heterogeneous traversal = %v; want %v", names, want)
	}
	kind := inspectText(t, decorated, []string{"primary"}, "kind")
	if kind.LabelText() != "Link kind" {
		t.Fatal("deliberate replacement did not replace the child")
	}
}

func TestExternalEditDetectsKindMismatchAndAllowsDeliberateReplacement(t *testing.T) {
	original := field.Fields{field.Number("priority").Min(0)}
	called := false
	unchanged, err := original.Edit(func(children *field.ChildrenDraft) error {
		return children.EditText("priority", func(text field.TextField) field.TextField {
			called = true
			return text.MaxLength(1)
		})
	})
	var editError *field.EditError
	if !errors.As(err, &editError) || editError.Code != "incompatible_kind" || editError.Expected != field.TextKind || editError.Actual != field.NumberKind || called || unchanged[0].Kind() != field.NumberKind {
		t.Fatalf("incompatible edit = %v; callback called=%v", err, called)
	}
	replaced, err := original.Edit(func(children *field.ChildrenDraft) error {
		return children.Replace("priority", field.Text("priority").MaxLength(20))
	})
	if err != nil || replaced[0].Kind() != field.TextKind || original[0].Kind() != field.NumberKind {
		t.Fatalf("deliberate kind replacement failed: %v", err)
	}
}

// Inspection uses the same supported typed edit boundary, returning each value
// unchanged. Ordinary application construction/editing needs no type assertion.
func inspectText(t *testing.T, fields field.Fields, parents []string, name string) field.TextField {
	t.Helper()
	var result field.TextField
	var inspect func(*field.ChildrenDraft, []string) error
	inspect = func(children *field.ChildrenDraft, path []string) error {
		if len(path) == 0 {
			return children.EditText(name, func(text field.TextField) field.TextField { result = text; return text })
		}
		return children.EditChildren(path[0], func(nested *field.ChildrenDraft) error { return inspect(nested, path[1:]) })
	}
	if _, err := fields.Edit(func(children *field.ChildrenDraft) error { return inspect(children, parents) }); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertStringChange(t *testing.T, change operation.Change[string], err error, want string) {
	t.Helper()
	value, replaced := change.Replacement()
	text, present := value.Get()
	if err != nil || !replaced || !present || text != want {
		t.Fatalf("change = %q present=%v replace=%v err=%v; want %q", text, present, replaced, err, want)
	}
}

func assertReference(t *testing.T, reference field.Reference, scope field.ReferenceScope, name string) {
	t.Helper()
	if reference.Scope() != scope || reference.Name() != name {
		t.Fatalf("symbolic reference = %s/%s; want %s/%s", reference.Scope(), reference.Name(), scope, name)
	}
}
