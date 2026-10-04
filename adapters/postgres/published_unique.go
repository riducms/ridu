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
func (transaction *documentTransaction) lockUniqueUnion(ctx context.Context, collection schema.Collection) error {
	if collection.Versions == nil || len(publishedUniqueConstraints(collection)) == 0 {
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
// JSONB equality preserves text/number/boolean distinctions across physical
// working columns and JSON snapshots, including numeric signed zero.
func (transaction *documentTransaction) checkUniqueUnion(ctx context.Context, collection schema.Collection, locales []schema.LocaleCode) error {
	return checkPublishedUniqueUnion(ctx, collection, locales, func(statement string, collectionID string) error {
		var duplicate int
		err := transaction.transaction.QueryRow(ctx, statement, collectionID).Scan(&duplicate)
		if err == nil {
			return store.ErrConflict
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return translateError(err)
		}
		return nil
	})
}

func checkMigrationPublishedUniqueUnion(ctx context.Context, transaction *sql.Tx, manifest schema.Manifest) error {
	available, err := transactionTableExists(ctx, transaction, "ridu_published_documents")
	if err != nil || !available {
		return err
	}
	var locales []schema.LocaleCode
	if localization := manifest.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	resources := append(append([]schema.Collection(nil), manifest.Snapshot().Collections...), manifest.Snapshot().Globals...)
	for _, collection := range resources {
		if err := checkPublishedUniqueUnion(ctx, collection, locales, func(statement string, collectionID string) error {
			var duplicate int
			err := transaction.QueryRowContext(ctx, statement, collectionID).Scan(&duplicate)
			if err == nil {
				return fmt.Errorf("new unique constraint conflicts across published and working heads in %s: %w", collection.Slug, store.ErrConflict)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func checkPublishedUniqueUnion(ctx context.Context, collection schema.Collection, locales []schema.LocaleCode, check func(string, string) error) error {
	if collection.Versions == nil {
		return nil
	}
	table := quote(collectionTable(collection.ID))
	for _, constraint := range publishedUniqueConstraints(collection) {
		variants := []schema.LocaleCode{""}
		if constraint.localized {
			variants = locales
		}
		for _, locale := range variants {
			workingCompiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{locale}}
			liveCompiler := predicateCompiler{collection: collection, snapshot: true, localeChain: []schema.LocaleCode{locale}}
			workingKeys := make([]string, 0, len(constraint.paths))
			liveKeys := make([]string, 0, len(constraint.paths))
			aliases := make([]string, 0, len(constraint.paths))
			present := make([]string, 0, len(constraint.paths))
			for index, path := range constraint.paths {
				working, err := workingCompiler.column(path)
				if err != nil {
					return err
				}
				live, err := liveCompiler.column(path)
				if err != nil {
					return err
				}
				alias := fmt.Sprintf("key_%d", index+1)
				workingKeys = append(workingKeys, "to_jsonb("+working+") AS "+alias)
				liveKeys = append(liveKeys, "to_jsonb("+live+") AS "+alias)
				aliases = append(aliases, alias)
				present = append(present, alias+" IS NOT NULL")
			}
			if len(aliases) == 0 {
				continue
			}
			statement := fmt.Sprintf(`WITH occupied AS (
SELECT id, %s FROM %s WHERE deleted_at IS NULL
UNION ALL
SELECT working.id, %s FROM ridu_published_documents AS head
JOIN %s AS working ON working.id = head.document_id
WHERE head.collection_id = $1 AND working.deleted_at IS NULL
) SELECT 1 FROM occupied WHERE %s GROUP BY %s HAVING count(DISTINCT id) > 1 LIMIT 1`,
				strings.Join(workingKeys, ", "), table, strings.Join(liveKeys, ", "), table,
				strings.Join(present, " AND "), strings.Join(aliases, ", "))
			if err := check(statement, string(collection.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}
