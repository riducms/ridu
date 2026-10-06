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

// `ridu dev` synchronizes a schema without recording migrations. Baseline
// records the committed history that database already has, and a later
// baseline catches up after ridu dev runs ahead again.
func TestPostgresBaselineAdoptsADevelopmentSynchronizedDatabase(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	title := atlasTextField("posts-title", "title")
	initial := atlasTestManifest(title)
	withSummary := atlasTestManifest(title, atlasTextField("posts-summary", "summary"))
	directory := t.TempDir()
	synchronize := func(manifest schema.Manifest) {
		t.Helper()
		if err := backend.SyncDevelopmentSchema(ctx, manifest); err != nil {
			t.Fatal(err)
		}
	}
	create := func(name string, before *schema.Manifest, after schema.Manifest, at int64) migrationartifact.File {
		t.Helper()
		artifact, err := BuildArtifact(ctx, name, before, after, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		file, err := migrationartifact.Create(directory, name, artifact, time.Unix(at, 0))
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	requireCurrent := func(manifest schema.Manifest, files ...migrationartifact.File) {
		t.Helper()
		statuses, err := backend.ArtifactStatus(ctx, directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range statuses {
			if !status.Applied {
				t.Fatalf("%s is pending after baseline", status.Name)
			}
		}
		identities := make([]ridumigration.ArtifactIdentity, len(files))
		for index, file := range files {
			identities[index] = ridumigration.ArtifactIdentity{Name: file.Name, Digest: file.Digest}
		}
		history, err := ridumigration.DigestArtifactHistory(identities, files[len(files)-1].Artifact.ToDigest, manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := backend.ReadyWithMigrationHistory(ctx, manifest, history); err != nil {
			t.Fatalf("readiness after baseline: %v", err)
		}
	}

	first := create("initial", nil, initial, 1)
	synchronize(initial)
	adopted, err := backend.AdoptArtifacts(ctx, directory)
	if err != nil || len(adopted) != 1 || adopted[0] != first.Name {
		t.Fatalf("baseline = %v, %v", adopted, err)
	}
	requireCurrent(initial, first)
	// ridu dev keeps synchronizing an adopted database.
	if err := backend.VerifySchema(ctx, initial); err != nil {
		t.Fatalf("schema over adopted history = %v", err)
	}

	synchronize(withSummary)
	second := create("summary", &initial, withSummary, 2)
	if err := backend.ApplyArtifacts(ctx, directory); err == nil {
		t.Fatal("migrate up applied over a schema ridu dev already changed")
	}
	adopted, err = backend.AdoptArtifacts(ctx, directory)
	if err != nil || len(adopted) != 1 || adopted[0] != second.Name {
		t.Fatalf("second baseline = %v, %v", adopted, err)
	}
	requireCurrent(withSummary, first, second)
	if again, err := backend.AdoptArtifacts(ctx, directory); err != nil || len(again) != 0 {
		t.Fatalf("baseline of a current history = %v, %v", again, err)
	}
}

// ridu dev pauses before a rename. Baseline records what the database has and
// leaves the rename, which rewrites content, for ridu migrate up.
func TestPostgresBaselineLeavesARenameForMigrateUp(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	title := atlasTextField("posts-title", "title")
	headline := atlasTextField("posts-headline", "headline")
	before := atlasTestManifest(title)
	after := atlasTestManifest(headline)
	directory := t.TempDir()
	initial, err := BuildArtifact(ctx, "initial", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	rename, err := BuildArtifact(ctx, "rename-title", &before, after, []Rename{{
		Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: after.Snapshot().Collections[0],
		BeforeField: &title, AfterField: &headline,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "rename-title", rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	adopted, err := backend.AdoptArtifacts(ctx, directory)
	if err != nil || len(adopted) != 1 || adopted[0] != first.Name {
		t.Fatalf("baseline before a rename = %v, %v", adopted, err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatalf("migrate up after baseline: %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[1].Applied {
		t.Fatalf("status after the rename = %#v, %v", statuses, err)
	}
}

func TestPostgresBaselineRefusesASchemaNoMigrationDescribes(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	title := atlasTextField("posts-title", "title")
	directory := t.TempDir()
	artifact, err := BuildArtifact(ctx, "initial", nil, atlasTestManifest(title), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, atlasTestManifest(title, atlasTextField("posts-summary", "summary"))); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.AdoptArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "matches no committed migration") {
		t.Fatalf("baseline of a schema ahead of history = %v", err)
	}
	var ledger bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL`).Scan(&ledger); err != nil || ledger {
		t.Fatalf("a refused baseline created a ledger: %t, %v", ledger, err)
	}
}
