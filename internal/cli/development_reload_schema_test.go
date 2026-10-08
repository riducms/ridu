package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
)

// A merge or checkout can bring in contracts that were generated elsewhere, so
// generation rewrites nothing on reload. The database still has the previous
// schema and must be synchronized anyway.
func TestReloadSynchronizesAManifestWhoseContractsWereAlreadyCurrent(t *testing.T) {
	ctx := context.Background()
	manifestFor := func(collections ...core.Collection) schema.Manifest {
		t.Helper()
		app, err := core.New(core.Config{Name: "Reload", Collections: collections}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		return app.Manifest()
	}
	posts := core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}}
	comments := core.Collection{Slug: "comments", Fields: field.Fields{field.Text("body")}}
	served := manifestFor(posts)
	merged := manifestFor(posts, comments)

	databasePath := filepath.Join(t.TempDir(), "development.sqlite")
	reporter := newCLIOutput(&bytes.Buffer{}, io.Discard, cliOutputOptions{})
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: served}, reporter); err != nil {
		t.Fatal(err)
	}

	unchanged := developmentPreparation{manifest: served}.comparedWith(served)
	if unchanged.schemaChanged {
		t.Fatal("an unchanged manifest was marked as a schema change")
	}
	reloaded := developmentPreparation{manifest: merged}.comparedWith(served)
	if !reloaded.schemaChanged {
		t.Fatal("a manifest that differs from the running server's was not marked as a schema change")
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, false, reloaded, reporter); err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.Ready(ctx, merged); err != nil {
		t.Fatalf("the reload left the database behind the merged schema: %v", err)
	}
}

// Once ridu migrate manages a SQLite development database, for example after
// ridu migrate baseline, ridu dev still starts over it but never changes its
// schema behind the ledger.
func TestDevelopmentSkipsSchemaSyncForAMigrationsManagedSQLiteDatabase(t *testing.T) {
	ctx := context.Background()
	manifestFor := func(collections ...core.Collection) schema.Manifest {
		t.Helper()
		app, err := core.New(core.Config{Name: "Managed", Collections: collections}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		return app.Manifest()
	}
	posts := core.Collection{Slug: "posts", Fields: field.Fields{field.Text("title")}}
	initial := manifestFor(posts)
	changed := manifestFor(posts, core.Collection{Slug: "comments", Fields: field.Fields{field.Text("body")}})
	root := t.TempDir()
	databasePath := filepath.Join(root, "development.sqlite")
	directory := filepath.Join(root, "migrations")
	var logs bytes.Buffer
	reporter := newCLIOutput(&logs, io.Discard, cliOutputOptions{})
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: initial}, reporter); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", initial, time.Unix(1, 0), sqlite.ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if adopted, err := backend.AdoptArtifacts(ctx, directory); err != nil || len(adopted) != 1 {
		t.Fatalf("baseline = %v, %v", adopted, err)
	}
	backend.Close()

	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: initial}, reporter); err != nil {
		t.Fatalf("ridu dev over a current managed database: %v", err)
	}
	if !strings.Contains(logs.String(), "managed by ridu migrate") {
		t.Fatalf("ridu dev did not say it skipped schema sync: %s", logs.String())
	}
	_, err = synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, false, developmentPreparation{manifest: changed, schemaChanged: true}, reporter)
	if err == nil || !strings.Contains(err.Error(), "ridu migrate create") {
		t.Fatalf("ridu dev over a managed database behind the config = %v", err)
	}
}
