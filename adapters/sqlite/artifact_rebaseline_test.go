package sqlite

import (
	"context"
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

func TestSQLiteRebaselineAdoptsAdminOnlyReplacementAndRefusesUnsafeReplacement(t *testing.T) {
	ctx := t.Context()
	before, err := ridu.Resolve(ridu.Config{Name: "Presentation replacement", Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Index().Unique()}}}})
	if err != nil {
		t.Fatal(err)
	}
	presentation := before.Snapshot()
	presentation.Collections[0].Fields[0].Admin.Label = "Updated title label"
	after := schema.NewManifest(presentation)
	old, next := t.TempDir(), t.TempDir()
	for index, entry := range []struct {
		directory string
		manifest  schema.Manifest
	}{{old, before}, {next, after}} {
		artifact, err := planArtifact(ctx, "initial", nil, entry.manifest, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrationartifact.Create(entry.directory, "initial", artifact, time.Unix(int64(index+1), 0)); err != nil {
			t.Fatal(err)
		}
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, old); err != nil {
		t.Fatal(err)
	}
	resource := before.Snapshot().Collections[0]
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document, err := write.Create(ctx, store.CreateRequest{Collection: resource, ID: "retained", Values: store.Values{"title": store.String("Keep this title")}})
	if err != nil {
		t.Fatal(err)
	}
	version, err := write.(store.VersionTransaction).SaveVersion(ctx, resource, document, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := readSQLiteArtifactLedger(ctx, backend.db)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func() {
		t.Helper()
		current, err := readSQLiteArtifactLedger(ctx, backend.db)
		if err != nil || !reflect.DeepEqual(rows, current) {
			t.Fatalf("refusal changed ledger: %#v, %v", current, err)
		}
	}
	if _, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{}); err == nil || !strings.Contains(err.Error(), "--previous-history") {
		t.Fatalf("missing evidence error = %v", err)
	}
	assertUnchanged()
	drift := t.TempDir()
	changed, err := planArtifact(ctx, "initial", nil, sqliteMigrationManifest(t, true), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(drift, "initial", changed, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReplaceBaseline(ctx, drift, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "replacement schema differs") {
		t.Fatalf("drift error = %v", err)
	}
	assertUnchanged()
	// SQLite never synchronizes a database with history, so it is always
	// exactly at its recorded head: a binary built from that history may serve it.
	options := migration.BaselineReplacementOptions{PreviousDirectory: old}
	if _, err := backend.ReplaceBaseline(ctx, next, options); !errors.Is(err, migration.ErrReplacementNeedsConfirmation) {
		t.Fatalf("replacement at the recorded head = %v", err)
	}
	assertUnchanged()
	options.AllowProduction = true
	if recorded, err := backend.ReplaceBaseline(ctx, next, options); err != nil || len(recorded) != 1 {
		t.Fatalf("admin-only baseline replacement = %v, %v", recorded, err)
	}
	if statuses, err := backend.ArtifactStatus(ctx, next, after); err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("replacement history = %#v, %v", statuses, err)
	}
	recorded, exists, err := backend.DevelopmentManifest(ctx)
	if err != nil || !exists || !recorded.Equal(after) {
		t.Fatalf("replacement schema record = %t, %v", exists, err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatalf("replacement readiness = %v", err)
	}
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := read.Find(ctx, store.Request{Collection: after.Snapshot().Collections[0], ID: document.ID})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := read.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: after.Snapshot().Collections[0], DocumentID: document.ID})
	if err != nil || len(versions) != 1 || !reflect.DeepEqual(document, actual) || !reflect.DeepEqual(version, versions[0]) {
		t.Fatalf("replacement changed content or snapshot metadata: %#v, %#v, %v", actual, versions, err)
	}
	if err := read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// The accepted unique-value projection must remain intact too.
	duplicate, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := duplicate.Create(ctx, store.CreateRequest{Collection: after.Snapshot().Collections[0], ID: "duplicate", Values: store.Values{"title": store.String("Keep this title")}}); err == nil {
		t.Fatal("replacement lost the existing uniqueness projection")
	}
	if err := duplicate.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if again, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{}); err != nil || len(again) != 0 {
		t.Fatalf("repeat = %v, %v", again, err)
	}
}

// A squash may be followed by a new data transform. The replacement records
// the squashed history and leaves the transform for migrate up to run.
func TestSQLiteRebaselineLeavesNewTransformPendingForMigrateUp(t *testing.T) {
	ctx := t.Context()
	manifest := sqliteMigrationManifest(t, false)
	old, next := t.TempDir(), t.TempDir()
	if _, err := CreateArtifact(ctx, old, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	squashed, err := CreateArtifact(ctx, next, "initial", manifest, time.Unix(2, 0), ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "seed", Checksum: migration.DataTransformChecksum([]byte("seed"))}
	ran := false
	transform := migration.DataTransform{DataTransformDescriptor: descriptor,
		Up:   func(context.Context, migration.DataTransaction) error { ran = true; return nil },
		Down: func(context.Context, migration.DataTransaction) error { return nil }}
	if _, err := CreateArtifact(ctx, next, "seed", manifest, time.Unix(3, 0), ArtifactOptions{DataTransforms: []migration.DataTransformDescriptor{descriptor}}); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, old); err != nil {
		t.Fatal(err)
	}
	recorded, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true})
	if err != nil || !reflect.DeepEqual(recorded, []string{squashed.Name}) {
		t.Fatalf("replacement before a new transform = %v, %v", recorded, err)
	}
	statuses, err := backend.ArtifactStatus(ctx, next, manifest)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("status after replacement = %#v, %v", statuses, err)
	}
	if err := backend.ApplyArtifacts(ctx, next, transform); err != nil || !ran {
		t.Fatalf("migrate up after replacement ran transform %t: %v", ran, err)
	}
}

func TestSQLiteRebaselineRefusesDiscardingRecordedTransforms(t *testing.T) {
	ctx := t.Context()
	manifest := sqliteMigrationManifest(t, false)
	old, next := t.TempDir(), t.TempDir()
	descriptor := migration.DataTransformDescriptor{Name: "seed", Checksum: migration.DataTransformChecksum([]byte("seed"))}
	transform := migration.DataTransform{DataTransformDescriptor: descriptor, Up: func(context.Context, migration.DataTransaction) error { return nil }, Down: func(context.Context, migration.DataTransaction) error { return nil }}
	artifact, err := planArtifact(ctx, "initial", nil, manifest, false, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(old, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	plain, err := planArtifact(ctx, "initial", nil, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(next, "initial", plain, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, old, transform); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "data_transform step would be discarded") {
		t.Fatalf("discard transform = %v", err)
	}
	if status, err := backend.ArtifactStatus(ctx, old, manifest); err != nil || !status[0].Applied {
		t.Fatalf("refusal damaged original history: %#v, %v", status, err)
	}
}
