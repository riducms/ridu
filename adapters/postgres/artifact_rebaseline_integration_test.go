package postgres

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// A development database baselined onto the first initial, then advanced by
// ridu dev, adopts a re-squashed initial without running it. ridu dev added a
// field and changed the kind of an empty nested field after scanning it; the
// replacement accepts both instead of re-planning them as a migration.
func TestPostgresRebaselineAdoptsSquashedHistoryAfterDevelopmentSync(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	resolve := func(fields field.Fields) schema.Manifest {
		t.Helper()
		manifest, err := ridu.Resolve(ridu.Config{Name: "Rebaseline", Collections: []ridu.Collection{{Slug: "posts", Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before := resolve(field.Fields{field.Text("title"), field.Group("meta", field.Fields{field.Text("rating")})})
	after := resolve(field.Fields{field.Text("title"), field.Text("summary"), field.Group("meta", field.Fields{field.Number("rating")})})
	old, same, next := t.TempDir(), t.TempDir(), t.TempDir()
	create := func(directory string, manifest schema.Manifest, at int64) migrationartifact.File {
		t.Helper()
		artifact, err := BuildArtifact(ctx, "initial", nil, manifest, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		file, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(at, 0))
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	create(old, before, 1)
	create(same, before, 2)
	squashed := create(next, after, 3)
	if err := backend.ApplyArtifacts(ctx, old); err != nil {
		t.Fatal(err)
	}
	oldHistoryRecorded := func() {
		t.Helper()
		if status, err := backend.ArtifactStatus(ctx, old); err != nil || len(status) != 1 || !status[0].Applied {
			t.Fatalf("refusal changed old history: %#v, %v", status, err)
		}
	}
	if _, err := backend.ReplaceBaseline(ctx, same, migration.BaselineReplacementOptions{}); err == nil || !strings.Contains(err.Error(), "--previous-history") {
		t.Fatalf("missing evidence = %v", err)
	}
	if _, err := backend.ReplaceBaseline(ctx, same, migration.BaselineReplacementOptions{PreviousDirectory: old}); !errors.Is(err, migration.ErrReplacementNeedsConfirmation) {
		t.Fatalf("replacement at the recorded head = %v", err)
	}
	oldHistoryRecorded()
	if _, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "replacement schema differs") {
		t.Fatalf("schema drift = %v", err)
	}
	oldHistoryRecorded()

	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	collection := after.Snapshot().Collections[0]
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "retained", Values: store.Values{"title": store.String("Kept"), "summary": store.String("Synchronized by ridu dev")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	recorded, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old})
	if err != nil || !reflect.DeepEqual(recorded, []string{squashed.Name}) {
		t.Fatalf("replace = %v, %v", recorded, err)
	}
	history, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{{Name: squashed.Name, Digest: squashed.Digest}}, squashed.Artifact.ToDigest, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, after, history); err != nil {
		t.Fatalf("readiness for the replacement history: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, next); err != nil {
		t.Fatalf("migrate up after replacement: %v", err)
	}
	if status, err := backend.ArtifactStatus(ctx, next); err != nil || len(status) != 1 || !status[0].Applied {
		t.Fatalf("replacement status: %#v, %v", status, err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := read.Find(ctx, store.Request{Collection: collection, ID: "retained"})
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(document, actual) {
		t.Fatalf("content changed: %#v -> %#v", document, actual)
	}
	if again, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{}); err != nil || len(again) != 0 {
		t.Fatalf("repeat = %v, %v", again, err)
	}
}

func TestPostgresRebaselineRefusesDiscardingRecordedSemanticStep(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	title := atlasTextField("posts-title", "title")
	headline := atlasTextField("posts-headline", "headline")
	before, after := atlasTestManifest(title), atlasTestManifest(headline)
	old, next := t.TempDir(), t.TempDir()
	initial, err := BuildArtifact(ctx, "initial", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(old, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	rename, err := BuildArtifact(ctx, "rename-title", &before, after, []Rename{{
		Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: after.Snapshot().Collections[0],
		BeforeField: &title, AfterField: &headline,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(old, "rename-title", rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	squashed, err := BuildArtifact(ctx, "initial", nil, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(next, "initial", squashed, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, old, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "rename_content step would be discarded") {
		t.Fatalf("discard recorded rename = %v", err)
	}
	if status, err := backend.ArtifactStatus(ctx, old); err != nil || len(status) != 2 || !status[1].Applied {
		t.Fatalf("refusal changed old history: %#v, %v", status, err)
	}
}
