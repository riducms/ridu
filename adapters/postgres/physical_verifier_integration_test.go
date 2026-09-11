package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresPhysicalVerifierRejectsRequiredIndexDrift(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(context.Context, *Store, string, string) error
	}{
		{
			name: "missing required unique index",
			mutate: func(ctx context.Context, backend *Store, _, index string) error {
				_, err := backend.pool.Exec(ctx, "DROP INDEX "+quote(index))
				return err
			},
		},
		{
			name: "required index is invalid",
			mutate: func(ctx context.Context, backend *Store, _, index string) error {
				_, err := backend.pool.Exec(ctx, `UPDATE pg_index SET indisvalid = false WHERE indexrelid = to_regclass(current_schema() || '.' || $1)`, index)
				return err
			},
		},
		{
			name: "required index is not ready",
			mutate: func(ctx context.Context, backend *Store, _, index string) error {
				_, err := backend.pool.Exec(ctx, `UPDATE pg_index SET indisready = false WHERE indexrelid = to_regclass(current_schema() || '.' || $1)`, index)
				return err
			},
		},
		{
			name: "required unique index materially changed",
			mutate: func(ctx context.Context, backend *Store, table, index string) error {
				if _, err := backend.pool.Exec(ctx, "DROP INDEX "+quote(index)); err != nil {
					return err
				}
				_, err := backend.pool.Exec(ctx, "CREATE INDEX "+quote(index)+" ON "+quote(table)+" (id)")
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			backend, manifest, directory, history, table, requiredUnique := postgresPhysicalVerifierFixture(t, ctx)
			if err := test.mutate(ctx, backend, table, requiredUnique); err != nil {
				t.Fatal(err)
			}
			assertPostgresPhysicalVerificationRejected(t, ctx, backend, manifest, directory, history, requiredUnique)
		})
	}
}

func TestPostgresPhysicalVerifierRejectsFirstMigrationDriftBeforeLedgerMutation(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	directory := t.TempDir()
	artifact, err := BuildArtifact(ctx, "initial", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `CREATE TABLE application_preexisting (id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "application_preexisting") {
		t.Fatalf("first migration drift error = %v", err)
	}
	var artifactLedger, stepLedger bool
	if err := backend.pool.QueryRow(ctx, `SELECT
to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL,
to_regclass(current_schema() || '.ridu_migration_steps') IS NOT NULL`).Scan(&artifactLedger, &stepLedger); err != nil {
		t.Fatal(err)
	}
	if artifactLedger || stepLedger {
		t.Fatalf("failed first-migration preflight mutated ledgers: migrations=%t steps=%t", artifactLedger, stepLedger)
	}
}

func TestPostgresPhysicalVerifierRejectsBehaviorChangingExtras(t *testing.T) {
	for _, test := range []struct {
		name      string
		object    string
		statement func(string, string) string
	}{
		{
			name:   "unique index",
			object: "application_unique",
			statement: func(table, object string) string {
				return "CREATE UNIQUE INDEX " + quote(object) + " ON " + quote(table) + " (created_at)"
			},
		},
		{
			name:   "exclusion index",
			object: "application_exclusion",
			statement: func(table, object string) string {
				return "ALTER TABLE " + quote(table) + " ADD CONSTRAINT " + quote(object) + " EXCLUDE USING gist ((point(0, 0)) WITH ~=)"
			},
		},
		{
			name:   "partial index",
			object: "application_partial",
			statement: func(table, object string) string {
				return "CREATE INDEX " + quote(object) + " ON " + quote(table) + " (created_at) WHERE id <> ''"
			},
		},
		{
			name:   "expression index",
			object: "application_expression",
			statement: func(table, object string) string {
				return "CREATE INDEX " + quote(object) + " ON " + quote(table) + " ((lower(id)))"
			},
		},
		{
			name:   "non-btree access method",
			object: "application_hash",
			statement: func(table, object string) string {
				return "CREATE INDEX " + quote(object) + " ON " + quote(table) + " USING hash (id)"
			},
		},
		{
			name:   "non-default operator class",
			object: "application_pattern_ops",
			statement: func(table, object string) string {
				return "CREATE INDEX " + quote(object) + " ON " + quote(table) + " (id text_pattern_ops)"
			},
		},
		{
			name:   "reserved ordinary index",
			object: "RIDU_application_lookup",
			statement: func(table, object string) string {
				return "CREATE INDEX " + quote(object) + " ON " + quote(table) + " (created_at)"
			},
		},
		{
			name:   "reserved z-prefixed ordinary index",
			object: "Z_application_lookup",
			statement: func(table, object string) string {
				return "CREATE INDEX " + quote(object) + " ON " + quote(table) + " (created_at)"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			backend, manifest, directory, history, table, _ := postgresPhysicalVerifierFixture(t, ctx)
			if _, err := backend.pool.Exec(ctx, test.statement(table, test.object)); err != nil {
				t.Fatal(err)
			}
			assertPostgresPhysicalVerificationRejected(t, ctx, backend, manifest, directory, history, test.object)
		})
	}
}

func TestPostgresPhysicalVerifierRejectsTriggerThatBreaksValidWrite(t *testing.T) {
	ctx := context.Background()
	backend, manifest, directory, history, table, _ := postgresPhysicalVerifierFixture(t, ctx)
	if _, err := backend.pool.Exec(ctx, `CREATE FUNCTION application_reject_ridu_write() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'application trigger rejected valid Ridu write'; END $$`); err != nil {
		t.Fatal(err)
	}
	const trigger = "application_reject_write"
	if _, err := backend.pool.Exec(ctx, "CREATE TRIGGER "+quote(trigger)+" BEFORE INSERT ON "+quote(table)+" FOR EACH ROW EXECUTE FUNCTION application_reject_ridu_write()"); err != nil {
		t.Fatal(err)
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := transaction.Create(ctx, store.CreateRequest{
		Collection: manifest.Snapshot().Collections[0],
		ID:         "otherwise-valid",
		Values:     store.Values{"title": store.String("Valid title")},
	})
	if rollbackErr := transaction.Rollback(ctx); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	if writeErr == nil || !strings.Contains(writeErr.Error(), "application trigger rejected valid Ridu write") {
		t.Fatalf("valid Ridu write error = %v", writeErr)
	}
	assertPostgresPhysicalVerificationRejected(t, ctx, backend, manifest, directory, history, trigger)
}

func TestPostgresPhysicalVerifierAllowsAndPreservesClusteredOrdinaryLookupIndex(t *testing.T) {
	ctx := context.Background()
	backend, manifest, directory, history, table, _ := postgresPhysicalVerifierFixture(t, ctx)
	const index = "application_created_lookup"
	if _, err := backend.pool.Exec(ctx, "CREATE INDEX "+quote(index)+" ON "+quote(table)+" (created_at DESC)"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, "CLUSTER "+quote(table)+" USING "+quote(index)); err != nil {
		t.Fatal(err)
	}
	var clustered bool
	if err := backend.pool.QueryRow(ctx, `SELECT indisclustered FROM pg_index WHERE indexrelid = to_regclass(current_schema() || '.' || $1)`, index).Scan(&clustered); err != nil || !clustered {
		t.Fatalf("harmless lookup index clustered = %t, %v", clustered, err)
	}
	if err := backend.Ready(ctx, manifest); err != nil {
		t.Fatalf("Ready rejected harmless clustered lookup index: %v", err)
	}
	if err := backend.ReadyWithMigrationHistory(ctx, manifest, history); err != nil {
		t.Fatalf("ReadyWithMigrationHistory rejected harmless clustered lookup index: %v", err)
	}
	if statuses, err := backend.ArtifactStatus(ctx, directory); err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("ArtifactStatus with harmless clustered lookup index = %#v, %v", statuses, err)
	}

	title := atlasTextField("posts-title", "title")
	title.Unique = true
	ahead := atlasTestManifest(title, atlasTextField("posts-summary", "summary"))
	artifact, err := BuildArtifact(ctx, "add-summary", &manifest, ahead, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "add-summary", artifact, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("later migration rejected harmless clustered lookup index: %v", err)
	}
	var preserved, stillClustered bool
	if err := backend.pool.QueryRow(ctx, `SELECT true, indisclustered FROM pg_index WHERE indexrelid = to_regclass(current_schema() || '.' || $1)`, index).Scan(&preserved, &stillClustered); err != nil || !preserved || !stillClustered {
		t.Fatalf("harmless clustered lookup index preserved = %t, clustered = %t, %v", preserved, stillClustered, err)
	}
	if err := backend.Ready(ctx, ahead); err != nil {
		t.Fatalf("post-migration Ready rejected harmless clustered lookup index: %v", err)
	}
}

func TestPostgresPhysicalVerifierRejectsPendingArtifactBeforeSchemaOrLedgerMutation(t *testing.T) {
	ctx := context.Background()
	backend, manifest, directory, _, table, _ := postgresPhysicalVerifierFixture(t, ctx)
	title := atlasTextField("posts-title", "title")
	title.Unique = true
	ahead := atlasTestManifest(title, atlasTextField("posts-summary", "summary"))
	artifact, err := BuildArtifact(ctx, "add-summary", &manifest, ahead, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := migrationartifact.Create(directory, "add-summary", artifact, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	const driftIndex = "application_pending_preflight_unique"
	if _, err := backend.pool.Exec(ctx, "CREATE UNIQUE INDEX "+quote(driftIndex)+" ON "+quote(table)+" (created_at)"); err != nil {
		t.Fatal(err)
	}

	assertPendingArtifactUntouched := func(stage string) {
		t.Helper()
		var columnExists, artifactApplied, stepsExist bool
		if err := backend.pool.QueryRow(ctx, `SELECT
EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2),
EXISTS (SELECT 1 FROM ridu_migrations WHERE name = $3),
EXISTS (SELECT 1 FROM ridu_migration_steps WHERE artifact_name = $3)`, table, fieldColumn("posts-summary"), pending.Name).Scan(&columnExists, &artifactApplied, &stepsExist); err != nil {
			t.Fatal(err)
		}
		if columnExists || artifactApplied || stepsExist {
			t.Fatalf("%s pending migration state: column=%t artifact=%t steps=%t", stage, columnExists, artifactApplied, stepsExist)
		}
	}
	assertPendingArtifactUntouched("before preflight")
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), driftIndex) {
		t.Fatalf("pending migration preflight error = %v", err)
	}
	assertPendingArtifactUntouched("after rejected preflight")
}

func postgresPhysicalVerifierFixture(t *testing.T, ctx context.Context) (*Store, schema.Manifest, string, string, string, string) {
	t.Helper()
	backend := migrationArtifactTestBackend(t)
	title := atlasTextField("posts-title", "title")
	title.Unique = true
	manifest := atlasTestManifest(title)
	directory := t.TempDir()
	artifact, err := BuildArtifact(ctx, "initial", nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	file, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	history, err := ridumigration.DigestArtifactHistory([]ridumigration.ArtifactIdentity{{Name: file.Name, Digest: file.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	physical := atlasSchema(manifest, atlasIdentityMap{})
	table := physical.Tables[0]
	for _, index := range table.Indexes {
		if index.Unique {
			return backend, manifest, directory, history, table.Name, index.Name
		}
	}
	t.Fatal("test manifest did not produce a required unique index")
	return nil, schema.Manifest{}, "", "", "", ""
}

func assertPostgresPhysicalVerificationRejected(t *testing.T, ctx context.Context, backend *Store, manifest schema.Manifest, directory, history, object string) {
	t.Helper()
	checks := []struct {
		name string
		run  func() error
	}{
		{"Ready", func() error { return backend.Ready(ctx, manifest) }},
		{"ReadyWithMigrationHistory", func() error { return backend.ReadyWithMigrationHistory(ctx, manifest, history) }},
		{"ArtifactStatus", func() error { _, err := backend.ArtifactStatus(ctx, directory); return err }},
		{"completed ApplyArtifacts preflight", func() error { return backend.ApplyArtifacts(ctx, directory) }},
	}
	for _, check := range checks {
		if err := check.run(); err == nil || !strings.Contains(err.Error(), object) {
			t.Errorf("%s error = %v, want object %q", check.name, err, object)
		}
	}
}

func TestPostgresPhysicalVerifierErrorsDoNotExposeDatabaseURL(t *testing.T) {
	ctx := context.Background()
	backend, manifest, _, _, table, _ := postgresPhysicalVerifierFixture(t, ctx)
	const object = "application_secret_sentinel"
	if _, err := backend.pool.Exec(ctx, fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (created_at)", quote(object), quote(table))); err != nil {
		t.Fatal(err)
	}
	err := backend.Ready(ctx, manifest)
	if err == nil || !strings.Contains(err.Error(), object) {
		t.Fatalf("Ready error = %v", err)
	}
	if strings.Contains(err.Error(), backend.pool.Config().ConnString()) {
		t.Fatalf("physical verifier exposed the PostgreSQL connection URL: %v", err)
	}
}
