package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func buildSQLiteArtifactWithDataTransformDescriptors(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, previousPlannerVersion string, contract sqlitePlannerContract, transforms []ridumigration.DataTransformDescriptor, allowTransformedSchema bool) (ridumigration.Artifact, error) {
	return buildSQLiteArtifactWithTransitionValidation(ctx, name, before, after, previousPlannerVersion, contract, transforms, allowTransformedSchema, validateSQLiteAdditiveTransition, validateSQLiteTransformedTransition)
}

// rebuildSQLiteArtifactWithDataTransformDescriptors preserves the exact risk
// classification of historical transformed artifacts. Earlier planners compared
// presentation metadata too, so an otherwise additive edit could require a
// transform. Reconstruct that original plan rather than altering its recorded
// risks or relaxing the caller's full artifact digest comparison.
func rebuildSQLiteArtifactWithDataTransformDescriptors(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, previousPlannerVersion string, contract sqlitePlannerContract, transforms []ridumigration.DataTransformDescriptor, transformedSchema bool) (ridumigration.Artifact, error) {
	artifact, err := buildSQLiteArtifactWithDataTransformDescriptors(ctx, name, before, after, previousPlannerVersion, contract, transforms, transformedSchema)
	if err != nil || !transformedSchema || sqliteArtifactAllowsTransformedSchema(artifact) {
		return artifact, err
	}
	return buildSQLiteArtifactWithTransitionValidation(ctx, name, before, after, previousPlannerVersion, contract, transforms, true, validateSQLiteAdditiveSnapshotTransition, validateSQLiteTransformedSnapshotTransition)
}

func buildSQLiteArtifactWithTransitionValidation(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, previousPlannerVersion string, contract sqlitePlannerContract, transforms []ridumigration.DataTransformDescriptor, allowTransformedSchema bool, validateAdditive, validateTransformed func(schema.Snapshot, schema.Snapshot) error) (ridumigration.Artifact, error) {
	if len(transforms) != 0 && before != nil && previousPlannerVersion == contract.version {
		fromDigest, err := ridumigration.DigestManifest(*before)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		toDigest, err := ridumigration.DigestManifest(after)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if fromDigest == toDigest {
			artifact, err := buildSQLiteDataOnlyArtifact(name, *before, after, contract)
			if err != nil {
				return ridumigration.Artifact{}, err
			}
			return bindSQLiteDataTransforms(artifact, transforms)
		}
	}
	artifact, err := buildSQLiteArtifactWithValidation(ctx, name, before, after, previousPlannerVersion, contract, validateAdditive)
	if err != nil {
		if len(transforms) == 0 || !allowTransformedSchema {
			return ridumigration.Artifact{}, err
		}
		artifact, err = buildSQLiteArtifactWithValidation(ctx, name, before, after, previousPlannerVersion, contract, validateTransformed)
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
	return validateSQLiteTransformedSnapshotTransition(sqliteWithoutPresentation(before), sqliteWithoutPresentation(after))
}

func validateSQLiteTransformedSnapshotTransition(before, after schema.Snapshot) error {
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
		if previous.Versions != nil && !reflect.DeepEqual(previous.Fields, current.Fields) {
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
		if previous.Versions != nil && !reflect.DeepEqual(previous.Fields, current.Fields) {
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

func buildSQLiteDataOnlyArtifact(name string, before, after schema.Manifest, contract sqlitePlannerContract) (ridumigration.Artifact, error) {
	artifact, err := ridumigration.NewArtifact(name, ridumigration.Planner{Name: sqlitePlannerName, Version: contract.version}, &before, after)
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
	assertion := phase.Steps[len(phase.Steps)-1]
	steps := append([]ridumigration.Step(nil), phase.Steps[:len(phase.Steps)-1]...)
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
	steps = append(steps, assertion)
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

func (backend *Store) executeSQLiteDataTransformWithTransaction(ctx context.Context, file migrationartifact.File, descriptor ridumigration.DataTransformDescriptor, registry sqliteDataTransformRegistry, down bool, transaction sqliteMigrationDataTransaction) error {
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

type sqliteMigrationDataTransaction struct {
	transaction                 *documentTransaction
	versionedResourceIdentities map[schema.StableID]struct{}
	resourceShapes              map[schema.StableID][]schema.Collection
	locales                     map[string]struct{}
}

func newSQLiteMigrationDataTransaction(backend *Store, connection *sql.Conn, artifact ridumigration.Artifact) sqliteMigrationDataTransaction {
	versioned := make(map[schema.StableID]struct{})
	resourceShapes := make(map[schema.StableID][]schema.Collection)
	locales := make(map[string]struct{})
	collect := func(snapshot schema.Snapshot) {
		if snapshot.Application.Localization != nil {
			for _, locale := range snapshot.Application.Localization.Locales {
				locales[string(locale.Code)] = struct{}{}
			}
		}
		resources := append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...)
		for _, resource := range resources {
			if resource.Versions != nil || resource.Capabilities.Versions {
				versioned[resource.ID] = struct{}{}
			}
			known := false
			for _, shape := range resourceShapes[resource.ID] {
				if reflect.DeepEqual(shape, resource) {
					known = true
					break
				}
			}
			if !known {
				resourceShapes[resource.ID] = append(resourceShapes[resource.ID], resource)
			}
		}
	}
	if artifact.Before != nil {
		collect(*artifact.Before)
	}
	collect(artifact.After)
	return sqliteMigrationDataTransaction{
		transaction:                 &documentTransaction{store: backend, connection: connection},
		versionedResourceIdentities: versioned,
		resourceShapes:              resourceShapes,
		locales:                     locales,
	}
}

func (transaction sqliteMigrationDataTransaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, request.Values); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Create(ctx, request)
}
func (transaction sqliteMigrationDataTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.requireResource(request.Collection); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Find(ctx, request)
}
func (transaction sqliteMigrationDataTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	if err := transaction.requireResource(request.Collection); err != nil {
		return store.Page{}, err
	}
	return transaction.transaction.List(ctx, request)
}
func (transaction sqliteMigrationDataTransaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, request.Values); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Update(ctx, request)
}
func (transaction sqliteMigrationDataTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, nil); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Trash(ctx, request)
}
func (transaction sqliteMigrationDataTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, nil); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Restore(ctx, request)
}
func (transaction sqliteMigrationDataTransaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.validateMutation(request.Collection, nil); err != nil {
		return store.Document{}, err
	}
	return transaction.transaction.Delete(ctx, request)
}

func (transaction sqliteMigrationDataTransaction) validateMutation(collection schema.Collection, values store.Values) error {
	if err := transaction.requireResource(collection); err != nil {
		return err
	}
	if _, versioned := transaction.versionedResourceIdentities[collection.ID]; versioned {
		return fmt.Errorf("SQLite data transforms cannot mutate versioned resource %q until retained snapshots can be rewritten atomically", collection.ID)
	}
	if values != nil {
		if err := transaction.validateValues(collection.Fields, values, string(collection.ID), nil); err != nil {
			return err
		}
	}
	return nil
}

func (transaction sqliteMigrationDataTransaction) requireResource(collection schema.Collection) error {
	shapes := transaction.resourceShapes[collection.ID]
	if len(shapes) == 0 {
		return fmt.Errorf("SQLite data transform resource %q is outside the immutable artifact manifests", collection.ID)
	}
	for _, shape := range shapes {
		if reflect.DeepEqual(shape, collection) {
			return nil
		}
	}
	return fmt.Errorf("SQLite data transform resource %q must exactly match its immutable before or after manifest shape", collection.ID)
}

func (transaction sqliteMigrationDataTransaction) validateValues(fields []schema.Field, values store.Values, path string, special map[string]struct{}) error {
	byName := make(map[string]schema.Field, len(fields))
	for _, field := range fields {
		if field.Category != schema.FieldCategoryPresentation {
			byName[field.Name] = field
		}
	}
	for name, value := range values {
		if _, allowed := special[name]; allowed {
			continue
		}
		field, exists := byName[name]
		if !exists {
			return fmt.Errorf("SQLite data transform value %q is outside the immutable resource shape", path+"."+name)
		}
		if err := transaction.validateValue(field, value, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func (transaction sqliteMigrationDataTransaction) validateValue(field schema.Field, value store.Value, path string) error {
	if value.Kind() == store.ValueNull {
		return nil
	}
	if field.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return fmt.Errorf("SQLite data transform localized value %q must be an object", path)
		}
		field.Localized = false
		for locale, localizedValue := range localized {
			if _, allowed := transaction.locales[locale]; !allowed {
				return fmt.Errorf("SQLite data transform locale %q at %q is outside the immutable manifests", locale, path)
			}
			if err := transaction.validateValue(field, localizedValue, path+"."+locale); err != nil {
				return err
			}
		}
		return nil
	}
	if embedded.HasFields(field) {
		if err := embedded.ValidateValue(field, value, path, true, nil); err != nil {
			return err
		}
		_, err := embedded.Transform(field, value, path, embedded.NewBudget(), func(o embedded.Occurrence) (store.Values, error) {
			return o.Payload, transaction.validateValues(o.Fields, o.Payload, o.RuntimePath, map[string]struct{}{o.Case.Identity: {}, o.Case.Discriminator: {}})
		})
		return err
	}

	switch field.Type {
	case schema.FieldTypeGroup:
		object, valid := value.CopyObject()
		if !valid || field.Nested == nil {
			return fmt.Errorf("SQLite data transform group value %q must match its immutable resource shape", path)
		}
		return transaction.validateValues(field.Nested.ResolvedFields(), object, path, nil)
	case schema.FieldTypeArray:
		items, valid := value.CopyList()
		if !valid || field.Nested == nil {
			return fmt.Errorf("SQLite data transform array value %q must match its immutable resource shape", path)
		}
		special := map[string]struct{}{"_key": {}}
		for index, item := range items {
			object, valid := item.CopyObject()
			if !valid {
				return fmt.Errorf("SQLite data transform array row %q must be an object", fmt.Sprintf("%s.%d", path, index))
			}
			if err := transaction.validateValues(field.Nested.ResolvedFields(), object, fmt.Sprintf("%s.%d", path, index), special); err != nil {
				return err
			}
		}
	case schema.FieldTypeBlocks:
		items, valid := value.CopyList()
		if !valid || field.Blocks == nil {
			return fmt.Errorf("SQLite data transform blocks value %q must match its immutable resource shape", path)
		}
		if len(items) < field.Blocks.MinRows || field.Blocks.MaxRows > 0 && len(items) > field.Blocks.MaxRows {
			return fmt.Errorf("SQLite data transform blocks value %q violates its row bounds", path)
		}
		special := map[string]struct{}{"_key": {}, "blockType": {}}
		for index, item := range items {
			itemPath := fmt.Sprintf("%s.%d", path, index)
			object, valid := item.CopyObject()
			if !valid {
				return fmt.Errorf("SQLite data transform block %q must be an object", itemPath)
			}
			blockKey, valid := object["blockType"].StringValue()
			if !valid {
				return fmt.Errorf("SQLite data transform block %q must name an immutable block type", itemPath)
			}
			var blockFields []schema.Field
			found := false
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug == blockKey {
					blockFields = block.ResolvedFields()
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("SQLite data transform block type %q at %q is outside the immutable resource shape", blockKey, itemPath)
			}
			if err := transaction.validateValues(blockFields, object, itemPath, special); err != nil {
				return err
			}
		}
	}
	return nil
}

var _ ridumigration.DataTransaction = sqliteMigrationDataTransaction{}

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
		return backend.applySQLiteArtifacts(ctx, files, sqlitePlannerContractFor, registry)
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
