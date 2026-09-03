package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func canonicalizeSQLiteAuthIdentities(
	ctx context.Context,
	runner sqlRunner,
	before schema.Manifest,
	after schema.Manifest,
	resources []ridumigration.AuthIdentityResource,
) error {
	expected := ridumigration.RetainedAuthIdentityResources(before.Snapshot(), after.Snapshot())
	if !sameSQLiteAuthIdentityResources(resources, expected) {
		return fmt.Errorf("auth identity migration scope does not match the immutable manifest")
	}
	type update struct {
		collection schema.StableID
		id         string
		values     string
	}
	updates := make([]update, 0)
	for _, resource := range resources {
		rows, err := runner.QueryContext(ctx, `SELECT id, deleted_at, values_json
FROM ridu_documents WHERE collection_id = ? ORDER BY id`, string(resource.CollectionID))
		if err != nil {
			return translateError(err)
		}
		seenActive := make(map[string]struct{})
		for rows.Next() {
			var id, encoded string
			var deletedAt sql.NullInt64
			if err := rows.Scan(&id, &deletedAt, &encoded); err != nil {
				rows.Close()
				return translateError(err)
			}
			var values map[string]json.RawMessage
			if err := json.Unmarshal([]byte(encoded), &values); err != nil {
				rows.Close()
				return fmt.Errorf("decode stored auth document in collection %s: %w", resource.CollectionID, err)
			}
			raw, exists := values[resource.FieldName]
			var identity string
			if !exists || json.Unmarshal(raw, &identity) != nil {
				rows.Close()
				return fmt.Errorf("stored auth identity in collection %s is missing or malformed", resource.CollectionID)
			}
			canonical := store.CanonicalAuthIdentity(identity)
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
			if canonical == identity {
				continue
			}
			values[resource.FieldName], _ = json.Marshal(canonical)
			rewritten, err := json.Marshal(values)
			if err != nil {
				rows.Close()
				return fmt.Errorf("encode stored auth document in collection %s: %w", resource.CollectionID, err)
			}
			updates = append(updates, update{collection: resource.CollectionID, id: id, values: string(rewritten)})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return translateError(err)
		}
		if err := rows.Close(); err != nil {
			return translateError(err)
		}
	}
	for _, item := range updates {
		result, err := runner.ExecContext(ctx, `UPDATE ridu_documents SET values_json = ?
WHERE collection_id = ? AND id = ?`, item.values, string(item.collection), item.id)
		if err != nil {
			return translateError(err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("auth identity migration lost a document in collection %s", item.collection)
		}
	}
	return nil
}

func sameSQLiteAuthIdentityResources(left, right []ridumigration.AuthIdentityResource) bool {
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
