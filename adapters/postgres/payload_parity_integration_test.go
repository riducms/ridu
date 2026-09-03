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
			{Slug: "users", Fields: []field.Definition{
				field.Email("email", field.Required()),
				field.Select("roles", field.Required(), field.Multiple(), field.Choices(
					field.Choice{Value: "admin", Label: "Admin"},
					field.Choice{Value: "editor", Label: "Editor"},
				)),
			}},
			{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: []field.Definition{
				field.Text("title", field.Required()),
			}},
			{Slug: "islands", Fields: []field.Definition{
				field.Text("title", field.Required()),
				field.Relationship("lesson", field.To("lessons"), field.Required(), field.FilterOptionRules(
					field.OptionFilterValue("_status", field.FilterEquals, "published"),
				)),
			}},
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
	}, nil)
	if err != nil {
		var operationError *ridu.OperationError
		if errors.As(err, &operationError) {
			t.Fatalf("create multi-select user: %v (cause: %v)", err, operationError.Cause)
		}
		t.Fatal(err)
	}
	storedRoles, valid := user.Values["roles"].Values()
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

	draft, err := application.Local().Create(ctx, "lessons", store.Values{"title": store.String("Draft lesson")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "islands", store.Values{
		"title": store.String("Rejected island"), "lesson": store.String(draft.ID),
	}, nil); !hasRelationshipIssue(err, "lesson") {
		t.Fatalf("PostgreSQL accepted draft target through a published-only rule: %v", err)
	}
	published, err := application.Local().Publish(ctx, "lessons", draft.ID, draft.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	island, err := application.Local().Create(ctx, "islands", store.Values{
		"title": store.String("Published island"), "lesson": store.String(published.ID),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	unpublished, err := application.Local().Unpublish(ctx, "lessons", published.ID, published.Revision, nil)
	if err != nil || unpublished.Status != store.StatusDraft {
		t.Fatalf("PostgreSQL unpublish = %#v, %v", unpublished, err)
	}
	if _, err := application.Local().Update(ctx, "islands", island.ID, store.Values{"title": store.String("Revalidated island")}, nil); !hasRelationshipIssue(err, "lesson") {
		t.Fatalf("PostgreSQL retained a now-draft target through update: %v", err)
	}
}

func TestPostgresAnonymousPopulationDoesNotExposeDraftVersionedTargets(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "Published population", Collections: []ridu.Collection{
		{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: []field.Definition{field.Text("title", field.Required())}},
		{Slug: "links", Fields: []field.Definition{field.Relationship("lesson", field.To("lessons"), field.Required())}},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	staff := store.Document{ID: "staff-1"}
	draftMode := true
	draft, err := application.Local().CreateWithOptions(ctx, "lessons", store.Values{"title": store.String("Draft lesson")}, ridu.MutationOptions{Actor: &staff, Draft: &draftMode})
	if err != nil {
		t.Fatal(err)
	}
	link, err := application.Local().Create(ctx, "links", store.Values{"lesson": store.String(draft.ID)}, &staff)
	if err != nil {
		t.Fatal(err)
	}
	lessonPath, _ := query.NewPath("lesson")

	anonymous, err := application.Local().FindWithOptions(ctx, "links", link.ID, ridu.FindOptions{Populate: []query.Population{{Path: lessonPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := anonymous.Values["lesson"].DocumentValue(); populated {
		t.Fatalf("PostgreSQL anonymous population exposed draft target: %#v", anonymous.Values["lesson"])
	}
	if lessonID, _ := anonymous.Values["lesson"].StringValue(); lessonID != draft.ID {
		t.Fatalf("PostgreSQL anonymous unresolved relationship = %q, want %q", lessonID, draft.ID)
	}
	anonymousDraftMode := true
	anonymousDraft, err := application.Local().FindWithOptions(ctx, "links", link.ID, ridu.FindOptions{
		Draft: &anonymousDraftMode, Populate: []query.Population{{Path: lessonPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, populated := anonymousDraft.Values["lesson"].DocumentValue(); populated {
		t.Fatalf("PostgreSQL anonymous draft override exposed draft target: %#v", anonymousDraft.Values["lesson"])
	}

	staffView, err := application.Local().FindWithOptions(ctx, "links", link.ID, ridu.FindOptions{Actor: &staff, Populate: []query.Population{{Path: lessonPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if populated, ok := staffView.Values["lesson"].DocumentValue(); !ok || populated.ID != draft.ID {
		t.Fatalf("PostgreSQL authorized draft population = %#v", staffView.Values["lesson"])
	}
}
