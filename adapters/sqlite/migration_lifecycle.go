package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/referenceindex"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// DownArtifacts rolls back the most recently applied immutable artifact. The
// artifact file remains committed, so status reports it as pending and a later
// ApplyArtifacts call can replay it. Registered callbacks are checksum-bound
// to the artifact. An already-empty database is a no-op.
func (backend *Store) DownArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	files, err := readSQLiteLifecycleFiles(ctx, directory, registry)
	if err != nil {
		return err
	}
	return backend.downSQLiteArtifacts(ctx, files, registry)
}

func (backend *Store) downSQLiteArtifacts(ctx context.Context, files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		applied, err := validateSQLiteLifecycleState(ctx, connection, files)
		if err != nil || len(applied) == 0 {
			return err
		}
		return backend.rollbackSQLiteArtifact(ctx, connection, files, len(applied)-1, registry)
	})
}

// ResetArtifacts rolls back every applied artifact in exact reverse order.
// Files and their immutable checksums remain untouched, and registered
// callbacks run in the same transaction.
func (backend *Store) ResetArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	files, err := readSQLiteLifecycleFiles(ctx, directory, registry)
	if err != nil {
		return err
	}
	return backend.resetSQLiteArtifacts(ctx, files, registry)
}

func (backend *Store) resetSQLiteArtifacts(ctx context.Context, files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		applied, err := validateSQLiteLifecycleState(ctx, connection, files)
		if err != nil {
			return err
		}
		for index := len(applied) - 1; index >= 0; index-- {
			if err := backend.rollbackSQLiteArtifact(ctx, connection, files, index, registry); err != nil {
				return err
			}
		}
		return nil
	})
}

// RefreshArtifacts rolls back every applied artifact and replays the complete
// committed schema and callback history in one SQLite transaction.
func (backend *Store) RefreshArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	files, err := readSQLiteLifecycleFiles(ctx, directory, registry)
	if err != nil {
		return err
	}
	return backend.refreshSQLiteArtifacts(ctx, files, registry)
}

func (backend *Store) refreshSQLiteArtifacts(ctx context.Context, files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		applied, err := validateSQLiteLifecycleState(ctx, connection, files)
		if err != nil {
			return err
		}
		for index := len(applied) - 1; index >= 0; index-- {
			if err := backend.rollbackSQLiteArtifact(ctx, connection, files, index, registry); err != nil {
				return err
			}
		}
		return backend.applySQLiteLifecycleFiles(ctx, connection, files, registry)
	})
}

// FreshArtifacts drops every non-internal object in the selected SQLite
// database and replays committed history in one transaction. Callers must put
// an explicit destructive confirmation in front of this method. Registered up
// callbacks replay with their committed artifacts.
func (backend *Store) FreshArtifacts(ctx context.Context, directory string, transforms ...ridumigration.DataTransform) error {
	registry, err := newSQLiteDataTransformRegistry(transforms)
	if err != nil {
		return err
	}
	files, err := readSQLiteLifecycleFiles(ctx, directory, registry)
	if err != nil {
		return err
	}
	return backend.freshSQLiteArtifacts(ctx, files, registry)
}

func (backend *Store) freshSQLiteArtifacts(ctx context.Context, files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		objects, err := readAllSQLiteSchemaObjects(ctx, connection)
		if err != nil {
			return err
		}
		if err := dropSQLiteSchemaObjects(ctx, connection, objects); err != nil {
			return fmt.Errorf("fresh SQLite database: %w", err)
		}
		return backend.applySQLiteLifecycleFiles(ctx, connection, files, registry)
	})
}

func readSQLiteLifecycleFiles(ctx context.Context, directory string, registry sqliteDataTransformRegistry) ([]migrationartifact.File, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return nil, err
	}
	if err := validateSQLiteLifecycleFiles(ctx, files, registry); err != nil {
		return nil, err
	}
	return files, nil
}

func validateSQLiteLifecycleFiles(ctx context.Context, files []migrationartifact.File, registry sqliteDataTransformRegistry) error {
	if len(files) == 0 {
		return fmt.Errorf("migration artifact history is empty; create and commit an initial migration first")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return err
	}
	if err := requireSQLiteDataTransformRegistry(files, registry); err != nil {
		return err
	}
	return nil
}

func validateSQLiteLifecycleState(ctx context.Context, connection *sql.Conn, files []migrationartifact.File) ([]sqliteArtifactLedgerRow, error) {
	exists, err := sqliteArtifactLedgerExists(ctx, connection)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := assertNoSQLiteManagedSchema(ctx, connection); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := validateSQLiteArtifactLedgerShape(ctx, connection); err != nil {
		return nil, err
	}
	applied, err := readSQLiteArtifactLedger(ctx, connection)
	if err != nil {
		return nil, err
	}
	if len(applied) == 0 {
		return nil, fmt.Errorf("SQLite migration ledger exists without an applied artifact")
	}
	if err := validateSQLiteArtifactLedgerContinuity(applied); err != nil {
		return nil, err
	}
	if err := validateSQLiteAppliedArtifacts(files, applied); err != nil {
		return nil, err
	}
	head := files[len(applied)-1]
	manifest, err := head.Artifact.AfterManifest()
	if err != nil {
		return nil, err
	}
	contract, err := resolveSQLitePlannerContract(head, sqlitePlannerContractFor)
	if err != nil {
		return nil, err
	}
	if err := assertSQLitePhysicalSchema(ctx, connection, manifest, true, contract); err != nil {
		return nil, fmt.Errorf("current SQLite migration state: %w", err)
	}
	if err := assertSQLiteManifestDigest(ctx, connection, head.Artifact.ToDigest); err != nil {
		return nil, fmt.Errorf("current SQLite migration state: %w", err)
	}
	return applied, nil
}

func (backend *Store) rollbackSQLiteArtifact(ctx context.Context, connection *sql.Conn, files []migrationartifact.File, index int, transforms sqliteDataTransformRegistry) error {
	file := files[index]
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == ridumigration.StepCanonicalizeAuthIdentities {
				return fmt.Errorf("SQLite migration %s contains an irreversible auth-identity canonicalization and cannot be rolled back", file.Name)
			}
		}
	}
	current, err := file.Artifact.AfterManifest()
	if err != nil {
		return err
	}
	if err := backend.executeSQLiteDataTransforms(ctx, connection, file, transforms, true); err != nil {
		return err
	}
	currentContract, err := resolveSQLitePlannerContract(file, sqlitePlannerContractFor)
	if err != nil {
		return err
	}

	if index == 0 {
		boundary, err := newSQLitePluginSchemaBoundary(ctx, connection, &current, nil)
		if err != nil {
			return fmt.Errorf("prepare SQLite rollback %s plugin boundary: %w", file.Name, err)
		}
		operations, err := inverseSQLitePluginOperations(file.Artifact)
		if err != nil {
			return err
		}
		if err := executeSQLitePluginOperations(ctx, connection, operations, func() error {
			return boundary.validateIntermediate(ctx, connection)
		}); err != nil {
			return fmt.Errorf("roll back SQLite migration %s plugin schema: %w", file.Name, err)
		}
		if err := boundary.validateFinal(ctx, connection); err != nil {
			return fmt.Errorf("verify SQLite rollback %s plugin schema: %w", file.Name, err)
		}
		objects, err := expectedSQLiteObjects(ctx, &current, true, currentContract)
		if err != nil {
			return err
		}
		actual, err := readAllSQLiteSchemaObjects(ctx, connection)
		if err != nil {
			return err
		}
		owned := make(map[string]sqliteSchemaObject, len(objects))
		for key := range objects {
			if object, exists := actual[key]; exists {
				owned[key] = object
			}
		}
		if err := dropSQLiteSchemaObjects(ctx, connection, owned); err != nil {
			return fmt.Errorf("roll back initial SQLite migration %s: %w", file.Name, err)
		}
		return assertNoSQLiteManagedSchema(ctx, connection)
	}

	target, err := file.Artifact.BeforeManifest()
	if err != nil {
		return err
	}
	targetContract, err := resolveSQLitePlannerContract(files[index-1], sqlitePlannerContractFor)
	if err != nil {
		return err
	}
	boundary, err := newSQLitePluginSchemaBoundary(ctx, connection, &current, &target)
	if err != nil {
		return fmt.Errorf("prepare SQLite rollback %s plugin boundary: %w", file.Name, err)
	}
	operations, err := inverseSQLitePluginOperations(file.Artifact)
	if err != nil {
		return err
	}
	if err := executeSQLitePluginOperations(ctx, connection, operations, func() error {
		return boundary.validateIntermediate(ctx, connection)
	}); err != nil {
		return fmt.Errorf("roll back SQLite migration %s plugin schema: %w", file.Name, err)
	}
	if err := boundary.validateFinal(ctx, connection); err != nil {
		return fmt.Errorf("verify SQLite rollback %s plugin schema: %w", file.Name, err)
	}
	if err := backend.scrubSQLiteRollbackFields(ctx, connection, current, target); err != nil {
		return fmt.Errorf("roll back SQLite migration %s fields: %w", file.Name, err)
	}
	if err := backend.retireSQLiteRollbackResources(ctx, connection, current, target); err != nil {
		return fmt.Errorf("roll back SQLite migration %s resources: %w", file.Name, err)
	}
	if err := targetContract.reconcileIndexes(ctx, connection, target); err != nil {
		return fmt.Errorf("roll back SQLite migration %s indexes: %w", file.Name, err)
	}
	if err := rebuildDocumentReferences(ctx, connection, target); err != nil {
		return fmt.Errorf("roll back SQLite migration %s references: %w", file.Name, err)
	}
	if err := targetContract.rebuildUniqueness(ctx, connection, target); err != nil {
		return fmt.Errorf("roll back SQLite migration %s uniqueness: %w", file.Name, err)
	}
	if err := assertSQLitePhysicalSchema(ctx, connection, target, true, targetContract); err != nil {
		return fmt.Errorf("roll back SQLite migration %s: %w", file.Name, err)
	}
	if err := writeSQLiteManifest(ctx, connection, target, files[len(files)-1].Digest, backend.now().UTC()); err != nil {
		return fmt.Errorf("record SQLite rollback %s manifest: %w", file.Name, err)
	}
	if _, err := connection.ExecContext(ctx, `DELETE FROM ridu_migrations WHERE position = ? AND name = ?`, index+1, file.Name); err != nil {
		return fmt.Errorf("record SQLite rollback %s: %w", file.Name, translateError(err))
	}
	if err := assertSQLiteManifestDigest(ctx, connection, files[index-1].Artifact.ToDigest); err != nil {
		return err
	}
	return assertSQLiteExpectedArtifactDigest(ctx, connection, files[len(files)-1].Digest)
}

// inverseSQLitePluginOperations derives rollback from the immutable artifact,
// not from a fresh manifest diff. Reversing the complete forward sequence is
// required when one artifact changes multiple plugins or mixes up and down
// directions across plugin keys.
func inverseSQLitePluginOperations(artifact ridumigration.Artifact) ([]ridumigration.Operation, error) {
	var forward []ridumigration.PluginStep
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepPluginSQL {
				continue
			}
			var payload ridumigration.PluginPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode SQLite plugin rollback step %s: %w", step.ID, err)
			}
			forward = append(forward, payload.Plugin)
		}
	}
	operations := make([]ridumigration.Operation, 0, len(forward))
	for index := len(forward) - 1; index >= 0; index-- {
		step := forward[index]
		if step.Adapter != schema.PluginDatabaseAdapterSQLite || step.Checksum != ridumigration.PluginStepChecksum(step.Adapter, step.Plugin, step.Version, step.Direction, step.SQL) {
			return nil, fmt.Errorf("invalid SQLite plugin migration checksum for %s version %d", step.Plugin, step.Version)
		}
		var snapshot schema.Snapshot
		var direction string
		switch step.Direction {
		case "up":
			snapshot = artifact.After
			direction = "down"
		case "down":
			if artifact.Before == nil {
				return nil, fmt.Errorf("initial SQLite migration cannot contain plugin down step %s version %d", step.Plugin, step.Version)
			}
			snapshot = *artifact.Before
			direction = "up"
		default:
			return nil, fmt.Errorf("SQLite plugin migration %s version %d has unsupported direction %q", step.Plugin, step.Version, step.Direction)
		}
		pluginMigration, found := sqlitePluginMigration(snapshot, step.Plugin, step.Version)
		if !found {
			return nil, fmt.Errorf("SQLite plugin migration %s version %d is absent from its artifact manifest", step.Plugin, step.Version)
		}
		forwardSQL := pluginMigration.UpSQL
		inverseSQL := pluginMigration.DownSQL
		if step.Direction == "down" {
			forwardSQL, inverseSQL = pluginMigration.DownSQL, pluginMigration.UpSQL
		}
		if !sameSQLiteStatements(step.SQL, forwardSQL) {
			return nil, fmt.Errorf("SQLite plugin migration %s version %d differs from its artifact manifest", step.Plugin, step.Version)
		}
		inverse := ridumigration.PluginStep{
			Adapter: schema.PluginDatabaseAdapterSQLite, Plugin: step.Plugin, Version: step.Version,
			Direction: direction, SQL: append([]string(nil), inverseSQL...),
		}
		inverse.Checksum = ridumigration.PluginStepChecksum(inverse.Adapter, inverse.Plugin, inverse.Version, inverse.Direction, inverse.SQL)
		operations = append(operations, ridumigration.Operation{
			Kind:   ridumigration.StepPluginSQL,
			Name:   fmt.Sprintf("plugin %s migration %d %s %s", step.Plugin, step.Version, pluginMigration.Name, direction),
			Plugin: &inverse,
		})
	}
	return operations, nil
}

func sqlitePluginMigration(snapshot schema.Snapshot, pluginKey string, version uint32) (schema.PluginMigration, bool) {
	for _, plugin := range snapshot.Plugins {
		if plugin.Key != pluginKey {
			continue
		}
		contribution, supported := plugin.DatabaseContribution(schema.PluginDatabaseAdapterSQLite)
		if !supported {
			return schema.PluginMigration{}, false
		}
		for _, pluginMigration := range contribution.Migrations {
			if pluginMigration.Version == version {
				return pluginMigration, true
			}
		}
		return schema.PluginMigration{}, false
	}
	return schema.PluginMigration{}, false
}

func sameSQLiteStatements(left, right []string) bool {
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

func (backend *Store) scrubSQLiteRollbackFields(ctx context.Context, connection *sql.Conn, current, target schema.Manifest) error {
	targetResources := make(map[schema.StableID]schema.Collection)
	for _, resource := range sqliteManifestResources(target) {
		targetResources[resource.ID] = resource
	}
	transaction := &documentTransaction{store: backend, connection: connection}
	for _, currentResource := range sqliteManifestResources(current) {
		targetResource, survives := targetResources[currentResource.ID]
		if !survives {
			continue
		}
		// Down callbacks run before this scrub and may already have restored
		// target-only fields. Keep both manifest projections visible while the
		// recursive scrub removes current-only values.
		readResource := currentResource
		readResource.Fields = sqliteRollbackReadFields(currentResource.Fields, targetResource.Fields)
		documents, err := loadCollectionDocumentRecords(ctx, connection, readResource)
		if err != nil {
			return fmt.Errorf("load resource %s: %w", currentResource.ID, err)
		}
		for _, document := range documents {
			values, changed := scrubSQLiteRollbackValues(currentResource.Fields, targetResource.Fields, document.Values)
			if !changed {
				continue
			}
			document.Values = values
			if err := transaction.persistDocument(ctx, targetResource, document); err != nil {
				return fmt.Errorf("scrub resource %s document %s: %w", currentResource.ID, document.ID, err)
			}
		}
		if targetResource.Versions == nil {
			continue
		}
		rows, err := connection.QueryContext(ctx, `SELECT document_id, revision, snapshot_json
FROM ridu_versions WHERE collection_id = ? ORDER BY document_id, revision`, string(currentResource.ID))
		if err != nil {
			return translateError(err)
		}
		type versionSnapshot struct {
			documentID string
			revision   int
			document   store.Document
		}
		var updates []versionSnapshot
		for rows.Next() {
			var update versionSnapshot
			var encoded string
			if err := rows.Scan(&update.documentID, &update.revision, &encoded); err != nil {
				rows.Close()
				return translateError(err)
			}
			if err := json.Unmarshal([]byte(encoded), &update.document); err != nil {
				rows.Close()
				return fmt.Errorf("decode version snapshot for %s/%s revision %d: %w", currentResource.ID, update.documentID, update.revision, err)
			}
			values, changed := scrubSQLiteRollbackValues(currentResource.Fields, targetResource.Fields, update.document.Values)
			if changed {
				update.document.Values = values
				updates = append(updates, update)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return translateError(err)
		}
		if err := rows.Close(); err != nil {
			return translateError(err)
		}
		for _, update := range updates {
			encoded, err := json.Marshal(update.document)
			if err != nil {
				return fmt.Errorf("encode version snapshot for %s/%s revision %d: %w", currentResource.ID, update.documentID, update.revision, err)
			}
			if _, err := connection.ExecContext(ctx, `UPDATE ridu_versions SET snapshot_json = ?
WHERE collection_id = ? AND document_id = ? AND revision = ?`, string(encoded), string(currentResource.ID), update.documentID, update.revision); err != nil {
				return translateError(err)
			}
		}
	}
	return nil
}

func sqliteRollbackReadFields(current, target []schema.Field) []schema.Field {
	fields := make([]schema.Field, 0, len(target)+len(current))
	seen := make(map[string]struct{}, len(target)+len(current))
	for _, field := range target {
		seen[field.Name] = struct{}{}
		fields = append(fields, field)
	}
	for _, field := range current {
		if _, exists := seen[field.Name]; exists {
			continue
		}
		seen[field.Name] = struct{}{}
		fields = append(fields, field)
	}
	return fields
}

func scrubSQLiteRollbackValues(currentFields, targetFields []schema.Field, values store.Values) (store.Values, bool) {
	targetByID := make(map[schema.StableID]schema.Field, len(targetFields))
	for _, field := range targetFields {
		targetByID[field.ID] = field
	}
	result := store.CloneValues(values)
	changed := false
	for _, current := range currentFields {
		target, exists := targetByID[current.ID]
		if !exists {
			if _, present := result[current.Name]; present {
				delete(result, current.Name)
				changed = true
			}
			continue
		}
		value, present := result[current.Name]
		if !present {
			continue
		}
		updated, fieldChanged := scrubSQLiteRollbackField(current, target, value)
		if fieldChanged {
			result[current.Name] = updated
			changed = true
		}
	}
	return result, changed
}

func scrubSQLiteRollbackField(current, target schema.Field, value store.Value) (store.Value, bool) {
	if current.Localized {
		localized, valid := value.ObjectValue()
		if !valid {
			return value, false
		}
		current.Localized = false
		target.Localized = false
		result := store.CloneValues(localized)
		changed := false
		for locale, localizedValue := range localized {
			updated, localeChanged := scrubSQLiteRollbackFieldValue(current, target, localizedValue)
			if localeChanged {
				result[locale] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(result), true
		}
		return value, false
	}
	return scrubSQLiteRollbackFieldValue(current, target, value)
}

func scrubSQLiteRollbackFieldValue(current, target schema.Field, value store.Value) (store.Value, bool) {
	switch current.Type {
	case schema.FieldTypeGroup:
		object, valid := value.ObjectValue()
		if !valid || current.Nested == nil || target.Nested == nil {
			return value, false
		}
		updated, changed := scrubSQLiteRollbackValues(current.Nested.Fields, target.Nested.Fields, object)
		if changed {
			return store.Object(updated), true
		}
	case schema.FieldTypeArray:
		items, valid := value.Values()
		if !valid || current.Nested == nil || target.Nested == nil {
			return value, false
		}
		updated := append([]store.Value(nil), items...)
		changed := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			itemValues, itemChanged := scrubSQLiteRollbackValues(current.Nested.Fields, target.Nested.Fields, object)
			if itemChanged {
				updated[index] = store.Object(itemValues)
				changed = true
			}
		}
		if changed {
			return store.List(updated...), true
		}
	case schema.FieldTypeBlocks:
		items, valid := value.Values()
		if !valid || current.Blocks == nil || target.Blocks == nil {
			return value, false
		}
		currentTypes := make(map[string]schema.BlockType, len(current.Blocks.Types))
		for _, block := range current.Blocks.Types {
			currentTypes[block.Key] = block
		}
		targetTypes := make(map[string]schema.BlockType, len(target.Blocks.Types))
		for _, block := range target.Blocks.Types {
			targetTypes[block.Key] = block
		}
		updated := make([]store.Value, 0, len(items))
		changed := false
		for _, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				updated = append(updated, item)
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			currentBlock, known := currentTypes[blockType]
			targetBlock, survives := targetTypes[blockType]
			if known && !survives {
				changed = true
				continue
			}
			if known && survives {
				itemValues, itemChanged := scrubSQLiteRollbackValues(currentBlock.Fields, targetBlock.Fields, object)
				if itemChanged {
					item = store.Object(itemValues)
					changed = true
				}
			}
			updated = append(updated, item)
		}
		if changed {
			return store.List(updated...), true
		}
	}
	return value, false
}

func (backend *Store) retireSQLiteRollbackResources(ctx context.Context, connection *sql.Conn, current, target schema.Manifest) error {
	currentResources := sqliteManifestResources(current)
	targetIDs := make(map[schema.StableID]struct{})
	for _, resource := range sqliteManifestResources(target) {
		targetIDs[resource.ID] = struct{}{}
	}
	var retired []schema.StableID
	for _, resource := range currentResources {
		if _, survives := targetIDs[resource.ID]; !survives {
			retired = append(retired, resource.ID)
		}
	}
	if len(retired) == 0 {
		return nil
	}
	sort.Slice(retired, func(left, right int) bool { return retired[left] < retired[right] })

	transaction := &documentTransaction{store: backend, connection: connection}
	for _, resource := range currentResources {
		if _, survives := targetIDs[resource.ID]; !survives || !referenceindex.TargetsAnyResource(resource, retired) {
			continue
		}
		documents, err := loadCollectionDocumentRecords(ctx, connection, resource)
		if err != nil {
			return fmt.Errorf("load reference-bearing resource %s: %w", resource.ID, err)
		}
		for _, document := range documents {
			values, changed := referenceindex.RemoveResourceTargets(resource, document.Values, retired)
			if !changed {
				continue
			}
			document.Values = values
			if err := transaction.persistDocument(ctx, resource, document); err != nil {
				return fmt.Errorf("scrub retired resource references from %s/%s: %w", resource.ID, document.ID, err)
			}
		}
	}

	statements := []struct {
		name string
		sql  string
	}{
		{"document references", `DELETE FROM ridu_document_references WHERE owner_collection_id = ? OR target_collection_id = ?`},
		{"tasks", `DELETE FROM ridu_tasks WHERE target_collection_id = ? OR requested_by_collection_id = ?`},
		{"document locks", `DELETE FROM ridu_document_locks WHERE collection_id = ? OR owner_collection_id = ?`},
		{"versions", `DELETE FROM ridu_versions WHERE collection_id = ?`},
		{"preferences", `DELETE FROM ridu_preferences WHERE collection_id = ?`},
		{"auth tokens", `DELETE FROM ridu_auth_tokens WHERE collection_id = ?`},
		{"auth sessions", `DELETE FROM ridu_auth_sessions WHERE collection_id = ?`},
		{"auth API keys", `DELETE FROM ridu_auth_api_keys WHERE collection_id = ?`},
		{"auth credentials", `DELETE FROM ridu_auth_credentials WHERE collection_id = ?`},
		{"documents", `DELETE FROM ridu_documents WHERE collection_id = ?`},
	}
	for _, statement := range statements {
		for _, resourceID := range retired {
			arguments := []any{string(resourceID)}
			if strings.Count(statement.sql, "?") == 2 {
				arguments = append(arguments, string(resourceID))
			}
			if _, err := connection.ExecContext(ctx, statement.sql, arguments...); err != nil {
				return fmt.Errorf("retire SQLite %s for resource %s: %w", statement.name, resourceID, translateError(err))
			}
		}
	}
	// ridu_auth_rate_limits has no resource identity and contains only
	// short-lived irreversible hashes. Clearing it globally would weaken
	// throttling for unrelated auth collections.
	return nil
}

func sqliteManifestResources(manifest schema.Manifest) []schema.Collection {
	snapshot := manifest.Snapshot()
	resources := make([]schema.Collection, 0, len(snapshot.Collections)+len(snapshot.Globals))
	resources = append(resources, snapshot.Collections...)
	resources = append(resources, snapshot.Globals...)
	sort.Slice(resources, func(left, right int) bool { return resources[left].ID < resources[right].ID })
	return resources
}

func (backend *Store) applySQLiteLifecycleFiles(ctx context.Context, connection *sql.Conn, files []migrationartifact.File, transforms sqliteDataTransformRegistry) error {
	expectedHead := files[len(files)-1].Digest
	if _, err := connection.ExecContext(ctx, sqliteArtifactLedgerSQL); err != nil {
		return fmt.Errorf("create SQLite migration ledger: %w", translateError(err))
	}
	for index, file := range files {
		contract, err := resolveSQLitePlannerContract(file, sqlitePlannerContractFor)
		if err != nil {
			return err
		}
		if err := backend.applySQLiteArtifact(ctx, connection, file, index+1, expectedHead, contract, transforms); err != nil {
			return err
		}
	}
	latest, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		return err
	}
	contract, err := resolveSQLitePlannerContract(files[len(files)-1], sqlitePlannerContractFor)
	if err != nil {
		return err
	}
	if err := assertSQLitePhysicalSchema(ctx, connection, latest, true, contract); err != nil {
		return fmt.Errorf("completed SQLite migration state: %w", err)
	}
	if err := assertSQLiteManifestDigest(ctx, connection, files[len(files)-1].Artifact.ToDigest); err != nil {
		return err
	}
	return assertSQLiteExpectedArtifactDigest(ctx, connection, expectedHead)
}

func dropSQLiteSchemaObjects(ctx context.Context, connection *sql.Conn, objects map[string]sqliteSchemaObject) error {
	var early, tables []sqliteSchemaObject
	for _, object := range objects {
		if strings.HasPrefix(object.name, "sqlite_") {
			continue
		}
		switch object.objectType {
		case "trigger", "view", "index":
			early = append(early, object)
		case "table":
			tables = append(tables, object)
		}
	}
	sort.Slice(early, func(left, right int) bool {
		if early[left].objectType != early[right].objectType {
			return early[left].objectType < early[right].objectType
		}
		return early[left].name < early[right].name
	})
	for _, object := range early {
		kind := strings.ToUpper(object.objectType)
		if _, err := connection.ExecContext(ctx, "DROP "+kind+" IF EXISTS "+quoteSQLiteIdentifier(object.name)); err != nil {
			return fmt.Errorf("drop SQLite %s %q: %w", object.objectType, object.name, translateError(err))
		}
	}
	// Defer FK checks until commit so cycles are harmless once every selected
	// table has disappeared. This pragma is transaction-scoped and does not
	// weaken normal application connections.
	if _, err := connection.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return translateError(err)
	}
	sort.Slice(tables, func(left, right int) bool { return tables[left].name > tables[right].name })
	for _, table := range tables {
		if _, err := connection.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoteSQLiteIdentifier(table.name)); err != nil {
			return fmt.Errorf("drop SQLite table %q: %w", table.name, translateError(err))
		}
	}
	return nil
}
