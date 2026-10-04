package core_test

import (
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestFirstUnversionedGlobalSaveHasNoPublicationStatus(t *testing.T) {
	app, err := ridu.New(ridu.Config{
		Name:        "unversioned global",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Globals:     []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("title").Required()}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().UpdateGlobal(t.Context(), "settings", store.Values{"title": store.String("Initial")}, ridu.MutationOptions{})
	if err != nil || created.Status != "" {
		t.Fatalf("first unversioned global save status = %q, error = %v", created.Status, err)
	}
	current, err := app.Local().Global(t.Context(), "settings", ridu.FindOptions{})
	if err != nil || current.Status != "" || stringValue(current.Values["title"]) != "Initial" {
		t.Fatalf("persisted unversioned global = %#v, %v", current, err)
	}
}
