package sqlite

import (
	"context"
	"strings"
	"testing"
)

func TestReconcileDocumentIndexesScopesAdapterOwnershipToDocuments(t *testing.T) {
	ctx := context.Background()

	t.Run("removes only stale document indexes", func(t *testing.T) {
		backend := newSQLiteMigrationStore(t)
		if err := installSchema(ctx, backend.db); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, `CREATE TABLE ridu_plugin_search_entries (id TEXT PRIMARY KEY, payload TEXT) STRICT`); err != nil {
			t.Fatal(err)
		}
		staleDocumentIndex := documentIndexPrefix + "stale_document"
		pluginIndex := documentIndexPrefix + "plugin_search"
		if _, err := backend.db.ExecContext(ctx, "CREATE INDEX "+quoteSQLiteIdentifier(staleDocumentIndex)+" ON ridu_documents(created_at)"); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, "CREATE INDEX "+quoteSQLiteIdentifier(pluginIndex)+" ON ridu_plugin_search_entries(payload)"); err != nil {
			t.Fatal(err)
		}

		if err := reconcileDocumentIndexes(ctx, backend.db, sqliteMigrationManifest(t, false)); err != nil {
			t.Fatal(err)
		}

		var staleCount, pluginCount int
		if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ?`, staleDocumentIndex).Scan(&staleCount); err != nil {
			t.Fatal(err)
		}
		if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ? AND tbl_name = 'ridu_plugin_search_entries'`, pluginIndex).Scan(&pluginCount); err != nil {
			t.Fatal(err)
		}
		if staleCount != 0 || pluginCount != 1 {
			t.Fatalf("reconciled index ownership: stale document=%d plugin=%d", staleCount, pluginCount)
		}
	})

	t.Run("rejects desired name owned by another table", func(t *testing.T) {
		backend := newSQLiteMigrationStore(t)
		if err := installSchema(ctx, backend.db); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.db.ExecContext(ctx, `CREATE TABLE ridu_plugin_search_entries (id TEXT PRIMARY KEY, summary TEXT) STRICT`); err != nil {
			t.Fatal(err)
		}
		manifest := sqliteMigrationManifest(t, true)
		collision := documentFieldIndexName(manifest.Snapshot().Collections[0].ID, "summary", nil)
		if _, err := backend.db.ExecContext(ctx, "CREATE INDEX "+quoteSQLiteIdentifier(collision)+" ON ridu_plugin_search_entries(summary)"); err != nil {
			t.Fatal(err)
		}

		err := reconcileDocumentIndexes(ctx, backend.db, manifest)
		if err == nil || !strings.Contains(err.Error(), "name is already used by table \"ridu_plugin_search_entries\"") {
			t.Fatalf("document index collision error = %v", err)
		}
		var table string
		if err := backend.db.QueryRowContext(ctx, `SELECT tbl_name FROM sqlite_schema WHERE type = 'index' AND name = ?`, collision).Scan(&table); err != nil {
			t.Fatal(err)
		}
		if table != "ridu_plugin_search_entries" {
			t.Fatalf("colliding index table = %q", table)
		}
	})

	t.Run("replaces a desired document index with the wrong expression", func(t *testing.T) {
		backend := newSQLiteMigrationStore(t)
		if err := installSchema(ctx, backend.db); err != nil {
			t.Fatal(err)
		}
		manifest := sqliteMigrationManifest(t, true)
		name := documentFieldIndexName(manifest.Snapshot().Collections[0].ID, "summary", nil)
		if _, err := backend.db.ExecContext(ctx, "CREATE INDEX "+quoteSQLiteIdentifier(name)+" ON ridu_documents(created_at)"); err != nil {
			t.Fatal(err)
		}

		if err := reconcileDocumentIndexes(ctx, backend.db, manifest); err != nil {
			t.Fatal(err)
		}

		var statement string
		if err := backend.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type = 'index' AND name = ?`, name).Scan(&statement); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(statement, "created_at") || !strings.Contains(statement, "json_extract") {
			t.Fatalf("reconciled document index = %q", statement)
		}
	})
}
