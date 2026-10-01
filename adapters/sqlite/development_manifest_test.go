package sqlite

import (
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestDevelopmentManifestRequiresDatabaseProvenance(t *testing.T) {
	ctx := t.Context()
	backend := newSQLiteMigrationStore(t)
	if _, exists, err := backend.DevelopmentManifest(ctx); err != nil || exists {
		t.Fatalf("fresh database baseline: exists=%t, %v", exists, err)
	}
	manifest := sqliteMigrationManifest(t, false)
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(manifest) {
		t.Fatalf("synchronized baseline: exists=%t, %v", exists, err)
	}
	collection := manifest.Snapshot().Collections[0]
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: collection, ID: "kept", Values: store.Values{"title": store.String("Keep this content")}}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	candidate, err := ridu.Resolve(ridu.Config{Name: "SQLite migrations", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Number("title")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, candidate); err == nil || !strings.Contains(err.Error(), "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM") {
		t.Fatalf("synchronization certified incompatible content: %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(manifest) {
		t.Fatalf("rejected synchronization changed baseline: exists=%t, %v", exists, err)
	}
	if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_sqlite_schema SET manifest_digest = 'corrupt' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("corrupt schema digest was accepted: %v", err)
	}
	if _, err := backend.db.ExecContext(ctx, `DELETE FROM ridu_sqlite_schema WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.DevelopmentManifest(ctx); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("missing application schema was accepted: %v", err)
	}
	if err := backend.Migrate(ctx, manifest); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("synchronization fabricated a missing baseline: %v", err)
	}
	transaction, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	document, err := transaction.Find(ctx, store.Request{Collection: collection, ID: "kept"})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := document.Values["title"].StringValue(); title != "Keep this content" {
		t.Fatalf("unknown-schema refusal changed content: %#v", document.Values)
	}
}

func TestDevelopmentBaselineAdoptionAllowsPresentationOnlyChanges(t *testing.T) {
	ctx := t.Context()
	before := sqliteMigrationManifest(t, false)
	snapshot := before.Snapshot()
	snapshot.Collections[0].Fields[0].Admin.Label = "Editorial title"
	after := schema.NewManifest(snapshot)
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, after); err != nil {
		t.Fatal(err)
	}
	if adopted, err := backend.AdoptArtifacts(ctx, directory); err != nil || len(adopted) != 1 {
		t.Fatalf("presentation-only baseline failed: adopted=%v, %v", adopted, err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatalf("adopted storage readiness failed: %v", err)
	}
}
