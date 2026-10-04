package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestAuthDraftReadOutputKeepsIdentityNonNullWhileEditorialFieldsMayClear(t *testing.T) {
	clearIdentity, clearBio := false, false
	application, err := ridu.New(ridu.Config{
		Name: "Auth draft read output", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true, Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{
				field.Email("email").Required().Unique().ReplaceAfterRead(func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
					if clearIdentity {
						return operation.Clear[string](), nil
					}
					return operation.Keep[string](), nil
				}),
				field.Text("bio").Required().ReplaceAfterRead(func(operation.Context, operation.Value[string]) (operation.Change[string], error) {
					if clearBio {
						return operation.Clear[string](), nil
					}
					return operation.Keep[string](), nil
				}),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	created, err := application.Local().Create(t.Context(), "users", store.Values{
		"email": store.String("editor@example.test"), "bio": store.String("Working biography"),
	}, ridu.MutationOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	clearBio = true
	document, err := application.Local().Find(t.Context(), "users", created.ID, ridu.FindOptions{Draft: &draft, System: true})
	if err != nil || document.Values["bio"].Kind() != store.ValueNull {
		t.Fatalf("required editorial draft field should be nullable on read: %#v, %v", document, err)
	}
	clearIdentity = true
	if _, err := application.Local().Find(t.Context(), "users", created.ID, ridu.FindOptions{Draft: &draft, System: true}); !operationCode(err, "invalid_field_output") {
		t.Fatalf("cleared auth identity escaped strict output validation: %v", err)
	}
}
