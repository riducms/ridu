package field_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// Link is an application factory; it only uses public field APIs.
func Link(name string) field.GroupField {
	label := field.Text("label").Required().
		Admin(field.Admin{Placeholder: "Read more", LabelTranslations: map[string]string{"fr": "Libellé"}}).
		Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
			if text, present := value.Get(); present && strings.TrimSpace(text) == "" {
				return []operation.Issue{{Code: "blank_link_label", Message: "Enter a link label"}}, nil
			}
			return nil, nil
		}).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{
			func(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
				if text, present := value.Get(); present {
					return operation.Replace(operation.Present(strings.TrimSpace(text))), nil
				}
				return operation.Keep[string](), nil
			},
		}}).
		Access(field.Access{Update: func(ctx operation.Context) (bool, error) { return ctx.Actor.ID == "editor", nil }}).
		Private("factory", store.String("link-label"))
	return field.Group(name, field.Fields{label, field.Text("href").Required()}).
		Private("factory", store.String("link-group"))
}

func CompactLink(name string) (field.GroupField, error) {
	return field.EditChild(Link(name), "label", field.AsText, func(label field.TextField) field.TextField {
		return label.MaxLength(32)
	})
}

func TestChildConveniencesRefineRealFactoriesWithoutLosingPolicies(t *testing.T) {
	base := Link("cta")
	var retainedAdmin *field.Admin
	refined, err := field.EditChild(base, "label", field.AsText, func(label field.TextField) field.TextField {
		return label.MaxLength(32).EditAdmin(func(admin *field.Admin) {
			admin.Description = "Keep navigation labels short"
			retainedAdmin = admin
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	retainedAdmin.Placeholder = "escaped edit"
	retainedAdmin.LabelTranslations["fr"] = "escaped edit"
	label, err := field.AsText(refined.Children()[0])
	if err != nil {
		t.Fatal(err)
	}
	view := field.Snapshot(label)
	if max, set := view.MaxLength(); !set || max != 32 {
		t.Fatal("child refinement did not retain concrete text methods")
	}
	if max, set := field.Snapshot(base.Children()[0]).MaxLength(); set || max != 0 {
		t.Fatal("child refinement mutated the factory value")
	}
	admin := label.AdminPolicy()
	if admin.Placeholder != "Read more" || admin.LabelTranslations["fr"] != "Libellé" || admin.Description != "Keep navigation labels short" {
		t.Fatalf("child admin refinement lost or aliased metadata: %#v", admin)
	}
	if !reflect.DeepEqual(view.BehaviorSummary(), field.Snapshot(base.Children()[0]).BehaviorSummary()) {
		t.Fatal("refinement changed unrelated executable policies")
	}
	issues, err := label.Validators()[0](operation.Context{}, operation.Present(" "))
	if err != nil || len(issues) != 1 || issues[0].Code != "blank_link_label" {
		t.Fatalf("factory validation was lost: %#v, %v", issues, err)
	}
	change, err := label.HookPolicy().BeforeChange[0](operation.Context{}, operation.Present(" Read more "))
	value, replace := change.Replacement()
	if text, _ := value.Get(); err != nil || !replace || text != "Read more" {
		t.Fatalf("factory hook was lost: %#v, %v", change, err)
	}
	allowed, err := label.AccessPolicy().Update(operation.Context{Actor: operation.Actor{ID: "editor"}})
	if err != nil || !allowed {
		t.Fatal("factory access rule was lost")
	}
	for _, test := range []struct {
		node field.Node
		want string
	}{{label, "link-label"}, {refined, "link-group"}} {
		private, found := field.Snapshot(test.node).Private("factory")
		if text, _ := private.StringValue(); !found || text != test.want {
			t.Fatal("refinement lost factory private attachments")
		}
	}
	compact, err := CompactLink("footer")
	if err != nil || compact.Name() != "footer" {
		t.Fatalf("external concrete factory failed: %v", err)
	}
}

func TestChildConveniencesAppendReplaceAndSupportArrayParents(t *testing.T) {
	base := Link("cta")
	extra := field.Checkbox("newTab").Default(false)
	appended, err := field.AppendChild(base, &extra)
	if err != nil {
		t.Fatal(err)
	}
	extra = extra.Label("Escaped pointer")
	if len(base.Children()) != 2 || len(appended.Children()) != 3 || field.Snapshot(appended.Children()[2]).Label() != "" {
		t.Fatal("append mutated the parent or retained a mutable input")
	}
	refined, err := field.EditChild(appended, "newTab", field.AsCheckbox, func(checkbox field.CheckboxField) field.CheckboxField {
		return checkbox.Default(true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := field.Snapshot(refined.Children()[2]).Default(); value.String() != "true" {
		t.Fatal("generic child edit did not support a non-text kind")
	}
	replacement := field.Textarea("label").Required().Private("replacement", store.String("deliberate"))
	replaced, err := field.ReplaceChild(refined, "label", &replacement)
	if err != nil {
		t.Fatal(err)
	}
	replacement = replacement.Label("Escaped replacement")
	replacedLabel := field.Snapshot(replaced.Children()[0])
	if replacedLabel.Kind() != field.KindTextarea || replacedLabel.HasBehavior() || replacedLabel.Label() != "" {
		t.Fatal("whole child replacement retained old policies or aliased input")
	}
	if _, retained := replacedLabel.Private("factory"); retained {
		t.Fatal("whole replacement unexpectedly merged private attachments")
	}
	if private, found := field.Snapshot(replaced).Private("factory"); !found || private.Kind() != store.ValueString {
		t.Fatal("whole child replacement lost parent attachments")
	}
	var rows field.ArrayField
	rows, err = field.EditChild(field.Array("links", base.Children()), "href", field.AsText, func(href field.TextField) field.TextField {
		return href.MaxLength(2048)
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = field.AppendChild(rows, field.Checkbox("newTab"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err = field.ReplaceChild(rows, "newTab", field.Checkbox("newTab").Default(true))
	if err != nil || len(rows.Children()) != 3 || rows.Name() != "links" {
		t.Fatalf("array child conveniences lost the concrete parent: %v", err)
	}
}

func TestChildConvenienceFailuresReturnTheOriginalFactory(t *testing.T) {
	base := Link("cta")
	identity := func(label field.TextField) field.TextField { return label }
	for name, test := range map[string]struct {
		edit func() (field.GroupField, error)
		code string
	}{
		"missing child": {func() (field.GroupField, error) { return field.EditChild(base, "missing", field.AsText, identity) }, "not_found"},
		"incompatible kind": {func() (field.GroupField, error) {
			return field.EditChild(base, "label", field.AsNumber, func(number field.NumberField) field.NumberField { return number.Min(0) })
		}, "incompatible_kind"},
		"rename": {func() (field.GroupField, error) {
			return field.EditChild(base, "label", field.AsText, func(label field.TextField) field.TextField { return label.Rename("title") })
		}, "rename_requires_explicit_edit"},
		"duplicate append":      {func() (field.GroupField, error) { return field.AppendChild(base, field.Text("label")) }, "duplicate_name"},
		"duplicate replacement": {func() (field.GroupField, error) { return field.ReplaceChild(base, "label", field.Text("href")) }, "duplicate_name"},
		"missing replacement":   {func() (field.GroupField, error) { return field.ReplaceChild(base, "missing", field.Text("label")) }, "not_found"},
		"nil converter":         {func() (field.GroupField, error) { return field.EditChild(base, "label", nil, identity) }, "missing_child_editor"},
		"nil callback":          {func() (field.GroupField, error) { return field.EditChild(base, "label", field.AsText, nil) }, "missing_child_editor"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := test.edit()
			var editError *field.EditError
			if !errors.As(err, &editError) || editError.Code != test.code {
				t.Fatalf("child convenience error = %v; want %s", err, test.code)
			}
			if len(result.Children()) != 2 || result.Children()[0].Name() != "label" || result.Children()[1].Name() != "href" || result.Name() != base.Name() {
				t.Fatal("failed convenience edit changed the original factory")
			}
			if !reflect.DeepEqual(field.Snapshot(result.Children()[0]).BehaviorSummary(), field.Snapshot(base.Children()[0]).BehaviorSummary()) {
				t.Fatal("failed convenience edit lost original behavior")
			}
		})
	}
}
