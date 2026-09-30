// Package cascade is the cross-adapter contract for cascade delete policies:
// deleting a document also deletes the documents it owns, through the normal
// delete lifecycle and in one transaction.
package cascade

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/store"
)

type Factory func(*testing.T, ridu.Config) (store.Store, *ridu.App)

// Run verifies cascade deletes against one store.
func Run(t *testing.T, factory Factory) {
	t.Run("account-deletion-removes-owned-data", func(t *testing.T) { accountDeletion(t, factory) })
	t.Run("a-restrict-anywhere-keeps-everything", func(t *testing.T) { restrictInTree(t, factory) })
	t.Run("cycles-terminate", func(t *testing.T) { cycles(t, factory) })
}

func never(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }

func accountDeletion(t *testing.T, factory Factory) {
	var profileDeletes []bool
	_, app := factory(t, ridu.Config{Name: "Cascade account deletion", Collections: []ridu.Collection{
		{Slug: "learners", Fields: field.Fields{field.Text("name").Required()}},
		{
			Slug:   "profiles",
			Fields: field.Fields{field.Relationship("learner", "learners").Required().OnDelete(field.ReferenceDeleteCascade)},
			// Learners cannot delete profiles themselves; the cascade still does.
			Access: ridu.CollectionAccess{Delete: never},
			Hooks: ridu.CollectionHooks{BeforeDelete: []ridu.Hook{func(ctx ridu.HookContext) error {
				profileDeletes = append(profileDeletes, ctx.System)
				return nil
			}}},
		},
		{Slug: "progress", Trash: true, Fields: field.Fields{field.Relationship("learner", "learners").Required().OnDelete(field.ReferenceDeleteCascade), field.Text("lesson")}},
		{Slug: "friendships", Fields: field.Fields{
			field.Relationship("requester", "learners").Required().OnDelete(field.ReferenceDeleteCascade),
			field.Relationship("addressee", "learners").Required().OnDelete(field.ReferenceDeleteCascade),
		}},
		{Slug: "leagues", Fields: field.Fields{field.Relationship("owner", "learners").Required().OnDelete(field.ReferenceDeleteCascade)}},
		{Slug: "memberships", Fields: field.Fields{
			field.Relationship("league", "leagues").Required().OnDelete(field.ReferenceDeleteCascade),
			field.Relationship("learner", "learners").Required().OnDelete(field.ReferenceDeleteCascade),
		}},
		{Slug: "notes", Fields: field.Fields{field.Relationship("learner", "learners").OnDelete(field.ReferenceDeleteNullify), field.Text("body")}},
	}})
	local := app.Local()
	ctx := t.Context()
	create := func(collection string, values store.Values) store.Document {
		t.Helper()
		document, err := local.Create(ctx, collection, values, ridu.MutationOptions{})
		if err != nil {
			t.Fatalf("create %s: %v", collection, err)
		}
		return document
	}
	ada := create("learners", store.Values{"name": store.String("Ada")})
	ben := create("learners", store.Values{"name": store.String("Ben")})
	create("profiles", store.Values{"learner": store.String(ada.ID)})
	benProfile := create("profiles", store.Values{"learner": store.String(ben.ID)})
	create("progress", store.Values{"learner": store.String(ada.ID), "lesson": store.String("one")})
	trashed := create("progress", store.Values{"learner": store.String(ada.ID), "lesson": store.String("two")})
	if _, err := local.Delete(ctx, "progress", trashed.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	// One owner referencing the target through two cascade fields.
	create("friendships", store.Values{"requester": store.String(ada.ID), "addressee": store.String(ben.ID)})
	league := create("leagues", store.Values{"owner": store.String(ada.ID)})
	// Ben's membership of Ada's league goes with the league: a chain.
	create("memberships", store.Values{"league": store.String(league.ID), "learner": store.String(ben.ID)})
	note := create("notes", store.Values{"learner": store.String(ada.ID), "body": store.String("kept")})

	if _, err := local.Delete(ctx, "learners", ada.ID, ridu.MutationOptions{}); err != nil {
		t.Fatalf("delete learner with owned data: %v", err)
	}
	count := func(collection string, trash bool) int {
		t.Helper()
		page, err := local.List(ctx, collection, ridu.ListOptions{TrashOnly: trash})
		if err != nil {
			t.Fatalf("list %s: %v", collection, err)
		}
		return page.Total
	}
	for collection, want := range map[string]int{"learners": 1, "profiles": 1, "progress": 0, "friendships": 0, "leagues": 0, "memberships": 0, "notes": 1} {
		if got := count(collection, false); got != want {
			t.Errorf("%s after delete = %d, want %d", collection, got, want)
		}
	}
	if got := count("progress", true); got != 0 {
		t.Errorf("a trashed owner survived the cascade: %d in trash", got)
	}
	if _, err := local.Find(ctx, "profiles", benProfile.ID, ridu.FindOptions{}); err != nil {
		t.Errorf("an unrelated profile was deleted: %v", err)
	}
	kept, err := local.Find(ctx, "notes", note.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if learner := kept.Values["learner"]; learner.Kind() != store.ValueNull && !learner.IsZero() {
		t.Errorf("a nullify reference kept the deleted learner: %#v", kept.Values["learner"])
	}
	if len(profileDeletes) != 1 || !profileDeletes[0] {
		t.Errorf("profile delete hooks saw System = %v, want one system delete", profileDeletes)
	}
}

func restrictInTree(t *testing.T, factory Factory) {
	_, app := factory(t, ridu.Config{Name: "Cascade restrict", Collections: []ridu.Collection{
		{Slug: "learners", Fields: field.Fields{field.Text("name").Required()}},
		{Slug: "profiles", Fields: field.Fields{field.Relationship("learner", "learners").Required().OnDelete(field.ReferenceDeleteCascade)}},
		{Slug: "invoices", Fields: field.Fields{field.Relationship("profile", "profiles").Required().OnDelete(field.ReferenceDeleteRestrict)}},
	}})
	local := app.Local()
	ctx := t.Context()
	learner, err := local.Create(ctx, "learners", store.Values{"name": store.String("Ada")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := local.Create(ctx, "profiles", store.Values{"learner": store.String(learner.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Create(ctx, "invoices", store.Values{"profile": store.String(profile.ID)}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = local.Delete(ctx, "learners", learner.ID, ridu.MutationOptions{})
	var failure *operation.Error
	if !errors.As(err, &failure) || failure.Code != "delete_restricted" {
		t.Fatalf("delete blocked deeper in the cascade = %v, want delete_restricted", err)
	}
	// Server code learns what blocks the delete from the cause; the message
	// that transports send stays free of schema names.
	if failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "invoices.profile") || strings.Contains(failure.Message, "invoices") {
		t.Fatalf("restricted delete = message %q, cause %v", failure.Message, failure.Cause)
	}
	if _, err := local.Find(ctx, "learners", learner.ID, ridu.FindOptions{}); err != nil {
		t.Errorf("the learner was deleted despite the restrict: %v", err)
	}
	if _, err := local.Find(ctx, "profiles", profile.ID, ridu.FindOptions{}); err != nil {
		t.Errorf("the cascade was not rolled back: %v", err)
	}
}

func cycles(t *testing.T, factory Factory) {
	_, app := factory(t, ridu.Config{Name: "Cascade cycles", Collections: []ridu.Collection{
		{Slug: "pairs", Fields: field.Fields{field.Text("name"), field.Relationship("partner", "pairs").OnDelete(field.ReferenceDeleteCascade)}},
	}})
	local := app.Local()
	ctx := t.Context()
	left, err := local.Create(ctx, "pairs", store.Values{"name": store.String("left")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	right, err := local.Create(ctx, "pairs", store.Values{"name": store.String("right"), "partner": store.String(left.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Update(ctx, "pairs", left.ID, store.Values{"partner": store.String(right.ID)}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Delete(ctx, "pairs", left.ID, ridu.MutationOptions{}); err != nil {
		t.Fatalf("delete one side of a cascade cycle: %v", err)
	}
	page, err := local.List(ctx, "pairs", ridu.ListOptions{})
	if err != nil || page.Total != 0 {
		t.Fatalf("pairs after deleting a cycle = %#v, %v", page, err)
	}
}
