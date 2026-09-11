package formbuilder_test

import (
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/formbuilder"
	"github.com/riducms/ridu/store"
)

func TestGeneratedFormGraphOwnsProtectedEmailConfiguration(t *testing.T) {
	config := formbuilder.Config{Forms: func(collection ridu.Collection) (ridu.Collection, error) {
		if len(collection.Fields) == 0 {
			t.Fatal("form configuration must contain executable fields")
		}
		graph, err := collection.Fields.Edit(func(draft *field.ChildrenDraft) error {
			return draft.EditText("title", func(title field.TextField) field.TextField {
				return title.Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
					if text, _ := value.Get(); text == "Reserved" {
						return []operation.Issue{{Code: "reserved_form_title", Message: "Choose another title"}}, nil
					}
					return nil, nil
				})
			})
		})
		collection.Fields = graph
		return collection, err
	}}
	app := newApplication(t, config)
	manifest := app.Manifest().Snapshot()
	var protected bool
	for _, collection := range manifest.Collections {
		if collection.Slug != "forms" {
			continue
		}
		for _, current := range collection.Fields {
			if current.Name == "emails" {
				protected = current.QueryRestricted
			}
		}
	}
	if !protected {
		t.Fatal("attached email read policy missing from canonical query capabilities")
	}
	_, err := app.Local().Create(t.Context(), "forms", store.Values{"title": store.String("Reserved"), "confirmationMessage": store.String("Thanks")}, ridu.MutationOptions{Actor: formManager()})
	var validation *ridu.OperationError
	if !errors.As(err, &validation) || !hasIssueCode(validation.Issues, "reserved_form_title") {
		t.Fatalf("form override lost graph validator: %v", err)
	}
}
