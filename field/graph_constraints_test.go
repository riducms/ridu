package field_test

import (
	"github.com/riducms/ridu/field"
	"testing"
)

func TestFluentConstraintReplacementRevalidatesFinalSettings(t *testing.T) {
	invalid := field.Text("code").MaxLength(-1)
	fixed := invalid.MaxLength(20)
	if len(field.Snapshot(invalid).Issues()) == 0 {
		t.Fatal("invalid base has no diagnostic")
	}
	if issues := field.Snapshot(fixed).Issues(); len(issues) != 0 {
		t.Fatalf("replaced invalid scalar retained stale errors: %v", issues)
	}
	badBounds := field.Text("code").MinLength(20).MaxLength(10)
	if len(field.Snapshot(badBounds).Issues()) == 0 {
		t.Fatal("invalid bounds admitted")
	}
	if issues := field.Snapshot(badBounds.MaxLength(30)).Issues(); len(issues) != 0 {
		t.Fatalf("repaired bounds retained stale errors: %v", issues)
	}
	badDefault := field.Text("code").Default("too long").MaxLength(2)
	if len(field.Snapshot(badDefault).Issues()) == 0 {
		t.Fatal("invalid default admitted")
	}
	if issues := field.Snapshot(badDefault.MaxLength(50)).Issues(); len(issues) != 0 {
		t.Fatalf("repaired default retained stale errors: %v", issues)
	}
	number := field.Number("rank").Min(20).Max(10)
	if len(field.Snapshot(number).Issues()) == 0 {
		t.Fatal("invalid numeric bounds admitted")
	}
	if issues := field.Snapshot(number.Min(0)).Issues(); len(issues) != 0 {
		t.Fatalf("repaired number retained stale errors: %v", issues)
	}
	rows := field.Array("items", field.Fields{field.Text("name")}).MinRows(3).MaxRows(2)
	if len(field.Snapshot(rows).Issues()) == 0 {
		t.Fatal("invalid row bounds admitted")
	}
	if issues := field.Snapshot(rows.MaxRows(4)).Issues(); len(issues) != 0 {
		t.Fatalf("repaired rows retained stale errors: %v", issues)
	}
}

func TestNilCallbacksAreAuthoringDiagnostics(t *testing.T) {
	node := field.Text("code").Validate(nil).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{nil}}).ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{nil}})
	issues := field.Snapshot(node).Issues()
	if len(issues) != 3 {
		t.Fatalf("want three nil callback diagnostics, got %v", issues)
	}
	for _, issue := range issues {
		if issue.Code != "nil_field_callback" || issue.Path == "" {
			t.Fatalf("unstructured issue: %+v", issue)
		}
	}
	if issues := field.Snapshot(node.ReplaceValidators().Hooks(field.Hooks[string]{}).ReadHooks(field.ReadHooks[string]{})).Issues(); len(issues) != 0 {
		t.Fatalf("replaced callback groups retained diagnostics: %v", issues)
	}
}

func TestLayoutEditingUsesAuthoredPositionsAndStoredScopeNames(t *testing.T) {
	fields := field.Fields{field.Collapsible("details", field.Fields{field.Text("inside")}), field.Text("details")}
	if _, err := fields.Edit(nil); err != nil {
		t.Fatalf("valid separate layout/stored names rejected: %v", err)
	}
	edited, err := fields.Edit(func(d *field.ChildrenDraft) error {
		if err := d.EditBranchAt(0, field.BranchSelector{Boundary: field.Layout}, func(children *field.ChildrenDraft) error {
			return children.EditText("inside", func(f field.TextField) field.TextField { return f.Label("Inside") })
		}); err != nil {
			return err
		}
		text, err := field.AsText(d.Fields()[1])
		if err != nil {
			return err
		}
		return d.ReplaceAt(1, text.Label("Details"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Snapshot(edited[1]).Label() != "Details" || field.Snapshot(field.Snapshot(edited[0]).Fields()[0]).Label() != "Inside" {
		t.Fatal("position edit lost layout or stored child")
	}
	if _, err := fields.Edit(func(d *field.ChildrenDraft) error { return d.Insert(2, field.Text("inside")) }); err == nil {
		t.Fatal("duplicate stored name through layout admitted")
	}
	if _, err := fields.Edit(func(d *field.ChildrenDraft) error {
		return d.Insert(2, field.Collapsible("details", field.Fields{field.Text("another")}))
	}); err != nil {
		t.Fatalf("presentation labels treated as stored identity: %v", err)
	}
	if _, err := fields.Edit(func(d *field.ChildrenDraft) error {
		return d.ReplaceAt(0, field.Row(field.Fields{field.Text("details")}))
	}); err == nil {
		t.Fatal("replacement duplicate through layout admitted")
	}
}
