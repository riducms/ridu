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

// Linking to a document is not reading it: a learner may send a friend
// request to someone whose profile stays private.
func TestReferenceRuleAdmitsLinksWithoutGrantingReads(t *testing.T) {
	ctx := context.Background()
	signedInLearners := false
	ownProfileOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal("id", ctx.Actor.ID)), nil
	}
	application, err := ridu.New(ridu.Config{Name: "Reference access", Admin: ridu.AdminConfig{User: "admins"}, Collections: []ridu.Collection{
		{Slug: "admins", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
		{Slug: "learners", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()},
			Access: ridu.CollectionAccess{Read: ownProfileOnly, Reference: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if signedInLearners && ctx.ActorCollection == "learners" {
					return ridu.Allow(), nil
				}
				return ownProfileOnly(ctx)
			}}},
		{Slug: "friendships", Fields: field.Fields{field.Relationship("addressee", "learners").Required()}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ada, err := application.CreateAuthUser(ctx, "learners", store.Values{"email": store.String("ada@example.test")}, "ada-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ben, err := application.CreateAuthUser(ctx, "learners", store.Values{"email": store.String("ben@example.test")}, "ben-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	asAda := ridu.MutationOptions{Actor: &ada, ActorCollection: "learners"}
	request := store.Values{"addressee": store.String(ben.ID)}

	var failure *ridu.OperationError
	if _, err := application.Local().Create(ctx, "friendships", request, asAda); !errors.As(err, &failure) {
		t.Fatalf("a rule that allows only your own profile admitted a link to another learner: %v", err)
	}
	signedInLearners = true
	friendship, err := application.Local().Create(ctx, "friendships", request, asAda)
	if err != nil {
		t.Fatalf("Reference allowed the link, but it was refused: %v", err)
	}
	if _, err := application.Local().Find(ctx, "learners", ben.ID, ridu.FindOptions{Actor: &ada, ActorCollection: "learners"}); !operationCode(err, "not_found") {
		t.Fatalf("Reference also granted a read of the profile: %v", err)
	}
	addressee, _ := friendship.Values["addressee"].StringValue()
	if addressee != ben.ID {
		t.Fatalf("stored addressee = %q", addressee)
	}
}
