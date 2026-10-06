package core_test

import (
	"context"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// Drafts are editorial work. An app's own users sign in through another auth
// collection and must not see them through a permissive Read rule.
func TestOnlyEditorsReadDraftsUnlessReadDraftsSaysOtherwise(t *testing.T) {
	ctx := context.Background()
	learnersSeeDrafts := false
	application, err := ridu.New(ridu.Config{Name: "Read drafts", Admin: ridu.AdminConfig{User: "admins"}, Collections: []ridu.Collection{
		{Slug: "admins", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
		{Slug: "learners", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
		{Slug: "articles", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
		{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()},
			Access: ridu.CollectionAccess{ReadDrafts: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if learnersSeeDrafts && ctx.ActorCollection == "learners" {
					return ridu.Allow(), nil
				}
				return ridu.Deny(), nil
			}}},
		{Slug: "bookmarks", Fields: field.Fields{field.Relationship("article", "articles")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := application.Local()
	admin, err := application.CreateAuthUser(ctx, "admins", store.Values{"email": store.String("admin@example.test")}, "admin-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	learner, err := application.CreateAuthUser(ctx, "learners", store.Values{"email": store.String("learner@example.test")}, "learner-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	asAdmin := ridu.FindOptions{Actor: &admin, ActorCollection: "admins"}
	asLearner := ridu.FindOptions{Actor: &learner, ActorCollection: "learners"}

	draft, err := local.Create(ctx, "articles", store.Values{"title": store.String("Unreleased")}, ridu.MutationOptions{Actor: &admin, ActorCollection: "admins"})
	if err != nil || draft.Status != store.StatusDraft {
		t.Fatalf("create draft = %#v, %v", draft, err)
	}
	if _, err := local.Find(ctx, "articles", draft.ID, asAdmin); err != nil {
		t.Fatalf("an editor lost the draft: %v", err)
	}
	if _, err := local.Find(ctx, "articles", draft.ID, asLearner); !operationCode(err, "not_found") {
		t.Fatalf("a learner read a draft by default: %v", err)
	}
	learnerPage, err := local.List(ctx, "articles", ridu.ListOptions{Actor: &learner, ActorCollection: "learners"})
	if err != nil || *learnerPage.Total != 0 {
		t.Fatalf("a learner listed drafts: %#v, %v", learnerPage, err)
	}
	include := true
	explicit := asLearner
	explicit.Draft = &include
	if _, err := local.Find(ctx, "articles", draft.ID, explicit); !operationCode(err, "access_denied") {
		t.Fatalf("a learner's explicit draft read = %v, want access_denied", err)
	}
	if _, err := local.Find(ctx, "articles", draft.ID, ridu.FindOptions{Draft: &include, System: true}); err != nil {
		t.Fatalf("trusted server code lost its explicit draft read: %v", err)
	}
	// Distinct judges the explicit draft read by its caller too.
	titlePath, _ := query.NewPath("title")
	if _, err := local.Distinct(ctx, "articles", ridu.DistinctOptions{Field: titlePath, Draft: &include, Actor: &learner, ActorCollection: "learners"}); !operationCode(err, "access_denied") {
		t.Fatalf("a learner's explicit draft Distinct = %v, want access_denied", err)
	}

	// A learner cannot point at, or populate, a draft they may not read.
	_, err = local.Create(ctx, "bookmarks", store.Values{"article": store.String(draft.ID)}, ridu.MutationOptions{Actor: &learner, ActorCollection: "learners"})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) {
		t.Fatalf("a learner referenced a draft: %v", err)
	}
	bookmark, err := local.Create(ctx, "bookmarks", store.Values{"article": store.String(draft.ID)}, ridu.MutationOptions{Actor: &admin, ActorCollection: "admins"})
	if err != nil {
		t.Fatal(err)
	}
	articlePath, _ := query.NewPath("article")
	populated, err := local.Find(ctx, "bookmarks", bookmark.ID, ridu.FindOptions{Actor: &learner, ActorCollection: "learners", Populate: []query.Population{{Path: articlePath}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := populated.Values["article"].CopyDocument(); exposed {
		t.Fatalf("population exposed a draft to a learner: %#v", populated.Values["article"])
	}

	// ReadDrafts replaces the editor default.
	lesson, err := local.Create(ctx, "lessons", store.Values{"title": store.String("Next week")}, ridu.MutationOptions{Actor: &admin, ActorCollection: "admins"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Find(ctx, "lessons", lesson.ID, asAdmin); !operationCode(err, "not_found") {
		t.Fatalf("an editor read drafts that ReadDrafts denies: %v", err)
	}
	learnersSeeDrafts = true
	if _, err := local.Find(ctx, "lessons", lesson.ID, asLearner); err != nil {
		t.Fatalf("ReadDrafts allowed learners, but the read failed: %v", err)
	}
}
