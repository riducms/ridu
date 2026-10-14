package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func developmentUpgradeManifest(t *testing.T) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite development upgrade", Collections: []ridu.Collection{{
		Slug: "posts", Versions: true,
		Fields:  field.Fields{field.Text("title").Index(), field.Text("tenant"), field.Number("score")},
		Indexes: []ridu.CollectionIndex{{Fields: []string{"tenant", "score"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// ageAdapterIndexes rewrites a development database's indexes as an older adapter could have left
// them: every document index with another definition and every base-schema index missing.
func ageAdapterIndexes(t *testing.T, backend *Store) {
	t.Helper()
	ctx := context.Background()
	rows, err := backend.db.QueryContext(ctx, `SELECT name, tbl_name FROM sqlite_schema WHERE type = 'index' AND sql IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for rows.Next() {
		var name, table string
		if err := rows.Scan(&name, &table); err != nil {
			t.Fatal(err)
		}
		statements = append(statements, "DROP INDEX "+quoteSQLiteIdentifier(name))
		if strings.HasPrefix(name, documentIndexPrefix) {
			statements = append(statements, "CREATE INDEX "+quoteSQLiteIdentifier(name)+" ON "+table+" (collection_id, id)")
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := backend.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func TestSQLiteDevelopmentSyncRebuildsIndexesAnOlderAdapterLeft(t *testing.T) {
	ctx := context.Background()
	manifest := developmentUpgradeManifest(t)
	collection := manifest.Snapshot().Collections[0]
	backend, err := Open(ctx, filepath.Join(t.TempDir(), "development.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "kept", Values: store.Values{"title": store.String("Kept")}}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	ageAdapterIndexes(t, backend)
	if err := backend.Ready(ctx, manifest); err == nil {
		t.Fatal("readiness accepted indexes from another adapter contract")
	}

	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatalf("development sync over older indexes: %v", err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("readiness after development sync: %v", err)
	}
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	if _, err := read.Find(ctx, store.Request{Collection: collection, ID: "kept"}); err != nil {
		t.Fatalf("document after development sync: %v", err)
	}
}

func TestSQLiteDevelopmentSyncStillRejectsAMissingTable(t *testing.T) {
	ctx := context.Background()
	manifest := developmentUpgradeManifest(t)
	backend, err := Open(ctx, filepath.Join(t.TempDir(), "development.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `DROP TABLE ridu_preferences`); err != nil {
		t.Fatal(err)
	}
	err = backend.Migrate(ctx, manifest)
	if err == nil || !strings.Contains(err.Error(), "ridu_preferences") {
		t.Fatalf("development sync without a table = %v, want the missing table reported", err)
	}
}
