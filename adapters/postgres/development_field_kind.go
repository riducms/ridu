package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// HasMigrationHistory reports applied or incomplete immutable history. A
// development recovery must use the migration runner once that history exists.
func (backend *Store) HasMigrationHistory(ctx context.Context) (bool, error) {
	return postgresHasFieldRecoveryHistory(ctx, backend.pool)
}

type fieldRecoveryQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func postgresHasFieldRecoveryHistory(ctx context.Context, transaction fieldRecoveryQueryer) (bool, error) {
	for _, table := range []string{"ridu_migrations", "ridu_migration_steps"} {
		var exists bool
		if err := transaction.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+quote(table)+`)`).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

// ReviewDevelopmentFieldKinds counts stored current values and retained
// snapshots without interpreting them under the candidate field kinds.
func (backend *Store) ReviewDevelopmentFieldKinds(ctx context.Context, before, after schema.Manifest) ([]fieldchange.Report, error) {
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	reports := fieldchange.Reports(changes)
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollbackPostgresTransaction(ctx, transaction)
	err = scanPostgresFieldKinds(ctx, transaction, before, changes, reports, false)
	return reports, err
}

// ClearDevelopmentFieldKinds atomically clears precisely the confirmed field
// values in current documents and every snapshot. It refuses immutable history
// and changes physical scalar column types only after their values are cleared.
func (backend *Store) ClearDevelopmentFieldKinds(ctx context.Context, before, after schema.Manifest, expected []fieldchange.Report) error {
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	if err := fieldchange.ValidateClear(changes); err != nil {
		return err
	}
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer rollbackPostgresTransaction(ctx, transaction)
	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}
	managed, err := postgresHasFieldRecoveryHistory(ctx, transaction)
	if err != nil {
		return err
	}
	if managed {
		return fmt.Errorf("this PostgreSQL database is managed by ridu migrate; field-kind recovery requires a reviewed migration")
	}
	for _, resource := range fieldchange.AffectedResources(changes) {
		if _, err := transaction.Exec(ctx, `LOCK TABLE `+quote(collectionTable(resource.ID))+` IN ACCESS EXCLUSIVE MODE`); err != nil {
			return err
		}
	}
	reports := fieldchange.Reports(changes)
	if err := scanPostgresFieldKinds(ctx, transaction, before, changes, reports, false); err != nil {
		return err
	}
	if err := fieldchange.ConfirmCounts(expected, reports); err != nil {
		return err
	}
	// An optional replacement may clear an originally required column. Relax
	// that old constraint in this same transaction, before writing NULL.
	// ValidateClear already refused a replacement that remains required.
	for _, change := range changes {
		if len(change.Containers) != 0 || !change.Before.Required || change.After.Required || change.Before.Localized {
			continue
		}
		if _, err := transaction.Exec(ctx, `ALTER TABLE `+quote(collectionTable(change.Resource.ID))+` ALTER COLUMN `+quote(fieldColumn(change.Before.ID))+` DROP NOT NULL`); err != nil {
			return err
		}
	}
	reports = fieldchange.Reports(changes)
	if err := scanPostgresFieldKinds(ctx, transaction, before, changes, reports, true); err != nil {
		return err
	}
	if err := retypeEmptyPostgresColumns(before, changes, func(statement string) error {
		_, err := transaction.Exec(ctx, statement)
		return err
	}); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

// retypeEmptyPostgresColumns changes top-level columns whose values are
// already empty to the candidate's physical type. PostgreSQL cannot cast
// between these scalar types, so each column restarts with NULL. A foreign key
// or default of the old kind is dropped first; the development plan creates
// the candidate's.
func retypeEmptyPostgresColumns(before schema.Manifest, changes []fieldchange.Change, exec func(string) error) error {
	var locales []schema.LocaleCode
	if localization := before.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	for _, change := range changes {
		if len(change.Containers) != 0 || change.Payload != nil || columnType(change.Before) == columnType(change.After) {
			continue
		}
		table := quote(collectionTable(change.Resource.ID))
		type physicalColumn struct{ name, constraintKey string }
		key := string(change.Resource.ID) + ":" + string(change.Before.ID)
		columns := []physicalColumn{{name: fieldColumn(change.Before.ID), constraintKey: key}}
		if change.Before.Localized {
			columns = nil
			for _, locale := range locales {
				columns = append(columns, physicalColumn{name: localizedFieldColumn(change.Before.ID, locale), constraintKey: key + ":" + string(locale)})
			}
		}
		for _, column := range columns {
			if hasForeignKey(change.Before) {
				if err := exec(`ALTER TABLE ` + table + ` DROP CONSTRAINT IF EXISTS ` + quote("z_fk_"+identifierHash(column.constraintKey))); err != nil {
					return err
				}
			}
			if err := exec(`ALTER TABLE ` + table + ` ALTER COLUMN ` + quote(column.name) + ` DROP DEFAULT`); err != nil {
				return err
			}
			if err := exec(`ALTER TABLE ` + table + ` ALTER COLUMN ` + quote(column.name) + ` TYPE ` + columnType(change.After) + ` USING NULL::` + columnType(change.After)); err != nil {
				return fmt.Errorf("change emptied field column type: %w", err)
			}
		}
	}
	return nil
}

func scanPostgresFieldKinds(ctx context.Context, transaction pgx.Tx, before schema.Manifest, changes []fieldchange.Change, reports []fieldchange.Report, clear bool) error {
	var locales []schema.LocaleCode
	if localization := before.Snapshot().Application.Localization; localization != nil {
		for _, locale := range localization.Locales {
			locales = append(locales, locale.Code)
		}
	}
	for _, resource := range fieldchange.AffectedResources(changes) {
		var exists bool
		if err := transaction.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, collectionTable(resource.ID)).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		fields := storedSchemaFields(resource.Fields)
		// Read the physical values without casting them through either schema.
		// Drift in another column must not bypass an incompatible JSON root, and
		// an already-cleared scalar can have its candidate physical type here.
		rows, err := transaction.Query(ctx, `SELECT id, to_jsonb(content) FROM `+quote(collectionTable(resource.ID))+` AS content ORDER BY id`)
		if err != nil {
			return err
		}
		var rewritten []store.Document
		for rows.Next() {
			document := store.Document{Values: make(store.Values)}
			var encoded []byte
			if err := rows.Scan(&document.ID, &encoded); err != nil {
				rows.Close()
				return err
			}
			var physical map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &physical); err != nil {
				rows.Close()
				return err
			}
			for _, field := range fields {
				if !field.Localized {
					if raw, exists := physical[fieldColumn(field.ID)]; exists {
						var value store.Value
						if err := value.UnmarshalJSON(raw); err != nil {
							rows.Close()
							return err
						}
						document.Values[field.Name] = value
					}
					continue
				}
				localized := make(store.Values)
				for _, locale := range locales {
					if raw, exists := physical[localizedFieldColumn(field.ID, locale)]; exists {
						var value store.Value
						if err := value.UnmarshalJSON(raw); err != nil {
							rows.Close()
							return err
						}
						localized[string(locale)] = value
					}
				}
				document.Values[field.Name] = store.Object(localized)
			}
			values, found, err := fieldchange.Process(changes, reports, resource.ID, document.Values, false, clear)
			if err != nil {
				rows.Close()
				return err
			}
			if clear && found {
				document.Values = values
				rewritten = append(rewritten, document)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, document := range rewritten {
			var assignments []string
			arguments := []any{document.ID}
			roots := make(map[string]bool)
			for _, change := range changes {
				if change.Resource.ID == resource.ID {
					root := change.Before
					if len(change.Containers) != 0 {
						root = change.Containers[0].Field
					}
					roots[root.Name] = true
				}
			}
			for _, field := range fields {
				if !roots[field.Name] {
					continue
				}
				value, exists := document.Values[field.Name]
				if !exists {
					value = store.Null()
				}
				columns := []string{fieldColumn(field.ID)}
				values := []store.Value{value}
				if field.Localized {
					columns = nil
					values = nil
					for _, locale := range locales {
						columns = append(columns, localizedFieldColumn(field.ID, locale))
						values = append(values, value.Get(string(locale)))
					}
				}
				unlocalized := field
				unlocalized.Localized = false
				for index, column := range columns {
					encoded, err := databaseValue(unlocalized, values[index])
					if err != nil {
						return err
					}
					arguments = append(arguments, encoded)
					assignments = append(assignments, fmt.Sprintf("%s = $%d", quote(column), len(arguments)))
				}
			}
			if _, err := transaction.Exec(ctx, `UPDATE `+quote(collectionTable(resource.ID))+` SET `+strings.Join(assignments, ", ")+` WHERE id = $1`, arguments...); err != nil {
				return err
			}
			wrapped := &documentTransaction{transaction: transaction, tableExists: make(map[string]bool)}
			if err := wrapped.replaceDocumentReferences(ctx, resource, document); err != nil {
				return err
			}
		}
		if err := scanPostgresFieldKindSnapshots(ctx, transaction, resource, changes, reports, clear); err != nil {
			return err
		}
	}
	return nil
}

func scanPostgresFieldKindSnapshots(ctx context.Context, transaction pgx.Tx, resource schema.Collection, changes []fieldchange.Change, reports []fieldchange.Report, clear bool) error {
	for _, table := range []string{"ridu_versions", "ridu_published_documents"} {
		if err := scanPostgresFieldKindSnapshotTable(ctx, transaction, table, resource, changes, reports, clear); err != nil {
			return err
		}
	}
	return nil
}

func scanPostgresFieldKindSnapshotTable(ctx context.Context, transaction pgx.Tx, table string, resource schema.Collection, changes []fieldchange.Change, reports []fieldchange.Report, clear bool) error {
	var exists bool
	if err := transaction.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	rows, err := transaction.Query(ctx, `SELECT document_id, revision, snapshot FROM `+quote(table)+` WHERE collection_id = $1 ORDER BY document_id, revision`, string(resource.ID))
	if err != nil {
		return err
	}
	type rewrite struct {
		id       string
		revision int
		value    []byte
	}
	var rewrites []rewrite
	for rows.Next() {
		var update rewrite
		if err := rows.Scan(&update.id, &update.revision, &update.value); err != nil {
			rows.Close()
			return err
		}
		var document store.Document
		if err := json.Unmarshal(update.value, &document); err != nil {
			rows.Close()
			return err
		}
		values, found, err := fieldchange.Process(changes, reports, resource.ID, document.Values, true, clear)
		if err != nil {
			rows.Close()
			return err
		}
		if !clear || !found {
			continue
		}
		document.Values = values
		update.value, err = json.Marshal(document)
		if err != nil {
			rows.Close()
			return err
		}
		rewrites = append(rewrites, update)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, update := range rewrites {
		if _, err := transaction.Exec(ctx, `UPDATE `+quote(table)+` SET snapshot = $1 WHERE collection_id = $2 AND document_id = $3 AND revision = $4`, update.value, string(resource.ID), update.id, update.revision); err != nil {
			return err
		}
	}
	return nil
}
