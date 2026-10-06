package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type publishedUniqueConstraint struct {
	paths     []query.Path
	localized bool
}

func publishedUniqueConstraints(collection schema.Collection) []publishedUniqueConstraint {
	var result []publishedUniqueConstraint
	for _, field := range collection.Fields {
		if !field.Unique {
			continue
		}
		path, err := query.NewPath(field.Name)
		if err != nil {
			continue // The resolved manifest has already validated field names.
		}
		result = append(result, publishedUniqueConstraint{paths: []query.Path{path}, localized: field.Localized})
	}
	for _, index := range collection.Indexes {
		if !index.Unique {
			continue
		}
		constraint := publishedUniqueConstraint{paths: index.Fields}
		for _, path := range index.Fields {
			constraint.localized = constraint.localized || atlasIndexChainLocalized(atlasIndexFieldChain(collection.Fields, path.Segments()))
		}
		result = append(result, constraint)
	}
	return result
}

// A collection-scoped transaction lock closes the cross-table race between
// working unique indexes and independently retained published values. Both
// direct-field and declared compound unique indexes share this admission.
func (transaction *documentTransaction) lockUniqueUnion(ctx context.Context, collection schema.Collection, locales []schema.LocaleCode, candidates ...store.Values) error {
	if collection.Versions == nil || len(publishedUniqueConstraints(collection)) == 0 {
		return nil
	}
	// Trash restoration reserves both heads; callers without candidate values
	// take the lock before loading them. Ordinary writes with no complete tuple
	// cannot claim a key and need neither the lock nor an admission query.
	if len(candidates) > 0 && !hasUniqueUnionCandidate(collection, locales, candidates...) {
		return nil
	}
	digest := sha256.Sum256([]byte("ridu-unique-union:\x00" + string(collection.ID)))
	key := int64(binary.BigEndian.Uint64(digest[:8]))
	if _, err := transaction.transaction.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		return translateError(err)
	}
	return nil
}

// Each candidate is a complete unique tuple. PostgreSQL unique indexes use
// NULLS DISTINCT: if any component is absent, the tuple occupies no key.
// Working and live tables share typed columns, so candidate keys are read from
// the persisted rows and compared with the same equality, and through the same
// indexes, as each table's unique constraint. Restoring trash admits both its
// working values and its independently retained live row.
func (transaction *documentTransaction) checkUniqueUnion(ctx context.Context, collection schema.Collection, locales []schema.LocaleCode, documents ...store.Document) error {
	if collection.Versions == nil || len(documents) == 0 {
		return nil
	}
	table := quote(collectionTable(collection.ID))
	live := quote(publishedCollectionTable(collection.ID))
	for _, constraint := range publishedUniqueConstraints(collection) {
		variants := []schema.LocaleCode{""}
		if constraint.localized {
			variants = locales
		}
		for _, locale := range variants {
			present := false
			for _, document := range documents {
				present = present || uniqueTuplePresent(collection, constraint, locale, document.Values)
			}
			if !present {
				continue
			}
			compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{locale}}
			keys := make([]string, 0, len(constraint.paths))
			matches := make([]string, 0, len(constraint.paths))
			complete := make([]string, 0, len(constraint.paths))
			for index, path := range constraint.paths {
				column, err := compiler.column(path)
				if err != nil {
					return err
				}
				alias := fmt.Sprintf("key_%d", index+1)
				keys = append(keys, column+" AS "+alias)
				matches = append(matches, column+" = candidate."+alias)
				complete = append(complete, "candidate."+alias+" IS NOT NULL")
			}
			candidate := fmt.Sprintf("SELECT %s FROM %s WHERE id = $1 AND deleted_at IS NULL", strings.Join(keys, ", "), table)
			if len(documents) > 1 {
				candidate += fmt.Sprintf(" UNION ALL SELECT %s FROM %s WHERE id = $1", strings.Join(keys, ", "), live)
			}
			statement := fmt.Sprintf(`WITH candidate AS (%s)
SELECT 1 FROM candidate WHERE %s AND (
EXISTS (SELECT 1 FROM %s WHERE id <> $1 AND deleted_at IS NULL AND %s)
OR EXISTS (SELECT 1 FROM %s WHERE id <> $1 AND deleted_at IS NULL AND %s)
) LIMIT 1`, candidate, strings.Join(complete, " AND "), table, strings.Join(matches, " AND "), live, strings.Join(matches, " AND "))
			var duplicate int
			err := transaction.transaction.QueryRow(ctx, statement, documents[0].ID).Scan(&duplicate)
			if err == nil {
				return store.ErrConflict
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return translateError(err)
			}
		}
	}
	return nil
}

func hasUniqueUnionCandidate(collection schema.Collection, locales []schema.LocaleCode, candidates ...store.Values) bool {
	if collection.Versions == nil {
		return false
	}
	for _, constraint := range publishedUniqueConstraints(collection) {
		variants := []schema.LocaleCode{""}
		if constraint.localized {
			variants = locales
		}
		for _, locale := range variants {
			for _, candidate := range candidates {
				if uniqueTuplePresent(collection, constraint, locale, candidate) {
					return true
				}
			}
		}
	}
	return false
}

func uniqueTuplePresent(collection schema.Collection, constraint publishedUniqueConstraint, locale schema.LocaleCode, values store.Values) bool {
	for _, path := range constraint.paths {
		chain := atlasIndexFieldChain(collection.Fields, path.Segments())
		if len(chain) != len(path.Segments()) {
			return false // The resolved manifest has already validated index paths.
		}
		value := values[chain[0].Name]
		for index, field := range chain {
			if index > 0 {
				value = value.Get(field.Name)
			}
			if field.Localized {
				value = value.Get(string(locale))
			}
		}
		if value.IsZero() || value.Kind() == store.ValueNull {
			return false
		}
	}
	return len(constraint.paths) > 0
}

// checkMigrationPublishedUniqueUnion audits every unique constraint of a
// versioned resource across its working and live tables. Each table's unique
// index cannot see the other table, so migrations run this audit before their
// DDL, skipping constraints whose columns do not exist yet, and again after it.
func checkMigrationPublishedUniqueUnion(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest) error {
	var locales []schema.LocaleCode
	if localization := manifest.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	resources := append(append([]schema.Collection(nil), manifest.Snapshot().Collections...), manifest.Snapshot().Globals...)
	for _, collection := range resources {
		if collection.Versions == nil {
			continue
		}
		for _, constraint := range publishedUniqueConstraints(collection) {
			variants := []schema.LocaleCode{""}
			if constraint.localized {
				variants = locales
			}
			for _, locale := range variants {
				ready, err := uniqueUnionColumnsExist(ctx, transaction, collection, constraint, locale)
				if err != nil {
					return err
				}
				if !ready {
					continue
				}
				statement, err := uniqueUnionAuditStatement(collection, constraint, locale)
				if err != nil {
					return err
				}
				var duplicate int
				err = transaction.QueryRowContext(ctx, statement).Scan(&duplicate)
				if err == nil {
					return fmt.Errorf("unique constraint conflicts across published and working content in %s: %w", collection.Slug, store.ErrConflict)
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
		}
	}
	return nil
}

// uniqueUnionColumnsExist reports whether both document tables already have
// every root column a constraint reads.
func uniqueUnionColumnsExist(ctx context.Context, transaction *sql.Tx, collection schema.Collection, constraint publishedUniqueConstraint, locale schema.LocaleCode) (bool, error) {
	for _, path := range constraint.paths {
		chain := atlasIndexFieldChain(collection.Fields, path.Segments())
		if len(chain) == 0 {
			return false, nil
		}
		column := fieldColumn(chain[0].ID)
		if chain[0].Localized {
			column = localizedFieldColumn(chain[0].ID, locale)
		}
		for _, table := range documentTables(collection, collection.ID) {
			exists, err := transactionColumnExists(ctx, transaction, table, column)
			if err != nil || !exists {
				return false, err
			}
		}
	}
	return true, nil
}

func uniqueUnionAuditStatement(collection schema.Collection, constraint publishedUniqueConstraint, locale schema.LocaleCode) (string, error) {
	compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{locale}}
	keys := make([]string, 0, len(constraint.paths))
	aliases := make([]string, 0, len(constraint.paths))
	present := make([]string, 0, len(constraint.paths))
	for index, path := range constraint.paths {
		column, err := compiler.column(path)
		if err != nil {
			return "", err
		}
		alias := fmt.Sprintf("key_%d", index+1)
		keys = append(keys, column+" AS "+alias)
		aliases = append(aliases, alias)
		present = append(present, alias+" IS NOT NULL")
	}
	return fmt.Sprintf(`WITH occupied AS (
SELECT id, %s FROM %s WHERE deleted_at IS NULL
UNION ALL
SELECT id, %s FROM %s WHERE deleted_at IS NULL
) SELECT 1 FROM occupied WHERE %s GROUP BY %s HAVING count(DISTINCT id) > 1 LIMIT 1`,
		strings.Join(keys, ", "), quote(collectionTable(collection.ID)), strings.Join(keys, ", "), quote(publishedCollectionTable(collection.ID)),
		strings.Join(present, " AND "), strings.Join(aliases, ", ")), nil
}

// migrationConflict reports a unique violation raised by migration DDL, such
// as a new unique index over duplicated values, as a structured conflict.
func migrationConflict(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("%w: %s", store.ErrConflict, postgresError.Message)
	}
	return err
}
