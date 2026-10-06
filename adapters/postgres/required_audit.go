package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/requiredfield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// postgresRequiredAuditScope names the rows a PostgreSQL required-value audit
// reads. Drafts defer required fields, and publication validates them.
const postgresRequiredAuditScope = "every stored document except draft working copies, including trashed documents and every published row"

// auditPostgresRequiredValues refuses requirements that stored rows leave
// without a value: every working row of a resource without drafts, non-draft
// working rows of a resource with drafts, and every live row. lock holds SHARE
// locks so no writer changes an audited table before the transaction commits.
//
// Storage may still have its previous layout before migration DDL or
// development synchronization. A top-level column that does not exist yet
// reads as absent in every row unless its column default will fill existing
// rows when the column is added.
func auditPostgresRequiredValues(ctx context.Context, transaction *sql.Tx, locales []schema.LocaleCode, requirements []requiredfield.Requirement, lock, development bool) error {
	if len(requirements) == 0 {
		return nil
	}
	audit := requiredfield.NewAudit(requirements)
	for _, group := range requiredfield.Groups(requirements) {
		resource := group.Resource
		for index, table := range documentTables(resource, resource.ID) {
			exists, err := transactionTableExists(ctx, transaction, table)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			if lock {
				if _, err := transaction.ExecContext(ctx, `LOCK TABLE `+quote(table)+` IN SHARE MODE`); err != nil {
					return fmt.Errorf("lock %s for the required-value audit: %w", resource.Slug, err)
				}
			}
			roots, err := postgresAuditRoots(ctx, transaction, table, group.Roots, locales)
			if err != nil {
				return err
			}
			// A working row of a resource with drafts holds a draft when its
			// status is draft or its live row has pending draft changes.
			filter := ""
			if index == 0 && resource.Versions != nil && resource.Versions.Drafts {
				filter = fmt.Sprintf(` WHERE working._status <> 'draft' AND NOT EXISTS (SELECT 1 FROM %s AS live WHERE live.id = working.id AND live.has_draft_changes)`,
					quote(publishedCollectionTable(resource.ID)))
			}
			columns := []string{"working.id"}
			for _, root := range roots {
				columns = append(columns, root.selected...)
			}
			if err := scanPostgresRequiredRows(ctx, transaction, `SELECT `+strings.Join(columns, ", ")+` FROM `+quote(table)+` AS working`+filter+` ORDER BY working.id`, roots, func(id string, values store.Values) {
				audit.Inspect(resource.ID, id, values)
			}); err != nil {
				return fmt.Errorf("audit required values of %s: %w", resource.Slug, err)
			}
		}
	}
	return audit.Err(postgresRequiredAuditScope, development)
}

// postgresAuditRoot reads one top-level field from a document table. A
// column the table does not have yet contributes fill, its default for rows
// that exist when the column is added, or nothing.
type postgresAuditRoot struct {
	field    schema.Field
	locales  []schema.LocaleCode
	selected []string
	present  []bool
	fill     store.Value
}

func postgresAuditRoots(ctx context.Context, transaction *sql.Tx, table string, fields []schema.Field, locales []schema.LocaleCode) ([]postgresAuditRoot, error) {
	roots := make([]postgresAuditRoot, 0, len(fields))
	for _, field := range fields {
		root := postgresAuditRoot{field: field}
		names := []string{fieldColumn(field.ID)}
		if field.Localized {
			root.locales = locales
			names = names[:0]
			for _, locale := range locales {
				names = append(names, localizedFieldColumn(field.ID, locale))
			}
		}
		for _, name := range names {
			exists, err := transactionColumnExists(ctx, transaction, table, name)
			if err != nil {
				return nil, err
			}
			root.present = append(root.present, exists)
			if exists {
				root.selected = append(root.selected, quote(name))
			}
		}
		if !field.Localized && !root.present[0] {
			root.fill = postgresColumnDefault(field)
		}
		roots = append(roots, root)
	}
	return roots, nil
}

// postgresColumnDefault is the value PostgreSQL writes into existing rows
// when it adds the field's column, matching atlasFieldColumn.
func postgresColumnDefault(field schema.Field) store.Value {
	if field.Default == nil {
		return store.Value{}
	}
	switch physicalColumnKind(field) {
	case columnText:
		return store.String(*field.Default)
	case columnNumber:
		if number, err := strconv.ParseFloat(*field.Default, 64); err == nil {
			return store.Number(number)
		}
	case columnBoolean:
		if flag, err := strconv.ParseBool(*field.Default); err == nil {
			return store.Boolean(flag)
		}
	}
	return store.Value{}
}

func scanPostgresRequiredRows(ctx context.Context, transaction *sql.Tx, statement string, roots []postgresAuditRoot, inspect func(string, store.Values)) error {
	rows, err := transaction.QueryContext(ctx, statement)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cell struct {
		root   int
		locale schema.LocaleCode
		value  columnValue
	}
	var cells []cell
	for index, root := range roots {
		for offset, present := range root.present {
			if !present {
				continue
			}
			locale := schema.LocaleCode("")
			if root.field.Localized {
				locale = root.locales[offset]
			}
			cells = append(cells, cell{root: index, locale: locale, value: columnValue{kind: physicalColumnKind(root.field)}})
		}
	}
	var id string
	destinations := []any{&id}
	for index := range cells {
		destinations = append(destinations, cells[index].value.destination())
	}
	for rows.Next() {
		if err := rows.Scan(destinations...); err != nil {
			return err
		}
		values := make(store.Values, len(roots))
		for _, root := range roots {
			if root.field.Localized {
				values[root.field.Name] = store.Object(store.Values{})
			} else if !root.fill.IsZero() {
				values[root.field.Name] = root.fill
			}
		}
		for _, cell := range cells {
			root := roots[cell.root]
			value, present, err := cell.value.value(root.field)
			if err != nil {
				return err
			}
			if !present {
				continue
			}
			if !root.field.Localized {
				values[root.field.Name] = value
				continue
			}
			if value.Kind() == store.ValueNull {
				continue
			}
			translations, _ := values[root.field.Name].CopyObject()
			translations[string(cell.locale)] = value
			values[root.field.Name] = store.Object(translations)
		}
		inspect(id, values)
	}
	return rows.Err()
}

// preflightPostgresRequiredValues runs an artifact's required-value audit
// before any of its phases commits, when only stored values can decide it:
// no data transform can write a value and no content rename moves one. The
// audit step itself still runs, locked, in the final transaction phase.
func preflightPostgresRequiredValues(ctx context.Context, transaction *sql.Tx, artifact ridumigration.Artifact) error {
	var audits []ridumigration.AuditRequiredValuesPayload
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			switch step.Kind {
			case ridumigration.StepDataTransform, ridumigration.StepRenameContent:
				return nil
			case ridumigration.StepAuditRequiredValues:
				var payload ridumigration.AuditRequiredValuesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return err
				}
				audits = append(audits, payload)
			}
		}
	}
	for _, payload := range audits {
		requirements, locales, err := postgresRequiredRequirements(artifact, payload)
		if err != nil {
			return err
		}
		if err := auditPostgresRequiredValues(ctx, transaction, locales, requirements, false, false); err != nil {
			return err
		}
	}
	return nil
}

// postgresRequiredRequirements resolves an audit step's payload against the
// artifact's after manifest.
func postgresRequiredRequirements(artifact ridumigration.Artifact, payload ridumigration.AuditRequiredValuesPayload) ([]requiredfield.Requirement, []schema.LocaleCode, error) {
	after, err := artifact.AfterManifest()
	if err != nil {
		return nil, nil, err
	}
	requirements, err := requiredfield.Resolve(after.Snapshot(), payload.Fields)
	if err != nil {
		return nil, nil, err
	}
	var locales []schema.LocaleCode
	if localization := after.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	return requirements, locales, nil
}
