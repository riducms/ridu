package core_test

import (
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

// Moving a document to trash must not block writes to the documents that
// refer to it. A reference the write adds must still point at an active one.
func TestSavedReferencesToTrashedTargetsStayValid(t *testing.T) {
	app, err := ridu.New(ridu.Config{
		Name: "trashed references",
		Collections: []ridu.Collection{
			{Slug: "projects", Trash: true, Fields: field.Fields{field.Text("name").Required()}},
			{Slug: "issues", Trash: true, Fields: field.Fields{
				field.Text("title").Required(),
				field.Relationship("project", "projects"),
				field.Relationship("parent", "issues"),
				field.Relationships("related", "issues"),
			}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, local := t.Context(), app.Local()
	create := func(collection string, values store.Values) store.Document {
		t.Helper()
		document, createError := local.Create(ctx, collection, values, ridu.MutationOptions{})
		if createError != nil {
			t.Fatal(createError)
		}
		return document
	}
	trash := func(collection, id string) {
		t.Helper()
		if _, deleteError := local.Delete(ctx, collection, id, ridu.MutationOptions{}); deleteError != nil {
			t.Fatal(deleteError)
		}
	}
	project := create("projects", store.Values{"name": store.String("Launch")})
	other := create("projects", store.Values{"name": store.String("Archive")})
	parent := create("issues", store.Values{"title": store.String("Parent")})
	sibling := create("issues", store.Values{"title": store.String("Sibling")})
	child := create("issues", store.Values{
		"title": store.String("Child"), "project": store.String(project.ID), "parent": store.String(parent.ID),
		"related": store.List(store.String(sibling.ID)),
	})
	trash("projects", project.ID)
	trash("projects", other.ID)
	trash("issues", parent.ID)
	trash("issues", sibling.ID)

	if _, err := local.Update(ctx, "issues", child.ID, store.Values{"title": store.String("Renamed")}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("update with saved references to trashed targets: %v", err)
	}
	fresh := create("issues", store.Values{"title": store.String("Fresh")})
	if _, err := local.Update(ctx, "issues", child.ID, store.Values{
		"related": store.List(store.String(fresh.ID), store.String(sibling.ID)),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("adding a reference beside a saved trashed one: %v", err)
	}
	for _, change := range []store.Values{
		{"project": store.String(other.ID)},
		{"related": store.List(store.String(sibling.ID), store.String(parent.ID))},
	} {
		_, err := local.Update(ctx, "issues", child.ID, change, ridu.MutationOptions{})
		var failure *ridu.OperationError
		if !errors.As(err, &failure) || failure.Code != "validation" || len(failure.Issues) != 1 || failure.Issues[0].Code != "invalid_relationship" {
			t.Fatalf("new reference to a trashed target %v = %#v (%v), want invalid_relationship", change, failure, err)
		}
	}
	if _, err := local.Create(ctx, "issues", store.Values{"title": store.String("Orphan"), "project": store.String(project.ID)}, ridu.MutationOptions{}); err == nil {
		t.Fatal("create accepted a reference to a trashed project")
	}

	trash("issues", child.ID)
	restored, err := local.RestoreDeleted(ctx, "issues", child.ID, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("restore while its targets are in trash: %v", err)
	}
	if id, _ := restored.Values["project"].StringValue(); id != project.ID {
		t.Fatalf("restored project = %q, want %q", id, project.ID)
	}
}
