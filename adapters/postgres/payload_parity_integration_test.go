package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestPostgresMultiSelectAndPublishedOnlyRelationshipParity(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{
		Name: "Payload parity storage",
		Collections: []ridu.Collection{
			{Slug: "users", Fields: field.Fields{field.Email("email").Required(), field.MultiSelect("roles", "admin", "editor").Required()}},
			{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
			{Slug: "islands", Fields: field.Fields{field.Text("title").Required(), field.Relationship("lesson", "lessons").Required().FilterOptionRules(field.OptionFilterValue("_status", field.FilterEquals, "published"))}},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}

	user, err := application.Local().Create(ctx, "users", store.Values{
		"email": store.String("editor@example.test"),
		"roles": store.List(store.String("editor"), store.String("admin")),
	}, ridu.MutationOptions{})
	if err != nil {
		var operationError *ridu.OperationError
		if errors.As(err, &operationError) {
			t.Fatalf("create multi-select user: %v (cause: %v)", err, operationError.Cause)
		}
		t.Fatal(err)
	}
	storedRoles, valid := user.Values["roles"].CopyList()
	firstRole, _ := storedRoles[0].StringValue()
	secondRole, _ := storedRoles[1].StringValue()
	if !valid || firstRole != "editor" || secondRole != "admin" {
		t.Fatalf("PostgreSQL multi-select order = %#v", user.Values["roles"])
	}
	rolesPath, _ := query.NewPath("roles")
	admins, err := application.Local().List(ctx, "users", ridu.ListOptions{
		Where: query.Contains(rolesPath, "admin"), Page: 1, Limit: 10,
	})
	if err != nil || admins.Total != 1 || admins.Documents[0].ID != user.ID {
		t.Fatalf("PostgreSQL multi-select membership = %#v, %v", admins, err)
	}

	draft, err := application.Local().Create(ctx, "lessons", store.Values{"title": store.String("Draft lesson")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "islands", store.Values{
		"title": store.String("Rejected island"), "lesson": store.String(draft.ID),
	}, ridu.MutationOptions{}); !hasRelationshipIssue(err, "lesson") {
		t.Fatalf("PostgreSQL accepted draft target through a published-only rule: %v", err)
	}
	published, err := application.Local().Publish(ctx, "lessons", draft.ID, ridu.MutationOptions{ExpectedRevision: draft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	island, err := application.Local().Create(ctx, "islands", store.Values{
		"title": store.String("Published island"), "lesson": store.String(published.ID),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	unpublished, err := application.Local().Unpublish(ctx, "lessons", published.ID, ridu.MutationOptions{ExpectedRevision: published.Revision})
	if err != nil || unpublished.Status != store.StatusDraft {
		t.Fatalf("PostgreSQL unpublish = %#v, %v", unpublished, err)
	}
	if _, err := application.Local().Update(ctx, "islands", island.ID, store.Values{"title": store.String("Revalidated island")}, ridu.MutationOptions{}); !hasRelationshipIssue(err, "lesson") {
		t.Fatalf("PostgreSQL retained a now-draft target through update: %v", err)
	}
}

func TestPostgresAnonymousPopulationDoesNotExposeDraftVersionedTargets(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "Published population", Collections: []ridu.Collection{
		{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
		{Slug: "links", Fields: field.Fields{field.Relationship("lesson", "lessons").Required()}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	staff := store.Document{ID: "staff-1"}
	draftMode := true
	draft, err := application.Local().Create(ctx, "lessons", store.Values{"title": store.String("Draft lesson")}, ridu.MutationOptions{Actor: &staff, Draft: &draftMode})
	if err != nil {
		t.Fatal(err)
	}
	link, err := application.Local().Create(ctx, "links", store.Values{"lesson": store.String(draft.ID)}, ridu.MutationOptions{Actor: &staff})
	if err != nil {
		t.Fatal(err)
	}
	lessonPath, _ := query.NewPath("lesson")

	anonymous, err := application.Local().Find(ctx, "links", link.ID, ridu.FindOptions{Populate: []query.Population{{Path: lessonPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := anonymous.Values["lesson"].CopyDocument(); populated {
		t.Fatalf("PostgreSQL anonymous population exposed draft target: %#v", anonymous.Values["lesson"])
	}
	if lessonID, _ := anonymous.Values["lesson"].StringValue(); lessonID != draft.ID {
		t.Fatalf("PostgreSQL anonymous unresolved relationship = %q, want %q", lessonID, draft.ID)
	}
	anonymousDraftMode := true
	anonymousDraft, err := application.Local().Find(ctx, "links", link.ID, ridu.FindOptions{
		Draft: &anonymousDraftMode, Populate: []query.Population{{Path: lessonPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := anonymousDraft.Values["lesson"].CopyDocument(); populated {
		t.Fatalf("PostgreSQL anonymous draft override exposed draft target: %#v", anonymousDraft.Values["lesson"])
	}

	staffView, err := application.Local().Find(ctx, "links", link.ID, ridu.FindOptions{Actor: &staff, Populate: []query.Population{{Path: lessonPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if populated, ok := staffView.Values["lesson"].CopyDocument(); !ok || populated.ID != draft.ID {
		t.Fatalf("PostgreSQL authorized draft population = %#v", staffView.Values["lesson"])
	}
}
