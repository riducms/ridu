package sqlite

import (
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

func TestSQLiteRebaselineRefusesUnsynchronizedUniquenessDespiteMatchingPhysicalIndex(t *testing.T) {
	ctx := t.Context()
	oldManifest, err := ridu.Resolve(ridu.Config{Name: "rebaseline derived state", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Index()}}}})
	if err != nil {
		t.Fatal(err)
	}
	uniqueManifest, err := ridu.Resolve(ridu.Config{Name: "rebaseline derived state", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title").Index().Unique()}}}})
	if err != nil {
		t.Fatal(err)
	}
	oldDirectory, replacementDirectory := t.TempDir(), t.TempDir()
	for index, item := range []struct {
		directory string
		manifest  schema.Manifest
	}{{oldDirectory, oldManifest}, {replacementDirectory, uniqueManifest}} {
		artifact, err := planArtifact(ctx, "initial", nil, item.manifest, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrationartifact.Create(item.directory, "initial", artifact, time.Unix(int64(index+1), 0)); err != nil {
			t.Fatal(err)
		}
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, oldDirectory); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document, err := write.Create(ctx, store.CreateRequest{Collection: oldManifest.Snapshot().Collections[0], ID: "retained", Values: store.Values{"title": store.String("duplicate")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := readSQLiteArtifactLedger(ctx, backend.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ReplaceBaseline(ctx, replacementDirectory, migration.BaselineReplacementOptions{PreviousDirectory: oldDirectory, AllowProduction: true}); err == nil || !strings.Contains(err.Error(), "replacement schema metadata differs from the synchronized database") {
		t.Fatalf("unsynchronized uniqueness replacement = %v", err)
	}
	after, err := readSQLiteArtifactLedger(ctx, backend.db)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("refusal changed history: before %#v, after %#v, error %v", before, after, err)
	}
	if err := backend.Ready(ctx, oldManifest); err != nil {
		t.Fatalf("refusal changed old schema readiness: %v", err)
	}
	if err := backend.Ready(ctx, uniqueManifest); err == nil {
		t.Fatal("refusal falsely authorized the unsynchronized unique schema")
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := read.Find(ctx, store.Request{Collection: oldManifest.Snapshot().Collections[0], ID: "retained"})
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(document, retained) {
		t.Fatalf("refusal changed content: before %#v, after %#v", document, retained)
	}
}
