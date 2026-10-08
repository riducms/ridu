package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/riducms/ridu/internal/datatransform"
	"github.com/riducms/ridu/internal/enableversions"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func buildSQLiteArtifactWithDataTransformDescriptors(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, transforms []ridumigration.DataTransformDescriptor, allowTransformedSchema bool, existing enableversions.Choices) (ridumigration.Artifact, error) {
	if len(transforms) != 0 && len(existing) != 0 {
		return ridumigration.Artifact{}, fmt.Errorf("enable versions in a SQLite migration of its own; it cannot share one with data transforms")
	}
	if len(transforms) != 0 && before != nil {
		fromDigest, err := ridumigration.DigestManifest(*before)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		toDigest, err := ridumigration.DigestManifest(after)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if fromDigest == toDigest {
			artifact, err := buildSQLiteDataOnlyArtifact(name, *before, after)
			if err != nil {
				return ridumigration.Artifact{}, err
			}
			return bindSQLiteDataTransforms(artifact, transforms)
		}
	}
	artifact, err := buildSQLiteArtifactWithValidation(ctx, name, before, after, validateSQLiteAdditiveTransition, nil, existing)
	if err != nil {
		if len(transforms) == 0 || !allowTransformedSchema {
			return ridumigration.Artifact{}, err
		}
		artifact, err = buildSQLiteArtifactWithValidation(ctx, name, before, after, validateSQLiteTransformedTransition, nil, existing)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		artifact.Risks = append(artifact.Risks, ridumigration.Risk{
			Code: "RIDU_SQLITE_TRANSFORMED_SCHEMA_CHANGE", Level: ridumigration.RiskDestructive,
			Message: "the compiled data transform is responsible for preserving or deliberately rewriting content across this otherwise unsupported SQLite schema change",
		})
		if err := artifact.Validate(); err != nil {
			return ridumigration.Artifact{}, err
		}
	}
	return bindSQLiteDataTransforms(artifact, transforms)
}

// validateSQLiteTransformedTransition relaxes only the existing-resource field
// shape rules that a bounded current-document callback can address. Application
// and resource behavior remains on the ordinary fail-closed contract because a
// callback cannot repair framework-owned auth, upload, version, task, lock, or
// preference state. Versioned field changes remain unsupported until callbacks
// can rewrite retained snapshots atomically as well as current documents.
func validateSQLiteTransformedTransition(before, after schema.Snapshot) error {
	before, after = sqliteStorageSchema(before), sqliteStorageSchema(after)
	currentApplication := after.Application
	currentApplication.AllowIDOnCreate = before.Application.AllowIDOnCreate
	if !reflect.DeepEqual(before.Application, currentApplication) {
		return fmt.Errorf("SQLite data transforms cannot change application settings")
	}
	if err := validateSQLiteTransformedCollections(before.Collections, after.Collections); err != nil {
		return err
	}
	return validateSQLiteTransformedResources("global", before.Globals, after.Globals)
}

func validateSQLiteTransformedCollections(before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite data transforms cannot retire collection %q; use the typed resource-retirement migration path", previous.ID)
		}
		comparison := current
		comparison.Fields = previous.Fields
		comparison.Indexes = previous.Indexes
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("SQLite data transforms cannot change collection %q outside its fields and additive indexes", previous.ID)
		}
		if len(current.Indexes) < len(previous.Indexes) || !reflect.DeepEqual(previous.Indexes, current.Indexes[:len(previous.Indexes)]) {
			return fmt.Errorf("SQLite data transforms cannot change or remove an existing index on collection %q", previous.ID)
		}
		if previous.Versions != nil && !schema.EqualFields(previous.Fields, current.Fields) {
			return fmt.Errorf("SQLite data transforms cannot change fields on versioned collection %q until retained snapshots can be rewritten atomically", previous.ID)
		}
	}
	return nil
}

func validateSQLiteTransformedResources(kind string, before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("SQLite data transforms cannot retire %s %q; use the typed resource-retirement migration path", kind, previous.ID)
		}
		comparison := current
		comparison.Fields = previous.Fields
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("SQLite data transforms cannot change %s %q outside its fields", kind, previous.ID)
		}
		if previous.Versions != nil && !schema.EqualFields(previous.Fields, current.Fields) {
			return fmt.Errorf("SQLite data transforms cannot change fields on versioned %s %q until retained snapshots can be rewritten atomically", kind, previous.ID)
		}
	}
	return nil
}

func sqliteArtifactAllowsTransformedSchema(artifact ridumigration.Artifact) bool {
	for _, risk := range artifact.Risks {
		if risk.Code == "RIDU_SQLITE_TRANSFORMED_SCHEMA_CHANGE" && risk.Level == ridumigration.RiskDestructive {
			return true
		}
	}
	return false
}

func buildSQLiteDataOnlyArtifact(name string, before, after schema.Manifest) (ridumigration.Artifact, error) {
	artifact, err := ridumigration.NewArtifact(name, ridumigration.Planner{Name: sqlitePlannerName, Version: sqlitePlannerVersion}, &before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	payload, err := ridumigration.MarshalStepPayload(ridumigration.AssertSchemaPayload{})
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	steps := []ridumigration.Step{{
		ID: "step-0001", Kind: ridumigration.StepAssertSchema, ExecutorVersion: 1,
		Name: "verify resulting SQLite schema", Payload: payload,
	}}
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

func bindSQLiteDataTransforms(artifact ridumigration.Artifact, transforms []ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	if len(transforms) == 0 {
		return artifact, nil
	}
	seen := make(map[string]struct{}, len(transforms))
	for _, transform := range transforms {
		if err := transform.Validate(); err != nil {
			return ridumigration.Artifact{}, err
		}
		if _, duplicate := seen[transform.Name]; duplicate {
			return ridumigration.Artifact{}, fmt.Errorf("data transform %q is bound more than once", transform.Name)
		}
		seen[transform.Name] = struct{}{}
	}
	if len(artifact.Phases) != 1 || artifact.Phases[0].Mode != ridumigration.PhaseTransaction {
		return ridumigration.Artifact{}, fmt.Errorf("SQLite data transforms require one transaction phase")
	}
	phase := &artifact.Phases[0]
	if len(phase.Steps) == 0 || phase.Steps[len(phase.Steps)-1].Kind != ridumigration.StepAssertSchema {
		return ridumigration.Artifact{}, fmt.Errorf("SQLite data transforms require a final schema assertion")
	}
	// Transforms run before the required-value audit, so it sees the values
	// they write, and otherwise immediately before the schema assertion.
	insertion := len(phase.Steps) - 1
	for index, step := range phase.Steps {
		if step.Kind == ridumigration.StepAuditRequiredValues {
			insertion = index
			break
		}
	}
	steps := append([]ridumigration.Step(nil), phase.Steps[:insertion]...)
	for _, transform := range transforms {
		payload, err := ridumigration.MarshalStepPayload(ridumigration.DataTransformPayload{Transform: transform})
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		steps = append(steps, ridumigration.Step{
			Kind: ridumigration.StepDataTransform, ExecutorVersion: 1,
			Name: "run data transform " + transform.Name, Payload: payload,
		})
	}
	steps = append(steps, phase.Steps[insertion:]...)
	for index := range steps {
		steps[index].ID = fmt.Sprintf("step-%04d", index+1)
	}
	phase.Steps = steps
	physicalAfter, err := ridumigration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	phase.AfterPhysicalDigest = physicalAfter
	if err := artifact.Validate(); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func sqliteArtifactDataTransformDescriptors(artifact ridumigration.Artifact) ([]ridumigration.DataTransformDescriptor, error) {
	var result []ridumigration.DataTransformDescriptor
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepDataTransform {
				continue
			}
			var payload ridumigration.DataTransformPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode data transform step %s: %w", step.ID, err)
			}
			result = append(result, payload.Transform)
		}
	}
	return result, nil
}

func validateSQLiteDataTransformIdentities(files []migrationartifact.File, additional []ridumigration.DataTransformDescriptor) error {
	checksums := make(map[string]string)
	validate := func(descriptors []ridumigration.DataTransformDescriptor) error {
		for _, descriptor := range descriptors {
			if previous, exists := checksums[descriptor.Name]; exists && previous != descriptor.Checksum {
				return fmt.Errorf("SQLite data transform %q changed checksum after immutable history; use a new transform name", descriptor.Name)
			}
			checksums[descriptor.Name] = descriptor.Checksum
		}
		return nil
	}
	for _, file := range files {
		descriptors, err := sqliteArtifactDataTransformDescriptors(file.Artifact)
		if err != nil {
			return fmt.Errorf("inspect SQLite migration %s data transforms: %w", file.Name, err)
		}
		if err := validate(descriptors); err != nil {
			return err
		}
	}
	return validate(additional)
}

type sqliteDataTransformRegistry map[string]ridumigration.DataTransform

func newSQLiteDataTransformRegistry(transforms []ridumigration.DataTransform) (sqliteDataTransformRegistry, error) {
	registry := make(sqliteDataTransformRegistry, len(transforms))
	for _, transform := range transforms {
		if err := transform.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := registry[transform.Name]; duplicate {
			return nil, fmt.Errorf("data transform %q is registered more than once", transform.Name)
		}
		registry[transform.Name] = transform
	}
	return registry, nil
}

func requireSQLiteDataTransformRegistry(files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	for _, file := range files {
		descriptors, err := sqliteArtifactDataTransformDescriptors(file.Artifact)
		if err != nil {
			return fmt.Errorf("inspect SQLite migration %s data transforms: %w", file.Name, err)
		}
		for _, descriptor := range descriptors {
			registered, exists := registry[descriptor.Name]
			if !exists {
				return fmt.Errorf("SQLite migration %s requires unregistered data transform %q", file.Name, descriptor.Name)
			}
			if registered.Checksum != descriptor.Checksum {
				return fmt.Errorf("SQLite migration %s data transform %q checksum differs from the immutable artifact", file.Name, descriptor.Name)
			}
		}
	}
	return nil
}

func (backend *Store) executeSQLiteDataTransforms(ctx context.Context, connection *sql.Conn, file migrationartifact.File, registry sqliteDataTransformRegistry, down bool) error {
	descriptors, err := sqliteArtifactDataTransformDescriptors(file.Artifact)
	if err != nil {
		return err
	}
	if down {
		for left, right := 0, len(descriptors)-1; left < right; left, right = left+1, right-1 {
			descriptors[left], descriptors[right] = descriptors[right], descriptors[left]
		}
	}
	transaction := newSQLiteMigrationDataTransaction(backend, connection, file.Artifact)
	for _, descriptor := range descriptors {
		if err := backend.executeSQLiteDataTransformWithTransaction(ctx, file, descriptor, registry, down, transaction); err != nil {
			return err
		}
	}
	return nil
}

func (backend *Store) executeSQLiteDataTransform(ctx context.Context, connection *sql.Conn, file migrationartifact.File, descriptor ridumigration.DataTransformDescriptor, registry sqliteDataTransformRegistry, down bool) error {
	transaction := newSQLiteMigrationDataTransaction(backend, connection, file.Artifact)
	return backend.executeSQLiteDataTransformWithTransaction(ctx, file, descriptor, registry, down, transaction)
}

func newSQLiteMigrationDataTransaction(backend *Store, connection *sql.Conn, artifact ridumigration.Artifact) ridumigration.DataTransaction {
	documents := &documentTransaction{store: backend, connection: connection}
	return datatransform.New(documents, artifact, datatransform.Options{Engine: "SQLite"})
}

func (backend *Store) executeSQLiteDataTransformWithTransaction(ctx context.Context, file migrationartifact.File, descriptor ridumigration.DataTransformDescriptor, registry sqliteDataTransformRegistry, down bool, transaction ridumigration.DataTransaction) error {
	registered, exists := registry[descriptor.Name]
	if !exists {
		return fmt.Errorf("SQLite migration %s requires unregistered data transform %q", file.Name, descriptor.Name)
	}
	if registered.Checksum != descriptor.Checksum {
		return fmt.Errorf("SQLite migration %s data transform %q checksum differs from the immutable artifact", file.Name, descriptor.Name)
	}
	callback := registered.Up
	direction := "up"
	if down {
		callback = registered.Down
		direction = "down"
	}
	if err := callback(ctx, transaction); err != nil {
		return fmt.Errorf("SQLite migration %s data transform %q %s: %w", file.Name, descriptor.Name, direction, err)
	}
	return nil
}

type sqliteProjectMigrationDriver struct {
	transforms []ridumigration.DataTransform
}

// ProjectMigrations returns the narrow compiled-project driver used when an
// immutable artifact names application data callbacks. Register it with
// ridu.WithProjectMigrations even in project-command mode; ordinary server
// runtime still owns its separate Store factory.
func ProjectMigrations(transforms ...ridumigration.DataTransform) ridumigration.ProjectDriver {
	return &sqliteProjectMigrationDriver{transforms: append([]ridumigration.DataTransform(nil), transforms...)}
}

func (driver *sqliteProjectMigrationDriver) Validate() error {
	_, err := newSQLiteDataTransformRegistry(driver.transforms)
	return err
}

func (driver *sqliteProjectMigrationDriver) DataTransforms() []ridumigration.DataTransformDescriptor {
	descriptors := make([]ridumigration.DataTransformDescriptor, 0, len(driver.transforms))
	for _, transform := range driver.transforms {
		descriptors = append(descriptors, transform.DataTransformDescriptor)
	}
	return descriptors
}

func (driver *sqliteProjectMigrationDriver) RunProjectMigration(ctx context.Context, request ridumigration.ProjectRequest, executableManifest schema.Manifest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if request.LockTimeout != 0 || request.StatementTimeout != 0 || request.BatchTimeout != 0 ||
		request.IdleTransactionTimeout != 0 || request.StopAfterPhase != "" || request.StopAfterStep != "" {
		return fmt.Errorf("SQLite project migrations do not accept PostgreSQL-only timeout or stop-boundary options")
	}
	registry, err := newSQLiteDataTransformRegistry(driver.transforms)
	if err != nil {
		return err
	}
	files, err := migrationartifact.RequireCurrentHistory(request.Directory, executableManifest)
	if err != nil {
		return err
	}
	return driver.runProjectMigrationFiles(ctx, request, files, registry)
}

func (driver *sqliteProjectMigrationDriver) runProjectMigrationFiles(ctx context.Context, request ridumigration.ProjectRequest, files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	if request.Action == ridumigration.ProjectVerify {
		return verifySQLiteArtifacts(ctx, files, registry)
	}
	if request.Action == ridumigration.ProjectDown || request.Action == ridumigration.ProjectReset || request.Action == ridumigration.ProjectRefresh || request.Action == ridumigration.ProjectFresh {
		if err := validateSQLiteLifecycleFiles(ctx, files, registry); err != nil {
			return err
		}
	}
	backend, err := Open(ctx, request.DatabasePath)
	if err != nil {
		return err
	}
	defer backend.Close()
	switch request.Action {
	case ridumigration.ProjectApply:
		return backend.applySQLiteArtifacts(ctx, files, registry)
	case ridumigration.ProjectDown:
		return backend.downSQLiteArtifacts(ctx, files, registry)
	case ridumigration.ProjectReset:
		return backend.resetSQLiteArtifacts(ctx, files, registry)
	case ridumigration.ProjectRefresh:
		return backend.refreshSQLiteArtifacts(ctx, files, registry)
	case ridumigration.ProjectFresh:
		return backend.freshSQLiteArtifacts(ctx, files, registry)
	default:
		return fmt.Errorf("unsupported SQLite project migration action %q", request.Action)
	}
}

var _ ridumigration.ProjectDriver = (*sqliteProjectMigrationDriver)(nil)
