package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/primitivefield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const (
	sqlitePlannerName      = "ridu-sqlite"
	sqlitePlannerVersionV1 = "1.0.0"
	sqlitePlannerVersion   = "1.1.0"
)

type sqlitePlannerContract struct {
	version               string
	freshSchemaStatements []string
	reconcileIndexes      func(context.Context, sqlRunner, schema.Manifest) error
	rebuildUniqueness     func(context.Context, sqlRunner, schema.Manifest) error
}

type sqlitePlannerContractResolver func(string) (sqlitePlannerContract, bool)

func currentSQLitePlannerContract() sqlitePlannerContract {
	return sqlitePlannerContract{
		version:               sqlitePlannerVersion,
		freshSchemaStatements: append([]string(nil), sqliteSchemaStatements...),
		reconcileIndexes:      reconcileDocumentIndexes,
		rebuildUniqueness:     rebuildUniqueValues,
	}
}

func sqlitePlannerContractFor(version string) (sqlitePlannerContract, bool) {
	switch version {
	case sqlitePlannerVersionV1:
		return sqlitePlannerContract{
			version:               version,
			freshSchemaStatements: append([]string(nil), sqliteSchemaStatements...),
			reconcileIndexes:      reconcileDocumentIndexes,
			rebuildUniqueness:     rebuildLegacyAuthUniqueValues,
		}, true
	case sqlitePlannerVersion:
		return currentSQLitePlannerContract(), true
	default:
		return sqlitePlannerContract{}, false
	}
}

// MigrationStatus describes one immutable SQLite artifact relative to the
// database ledger. SQLite artifacts are atomic, so phases and steps are either
// wholly pending or wholly complete and do not need a checkpoint vocabulary.
type MigrationStatus struct {
	Name     string                 `json:"name"`
	Checksum string                 `json:"checksum"`
	Version  uint32                 `json:"version"`
	Applied  bool                   `json:"applied"`
	Phases   []MigrationPhaseStatus `json:"phases"`
}

// MigrationPhaseStatus reports one transaction phase in an immutable artifact.
type MigrationPhaseStatus struct {
	ID    string                  `json:"id"`
	Mode  ridumigration.PhaseMode `json:"mode"`
	State string                  `json:"state"`
	Steps []MigrationStepStatus   `json:"steps"`
}

// MigrationStepStatus reports one SQLite artifact step.
type MigrationStepStatus struct {
	ID    string                 `json:"id"`
	Kind  ridumigration.StepKind `json:"kind"`
	State string                 `json:"state"`
}

// SafetyError reports a valid SQLite transition that requires explicit
// destructive approval before an immutable artifact can be created.
type SafetyError struct {
	Risks []ridumigration.Risk
}

func (err *SafetyError) Error() string {
	messages := make([]string, len(err.Risks))
	for index, risk := range err.Risks {
		messages[index] = risk.Message
	}
	return "migration requires explicit safety resolution: " + strings.Join(messages, "; ")
}

// CreatedArtifact is the stable filesystem identity of one newly committed
// immutable SQLite migration. It deliberately does not expose Ridu's internal
// artifact-file representation.
type CreatedArtifact struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
	Version  uint32 `json:"version"`
}

// CreateArtifact plans and atomically creates one immutable SQLite migration
// file. Existing files are never overwritten and the new artifact must
// continue the latest committed manifest in directory. The before manifest is
// derived from that history so applications never need to decode private
// artifact files themselves.
func CreateArtifact(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (CreatedArtifact, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return CreatedArtifact{}, err
	}
	if err := validateSQLiteDataTransformIdentities(files, transforms); err != nil {
		return CreatedArtifact{}, err
	}
	var before *schema.Manifest
	previousPlannerVersion := ""
	if len(files) != 0 {
		head := files[len(files)-1].Artifact
		latest, err := head.AfterManifest()
		if err != nil {
			return CreatedArtifact{}, err
		}
		before = &latest
		previousPlannerVersion = head.Planner.Version
	}
	artifact, err := buildSQLiteArtifactWithDataTransformDescriptors(ctx, name, before, after, previousPlannerVersion, currentSQLitePlannerContract(), transforms, len(transforms) != 0)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := requireSQLiteDestructiveApproval(artifact.Risks, allowDestructive); err != nil {
		return CreatedArtifact{}, err
	}
	file, err := migrationartifact.Create(directory, name, artifact, now)
	if err != nil {
		return CreatedArtifact{}, err
	}
	return CreatedArtifact{
		Path: file.Path, Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version,
	}, nil
}

// planArtifact creates an unpublished deterministic SQLite migration plan.
// The generic JSON document layout makes adding resources and optional fields
// manifest-only changes. Initial history installs the fixed adapter schema.
// Transitions that need content rewrites or destructive interpretation are
// rejected until SQLite has a typed executor for them. A non-nil before value
// is assumed to have been produced by the current planner contract; ordinary
// CreateArtifact is the author-facing API: it derives adapter history and
// binds the plan to the exact predecessor before publishing it.
func planArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, allowDestructive bool, transforms ...ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	previousPlannerVersion := ""
	if before != nil {
		previousPlannerVersion = sqlitePlannerVersion
	}
	artifact, err := buildSQLiteArtifactWithDataTransformDescriptors(ctx, name, before, after, previousPlannerVersion, currentSQLitePlannerContract(), transforms, len(transforms) != 0)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	if err := requireSQLiteDestructiveApproval(artifact.Risks, allowDestructive); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func requireSQLiteDestructiveApproval(risks []ridumigration.Risk, allowDestructive bool) error {
	if allowDestructive {
		return nil
	}
	var blocked []ridumigration.Risk
	for _, risk := range risks {
		if risk.Level == ridumigration.RiskDestructive {
			blocked = append(blocked, risk)
		}
	}
	if len(blocked) != 0 {
		return &SafetyError{Risks: blocked}
	}
	return nil
}

func buildSQLiteArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, previousPlannerVersion string, contract sqlitePlannerContract) (ridumigration.Artifact, error) {
	return buildSQLiteArtifactWithValidation(ctx, name, before, after, previousPlannerVersion, contract, validateSQLiteAdditiveTransition)
}

func buildSQLiteArtifactWithValidation(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, previousPlannerVersion string, contract sqlitePlannerContract, validateTransition func(schema.Snapshot, schema.Snapshot) error) (ridumigration.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return ridumigration.Artifact{}, err
	}
	if strings.TrimSpace(contract.version) == "" || len(contract.freshSchemaStatements) == 0 || contract.reconcileIndexes == nil || contract.rebuildUniqueness == nil {
		return ridumigration.Artifact{}, fmt.Errorf("SQLite planner contract is incomplete")
	}
	if before != nil {
		if previousPlannerVersion == "" {
			return ridumigration.Artifact{}, fmt.Errorf("previous SQLite planner version is required for a non-initial artifact")
		}
		fromDigest, err := ridumigration.DigestManifest(*before)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		toDigest, err := ridumigration.DigestManifest(after)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if previousPlannerVersion != contract.version && !sqlitePlannerUpgradeSupported(previousPlannerVersion, contract.version) {
			return ridumigration.Artifact{}, fmt.Errorf("SQLite planner version %q does not match current version %q", previousPlannerVersion, contract.version)
		}
		canonicalUpgrade := sqliteAuthIdentityUpgradeRequired(previousPlannerVersion, contract.version, before.Snapshot(), after.Snapshot())
		if fromDigest == toDigest && !canonicalUpgrade {
			return ridumigration.Artifact{}, fmt.Errorf("schema is current; no SQLite migration steps were planned")
		}
		if fromDigest != toDigest {
			if err := validateTransition(before.Snapshot(), after.Snapshot()); err != nil {
				return ridumigration.Artifact{}, err
			}
		}
	} else if previousPlannerVersion != "" {
		return ridumigration.Artifact{}, fmt.Errorf("initial SQLite artifact cannot have previous planner version %s", previousPlannerVersion)
	}

	artifact, err := ridumigration.NewArtifact(name, ridumigration.Planner{
		Name: sqlitePlannerName, Version: contract.version,
	}, before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}

	var statements []string
	stepName := "install SQLite schema object"
	if before == nil {
		statements = contract.freshSchemaStatements
	}
	steps := make([]ridumigration.Step, 0, len(statements)+1)
	for index, statement := range statements {
		payload, err := ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: statement})
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		steps = append(steps, ridumigration.Step{
			ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepSQL,
			ExecutorVersion: 1, Name: fmt.Sprintf("%s %03d", stepName, index+1), Payload: payload,
		})
	}
	if before != nil && sqliteAuthIdentityUpgradeRequired(previousPlannerVersion, contract.version, before.Snapshot(), after.Snapshot()) {
		payload, err := ridumigration.MarshalStepPayload(ridumigration.CanonicalizeAuthIdentitiesPayload{
			Resources: ridumigration.RetainedAuthIdentityResources(before.Snapshot(), after.Snapshot()),
		})
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		steps = append(steps, ridumigration.Step{
			ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepCanonicalizeAuthIdentities,
			ExecutorVersion: 1, Name: "canonicalize authored authentication identities", Payload: payload,
		})
		artifact.Risks = append(artifact.Risks, ridumigration.Risk{
			Code: "RIDU_AUTH_IDENTITY_CANONICALIZATION", Level: ridumigration.RiskDestructive,
			Message: "irreversibly rewrite authored authentication identities to the shared lowercase-and-trimmed key after a collision preflight; coordinate the migration with application writers",
		})
	}
	assertion, err := ridumigration.MarshalStepPayload(ridumigration.AssertSchemaPayload{})
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	steps = append(steps, ridumigration.Step{
		ID: fmt.Sprintf("step-%04d", len(steps)+1), Kind: ridumigration.StepAssertSchema,
		ExecutorVersion: 1, Name: "verify resulting SQLite schema", Payload: assertion,
	})
	physicalBefore := ridumigration.PhysicalDigestSeed(artifact.FromDigest)
	physicalAfter, err := ridumigration.PhasePhysicalDigest(physicalBefore, ridumigration.PhaseTransaction, steps)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	artifact.Phases = []ridumigration.Phase{{
		ID: "phase-001", Mode: ridumigration.PhaseTransaction,
		PhysicalContractVersion: ridumigration.PhysicalContractVersion,
		BeforePhysicalDigest:    physicalBefore, AfterPhysicalDigest: physicalAfter, Steps: steps,
	}}
	if err := artifact.Validate(); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func sqlitePlannerUpgradeSupported(previous, next string) bool {
	return previous == sqlitePlannerVersionV1 && next == sqlitePlannerVersion
}

func sqliteAuthIdentityUpgradeRequired(previous, next string, before, after schema.Snapshot) bool {
	return sqlitePlannerUpgradeSupported(previous, next) && len(ridumigration.RetainedAuthIdentityResources(before, after)) != 0
}

func validateSQLiteAdditiveTransition(before, after schema.Snapshot) error {
	return validateSQLiteAdditiveSnapshotTransition(sqliteWithoutPresentation(before), sqliteWithoutPresentation(after))
}

// validateSQLiteAdditiveSnapshotTransition also preserves the original planner
// comparison for reconstructing artifacts created before presentation projection.
func validateSQLiteAdditiveSnapshotTransition(before, after schema.Snapshot) error {
	// Caller-supplied ID admission is an operation-layer policy flag. It has no
	// SQLite storage representation, so changing only this setting must not
	// manufacture a physical migration incompatibility.
	currentApplication := after.Application
	currentApplication.AllowIDOnCreate = before.Application.AllowIDOnCreate
	if !reflect.DeepEqual(before.Application, currentApplication) {
		return fmt.Errorf("SQLite artifact planner supports only additive transitions; application settings changed")
	}
	if err := validateSQLiteAdditiveCollections(before.Collections, after.Collections); err != nil {
		return err
	}
	return validateSQLiteAdditiveResources("global", before.Globals, after.Globals)
}

func validateSQLiteAdditiveCollections(before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; collection %q was removed", previous.ID)
		}
		currentWithoutFieldsAndIndexes := current
		currentWithoutFieldsAndIndexes.Fields = previous.Fields
		currentWithoutFieldsAndIndexes.Indexes = previous.Indexes
		if !reflect.DeepEqual(previous, currentWithoutFieldsAndIndexes) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; collection %q changed outside its fields and indexes", previous.ID)
		}
		if len(current.Indexes) < len(previous.Indexes) || !reflect.DeepEqual(previous.Indexes, current.Indexes[:len(previous.Indexes)]) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; collection %q changed or removed an existing index", previous.ID)
		}
		if err := validateSQLiteAdditiveFields(fmt.Sprintf("collection %q", previous.ID), previous.Fields, current.Fields); err != nil {
			return err
		}
	}
	return nil
}

func validateSQLiteAdditiveResources(kind string, before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; %s %q was removed", kind, previous.ID)
		}
		currentWithoutFields := current
		currentWithoutFields.Fields = previous.Fields
		if !reflect.DeepEqual(previous, currentWithoutFields) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; %s %q changed outside its fields", kind, previous.ID)
		}
		location := fmt.Sprintf("%s %q", kind, previous.ID)
		if err := validateSQLiteAdditiveFields(location, previous.Fields, current.Fields); err != nil {
			return err
		}
	}
	return nil
}

func validateSQLiteAdditiveFields(location string, before, after []schema.Field) error {
	afterByID := make(map[schema.StableID]schema.Field, len(after))
	for _, field := range after {
		afterByID[field.ID] = field
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; field %q in %s was removed", previous.ID, location)
		}

		comparison := current
		// Adding an index is physically reconciled from the after manifest.
		// Adding uniqueness is admitted only because the same transaction
		// rebuilds the derived unique-value table and rolls back on duplicates.
		if !previous.Index && comparison.Index {
			comparison.Index = false
		}
		if !previous.Unique && comparison.Unique {
			comparison.Unique = false
		}
		var embeddedErr error
		comparison, embeddedErr = embedded.CompareEvolution(previous, comparison, validateSQLiteAdditiveBlockTypes)
		if embeddedErr != nil {
			return embeddedErr
		}

		if previous.Type != comparison.Type && (primitivefield.IsList(previous) || primitivefield.IsList(comparison)) {
			return fmt.Errorf("field %q changes value shape from %q to %q; add a new field and migrate existing values explicitly, or register a supported compiled data transform; automatic list conversion is not available", previous.Path.String(), previous.Type, comparison.Type)
		}
		if !reflect.DeepEqual(sqliteFieldComparisonMetadata(previous), sqliteFieldComparisonMetadata(comparison)) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; field %q in %s changed", previous.ID, location)
		}
		if previous.Nested != nil {
			if err := validateSQLiteAdditiveFields(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Nested.ResolvedFields(), current.Nested.ResolvedFields()); err != nil {
				return err
			}
		}
		if previous.Blocks != nil {
			if err := validateSQLiteAdditiveBlockTypes(fmt.Sprintf("field %q in %s", previous.ID, location), previous.Blocks.ResolvedTypes(), current.Blocks.ResolvedTypes()); err != nil {
				return err
			}
		}
		delete(afterByID, previous.ID)
	}
	for _, added := range after {
		if _, remains := afterByID[added.ID]; !remains {
			continue
		}
		if added.Required && added.Category != schema.FieldCategoryPresentation {
			return fmt.Errorf("SQLite additive migration cannot add required field %q to existing %s without rewriting existing documents", added.ID, location)
		}
	}
	return nil
}

// Child evolution is checked separately. Compare only container metadata here,
// excluding lazy placement bindings and their definition caches.
func sqliteFieldComparisonMetadata(value schema.Field) schema.Field {
	if n := value.Nested; n != nil {
		value.Nested = &schema.NestedField{MinRows: n.MinRows, MaxRows: n.MaxRows, RowLabel: n.RowLabel, RowLabelComponent: n.RowLabelComponent, RowLabels: n.RowLabels}
	}
	if b := value.Blocks; b != nil {
		value.Blocks = &schema.BlocksField{MinRows: b.MinRows, MaxRows: b.MaxRows}
	}
	return value
}

func validateSQLiteAdditiveBlockTypes(location string, before, after []schema.BlockType) error {
	afterByKey := make(map[string]schema.BlockType, len(after))
	for _, block := range after {
		afterByKey[block.Slug] = block
	}
	for _, previous := range before {
		current, exists := afterByKey[previous.Slug]
		if !exists {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; block type %q in %s was removed", previous.Slug, location)
		}
		// Children are checked below; placement caches and generated type names
		// are not part of the stored block contract.
		if !reflect.DeepEqual(previous.Labels, current.Labels) {
			return fmt.Errorf("SQLite artifact planner supports only additive transitions; block type %q in %s changed", previous.Slug, location)
		}
		if err := validateSQLiteAdditiveFields(fmt.Sprintf("block type %q in %s", previous.Slug, location), previous.ResolvedFields(), current.ResolvedFields()); err != nil {
			return err
		}
		delete(afterByKey, previous.Slug)
	}
	return nil
}

type sqliteArtifactLedgerRow struct {
	position               int
	name                   string
	digest                 string
	previousArtifactDigest string
	fromDigest             string
	toDigest               string
	plannerName            string
	plannerVersion         string
}

const sqliteArtifactLedgerSQL = `CREATE TABLE IF NOT EXISTS ridu_migrations (
  position INTEGER PRIMARY KEY CHECK (position > 0),
  name TEXT NOT NULL UNIQUE,
  artifact_digest TEXT NOT NULL,
  previous_artifact_digest TEXT NOT NULL,
  from_digest TEXT NOT NULL,
  to_digest TEXT NOT NULL,
  planner_name TEXT NOT NULL,
  planner_version TEXT NOT NULL,
  applied_at INTEGER NOT NULL
) STRICT`

// ApplyArtifacts validates the complete committed history before creating a
// ledger or changing application schema. All pending artifacts and their
// ledger rows commit under one BEGIN IMMEDIATE writer reservation.
func (backend *Store) ApplyArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	return backend.applySQLiteArtifacts(ctx, files, sqlitePlannerContractFor, registry)
}

func (backend *Store) applySQLiteArtifacts(ctx context.Context, files []migrationartifact.File, resolve sqlitePlannerContractResolver, transforms sqliteDataTransformRegistry) error {
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration before apply")
	}
	if err := preflightSQLiteArtifactsWithResolver(ctx, files, resolve); err != nil {
		return err
	}
	if err := requireSQLiteDataTransformRegistry(files, transforms); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		exists, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		var applied []sqliteArtifactLedgerRow
		if exists {
			if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
				return err
			}
			applied, err = readSQLiteArtifactLedger(ctx, connection)
			if err != nil {
				return err
			}
			if len(applied) == 0 {
				return fmt.Errorf("SQLite migration ledger exists without an applied artifact")
			}
			if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
				return err
			}
		}
		if err := validateSQLiteAppliedArtifacts(files, applied); err != nil {
			return err
		}
		if len(applied) == 0 {
			if err := assertNoSQLiteManagedSchema(ctx, connection); err != nil {
				return err
			}
		} else {
			appliedManifest, err := files[len(applied)-1].Artifact.AfterManifest()
			if err != nil {
				return err
			}
			contract, err := resolveSQLitePlannerContract(files[len(applied)-1], resolve)
			if err != nil {
				return err
			}
			if err := assertSQLitePhysicalSchema(ctx, connection, appliedManifest, true, contract); err != nil {
				return fmt.Errorf("current SQLite migration state: %w", err)
			}
			if err := assertSQLiteManifestDigest(ctx, connection, files[len(applied)-1].Artifact.ToDigest); err != nil {
				return fmt.Errorf("current SQLite migration state: %w", err)
			}
		}
		expectedHead := files[len(files)-1].Digest
		if len(applied) == len(files) {
			if err := writeSQLiteExpectedArtifactDigest(ctx, connection, expectedHead); err != nil {
				return err
			}
			return assertSQLiteExpectedArtifactDigest(ctx, connection, expectedHead)
		}
		if _, err := connection.ExecContext(ctx, sqliteArtifactLedgerSQL); err != nil {
			return fmt.Errorf("create SQLite migration ledger: %w", translateError(err))
		}
		for index := len(applied); index < len(files); index++ {
			contract, err := resolveSQLitePlannerContract(files[index], resolve)
			if err != nil {
				return err
			}
			if err := backend.applySQLiteArtifact(ctx, connection, files[index], index+1, expectedHead, contract, transforms); err != nil {
				return err
			}
		}
		latestManifest, err := files[len(files)-1].Artifact.AfterManifest()
		if err != nil {
			return err
		}
		latestContract, err := resolveSQLitePlannerContract(files[len(files)-1], resolve)
		if err != nil {
			return err
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, latestManifest, true, latestContract); err != nil {
			return fmt.Errorf("completed SQLite migration state: %w", err)
		}
		if err := assertSQLiteManifestDigest(ctx, connection, files[len(files)-1].Artifact.ToDigest); err != nil {
			return err
		}
		return assertSQLiteExpectedArtifactDigest(ctx, connection, expectedHead)
	})
}

func resolveSQLitePlannerContract(file migrationartifact.File, resolve sqlitePlannerContractResolver) (sqlitePlannerContract, error) {
	contract, supported := resolve(file.Artifact.Planner.Version)
	if !supported {
		return sqlitePlannerContract{}, fmt.Errorf("SQLite migration %s uses unsupported planner version %q", file.Name, file.Artifact.Planner.Version)
	}
	return contract, nil
}

func preflightSQLiteArtifacts(ctx context.Context, files []migrationartifact.File) error {
	return preflightSQLiteArtifactsWithResolver(ctx, files, sqlitePlannerContractFor)
}

func preflightSQLiteArtifactsWithResolver(ctx context.Context, files []migrationartifact.File, resolve sqlitePlannerContractResolver) error {
	if err := validateSQLiteDataTransformIdentities(files, nil); err != nil {
		return err
	}
	previousPlannerVersion := ""
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.Artifact.Planner.Name != sqlitePlannerName {
			return fmt.Errorf("SQLite migration %s uses planner %q instead of %q", file.Name, file.Artifact.Planner.Name, sqlitePlannerName)
		}
		contract, supported := resolve(file.Artifact.Planner.Version)
		if !supported {
			return fmt.Errorf("SQLite migration %s uses unsupported planner version %q", file.Name, file.Artifact.Planner.Version)
		}
		for _, phase := range file.Artifact.Phases {
			if phase.Mode != ridumigration.PhaseTransaction {
				return fmt.Errorf("SQLite migration %s uses unsupported phase mode %q", file.Name, phase.Mode)
			}
			for _, step := range phase.Steps {
				if step.Kind != ridumigration.StepSQL && step.Kind != ridumigration.StepDataTransform && step.Kind != ridumigration.StepCanonicalizeAuthIdentities && step.Kind != ridumigration.StepAssertSchema {
					return fmt.Errorf("SQLite migration %s uses unsupported step kind %q", file.Name, step.Kind)
				}
			}
		}
		after, err := file.Artifact.AfterManifest()
		if err != nil {
			return err
		}
		var before *schema.Manifest
		if file.Artifact.Before != nil {
			manifest, err := file.Artifact.BeforeManifest()
			if err != nil {
				return err
			}
			before = &manifest
		}
		descriptors, err := sqliteArtifactDataTransformDescriptors(file.Artifact)
		if err != nil {
			return err
		}
		expected, err := rebuildSQLiteArtifactWithDataTransformDescriptors(ctx, file.Artifact.Name, before, after, previousPlannerVersion, contract, descriptors, sqliteArtifactAllowsTransformedSchema(file.Artifact))
		if err != nil {
			return fmt.Errorf("validate SQLite migration %s against planner: %w", file.Name, err)
		}
		expected.PreviousArtifactDigest = file.Artifact.PreviousArtifactDigest
		digest, err := expected.Digest()
		if err != nil {
			return err
		}
		if digest != file.Digest {
			return fmt.Errorf("SQLite migration %s does not match planner %s %s", file.Name, sqlitePlannerName, contract.version)
		}
		previousPlannerVersion = contract.version
	}
	return nil
}

func (backend *Store) applySQLiteArtifact(ctx context.Context, connection *sql.Conn, file migrationartifact.File, position int, expectedHead string, contract sqlitePlannerContract, transforms sqliteDataTransformRegistry) error {
	after, err := file.Artifact.AfterManifest()
	if err != nil {
		return err
	}
	var before *schema.Manifest
	if file.Artifact.Before != nil {
		manifest, err := file.Artifact.BeforeManifest()
		if err != nil {
			return err
		}
		before = &manifest
	}
	presentationOnly := sqlitePresentationOnlyArtifact(file.Artifact, before, after)
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			switch step.Kind {
			case ridumigration.StepSQL:
				var payload ridumigration.SQLPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode SQLite migration %s step %s: %w", file.Name, step.ID, err)
				}
				if _, err := connection.ExecContext(ctx, payload.SQL); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, translateError(err))
				}
			case ridumigration.StepDataTransform:
				var payload ridumigration.DataTransformPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode SQLite migration %s step %s: %w", file.Name, step.ID, err)
				}
				if err := backend.executeSQLiteDataTransform(ctx, connection, file, payload.Transform, transforms, false); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
			case ridumigration.StepCanonicalizeAuthIdentities:
				var payload ridumigration.CanonicalizeAuthIdentitiesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return fmt.Errorf("decode SQLite migration %s step %s: %w", file.Name, step.ID, err)
				}
				before, err := file.Artifact.BeforeManifest()
				if err != nil {
					return fmt.Errorf("decode SQLite migration %s before manifest: %w", file.Name, err)
				}
				if err := canonicalizeSQLiteAuthIdentities(ctx, connection, before, after, payload.Resources); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
			case ridumigration.StepAssertSchema:
				if !presentationOnly {
					if err := contract.reconcileIndexes(ctx, connection, after); err != nil {
						return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
					}
					if err := rebuildDocumentReferences(ctx, connection, after); err != nil {
						return fmt.Errorf("apply SQLite migration %s step %s (%s): rebuild references: %w", file.Name, step.ID, step.Name, err)
					}
					if err := contract.rebuildUniqueness(ctx, connection, after); err != nil {
						return fmt.Errorf("apply SQLite migration %s step %s (%s): rebuild uniqueness: %w", file.Name, step.ID, step.Name, err)
					}
				}
				if err := assertSQLitePhysicalSchema(ctx, connection, after, true, contract); err != nil {
					return fmt.Errorf("apply SQLite migration %s step %s (%s): %w", file.Name, step.ID, step.Name, err)
				}
			default:
				return fmt.Errorf("SQLite migration %s uses unsupported step kind %q", file.Name, step.Kind)
			}
		}
	}
	if err := writeSQLiteManifest(ctx, connection, after, expectedHead, backend.now().UTC()); err != nil {
		return fmt.Errorf("record SQLite manifest for migration %s: %w", file.Name, err)
	}
	_, err = connection.ExecContext(ctx, `INSERT INTO ridu_migrations
  (position, name, artifact_digest, previous_artifact_digest, from_digest, to_digest, planner_name, planner_version, applied_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, position, file.Name, file.Digest, file.Artifact.PreviousArtifactDigest, file.Artifact.FromDigest,
		file.Artifact.ToDigest, file.Artifact.Planner.Name, file.Artifact.Planner.Version, encodeTime(backend.now().UTC()))
	if err != nil {
		return fmt.Errorf("record SQLite migration %s: %w", file.Name, translateError(err))
	}
	return nil
}

func writeSQLiteManifest(ctx context.Context, runner sqlRunner, manifest schema.Manifest, expectedArtifactDigest string, now time.Time) error {
	digest, err := manifestDigest(manifest)
	if err != nil {
		return err
	}
	encoded, err := manifest.Bytes()
	if err != nil {
		return err
	}
	_, err = runner.ExecContext(ctx, `INSERT INTO ridu_sqlite_schema
  (singleton, manifest_digest, expected_artifact_digest, manifest_json, applied_at)
VALUES (1, ?, ?, ?, ?)
ON CONFLICT(singleton) DO UPDATE SET
  manifest_digest = excluded.manifest_digest,
  expected_artifact_digest = excluded.expected_artifact_digest,
  manifest_json = excluded.manifest_json,
  applied_at = excluded.applied_at`, digest, expectedArtifactDigest, string(encoded), encodeTime(now))
	return translateError(err)
}

func writeSQLiteExpectedArtifactDigest(ctx context.Context, runner sqlRunner, expected string) error {
	result, err := runner.ExecContext(ctx, `UPDATE ridu_sqlite_schema SET expected_artifact_digest = ? WHERE singleton = 1`, expected)
	if err != nil {
		return fmt.Errorf("record expected SQLite artifact head: %w", translateError(err))
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record expected SQLite artifact head: %w", translateError(err))
	}
	if updated != 1 {
		return fmt.Errorf("SQLite schema ledger is missing")
	}
	return nil
}

func sqliteArtifactLedgerExists(ctx context.Context, runner sqlRunner) (bool, error) {
	var count int
	err := runner.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_migrations'`).Scan(&count)
	return count == 1, translateError(err)
}

func readSQLiteArtifactLedger(ctx context.Context, runner sqlRunner) ([]sqliteArtifactLedgerRow, error) {
	rows, err := runner.QueryContext(ctx, `SELECT position, name, artifact_digest, previous_artifact_digest, from_digest, to_digest,
	  planner_name, planner_version FROM ridu_migrations ORDER BY position`)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var result []sqliteArtifactLedgerRow
	for rows.Next() {
		var row sqliteArtifactLedgerRow
		if err := rows.Scan(&row.position, &row.name, &row.digest, &row.previousArtifactDigest, &row.fromDigest, &row.toDigest, &row.plannerName, &row.plannerVersion); err != nil {
			return nil, translateError(err)
		}
		if row.position != len(result)+1 {
			return nil, fmt.Errorf("SQLite migration ledger position %d is not continuous after %d", row.position, len(result))
		}
		result = append(result, row)
	}
	return result, translateError(rows.Err())
}

func validateSQLiteAppliedArtifacts(files []migrationartifact.File, applied []sqliteArtifactLedgerRow) error {
	if len(applied) > len(files) {
		return fmt.Errorf("database contains %d applied SQLite migrations but the directory contains only %d", len(applied), len(files))
	}
	for index, row := range applied {
		file := files[index]
		if row.name != file.Name {
			return fmt.Errorf("SQLite migration history diverged at %s; database records %s", file.Name, row.name)
		}
		if row.digest != file.Digest {
			return fmt.Errorf("SQLite migration %s changed after application", file.Name)
		}
		if row.previousArtifactDigest != file.Artifact.PreviousArtifactDigest {
			return fmt.Errorf("SQLite migration %s predecessor differs from the database ledger", file.Name)
		}
		if row.fromDigest != file.Artifact.FromDigest || row.toDigest != file.Artifact.ToDigest {
			return fmt.Errorf("SQLite migration %s manifest lineage differs from the database ledger", file.Name)
		}
		if row.plannerName != file.Artifact.Planner.Name || row.plannerVersion != file.Artifact.Planner.Version {
			return fmt.Errorf("SQLite migration %s planner provenance differs from the database ledger", file.Name)
		}
	}
	return nil
}

func validateSQLiteArtifactLedgerContinuity(applied []sqliteArtifactLedgerRow) error {
	for index, row := range applied {
		if index == 0 {
			if row.previousArtifactDigest != "" {
				return fmt.Errorf("SQLite migration ledger initial artifact has a predecessor")
			}
			if row.fromDigest != "" {
				return fmt.Errorf("SQLite migration ledger does not start from an empty manifest")
			}
			continue
		}
		previous := applied[index-1]
		if row.name <= previous.name {
			return fmt.Errorf("SQLite migration ledger is reordered at %s after %s", row.name, previous.name)
		}
		if row.previousArtifactDigest != previous.digest {
			return fmt.Errorf("SQLite migration ledger artifact history is discontinuous between %s and %s", previous.name, row.name)
		}
		if row.fromDigest != previous.toDigest {
			return fmt.Errorf("SQLite migration ledger manifest history is discontinuous between %s and %s", previous.name, row.name)
		}
	}
	return nil
}

// verifyImmutableReadyState checks the schema ledger digest and physical shape
// in one read transaction. Immutable databases additionally verify artifact
// history; development databases intentionally have no ridu_migrations table.
func (backend *Store) verifyImmutableReadyState(ctx context.Context, manifest schema.Manifest) error {
	return backend.verifyImmutableReadyStateWithResolver(ctx, manifest, sqlitePlannerContractFor)
}

func (backend *Store) verifyImmutableReadyStateWithHistory(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest string) error {
	return backend.verifyImmutableReadyStateWithResolverAndHistory(ctx, manifest, sqlitePlannerContractFor, &expectedHistoryDigest)
}

func (backend *Store) verifyImmutableReadyStateWithResolver(ctx context.Context, manifest schema.Manifest, resolve sqlitePlannerContractResolver) error {
	return backend.verifyImmutableReadyStateWithResolverAndHistory(ctx, manifest, resolve, nil)
}

func (backend *Store) verifyImmutableReadyStateWithResolverAndHistory(
	ctx context.Context,
	manifest schema.Manifest,
	resolve sqlitePlannerContractResolver,
	expectedHistoryDigest *string,
) error {
	connection, err := backend.db.Conn(ctx)
	if err != nil {
		return translateError(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN"); err != nil {
		return translateError(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	exists, err := sqliteArtifactLedgerExists(ctx, connection)
	if err != nil {
		return err
	}
	expected, err := manifestDigest(manifest)
	if err != nil {
		return fmt.Errorf("digest executable SQLite manifest: %w", err)
	}
	if err := assertSQLiteManifestDigest(ctx, connection, expected); err != nil {
		return err
	}
	if !exists {
		if expectedHistoryDigest != nil {
			return fmt.Errorf("SQLite migration ledger is missing for executable migration history")
		}
		expectedArtifactDigest, err := readSQLiteExpectedArtifactDigest(ctx, connection)
		if err != nil {
			return err
		}
		if expectedArtifactDigest != "" {
			return fmt.Errorf("SQLite schema ledger expects artifact digest %s but the migration ledger is missing", expectedArtifactDigest)
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, manifest, false, currentSQLitePlannerContract()); err != nil {
			return fmt.Errorf("SQLite development readiness physical schema: %w", err)
		}
		return nil
	}
	if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
		return err
	}
	applied, err := readSQLiteArtifactLedger(ctx, connection)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return fmt.Errorf("SQLite migration ledger exists without an applied artifact")
	}
	if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
		return err
	}
	if expectedHistoryDigest != nil {
		if err := validateSQLiteMigrationHistory(applied, *expectedHistoryDigest); err != nil {
			return err
		}
	}
	head := applied[len(applied)-1]
	if head.toDigest != expected {
		return fmt.Errorf("SQLite migration ledger head %s digest %s does not match executable digest %s", head.name, head.toDigest, expected)
	}
	if err := assertSQLiteExpectedArtifactDigest(ctx, connection, head.digest); err != nil {
		return fmt.Errorf("SQLite migration ledger head %s: %w", head.name, err)
	}
	if head.plannerName != sqlitePlannerName {
		return fmt.Errorf("SQLite migration ledger head %s uses planner %q instead of %q", head.name, head.plannerName, sqlitePlannerName)
	}
	if head.plannerVersion == sqlitePlannerVersionV1 && len(ridumigration.AuthIdentityResources(manifest.Snapshot())) != 0 {
		return fmt.Errorf("SQLite authentication identities require the planner %s canonicalization artifact", sqlitePlannerVersion)
	}
	contract, supported := resolve(head.plannerVersion)
	if !supported {
		return fmt.Errorf("SQLite migration ledger head %s uses unsupported planner version %q", head.name, head.plannerVersion)
	}
	if err := assertSQLitePhysicalSchema(ctx, connection, manifest, true, contract); err != nil {
		return fmt.Errorf("SQLite migration readiness physical schema: %w", err)
	}
	return nil
}

func validateSQLiteMigrationHistory(applied []sqliteArtifactLedgerRow, expected string) error {
	identities := make([]ridumigration.ArtifactIdentity, len(applied))
	for index, row := range applied {
		identities[index] = ridumigration.ArtifactIdentity{Name: row.name, Digest: row.digest}
	}
	actual, err := ridumigration.DigestArtifactHistory(identities)
	if err != nil {
		return fmt.Errorf("digest applied SQLite migration history: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("applied SQLite migration history digest %s does not match executable history digest %s", actual, expected)
	}
	return nil
}

// ArtifactStatus binds immutable history to the executable schema, then uses
// that exact history snapshot to report the atomic applied/pending boundary.
func (backend *Store) ArtifactStatus(ctx context.Context, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	files, err := migrationartifact.RequireCurrentHistory(directory, executableManifest)
	if err != nil {
		return nil, err
	}
	return backend.artifactStatusWithResolver(ctx, files, sqlitePlannerContractFor)
}

// InspectArtifacts reports immutable migration state without creating or
// changing the selected SQLite database. Missing files have an all-pending
// state; existing files are opened read-only without adapter write pragmas.
func InspectArtifacts(ctx context.Context, path, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	files, err := migrationartifact.RequireCurrentHistory(directory, executableManifest)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial migration before status")
	}
	if err := preflightSQLiteArtifactsWithResolver(ctx, files, sqlitePlannerContractFor); err != nil {
		return nil, err
	}
	backend, exists, err := openSQLiteArtifactInspection(ctx, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return sqliteMigrationStatuses(files, 0), nil
	}
	defer backend.Close()
	return backend.artifactStatusWithResolver(ctx, files, sqlitePlannerContractFor)
}

func openSQLiteArtifactInspection(ctx context.Context, path string) (*Store, bool, error) {
	dsn, filePath, memory, err := sqliteDSN(path)
	if err != nil {
		return nil, false, err
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, false, fmt.Errorf("parse SQLite inspection path: %w", err)
	}
	parameters := parsed.Query()
	if err := rejectProtectedSQLiteParameters(parameters); err != nil {
		return nil, false, err
	}
	if memory {
		return nil, false, nil
	}
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("inspect SQLite target: %w", err)
	}
	parameters.Set("mode", "ro")
	parameters.Set("_query_only", "1")
	parsed.RawQuery = parameters.Encode()
	database, err := sql.Open("sqlite", parsed.String())
	if err != nil {
		return nil, false, fmt.Errorf("configure read-only SQLite inspection: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, false, fmt.Errorf("connect read-only SQLite inspection: %w", sanitizeSQLiteError(err))
	}
	return &Store{db: database}, true, nil
}

func (backend *Store) artifactStatusWithResolver(ctx context.Context, files []migrationartifact.File, resolve sqlitePlannerContractResolver) ([]MigrationStatus, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty; create and commit an initial migration before status")
	}
	if err := preflightSQLiteArtifactsWithResolver(ctx, files, resolve); err != nil {
		return nil, err
	}
	connection, err := backend.db.Conn(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN"); err != nil {
		return nil, translateError(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	exists, err := sqliteArtifactLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	var applied []sqliteArtifactLedgerRow
	if exists {
		if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
			return nil, err
		}
		applied, err = readSQLiteArtifactLedger(ctx, connection)
		if err != nil {
			return nil, err
		}
		if len(applied) == 0 {
			return nil, fmt.Errorf("SQLite migration ledger exists without an applied artifact")
		}
		if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
			return nil, err
		}
	}
	if err := validateSQLiteAppliedArtifacts(files, applied); err != nil {
		return nil, err
	}
	if len(applied) == 0 {
		if err := assertNoSQLiteManagedSchema(ctx, connection); err != nil {
			return nil, err
		}
	} else {
		appliedManifest, err := files[len(applied)-1].Artifact.AfterManifest()
		if err != nil {
			return nil, err
		}
		contract, err := resolveSQLitePlannerContract(files[len(applied)-1], resolve)
		if err != nil {
			return nil, err
		}
		if err := assertSQLitePhysicalSchema(ctx, connection, appliedManifest, true, contract); err != nil {
			return nil, fmt.Errorf("SQLite migration status physical schema: %w", err)
		}
		if err := assertSQLiteManifestDigest(ctx, connection, files[len(applied)-1].Artifact.ToDigest); err != nil {
			return nil, fmt.Errorf("SQLite migration status manifest: %w", err)
		}
	}
	return sqliteMigrationStatuses(files, len(applied)), nil
}

func sqliteMigrationStatuses(files []migrationartifact.File, appliedCount int) []MigrationStatus {
	statuses := make([]MigrationStatus, len(files))
	for index, file := range files {
		state := "pending"
		if index < appliedCount {
			state = "complete"
		}
		status := MigrationStatus{Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version, Applied: index < appliedCount}
		for _, phase := range file.Artifact.Phases {
			phaseStatus := MigrationPhaseStatus{ID: phase.ID, Mode: phase.Mode, State: state}
			for _, step := range phase.Steps {
				phaseStatus.Steps = append(phaseStatus.Steps, MigrationStepStatus{ID: step.ID, Kind: step.Kind, State: state})
			}
			status.Phases = append(status.Phases, phaseStatus)
		}
		statuses[index] = status
	}
	return statuses
}

// ArtifactPlan returns the same manifest-bound immutable SQLite topology as status.
func (backend *Store) ArtifactPlan(ctx context.Context, directory string, executableManifest schema.Manifest) ([]MigrationStatus, error) {
	return backend.ArtifactStatus(ctx, directory, executableManifest)
}

// VerifyArtifacts replays complete history into an isolated temporary SQLite
// database and proves that every artifact and the latest manifest are complete.
func VerifyArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	return verifySQLiteArtifacts(ctx, files, registry)
}

func verifySQLiteArtifacts(ctx context.Context, files []migrationartifact.File, transforms sqliteDataTransformRegistry) error {
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration before verification")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "ridu-sqlite-shadow-")
	if err != nil {
		return fmt.Errorf("create SQLite shadow directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	shadow, err := Open(ctx, filepath.Join(temporary, "shadow.sqlite"))
	if err != nil {
		return fmt.Errorf("open SQLite shadow database: %w", err)
	}
	defer shadow.Close()
	if err := shadow.applySQLiteArtifacts(ctx, files, sqlitePlannerContractFor, transforms); err != nil {
		return fmt.Errorf("verify SQLite migrations in shadow database: %w", err)
	}
	statuses, err := shadow.artifactStatusWithResolver(ctx, files, sqlitePlannerContractFor)
	if err != nil {
		return fmt.Errorf("verify completed SQLite shadow migration state: %w", err)
	}
	if len(statuses) != len(files) {
		return fmt.Errorf("SQLite shadow replay reported %d of %d migration artifacts", len(statuses), len(files))
	}
	for _, status := range statuses {
		if !status.Applied {
			return fmt.Errorf("SQLite shadow replay left migration %s pending", status.Name)
		}
	}
	latest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		return err
	}
	if err := shadow.Ready(ctx, latest); err != nil {
		return fmt.Errorf("verify SQLite shadow readiness: %w", err)
	}
	return nil
}

func withSQLiteMigrationShadow(ctx context.Context, action func(*sql.Conn) error) error {
	shadow, err := Open(ctx, ":memory:")
	if err != nil {
		return err
	}
	defer shadow.Close()
	return shadow.withImmediate(ctx, action)
}

func expectedSQLiteObjects(ctx context.Context, manifest *schema.Manifest, includeArtifactLedger bool, contract sqlitePlannerContract) (map[string]string, error) {
	var objects map[string]string
	err := withSQLiteMigrationShadow(ctx, func(database *sql.Conn) error {
		if includeArtifactLedger {
			if _, err := database.ExecContext(ctx, sqliteArtifactLedgerSQL); err != nil {
				return err
			}
		}
		for _, statement := range contract.freshSchemaStatements {
			if _, err := database.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		if manifest != nil {
			if contract.reconcileIndexes == nil {
				return fmt.Errorf("SQLite planner contract %q has no index reconciler", contract.version)
			}
			if err := contract.reconcileIndexes(ctx, database, *manifest); err != nil {
				return err
			}
		}
		var readError error
		objects, readError = readSQLiteObjects(ctx, database, manifest)
		return readError
	})
	return objects, err
}

type sqliteSchemaObject struct {
	objectType string
	name       string
	table      string
	statement  string
}

func readAllSQLiteSchemaObjects(ctx context.Context, runner sqlRunner) (map[string]sqliteSchemaObject, error) {
	rows, err := runner.QueryContext(ctx, `SELECT type, name, tbl_name, sql FROM sqlite_master
WHERE sql IS NOT NULL ORDER BY type, name`)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	objects := make(map[string]sqliteSchemaObject)
	for rows.Next() {
		var object sqliteSchemaObject
		if err := rows.Scan(&object.objectType, &object.name, &object.table, &object.statement); err != nil {
			return nil, translateError(err)
		}
		object.statement = normalizeSQLiteSchemaSQL(object.statement)
		objects[object.objectType+":"+object.name] = object
	}
	return objects, translateError(rows.Err())
}

func readSQLiteObjects(ctx context.Context, runner sqlRunner, manifest *schema.Manifest) (map[string]string, error) {
	all, err := readAllSQLiteSchemaObjects(ctx, runner)
	if err != nil {
		return nil, err
	}
	objects := make(map[string]string)
	for key, object := range all {
		if !strings.HasPrefix(object.name, "ridu_") && !strings.HasPrefix(object.table, "ridu_") {
			continue
		}
		objects[key] = object.statement
	}
	return objects, nil
}

func normalizeSQLiteSchemaSQL(statement string) string {
	var normalized strings.Builder
	normalized.Grow(len(statement))
	pendingSpace := false
	quote := byte(0)
	lineComment := false
	blockComment := false
	for index := 0; index < len(statement); index++ {
		current := statement[index]
		if lineComment {
			normalized.WriteByte(current)
			if current == '\n' || current == '\r' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			normalized.WriteByte(current)
			if current == '*' && index+1 < len(statement) && statement[index+1] == '/' {
				normalized.WriteByte('/')
				index++
				blockComment = false
			}
			continue
		}
		if quote != 0 {
			normalized.WriteByte(current)
			if quote == '[' {
				if current == ']' {
					quote = 0
				}
				continue
			}
			if current == quote {
				if index+1 < len(statement) && statement[index+1] == quote {
					normalized.WriteByte(statement[index+1])
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if isSQLiteSchemaSpace(current) {
			pendingSpace = normalized.Len() != 0
			continue
		}
		if pendingSpace {
			normalized.WriteByte(' ')
			pendingSpace = false
		}
		if current == '-' && index+1 < len(statement) && statement[index+1] == '-' {
			normalized.WriteString("--")
			index++
			lineComment = true
			continue
		}
		if current == '/' && index+1 < len(statement) && statement[index+1] == '*' {
			normalized.WriteString("/*")
			index++
			blockComment = true
			continue
		}
		normalized.WriteByte(current)
		if current == '\'' || current == '"' || current == '`' || current == '[' {
			quote = current
		}
	}
	return normalized.String()
}

func isSQLiteSchemaSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func validateSQLiteArtifactLedgerShape(ctx context.Context, runner sqlRunner) error {
	expected, err := expectedSQLiteObjects(ctx, nil, true, currentSQLitePlannerContract())
	if err != nil {
		return err
	}
	var statement string
	if err := runner.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'ridu_migrations'`).Scan(&statement); err != nil {
		return fmt.Errorf("read SQLite migration ledger shape: %w", translateError(err))
	}
	if normalizeSQLiteSchemaSQL(statement) != expected["table:ridu_migrations"] {
		return fmt.Errorf("SQLite migration ledger is malformed")
	}
	return nil
}

func assertNoSQLiteManagedSchema(ctx context.Context, runner sqlRunner) error {
	rows, err := runner.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE name GLOB 'ridu_*' ORDER BY name`)
	if err != nil {
		return translateError(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return translateError(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return translateError(err)
	}
	if len(names) != 0 {
		return fmt.Errorf("unmanaged SQLite Ridu schema exists without migration history: %s", strings.Join(names, ", "))
	}
	return nil
}

func assertSQLitePhysicalSchema(ctx context.Context, runner sqlRunner, manifest schema.Manifest, includeArtifactLedger bool, contract sqlitePlannerContract) error {
	expected, err := expectedSQLiteObjects(ctx, &manifest, includeArtifactLedger, contract)
	if err != nil {
		return err
	}
	actual, err := readSQLiteObjects(ctx, runner, &manifest)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(expected, actual) {
		return nil
	}
	keys := make([]string, 0, len(expected)+len(actual))
	seen := make(map[string]bool, len(expected)+len(actual))
	for key := range expected {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range actual {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		expectedSQL, expectedExists := expected[key]
		actualSQL, actualExists := actual[key]
		switch {
		case !actualExists:
			return fmt.Errorf("SQLite physical schema drift: required object %s is missing", key)
		case !expectedExists:
			if strings.HasPrefix(key, "index:") {
				name := strings.TrimPrefix(key, "index:")
				ordinary, err := sqliteUnexpectedIndexIsOrdinary(ctx, runner, name)
				if err != nil {
					return err
				}
				if ordinary && !sqliteObjectNameIsReserved(name) {
					continue
				}
			}
			return fmt.Errorf("SQLite physical schema drift: unexpected behavior-changing object %s exists", key)
		case actualSQL != expectedSQL:
			return fmt.Errorf("SQLite physical schema drift: object %s differs from the adapter contract", key)
		}
	}
	return nil
}

func sqliteObjectNameIsReserved(name string) bool {
	const prefix = "ridu_"
	return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
}

func sqliteUnexpectedIndexIsOrdinary(ctx context.Context, runner sqlRunner, name string) (bool, error) {
	var table string
	if err := runner.QueryRowContext(ctx, `SELECT tbl_name FROM main.sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&table); err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	var unique, partial int
	if err := runner.QueryRowContext(ctx, `SELECT "unique", partial FROM pragma_index_list(?, 'main') WHERE name = ?`, table, name).Scan(&unique, &partial); err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	if unique != 0 || partial != 0 {
		return false, nil
	}
	rows, err := runner.QueryContext(ctx, `SELECT cid, coll, "key" FROM pragma_index_xinfo(?, 'main')`, name)
	if err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	defer rows.Close()
	foundKey := false
	for rows.Next() {
		var cid, key int
		var collation sql.NullString
		if err := rows.Scan(&cid, &collation, &key); err != nil {
			return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
		}
		if key == 0 {
			continue
		}
		foundKey = true
		if cid < 0 || !sqliteBuiltInIndexCollation(collation.String) {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("inspect unexpected SQLite index %q: %w", name, translateError(err))
	}
	return foundKey, nil
}

func sqliteBuiltInIndexCollation(value string) bool {
	switch strings.ToUpper(value) {
	case "BINARY", "NOCASE", "RTRIM":
		return true
	default:
		return false
	}
}

func assertSQLiteManifestDigest(ctx context.Context, runner sqlRunner, expected string) error {
	var actual string
	err := runner.QueryRowContext(ctx, `SELECT manifest_digest FROM ridu_sqlite_schema WHERE singleton = 1`).Scan(&actual)
	if err == sql.ErrNoRows || isNoSuchTable(err) {
		return fmt.Errorf("SQLite schema ledger is missing")
	}
	if err != nil {
		return fmt.Errorf("read SQLite schema ledger: %w", translateError(err))
	}
	if actual != expected {
		return fmt.Errorf("database manifest digest %s does not match executable digest %s", actual, expected)
	}
	return nil
}

func readSQLiteExpectedArtifactDigest(ctx context.Context, runner sqlRunner) (string, error) {
	var actual string
	err := runner.QueryRowContext(ctx, `SELECT expected_artifact_digest FROM ridu_sqlite_schema WHERE singleton = 1`).Scan(&actual)
	if err == sql.ErrNoRows || isNoSuchTable(err) {
		return "", fmt.Errorf("SQLite schema ledger is missing")
	}
	if err != nil {
		return "", fmt.Errorf("read expected SQLite artifact head: %w", translateError(err))
	}
	return actual, nil
}

func assertSQLiteExpectedArtifactDigest(ctx context.Context, runner sqlRunner, expected string) error {
	actual, err := readSQLiteExpectedArtifactDigest(ctx, runner)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("expected artifact digest %s does not match applied artifact digest %s", actual, expected)
	}
	return nil
}
