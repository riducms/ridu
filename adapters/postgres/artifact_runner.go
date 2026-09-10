package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const migrationLockID int64 = 795552720260

// RunnerOptions controls explicit operational admissions without changing the
// immutable artifact being replayed.
type RunnerOptions struct {
	// AllowInsecureDatabase explicitly permits a PostgreSQL connection that can
	// fall back to plaintext. It is intended only for local migration drills.
	AllowInsecureDatabase bool
	// AllowMaintenance admits traffic-sensitive semantic steps such as content
	// rewrites, compiled data transforms, resource retirement, and
	// reference-index rebuilds. Every old writer must be stopped while they run.
	// It does not relax artifact validation.
	AllowMaintenance bool
	// AllowUnbounded permits a zero operational timeout. Without this explicit
	// admission, zero selects the runner's bounded production default.
	AllowUnbounded bool
	// AdvisoryLockWait bounds how long this runner waits for another migrator.
	AdvisoryLockWait time.Duration
	// LockTimeout bounds PostgreSQL lock acquisition inside a phase.
	LockTimeout time.Duration
	// StatementTimeout bounds one transactional SQL statement.
	StatementTimeout time.Duration
	// BatchTimeout bounds the complete execution of one checkpoint batch.
	BatchTimeout time.Duration
	// ConcurrentIndexTimeout bounds one CREATE/DROP INDEX CONCURRENTLY command.
	ConcurrentIndexTimeout time.Duration
	// IdleInTransactionTimeout protects a paused transactional phase.
	IdleInTransactionTimeout time.Duration
	// StopAfterPhase and StopAfterStep are explicit, successfully committed
	// rollout boundaries. A transaction phase can stop only at its final step.
	StopAfterPhase string
	StopAfterStep  string
	// Notice receives non-blocking execution provenance after the complete
	// pending history has passed preflight.
	Notice func(MigrationNotice)

	// afterBatch is an internal deterministic interruption seam used to prove
	// that committed data and checkpoints resume together. Production callers
	// cannot configure it.
	afterBatch func(artifact, phase, step string, checkpoint json.RawMessage) error
}

// MigrationNotice is stable, machine-readable runner context.
type MigrationNotice struct {
	Code     string
	Artifact string
	Message  string
}

// NoticeAtlasProvenance identifies replay by an Atlas runner version other
// than the one that planned the frozen artifact SQL.
const NoticeAtlasProvenance = "RIDU_ATLAS_PROVENANCE"

// ErrMaintenanceRequired identifies pending whole-dataset work that requires
// an explicit maintenance window admission.
var ErrMaintenanceRequired = errors.New("migration maintenance admission is required")

// MaintenanceRequiredError lists artifacts refused before any schema or ledger
// mutation. SQL and content details intentionally remain in the artifact files.
type MaintenanceRequiredError struct {
	Artifacts []string
}

func (err *MaintenanceRequiredError) Error() string {
	return fmt.Sprintf("%v for a traffic-sensitive content rewrite, compiled data transform, resource retirement, or reference-index rebuild in %s; stop every application process, writer, and worker through completion and every retry, then retry with explicit maintenance admission", ErrMaintenanceRequired, strings.Join(err.Artifacts, ", "))
}

func (err *MaintenanceRequiredError) Is(target error) bool {
	return target == ErrMaintenanceRequired
}

// VerifyArtifacts replays the complete history in a uniquely named temporary
// PostgreSQL schema, asserts every intermediate state, and always drops the
// isolated schema before returning. It never touches application tables.
func VerifyArtifacts(ctx context.Context, databaseURL, directory string) (resultError error) {
	return VerifyArtifactsWithOptions(ctx, databaseURL, directory, RunnerOptions{})
}

// VerifyArtifactsWithOptions replays history with the same operational
// admissions as ApplyArtifactsWithOptions.
func VerifyArtifactsWithOptions(ctx context.Context, databaseURL, directory string, options RunnerOptions) (resultError error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	return verifyPostgresArtifactFiles(ctx, databaseURL, files, options, nil)
}

func verifyPostgresArtifactFiles(ctx context.Context, databaseURL string, files []migrationartifact.File, options RunnerOptions, registry postgresDataTransformRegistry) (resultError error) {
	var err error
	options, err = normalizeRunnerOptions(options)
	if err != nil {
		return err
	}
	if options.StopAfterPhase != "" || options.StopAfterStep != "" {
		return fmt.Errorf("migration verification requires a complete shadow replay and does not accept stop boundaries")
	}
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration before verification")
	}
	if err := requirePostgresDataTransformRegistry(files, registry); err != nil {
		return err
	}
	notices, err := preflightArtifactExecution(ctx, files, 0, options)
	if err != nil {
		return err
	}
	identifier := make([]byte, 12)
	if _, err := rand.Read(identifier); err != nil {
		return fmt.Errorf("create shadow schema identity: %w", err)
	}
	shadowSchema := "ridu_shadow_" + hex.EncodeToString(identifier)
	admin, err := OpenWithConfig(ctx, PoolConfig{DatabaseURL: databaseURL, AllowInsecureTransport: options.AllowInsecureDatabase, ApplicationName: "ridu-migration-verify"})
	if err != nil {
		return err
	}
	defer admin.Close()
	if _, err := admin.pool.Exec(ctx, "CREATE SCHEMA "+quote(shadowSchema)); err != nil {
		return fmt.Errorf("create shadow schema: %w", err)
	}
	defer func() {
		if _, err := admin.pool.Exec(context.Background(), "DROP SCHEMA "+quote(shadowSchema)+" CASCADE"); resultError == nil && err != nil {
			resultError = fmt.Errorf("drop shadow schema %s: %w", shadowSchema, err)
		}
	}()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse PostgreSQL URL: %w", err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", shadowSchema)
	parsed.RawQuery = parameters.Encode()
	shadow, err := OpenWithConfig(ctx, PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: options.AllowInsecureDatabase, ApplicationName: "ridu-migration-shadow"})
	if err != nil {
		return err
	}
	defer shadow.Close()
	shadowOptions := options
	shadowOptions.Notice = nil
	if err := shadow.applyArtifactFilesWithRegistry(ctx, files, shadowOptions, registry); err != nil {
		return fmt.Errorf("verify migrations in shadow schema: %w", err)
	}
	statuses, err := shadow.artifactStatusFiles(ctx, files)
	if err != nil {
		return fmt.Errorf("verify completed shadow migration state: %w", err)
	}
	if err := requireCompleteShadowReplay(files, statuses); err != nil {
		return fmt.Errorf("verify completed shadow migration state: %w", err)
	}
	emitMigrationNotices(options, notices)
	return nil
}

func requireCompleteShadowReplay(files []migrationartifact.File, statuses []MigrationStatus) error {
	if len(statuses) != len(files) {
		return fmt.Errorf("shadow replay reported %d of %d migration artifacts", len(statuses), len(files))
	}
	for index, file := range files {
		status := statuses[index]
		if status.Name != file.Name || status.Checksum != file.Digest || status.Version != file.Artifact.Version {
			return fmt.Errorf("shadow replay artifact %d identity is %s/%s/v%d, want %s/%s/v%d", index+1, status.Name, status.Checksum, status.Version, file.Name, file.Digest, file.Artifact.Version)
		}
		if !status.Applied {
			return fmt.Errorf("shadow replay left migration %s pending", file.Name)
		}
		if len(status.Phases) != len(file.Artifact.Phases) {
			return fmt.Errorf("shadow replay reported %d of %d phases for migration %s", len(status.Phases), len(file.Artifact.Phases), file.Name)
		}
		for phaseIndex, expectedPhase := range file.Artifact.Phases {
			phase := status.Phases[phaseIndex]
			if phase.ID != expectedPhase.ID || phase.Mode != expectedPhase.Mode {
				return fmt.Errorf("shadow replay migration %s phase %d identity is %s/%s, want %s/%s", file.Name, phaseIndex+1, phase.ID, phase.Mode, expectedPhase.ID, expectedPhase.Mode)
			}
			if phase.State != "complete" {
				return fmt.Errorf("shadow replay left migration %s phase %s in state %s", file.Name, phase.ID, phase.State)
			}
			if len(phase.Steps) != len(expectedPhase.Steps) {
				return fmt.Errorf("shadow replay reported %d of %d steps for migration %s phase %s", len(phase.Steps), len(expectedPhase.Steps), file.Name, phase.ID)
			}
			for stepIndex, expectedStep := range expectedPhase.Steps {
				step := phase.Steps[stepIndex]
				if step.ID != expectedStep.ID || step.Kind != expectedStep.Kind {
					return fmt.Errorf("shadow replay migration %s phase %s step %d identity is %s/%s, want %s/%s", file.Name, phase.ID, stepIndex+1, step.ID, step.Kind, expectedStep.ID, expectedStep.Kind)
				}
				if step.State != "complete" {
					return fmt.Errorf("shadow replay left migration %s step %s/%s in state %s", file.Name, phase.ID, step.ID, step.State)
				}
			}
		}
	}
	return nil
}

// ApplyArtifacts validates the complete immutable history and applies each
// pending artifact in its own transaction while holding a session advisory lock.
func (backend *Store) ApplyArtifacts(ctx context.Context, directory string) error {
	return backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{})
}

// ApplyArtifactsWithOptions validates the complete immutable history before
// mutating the migration ledger or application schema.
func (backend *Store) ApplyArtifactsWithOptions(ctx context.Context, directory string, options RunnerOptions) error {
	var err error
	options, err = normalizeRunnerOptions(options)
	if err != nil {
		return err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	return backend.applyArtifactFiles(ctx, files, options)
}

func (backend *Store) applyArtifactFiles(ctx context.Context, files []migrationartifact.File, options RunnerOptions) error {
	return backend.applyArtifactFilesWithRegistry(ctx, files, options, nil)
}

func (backend *Store) applyArtifactFilesWithRegistry(ctx context.Context, files []migrationartifact.File, options RunnerOptions, registry postgresDataTransformRegistry) error {
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration before apply")
	}
	if err := requirePostgresDataTransformRegistry(files, registry); err != nil {
		return err
	}
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := acquireMigrationLock(ctx, connection, options.AdvisoryLockWait); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer connection.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	exists, err := artifactLedgerExists(ctx, connection)
	if err != nil {
		return err
	}
	var applied []artifactLedgerRow
	if exists {
		if err := validateArtifactLedger(ctx, connection); err != nil {
			return err
		}
		applied, err = readArtifactLedger(ctx, connection)
		if err != nil {
			return err
		}
	}
	if err := validateAppliedArtifacts(files, applied); err != nil {
		return err
	}
	stepLedgerExists, err := artifactStepLedgerExists(ctx, connection)
	if err != nil {
		return err
	}
	if stepLedgerExists {
		if err := validateArtifactStepLedger(ctx, connection); err != nil {
			return err
		}
		if err := validateArtifactStepHistory(ctx, connection, files); err != nil {
			return err
		}
	}
	if err := validateCompletedArtifactSteps(ctx, connection, files, len(applied), stepLedgerExists); err != nil {
		return err
	}
	inProgress, err := artifactExecutionInProgress(ctx, connection, files, len(applied), stepLedgerExists)
	if err != nil {
		return err
	}
	notices, err := preflightArtifactExecution(ctx, files, len(applied), options)
	if err != nil {
		return err
	}
	if !inProgress {
		var manifest *schema.Manifest
		contract := currentAtlasPlannerContract()
		if len(applied) != 0 {
			last := files[len(applied)-1]
			appliedManifest := schema.NewManifest(last.Artifact.After)
			manifest = &appliedManifest
			contract = postgresArtifactTargetContract(last.Artifact)
		}
		if err := verifyPostgresPhysicalState(ctx, connection, manifest, contract); err != nil {
			return fmt.Errorf("current migration state: %w", err)
		}
		if manifest != nil {
			if err := verifyPostgresPluginMigrationState(ctx, connection, *manifest); err != nil {
				return fmt.Errorf("current migration state: %w", err)
			}
		}
	}
	if !exists {
		if err := ensureArtifactLedger(ctx, connection); err != nil {
			return err
		}
	}
	if err := ensureArtifactStepLedger(ctx, connection); err != nil {
		return err
	}
	emitMigrationNotices(options, notices)
	for index, file := range files {
		if index < len(applied) {
			continue
		}
		complete, err := applyArtifact(ctx, connection, file, options, registry)
		if err != nil {
			return err
		}
		if !complete {
			return nil
		}
	}
	if len(files) != 0 {
		last := files[len(files)-1]
		manifest := schema.NewManifest(last.Artifact.After)
		if err := verifyPostgresPhysicalState(ctx, connection, &manifest, postgresArtifactTargetContract(last.Artifact)); err != nil {
			return fmt.Errorf("current migration state: %w", err)
		}
		if err := verifyPostgresPluginMigrationState(ctx, connection, manifest); err != nil {
			return fmt.Errorf("current migration state: %w", err)
		}
	}
	return nil
}

func preflightPendingArtifacts(ctx context.Context, files []migrationartifact.File, options RunnerOptions) ([]MigrationNotice, error) {
	return preflightArtifactExecution(ctx, files, 0, options)
}

func preflightArtifactExecution(ctx context.Context, history []migrationartifact.File, pendingStart int, options RunnerOptions) ([]MigrationNotice, error) {
	if pendingStart < 0 || pendingStart > len(history) {
		return nil, fmt.Errorf("pending boundary %d is outside history of %d artifacts", pendingStart, len(history))
	}
	if err := validatePostgresSemanticHistory(history); err != nil {
		return nil, err
	}
	files := history[pendingStart:]
	if err := validatePendingArtifactInspection(ctx, files); err != nil {
		return nil, err
	}
	if err := validateStopBoundary(files, options); err != nil {
		return nil, err
	}
	var notices []MigrationNotice
	var maintenance []string
	for _, file := range files {
		if file.Artifact.Planner.Version != AtlasVersion {
			notices = append(notices, MigrationNotice{
				Code: NoticeAtlasProvenance, Artifact: file.Name,
				Message: fmt.Sprintf("artifact %s was planned by Atlas %s; runner Atlas %s independently regenerated and exactly matched the complete execution contract before execution", file.Name, file.Artifact.Planner.Version, AtlasVersion),
			})
		}
		if artifactRequiresMaintenance(file.Artifact) && !options.AllowMaintenance {
			maintenance = append(maintenance, file.Name)
		}
	}
	if len(maintenance) != 0 {
		return nil, &MaintenanceRequiredError{Artifacts: maintenance}
	}
	return notices, nil
}

func validatePostgresResourceRetirementTopology(artifact ridumigration.Artifact) error {
	// Initial artifacts cannot contain a valid retirement or rename.
	if artifact.Before == nil {
		return nil
	}
	type locatedSQL struct {
		stepIndex int
		sql       string
	}
	type fieldCollectionBinding struct {
		before schema.Collection
		after  schema.Collection
	}
	physicalByPhase := make([][]locatedSQL, len(artifact.Phases))
	for phaseIndex, phase := range artifact.Phases {
		for stepIndex, step := range phase.Steps {
			if step.Kind != ridumigration.StepSQL {
				continue
			}
			var payload ridumigration.SQLPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return fmt.Errorf("decode physical SQL: %w", err)
			}
			physicalByPhase[phaseIndex] = append(physicalByPhase[phaseIndex], locatedSQL{stepIndex: stepIndex, sql: payload.SQL})
		}
	}

	var retirement *ridumigration.RetireResourcesPayload
	retirementPhaseIndex := -1
	retirementStepIndex := -1
	var retirementPhasePhysicalSteps []ridumigration.Operation
	mapping := atlasIdentityMap{collections: make(map[schema.StableID]schema.StableID), fields: make(map[string]schema.StableID)}
	fieldMapping := referenceShapeMapping{
		fields: make(map[string]referenceShapeFieldIdentity), targets: make(map[string]string),
	}
	renameTargets := make(map[schema.StableID]schema.StableID)
	var fieldBindings []fieldCollectionBinding
	var collectionBindings []fieldCollectionBinding
	for phaseIndex, phase := range artifact.Phases {
		for stepIndex, step := range phase.Steps {
			if step.Kind == ridumigration.StepRenameContent {
				var payload ridumigration.RenamePayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode content rename: %w", err)
				}
				before, beforeFound := collectionBySlug(artifact.Before.Collections, payload.Rename.CollectionBefore)
				after, afterFound := collectionBySlug(artifact.After.Collections, payload.Rename.CollectionAfter)
				if !beforeFound || !afterFound {
					return fmt.Errorf("content rename addresses absent collection %q -> %q", payload.Rename.CollectionBefore, payload.Rename.CollectionAfter)
				}
				if payload.Rename.FieldBefore != "" || payload.Rename.FieldAfter != "" {
					if err := bindReferenceShapeFieldRename(&mapping, &fieldMapping, before, after, payload.Rename.FieldBefore, payload.Rename.FieldAfter); err != nil {
						return err
					}
				}
				for _, pair := range payload.Rename.Fields {
					if err := bindReferenceShapeFieldRename(&mapping, &fieldMapping, before, after, pair.Before, pair.After); err != nil {
						return err
					}
				}
				if payload.Rename.FieldBefore != "" || payload.Rename.FieldAfter != "" {
					fieldBindings = append(fieldBindings, fieldCollectionBinding{before: before, after: after})
				} else {
					if phaseIndex != 0 {
						return fmt.Errorf("collection rename %s -> %s must execute in the artifact's first atomic transaction phase", before.ID, after.ID)
					}
					if before.ID == after.ID {
						// Physical identity stays fixed, while public slugs embedded in
						// polymorphic/plugin JSON still need the semantic rewrite. Exact
						// one-to-one intent is checked independently for every contract.
						continue
					}
					if postgresResourceIDExists(artifact.After, before.ID) {
						return fmt.Errorf("collection rename source %s remains in the after manifest", before.ID)
					}
					if postgresResourceIDExists(*artifact.Before, after.ID) {
						return fmt.Errorf("collection rename target %s already existed in the before manifest", after.ID)
					}
					if previousTarget, existed := collectionBySlug(artifact.Before.Collections, after.Slug); existed && previousTarget.ID != before.ID {
						return fmt.Errorf("collection rename target slug %q belonged to different before collection %s", after.Slug, previousTarget.ID)
					}
					if existing, duplicate := mapping.collections[before.ID]; duplicate {
						return fmt.Errorf("collection rename source %s is mapped more than once (first target %s)", before.ID, existing)
					}
					if existing, duplicate := renameTargets[after.ID]; duplicate {
						return fmt.Errorf("collection rename target %s is mapped more than once (first source %s)", after.ID, existing)
					}
					beforeTable, afterTable := collectionTable(before.ID), collectionTable(after.ID)
					physicalRenames := 0
					for physicalPhaseIndex, physicalSteps := range physicalByPhase {
						for _, physical := range physicalSteps {
							if isRenameTableStatement(physical.sql, beforeTable, afterTable) {
								if physicalPhaseIndex != phaseIndex || physical.stepIndex >= stepIndex {
									return fmt.Errorf("collection rename %s -> %s has a matching physical table rename outside its preceding transaction lineage", before.ID, after.ID)
								}
								physicalRenames++
								continue
							}
							if sqlReplacesRenameTableIdentity(physical.sql, beforeTable, afterTable) {
								return fmt.Errorf("collection rename %s -> %s has an additional source/target table data or identity mutation outside its matching physical rename", before.ID, after.ID)
							}
						}
					}
					if physicalRenames != 1 {
						return fmt.Errorf("collection rename %s -> %s requires exactly one matching physical table rename earlier in the same transaction phase", before.ID, after.ID)
					}
					mapping.collections[before.ID] = after.ID
					renameTargets[after.ID] = before.ID
					collectionBindings = append(collectionBindings, fieldCollectionBinding{before: before, after: after})
				}
			}
			if step.Kind != ridumigration.StepRetireResources {
				continue
			}
			if retirement != nil {
				return fmt.Errorf("multiple resource-retirement steps are not allowed")
			}
			var payload ridumigration.RetireResourcesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return fmt.Errorf("decode resource-retirement payload: %w", err)
			}
			copy := payload
			retirement = &copy
			retirementPhaseIndex = phaseIndex
			retirementStepIndex = stepIndex
		}
	}
	for _, field := range fieldBindings {
		if field.before.ID == field.after.ID || mapping.collections[field.before.ID] == field.after.ID {
			continue
		}
		return fmt.Errorf("field content rename crosses unconfirmed collection identity %s -> %s", field.before.ID, field.after.ID)
	}
	// Frozen collection-rename intent serializes only paths that changed because
	// those are the addresses the content executor must rewrite. A confirmed
	// collection rename also changes derived field IDs at unchanged paths (for
	// example users.manager -> members.manager). Once the one-to-one collection
	// identity and physical table rename above are proven, bind only equal root
	// paths from the embedded manifests. Reference kind, targets, cardinality,
	// localization, and every nested container remain independently compared by
	// unsafeReferenceShapeDecreases, so this does not infer continuity for a
	// removed, retyped, or narrowed reference.
	for _, binding := range collectionBindings {
		for _, beforeRoot := range binding.before.Fields {
			afterRoot, exists := schemaFieldByPath(binding.after.Fields, beforeRoot.Path.String())
			if !exists || len(afterRoot.Path.Segments()) != 1 {
				continue
			}
			if err := bindReferenceShapeFieldRename(
				&mapping, &fieldMapping, binding.before, binding.after,
				beforeRoot.Path.String(), afterRoot.Path.String(),
			); err != nil {
				return err
			}
		}
	}

	expectedRetired := removedResourceIDs(*artifact.Before, artifact.After, mapping)
	if retirement == nil {
		if len(expectedRetired) != 0 {
			return fmt.Errorf("removed resources %v require an exact resource-retirement step before physical removal", expectedRetired)
		}
		if blocked := unsafeReferenceShapeDecreases(*artifact.Before, artifact.After, mapping, fieldMapping, nil, nil, nil); len(blocked) != 0 {
			return fmt.Errorf("reference-shape decrease contract is unsafe (%s): %s", blocked[0].Code, blocked[0].Message)
		}
		return nil
	}
	if !sameStableIDList(retirement.ResourceIDs, expectedRetired) {
		return fmt.Errorf("resource retirement does not exactly match removed resources: got %v, want %v", retirement.ResourceIDs, expectedRetired)
	}
	for _, resourceID := range retirement.ResourceIDs {
		found := false
		for _, physical := range physicalByPhase[retirementPhaseIndex] {
			if physical.stepIndex > retirementStepIndex && isDropTableStatement(physical.sql, collectionTable(resourceID)) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("resource retirement for %s is not followed by its physical table drop in the same transaction phase", resourceID)
		}
	}
	for _, physical := range physicalByPhase[retirementPhaseIndex] {
		retirementPhasePhysicalSteps = append(retirementPhasePhysicalSteps, ridumigration.Operation{Kind: ridumigration.StepSQL, SQL: physical.sql})
	}
	expectedVersionOwners, blocked := resourceRetirementReferencePolicy(*artifact.Before, artifact.After, mapping, retirementPhasePhysicalSteps, retirement.ResourceIDs)
	if len(blocked) != 0 {
		return fmt.Errorf("resource-retirement reference contract is unsafe: %s", blocked[0].Message)
	}
	if blocked := unsafeReferenceShapeDecreases(
		*artifact.Before, artifact.After, mapping, fieldMapping, retirementPhasePhysicalSteps,
		retirement.ResourceIDs, expectedVersionOwners,
	); len(blocked) != 0 {
		return fmt.Errorf("reference-shape decrease contract is unsafe (%s): %s", blocked[0].Code, blocked[0].Message)
	}
	if !sameStableIDList(expectedVersionOwners, retirement.PurgeVersionOwnerIDs) {
		return fmt.Errorf("resource-retirement version-owner purge does not match reference topology: got %v, want %v", retirement.PurgeVersionOwnerIDs, expectedVersionOwners)
	}
	if len(expectedVersionOwners) != 0 {
		foundRisk := false
		for _, risk := range artifact.Risks {
			if risk.Code == "RIDU_RETIRE_DEPENDENT_VERSION_HISTORY" && risk.Level == ridumigration.RiskDestructive {
				foundRisk = true
			}
		}
		if !foundRisk {
			return fmt.Errorf("dependent version-history purge lacks its destructive risk")
		}
	}
	return nil
}

func bindReferenceShapeFieldRename(
	physical *atlasIdentityMap,
	shape *referenceShapeMapping,
	beforeCollection, afterCollection schema.Collection,
	beforePath, afterPath string,
) error {
	if beforePath != afterPath && (embedded.DescendantPath(beforeCollection.Fields, beforePath) || embedded.DescendantPath(afterCollection.Fields, afterPath)) {
		return fmt.Errorf("embedded field rename %q to %q requires an explicit data migration", beforePath, afterPath)
	}

	if beforePath == "" || afterPath == "" {
		return fmt.Errorf("field content rename requires both before and after paths")
	}
	beforeField, beforeFound := schemaFieldByPath(beforeCollection.Fields, beforePath)
	afterField, afterFound := schemaFieldByPath(afterCollection.Fields, afterPath)
	if !beforeFound || !afterFound {
		return fmt.Errorf("field content rename addresses absent field %s.%s -> %s.%s", beforeCollection.ID, beforePath, afterCollection.ID, afterPath)
	}
	if err := shape.add(beforeCollection.ID, beforeField, afterField); err != nil {
		return err
	}
	if len(beforeField.Path.Segments()) != 1 || len(afterField.Path.Segments()) != 1 {
		return nil
	}
	key := statementFieldKey(beforeCollection.ID, beforeField.ID)
	if existing, duplicate := physical.fields[key]; duplicate && existing != afterField.ID {
		return fmt.Errorf("field rename source %s.%s is mapped more than once (first target %s)", beforeCollection.ID, beforeField.ID, existing)
	}
	physical.fields[key] = afterField.ID
	return nil
}

func schemaFieldByPath(fields []schema.Field, path string) (schema.Field, bool) {
	for _, field := range fields {
		if field.Path.String() == path {
			return field, true
		}
		if found, exists := schemaFieldByPath(schema.ChildFields(field), path); exists {
			return found, true
		}
	}
	return schema.Field{}, false
}

func postgresResourceIDExists(snapshot schema.Snapshot, id schema.StableID) bool {
	for _, collection := range snapshot.Collections {
		if collection.ID == id {
			return true
		}
	}
	for _, global := range snapshot.Globals {
		if global.ID == id {
			return true
		}
	}
	return false
}

func isRenameTableStatement(statement, beforeTable, afterTable string) bool {
	statement = strings.TrimSuffix(strings.TrimSpace(statement), ";")
	return statement == "ALTER TABLE "+quote(beforeTable)+" RENAME TO "+quote(afterTable)
}

func sqlReplacesRenameTableIdentity(statement, beforeTable, afterTable string) bool {
	normalized := strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(statement)), " "))
	for _, table := range []string{beforeTable, afterTable} {
		identifier := strings.ToUpper(quote(table))
		for _, prefix := range []string{
			"CREATE TABLE " + identifier,
			"CREATE TABLE IF NOT EXISTS " + identifier,
			"DROP TABLE " + identifier,
			"DROP TABLE IF EXISTS " + identifier,
			"TRUNCATE " + identifier,
			"TRUNCATE TABLE " + identifier,
			"DELETE FROM " + identifier,
			"INSERT INTO " + identifier,
			"UPDATE " + identifier,
			"MERGE INTO " + identifier,
			"COPY " + identifier,
			"ALTER TABLE " + identifier + " RENAME TO ",
			"ALTER TABLE IF EXISTS " + identifier + " RENAME TO ",
		} {
			if strings.HasSuffix(prefix, " ") && strings.HasPrefix(normalized, prefix) ||
				normalized == prefix || strings.HasPrefix(normalized, prefix+" ") || strings.HasPrefix(normalized, prefix+"(") {
				return true
			}
		}
		if strings.HasPrefix(normalized, "ALTER TABLE ") && strings.Contains(normalized, " RENAME TO "+identifier) {
			return true
		}
	}
	return false
}

func sameStableIDList(left, right []schema.StableID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func artifactRequiresMaintenance(artifact ridumigration.Artifact) bool {
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if migrationStepRequiresMaintenance(step.Kind) {
				return true
			}
		}
	}
	return false
}

func snapshotHasAuthCollections(snapshot schema.Snapshot) bool {
	for _, collection := range snapshot.Collections {
		if collection.Auth != nil {
			return true
		}
	}
	return false
}

func migrationStepRequiresMaintenance(kind ridumigration.StepKind) bool {
	return kind == ridumigration.StepRenameContent || kind == ridumigration.StepBackfillReferences || kind == ridumigration.StepRetireResources || kind == ridumigration.StepCanonicalizeAuthIdentities || kind == ridumigration.StepDataTransform
}

func emitMigrationNotices(options RunnerOptions, notices []MigrationNotice) {
	if options.Notice == nil {
		return
	}
	for _, notice := range notices {
		options.Notice(notice)
	}
}

type artifactLedgerRow struct {
	name, digest, fromDigest, toDigest string
}

func ensureArtifactLedger(ctx context.Context, connection *sql.Conn) error {
	exists, err := artifactLedgerExists(ctx, connection)
	if err != nil {
		return err
	}
	if exists {
		return validateArtifactLedger(ctx, connection)
	}
	_, err = connection.ExecContext(ctx, `CREATE TABLE ridu_migrations (
name text PRIMARY KEY,
artifact_digest text NOT NULL,
from_digest text NOT NULL,
to_digest text NOT NULL,
planner_name text NOT NULL,
planner_version text NOT NULL,
applied_at timestamptz NOT NULL DEFAULT now()
)`)
	if err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	return nil
}

func artifactLedgerExists(ctx context.Context, connection *sql.Conn) (bool, error) {
	var exists bool
	err := connection.QueryRowContext(ctx, `SELECT to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL`).Scan(&exists)
	return exists, err
}

func validateArtifactLedger(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	rows, err := queryer.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'ridu_migrations'`)
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
	for _, required := range []string{"name", "artifact_digest", "from_digest", "to_digest", "planner_name", "planner_version", "applied_at"} {
		if !columns[required] {
			return fmt.Errorf("migration ledger is malformed: required column %s is missing", required)
		}
	}
	return nil
}

func readArtifactLedger(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]artifactLedgerRow, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT name, artifact_digest, from_digest, to_digest FROM ridu_migrations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []artifactLedgerRow
	for rows.Next() {
		var row artifactLedgerRow
		if err := rows.Scan(&row.name, &row.digest, &row.fromDigest, &row.toDigest); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func validateAppliedArtifacts(files []migrationartifact.File, applied []artifactLedgerRow) error {
	if len(applied) > len(files) {
		return fmt.Errorf("database contains %d applied migrations but the directory contains only %d", len(applied), len(files))
	}
	for index, row := range applied {
		file := files[index]
		if row.name != file.Name {
			return fmt.Errorf("migration history diverged at %s; database records %s", file.Name, row.name)
		}
		if row.digest != file.Digest {
			return fmt.Errorf("migration %s changed after application", file.Name)
		}
		if row.fromDigest != file.Artifact.FromDigest || row.toDigest != file.Artifact.ToDigest {
			return fmt.Errorf("migration %s manifest lineage differs from the database ledger", file.Name)
		}
	}
	return nil
}

func artifactExecutionInProgress(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, files []migrationartifact.File, applied int, stepLedgerExists bool) (bool, error) {
	if !stepLedgerExists {
		return false, nil
	}
	for _, file := range files[applied:] {
		ledger, err := readArtifactStepLedger(ctx, queryer, file)
		if err != nil {
			return false, err
		}
		if len(ledger) != 0 {
			return true, nil
		}
	}
	return false, nil
}

func assertPhysicalSchema(ctx context.Context, transaction *sql.Tx, expectedManifest schema.Manifest) error {
	return assertPhysicalSchemaForContract(ctx, transaction, expectedManifest, currentAtlasPlannerContract())
}

func assertPhysicalSchemaForContract(ctx context.Context, transaction *sql.Tx, expectedManifest schema.Manifest, contract atlasPlannerContract) error {
	return assertPhysicalSchemaShape(ctx, transaction, expectedManifest, atlasSchemaForContract(expectedManifest, atlasIdentityMap{}, contract))
}

func assertMigrationPhysicalSchemaForContract(ctx context.Context, transaction *sql.Tx, expectedManifest schema.Manifest, contract atlasPlannerContract) error {
	if err := assertPhysicalSchemaForContract(ctx, transaction, expectedManifest, contract); err != nil {
		return err
	}
	return assertPostgresPluginTablesPresent(ctx, transaction, expectedManifest)
}

func verifyPostgresPluginMigrationState(ctx context.Context, connection *sql.Conn, manifest schema.Manifest) error {
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	if err := assertPostgresPluginTablesPresent(ctx, transaction, manifest); err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Rollback()
}

func assertPostgresPluginTablesPresent(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest) error {
	pluginTables := postgresPluginTables(manifest)
	tables := make([]string, 0, len(pluginTables))
	for table := range pluginTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		exists, err := transactionPluginTableExists(ctx, transaction, table)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("physical schema drift detected: declared plugin table %s is missing or is not an ordinary or partitioned table", table)
		}
	}
	return nil
}

func transactionPluginTableExists(ctx context.Context, transaction *sql.Tx, table string) (bool, error) {
	var exists bool
	err := transaction.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1
FROM pg_class relation
JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = current_schema()
  AND relation.relname = $1
  AND relation.relkind IN ('r', 'p')
)`, table).Scan(&exists)
	return exists, err
}

func assertEmptyPhysicalSchema(ctx context.Context, transaction *sql.Tx) error {
	empty := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Empty migration baseline"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	return assertPhysicalSchemaShape(ctx, transaction, empty, emptyAtlasSchema())
}

func assertPhysicalSchemaShape(ctx context.Context, transaction *sql.Tx, expectedManifest schema.Manifest, expected *atlasschema.Schema) error {
	driver, err := atlaspostgres.Open(transaction)
	if err != nil {
		return fmt.Errorf("open Atlas inspector: %w", err)
	}
	actual, err := driver.InspectSchema(ctx, "", &atlasschema.InspectOptions{Mode: atlasschema.InspectTables})
	if err != nil {
		return fmt.Errorf("inspect PostgreSQL schema: %w", err)
	}
	pluginTables := postgresPluginTables(expectedManifest)
	for index := len(actual.Tables) - 1; index >= 0; index-- {
		if actual.Tables[index].Name == "ridu_migrations" || actual.Tables[index].Name == "ridu_migration_steps" {
			actual.Tables = append(actual.Tables[:index], actual.Tables[index+1:]...)
			continue
		}
		if _, owned := pluginTables[actual.Tables[index].Name]; owned {
			actual.Tables = append(actual.Tables[:index], actual.Tables[index+1:]...)
		}
	}
	expected.Name = actual.Name
	for _, table := range expected.Tables {
		table.Schema = expected
	}
	allowedIndexes, err := classifyPostgresManagedPhysicalState(ctx, transaction, expected)
	if err != nil {
		return err
	}
	for _, table := range actual.Tables {
		allowed := allowedIndexes[table.Name]
		if len(allowed) != 0 {
			indexes := table.Indexes[:0]
			for _, index := range table.Indexes {
				if _, omit := allowed[index.Name]; !omit {
					indexes = append(indexes, index)
				}
			}
			table.Indexes = indexes
		}
		// Enabled non-internal triggers were rejected above. Disabled user
		// triggers do not affect Ridu operations and are outside the required
		// manifest contract, so they do not impose catalogue purity.
		table.Triggers = nil
	}
	normalizePostgresJSONBDefaults(actual, expected)
	changes, err := atlaspostgres.DefaultDiff.SchemaDiff(actual, expected)
	if err != nil {
		return fmt.Errorf("compare PostgreSQL schema: %w", err)
	}
	if len(changes) != 0 {
		plan, planErr := atlaspostgres.DefaultPlan.PlanChanges(ctx, "schema-drift", changes)
		if planErr != nil {
			return fmt.Errorf("physical schema drift detected (%d changes)", len(changes))
		}
		var descriptions []string
		for _, change := range plan.Changes {
			descriptions = append(descriptions, fmt.Sprintf("%s: %s", change.Comment, change.Cmd))
		}
		return fmt.Errorf("physical schema drift detected: %s", strings.Join(descriptions, "; "))
	}
	return nil
}

func postgresPluginTables(manifest schema.Manifest) map[string]struct{} {
	tables := make(map[string]struct{})
	for _, plugin := range manifest.Snapshot().Plugins {
		contribution, supported := plugin.DatabaseContribution(schema.PluginDatabaseAdapterPostgres)
		if !supported {
			continue
		}
		for _, owned := range contribution.Tables {
			tables[owned] = struct{}{}
		}
	}
	return tables
}

func applyContentRename(ctx context.Context, transaction *sql.Tx, artifact ridumigration.Artifact, intent ridumigration.Rename) error {
	if artifact.Before == nil {
		return fmt.Errorf("content rename requires a before manifest")
	}
	beforeCollection, ok := collectionBySlug(artifact.Before.Collections, intent.CollectionBefore)
	if !ok {
		return fmt.Errorf("before collection %q is absent", intent.CollectionBefore)
	}
	afterCollection, ok := collectionBySlug(artifact.After.Collections, intent.CollectionAfter)
	if !ok {
		return fmt.Errorf("after collection %q is absent", intent.CollectionAfter)
	}
	renames := append([]ridumigration.FieldRename(nil), intent.Fields...)
	if intent.FieldBefore != "" {
		renames = append(renames, ridumigration.FieldRename{Before: intent.FieldBefore, After: intent.FieldAfter})
	}
	sort.Slice(renames, func(i, j int) bool {
		return strings.Count(renames[i].Before, ".") > strings.Count(renames[j].Before, ".")
	})
	for _, rename := range renames {
		beforePath, afterPath := strings.Split(rename.Before, "."), strings.Split(rename.After, ".")
		if len(beforePath) > 1 {
			afterTop, found := topField(afterCollection.Fields, afterPath[0])
			if !found {
				return fmt.Errorf("after field %q is absent", afterPath[0])
			}
			if err := rewriteJSONColumn(ctx, transaction, collectionTable(afterCollection.ID), fieldColumn(afterTop.ID), func(value any) (bool, error) {
				if jsonRenameCollision(value, beforePath[1:], afterPath[len(afterPath)-1]) {
					return false, fmt.Errorf("field rename %s to %s would overwrite existing content", rename.Before, rename.After)
				}
				return renameJSONKey(value, beforePath[1:], afterPath[len(afterPath)-1]), nil
			}); err != nil {
				return err
			}
		}
	}
	if err := rewriteVersionSnapshots(ctx, transaction, beforeCollection.ID, renames); err != nil {
		return err
	}
	collectionIdentityRename := intent.FieldBefore == "" && intent.FieldAfter == ""
	if collectionIdentityRename && intent.CollectionBefore != intent.CollectionAfter {
		if err := rewriteCollectionReferences(ctx, transaction, artifact, intent.CollectionBefore, intent.CollectionAfter); err != nil {
			return err
		}
	}
	if collectionIdentityRename && beforeCollection.ID != afterCollection.ID {
		for _, table := range []string{"ridu_auth_credentials", "ridu_auth_sessions", "ridu_auth_tokens", "ridu_auth_api_keys", "ridu_preferences", "ridu_versions", "ridu_document_locks"} {
			exists, err := transactionTableExists(ctx, transaction, table)
			if err != nil {
				return err
			}
			if exists {
				if _, err := transaction.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET collection_id = $1 WHERE collection_id = $2", quote(table)), afterCollection.ID, beforeCollection.ID); err != nil {
					return err
				}
			}
		}
		if exists, err := transactionTableExists(ctx, transaction, "ridu_document_locks"); err != nil {
			return err
		} else if exists {
			if _, err := transaction.ExecContext(ctx, `UPDATE ridu_document_locks SET owner_collection_id = $1 WHERE owner_collection_id = $2`, afterCollection.ID, beforeCollection.ID); err != nil {
				return err
			}
		}
		if err := rewriteDurableTaskCollectionIDs(ctx, transaction, string(beforeCollection.ID), string(afterCollection.ID)); err != nil {
			return err
		}
	}
	return nil
}

type scheduledPublishTaskRenameRow struct {
	ID                      string
	TargetCollectionID      sql.NullString
	TargetDocumentID        sql.NullString
	RequestedByCollectionID sql.NullString
	RequestedByDocumentID   sql.NullString
	Input                   []byte
	ConcurrencyKey          sql.NullString
}

func rewriteDurableTaskCollectionIDs(ctx context.Context, transaction *sql.Tx, before, after string) error {
	exists, err := transactionTableExists(ctx, transaction, "ridu_tasks")
	if err != nil || !exists {
		return err
	}
	rows, err := transaction.QueryContext(ctx, `SELECT id, target_collection_id, target_document_id,
requested_by_collection_id, requested_by_document_id, input, concurrency_key
FROM ridu_tasks
WHERE task_slug = 'ridu-schedule-publish' AND (
  target_collection_id = $1 OR requested_by_collection_id = $1 OR
  input->>'collectionID' = $1 OR input->>'requestedByCollectionID' = $1
)
FOR UPDATE`, before)
	if err != nil {
		return err
	}
	var scheduled []scheduledPublishTaskRenameRow
	for rows.Next() {
		var row scheduledPublishTaskRenameRow
		if err := rows.Scan(&row.ID, &row.TargetCollectionID, &row.TargetDocumentID,
			&row.RequestedByCollectionID, &row.RequestedByDocumentID, &row.Input, &row.ConcurrencyKey); err != nil {
			rows.Close()
			return err
		}
		scheduled = append(scheduled, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range scheduled {
		input, concurrencyKey, targetCollection, requesterCollection, err := rewriteScheduledPublishTaskCollectionID(row, before, after)
		if err != nil {
			return fmt.Errorf("rewrite scheduled-publish task %s: %w", row.ID, err)
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE ridu_tasks SET
target_collection_id = $2, requested_by_collection_id = $3, input = $4::jsonb, concurrency_key = $5
WHERE id = $1`, row.ID, targetCollection, requesterCollection, input, concurrencyKey); err != nil {
			return err
		}
	}
	// Task payloads are application-owned except for Ridu's built-in scheduled
	// publish task. Generic durable task identities still follow a supported
	// collection-ID rename, but their opaque input and concurrency keys do not.
	if _, err := transaction.ExecContext(ctx, `UPDATE ridu_tasks SET target_collection_id = $1 WHERE target_collection_id = $2`, after, before); err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `UPDATE ridu_tasks SET requested_by_collection_id = $1 WHERE requested_by_collection_id = $2`, after, before)
	return err
}

func rewriteScheduledPublishTaskCollectionID(row scheduledPublishTaskRenameRow, before, after string) ([]byte, string, any, any, error) {
	var input map[string]json.RawMessage
	if err := json.Unmarshal(row.Input, &input); err != nil || input == nil {
		if err == nil {
			err = fmt.Errorf("input must be a JSON object")
		}
		return nil, "", nil, nil, err
	}
	readRequired := func(key string) (string, error) {
		var value string
		encoded, ok := input[key]
		if !ok || json.Unmarshal(encoded, &value) != nil || value == "" {
			return "", fmt.Errorf("input.%s must be a non-empty string", key)
		}
		return value, nil
	}
	collectionID, err := readRequired("collectionID")
	if err != nil {
		return nil, "", nil, nil, err
	}
	documentID, err := readRequired("documentID")
	if err != nil {
		return nil, "", nil, nil, err
	}
	if !row.TargetCollectionID.Valid || !row.TargetDocumentID.Valid ||
		row.TargetCollectionID.String != collectionID || row.TargetDocumentID.String != documentID {
		return nil, "", nil, nil, fmt.Errorf("target columns do not match built-in input")
	}
	requestedCollection, requestedCollectionPresent, err := optionalJSONString(input, "requestedByCollectionID")
	if err != nil {
		return nil, "", nil, nil, err
	}
	requestedDocument, requestedDocumentPresent, err := optionalJSONString(input, "requestedByUserID")
	if err != nil {
		return nil, "", nil, nil, err
	}
	if requestedCollectionPresent != requestedDocumentPresent ||
		row.RequestedByCollectionID.Valid != row.RequestedByDocumentID.Valid ||
		requestedCollectionPresent != row.RequestedByCollectionID.Valid ||
		(requestedCollectionPresent && (row.RequestedByCollectionID.String != requestedCollection || row.RequestedByDocumentID.String != requestedDocument)) {
		return nil, "", nil, nil, fmt.Errorf("requester columns do not match built-in input")
	}
	concurrencyKey := row.ConcurrencyKey.String
	if collectionID == before {
		collectionID = after
		input["collectionID"], _ = json.Marshal(after)
		concurrencyKey = scheduledPublishConcurrencyKey(collectionID, documentID)
	}
	if requestedCollectionPresent && requestedCollection == before {
		requestedCollection = after
		input["requestedByCollectionID"], _ = json.Marshal(after)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, "", nil, nil, err
	}
	var requester any
	if requestedCollectionPresent {
		requester = requestedCollection
	}
	return encoded, concurrencyKey, collectionID, requester, nil
}

func optionalJSONString(input map[string]json.RawMessage, key string) (string, bool, error) {
	encoded, ok := input[key]
	if !ok {
		return "", false, nil
	}
	var value *string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return "", false, fmt.Errorf("input.%s must be a string or null", key)
	}
	if value == nil {
		return "", false, nil
	}
	if *value == "" {
		return "", false, fmt.Errorf("input.%s must be a non-empty string when present", key)
	}
	return *value, true, nil
}

func rewriteVersionSnapshots(ctx context.Context, transaction *sql.Tx, collectionID schema.StableID, renames []ridumigration.FieldRename) error {
	exists, err := transactionTableExists(ctx, transaction, "ridu_versions")
	if err != nil || !exists {
		return err
	}
	return rewriteJSONRows(ctx, transaction,
		`SELECT document_id, revision, snapshot FROM ridu_versions WHERE collection_id = $1 FOR UPDATE`, []any{collectionID},
		func(key1 string, key2 int, encoded []byte) (bool, []byte, error) {
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				return false, nil, err
			}
			values, _ := document["Values"].(map[string]any)
			changed := false
			for _, rename := range renames {
				if jsonRenameCollision(values, strings.Split(rename.Before, "."), lastPathSegment(rename.After)) {
					return false, nil, fmt.Errorf("version snapshot field rename %s to %s would overwrite existing content", rename.Before, rename.After)
				}
				changed = renameJSONKey(values, strings.Split(rename.Before, "."), lastPathSegment(rename.After)) || changed
			}
			if !changed {
				return false, nil, nil
			}
			updated, err := json.Marshal(document)
			return true, updated, err
		},
		`UPDATE ridu_versions SET snapshot = $1 WHERE collection_id = $2 AND document_id = $3 AND revision = $4`, collectionID,
	)
}

func rewriteCollectionReferences(ctx context.Context, transaction *sql.Tx, artifact ridumigration.Artifact, before, after schema.CollectionSlug) error {
	if artifact.Before == nil {
		return fmt.Errorf("collection reference rewrite requires a before manifest")
	}
	beforeSnapshot, afterSnapshot := *artifact.Before, artifact.After
	aliases, err := postgresCurrentReferencePhysicalAliases(artifact)
	if err != nil {
		return err
	}
	// Inspect both schemas so a field/root rename in the same artifact cannot
	// hide an old location. Each transform is constrained to an exact owner,
	// physical root, nested field path, and declared reference property.
	for snapshotIndex, snapshot := range []schema.Snapshot{beforeSnapshot, afterSnapshot} {
		for _, owner := range snapshotReferenceOwners(snapshot) {
			physicalOwnerID := owner.ID
			if snapshotIndex == 0 {
				physicalOwnerID = aliases.owner(owner.ID)
			}
			table := collectionTable(physicalOwnerID)
			exists, err := transactionTableExists(ctx, transaction, table)
			if err != nil || !exists {
				if err != nil {
					return err
				}
				continue
			}
			for _, field := range owner.Fields {
				if columnType(field) != "jsonb" || !fieldContainsCollectionReferences(field) {
					continue
				}
				physicalRootID := field.ID
				if snapshotIndex == 0 {
					physicalRootID = aliases.root(owner.ID, field.ID)
				}
				columns := []string{fieldColumn(physicalRootID)}
				if field.Localized {
					columns = nil
					if snapshot.Application.Localization != nil {
						for _, locale := range snapshot.Application.Localization.LocaleCodes() {
							columns = append(columns, localizedFieldColumn(physicalRootID, locale))
						}
					}
				}
				for _, column := range columns {
					exists, err := transactionColumnExists(ctx, transaction, table, column)
					if err != nil {
						return err
					}
					if !exists {
						// Semantic rewrites precede ordinary additive Atlas SQL. A
						// newly added or already-renamed root has no value here.
						continue
					}
					if err := rewriteJSONColumn(ctx, transaction, table, column, func(value any) (bool, error) {
						if err := validateEmbeddedJSON(field, value, true); err != nil {
							return false, err
						}
						return rewriteFieldCollectionReferences(value, field, true, string(before), string(after))
					}); err != nil {
						return err
					}
				}
			}
		}
	}
	beforeOwner, beforeFound := collectionBySlug(beforeSnapshot.Collections, before)
	afterOwner, afterFound := collectionBySlug(afterSnapshot.Collections, after)
	if !beforeFound || !afterFound {
		return fmt.Errorf("collection reference rewrite cannot resolve owner identity %q -> %q", before, after)
	}
	return rewriteAllVersionReferences(ctx, transaction, beforeSnapshot, afterSnapshot, beforeOwner.ID, afterOwner.ID, string(before), string(after))
}

type snapshotReferenceOwner struct {
	ID     schema.StableID
	Fields []schema.Field
}

func snapshotReferenceOwners(snapshot schema.Snapshot) []snapshotReferenceOwner {
	owners := make([]snapshotReferenceOwner, 0, len(snapshot.Collections)+len(snapshot.Globals))
	for _, collection := range snapshot.Collections {
		owners = append(owners, snapshotReferenceOwner{ID: collection.ID, Fields: collection.Fields})
	}
	for _, global := range snapshot.Globals {
		owners = append(owners, snapshotReferenceOwner{ID: global.ID, Fields: global.Fields})
	}
	return owners
}

func rewriteAllVersionReferences(
	ctx context.Context,
	transaction *sql.Tx,
	beforeSnapshot, afterSnapshot schema.Snapshot,
	beforeOwnerID, afterOwnerID schema.StableID,
	before, after string,
) error {
	exists, err := transactionTableExists(ctx, transaction, "ridu_versions")
	if err != nil || !exists {
		return err
	}
	ownerSchemas := make(map[schema.StableID][][]schema.Field)
	for snapshotIndex, snapshot := range []schema.Snapshot{beforeSnapshot, afterSnapshot} {
		for _, owner := range snapshotReferenceOwners(snapshot) {
			ownerSchemas[owner.ID] = append(ownerSchemas[owner.ID], owner.Fields)
			if snapshotIndex == 0 && beforeOwnerID != afterOwnerID && owner.ID == beforeOwnerID {
				ownerSchemas[afterOwnerID] = append(ownerSchemas[afterOwnerID], owner.Fields)
			}
			if snapshotIndex == 1 && beforeOwnerID != afterOwnerID && owner.ID == afterOwnerID {
				// Collection identity is updated in shared tables only after this
				// semantic step. Field-content renames have already moved snapshot
				// keys, so the old owner ID must also inspect the after schema.
				ownerSchemas[beforeOwnerID] = append(ownerSchemas[beforeOwnerID], owner.Fields)
			}
		}
	}
	rows, err := transaction.QueryContext(ctx, `SELECT collection_id, document_id, revision, snapshot FROM ridu_versions FOR UPDATE`)
	if err != nil {
		return err
	}
	type update struct {
		collection, document string
		revision             int
		value                []byte
	}
	var updates []update
	for rows.Next() {
		var item update
		var encoded []byte
		if err := rows.Scan(&item.collection, &item.document, &item.revision, &encoded); err != nil {
			rows.Close()
			return err
		}
		var document map[string]any
		if err := json.Unmarshal(encoded, &document); err != nil {
			rows.Close()
			return err
		}
		values, _ := document["Values"].(map[string]any)
		changed := false
		for _, fields := range ownerSchemas[schema.StableID(item.collection)] {
			for _, field := range fields {
				value, exists := values[field.Name]
				if !exists || !fieldContainsCollectionReferences(field) {
					continue
				}
				if err := validateEmbeddedJSON(field, value, false); err != nil {
					rows.Close()
					return err
				}
				fieldChanged, err := rewriteFieldCollectionReferences(value, field, false, before, after)
				if err != nil {
					rows.Close()
					return err
				}
				changed = fieldChanged || changed
			}
		}
		if changed {
			item.value, err = json.Marshal(document)
			if err != nil {
				rows.Close()
				return err
			}
			updates = append(updates, item)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := transaction.ExecContext(ctx, `UPDATE ridu_versions SET snapshot = $1 WHERE collection_id = $2 AND document_id = $3 AND revision = $4`, item.value, item.collection, item.document, item.revision); err != nil {
			return err
		}
	}
	return nil
}

func rewriteJSONColumn(ctx context.Context, transaction *sql.Tx, table, column string, transform func(any) (bool, error)) error {
	statement := fmt.Sprintf("SELECT id, %s FROM %s WHERE %s IS NOT NULL FOR UPDATE", quote(column), quote(table), quote(column))
	rows, err := transaction.QueryContext(ctx, statement)
	if err != nil {
		return err
	}
	type update struct {
		id    string
		value []byte
	}
	var updates []update
	for rows.Next() {
		var id string
		var encoded []byte
		if err := rows.Scan(&id, &encoded); err != nil {
			rows.Close()
			return err
		}
		var value any
		if err := json.Unmarshal(encoded, &value); err != nil {
			rows.Close()
			return err
		}
		changed, err := transform(value)
		if err != nil {
			rows.Close()
			return err
		}
		if changed {
			updated, err := json.Marshal(value)
			if err != nil {
				rows.Close()
				return err
			}
			updates = append(updates, update{id: id, value: updated})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := transaction.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s = $1 WHERE id = $2", quote(table), quote(column)), update.value, update.id); err != nil {
			return err
		}
	}
	return nil
}

func rewriteJSONRows(ctx context.Context, transaction *sql.Tx, query string, arguments []any, transform func(string, int, []byte) (bool, []byte, error), update string, collectionID schema.StableID) error {
	rows, err := transaction.QueryContext(ctx, query, arguments...)
	if err != nil {
		return err
	}
	type item struct {
		document string
		revision int
		value    []byte
	}
	var updates []item
	for rows.Next() {
		var document string
		var revision int
		var encoded []byte
		if err := rows.Scan(&document, &revision, &encoded); err != nil {
			rows.Close()
			return err
		}
		changed, value, err := transform(document, revision, encoded)
		if err != nil {
			rows.Close()
			return err
		}
		if changed {
			updates = append(updates, item{document: document, revision: revision, value: value})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := transaction.ExecContext(ctx, update, item.value, collectionID, item.document, item.revision); err != nil {
			return err
		}
	}
	return nil
}

func renameJSONKey(value any, path []string, destination string) bool {
	if len(path) == 0 || value == nil {
		return false
	}
	switch current := value.(type) {
	case []any:
		changed := false
		for _, item := range current {
			changed = renameJSONKey(item, path, destination) || changed
		}
		return changed
	case map[string]any:
		if len(path) == 1 {
			original, exists := current[path[0]]
			if !exists || path[0] == destination {
				return false
			}
			if _, collision := current[destination]; collision {
				return false
			}
			delete(current, path[0])
			current[destination] = original
			return true
		}
		if child, exists := current[path[0]]; exists {
			return renameJSONKey(child, path[1:], destination)
		}
		// Canonical block paths contain the block key, while stored block
		// objects identify it through blockType rather than another wrapper.
		if blockType, _ := current["blockType"].(string); blockType == path[0] {
			return renameJSONKey(current, path[1:], destination)
		}
		return false
	default:
		return false
	}
}

func jsonRenameCollision(value any, path []string, destination string) bool {
	if len(path) == 0 || value == nil {
		return false
	}
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			if jsonRenameCollision(item, path, destination) {
				return true
			}
		}
	case map[string]any:
		if len(path) == 1 {
			_, sourceExists := current[path[0]]
			_, destinationExists := current[destination]
			return path[0] != destination && sourceExists && destinationExists
		}
		if child, exists := current[path[0]]; exists {
			return jsonRenameCollision(child, path[1:], destination)
		}
		if blockType, _ := current["blockType"].(string); blockType == path[0] {
			return jsonRenameCollision(current, path[1:], destination)
		}
	}
	return false
}

func replaceDeclaredPluginReferences(value any, keys map[string]bool, before, after string) bool {
	changed := false
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			changed = replaceDeclaredPluginReferences(item, keys, before, after) || changed
		}
	case map[string]any:
		for key := range keys {
			if relation, ok := current[key].(string); ok && relation == before {
				current[key] = after
				changed = true
			}
		}
		for _, child := range current {
			changed = replaceDeclaredPluginReferences(child, keys, before, after) || changed
		}
	}
	return changed
}

func replacePolymorphicReference(value any, before, after string) bool {
	switch current := value.(type) {
	case []any:
		changed := false
		for _, item := range current {
			changed = replacePolymorphicReference(item, before, after) || changed
		}
		return changed
	case map[string]any:
		if relation, ok := current["relationTo"].(string); ok && relation == before {
			current["relationTo"] = after
			return true
		}
	}
	return false
}

func rewriteFieldCollectionReferences(value any, field schema.Field, currentRoot bool, before, after string) (bool, error) {
	if field.Localized && !currentRoot {
		localized, ok := value.(map[string]any)
		if !ok {
			return false, nil
		}
		changed := false
		unlocalized := field
		unlocalized.Localized = false
		for _, candidate := range localized {
			fieldChanged, err := rewriteFieldCollectionReferences(candidate, unlocalized, false, before, after)
			if err != nil {
				return false, err
			}
			changed = fieldChanged || changed
		}
		return changed, nil
	}
	if embedded.HasFields(field) {
		return rewriteEmbeddedCollectionReferences(value, field, before, after)
	}

	if field.Plugin != nil && len(field.Plugin.ReferenceKeys) != 0 {
		keys := make(map[string]bool, len(field.Plugin.ReferenceKeys))
		for _, key := range field.Plugin.ReferenceKeys {
			keys[key] = true
		}
		return replaceDeclaredPluginReferences(value, keys, before, after), nil
	}
	if field.Relationship != nil && field.Relationship.Polymorphic {
		return replacePolymorphicReference(value, before, after), nil
	}

	changed := false
	switch field.Type {
	case schema.FieldTypeGroup:
		object, ok := value.(map[string]any)
		if !ok || field.Nested == nil {
			return false, nil
		}
		for _, child := range field.Nested.ResolvedFields() {
			candidate, exists := object[child.Name]
			if exists {
				fieldChanged, err := rewriteFieldCollectionReferences(candidate, child, false, before, after)
				if err != nil {
					return false, err
				}
				changed = fieldChanged || changed
			}
		}
	case schema.FieldTypeArray:
		items, ok := value.([]any)
		if !ok || field.Nested == nil {
			return false, nil
		}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for _, child := range field.Nested.ResolvedFields() {
				candidate, exists := object[child.Name]
				if exists {
					fieldChanged, err := rewriteFieldCollectionReferences(candidate, child, false, before, after)
					if err != nil {
						return false, err
					}
					changed = fieldChanged || changed
				}
			}
		}
	case schema.FieldTypeBlocks:
		items, ok := value.([]any)
		if !ok || field.Blocks == nil {
			return false, nil
		}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := object["blockType"].(string)
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug != blockType {
					continue
				}
				for _, child := range block.ResolvedFields() {
					candidate, exists := object[child.Name]
					if exists {
						fieldChanged, err := rewriteFieldCollectionReferences(candidate, child, false, before, after)
						if err != nil {
							return false, err
						}
						changed = fieldChanged || changed
					}
				}
				break
			}
		}
	}
	return changed, nil
}

func fieldContainsCollectionReferences(field schema.Field) bool {
	if field.Relationship != nil && field.Relationship.Polymorphic || field.Plugin != nil && len(field.Plugin.ReferenceKeys) != 0 {
		return true
	}
	for _, child := range schema.ChildFields(field) {
		if fieldContainsCollectionReferences(child) {
			return true
		}
	}
	return false
}

func collectionBySlug(collections []schema.Collection, slug schema.CollectionSlug) (schema.Collection, bool) {
	for _, collection := range collections {
		if collection.Slug == slug {
			return collection, true
		}
	}
	return schema.Collection{}, false
}

func topField(fields []schema.Field, name string) (schema.Field, bool) {
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}
	return schema.Field{}, false
}

func lastPathSegment(path string) string {
	parts := strings.Split(path, ".")
	return parts[len(parts)-1]
}

func transactionTableExists(ctx context.Context, transaction *sql.Tx, table string) (bool, error) {
	var exists bool
	err := transaction.QueryRowContext(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table).Scan(&exists)
	return exists, err
}

func transactionColumnExists(ctx context.Context, transaction *sql.Tx, table, column string) (bool, error) {
	var exists bool
	err := transaction.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
)`, table, column).Scan(&exists)
	return exists, err
}

// ArtifactStatus returns immutable artifact state relative to the database ledger.
func (backend *Store) ArtifactStatus(ctx context.Context, directory string) ([]MigrationStatus, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return nil, err
	}
	return backend.artifactStatusFiles(ctx, files)
}

func (backend *Store) artifactStatusFiles(ctx context.Context, files []migrationartifact.File) ([]MigrationStatus, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial migration before status")
	}
	if err := validatePostgresSemanticHistory(files); err != nil {
		return nil, err
	}
	database := stdlib.OpenDB(*backend.pool.Config().ConnConfig)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	var exists bool
	if err := connection.QueryRowContext(ctx, `SELECT to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	var applied []artifactLedgerRow
	if exists {
		if err := validateArtifactLedger(ctx, connection); err != nil {
			return nil, err
		}
		applied, err = readArtifactLedger(ctx, connection)
		if err != nil {
			return nil, err
		}
		if err := validateAppliedArtifacts(files, applied); err != nil {
			return nil, err
		}
	}
	stepLedgerExists, err := artifactStepLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	if stepLedgerExists {
		if err := validateArtifactStepLedger(ctx, connection); err != nil {
			return nil, err
		}
		if err := validateArtifactStepHistory(ctx, connection, files); err != nil {
			return nil, err
		}
	}
	if err := validateCompletedArtifactSteps(ctx, connection, files, len(applied), stepLedgerExists); err != nil {
		return nil, err
	}
	if err := validatePendingArtifactInspection(ctx, files[len(applied):]); err != nil {
		return nil, err
	}
	status := make([]MigrationStatus, len(files))
	inProgress, err := artifactExecutionInProgress(ctx, connection, files, len(applied), stepLedgerExists)
	if err != nil {
		return nil, err
	}
	for index, file := range files {
		status[index] = MigrationStatus{Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version, Applied: index < len(applied)}
		ledger := map[string]artifactStepLedgerRow{}
		if stepLedgerExists {
			ledger, err = readArtifactStepLedger(ctx, connection, file)
			if err != nil {
				return nil, err
			}
		}
		for _, phase := range file.Artifact.Phases {
			phaseStatus := MigrationPhaseStatus{ID: phase.ID, Mode: phase.Mode, State: "pending", Steps: make([]MigrationStepStatus, 0, len(phase.Steps))}
			complete := 0
			for _, step := range phase.Steps {
				row := ledger[phase.ID+"\x00"+step.ID]
				state := row.State
				if state == "" {
					state = "pending"
				}
				if state == "complete" {
					complete++
				}
				phaseStatus.Steps = append(phaseStatus.Steps, MigrationStepStatus{ID: step.ID, Kind: step.Kind, State: state, Checkpoint: row.Checkpoint})
			}
			if complete == len(phase.Steps) {
				phaseStatus.State = "complete"
			} else if len(ledger) != 0 {
				for _, candidate := range phaseStatus.Steps {
					if candidate.State != "pending" {
						phaseStatus.State = "running"
						break
					}
				}
			}
			status[index].Phases = append(status[index].Phases, phaseStatus)
		}
	}
	if !inProgress {
		var manifest *schema.Manifest
		contract := currentAtlasPlannerContract()
		if len(applied) == 0 {
		} else {
			last := files[len(applied)-1]
			appliedManifest := schema.NewManifest(last.Artifact.After)
			manifest = &appliedManifest
			contract = postgresArtifactTargetContract(last.Artifact)
		}
		if err := verifyPostgresPhysicalState(ctx, connection, manifest, contract); err != nil {
			return nil, fmt.Errorf("migration status physical schema: %w", err)
		}
	}
	return status, nil
}

// ArtifactPlan returns the same immutable execution topology as status,
// including durable checkpoints, without applying any step.
func (backend *Store) ArtifactPlan(ctx context.Context, directory string) ([]MigrationStatus, error) {
	return backend.ArtifactStatus(ctx, directory)
}
