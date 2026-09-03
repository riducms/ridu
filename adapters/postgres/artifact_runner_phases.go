package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/jsonlimit"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/referenceindex"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	defaultAdvisoryLockWait       = 30 * time.Second
	defaultMigrationLockTimeout   = 10 * time.Second
	defaultMigrationStatementTime = 15 * time.Minute
	defaultMigrationBatchTimeout  = 30 * time.Second
	defaultConcurrentIndexTimeout = 30 * time.Minute
	defaultIdleTransactionTimeout = time.Minute
)

func normalizeRunnerOptions(options RunnerOptions) (RunnerOptions, error) {
	values := []struct {
		name  string
		value *time.Duration
		def   time.Duration
	}{
		{"advisory lock wait", &options.AdvisoryLockWait, defaultAdvisoryLockWait},
		{"lock timeout", &options.LockTimeout, defaultMigrationLockTimeout},
		{"statement timeout", &options.StatementTimeout, defaultMigrationStatementTime},
		{"batch timeout", &options.BatchTimeout, defaultMigrationBatchTimeout},
		{"concurrent index timeout", &options.ConcurrentIndexTimeout, defaultConcurrentIndexTimeout},
		{"idle-in-transaction timeout", &options.IdleInTransactionTimeout, defaultIdleTransactionTimeout},
	}
	for _, candidate := range values {
		if *candidate.value < 0 {
			return RunnerOptions{}, fmt.Errorf("migration %s must be positive", candidate.name)
		}
		if *candidate.value == 0 && !options.AllowUnbounded {
			*candidate.value = candidate.def
		}
	}
	if strings.TrimSpace(options.StopAfterPhase) != options.StopAfterPhase || strings.TrimSpace(options.StopAfterStep) != options.StopAfterStep {
		return RunnerOptions{}, fmt.Errorf("migration stop boundary cannot contain surrounding whitespace")
	}
	if options.StopAfterPhase != "" && options.StopAfterStep != "" {
		return RunnerOptions{}, fmt.Errorf("choose either a phase or a step stop boundary")
	}
	return options, nil
}

func acquireMigrationLock(ctx context.Context, connection *sql.Conn, wait time.Duration) error {
	lockContext := ctx
	cancel := func() {}
	if wait > 0 {
		lockContext, cancel = context.WithTimeout(ctx, wait)
	}
	defer cancel()
	for {
		var acquired bool
		if err := connection.QueryRowContext(lockContext, `SELECT pg_try_advisory_lock($1)`, migrationLockID).Scan(&acquired); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(lockContext.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("advisory lock wait exceeded %s", wait)
			}
			return err
		}
		if acquired {
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-lockContext.Done():
			timer.Stop()
			if errors.Is(lockContext.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("advisory lock wait exceeded %s", wait)
			}
			return lockContext.Err()
		case <-timer.C:
		}
	}
}

func setTransactionTimeouts(ctx context.Context, transaction *sql.Tx, options RunnerOptions) error {
	settings := []struct {
		name  string
		value time.Duration
	}{
		{"lock_timeout", options.LockTimeout},
		{"statement_timeout", options.StatementTimeout},
		{"idle_in_transaction_session_timeout", options.IdleInTransactionTimeout},
	}
	for _, setting := range settings {
		value := "0"
		if setting.value > 0 {
			value = strconv.FormatInt(setting.value.Milliseconds(), 10) + "ms"
		}
		if _, err := transaction.ExecContext(ctx, `SELECT set_config($1, $2, true)`, setting.name, value); err != nil {
			return fmt.Errorf("set migration %s: %w", setting.name, err)
		}
	}
	return nil
}

func ensureArtifactStepLedger(ctx context.Context, connection *sql.Conn) error {
	exists, err := artifactStepLedgerExists(ctx, connection)
	if err != nil {
		return err
	}
	if !exists {
		if _, err := connection.ExecContext(ctx, `CREATE TABLE ridu_migration_steps (
artifact_name text NOT NULL,
artifact_digest text NOT NULL,
phase_id text NOT NULL,
step_id text NOT NULL,
phase_mode text NOT NULL CHECK (phase_mode IN ('transaction', 'batch', 'no_transaction')),
state text NOT NULL CHECK (state IN ('running', 'complete')),
checkpoint jsonb NOT NULL DEFAULT '{}'::jsonb,
attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
started_at timestamptz NOT NULL DEFAULT now(),
updated_at timestamptz NOT NULL DEFAULT now(),
completed_at timestamptz,
PRIMARY KEY (artifact_name, phase_id, step_id)
		)`); err != nil {
			return fmt.Errorf("create migration step ledger: %w", err)
		}
	}
	return validateArtifactStepLedger(ctx, connection)
}

func artifactStepLedgerExists(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (bool, error) {
	var exists bool
	err := queryer.QueryRowContext(ctx, `SELECT to_regclass(current_schema() || '.ridu_migration_steps') IS NOT NULL`).Scan(&exists)
	return exists, err
}

func validateArtifactStepLedger(ctx context.Context, connection *sql.Conn) error {
	rows, err := connection.QueryContext(ctx, `SELECT column_name FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'ridu_migration_steps'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return err
		}
		columns[column] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, required := range []string{
		"artifact_name", "artifact_digest", "phase_id", "step_id", "phase_mode", "state", "checkpoint",
		"attempts", "started_at", "updated_at", "completed_at",
	} {
		if !columns[required] {
			return fmt.Errorf("migration step ledger is malformed: required column %s is missing", required)
		}
	}
	return nil
}

type artifactStepLedgerRow struct {
	Digest     string
	PhaseID    string
	StepID     string
	Mode       ridumigration.PhaseMode
	State      string
	Checkpoint json.RawMessage
	Attempts   int
}

func readArtifactStepLedger(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, file migrationartifact.File) (map[string]artifactStepLedgerRow, error) {
	expected := make(map[string]ridumigration.PhaseMode)
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			expected[phase.ID+"\x00"+step.ID] = phase.Mode
		}
	}
	rows, err := queryer.QueryContext(ctx, `SELECT artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts
FROM ridu_migration_steps WHERE artifact_name = $1 ORDER BY phase_id, step_id`, file.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]artifactStepLedgerRow)
	for rows.Next() {
		var row artifactStepLedgerRow
		if err := rows.Scan(&row.Digest, &row.PhaseID, &row.StepID, &row.Mode, &row.State, &row.Checkpoint, &row.Attempts); err != nil {
			return nil, err
		}
		if row.Digest != file.Digest {
			return nil, fmt.Errorf("migration %s changed after phased execution began", file.Name)
		}
		key := row.PhaseID + "\x00" + row.StepID
		mode, ok := expected[key]
		if !ok {
			return nil, fmt.Errorf("migration step ledger contains unknown step %s/%s for %s", row.PhaseID, row.StepID, file.Name)
		}
		if row.Mode != mode {
			return nil, fmt.Errorf("migration step ledger mode for %s/%s in %s is %q, want %q", row.PhaseID, row.StepID, file.Name, row.Mode, mode)
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("migration step ledger contains duplicate step %s/%s for %s", row.PhaseID, row.StepID, file.Name)
		}
		result[key] = row
	}
	return result, rows.Err()
}

func validateArtifactStepHistory(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, files []migrationartifact.File) error {
	expected := make(map[string]struct {
		digest string
		mode   ridumigration.PhaseMode
	})
	for _, file := range files {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				expected[file.Name+"\x00"+phase.ID+"\x00"+step.ID] = struct {
					digest string
					mode   ridumigration.PhaseMode
				}{file.Digest, phase.Mode}
			}
		}
	}
	rows, err := queryer.QueryContext(ctx, `SELECT artifact_name, artifact_digest, phase_id, step_id, phase_mode
FROM ridu_migration_steps ORDER BY artifact_name, phase_id, step_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var artifact, digest, phase, step string
		var mode ridumigration.PhaseMode
		if err := rows.Scan(&artifact, &digest, &phase, &step, &mode); err != nil {
			return err
		}
		want, ok := expected[artifact+"\x00"+phase+"\x00"+step]
		if !ok {
			return fmt.Errorf("migration step ledger contains unknown artifact or step %s/%s/%s", artifact, phase, step)
		}
		if digest != want.digest {
			return fmt.Errorf("migration %s changed after phased execution began", artifact)
		}
		if mode != want.mode {
			return fmt.Errorf("migration step ledger mode for %s/%s/%s is %q, want %q", artifact, phase, step, mode, want.mode)
		}
	}
	return rows.Err()
}

func validateCompletedArtifactSteps(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, files []migrationartifact.File, applied int, stepLedgerExists bool) error {
	for index := 0; index < applied; index++ {
		file := files[index]
		if !stepLedgerExists {
			return fmt.Errorf("migration %s is complete in the artifact ledger but the phase/step ledger is missing", file.Name)
		}
		ledger, err := readArtifactStepLedger(ctx, queryer, file)
		if err != nil {
			return err
		}
		expected := 0
		for _, phase := range file.Artifact.Phases {
			expected += len(phase.Steps)
			for _, step := range phase.Steps {
				if ledger[phase.ID+"\x00"+step.ID].State != "complete" {
					return fmt.Errorf("migration %s is complete in the artifact ledger but step %s/%s is not complete", file.Name, phase.ID, step.ID)
				}
			}
		}
		if len(ledger) != expected {
			return fmt.Errorf("migration %s is complete in the artifact ledger but has %d of %d phase/step rows", file.Name, len(ledger), expected)
		}
	}
	return nil
}

func validateStopBoundary(files []migrationartifact.File, options RunnerOptions) error {
	if options.StopAfterPhase == "" && options.StopAfterStep == "" {
		return nil
	}
	matches := 0
	for _, file := range files {
		for _, phase := range file.Artifact.Phases {
			if phase.ID == options.StopAfterPhase || file.Artifact.Name+"/"+phase.ID == options.StopAfterPhase || file.Name+"/"+phase.ID == options.StopAfterPhase {
				matches++
			}
			for stepIndex, step := range phase.Steps {
				if step.ID != options.StopAfterStep && file.Artifact.Name+"/"+step.ID != options.StopAfterStep && file.Name+"/"+step.ID != options.StopAfterStep {
					continue
				}
				if phase.Mode == ridumigration.PhaseTransaction && stepIndex != len(phase.Steps)-1 {
					return fmt.Errorf("stop step %s is inside atomic transaction phase %s; choose its final step or phase", step.ID, phase.ID)
				}
				matches++
			}
		}
	}
	if matches == 0 {
		return fmt.Errorf("migration stop boundary was not found in pending history")
	}
	if matches > 1 {
		return fmt.Errorf("migration stop boundary is ambiguous; qualify it as artifact-name/boundary-id")
	}
	return nil
}

func applyArtifact(ctx context.Context, connection *sql.Conn, file migrationartifact.File, options RunnerOptions, registry postgresDataTransformRegistry) (bool, error) {
	rows, err := readArtifactStepLedger(ctx, connection, file)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		transaction, err := connection.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return false, err
		}
		if err := setTransactionTimeouts(ctx, transaction, options); err != nil {
			_ = transaction.Rollback()
			return false, err
		}
		if err := assertArtifactPrecondition(ctx, transaction, file); err != nil {
			_ = transaction.Rollback()
			return false, err
		}
		if err := transaction.Rollback(); err != nil {
			return false, err
		}
	}
	for _, phase := range file.Artifact.Phases {
		switch phase.Mode {
		case ridumigration.PhaseTransaction:
			if err := applyTransactionPhase(ctx, connection, file, phase, rows, options, registry); err != nil {
				return false, err
			}
		case ridumigration.PhaseBatch:
			if err := applyBatchPhase(ctx, connection, file, phase, rows, options); err != nil {
				return false, err
			}
		case ridumigration.PhaseNoTransaction:
			if err := applyNoTransactionPhase(ctx, connection, file, phase, rows, options); err != nil {
				return false, err
			}
		default:
			return false, fmt.Errorf("migration %s phase %s has unsupported mode %q", file.Name, phase.ID, phase.Mode)
		}
		for _, step := range phase.Steps {
			rows[phase.ID+"\x00"+step.ID] = artifactStepLedgerRow{Digest: file.Digest, PhaseID: phase.ID, StepID: step.ID, Mode: phase.Mode, State: "complete"}
		}
		if stopAtPhase(file, phase, options) {
			return false, nil
		}
	}
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return false, err
	}
	if err := setTransactionTimeouts(ctx, transaction, options); err != nil {
		_ = transaction.Rollback()
		return false, err
	}
	var completed int
	if err := transaction.QueryRowContext(ctx, `SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1 AND artifact_digest = $2 AND state = 'complete'`, file.Name, file.Digest).Scan(&completed); err != nil {
		_ = transaction.Rollback()
		return false, err
	}
	expected := 0
	for _, phase := range file.Artifact.Phases {
		expected += len(phase.Steps)
	}
	if completed != expected {
		_ = transaction.Rollback()
		return false, fmt.Errorf("migration %s completed %d of %d phased steps", file.Name, completed, expected)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migrations (name, artifact_digest, from_digest, to_digest, planner_name, planner_version) VALUES ($1, $2, $3, $4, $5, $6)`, file.Name, file.Digest, file.Artifact.FromDigest, file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version); err != nil {
		_ = transaction.Rollback()
		return false, err
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", file.Name, err)
	}
	return true, nil
}

func assertArtifactPrecondition(ctx context.Context, transaction *sql.Tx, file migrationartifact.File) error {
	if file.Artifact.Before == nil {
		if err := assertEmptyPhysicalSchema(ctx, transaction); err != nil {
			return fmt.Errorf("migration %s precondition: %w", file.Name, err)
		}
		return nil
	}
	before, err := file.Artifact.BeforeManifest()
	if err != nil {
		return err
	}
	if err := assertMigrationPhysicalSchemaForContract(ctx, transaction, before, postgresArtifactSourceContract(file.Artifact)); err != nil {
		return fmt.Errorf("migration %s precondition: %w", file.Name, err)
	}
	return nil
}

func applyTransactionPhase(ctx context.Context, connection *sql.Conn, file migrationartifact.File, phase ridumigration.Phase, ledger map[string]artifactStepLedgerRow, options RunnerOptions, registry postgresDataTransformRegistry) error {
	completed := 0
	for _, step := range phase.Steps {
		if ledger[phase.ID+"\x00"+step.ID].State == "complete" {
			completed++
		}
	}
	if completed == len(phase.Steps) {
		return nil
	}
	if completed != 0 {
		return fmt.Errorf("migration %s transaction phase %s has a partial ledger", file.Name, phase.ID)
	}
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := setTransactionTimeouts(ctx, transaction, options); err != nil {
		return err
	}
	continuity, err := captureCollectionRenameContinuity(ctx, transaction, file.Artifact, phase.ID)
	if err != nil {
		return err
	}
	for _, step := range phase.Steps {
		if err := executeTransactionStep(ctx, connection, transaction, file, step, registry); err != nil {
			return fmt.Errorf("migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_migration_steps
(artifact_name, artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts, completed_at)
VALUES ($1, $2, $3, $4, $5, 'complete', '{}'::jsonb, 1, now())`, file.Name, file.Digest, phase.ID, step.ID, phase.Mode); err != nil {
			return err
		}
	}
	if err := assertCollectionRenameContinuity(ctx, transaction, continuity); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration %s phase %s: %w", file.Name, phase.ID, err)
	}
	return nil
}

type collectionRenameBinding struct {
	beforeTable string
	afterTable  string
	phaseIndex  int
}

type tableContinuitySignature struct {
	oid       int64
	documents int64
	idsDigest string
}

type collectionRenameGuard struct {
	startTable string
	endTable   string
	start      tableContinuitySignature
}

func captureCollectionRenameContinuity(ctx context.Context, transaction *sql.Tx, artifact ridumigration.Artifact, phaseID string) ([]collectionRenameGuard, error) {
	phaseIndex := -1
	for index, phase := range artifact.Phases {
		if phase.ID == phaseID {
			phaseIndex = index
			break
		}
	}
	if phaseIndex < 0 {
		return nil, fmt.Errorf("transaction phase %s is absent from its artifact", phaseID)
	}
	bindings, err := collectionRenameBindings(artifact)
	if err != nil {
		return nil, err
	}
	sort.Slice(bindings, func(left, right int) bool { return bindings[left].beforeTable < bindings[right].beforeTable })
	guards := make([]collectionRenameGuard, 0, len(bindings))
	for _, binding := range bindings {
		startTable := binding.beforeTable
		if phaseIndex > binding.phaseIndex {
			startTable = binding.afterTable
		}
		endTable := binding.beforeTable
		if phaseIndex >= binding.phaseIndex {
			endTable = binding.afterTable
		}
		if _, err := transaction.ExecContext(ctx, "LOCK TABLE "+quote(startTable)+" IN SHARE MODE"); err != nil {
			return nil, fmt.Errorf("lock collection-rename continuity table %s: %w", startTable, err)
		}
		signature, err := readTableContinuitySignature(ctx, transaction, startTable)
		if err != nil {
			return nil, fmt.Errorf("read collection-rename continuity table %s: %w", startTable, err)
		}
		guards = append(guards, collectionRenameGuard{startTable: startTable, endTable: endTable, start: signature})
	}
	return guards, nil
}

func assertCollectionRenameContinuity(ctx context.Context, transaction *sql.Tx, guards []collectionRenameGuard) error {
	for _, guard := range guards {
		end, err := readTableContinuitySignature(ctx, transaction, guard.endTable)
		if err != nil {
			return fmt.Errorf("read collection-rename continuity table %s: %w", guard.endTable, err)
		}
		if end != guard.start {
			return fmt.Errorf("collection rename did not preserve PostgreSQL relation identity and exact document IDs from %s to %s", guard.startTable, guard.endTable)
		}
	}
	return nil
}

func collectionRenameBindings(artifact ridumigration.Artifact) ([]collectionRenameBinding, error) {
	if artifact.Before == nil {
		return nil, nil
	}
	var bindings []collectionRenameBinding
	for phaseIndex, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			var payload ridumigration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode collection rename continuity: %w", err)
			}
			if payload.Rename.FieldBefore != "" || payload.Rename.FieldAfter != "" {
				continue
			}
			before, beforeFound := collectionBySlug(artifact.Before.Collections, payload.Rename.CollectionBefore)
			after, afterFound := collectionBySlug(artifact.After.Collections, payload.Rename.CollectionAfter)
			if !beforeFound || !afterFound {
				return nil, fmt.Errorf("collection rename continuity addresses absent collection %q -> %q", payload.Rename.CollectionBefore, payload.Rename.CollectionAfter)
			}
			bindings = append(bindings, collectionRenameBinding{
				beforeTable: collectionTable(before.ID),
				afterTable:  collectionTable(after.ID),
				phaseIndex:  phaseIndex,
			})
		}
	}
	return bindings, nil
}

func readTableContinuitySignature(ctx context.Context, transaction *sql.Tx, table string) (tableContinuitySignature, error) {
	var signature tableContinuitySignature
	if err := transaction.QueryRowContext(ctx, `SELECT c.oid::bigint
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = current_schema() AND c.relname = $1 AND c.relkind = 'r'`, table).Scan(&signature.oid); err != nil {
		return tableContinuitySignature{}, err
	}
	rows, err := transaction.QueryContext(ctx, "SELECT id FROM "+quote(table)+" ORDER BY id")
	if err != nil {
		return tableContinuitySignature{}, err
	}
	hash := sha256.New()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return tableContinuitySignature{}, err
		}
		signature.documents++
		_, _ = hash.Write([]byte(id))
		_, _ = hash.Write([]byte{0})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return tableContinuitySignature{}, err
	}
	if err := rows.Close(); err != nil {
		return tableContinuitySignature{}, err
	}
	signature.idsDigest = hex.EncodeToString(hash.Sum(nil))
	return signature, nil
}

func executeTransactionStep(ctx context.Context, connection *sql.Conn, transaction *sql.Tx, file migrationartifact.File, step ridumigration.Step, registry postgresDataTransformRegistry) error {
	switch step.Kind {
	case ridumigration.StepSQL:
		var payload ridumigration.SQLPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		_, err := transaction.ExecContext(ctx, payload.SQL)
		return err
	case ridumigration.StepRenameContent:
		var payload ridumigration.RenamePayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		return applyContentRename(ctx, transaction, file.Artifact, payload.Rename)
	case ridumigration.StepPluginSQL:
		var payload ridumigration.PluginPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		if payload.Plugin.Adapter != schema.PluginDatabaseAdapterPostgres || payload.Plugin.Checksum != ridumigration.PluginStepChecksum(payload.Plugin.Adapter, payload.Plugin.Plugin, payload.Plugin.Version, payload.Plugin.Direction, payload.Plugin.SQL) {
			return fmt.Errorf("plugin checksum mismatch")
		}
		for _, statement := range payload.Plugin.SQL {
			if _, err := transaction.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	case ridumigration.StepRetireResources:
		var payload ridumigration.RetireResourcesPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		return retireFrameworkResourceState(ctx, transaction, payload.ResourceIDs, payload.PurgeVersionOwnerIDs)
	case ridumigration.StepCanonicalizeAuthIdentities:
		var payload ridumigration.CanonicalizeAuthIdentitiesPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		after, err := file.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		before, err := file.Artifact.BeforeManifest()
		if err != nil {
			return err
		}
		return canonicalizePostgresAuthIdentities(ctx, transaction, before, after, payload.Resources)
	case ridumigration.StepDataTransform:
		var payload ridumigration.DataTransformPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		transform, exists := registry[payload.Transform.Name]
		if !exists || transform.Checksum != payload.Transform.Checksum {
			return fmt.Errorf("compiled PostgreSQL data transform %q does not match the immutable artifact", payload.Transform.Name)
		}
		return executePostgresDataTransform(ctx, connection, file.Artifact, transform)
	case ridumigration.StepAssertSchema:
		after, err := file.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		return assertMigrationPhysicalSchemaForContract(ctx, transaction, after, postgresArtifactTargetContract(file.Artifact))
	default:
		return fmt.Errorf("unsupported transaction executor %q", step.Kind)
	}
}

type resourceRetirementStatement struct {
	table string
	query string
}

var frameworkResourceRetirementStatements = []resourceRetirementStatement{
	{
		table: "ridu_document_references",
		query: `DELETE FROM ridu_document_references
WHERE owner_collection_id = $1 OR target_collection_id = $1`,
	},
	{
		table: "ridu_tasks",
		query: `DELETE FROM ridu_tasks
WHERE target_collection_id = $1 OR requested_by_collection_id = $1
   OR (task_slug = 'ridu-schedule-publish' AND (
       input->>'collectionID' = $1 OR input->>'requestedByCollectionID' = $1
   ))`,
	},
	{
		table: "ridu_document_locks",
		query: `DELETE FROM ridu_document_locks
WHERE collection_id = $1 OR owner_collection_id = $1`,
	},
	{table: "ridu_versions", query: `DELETE FROM ridu_versions WHERE collection_id = $1`},
	{table: "ridu_preferences", query: `DELETE FROM ridu_preferences WHERE collection_id = $1`},
	{table: "ridu_auth_tokens", query: `DELETE FROM ridu_auth_tokens WHERE collection_id = $1`},
	{table: "ridu_auth_sessions", query: `DELETE FROM ridu_auth_sessions WHERE collection_id = $1`},
	{table: "ridu_auth_api_keys", query: `DELETE FROM ridu_auth_api_keys WHERE collection_id = $1`},
	{table: "ridu_auth_credentials", query: `DELETE FROM ridu_auth_credentials WHERE collection_id = $1`},
}

func retireFrameworkResourceState(ctx context.Context, transaction *sql.Tx, resourceIDs, purgeVersionOwnerIDs []schema.StableID) error {
	for _, statement := range frameworkResourceRetirementStatements {
		exists, err := transactionTableExists(ctx, transaction, statement.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		for _, resourceID := range resourceIDs {
			if _, err := transaction.ExecContext(ctx, statement.query, string(resourceID)); err != nil {
				return fmt.Errorf("retire %s state for resource %s: %w", statement.table, resourceID, err)
			}
		}
	}
	if len(purgeVersionOwnerIDs) != 0 {
		exists, err := transactionTableExists(ctx, transaction, "ridu_versions")
		if err != nil {
			return err
		}
		if exists {
			for _, ownerID := range purgeVersionOwnerIDs {
				if _, err := transaction.ExecContext(ctx, `DELETE FROM ridu_versions WHERE collection_id = $1`, string(ownerID)); err != nil {
					return fmt.Errorf("purge reference-bearing version history for surviving resource %s: %w", ownerID, err)
				}
			}
		}
	}
	// ridu_auth_rate_limits intentionally has no collection or resource column:
	// it contains short-lived irreversible hashes only. Clearing it globally
	// would weaken throttling for unrelated auth collections, while retaining a
	// bucket cannot authenticate or recover data for a reincarnated resource.
	return nil
}

func applyBatchPhase(ctx context.Context, connection *sql.Conn, file migrationartifact.File, phase ridumigration.Phase, ledger map[string]artifactStepLedgerRow, options RunnerOptions) error {
	step := phase.Steps[0]
	row := ledger[phase.ID+"\x00"+step.ID]
	if row.State == "complete" {
		return nil
	}
	switch step.Kind {
	case ridumigration.StepBackfillReferences:
		var payload ridumigration.BackfillReferencesPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			return err
		}
		return applyReferenceBackfillBatches(ctx, connection, file, phase, step, row, int(payload.BatchSize), options)
	default:
		return fmt.Errorf("unsupported batch executor %q", step.Kind)
	}
}

func scheduledPublishConcurrencyKey(collectionID, documentID string) string {
	key := collectionID + ":" + documentID
	if len(key) <= store.MaxTaskConcurrencyKeyBytes {
		return key
	}
	digest := sha256.Sum256([]byte(key))
	return "sha256-" + hex.EncodeToString(digest[:])
}

type referenceBackfillCheckpoint struct {
	Initialized bool   `json:"initialized"`
	Resource    int    `json:"resource"`
	LastID      string `json:"lastID,omitempty"`
}

func applyReferenceBackfillBatches(ctx context.Context, connection *sql.Conn, file migrationartifact.File, phase ridumigration.Phase, step ridumigration.Step, row artifactStepLedgerRow, batchSize int, options RunnerOptions) error {
	manifest, err := file.Artifact.AfterManifest()
	if err != nil {
		return err
	}
	snapshot := manifest.Snapshot()
	resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
	checkpoint := referenceBackfillCheckpoint{}
	if len(row.Checkpoint) != 0 {
		if err := decodeReferenceBackfillCheckpoint(row.Checkpoint, &checkpoint); err != nil {
			return fmt.Errorf("decode reference checkpoint: %w", err)
		}
	}
	if err := validateReferenceBackfillCheckpoint(checkpoint, len(resources)); err != nil {
		return fmt.Errorf("migration %s step %s has invalid reference checkpoint: %w", file.Name, step.ID, err)
	}
	for {
		batchContext, cancel := boundedContext(ctx, options.BatchTimeout)
		transaction, err := connection.BeginTx(batchContext, &sql.TxOptions{})
		if err != nil {
			cancel()
			return err
		}
		if err := setTransactionTimeouts(batchContext, transaction, options); err != nil {
			_ = transaction.Rollback()
			cancel()
			return err
		}
		completed, next, err := referenceBackfillBatch(batchContext, transaction, manifest, resources, checkpoint, batchSize)
		var committedCheckpoint json.RawMessage
		if err == nil {
			encoded, encodeErr := json.Marshal(next)
			if encodeErr != nil {
				err = encodeErr
			} else {
				committedCheckpoint = encoded
				state := "running"
				var completedAt any
				if completed {
					state, completedAt = "complete", time.Now().UTC()
				}
				_, err = transaction.ExecContext(batchContext, `INSERT INTO ridu_migration_steps
(artifact_name, artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8)
ON CONFLICT (artifact_name, phase_id, step_id) DO UPDATE SET
state = EXCLUDED.state, checkpoint = EXCLUDED.checkpoint, attempts = ridu_migration_steps.attempts + 1,
updated_at = now(), completed_at = EXCLUDED.completed_at
WHERE ridu_migration_steps.artifact_digest = EXCLUDED.artifact_digest`, file.Name, file.Digest, phase.ID, step.ID, phase.Mode, state, encoded, completedAt)
			}
		}
		if err != nil {
			_ = transaction.Rollback()
			cancel()
			return err
		}
		if err := transaction.Commit(); err != nil {
			cancel()
			return err
		}
		cancel()
		if completed {
			return nil
		}
		if options.afterBatch != nil {
			if err := options.afterBatch(file.Name, phase.ID, step.ID, committedCheckpoint); err != nil {
				return err
			}
		}
		if !referenceCheckpointAdvanced(checkpoint, next) {
			return fmt.Errorf("migration %s step %s checkpoint did not advance", file.Name, step.ID)
		}
		checkpoint = next
	}
}

func decodeReferenceBackfillCheckpoint(encoded json.RawMessage, checkpoint *referenceBackfillCheckpoint) error {
	if err := jsonlimit.Validate(encoded, 2, 16); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(checkpoint); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("checkpoint contains trailing JSON")
	}
	return nil
}

func validateReferenceBackfillCheckpoint(checkpoint referenceBackfillCheckpoint, resources int) error {
	if resources < 0 {
		return fmt.Errorf("resource count must not be negative")
	}
	if !checkpoint.Initialized {
		if checkpoint.Resource != 0 || checkpoint.LastID != "" {
			return fmt.Errorf("uninitialized checkpoint contains progress")
		}
		return nil
	}
	if checkpoint.Resource < 0 || checkpoint.Resource > resources {
		return fmt.Errorf("resource index %d is outside 0..%d", checkpoint.Resource, resources)
	}
	if checkpoint.Resource == resources && checkpoint.LastID != "" {
		return fmt.Errorf("completed checkpoint retains a document cursor")
	}
	return nil
}

func referenceBackfillBatch(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest, resources []schema.Collection, checkpoint referenceBackfillCheckpoint, batchSize int) (bool, referenceBackfillCheckpoint, error) {
	if !checkpoint.Initialized {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM ridu_document_references`); err != nil {
			return false, checkpoint, err
		}
		checkpoint.Initialized = true
		return len(resources) == 0, checkpoint, nil
	}
	if checkpoint.Resource >= len(resources) {
		return true, checkpoint, nil
	}
	resource := resources[checkpoint.Resource]
	fields := storedSchemaFields(resource.Fields)
	var locales []schema.LocaleCode
	if localization := manifest.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s > $1 ORDER BY %s LIMIT $2 FOR UPDATE", selectColumns(resource, fields, locales), quote(collectionTable(resource.ID)), quote("id"), quote("id"))
	rows, err := transaction.QueryContext(ctx, statement, checkpoint.LastID, batchSize)
	if err != nil {
		return false, checkpoint, err
	}
	documents := make([]schemaDocument, 0, batchSize)
	for rows.Next() {
		document, err := scanDocument(rows, resource, fields)
		if err != nil {
			rows.Close()
			return false, checkpoint, err
		}
		documents = append(documents, schemaDocument{ID: document.ID, Entries: referenceindex.Collect(resource, document)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, checkpoint, err
	}
	rows.Close()
	if len(documents) == 0 {
		checkpoint.Resource++
		checkpoint.LastID = ""
		return checkpoint.Resource >= len(resources), checkpoint, nil
	}
	for _, document := range documents {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM ridu_document_references WHERE owner_collection_id = $1 AND owner_document_id = $2`, resource.ID, document.ID); err != nil {
			return false, checkpoint, err
		}
		for _, entry := range document.Entries {
			if _, err := transaction.ExecContext(ctx, `INSERT INTO ridu_document_references (
owner_collection_id, owner_document_id, field_id, target_collection_id, target_document_id, locale, occurrence
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, entry.Owner.CollectionID, entry.Owner.DocumentID, entry.FieldID, entry.Target.CollectionID, entry.Target.DocumentID, entry.Locale, entry.Occurrence); err != nil {
				return false, checkpoint, err
			}
		}
	}
	checkpoint.LastID = documents[len(documents)-1].ID
	return false, checkpoint, nil
}

type schemaDocument struct {
	ID      string
	Entries []referenceindex.Entry
}

func referenceCheckpointAdvanced(before, after referenceBackfillCheckpoint) bool {
	if !before.Initialized && after.Initialized {
		return true
	}
	return after.Resource > before.Resource || after.Resource == before.Resource && after.LastID > before.LastID
}

func applyNoTransactionPhase(ctx context.Context, connection *sql.Conn, file migrationartifact.File, phase ridumigration.Phase, ledger map[string]artifactStepLedgerRow, options RunnerOptions) error {
	step := phase.Steps[0]
	if ledger[phase.ID+"\x00"+step.ID].State == "complete" {
		return nil
	}
	if step.Kind != ridumigration.StepConcurrentIndex {
		return fmt.Errorf("migration %s no-transaction step %s has unsupported executor %q", file.Name, step.ID, step.Kind)
	}
	var payload ridumigration.ConcurrentIndexPayload
	if err := json.Unmarshal(step.Payload, &payload); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO ridu_migration_steps
(artifact_name, artifact_digest, phase_id, step_id, phase_mode, state, checkpoint, attempts)
VALUES ($1, $2, $3, $4, $5, 'running', '{}'::jsonb, 1)
ON CONFLICT (artifact_name, phase_id, step_id) DO UPDATE SET
state = 'running', attempts = ridu_migration_steps.attempts + 1, updated_at = now(), completed_at = NULL
WHERE ridu_migration_steps.artifact_digest = EXCLUDED.artifact_digest AND ridu_migration_steps.state <> 'complete'`, file.Name, file.Digest, phase.ID, step.ID, phase.Mode); err != nil {
		return err
	}
	commandContext, cancel := boundedContext(ctx, options.ConcurrentIndexTimeout)
	defer cancel()
	if err := setConnectionTimeout(commandContext, connection, "lock_timeout", options.LockTimeout); err != nil {
		return err
	}
	defer setConnectionTimeout(context.Background(), connection, "lock_timeout", 0)
	if err := setConnectionTimeout(commandContext, connection, "statement_timeout", options.ConcurrentIndexTimeout); err != nil {
		return err
	}
	defer setConnectionTimeout(context.Background(), connection, "statement_timeout", 0)
	if err := reconcileConcurrentIndex(commandContext, connection, payload); err != nil {
		return fmt.Errorf("migration %s concurrent-index step %s: %w", file.Name, step.ID, err)
	}
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := setTransactionTimeouts(ctx, transaction, options); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE ridu_migration_steps SET state = 'complete', checkpoint = '{}'::jsonb, updated_at = now(), completed_at = now()
WHERE artifact_name = $1 AND artifact_digest = $2 AND phase_id = $3 AND step_id = $4 AND state = 'running'`, file.Name, file.Digest, phase.ID, step.ID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("migration %s step %s ledger completion was not admitted", file.Name, step.ID)
	}
	return transaction.Commit()
}

func setConnectionTimeout(ctx context.Context, connection *sql.Conn, name string, duration time.Duration) error {
	value := "0"
	if duration > 0 {
		value = strconv.FormatInt(duration.Milliseconds(), 10) + "ms"
	}
	_, err := connection.ExecContext(ctx, `SELECT set_config($1, $2, false)`, name, value)
	return err
}

type concurrentIndexCatalog struct {
	Exists    bool
	Valid     bool
	Table     string
	Unique    bool
	Method    string
	Parts     []string
	Predicate sql.NullString
}

func inspectConcurrentIndex(ctx context.Context, connection *sql.Conn, name string) (concurrentIndexCatalog, error) {
	var result concurrentIndexCatalog
	var encodedParts []byte
	err := connection.QueryRowContext(ctx, `SELECT i.indisvalid, table_class.relname, i.indisunique, access_method.amname,
COALESCE((SELECT jsonb_agg(pg_get_indexdef(i.indexrelid, position, false) ORDER BY position)
          FROM generate_series(1, i.indnkeyatts) AS position), '[]'::jsonb),
pg_get_expr(i.indpred, i.indrelid)
FROM pg_class index_class
JOIN pg_namespace namespace ON namespace.oid = index_class.relnamespace
JOIN pg_index i ON i.indexrelid = index_class.oid
JOIN pg_class table_class ON table_class.oid = i.indrelid
JOIN pg_am access_method ON access_method.oid = index_class.relam
WHERE namespace.nspname = current_schema() AND index_class.relname = $1`, name).Scan(
		&result.Valid, &result.Table, &result.Unique, &result.Method, &encodedParts, &result.Predicate,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(encodedParts, &result.Parts); err != nil {
		return result, err
	}
	result.Exists = true
	return result, nil
}

func reconcileConcurrentIndex(ctx context.Context, connection *sql.Conn, payload ridumigration.ConcurrentIndexPayload) error {
	actual, err := inspectConcurrentIndex(ctx, connection, payload.Name)
	if err != nil {
		return err
	}
	switch payload.Action {
	case ridumigration.ConcurrentIndexDrop:
		if !actual.Exists {
			return nil
		}
		_, err := connection.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+quote(payload.Name))
		return err
	case ridumigration.ConcurrentIndexCreate:
		if actual.Exists && actual.Valid {
			if !concurrentIndexDefinitionMatches(payload, actual) {
				return fmt.Errorf("valid index %s already exists with a different definition", payload.Name)
			}
			return nil
		}
		if actual.Exists {
			if _, err := connection.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+quote(payload.Name)); err != nil {
				return fmt.Errorf("remove invalid index %s: %w", payload.Name, err)
			}
		}
		command := "CREATE "
		if payload.Unique {
			command += "UNIQUE "
		}
		command += "INDEX CONCURRENTLY " + quote(payload.Name) + " ON " + quote(payload.Table)
		if payload.Method != "" {
			command += " USING " + payload.Method
		}
		command += " (" + strings.Join(payload.Parts, ", ") + ")"
		if payload.Predicate != "" {
			command += " WHERE " + payload.Predicate
		}
		if _, err := connection.ExecContext(ctx, command); err != nil {
			return err
		}
		verified, err := inspectConcurrentIndex(ctx, connection, payload.Name)
		if err != nil {
			return err
		}
		if !verified.Exists || !verified.Valid || !concurrentIndexDefinitionMatches(payload, verified) {
			return fmt.Errorf("concurrent index %s did not reach its reviewed valid definition", payload.Name)
		}
		return nil
	default:
		return fmt.Errorf("unsupported concurrent index action %q", payload.Action)
	}
}

func concurrentIndexDefinitionMatches(expected ridumigration.ConcurrentIndexPayload, actual concurrentIndexCatalog) bool {
	expectedMethod := strings.ToLower(expected.Method)
	if expectedMethod == "" {
		expectedMethod = "btree"
	}
	if actual.Table != expected.Table || actual.Unique != expected.Unique || strings.ToLower(actual.Method) != expectedMethod || len(actual.Parts) != len(expected.Parts) {
		return false
	}
	for index := range expected.Parts {
		if normalizeCatalogIndexSQL(actual.Parts[index]) != normalizeCatalogIndexSQL(expected.Parts[index]) {
			return false
		}
	}
	actualPredicate := ""
	if actual.Predicate.Valid {
		actualPredicate = actual.Predicate.String
	}
	return normalizeCatalogIndexSQL(actualPredicate) == normalizeCatalogIndexSQL(expected.Predicate)
}

func normalizeCatalogIndexSQL(value string) string {
	value = strings.ReplaceAll(value, `"`, "")
	value = strings.Join(strings.Fields(value), "")
	for len(value) >= 2 && value[0] == '(' && value[len(value)-1] == ')' && outerParenthesesWrap(value) {
		value = value[1 : len(value)-1]
	}
	return value
}

func outerParenthesesWrap(value string) bool {
	depth := 0
	quoted := false
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\'':
			if quoted && index+1 < len(value) && value[index+1] == '\'' {
				index++
				continue
			}
			quoted = !quoted
		case '(':
			if !quoted {
				depth++
			}
		case ')':
			if !quoted {
				depth--
				if depth == 0 && index != len(value)-1 {
					return false
				}
			}
		}
	}
	return depth == 0 && !quoted
}

func stopAtPhase(file migrationartifact.File, phase ridumigration.Phase, options RunnerOptions) bool {
	if options.StopAfterPhase == phase.ID || options.StopAfterPhase == file.Artifact.Name+"/"+phase.ID || options.StopAfterPhase == file.Name+"/"+phase.ID {
		return true
	}
	last := phase.Steps[len(phase.Steps)-1]
	return options.StopAfterStep == last.ID || options.StopAfterStep == file.Artifact.Name+"/"+last.ID || options.StopAfterStep == file.Name+"/"+last.ID
}

func boundedContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
