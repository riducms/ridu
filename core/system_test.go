package core_test

import (
	"context"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// Server code that maintains data on a user's behalf, such as stats written by
// a hook, passes System instead of teaching every access rule about a context
// flag. System skips access rules only: validation and hooks still run, and a
// nested call is trusted only when it asks again.
func TestSystemCallsSkipAccessRulesButNotTheLifecycle(t *testing.T) {
	ctx := context.Background()
	never := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }
	ownOnly := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal("learner", ctx.Actor.ID)), nil
	}
	hidden := func(operation.Context) (bool, error) { return false, nil }
	var hookSawSystem []bool
	var nestedError error
	application, err := ridu.New(ridu.Config{Name: "System calls", Admin: ridu.AdminConfig{User: "admins"}, Collections: []ridu.Collection{
		{Slug: "admins", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
		{Slug: "learners", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
		{
			Slug: "profiles",
			Fields: field.Fields{
				field.Text("learner").Required(),
				field.Number("xp").Access(field.Access{Create: hidden, Update: hidden}),
				field.Text("note").Access(field.Access{Read: hidden}),
			},
			Access: ridu.CollectionAccess{Read: ownOnly, Create: never, Update: never, Delete: never},
			Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
				hookSawSystem = append(hookSawSystem, ctx.System)
				if ctx.System && ctx.Operation == operation.Create {
					// A separate context keeps this expected denial from
					// failing the enclosing transaction.
					_, nestedError = ctx.Local.List(context.Background(), "audit", ridu.ListOptions{})
				}
				return nil
			}}},
		},
		{Slug: "audit", Fields: field.Fields{field.Text("entry")}, Access: ridu.CollectionAccess{Read: never}},
		{Slug: "lessons", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}},
		{Slug: "bookmarks", Fields: field.Fields{field.Relationship("lesson", "lessons")}, Access: ridu.CollectionAccess{Create: never}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := application.Local()
	learner, err := application.CreateAuthUser(ctx, "learners", store.Values{"email": store.String("learner@example.test")}, "learner-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	asLearner := ridu.MutationOptions{Actor: &learner, ActorCollection: "learners"}
	profileValues := store.Values{"learner": store.String(learner.ID), "xp": store.Number(10), "note": store.String("staff only")}

	if _, err := local.Create(ctx, "profiles", profileValues, asLearner); !operationCode(err, "access_denied") {
		t.Fatalf("a learner created a server-maintained profile: %v", err)
	}
	// The learner is still the one the work is for; hooks see both facts.
	system := asLearner
	system.System = true
	profile, err := local.Create(ctx, "profiles", profileValues, system)
	if err != nil {
		t.Fatalf("system create = %v", err)
	}
	if !operationCode(nestedError, "access_denied") {
		t.Fatalf("a hook's nested call inherited System: %v", nestedError)
	}
	if _, err := local.Update(ctx, "profiles", profile.ID, store.Values{"xp": store.Number(20)}, ridu.MutationOptions{System: true}); err != nil {
		t.Fatalf("system update of a rule-protected field = %v", err)
	}
	if len(hookSawSystem) != 2 || !hookSawSystem[0] || !hookSawSystem[1] {
		t.Fatalf("hooks saw System = %v, want true for both writes", hookSawSystem)
	}
	if _, err := local.Update(ctx, "profiles", profile.ID, store.Values{"learner": store.Null()}, ridu.MutationOptions{System: true}); !operationCode(err, "validation") {
		t.Fatalf("system update skipped validation: %v", err)
	}

	// Field read rules redact for users but not for System, which may also
	// query the protected field.
	own, err := local.Find(ctx, "profiles", profile.ID, ridu.FindOptions{Actor: &learner, ActorCollection: "learners"})
	if err != nil {
		t.Fatal(err)
	}
	if _, visible := own.Values["note"]; visible {
		t.Fatalf("a learner read a protected field: %#v", own.Values)
	}
	full, err := local.Find(ctx, "profiles", profile.ID, ridu.FindOptions{System: true})
	if note, _ := full.Values["note"].StringValue(); err != nil || note != "staff only" {
		t.Fatalf("system read = %#v, %v", full.Values, err)
	}
	if _, err := local.List(ctx, "profiles", ridu.ListOptions{Where: query.Equal("note", "staff only"), Actor: &learner, ActorCollection: "learners"}); !operationCode(err, "field_access_denied") {
		t.Fatalf("a learner queried a protected field: %v", err)
	}
	page, err := local.List(ctx, "profiles", ridu.ListOptions{Where: query.Equal("note", "staff only"), System: true})
	if err != nil || page.Total != 1 {
		t.Fatalf("system query of a protected field = %#v, %v", page, err)
	}

	// System sees and may link to drafts, and bypasses Create on the root.
	draft, err := local.Create(ctx, "lessons", store.Values{"title": store.String("Next week")}, ridu.MutationOptions{System: true})
	if err != nil || draft.Status != store.StatusDraft {
		t.Fatalf("system draft create = %#v, %v", draft, err)
	}
	lessons, err := local.List(ctx, "lessons", ridu.ListOptions{System: true})
	if err != nil || lessons.Total != 1 {
		t.Fatalf("system lesson list = %#v, %v", lessons, err)
	}
	if _, err := local.Create(ctx, "bookmarks", store.Values{"lesson": store.String(draft.ID)}, ridu.MutationOptions{System: true}); err != nil {
		t.Fatalf("system reference to a draft = %v", err)
	}
	if _, err := local.Create(ctx, "bookmarks", store.Values{"lesson": store.String("missing-lesson")}, ridu.MutationOptions{System: true}); !relationshipIssue(err, "lesson") {
		t.Fatalf("system reference skipped the existence check: %v", err)
	}
}
