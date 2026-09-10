package core_test

import (
	"context"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestJoinMutationUsesTargetPipelineAndRollsBackEveryDelta(t *testing.T) {
	ctx := context.Background()
	var rejectedID string
	application, err := ridu.New(ridu.Config{Name: "Atomic inverse joins", Collections: []ridu.Collection{
		{
			Slug:   "categories",
			Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category").Limit(1)},
		},
		{
			Slug: "posts", Versions: true,
			Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories")},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation == operation.Publish && hook.Original != nil && hook.Original.ID == rejectedID {
					return errors.New("target rejected the relation change")
				}
				return nil
			}}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	addition, err := application.Local().Import(ctx, "posts", store.Values{"title": store.String("Add")}, ridu.ImportOptions{ID: "post-a-add", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := application.Local().Import(ctx, "posts", store.Values{"title": store.String("Remove"), "category": store.String(source.ID)}, ridu.ImportOptions{ID: "post-z-remove", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Local().Import(ctx, "posts", store.Values{"title": store.String("Hidden"), "category": store.String(source.ID)}, ridu.ImportOptions{ID: "post-hidden", Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}

	rejectedID = removal.ID
	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{addition.ID}, []string{removal.ID}, nil); !operationCode(err, "hook_failed") {
		t.Fatalf("join mutation failure = %v, want hook_failed", err)
	}
	assertRelationship(t, application, addition.ID, "")
	assertRelationship(t, application, removal.ID, source.ID)

	rejectedID = ""
	result, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{addition.ID}, []string{removal.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.Removed != 1 || result.Document.ID != source.ID {
		t.Fatalf("join result = %#v", result)
	}
	assertRelationship(t, application, addition.ID, source.ID)
	assertRelationship(t, application, removal.ID, "")
	assertRelationship(t, application, hidden.ID, source.ID)

	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "missing", []string{addition.ID}, nil, nil); !operationCode(err, "not_found") {
		t.Fatalf("missing join field = %v, want not_found", err)
	}
	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{addition.ID}, []string{addition.ID}, nil); !operationCode(err, "bad_request") {
		t.Fatalf("duplicate join target = %v, want bad_request", err)
	}
}

func TestJoinMutationRejectsAReparentedTarget(t *testing.T) {
	ctx := context.Background()
	var targetID, competingSourceID string
	reparented := false
	application, err := ridu.New(ridu.Config{Name: "Concurrent inverse join", Collections: []ridu.Collection{
		{
			Slug:   "categories",
			Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category")},
		},
		{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories")},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation != operation.Update || hook.Original == nil || hook.Original.ID != targetID || reparented {
					return nil
				}
				reparented = true
				_, err := hook.Local.Update(hook.Context, "posts", targetID, store.Values{"category": store.String(competingSourceID)}, hook.Actor)
				return err
			}}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Primary")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	competing, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Competing")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	competingSourceID = competing.ID
	target, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	targetID = target.ID

	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{target.ID}, nil, nil); !operationCode(err, "conflict") {
		t.Fatalf("concurrent join mutation = %v, want conflict", err)
	}
	assertRelationship(t, application, target.ID, "")
}

func TestJoinMutationEnforcesSourceFieldAndTargetUpdateAccess(t *testing.T) {
	ctx := context.Background()
	joinReadable := false
	targetWritable := true
	application, err := ridu.New(ridu.Config{Name: "Authorized inverse joins", Collections: []ridu.Collection{
		{
			Slug: "categories",
			Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category").Access(field.Access{Read: func(fieldContext operation.AccessContext,

			) (bool, error) {
				return joinReadable && fieldContext.Siblings.Get("posts").Kind() == store.ValueList, nil
			}})},
		},
		{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories")},
			Access: ridu.CollectionAccess{Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				if targetWritable {
					return ridu.Allow(), nil
				}
				return ridu.Deny(), nil
			}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Target")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	hiddenSource, err := application.Local().Find(ctx, "categories", source.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, visible := hiddenSource.Values["posts"]; visible {
		t.Fatal("join field is visible while its value-sensitive read rule denies it")
	}
	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{target.ID}, nil, nil); !operationCode(err, "field_access_denied") {
		t.Fatalf("hidden join field = %v, want field_access_denied", err)
	}
	joinReadable = true
	visibleSource, err := application.Local().Find(ctx, "categories", source.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, visible := visibleSource.Values["posts"]; !visible || value.Kind() != store.ValueList {
		t.Fatalf("visible join value = %#v", value)
	}
	targetWritable = false
	if _, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{target.ID}, nil, nil); !operationCode(err, "access_denied") {
		t.Fatalf("target update denial = %v, want access_denied", err)
	}
	assertRelationship(t, application, target.ID, "")
}

func TestJoinMutationGatesOnInitialResolvedFieldVisibility(t *testing.T) {
	ctx := context.Background()
	targetHooks := 0
	application, err := ridu.New(ridu.Config{Name: "Content-sensitive inverse join access", Collections: []ridu.Collection{
		{
			Slug: "categories",
			Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category").Access(field.Access{Read: func(fieldContext operation.AccessContext,

			) (bool, error) {
				items, list := fieldContext.Siblings.Get("posts").CopyList()
				return list && len(items) > 0, nil
			}})},
		},
		{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories")},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation == operation.Update {
					targetHooks++
				}
				return nil
			}}},
		},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	visibleSource, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Visible")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hiddenSource, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("Hidden")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Linked"), "category": store.String(visibleSource.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Candidate")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := application.Local().MutateJoin(ctx, "categories", visibleSource.ID, "posts", nil, []string{linked.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Removed != 1 {
		t.Fatalf("last visible removal = %#v", removed)
	}
	if _, visible := removed.Document.Values["posts"]; visible {
		t.Fatal("empty post-mutation join should be redacted without rolling back the authorized removal")
	}
	assertRelationship(t, application, linked.ID, "")

	targetHooks = 0
	if _, err := application.Local().MutateJoin(ctx, "categories", hiddenSource.ID, "posts", []string{candidate.ID}, nil, nil); !operationCode(err, "field_access_denied") {
		t.Fatalf("initially hidden join mutation = %v, want field_access_denied", err)
	}
	if targetHooks != 0 {
		t.Fatalf("field-denied mutation ran %d target hooks", targetHooks)
	}
	assertRelationship(t, application, candidate.ID, "")
}

func TestJoinMutationCountsFinalMembershipAfterHooks(t *testing.T) {
	ctx := context.Background()
	rewriteBefore := true
	rewriteResponse := false
	redactInverse := false
	application, err := ridu.New(ridu.Config{Name: "Hook-adjusted inverse joins", Collections: []ridu.Collection{
		{Slug: "categories", Fields: field.Fields{field.Text("name").Required(), field.Join("posts", "posts", "category")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title").Required(), field.Relationship("category", "categories").Access(field.Access{Read: func(operation.AccessContext,

		) (bool, error) {
			return !redactInverse, nil
		}})},

			Hooks: ridu.CollectionHooks{
				BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
					if hook.Operation == operation.Update && rewriteBefore {
						hook.Data["category"] = store.Null()
					}
					return nil
				}},
				AfterChange: []ridu.Hook{func(hook ridu.HookContext) error {
					if hook.Operation == operation.Update && rewriteResponse && hook.Document != nil {
						hook.Document.Values["category"] = store.Null()
					}
					return nil
				}},
			}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	source, err := application.Local().Create(ctx, "categories", store.Values{"name": store.String("News")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Target")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{target.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 0 || result.Removed != 0 {
		t.Fatalf("hook-adjusted counts = added %d, removed %d", result.Added, result.Removed)
	}
	assertRelationship(t, application, target.ID, "")

	rewriteBefore = false
	rewriteResponse = true
	redactInverse = true
	result, err = application.Local().MutateJoin(ctx, "categories", source.ID, "posts", []string{target.ID}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 1 || result.Removed != 0 {
		t.Fatalf("persisted counts under response redaction = added %d, removed %d", result.Added, result.Removed)
	}
	items, list := result.Document.Values["posts"].CopyList()
	if !list || len(items) != 1 {
		t.Fatalf("authoritative source join = %#v", result.Document.Values["posts"])
	}
}

func assertRelationship(t *testing.T, application *ridu.App, documentID, expected string) {
	t.Helper()
	document, err := application.Local().Find(context.Background(), "posts", documentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := document.Values["category"].StringValue()
	if actual != expected {
		t.Fatalf("post %q category = %q, want %q", documentID, actual, expected)
	}
}
