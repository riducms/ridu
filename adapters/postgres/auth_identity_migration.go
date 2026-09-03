package postgres

import (
	"context"
	"database/sql"
	"fmt"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func canonicalizePostgresAuthIdentities(
	ctx context.Context,
	transaction *sql.Tx,
	before schema.Manifest,
	after schema.Manifest,
	resources []ridumigration.AuthIdentityResource,
) error {
	expected := ridumigration.RetainedAuthIdentityResources(before.Snapshot(), after.Snapshot())
	if !samePostgresAuthIdentityResources(resources, expected) {
		return fmt.Errorf("auth identity migration scope does not match the immutable manifest")
	}
	type update struct {
		table, column string
		collection    schema.StableID
		id, identity  string
	}
	updates := make([]update, 0)
	for _, resource := range resources {
		table := collectionTable(resource.CollectionID)
		column := fieldColumn(resource.FieldID)
		rows, err := transaction.QueryContext(ctx, fmt.Sprintf(`SELECT id, deleted_at, %s
FROM %s ORDER BY id`, quote(column), quote(table)))
		if err != nil {
			return err
		}
		seenActive := make(map[string]struct{})
		for rows.Next() {
			var id string
			var deletedAt sql.NullTime
			var identity sql.NullString
			if err := rows.Scan(&id, &deletedAt, &identity); err != nil {
				rows.Close()
				return err
			}
			if !identity.Valid {
				rows.Close()
				return fmt.Errorf("stored auth identity in collection %s is missing or malformed", resource.CollectionID)
			}
			canonical := store.CanonicalAuthIdentity(identity.String)
			if canonical == "" {
				rows.Close()
				return fmt.Errorf("stored auth identity in collection %s is empty after canonicalization", resource.CollectionID)
			}
			if !deletedAt.Valid {
				if _, duplicate := seenActive[canonical]; duplicate {
					rows.Close()
					return fmt.Errorf("auth identity canonicalization found an active collision in collection %s", resource.CollectionID)
				}
				seenActive[canonical] = struct{}{}
			}
			if canonical != identity.String {
				updates = append(updates, update{table: table, column: column, collection: resource.CollectionID, id: id, identity: canonical})
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for _, item := range updates {
		result, err := transaction.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE id = $2`, quote(item.table), quote(item.column)), item.identity, item.id)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("auth identity migration lost a document in collection %s", item.collection)
		}
	}
	return nil
}

func samePostgresAuthIdentityResources(left, right []ridumigration.AuthIdentityResource) bool {
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
