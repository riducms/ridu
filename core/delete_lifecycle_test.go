package core_test

import (
	"context"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestHardDeletesCleanStateWhileTrashRetainsIt(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "delete lifecycle",
		Collections: []ridu.Collection{
			{Slug: "posts", Fields: field.Fields{field.Text("title")}},
			{Slug: "archived-posts", Trash: true, Fields: field.Fields{field.Text("title")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}

	before := countEvent(backend.Events(), "delete-document-state")
	post, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("hard")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", post.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := countEvent(backend.Events(), "delete-document-state"); got != before+1 {
		t.Fatalf("non-trash cleanup events = %d, want %d", got, before+1)
	}

	archived, err := application.Local().Create(context.Background(), "archived-posts", store.Values{"title": store.String("soft")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "archived-posts", archived.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := countEvent(backend.Events(), "delete-document-state"); got != before+1 {
		t.Fatalf("soft-delete cleanup events = %d, want %d", got, before+1)
	}
	if _, err := application.Local().DeletePermanent(context.Background(), "archived-posts", archived.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := countEvent(backend.Events(), "delete-document-state"); got != before+2 {
		t.Fatalf("permanent-delete cleanup events = %d, want %d", got, before+2)
	}
}

func TestAfterCommitCleanupContinuesAfterFailure(t *testing.T) {
	secondRan := false
	application, err := ridu.New(ridu.Config{
		Name: "after commit cleanup",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
			Hooks: ridu.CollectionHooks{AfterCommit: []ridu.Hook{
				func(ridu.HookContext) error { return errors.New("first cleanup failed") },
				func(ridu.HookContext) error { secondRan = true; return nil },
			}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("committed")}, ridu.MutationOptions{}); !operationCode(err, "hook_failed") {
		t.Fatalf("after-commit error = %v", err)
	}
	if !secondRan {
		t.Fatal("later after-commit cleanup did not run after an earlier failure")
	}
}

func TestBulkDeleteCleanupRollsBackWithLaterHookFailure(t *testing.T) {
	var failingID string
	application, err := ridu.New(ridu.Config{
		Name: "bulk delete cleanup rollback",
		Collections: []ridu.Collection{{
			Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title")},
			Hooks: ridu.CollectionHooks{AfterDelete: []ridu.Hook{func(ctx ridu.HookContext) error {
				if ctx.Document != nil && ctx.Document.ID == failingID {
					return errors.New("reject second delete")
				}
				return nil
			}}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("first")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("second")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	failingID = second.ID
	if _, err := application.Local().BulkDelete(context.Background(), "posts", []string{first.ID, second.ID}, ridu.BulkOptions{}); !operationCode(err, "hook_failed") {
		t.Fatalf("bulk delete error = %v", err)
	}
	for _, document := range []store.Document{first, second} {
		if _, err := application.Local().Find(context.Background(), "posts", document.ID, ridu.FindOptions{}); err != nil {
			t.Errorf("document %q did not roll back: %v", document.ID, err)
		}
		if versions, err := application.Local().Versions(context.Background(), "posts", document.ID, ridu.FindOptions{}); err != nil || len(versions) != 1 {
			t.Errorf("versions for %q after rollback = %#v, %v", document.ID, versions, err)
		}
	}
}

func countEvent(events []string, name string) int {
	count := 0
	for _, event := range events {
		if event == name {
			count++
		}
	}
	return count
}
