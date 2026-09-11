package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestHardDeleteRechecksReferencesAfterBeforeDeleteHooks(t *testing.T) {
	ctx := context.Background()
	var cleanupOwnerID, automaticOwnerID string
	cleanupEnabled := false
	automaticOwnerUpdates := 0
	targetAfterDeletes := 0
	application, err := ridu.New(ridu.Config{
		Name: "reference delete lifecycle",
		Collections: []ridu.Collection{
			{
				Slug: "users", Fields: field.Fields{field.Text("name").Required()},
				Hooks: ridu.CollectionHooks{
					BeforeDelete: []ridu.Hook{func(hook ridu.HookContext) error {
						if !cleanupEnabled {
							return nil
						}
						current, findError := hook.Local.Find(hook.Context, "posts", cleanupOwnerID, ridu.FindOptions{Actor: hook.Actor})
						if findError != nil {
							return findError
						}
						_, updateError := hook.Local.PublishChanges(hook.Context, "posts", cleanupOwnerID, store.Values{"protectedOwner": store.Null()}, ridu.MutationOptions{Actor: hook.Actor, ExpectedRevision: current.Revision})
						return updateError
					}},
					AfterDelete: []ridu.Hook{func(ridu.HookContext) error {
						targetAfterDeletes++
						return nil
					}},
				},
			},
			{
				Slug: "posts", Trash: true, Versions: true,
				Fields: field.Fields{field.Text("title").Required(), field.Relationship("protectedOwner", "users").OnDelete(field.ReferenceDeleteRestrict), field.Relationship("owner", "users"), field.Relationships("related", "users")},
				Hooks: ridu.CollectionHooks{AfterChange: []ridu.Hook{func(hook ridu.HookContext) error {
					if hook.Operation == operation.Update && hook.Document != nil && hook.Document.ID == automaticOwnerID {
						automaticOwnerUpdates++
					}
					return nil
				}}},
			},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "users", store.Values{"name": store.String("Ada")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupOwner, err := application.Local().Create(ctx, "posts", store.Values{
		"title":          store.String("Hook cleanup"),
		"protectedOwner": store.String(target.ID),
		"owner":          store.String(target.ID),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupOwnerID = cleanupOwner.ID
	automaticOwner, err := application.Local().Create(ctx, "posts", store.Values{
		"title":   store.String("Automatic reconciliation"),
		"owner":   store.String(target.ID),
		"related": store.List(store.String(target.ID), store.String(target.ID)),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	automaticOwnerID = automaticOwner.ID
	automaticOwner, err = application.Local().Delete(ctx, "posts", automaticOwner.ID, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	metadataUpdatedAt, metadataRevision := automaticOwner.UpdatedAt, automaticOwner.Revision
	versionsBefore, err := application.Local().Versions(ctx, "posts", automaticOwner.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.Local().Delete(ctx, "users", target.ID, ridu.MutationOptions{})
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "delete_restricted" || operationError.Status != 409 {
		t.Fatalf("restricted delete error = %#v, %v", operationError, err)
	}
	if operationError.Message != "document deletion is restricted by current references" || strings.Contains(operationError.Message, cleanupOwner.ID) {
		t.Fatalf("restricted delete disclosed reference details: %q", operationError.Message)
	}
	if targetAfterDeletes != 0 {
		t.Fatalf("after-delete hooks ran for a rejected delete: %d", targetAfterDeletes)
	}
	if _, err := application.Local().Find(ctx, "users", target.ID, ridu.FindOptions{}); err != nil {
		t.Fatalf("restricted target was deleted: %v", err)
	}

	cleanupEnabled = true
	if _, err := application.Local().Delete(ctx, "users", target.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	if targetAfterDeletes != 1 {
		t.Fatalf("successful target after-delete hooks = %d, want 1", targetAfterDeletes)
	}
	cleaned, err := application.Local().Find(ctx, "posts", cleanupOwner.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.Values["protectedOwner"].Kind() != store.ValueNull || cleaned.Values["owner"].Kind() != store.ValueNull {
		t.Fatalf("hook and automatic cleanup values = %#v", cleaned.Values)
	}
	reconciled, err := application.Local().Find(ctx, "posts", automaticOwner.ID, ridu.FindOptions{TrashOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Values["owner"].Kind() != store.ValueNull {
		t.Fatalf("trashed singular reference = %#v", reconciled.Values["owner"])
	}
	if related, _ := reconciled.Values["related"].CopyList(); len(related) != 0 {
		t.Fatalf("trashed duplicate members = %#v", related)
	}
	if !reconciled.UpdatedAt.Equal(metadataUpdatedAt) || reconciled.Revision != metadataRevision {
		t.Fatalf("automatic cleanup changed owner metadata: before %s/%d after %s/%d", metadataUpdatedAt, metadataRevision, reconciled.UpdatedAt, reconciled.Revision)
	}
	if automaticOwnerUpdates != 0 {
		t.Fatalf("automatic cleanup ran owner update hooks: %d", automaticOwnerUpdates)
	}
	versionsAfter, err := application.Local().Versions(ctx, "posts", automaticOwner.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(versionsAfter) != len(versionsBefore) || len(versionsAfter) == 0 {
		t.Fatalf("versions changed during automatic cleanup: before %d after %d", len(versionsBefore), len(versionsAfter))
	}
	for index := range versionsBefore {
		beforeOwner, _ := versionsBefore[index].Snapshot.Values["owner"].StringValue()
		afterOwner, _ := versionsAfter[index].Snapshot.Values["owner"].StringValue()
		if beforeOwner != target.ID || afterOwner != target.ID {
			t.Fatalf("historical version %d was rewritten: before %q after %q", index, beforeOwner, afterOwner)
		}
	}
}

func TestBulkHardDeleteIgnoresOnlyOwnersDeletedInTheSameBatch(t *testing.T) {
	ctx := context.Background()
	application, err := ridu.New(ridu.Config{Name: "batch reference deletes", Collections: []ridu.Collection{{
		Slug: "nodes", Fields: field.Fields{field.Text("name"), field.Relationship("parent", "nodes").OnDelete(field.ReferenceDeleteRestrict)},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	parent, err := application.Local().Create(ctx, "nodes", store.Values{"name": store.String("Parent")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	child, err := application.Local().Create(ctx, "nodes", store.Values{"name": store.String("Child"), "parent": store.String(parent.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().BulkDelete(ctx, "nodes", []string{child.ID, parent.ID}, ridu.BulkOptions{}); err != nil {
		t.Fatalf("owner-before-target batch delete was restricted: %v", err)
	}

	parent, err = application.Local().Create(ctx, "nodes", store.Values{"name": store.String("Retained parent")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	child, err = application.Local().Create(ctx, "nodes", store.Values{"name": store.String("Batch child"), "parent": store.String(parent.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().BulkDelete(ctx, "nodes", []string{parent.ID, child.ID}, ridu.BulkOptions{}); !operationCode(err, "delete_restricted") {
		t.Fatalf("target-before-owner batch treated a future deletion as complete: %v", err)
	}
	for _, document := range []store.Document{parent, child} {
		if _, err := application.Local().Find(ctx, "nodes", document.ID, ridu.FindOptions{}); err != nil {
			t.Errorf("failed batch did not roll back %q: %v", document.ID, err)
		}
	}
}
