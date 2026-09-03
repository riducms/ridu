package schema_test

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestPluginMigrationSQLHasOneAdapterOwnedStatement(t *testing.T) {
	for name, test := range map[string]struct {
		adapter   schema.PluginDatabaseAdapter
		statement string
		valid     bool
	}{
		"ddl": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `CREATE TABLE ridu_plugin_search_entries (id TEXT PRIMARY KEY) STRICT;`, valid: true,
		},
		"literal semicolon": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `INSERT INTO ridu_plugin_search_entries (id) VALUES ('one;two')`, valid: true,
		},
		"postgres function body": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `CREATE FUNCTION ridu_plugin_search_touch() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;`, valid: true,
		},
		"postgres nested comment": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `SELECT 1 /* outer /* ; COMMIT */ still outer */;`, valid: true,
		},
		"postgres escape string": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `SELECT E'one\';two'`, valid: true,
		},
		"postgres escape string cannot hide second statement": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `SELECT E'one\';two'; COMMIT`, valid: false,
		},
		"postgres unterminated escape string": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `SELECT E'unfinished\'`, valid: false,
		},
		"postgres identifier dollar is not a quote": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `SELECT name$tag$; COMMIT; SELECT name$tag$`, valid: false,
		},
		"postgres abort": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `ABORT WORK`, valid: false,
		},
		"postgres start transaction": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `START /* adapter */ TRANSACTION`, valid: false,
		},
		"postgres prepare transaction": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `PREPARE TRANSACTION 'ridu-plugin-step'`, valid: false,
		},
		"postgres named prepared statement": {
			adapter: schema.PluginDatabaseAdapterPostgres, statement: `PREPARE ridu_plugin_query AS SELECT 1`, valid: true,
		},
		"sqlite attach database": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `ATTACH DATABASE 'sidecar.sqlite' AS plugin_sidecar`, valid: false,
		},
		"sqlite detach database": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `DETACH DATABASE plugin_sidecar`, valid: false,
		},
		"sqlite pragma": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `PRAGMA writable_schema=ON`, valid: false,
		},
		"sqlite trigger body": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `CREATE TRIGGER ridu_plugin_search_touch AFTER INSERT ON ridu_plugin_search_entries BEGIN UPDATE ridu_plugin_search_entries SET touched = CASE WHEN NEW.id = 'end' THEN 1 ELSE 0 END WHERE id = NEW.id; END;`, valid: true,
		},
		"transaction": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `/* plugin */ COMMIT`, valid: false,
		},
		"transaction after ddl": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `CREATE TABLE ridu_plugin_search_entries (id TEXT); COMMIT`, valid: false,
		},
		"second statement": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `CREATE TABLE ridu_plugin_search_entries (id TEXT); SELECT 1`, valid: false,
		},
		"backslash does not hide second statement": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `SELECT '\'; COMMIT; SELECT '`, valid: false,
		},
		"postgres dollar quote is not a sqlite quote": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `SELECT $tag$; COMMIT; SELECT $tag$`, valid: false,
		},
		"postgres nested comment is not a sqlite comment": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `SELECT 1 /* outer /* nested */; COMMIT; /* end */`, valid: false,
		},
		"carriage return ends line comment": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: "SELECT 1 -- comment\r; COMMIT", valid: false,
		},
		"transaction after trigger": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `CREATE TRIGGER ridu_plugin_search_touch AFTER INSERT ON ridu_plugin_search_entries BEGIN SELECT 1; END; COMMIT`, valid: false,
		},
		"unterminated literal": {
			adapter: schema.PluginDatabaseAdapterSQLite, statement: `SELECT 'unfinished`, valid: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if actual := schema.IsValidPluginMigrationSQL(test.adapter, test.statement); actual != test.valid {
				t.Fatalf("IsValidPluginMigrationSQL() = %t, want %t", actual, test.valid)
			}
		})
	}
}
