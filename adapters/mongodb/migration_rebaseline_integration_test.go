package mongodb

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

func resolveMongoRebaselineManifest(t *testing.T, collections ...ridu.Collection) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{Name: "Rebaseline", Collections: collections})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// A development database baselined onto the first initial, then advanced by
// ridu dev, adopts a re-squashed initial without running it. ridu dev added an
// indexed field and changed the kind of an empty nested field after scanning
// it; the replacement accepts both instead of re-planning them as a migration.
func TestMongoDBRebaselineAdoptsSquashedHistoryAfterDevelopmentSync(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	before := resolveMongoRebaselineManifest(t, ridu.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Group("meta", field.Fields{field.Text("rating")})}})
	after := resolveMongoRebaselineManifest(t, ridu.Collection{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary").Index(), field.Group("meta", field.Fields{field.Number("rating")})}})
	old, same, next := t.TempDir(), t.TempDir(), t.TempDir()
	if _, err := CreateArtifact(ctx, old, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, same, "initial", before, time.Unix(2, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	squashed, err := CreateArtifact(ctx, next, "initial", after, time.Unix(3, 0), ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("index drift = %v", err)
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
	files, err := migrationartifact.ReadAll(next)
	if err != nil {
		t.Fatal(err)
	}
	history, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{{Name: files[0].Name, Digest: files[0].Digest}}, files[0].Artifact.ToDigest, after)
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

// ridu dev index sync leaves a removed collection's namespaces and shared
// state behind, so a replacement cannot claim its retirement ran.
func TestMongoDBRebaselineRefusesRemovedResourcesThatWereNeverRetired(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	posts := ridu.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}}
	before := resolveMongoRebaselineManifest(t, posts, ridu.Collection{Slug: "drafts", Versions: true, Fields: field.Fields{field.Text("title")}})
	after := resolveMongoRebaselineManifest(t, posts)
	old, next := t.TempDir(), t.TempDir()
	if _, err := CreateArtifact(ctx, old, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, next, "initial", after, time.Unix(2, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "the replacement removes drafts") {
		t.Fatalf("unretired removed collection = %v", err)
	}
	if status, err := backend.ArtifactStatus(ctx, old); err != nil || len(status) != 1 || !status[0].Applied {
		t.Fatalf("refusal changed old history: %#v, %v", status, err)
	}
}

func TestMongoDBRebaselineRefusesDiscardingRecordedTransform(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	manifest := resolveMongoRebaselineManifest(t, ridu.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}})
	old, next := t.TempDir(), t.TempDir()
	if _, err := CreateArtifact(ctx, old, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "seed", Checksum: migration.DataTransformChecksum([]byte("seed"))}
	transform := migration.DataTransform{DataTransformDescriptor: descriptor,
		Up:   func(context.Context, migration.DataTransaction) error { return nil },
		Down: func(context.Context, migration.DataTransaction) error { return nil }}
	if _, err := CreateArtifact(ctx, old, "seed", manifest, time.Unix(2, 0), ArtifactOptions{DataTransforms: []migration.DataTransformDescriptor{descriptor}}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, next, "initial", manifest, time.Unix(3, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, old, RunnerOptions{AllowMaintenance: true}, transform); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReplaceBaseline(ctx, next, migration.BaselineReplacementOptions{PreviousDirectory: old, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "data_transform step would be discarded") {
		t.Fatalf("discard recorded transform = %v", err)
	}
	if status, err := backend.ArtifactStatus(ctx, old); err != nil || len(status) != 2 || !status[1].Applied {
		t.Fatalf("refusal changed old history: %#v, %v", status, err)
	}
}
