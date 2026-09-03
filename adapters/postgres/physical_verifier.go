package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	atlasschema "ariga.io/atlas/sql/schema"
	"github.com/riducms/ridu/schema"
)

// verifyPostgresPhysicalState runs the one non-mutating physical contract
// check shared by readiness, status, and completed migration preflight. A nil
// manifest represents the empty, pre-migration application schema.
func verifyPostgresPhysicalState(ctx context.Context, connection *sql.Conn, manifest *schema.Manifest, contract atlasPlannerContract) error {
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	if manifest == nil {
		err = assertEmptyPhysicalSchema(ctx, transaction)
	} else {
		err = assertPhysicalSchemaForContract(ctx, transaction, *manifest, contract)
	}
	if err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Rollback()
}

type postgresPhysicalIndex struct {
	table             string
	name              string
	valid             bool
	ready             bool
	unique            bool
	exclusion         bool
	primary           bool
	replicaIdentity   bool
	checkXmin         bool
	method            string
	withoutPredicate  bool
	withoutExpression bool
	directColumns     bool
	defaultOperator   bool
	defaultCollation  bool
}

// classifyPostgresManagedPhysicalState verifies catalogue facts Atlas does not
// model (index build health and trigger enablement) and returns the exact extra
// indexes that are safe to omit from Atlas's required-schema comparison.
func classifyPostgresManagedPhysicalState(ctx context.Context, transaction *sql.Tx, expected *atlasschema.Schema) (map[string]map[string]struct{}, error) {
	expectedTables := make(map[string]*atlasschema.Table, len(expected.Tables))
	requiredIndexes := make(map[string]map[string]bool, len(expected.Tables))
	requiredPrimary := make(map[string]bool, len(expected.Tables))
	for _, table := range expected.Tables {
		expectedTables[table.Name] = table
		required := make(map[string]bool, len(table.Indexes))
		if table.PrimaryKey != nil {
			requiredPrimary[table.Name] = true
		}
		for _, index := range table.Indexes {
			required[index.Name] = index.Unique
		}
		requiredIndexes[table.Name] = required
	}

	rows, err := transaction.QueryContext(ctx, `SELECT
table_class.relname,
index_class.relname,
index_catalog.indisvalid,
index_catalog.indisready,
index_catalog.indisunique,
index_catalog.indisexclusion,
index_catalog.indisprimary,
index_catalog.indisreplident,
index_catalog.indcheckxmin,
access_method.amname,
index_catalog.indpred IS NULL,
index_catalog.indexprs IS NULL,
NOT EXISTS (
  SELECT 1
  FROM unnest(index_catalog.indkey::smallint[]) AS attributes(attribute_number)
  WHERE attributes.attribute_number <= 0
),
NOT EXISTS (
  SELECT 1
  FROM unnest(index_catalog.indclass::oid[]) WITH ORDINALITY AS classes(opclass_oid, ordinal)
  JOIN pg_opclass operator_class ON operator_class.oid = classes.opclass_oid
  WHERE classes.ordinal <= index_catalog.indnkeyatts AND NOT operator_class.opcdefault
),
NOT EXISTS (
  SELECT 1
  FROM unnest(index_catalog.indkey::smallint[], index_catalog.indcollation::oid[])
       WITH ORDINALITY AS keys(attribute_number, collation_oid, ordinal)
  JOIN pg_attribute attribute
    ON attribute.attrelid = index_catalog.indrelid AND attribute.attnum = keys.attribute_number
  WHERE keys.ordinal <= index_catalog.indnkeyatts
    AND keys.collation_oid <> 0
    AND keys.collation_oid <> attribute.attcollation
)
FROM pg_index index_catalog
JOIN pg_class index_class ON index_class.oid = index_catalog.indexrelid
JOIN pg_class table_class ON table_class.oid = index_catalog.indrelid
JOIN pg_namespace namespace ON namespace.oid = table_class.relnamespace
JOIN pg_am access_method ON access_method.oid = index_class.relam
WHERE namespace.nspname = current_schema()
ORDER BY table_class.relname, index_class.relname`)
	if err != nil {
		return nil, fmt.Errorf("inspect PostgreSQL indexes: %w", err)
	}
	defer rows.Close()

	foundRequired := make(map[string]map[string]struct{}, len(expected.Tables))
	foundPrimary := make(map[string]bool, len(expected.Tables))
	allowed := make(map[string]map[string]struct{})
	for rows.Next() {
		var index postgresPhysicalIndex
		if err := rows.Scan(
			&index.table,
			&index.name,
			&index.valid,
			&index.ready,
			&index.unique,
			&index.exclusion,
			&index.primary,
			&index.replicaIdentity,
			&index.checkXmin,
			&index.method,
			&index.withoutPredicate,
			&index.withoutExpression,
			&index.directColumns,
			&index.defaultOperator,
			&index.defaultCollation,
		); err != nil {
			return nil, fmt.Errorf("inspect PostgreSQL indexes: %w", err)
		}
		if expectedTables[index.table] == nil {
			continue
		}
		if index.primary && requiredPrimary[index.table] {
			foundPrimary[index.table] = true
			if !index.valid || !index.ready {
				return nil, fmt.Errorf("PostgreSQL physical schema drift: required primary index %s.%s is not usable (indisvalid=%t, indisready=%t)", index.table, index.name, index.valid, index.ready)
			}
			continue
		}
		if _, required := requiredIndexes[index.table][index.name]; required {
			if foundRequired[index.table] == nil {
				foundRequired[index.table] = make(map[string]struct{})
			}
			foundRequired[index.table][index.name] = struct{}{}
			if !index.valid || !index.ready {
				return nil, fmt.Errorf("PostgreSQL physical schema drift: required index %s.%s is not usable (indisvalid=%t, indisready=%t)", index.table, index.name, index.valid, index.ready)
			}
			continue
		}

		ordinary := !postgresPhysicalNameIsReserved(index.name) && index.valid && index.ready &&
			!index.unique && !index.exclusion && !index.primary && !index.replicaIdentity && !index.checkXmin &&
			index.method == "btree" && index.withoutPredicate && index.withoutExpression && index.directColumns &&
			index.defaultOperator && index.defaultCollation
		if !ordinary {
			return nil, fmt.Errorf("PostgreSQL physical schema drift: unexpected behavior-changing index %s.%s exists", index.table, index.name)
		}
		if allowed[index.table] == nil {
			allowed[index.table] = make(map[string]struct{})
		}
		allowed[index.table][index.name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect PostgreSQL indexes: %w", err)
	}
	for table := range requiredPrimary {
		if !foundPrimary[table] {
			return nil, fmt.Errorf("PostgreSQL physical schema drift: required primary index on %s is missing", table)
		}
	}
	for table, indexes := range requiredIndexes {
		for name, unique := range indexes {
			if _, found := foundRequired[table][name]; found {
				continue
			}
			kind := "index"
			if unique {
				kind = "unique index"
			}
			return nil, fmt.Errorf("PostgreSQL physical schema drift: required %s %s.%s is missing", kind, table, name)
		}
	}

	triggerRows, err := transaction.QueryContext(ctx, `SELECT table_class.relname, trigger_catalog.tgname
FROM pg_trigger trigger_catalog
JOIN pg_class table_class ON table_class.oid = trigger_catalog.tgrelid
JOIN pg_namespace namespace ON namespace.oid = table_class.relnamespace
WHERE namespace.nspname = current_schema()
  AND NOT trigger_catalog.tgisinternal
  AND trigger_catalog.tgenabled <> 'D'
ORDER BY table_class.relname, trigger_catalog.tgname`)
	if err != nil {
		return nil, fmt.Errorf("inspect PostgreSQL triggers: %w", err)
	}
	defer triggerRows.Close()
	for triggerRows.Next() {
		var table, name string
		if err := triggerRows.Scan(&table, &name); err != nil {
			return nil, fmt.Errorf("inspect PostgreSQL triggers: %w", err)
		}
		if expectedTables[table] != nil {
			return nil, fmt.Errorf("PostgreSQL physical schema drift: enabled trigger %s.%s can change Ridu writes", table, name)
		}
	}
	if err := triggerRows.Err(); err != nil {
		return nil, fmt.Errorf("inspect PostgreSQL triggers: %w", err)
	}
	return allowed, nil
}

func postgresPhysicalNameIsReserved(name string) bool {
	for _, prefix := range []string{"ridu_", "z_"} {
		if len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}
