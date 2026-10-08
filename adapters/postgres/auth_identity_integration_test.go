package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestPostgresAuthIdentityCanonicalizationAtDirectStoreBoundary(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest := postgresAuthIdentityManifest()
	directory := t.TempDir()
	initial, err := BuildArtifact(ctx, "canonical-auth", nil, manifest, ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "canonical-auth", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	createValues := store.Values{"email": store.String(" \tUser@Example.COM\u00a0")}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	created, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "canonical-user", Values: createValues})
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
	updated, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{
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

	assertPostgresAuthIdentityPair(t, backend, collection, "kelvin", "\u212a@example.test", "ascii-k", "K@example.test", true)
	assertPostgresAuthIdentityPair(t, backend, collection, "long-s", "\u017f@example.test", "ascii-s", "S@example.test", false)
	assertPostgresAuthIdentityPair(t, backend, collection, "sigma", "\u03a3@example.test", "final-sigma", "\u03c2@example.test", false)
	assertPostgresAuthIdentityPair(t, backend, collection, "dotted-i", "\u0130@example.test", "ascii-i", "I@example.test", true)
	assertPostgresAuthIdentityPair(t, backend, collection, "dotless-i", "\u0131@dotless.test", "ascii-i-distinct", "i@dotless.test", false)
}

func assertPostgresAuthIdentityPair(t *testing.T, backend *Store, collection schema.Collection, firstID, firstIdentity, secondID, secondIdentity string, conflict bool) {
	t.Helper()
	ctx := context.Background()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: firstID, Values: store.Values{"email": store.String(firstIdentity)}}); err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatal(err)
	}
	_, err = transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: secondID, Values: store.Values{"email": store.String(secondIdentity)}})
	if conflict {
		if !errors.Is(err, store.ErrConflict) {
			_ = transaction.Rollback(ctx)
			t.Fatalf("second identity error = %v, want conflict", err)
		}
		if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		return
	}
	if err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatalf("distinct second identity: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func postgresAuthIdentityManifest() schema.Manifest {
	path, _ := query.NewPath("email")
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "auth identity"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: "users", Slug: "users", Capabilities: schema.Capabilities{Auth: true, Trash: true},
			Auth: &schema.AuthSettings{IdentityField: "email"},
			Fields: []schema.Field{{
				ID: "users-email", Name: "email", Path: path, Type: schema.FieldTypeEmail,
				Category: schema.FieldCategoryScalar, Required: true, Unique: true,
			}},
		}},
	})
}
