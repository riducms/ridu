package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

func TestDocumentLocksAcquireRefreshTakeOverAndRelease(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Locks", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Text("email").Required().Unique()}},
			{Slug: "posts", LockDocuments: true, DocumentLockConfig: ridu.DocumentLockConfig{Duration: time.Minute}, Fields: field.Fields{field.Text("title")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Locked")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstDocument, err := application.CreateAuthUser(ctx, "users", store.Values{"email": store.String("one@example.test")}, "first-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secondDocument, err := application.CreateAuthUser(ctx, "users", store.Values{"email": store.String("two@example.test")}, "second-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := &ridu.AuthIdentity{Collection: "users", Actor: firstDocument}
	second := &ridu.AuthIdentity{Collection: "users", Actor: secondDocument}

	acquired, err := application.AcquireDocumentLock(ctx, "posts", document.ID, false, first)
	if err != nil || !acquired.Acquired || !acquired.Owned || acquired.Lock == nil || acquired.Lock.OwnerLabel != "one@example.test" {
		t.Fatalf("first acquire = %#v, %v", acquired, err)
	}
	blocked, err := application.AcquireDocumentLock(ctx, "posts", document.ID, false, second)
	if err != nil || blocked.Acquired || blocked.Owned || !blocked.CanTakeOver || blocked.Lock == nil || blocked.Lock.OwnerID != first.Actor.ID {
		t.Fatalf("blocked acquire = %#v, %v", blocked, err)
	}
	taken, err := application.AcquireDocumentLock(ctx, "posts", document.ID, true, second)
	if err != nil || !taken.Acquired || !taken.Owned || taken.Lock == nil || taken.Lock.OwnerID != second.Actor.ID {
		t.Fatalf("takeover = %#v, %v", taken, err)
	}
	if err := application.ReleaseDocumentLock(ctx, "posts", document.ID, first); err != nil {
		t.Fatalf("old owner release: %v", err)
	}
	current, err := application.DocumentLock(ctx, "posts", document.ID, second)
	if err != nil || !current.Owned || current.Lock == nil || current.Lock.OwnerID != second.Actor.ID {
		t.Fatalf("lock after stale release = %#v, %v", current, err)
	}
	if _, err := application.Local().Delete(ctx, "posts", document.ID, ridu.MutationOptions{Actor: &second.Actor}); err != nil {
		t.Fatal(err)
	}
	if err := application.ReleaseDocumentLock(ctx, "posts", document.ID, second); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.FindDocumentLock(ctx, schema.StableID("posts"), document.ID, time.Now().UTC()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("lock after deleted-document release = %v", err)
	}
}

func TestDocumentLockConfigurationValidatesDuration(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name:        "Locks",
		Collections: []ridu.Collection{{Slug: "posts", LockDocuments: true, DocumentLockConfig: ridu.DocumentLockConfig{Duration: time.Second}, Fields: field.Fields{field.Text("title")}}},
	})
	if err == nil {
		t.Fatal("Resolve succeeded with a one-second document lock")
	}
}

func TestDocumentLockTakeoverUsesUnlockAccess(t *testing.T) {
	backend := teststore.New()
	var lockAdminID string
	application, err := ridu.New(ridu.Config{
		Name: "Lock access", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}}, Fields: field.Fields{field.Text("email").Required().Unique()}},
			{
				Slug: "posts", LockDocuments: true, Fields: field.Fields{field.Text("title")},
				Access: ridu.CollectionAccess{
					Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
					Unlock: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
						if ctx.Actor != nil && ctx.Actor.ID == lockAdminID {
							return ridu.Allow(), nil
						}
						return ridu.Deny(), nil
					},
				},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Restricted")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ownerDocument, err := application.CreateAuthUser(context.Background(), "users", store.Values{"email": store.String("admin@example.test")}, "admin-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lockAdminID = ownerDocument.ID
	editorDocument, err := application.CreateAuthUser(context.Background(), "users", store.Values{"email": store.String("editor@example.test")}, "editor-password-value", ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner := &ridu.AuthIdentity{Collection: "users", Actor: ownerDocument}
	editor := &ridu.AuthIdentity{Collection: "users", Actor: editorDocument}
	if _, err := application.AcquireDocumentLock(context.Background(), "posts", document.ID, false, owner); err != nil {
		t.Fatal(err)
	}
	blocked, err := application.AcquireDocumentLock(context.Background(), "posts", document.ID, false, editor)
	if err != nil || blocked.CanTakeOver {
		t.Fatalf("blocked lock = %#v, %v", blocked, err)
	}
	if _, err := application.AcquireDocumentLock(context.Background(), "posts", document.ID, true, editor); err == nil {
		t.Fatal("takeover succeeded without unlock access")
	}
}

func TestDocumentLocksUseExactAuthCollectionForSameIDActors(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Exact lock identity", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true, Admin: ridu.CollectionAdmin{UseAsTitle: "displayName"},
				Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("displayName").Required()},
			},
			{
				Slug: "staff", Auth: true, Admin: ridu.CollectionAdmin{UseAsTitle: "handle"},
				Fields: field.Fields{field.Text("email").Required().Unique(), field.Text("handle").Required()},
			},
			{Slug: "posts", LockDocuments: true, Fields: field.Fields{field.Text("title")}},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Locked")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Import(ctx, "users", store.Values{
		"email": store.String("user@example.test"), "displayName": store.String("User label"),
	}, ridu.ImportOptions{ID: "shared-actor", Status: store.StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	staff, err := application.Local().Import(ctx, "staff", store.Values{
		"email": store.String("staff@example.test"), "handle": store.String("Staff label"),
	}, ridu.ImportOptions{ID: user.ID, Status: store.StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	userIdentity := &ridu.AuthIdentity{Collection: "users", Actor: user}
	staffIdentity := &ridu.AuthIdentity{Collection: "staff", Actor: staff}

	acquired, err := application.AcquireDocumentLock(ctx, "posts", post.ID, false, userIdentity)
	if err != nil || !acquired.Acquired || !acquired.Owned || acquired.Lock == nil || acquired.Lock.OwnerCollectionID != "users" || acquired.Lock.OwnerLabel != "User label" {
		t.Fatalf("users acquire = %#v, %v", acquired, err)
	}
	blocked, err := application.DocumentLock(ctx, "posts", post.ID, staffIdentity)
	if err != nil || blocked.Owned || blocked.Lock == nil || blocked.Lock.OwnerCollectionID != "users" {
		t.Fatalf("staff view of users lock = %#v, %v", blocked, err)
	}
	if err := application.ReleaseDocumentLock(ctx, "posts", post.ID, staffIdentity); err != nil {
		t.Fatal(err)
	}
	current, err := application.DocumentLock(ctx, "posts", post.ID, userIdentity)
	if err != nil || !current.Owned || current.Lock == nil {
		t.Fatalf("users lock after staff release = %#v, %v", current, err)
	}
	taken, err := application.AcquireDocumentLock(ctx, "posts", post.ID, true, staffIdentity)
	if err != nil || !taken.Acquired || !taken.Owned || taken.Lock == nil || taken.Lock.OwnerCollectionID != "staff" || taken.Lock.OwnerLabel != "Staff label" {
		t.Fatalf("staff takeover = %#v, %v", taken, err)
	}
	if err := application.ReleaseDocumentLock(ctx, "posts", post.ID, userIdentity); err != nil {
		t.Fatal(err)
	}
	current, err = application.DocumentLock(ctx, "posts", post.ID, staffIdentity)
	if err != nil || !current.Owned || current.Lock == nil || current.Lock.OwnerCollectionID != "staff" {
		t.Fatalf("staff lock after users release = %#v, %v", current, err)
	}
	if err := application.ReleaseDocumentLock(ctx, "posts", post.ID, staffIdentity); err != nil {
		t.Fatal(err)
	}
}
