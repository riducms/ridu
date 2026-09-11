package field_test

import (
	"testing"

	"github.com/riducms/ridu/field"
)

func TestStringBuilderFamiliesPreserveKindsAndCheckedViews(t *testing.T) {
	for _, node := range []field.TextField{field.Text("body"), field.Code("body"), field.Textarea("body")} {
		view, err := field.AsText(node.Required().MinLength(1))
		if err != nil || view.Kind() != node.Kind() || !view.IsRequired() {
			t.Fatalf("text family lost kind or policy: %v %v", view, err)
		}
	}
	for _, node := range []field.SelectField{field.Select("choice", "a"), field.Radio("choice", "a")} {
		view, err := field.AsSelect(node.Required())
		if err != nil || view.Kind() != node.Kind() || !view.IsRequired() {
			t.Fatalf("choice family lost kind or policy: %v %v", view, err)
		}
	}
	for _, node := range []field.Node{field.Email("body"), field.Date("body"), field.TextList("body"), field.Relationship("body", "posts")} {
		if _, err := field.AsText(node); err == nil {
			t.Fatalf("accepted %s as a text-family builder", node.Kind())
		}
	}
	if _, err := field.AsSelect(field.MultiSelect("choice", "a")); err == nil {
		t.Fatal("accepted multiple-choice input as a singular choice")
	}
	base := field.Group("content", field.Fields{field.Text("body")})
	edited, err := field.EditChild(base, "body", field.AsText, func(f field.TextField) field.TextField {
		return field.Code(f.Name()).Admin(field.Admin{CodeLanguage: "go"})
	})
	if err != nil || edited.Children()[0].Kind() != field.KindCode || base.Children()[0].Kind() != field.KindText {
		t.Fatalf("presentation change failed or mutated the base: %v", err)
	}
	if _, err := field.EditChild(base, "body", field.AsText, func(f field.TextField) field.TextField {
		return field.Textarea("renamed")
	}); err == nil {
		t.Fatal("presentation change bypassed the explicit rename requirement")
	}
}
