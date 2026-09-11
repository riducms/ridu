package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

type runtimeResolutionProbe struct {
	calls   *int
	retired *bool
}

func (runtimeResolutionProbe) Key() string { return "runtime-resolution-probe" }
func (p runtimeResolutionProbe) TransformFields(_ ridu.FieldGraphContext, fields field.Fields) (field.Fields, error) {
	if *p.retired {
		panic("CRUD reentered graph authoring")
	}
	*p.calls++
	return fields.Edit(func(d *field.ChildrenDraft) error {
		return d.EditText("code", func(f field.TextField) field.TextField {
			return f.Validate(func(c operation.Context, v operation.Value[string]) ([]operation.Issue, error) {
				return nil, nil
			})
		})
	})
}

// Configuration becomes unavailable immediately after New. CRUD must use the
// resolved callbacks and references without revisiting a graph transform, editor,
// or field factory. This is a request-path sanity probe, not a throughput claim.
func TestFieldGraphCRUDConsumesResolvedBindings(t *testing.T) {
	calls, constructions, checks := 0, 0, 0
	retired := false
	factory := func() field.TextField {
		constructions++
		return field.Text("code").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("kind"), "active")}).Validate(func(c operation.Context, v operation.Value[string]) ([]operation.Issue, error) {
			checks++
			if c.SchemaOccurrenceID == "" || c.OccurrenceID == "" {
				t.Fatal("unbound runtime identity")
			}
			if kind, _ := c.Siblings.String("kind"); kind != "active" {
				t.Fatalf("sibling = %q", kind)
			}
			return nil, nil
		})
	}
	app, err := ridu.New(ridu.Config{Name: "Runtime resolution probe", Plugins: []ridu.Plugin{runtimeResolutionProbe{&calls, &retired}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Text("kind"), factory()}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	retired = true
	for range 3 {
		doc, err := app.Local().Create(t.Context(), "pages", store.Values{"kind": store.String("active"), "code": store.String("one")}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = app.Local().Update(t.Context(), "pages", doc.ID, store.Values{"code": store.String("two")}, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err = app.Local().Find(t.Context(), "pages", doc.ID, ridu.FindOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err = app.Local().Delete(t.Context(), "pages", doc.ID, ridu.MutationOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || constructions != 1 || checks != 6 {
		t.Fatalf("transforms=%d factories=%d validators=%d", calls, constructions, checks)
	}
}

func TestFieldGraphScalarViewsNormalizeEmptyWithinRetainedScopes(t *testing.T) {
	rawMissing, rawNull, validated := 0, 0, 0
	code := field.Text("code").Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(c operation.Context, v operation.Value[store.Value]) (operation.Change[store.Value], error) {
		if c.Operation != operation.Create && c.Operation != operation.Update {
			return operation.Keep[store.Value](), nil
		}
		value, present := v.Get()
		if !present {
			rawMissing++
		} else if value.Kind() == store.ValueNull {
			rawNull++
		}
		return operation.Keep[store.Value](), nil
	}}}).Validate(func(c operation.Context, v operation.Value[string]) ([]operation.Issue, error) {
		validated++
		if _, present := v.Get(); present {
			t.Fatal("empty scalar became a logical value")
		}
		if value, present := c.Siblings.Lookup("code"); !present || value.Kind() != store.ValueNull {
			t.Fatal("scalar view exposes stored absence")
		}
		_, prior := c.Prior.Lookup("code")
		if prior != (c.Operation == operation.Update) {
			t.Fatalf("prior occurrence existence = %v in %s", prior, c.Operation)
		}
		return nil, nil
	})
	app, err := ridu.New(ridu.Config{Name: "Portable callback views", Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{code, field.Text("other"), field.Group("group", field.Fields{code})}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := app.Local().Create(t.Context(), "pages", store.Values{"group": store.Object(store.Values{})}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Update(t.Context(), "pages", doc.ID, store.Values{"code": store.Null(), "other": store.String("changed")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rawMissing != 2 || rawNull != 1 || validated != 4 {
		t.Fatalf("missing=%d null=%d validators=%d", rawMissing, rawNull, validated)
	}
}
