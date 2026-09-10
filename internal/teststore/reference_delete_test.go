package teststore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestApplyReferenceDeletePlansRestrictBeforeReconcilingCurrentTrashValues(t *testing.T) {
	ctx := context.Background()
	backend := New()
	clock := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	backend.now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	users := schema.Collection{ID: "users"}
	posts := schema.Collection{
		ID: "posts", Capabilities: schema.Capabilities{Trash: true, Versions: true},
		Versions: &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
		Fields: []schema.Field{
			{ID: "posts-asset", Name: "asset", Type: schema.FieldTypeUpload, Upload: &schema.UploadField{CollectionID: "users", OnDelete: schema.ReferenceDeleteNullify}},
			{ID: "posts-guard", Name: "guard", Type: schema.FieldTypeRelationship, Required: true, Relationship: &schema.RelationshipField{CollectionID: "users", OnDelete: schema.ReferenceDeleteRestrict}},
			{ID: "posts-related", Name: "related", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionID: "users", HasMany: true, OnDelete: schema.ReferenceDeleteNullify}},
		},
	}
	collections := map[schema.StableID]schema.Collection{users.ID: users, posts.ID: posts}

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"user-1", "user-2"} {
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: users, ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := transaction.Create(ctx, store.CreateRequest{
		Collection: posts, ID: "post-1", Status: store.StatusDraft,
		Values: store.Values{
			"asset":   store.String("user-1"),
			"guard":   store.String("user-1"),
			"related": store.List(store.String("user-1"), store.String("user-2"), store.String("user-1")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	target := store.DocumentReference{CollectionID: users.ID, DocumentID: "user-1"}
	err = transaction.ApplyReferenceDelete(ctx, store.ReferenceDeleteRequest{Target: target, Collections: collections})
	if !errors.Is(err, store.ErrDeleteRestricted) {
		t.Fatalf("restricted delete error = %v", err)
	}
	var restricted *store.DeleteRestrictedError
	if !errors.As(err, &restricted) || len(restricted.Constraints) != 1 || restricted.Constraints[0].FieldID != "posts-guard" {
		t.Fatalf("constraints = %#v", restricted)
	}
	if strings.Contains(err.Error(), owner.ID) {
		t.Fatalf("restricted error disclosed owner document ID: %v", err)
	}
	unchanged, err := transaction.Find(ctx, store.Request{Collection: posts, ID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if asset, _ := unchanged.Values["asset"].StringValue(); asset != "user-1" {
		t.Fatalf("nullify ran before restrict planning: %#v", unchanged.Values)
	}

	owner, err = transaction.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: posts, ID: owner.ID},
		Values:  store.Values{"guard": store.String("user-2")},
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err = transaction.Trash(ctx, store.Request{Collection: posts, ID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	metadataTime, metadataRevision := owner.UpdatedAt, owner.Revision
	versions := transaction.(store.VersionTransaction)
	version, err := versions.SaveVersion(ctx, posts, owner, 10)
	if err != nil {
		t.Fatal(err)
	}

	if err := transaction.ApplyReferenceDelete(ctx, store.ReferenceDeleteRequest{Target: target, Collections: collections}); err != nil {
		t.Fatal(err)
	}
	reconciled, err := transaction.Find(ctx, store.Request{Collection: posts, ID: owner.ID, Deletion: store.DeletionTrash})
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Values["asset"].Kind() != store.ValueNull {
		t.Fatalf("singular upload = %#v", reconciled.Values["asset"])
	}
	related, _ := reconciled.Values["related"].CopyList()
	if len(related) != 1 {
		t.Fatalf("related = %#v", related)
	}
	if id, _ := related[0].StringValue(); id != "user-2" {
		t.Fatalf("remaining relationship = %#v", related[0])
	}
	if !reconciled.UpdatedAt.Equal(metadataTime) || reconciled.Revision != metadataRevision {
		t.Fatalf("owner metadata changed: before %s/%d after %s/%d", metadataTime, metadataRevision, reconciled.UpdatedAt, reconciled.Revision)
	}
	storedVersion, err := versions.FindVersion(ctx, posts, owner.ID, version.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if asset, _ := storedVersion.Snapshot.Values["asset"].StringValue(); asset != "user-1" {
		t.Fatalf("historical snapshot was rewritten: %#v", storedVersion.Snapshot.Values)
	}

	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestApplyReferenceDeleteHonorsIgnoredBatchOwners(t *testing.T) {
	ctx := context.Background()
	backend := New()
	users := schema.Collection{ID: "users", Fields: []schema.Field{{
		ID: "users-manager", Name: "manager", Type: schema.FieldTypeRelationship, Required: true,
		Relationship: &schema.RelationshipField{CollectionID: "users", OnDelete: schema.ReferenceDeleteRestrict},
	}}}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document, err := transaction.Create(ctx, store.CreateRequest{Collection: users, ID: "self", Values: store.Values{"manager": store.String("self")}})
	if err != nil {
		t.Fatal(err)
	}
	reference := store.DocumentReference{CollectionID: users.ID, DocumentID: document.ID}
	if err := transaction.ApplyReferenceDelete(ctx, store.ReferenceDeleteRequest{
		Target: reference, Collections: map[schema.StableID]schema.Collection{users.ID: users}, IgnoreOwners: []store.DocumentReference{reference},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyReferenceDeleteDoesNotIgnoreAnOwnerThatStillExists(t *testing.T) {
	ctx := context.Background()
	backend := New()
	nodes := schema.Collection{ID: "nodes", Fields: []schema.Field{{
		ID: "nodes-parent", Name: "parent", Type: schema.FieldTypeRelationship,
		Relationship: &schema.RelationshipField{CollectionID: "nodes", OnDelete: schema.ReferenceDeleteRestrict},
	}}}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := transaction.Create(ctx, store.CreateRequest{Collection: nodes, ID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := transaction.Create(ctx, store.CreateRequest{Collection: nodes, ID: "child", Values: store.Values{"parent": store.String(parent.ID)}})
	if err != nil {
		t.Fatal(err)
	}
	request := store.ReferenceDeleteRequest{
		Target:       store.DocumentReference{CollectionID: nodes.ID, DocumentID: parent.ID},
		Collections:  map[schema.StableID]schema.Collection{nodes.ID: nodes},
		IgnoreOwners: []store.DocumentReference{{CollectionID: nodes.ID, DocumentID: child.ID}},
	}
	if err := transaction.ApplyReferenceDelete(ctx, request); !errors.Is(err, store.ErrDeleteRestricted) {
		t.Fatalf("current future batch owner bypassed restriction: %v", err)
	}
}
