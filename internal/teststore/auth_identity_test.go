package teststore

import (
	"context"
	"errors"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAuthIdentityCanonicalizationAtDirectStoreBoundary(t *testing.T) {
	ctx := context.Background()
	backend := New()
	collection := testAuthIdentityCollection()
	createValues := store.Values{"email": store.String(" \tUser@Example.COM\u00a0")}

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "user", Values: createValues})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := created.Values["email"].StringValue(); got != "user@example.com" {
		t.Fatalf("created identity = %q", got)
	}
	if got, _ := createValues["email"].StringValue(); got != " \tUser@Example.COM\u00a0" {
		t.Fatalf("Create mutated caller value to %q", got)
	}

	updateValues := store.Values{"email": store.String(" Next@Example.COM ")}
	updated, err := transaction.Update(ctx, store.UpdateRequest{
		Request: store.Request{Collection: collection, ID: created.ID}, Values: updateValues,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := updated.Values["email"].StringValue(); got != "next@example.com" {
		t.Fatalf("updated identity = %q", got)
	}
	if got, _ := updateValues["email"].StringValue(); got != " Next@Example.COM " {
		t.Fatalf("Update mutated caller value to %q", got)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	assertTestAuthIdentityPair(t, backend, collection, "kelvin", "\u212a@example.test", "ascii-k", "K@example.test", true)
	assertTestAuthIdentityPair(t, backend, collection, "long-s", "\u017f@example.test", "ascii-s", "S@example.test", false)
	assertTestAuthIdentityPair(t, backend, collection, "sigma", "\u03a3@example.test", "final-sigma", "\u03c2@example.test", false)
	assertTestAuthIdentityPair(t, backend, collection, "dotted-i", "\u0130@example.test", "ascii-i", "I@example.test", true)
	assertTestAuthIdentityPair(t, backend, collection, "dotless-i", "\u0131@dotless.test", "ascii-i-distinct", "i@dotless.test", false)
}

func assertTestAuthIdentityPair(t *testing.T, backend *Store, collection schema.Collection, firstID, firstIdentity, secondID, secondIdentity string, conflict bool) {
	t.Helper()
	ctx := context.Background()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: firstID, Values: store.Values{"email": store.String(firstIdentity)}}); err != nil {
		t.Fatal(err)
	}
	_, err = transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: secondID, Values: store.Values{"email": store.String(secondIdentity)}})
	if conflict {
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second identity error = %v, want conflict", err)
		}
	} else if err != nil {
		t.Fatalf("distinct second identity: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func testAuthIdentityCollection() schema.Collection {
	return schema.Collection{
		ID: "users", Slug: "users", Capabilities: schema.Capabilities{Auth: true},
		Auth: &schema.AuthSettings{IdentityField: "email"},
		Fields: []schema.Field{{
			ID: "users-email", Name: "email", Type: schema.FieldTypeEmail,
			Category: schema.FieldCategoryScalar, Required: true, Unique: true,
		}},
	}
}
