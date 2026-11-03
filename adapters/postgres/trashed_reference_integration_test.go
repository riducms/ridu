package postgres_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

func TestPostgresSavedReferencesToTrashedTargetsStayValid(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "PostgreSQL trashed references", Collections: []ridu.Collection{
		{Slug: "projects", Trash: true, Fields: field.Fields{field.Text("name").Required()}},
		{Slug: "issues", Trash: true, Fields: field.Fields{field.Text("title").Required(), field.Relationship("project", "projects")}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	local := application.Local()
	project, err := local.Create(ctx, "projects", store.Values{"name": store.String("Launch")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := local.Create(ctx, "issues", store.Values{"title": store.String("Ship"), "project": store.String(project.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Delete(ctx, "projects", project.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Update(ctx, "issues", issue.ID, store.Values{"title": store.String("Shipped")}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("update with a saved reference to a trashed project: %v", err)
	}
	if _, err := local.Create(ctx, "issues", store.Values{"title": store.String("New"), "project": store.String(project.ID)}, ridu.MutationOptions{}); !hasOperationCode(err, "validation") {
		t.Fatalf("new reference to a trashed project = %v, want validation", err)
	}
	if _, err := local.Delete(ctx, "issues", issue.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.RestoreDeleted(ctx, "issues", issue.ID, ridu.MutationOptions{}); err != nil {
		t.Fatalf("restore while its project is in trash: %v", err)
	}
}
