package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// Readiness repeats its complete inspection only when verified state changes.
// Each probe must still observe any later drift in the physical schema or the
// ledger, and a different manifest instance is always verified in full.
func TestPostgresReadinessRechecksAfterDrift(t *testing.T) {
	ctx := context.Background()
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	schemaName := fmt.Sprintf("ridu_readiness_%d", time.Now().UnixNano())
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schemaName}.Sanitize()+` CASCADE`)
		admin.Close()
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	manifest, err := ridu.Resolve(ridu.Config{Name: "readiness", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	artifact, err := postgres.BuildArtifact(ctx, "initial", nil, manifest, nil, false)
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
	history, err := migration.DigestArtifactHistory([]migration.ArtifactIdentity{{Name: file.Name, Digest: file.Digest}}, artifact.ToDigest, manifest)
	if err != nil {
		t.Fatal(err)
	}
	var table string
	if err := admin.QueryRow(ctx, `SELECT relname FROM pg_class WHERE relnamespace = $1::regnamespace AND relname LIKE 'z_c_%' AND relkind = 'r'`, schemaName).Scan(&table); err != nil {
		t.Fatal(err)
	}
	qualified := pgx.Identifier{schemaName, table}.Sanitize()
	ready := func(manifest schema.Manifest) error {
		return backend.ReadyWithMigrationHistory(ctx, manifest, history)
	}
	execute := func(statement string) {
		t.Helper()
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for probe := 0; probe < 3; probe++ {
		if err := ready(manifest); err != nil {
			t.Fatalf("probe %d: %v", probe, err)
		}
	}
	for _, drift := range []struct {
		name, apply, restore string
	}{
		{"extra column", `ALTER TABLE ` + qualified + ` ADD COLUMN drift text`, `ALTER TABLE ` + qualified + ` DROP COLUMN drift`},
		{"column default", `ALTER TABLE ` + qualified + ` ALTER COLUMN id SET DEFAULT 'drift'`, `ALTER TABLE ` + qualified + ` ALTER COLUMN id DROP DEFAULT`},
		{"nullability", `ALTER TABLE ` + qualified + ` ALTER COLUMN created_at DROP NOT NULL`, `ALTER TABLE ` + qualified + ` ALTER COLUMN created_at SET NOT NULL`},
		{"missing table", `ALTER TABLE ` + qualified + ` RENAME TO drifted`, `ALTER TABLE ` + pgx.Identifier{schemaName, "drifted"}.Sanitize() + ` RENAME TO ` + pgx.Identifier{table}.Sanitize()},
	} {
		execute(drift.apply)
		if err := ready(manifest); err == nil {
			t.Fatalf("readiness accepted %s after a verified probe", drift.name)
		}
		execute(drift.restore)
		if err := ready(manifest); err != nil {
			t.Fatalf("readiness after restoring %s: %v", drift.name, err)
		}
	}
	// Incomplete migration work is visible on the next probe.
	steps := pgx.Identifier{schemaName, "ridu_migration_steps"}.Sanitize()
	execute(`CREATE TABLE IF NOT EXISTS ` + steps + ` (
artifact_name text NOT NULL, artifact_digest text NOT NULL, phase_id text NOT NULL, step_id text NOT NULL,
phase_mode text NOT NULL, state text NOT NULL, checkpoint jsonb NOT NULL DEFAULT '{}'::jsonb,
attempts integer NOT NULL DEFAULT 0, started_at timestamptz NOT NULL DEFAULT now(),
updated_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz,
PRIMARY KEY (artifact_name, phase_id, step_id))`)
	if err := ready(manifest); err != nil {
		t.Fatalf("readiness with an empty step ledger: %v", err)
	}
	execute(`INSERT INTO ` + steps + ` (artifact_name, artifact_digest, phase_id, step_id, phase_mode, state)
VALUES ('zz-in-progress', 'pending', 'phase', 'step', 'transaction', 'running')`)
	if err := ready(manifest); err == nil {
		t.Fatal("readiness accepted incomplete migration work after a verified probe")
	}
	execute(`DELETE FROM ` + pgx.Identifier{schemaName, "ridu_migration_steps"}.Sanitize() + ` WHERE artifact_name = 'zz-in-progress'`)
	if err := ready(manifest); err != nil {
		t.Fatal(err)
	}
	// A manifest instance that never passed a complete check is verified in full.
	ahead, err := ridu.Resolve(ridu.Config{Name: "readiness", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ready(ahead); err == nil {
		t.Fatal("readiness reused a verified result for another manifest")
	}
	if err := backend.Ready(ctx, ahead); err == nil {
		t.Fatal("manifest readiness reused a verified history result")
	}
	if err := ready(manifest); err != nil {
		t.Fatal(err)
	}
}
