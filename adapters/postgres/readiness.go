package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// readinessCache remembers the database state the last complete readiness
// check verified. A probe repeats only the work whose answer can change:
// connectivity, the migration ledger, incomplete migration work and the
// physical catalog fingerprint. The executable-side work (manifest and
// history digests, the expected physical schema) depends only on the manifest
// instance and expected history, and the physical inspection depends only on
// catalog state, so while all of these are unchanged a full check would repeat
// the verified result.
type readinessCache struct {
	mu       sync.Mutex
	verified *readinessProof
}

type readinessProof struct {
	manifest schema.Manifest
	history  string
	ledger   string
	catalog  string
}

func (cache *readinessCache) matches(proof readinessProof) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	verified := cache.verified
	return verified != nil && verified.manifest.SameInstance(proof.manifest) &&
		verified.history == proof.history && verified.ledger == proof.ledger && verified.catalog == proof.catalog
}

func (cache *readinessCache) record(proof readinessProof) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.verified = &proof
}

func (cache *readinessCache) forget() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.verified = nil
}

// Ready proves connectivity and that the latest complete immutable migration
// targets the exact manifest embedded in this process. In-progress migration work is
// rejected even if an older completed digest happens to match.
func (backend *Store) Ready(ctx context.Context, manifest schema.Manifest) error {
	return backend.ready(ctx, manifest, nil)
}

// ReadyWithMigrationHistory verifies the complete ordered migration history,
// recorded head and executable storage schema. Admin presentation may differ.
func (backend *Store) ReadyWithMigrationHistory(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest string) error {
	return backend.ready(ctx, manifest, &expectedHistoryDigest)
}

func (backend *Store) ready(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest *string) error {
	if err := backend.Ping(ctx); err != nil {
		return err
	}
	var ledgerExists bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL`).Scan(&ledgerExists); err != nil {
		return fmt.Errorf("inspect migration ledger: %w", err)
	}
	if !ledgerExists {
		return fmt.Errorf("immutable migration ledger is missing")
	}
	// Artifact names begin with a fixed-width UTC timestamp and are the immutable
	// history order. Database clocks can move backwards, so applied_at must not
	// decide which manifest is current.
	var actual, plannerName, plannerVersion string
	if err := backend.pool.QueryRow(ctx, `SELECT to_digest, planner_name, planner_version FROM ridu_migrations ORDER BY name DESC LIMIT 1`).Scan(&actual, &plannerName, &plannerVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("immutable migration ledger has no applied migration")
		}
		return fmt.Errorf("read migration ledger: %w", err)
	}
	if plannerName != atlasPlannerName || plannerVersion != AtlasVersion {
		return fmt.Errorf("database migration ledger head uses unsupported PostgreSQL planner %s %q; this Ridu release supports only %s %q, so create a new migration history with ridu migrate create and apply it to a new database", plannerName, plannerVersion, atlasPlannerName, AtlasVersion)
	}
	identities, err := backend.migrationHistory(ctx)
	if err != nil {
		return err
	}
	var stepsExist bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_migration_steps') IS NOT NULL`).Scan(&stepsExist); err != nil {
		return fmt.Errorf("inspect migration step ledger: %w", err)
	}
	if stepsExist {
		var incomplete bool
		if err := backend.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM ridu_migration_steps steps
WHERE NOT EXISTS (SELECT 1 FROM ridu_migrations migrations WHERE migrations.name = steps.artifact_name AND migrations.artifact_digest = steps.artifact_digest)
)`).Scan(&incomplete); err != nil {
			return fmt.Errorf("inspect incomplete migration work: %w", err)
		}
		if incomplete {
			return fmt.Errorf("database has incomplete migration work")
		}
	}
	// The fingerprint is taken before the full inspection: a schema change
	// that races the inspection changes the next probe's fingerprint and
	// forces another full check.
	catalog, err := physicalCatalogFingerprint(ctx, backend.pool)
	if err != nil {
		return fmt.Errorf("fingerprint PostgreSQL physical state: %w", err)
	}
	proof := readinessProof{manifest: manifest, ledger: migrationLedgerKey(actual, identities), catalog: catalog}
	if expectedHistoryDigest != nil {
		proof.history = "history:" + *expectedHistoryDigest
	}
	if backend.readiness.matches(proof) {
		return nil
	}
	backend.readiness.forget()
	if expectedHistoryDigest != nil {
		if err := validatePostgresMigrationHistory(identities, actual, manifest, *expectedHistoryDigest); err != nil {
			return err
		}
	} else {
		expected, err := ridumigration.DigestManifest(manifest)
		if err != nil {
			return fmt.Errorf("digest expected manifest: %w", err)
		}
		if actual != expected {
			return fmt.Errorf("database manifest digest %s does not match executable digest %s", actual, expected)
		}
	}
	database := stdlib.OpenDBFromPool(backend.pool)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("inspect PostgreSQL physical state: %w", err)
	}
	defer connection.Close()
	if err := verifyPostgresPhysicalState(ctx, connection, &manifest); err != nil {
		return fmt.Errorf("database physical state: %w", err)
	}
	backend.readiness.record(proof)
	return nil
}

func (backend *Store) migrationHistory(ctx context.Context) ([]ridumigration.ArtifactIdentity, error) {
	rows, err := backend.pool.Query(ctx, `SELECT name, artifact_digest FROM ridu_migrations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("read ordered PostgreSQL migration history: %w", err)
	}
	defer rows.Close()
	identities := make([]ridumigration.ArtifactIdentity, 0)
	for rows.Next() {
		var identity ridumigration.ArtifactIdentity
		if err := rows.Scan(&identity.Name, &identity.Digest); err != nil {
			return nil, fmt.Errorf("read ordered PostgreSQL migration history: %w", err)
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read ordered PostgreSQL migration history: %w", err)
	}
	return identities, nil
}

func migrationLedgerKey(headDigest string, identities []ridumigration.ArtifactIdentity) string {
	var key strings.Builder
	key.WriteString(headDigest)
	for _, identity := range identities {
		key.WriteString("\x00" + identity.Name + "\x00" + identity.Digest)
	}
	return key.String()
}

func validatePostgresMigrationHistory(identities []ridumigration.ArtifactIdentity, headDigest string, manifest schema.Manifest, expected string) error {
	actual, err := ridumigration.DigestArtifactHistory(identities, headDigest, manifest)
	if err != nil {
		return fmt.Errorf("digest applied PostgreSQL migration history: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("applied PostgreSQL migration history digest %s does not match executable history digest %s", actual, expected)
	}
	return nil
}

// physicalCatalogFingerprint digests every catalog row the physical-state
// verifier reads for the current schema: the schema itself, its relations,
// columns, defaults, constraints, indexes, triggers, comments and types. A
// transactional catalog change writes a new row version with a new xmin, and
// creating or dropping an object adds or removes rows, so any DDL that could
// change the verifier's answer changes the fingerprint. Index build state and
// trigger enablement are digested by value as well, because they can change
// without a new row version. Planner statistics written in place by VACUUM and
// ANALYZE do not affect the verifier and leave the fingerprint unchanged.
func physicalCatalogFingerprint(ctx context.Context, runner interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (string, error) {
	var fingerprint string
	err := runner.QueryRow(ctx, `WITH relation AS (
  SELECT oid, xmin FROM pg_catalog.pg_class WHERE relnamespace = current_schema()::regnamespace
), schema_type AS (
  SELECT oid, xmin FROM pg_catalog.pg_type WHERE typnamespace = current_schema()::regnamespace
)
SELECT md5(concat_ws('|',
  (SELECT oid::text || ':' || xmin::text FROM pg_catalog.pg_namespace WHERE oid = current_schema()::regnamespace),
  (SELECT string_agg(oid::text || ':' || xmin::text, ',' ORDER BY oid) FROM relation),
  (SELECT string_agg(attrelid::text || '.' || attnum::text || ':' || xmin::text, ',' ORDER BY attrelid, attnum)
    FROM pg_catalog.pg_attribute WHERE attrelid IN (SELECT oid FROM relation)),
  (SELECT string_agg(oid::text || ':' || xmin::text, ',' ORDER BY oid)
    FROM pg_catalog.pg_attrdef WHERE adrelid IN (SELECT oid FROM relation)),
  (SELECT string_agg(oid::text || ':' || xmin::text, ',' ORDER BY oid)
    FROM pg_catalog.pg_constraint WHERE conrelid IN (SELECT oid FROM relation) OR connamespace = current_schema()::regnamespace),
  (SELECT string_agg(indexrelid::text || ':' || xmin::text || ':' || indisvalid::text || indisready::text || indislive::text, ',' ORDER BY indexrelid)
    FROM pg_catalog.pg_index WHERE indrelid IN (SELECT oid FROM relation)),
  (SELECT string_agg(oid::text || ':' || xmin::text || ':' || tgenabled::text, ',' ORDER BY oid)
    FROM pg_catalog.pg_trigger WHERE tgrelid IN (SELECT oid FROM relation)),
  (SELECT string_agg(objoid::text || '.' || objsubid::text || ':' || xmin::text, ',' ORDER BY objoid, objsubid)
    FROM pg_catalog.pg_description WHERE classoid = 'pg_catalog.pg_class'::regclass AND objoid IN (SELECT oid FROM relation)),
  (SELECT string_agg(oid::text || ':' || xmin::text, ',' ORDER BY oid) FROM schema_type),
  (SELECT string_agg(oid::text || ':' || xmin::text, ',' ORDER BY oid)
    FROM pg_catalog.pg_enum WHERE enumtypid IN (SELECT oid FROM schema_type))
))`).Scan(&fingerprint)
	return fingerprint, err
}
