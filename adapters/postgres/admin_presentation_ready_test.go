package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// An executable built with ridu build is admitted by a database whose ledger
// matches its embedded history even when its admin settings differ from the
// latest migration: hiding or regrouping a collection needs no migration.
// Without the history, readiness still compares the exact manifest.
func TestPostgresReadinessIgnoresAdminPresentationWithHistory(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	committed := atlasTestManifest(atlasTextField("posts-title", "title"))
	directory := t.TempDir()
	artifact, err := BuildArtifact(ctx, "initial", nil, committed, ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{}); err != nil {
		t.Fatal(err)
	}
	history, err := ridumigration.DigestArtifactHistory([]ridumigration.ArtifactIdentity{{Name: file.Name, Digest: file.Digest}}, artifact.ToDigest, committed)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := committed.Snapshot()
	snapshot.Collections[0].Admin = schema.CollectionAdmin{Hidden: true, Group: "Plumbing"}
	snapshot.Collections[0].Fields[0].Admin.Hidden = true
	relabelled := schema.NewManifest(snapshot)
	if _, err := migrationartifact.RequireCurrentHistory(directory, relabelled); err != nil {
		t.Fatalf("ridu build would refuse an admin-only change: %v", err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, relabelled, history); err != nil {
		t.Fatalf("readiness with an admin-only change: %v", err)
	}
	if err := backend.Ready(ctx, relabelled); err == nil || !strings.Contains(err.Error(), "does not match executable digest") {
		t.Fatalf("readiness without the history = %v", err)
	}
	// Runtime schema changes must fail even when the physical schema is equal.
	snapshot.Application.AllowIDOnCreate = true
	if err := backend.ReadyWithMigrationHistory(ctx, schema.NewManifest(snapshot), history); err == nil {
		t.Fatal("readiness admitted an unrecorded runtime schema change")
	}
	// The history also authenticates the recorded head, independently of the
	// artifact checksum and physical schema.
	if _, err := backend.pool.Exec(ctx, `UPDATE ridu_migrations SET to_digest = $1 WHERE name = $2`, strings.Repeat("f", 64), file.Name); err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, relabelled, history); err == nil {
		t.Fatal("readiness admitted a corrupted ledger head")
	}
}
