package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

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
