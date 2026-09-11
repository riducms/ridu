package postgres

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

func TestPostgresStopResumeAndInvalidConcurrentIndexRecovery(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	directory, file := createInitialArtifact(t, "resume")
	artifact := file.Artifact
	if len(artifact.Phases) < 2 || artifact.Phases[0].Mode != ridumigration.PhaseTransaction {
		t.Fatalf("initial phases = %#v", artifact.Phases)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{StopAfterPhase: artifact.Name + "/" + artifact.Phases[0].ID}); err != nil {
		t.Fatal(err)
	}
	assertArtifactNotFinal(t, ctx, backend, file.Name)

	var concurrent ridumigration.ConcurrentIndexPayload
	found := false
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == ridumigration.StepConcurrentIndex {
				if err := json.Unmarshal(step.Payload, &concurrent); err != nil {
					t.Fatal(err)
				}
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found || concurrent.Action != ridumigration.ConcurrentIndexCreate {
		t.Fatalf("initial artifact has no concurrent create: %#v", artifact.Phases)
	}
	command := "CREATE "
	if concurrent.Unique {
		command += "UNIQUE "
	}
	command += "INDEX " + quote(concurrent.Name) + " ON " + quote(concurrent.Table)
	if concurrent.Method != "" {
		command += " USING " + concurrent.Method
	}
	command += " (" + strings.Join(concurrent.Parts, ", ") + ")"
	if concurrent.Predicate != "" {
		command += " WHERE " + concurrent.Predicate
	}
	if _, err := backend.pool.Exec(ctx, command); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, `UPDATE pg_index SET indisvalid = false WHERE indexrelid = to_regclass(current_schema() || '.' || $1)`, concurrent.Name); err != nil {
		t.Fatalf("mark test index invalid: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("resume after invalid concurrent index: %v", err)
	}
	var valid bool
	if err := backend.pool.QueryRow(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = to_regclass(current_schema() || '.' || $1)`, concurrent.Name).Scan(&valid); err != nil || !valid {
		t.Fatalf("recovered index valid = %t, %v", valid, err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("resumed status = %#v, %v", statuses, err)
	}
	corruptPhase := artifact.Phases[0]
	corruptStep := corruptPhase.Steps[0]
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("read artifact identity = %#v, %v", files, err)
	}
	if _, err := backend.pool.Exec(ctx, `DELETE FROM ridu_migration_steps WHERE artifact_name = $1 AND phase_id = $2 AND step_id = $3`, files[0].Name, corruptPhase.ID, corruptStep.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.ArtifactStatus(ctx, directory); err == nil || !strings.Contains(err.Error(), "is not complete") {
		t.Fatalf("status accepted incomplete step ledger for completed artifact: %v", err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "is not complete") {
		t.Fatalf("apply accepted incomplete step ledger for completed artifact: %v", err)
	}
}

func TestPostgresArtifactStatusReportsStoppedProgressWithoutCompletedStateVerification(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	directory, file := createInitialArtifact(t, "stopped-status")
	artifact := file.Artifact
	if len(artifact.Phases) < 2 || len(artifact.Phases[0].Steps) == 0 {
		t.Fatalf("initial phases = %#v", artifact.Phases)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{StopAfterPhase: artifact.Name + "/" + artifact.Phases[0].ID}); err != nil {
		t.Fatal(err)
	}

	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil {
		t.Fatalf("ArtifactStatus rejected intentionally incomplete physical state: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Applied || len(statuses[0].Phases) != len(artifact.Phases) {
		t.Fatalf("stopped artifact status = %#v", statuses)
	}
	for phaseIndex, phase := range statuses[0].Phases {
		expected := artifact.Phases[phaseIndex]
		if phase.ID != expected.ID || phase.Mode != expected.Mode || len(phase.Steps) != len(expected.Steps) {
			t.Fatalf("phase %d status = %#v, want %#v", phaseIndex, phase, expected)
		}
		wantState := "pending"
		if phaseIndex == 0 {
			wantState = "complete"
		}
		if phase.State != wantState {
			t.Fatalf("phase %s state = %q, want %q", phase.ID, phase.State, wantState)
		}
		for stepIndex, step := range phase.Steps {
			if step.ID != expected.Steps[stepIndex].ID || step.Kind != expected.Steps[stepIndex].Kind || step.State != wantState {
				t.Fatalf("phase %s step %d status = %#v, want state %q", phase.ID, stepIndex, step, wantState)
			}
		}
	}
}

func TestPostgresAdvisoryTimeout(t *testing.T) {
	t.Run("advisory timeout", func(t *testing.T) {
		backend := migrationArtifactTestBackend(t)
		ctx := context.Background()
		directory, file := createInitialArtifact(t, "lock-timeout")
		artifact := file.Artifact
		locker, err := backend.pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer locker.Release()
		if _, err := locker.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
			t.Fatal(err)
		}
		err = backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AdvisoryLockWait: 100 * time.Millisecond})
		if err == nil || !strings.Contains(err.Error(), "advisory lock wait exceeded") {
			t.Fatalf("timeout error = %v", err)
		}
		assertSchemaAbsent(t, ctx, backend, artifact)
		if _, err := locker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
			t.Fatal(err)
		}
		if err := backend.ApplyArtifacts(ctx, directory); err != nil {
			t.Fatalf("caller retry after timeout: %v", err)
		}
	})

}

func TestDevelopmentPlanAndArtifactRunnerShareSchemaLock(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	locker, err := backend.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Release()
	if _, err := locker.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	err = backend.ApplyPlan(blocked, []Statement{{Kind: "lock probe", SQL: `CREATE TABLE ridu_development_lock_probe (id integer)`}})
	cancel()
	if err == nil {
		t.Fatal("development plan bypassed the artifact runner schema lock")
	}
	var exists bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_development_lock_probe') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("blocked development plan mutated schema: exists=%t err=%v", exists, err)
	}
	if _, err := locker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyPlan(ctx, []Statement{{Kind: "lock probe", SQL: `CREATE TABLE ridu_development_lock_probe (id integer)`}}); err != nil {
		t.Fatalf("development plan after unlock: %v", err)
	}
}

func recomputeTestPhysicalDigests(t *testing.T, artifact *ridumigration.Artifact) {
	t.Helper()
	physical := ridumigration.PhysicalDigestSeed(artifact.FromDigest)
	for index := range artifact.Phases {
		phase := &artifact.Phases[index]
		phase.BeforePhysicalDigest = physical
		var err error
		phase.AfterPhysicalDigest, err = ridumigration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
		if err != nil {
			t.Fatal(err)
		}
		physical = phase.AfterPhysicalDigest
	}
}

func TestPostgresConcurrentIndexTimeoutLeavesRetryableLedger(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	directory, file := createInitialArtifact(t, "index-timeout")
	artifact := file.Artifact
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{StopAfterPhase: artifact.Name + "/" + artifact.Phases[0].ID}); err != nil {
		t.Fatal(err)
	}
	var payload ridumigration.ConcurrentIndexPayload
	found := false
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == ridumigration.StepConcurrentIndex {
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("initial artifact has no concurrent index")
	}
	blocker, err := backend.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "LOCK TABLE "+quote(payload.Table)+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		_ = blocker.Rollback(ctx)
		t.Fatal(err)
	}
	err = backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{ConcurrentIndexTimeout: 100 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "concurrent-index step") {
		_ = blocker.Rollback(ctx)
		t.Fatalf("concurrent index timeout = %v", err)
	}
	assertArtifactNotFinal(t, ctx, backend, file.Name)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatalf("retry concurrent index after timeout: %v", err)
	}
}

func createInitialArtifact(t *testing.T, name string) (string, migrationartifact.File) {
	t.Helper()
	directory := t.TempDir()
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	artifact, err := BuildArtifact(context.Background(), name, nil, manifest, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	file, err := migrationartifact.Create(directory, name, artifact, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	return directory, file
}

func assertArtifactNotFinal(t *testing.T, ctx context.Context, backend *Store, artifactFileName string) {
	t.Helper()
	var final int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations WHERE name = $1`, artifactFileName).Scan(&final); err != nil || final != 0 {
		t.Fatalf("final migration rows = %d, %v", final, err)
	}
}

func assertSchemaAbsent(t *testing.T, ctx context.Context, backend *Store, artifact ridumigration.Artifact) {
	t.Helper()
	var migrations, steps, collection bool
	if err := backend.pool.QueryRow(ctx, `SELECT
to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL,
to_regclass(current_schema() || '.ridu_migration_steps') IS NOT NULL,
to_regclass(current_schema() || '.' || $1) IS NOT NULL`, collectionTable(artifact.After.Collections[0].ID)).Scan(&migrations, &steps, &collection); err != nil || migrations || steps || collection {
		t.Fatalf("timeout mutated schema migrations:%t steps:%t collection:%t, %v", migrations, steps, collection, err)
	}
}

func migrationArtifactTestBackend(t *testing.T) *Store {
	t.Helper()
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	schemaName := "ridu_artifact_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
		admin.Close()
	})
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	backend, err := OpenWithConfig(ctx, PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	return backend
}
