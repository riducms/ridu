package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
)

func TestPostgresReadinessRequiresExactAppliedManifest(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "readiness", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}}
	backend, manifest := integrationBackend(t, ctx, config)
	if err := backend.Ready(ctx, manifest); err == nil {
		t.Fatal("readiness succeeded without an immutable migration ledger")
	}
	directory := t.TempDir()
	initial, err := postgres.BuildArtifact(ctx, "initial", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("applied manifest readiness: %v", err)
	}
	firstHistory, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{{Name: first.Name, Digest: first.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, manifest, firstHistory); err != nil {
		t.Fatalf("exact initial history readiness: %v", err)
	}
	ahead, err := ridu.Resolve(ridu.Config{Name: "readiness", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, ahead); err == nil {
		t.Fatal("readiness accepted an executable manifest ahead of the database ledger")
	}
	second, err := postgres.BuildArtifact(ctx, "add-summary", &manifest, ahead, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	secondFile, err := migrationartifact.Create(directory, "add-summary", second, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, ahead); err != nil {
		t.Fatalf("new manifest readiness: %v", err)
	}
	completeHistory, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{
		{Name: first.Name, Digest: first.Digest},
		{Name: secondFile.Name, Digest: secondFile.Digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, ahead, completeHistory); err != nil {
		t.Fatalf("exact complete history readiness: %v", err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, ahead, firstHistory); err == nil {
		t.Fatal("migration readiness accepted missing executable history")
	}
	if err := backend.Ready(ctx, manifest); err == nil {
		t.Fatal("readiness accepted an executable manifest behind the database ledger")
	}
}
