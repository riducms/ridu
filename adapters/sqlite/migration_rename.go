package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// sqliteFieldRename is one reviewed field rename resolved against its
// manifests. SQLite keeps each document as JSON keyed by field name, so a
// rename moves one key in its containing object; the collection is unchanged.
type sqliteFieldRename struct {
	collectionID schema.StableID
	before       schema.Field
	after        schema.Field
}

// sqliteFieldRenames resolves reviewed rename intent. Each rename must be one
// of the unambiguous, shape-compatible candidates the two manifests imply, so
// a stale or hand-edited intent cannot move unrelated content. Collection
// renames are refused: they would move every framework table keyed by the
// collection, and SQLite has no typed executor for that.
func sqliteFieldRenames(before, after schema.Manifest, renames []ridumigration.Rename) ([]sqliteFieldRename, error) {
	candidates := schemadiff.RenameCandidates(before, after)
	resolved := make([]sqliteFieldRename, 0, len(renames))
	seen := make(map[schema.StableID]bool, len(renames))
	for _, rename := range renames {
		if rename.FieldBefore == "" || rename.FieldAfter == "" {
			return nil, fmt.Errorf("SQLite cannot rename collection %q to %q; add the new collection and move its documents with a registered data transform", rename.CollectionBefore, rename.CollectionAfter)
		}
		matched := false
		for _, candidate := range candidates {
			if candidate.Kind != schemadiff.RenameField || candidate.BeforeCollection.Slug != rename.CollectionBefore || candidate.AfterCollection.Slug != rename.CollectionAfter ||
				candidate.BeforeField.Path.String() != rename.FieldBefore || candidate.AfterField.Path.String() != rename.FieldAfter {
				continue
			}
			if seen[candidate.BeforeField.ID] {
				return nil, fmt.Errorf("field %q in collection %q is renamed more than once", rename.FieldBefore, rename.CollectionBefore)
			}
			seen[candidate.BeforeField.ID] = true
			resolved = append(resolved, sqliteFieldRename{collectionID: candidate.BeforeCollection.ID, before: *candidate.BeforeField, after: *candidate.AfterField})
			matched = true
			break
		}
		if !matched {
			return nil, fmt.Errorf("%q -> %q in collection %q is not an unambiguous field rename between these schemas", rename.FieldBefore, rename.FieldAfter, rename.CollectionBefore)
		}
	}
	return resolved, nil
}

// buildSQLiteArtifactWithRenames plans a migration whose only non-additive
// changes are the reviewed field renames. It adds one content-rename step per
// rename; the closing schema step then rebuilds indexes, references and
// uniqueness from the renamed content.
func buildSQLiteArtifactWithRenames(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, renames []ridumigration.Rename) (ridumigration.Artifact, error) {
	if before == nil {
		return ridumigration.Artifact{}, fmt.Errorf("an initial SQLite migration cannot rename fields")
	}
	resolved, err := sqliteFieldRenames(*before, after, renames)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	rules := sqliteAdditiveRules{renames: make(map[schema.StableID]map[schema.StableID]schema.Field), paths: make(map[schema.StableID]map[string]string)}
	for _, rename := range resolved {
		if rules.renames[rename.collectionID] == nil {
			rules.renames[rename.collectionID] = make(map[schema.StableID]schema.Field)
			rules.paths[rename.collectionID] = make(map[string]string)
		}
		rules.renames[rename.collectionID][rename.before.ID] = rename.after
		rules.paths[rename.collectionID][rename.before.Path.String()] = rename.after.Path.String()
	}
	validate := func(before, after schema.Snapshot) error {
		return rules.snapshot(sqliteStorageSchema(before), sqliteStorageSchema(after))
	}
	return buildSQLiteArtifactWithValidation(ctx, name, before, after, validate, renames)
}

// sqliteArtifactRenames returns the rename intent an artifact recorded.
func sqliteArtifactRenames(artifact ridumigration.Artifact) ([]ridumigration.Rename, error) {
	var renames []ridumigration.Rename
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			var payload ridumigration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode content-rename step %s: %w", step.ID, err)
			}
			renames = append(renames, payload.Rename)
		}
	}
	return renames, nil
}

// applySQLiteArtifactRenames moves content for every rename an artifact
// recorded, or moves it back when reverse is set.
func applySQLiteArtifactRenames(ctx context.Context, connection *sql.Conn, artifact ridumigration.Artifact, before *schema.Manifest, after schema.Manifest, reverse bool) error {
	renames, err := sqliteArtifactRenames(artifact)
	if err != nil || len(renames) == 0 {
		return err
	}
	if before == nil {
		return fmt.Errorf("an initial SQLite migration cannot rename fields")
	}
	resolved, err := sqliteFieldRenames(*before, after, renames)
	if err != nil {
		return err
	}
	return renameSQLiteContent(ctx, connection, *before, after, resolved, reverse)
}

// CreateArtifactWithRenames plans and atomically creates one immutable SQLite
// migration that preserves content across reviewed field renames. Apart from
// those renames the transition must be additive, as CreateArtifact requires.
func CreateArtifactWithRenames(ctx context.Context, directory, name string, after schema.Manifest, now time.Time, renames []ridumigration.Rename) (CreatedArtifact, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if len(files) == 0 {
		return CreatedArtifact{}, fmt.Errorf("an initial SQLite migration cannot rename fields")
	}
	if err := preflightSQLiteArtifacts(ctx, files); err != nil {
		return CreatedArtifact{}, err
	}
	before, err := files[len(files)-1].Artifact.AfterManifest()
	if err != nil {
		return CreatedArtifact{}, err
	}
	artifact, err := buildSQLiteArtifactWithRenames(ctx, name, &before, after, renames)
	if err != nil {
		return CreatedArtifact{}, err
	}
	if err := requireSQLiteDestructiveApproval(artifact.Risks, false); err != nil {
		return CreatedArtifact{}, err
	}
	file, err := migrationartifact.Create(directory, name, artifact, now)
	if err != nil {
		return CreatedArtifact{}, err
	}
	return CreatedArtifact{Path: file.Path, Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version}, nil
}

// RenameDevelopmentFields moves content across reviewed field renames in a
// database that `ridu dev` synchronizes, using the executor migrations use,
// and synchronizes the database to the resulting schema in the same
// transaction. The database then records that schema as the one it has, so a
// rename that was applied is never mistaken for one still waiting. It refuses
// a database with migration history, which only ridu migrate may change.
func (backend *Store) RenameDevelopmentFields(ctx context.Context, before, after schema.Manifest, renames []ridumigration.Rename) error {
	resolved, err := sqliteFieldRenames(before, after, renames)
	if err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		managed, err := sqliteArtifactLedgerExists(ctx, connection)
		if err != nil {
			return err
		}
		if managed {
			return fmt.Errorf("this SQLite database is managed by ridu migrate; create the rename with ridu migrate create and apply it with ridu migrate up")
		}
		// A database ridu dev has not synchronized yet has no content to move.
		var installed int
		if err := connection.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'ridu_documents'`).Scan(&installed); err != nil {
			return translateError(err)
		}
		if installed != 0 {
			if err := renameSQLiteContent(ctx, connection, before, after, resolved, false); err != nil {
				return err
			}
		}
		return backend.migrateDevelopmentSchema(ctx, connection, after)
	})
}

// renameSQLiteContent rewrites every current document and retained version
// snapshot of each affected collection. reverse undoes the renames for a
// rollback: content then moves from the after names back to the before names.
func renameSQLiteContent(ctx context.Context, connection *sql.Conn, before, after schema.Manifest, renames []sqliteFieldRename, reverse bool) error {
	sourceManifest, targetManifest := before, after
	if reverse {
		sourceManifest, targetManifest = after, before
	}
	targets := make(map[schema.StableID]schema.Collection)
	for _, collection := range targetManifest.Snapshot().Collections {
		targets[collection.ID] = collection
	}
	for _, source := range sourceManifest.Snapshot().Collections {
		renamed := make(map[schema.StableID]schema.Field)
		for _, rename := range renames {
			if rename.collectionID != source.ID {
				continue
			}
			if reverse {
				renamed[rename.after.ID] = rename.before
			} else {
				renamed[rename.before.ID] = rename.after
			}
		}
		if len(renamed) == 0 {
			continue
		}
		target, exists := targets[source.ID]
		if !exists {
			return fmt.Errorf("rename collection %s is missing from the resulting schema", source.ID)
		}
		if err := renameSQLiteCollectionContent(ctx, connection, source, target, renamed); err != nil {
			return fmt.Errorf("rename content in collection %s: %w", source.Slug, err)
		}
	}
	return nil
}

func renameSQLiteCollectionContent(ctx context.Context, connection *sql.Conn, source, target schema.Collection, renamed map[schema.StableID]schema.Field) error {
	// Reading the raw values, not the schema's projection of them, keeps keys
	// no schema names in the rewritten document.
	rows, err := connection.QueryContext(ctx, `SELECT id, values_json FROM ridu_documents WHERE collection_id = ? ORDER BY id`, string(source.ID))
	if err != nil {
		return translateError(err)
	}
	type documentUpdate struct{ id, values string }
	var documents []documentUpdate
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			rows.Close()
			return translateError(err)
		}
		var values store.Values
		if err := values.UnmarshalJSON([]byte(encoded)); err != nil {
			rows.Close()
			return fmt.Errorf("decode document %s: %w", id, err)
		}
		updated, changed, err := renameSQLiteValues(source.Fields, target.Fields, renamed, values)
		if err != nil {
			rows.Close()
			return fmt.Errorf("document %s: %w", id, err)
		}
		if !changed {
			continue
		}
		rewritten, err := updated.MarshalJSON()
		if err != nil {
			rows.Close()
			return fmt.Errorf("encode document %s: %w", id, err)
		}
		documents = append(documents, documentUpdate{id: id, values: string(rewritten)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	if err := rows.Close(); err != nil {
		return translateError(err)
	}
	for _, document := range documents {
		if _, err := connection.ExecContext(ctx, `UPDATE ridu_documents SET values_json = ? WHERE collection_id = ? AND id = ?`, document.values, string(source.ID), document.id); err != nil {
			return translateError(err)
		}
	}
	if err := renameSQLitePublishedValues(ctx, connection, source, target, renamed); err != nil {
		return err
	}

	rows, err = connection.QueryContext(ctx, `SELECT document_id, revision, snapshot_json
FROM ridu_versions WHERE collection_id = ? ORDER BY document_id, revision`, string(source.ID))
	if err != nil {
		return translateError(err)
	}
	type versionUpdate struct {
		documentID string
		revision   int
		snapshot   string
	}
	var versions []versionUpdate
	for rows.Next() {
		var update versionUpdate
		var encoded string
		if err := rows.Scan(&update.documentID, &update.revision, &encoded); err != nil {
			rows.Close()
			return translateError(err)
		}
		var snapshot store.Document
		if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
			rows.Close()
			return fmt.Errorf("decode version snapshot %s revision %d: %w", update.documentID, update.revision, err)
		}
		updated, changed, err := renameSQLiteValues(source.Fields, target.Fields, renamed, snapshot.Values)
		if err != nil {
			rows.Close()
			return fmt.Errorf("version snapshot %s revision %d: %w", update.documentID, update.revision, err)
		}
		if !changed {
			continue
		}
		snapshot.Values = updated
		rewritten, err := json.Marshal(snapshot)
		if err != nil {
			rows.Close()
			return fmt.Errorf("encode version snapshot %s revision %d: %w", update.documentID, update.revision, err)
		}
		update.snapshot = string(rewritten)
		versions = append(versions, update)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	if err := rows.Close(); err != nil {
		return translateError(err)
	}
	for _, version := range versions {
		if _, err := connection.ExecContext(ctx, `UPDATE ridu_versions SET snapshot_json = ?
WHERE collection_id = ? AND document_id = ? AND revision = ?`, version.snapshot, string(source.ID), version.documentID, version.revision); err != nil {
			return translateError(err)
		}
	}
	return nil
}

func renameSQLitePublishedValues(ctx context.Context, connection *sql.Conn, source, target schema.Collection, renamed map[schema.StableID]schema.Field) error {
	rows, err := connection.QueryContext(ctx, `SELECT id, values_json FROM ridu_published_documents WHERE collection_id = ? ORDER BY id`, string(source.ID))
	if err != nil {
		return translateError(err)
	}
	type update struct{ id, values string }
	var updates []update
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			rows.Close()
			return translateError(err)
		}
		var values store.Values
		if err := values.UnmarshalJSON([]byte(encoded)); err != nil {
			rows.Close()
			return fmt.Errorf("decode published document %s: %w", id, err)
		}
		rewritten, changed, err := renameSQLiteValues(source.Fields, target.Fields, renamed, values)
		if err != nil {
			rows.Close()
			return fmt.Errorf("published document %s: %w", id, err)
		}
		if changed {
			encoded, err := rewritten.MarshalJSON()
			if err != nil {
				rows.Close()
				return err
			}
			updates = append(updates, update{id: id, values: string(encoded)})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	rows.Close()
	for _, item := range updates {
		if _, err := connection.ExecContext(ctx, `UPDATE ridu_published_documents SET values_json = ? WHERE collection_id = ? AND id = ?`, item.values, string(source.ID), item.id); err != nil {
			return translateError(err)
		}
	}
	return nil
}

// renameSQLiteValues moves renamed keys in one object and descends into the
// containers both schemas share, where a nested field may be renamed.
func renameSQLiteValues(sourceFields, targetFields []schema.Field, renamed map[schema.StableID]schema.Field, values store.Values) (store.Values, bool, error) {
	targetByID := make(map[schema.StableID]schema.Field, len(targetFields))
	for _, field := range targetFields {
		targetByID[field.ID] = field
	}
	result := store.CloneValues(values)
	changed := false
	for _, source := range sourceFields {
		value, present := result[source.Name]
		if !present {
			continue
		}
		if target, isRenamed := renamed[source.ID]; isRenamed {
			// Moving the key carries every locale and nested value with it.
			if existing, occupied := result[target.Name]; occupied && !existing.IsZero() && existing.Kind() != store.ValueNull {
				return nil, false, fmt.Errorf("field %s already has a value, so %s cannot be renamed onto it", target.Path, source.Path)
			}
			delete(result, source.Name)
			result[target.Name] = value
			changed = true
			continue
		}
		target, shared := targetByID[source.ID]
		if !shared {
			continue
		}
		updated, fieldChanged, err := renameSQLiteFieldValue(source, target, renamed, value)
		if err != nil {
			return nil, false, err
		}
		if fieldChanged {
			result[source.Name] = updated
			changed = true
		}
	}
	return result, changed, nil
}

func renameSQLiteFieldValue(source, target schema.Field, renamed map[schema.StableID]schema.Field, value store.Value) (store.Value, bool, error) {
	if source.Localized {
		localized, valid := value.CopyObject()
		if !valid {
			return value, false, nil
		}
		source.Localized, target.Localized = false, false
		result := store.CloneValues(localized)
		changed := false
		for locale, localizedValue := range localized {
			updated, localeChanged, err := renameSQLiteFieldValue(source, target, renamed, localizedValue)
			if err != nil {
				return value, false, err
			}
			if localeChanged {
				result[locale] = updated
				changed = true
			}
		}
		if changed {
			return store.Object(result), true, nil
		}
		return value, false, nil
	}
	switch source.Type {
	case schema.FieldTypeGroup:
		object, valid := value.CopyObject()
		if !valid || source.Nested == nil || target.Nested == nil {
			return value, false, nil
		}
		updated, changed, err := renameSQLiteValues(source.Nested.ResolvedFields(), target.Nested.ResolvedFields(), renamed, object)
		if err != nil || !changed {
			return value, false, err
		}
		return store.Object(updated), true, nil
	case schema.FieldTypeArray:
		items, valid := value.CopyList()
		if !valid || source.Nested == nil || target.Nested == nil {
			return value, false, nil
		}
		return renameSQLiteItems(items, value, renamed, func(store.Values) ([]schema.Field, []schema.Field, bool) {
			return source.Nested.ResolvedFields(), target.Nested.ResolvedFields(), true
		})
	case schema.FieldTypeBlocks:
		items, valid := value.CopyList()
		if !valid || source.Blocks == nil || target.Blocks == nil {
			return value, false, nil
		}
		sourceTypes := make(map[string]schema.BlockType, len(source.Blocks.ResolvedTypes()))
		for _, block := range source.Blocks.ResolvedTypes() {
			sourceTypes[block.Slug] = block
		}
		targetTypes := make(map[string]schema.BlockType, len(target.Blocks.ResolvedTypes()))
		for _, block := range target.Blocks.ResolvedTypes() {
			targetTypes[block.Slug] = block
		}
		return renameSQLiteItems(items, value, renamed, func(item store.Values) ([]schema.Field, []schema.Field, bool) {
			slug, _ := item["blockType"].StringValue()
			sourceBlock, known := sourceTypes[slug]
			targetBlock, survives := targetTypes[slug]
			return sourceBlock.ResolvedFields(), targetBlock.ResolvedFields(), known && survives
		})
	}
	return value, false, nil
}

// renameSQLiteItems applies renames to each row of an array or blocks value.
func renameSQLiteItems(items []store.Value, original store.Value, renamed map[schema.StableID]schema.Field, fields func(store.Values) ([]schema.Field, []schema.Field, bool)) (store.Value, bool, error) {
	updated := append([]store.Value(nil), items...)
	changed := false
	for index, item := range items {
		object, valid := item.CopyObject()
		if !valid {
			continue
		}
		sourceFields, targetFields, known := fields(object)
		if !known {
			continue
		}
		itemValues, itemChanged, err := renameSQLiteValues(sourceFields, targetFields, renamed, object)
		if err != nil {
			return original, false, err
		}
		if itemChanged {
			updated[index] = store.Object(itemValues)
			changed = true
		}
	}
	if !changed {
		return original, false, nil
	}
	return store.List(updated...), true, nil
}
