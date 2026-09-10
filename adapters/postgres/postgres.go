// Package postgres provides Ridu's first official document store.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riducms/ridu/internal/localization"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/referenceindex"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type Store struct {
	pool           *pgxpool.Pool
	uploadLockPool *pgxpool.Pool
	uploadLockWait time.Duration
}

// PoolConfig defines bounded production connection and PostgreSQL session
// defaults. Zero values select Ridu's defaults; negative duration values
// explicitly disable the corresponding timeout.
type PoolConfig struct {
	DatabaseURL string
	// AllowInsecureTransport explicitly permits plaintext PostgreSQL connections.
	// Keep this false in production; it exists for local Unix sockets and
	// development databases whose transport is secured outside PostgreSQL.
	AllowInsecureTransport bool
	ApplicationName        string
	MaxConnections         int32
	// MaxUploadLockConnections bounds the separate advisory-lock pool used
	// while object storage and document transactions are coordinated. Keeping
	// this pool separate prevents staged uploads from starving their own
	// document transactions. Zero selects four connections.
	MaxUploadLockConnections        int32
	MinConnections                  int32
	MaxConnectionLifetime           time.Duration
	MaxConnectionLifetimeJitter     time.Duration
	MaxConnectionIdleTime           time.Duration
	HealthCheckPeriod               time.Duration
	ConnectTimeout                  time.Duration
	StatementTimeout                time.Duration
	LockTimeout                     time.Duration
	IdleInTransactionSessionTimeout time.Duration
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	return OpenWithConfig(ctx, PoolConfig{DatabaseURL: databaseURL})
}

// OpenWithConfig opens the official PostgreSQL store with explicit pool and
// server-side resource bounds.
func OpenWithConfig(ctx context.Context, options PoolConfig) (*Store, error) {
	configured, err := normalizedPoolConfig(options)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL pool: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, configured)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL pool: %w", err)
	}
	pingContext := ctx
	cancel := func() {}
	if configured.ConnConfig.ConnectTimeout > 0 {
		pingContext, cancel = context.WithTimeout(ctx, configured.ConnConfig.ConnectTimeout)
	}
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	uploadLockConfigured := configured.Copy()
	uploadLockConfigured.MinConns = 0
	uploadLockConfigured.MaxConns = options.MaxUploadLockConnections
	if uploadLockConfigured.MaxConns == 0 {
		uploadLockConfigured.MaxConns = 4
	}
	uploadLockConfigured.ConnConfig.RuntimeParams["application_name"] = "ridu-upload-locks"
	uploadLockPool, err := pgxpool.NewWithConfig(ctx, uploadLockConfigured)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("configure PostgreSQL upload-lock pool: %w", err)
	}
	if err := uploadLockPool.Ping(pingContext); err != nil {
		uploadLockPool.Close()
		pool.Close()
		return nil, fmt.Errorf("connect PostgreSQL upload-lock pool: %w", err)
	}
	return &Store{
		pool: pool, uploadLockPool: uploadLockPool,
		uploadLockWait: durationDefault(options.LockTimeout, 10*time.Second),
	}, nil
}

func normalizedPoolConfig(options PoolConfig) (*pgxpool.Config, error) {
	if strings.TrimSpace(options.DatabaseURL) == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	if options.MaxConnections < 0 || options.MaxUploadLockConnections < 0 || options.MinConnections < 0 {
		return nil, fmt.Errorf("connection counts cannot be negative")
	}
	if options.MaxConnections == 0 {
		options.MaxConnections = 10
	}
	if options.MinConnections > options.MaxConnections {
		return nil, fmt.Errorf("minimum connections %d exceed maximum %d", options.MinConnections, options.MaxConnections)
	}
	configured, err := pgxpool.ParseConfig(options.DatabaseURL)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	if !options.AllowInsecureTransport && !securePostgresTransport(configured) {
		return nil, fmt.Errorf("PostgreSQL transport must require TLS; configure sslmode=require or stronger, or explicitly allow insecure transport for local development")
	}
	configured.MaxConns = options.MaxConnections
	configured.MinConns = options.MinConnections
	configured.MaxConnLifetime = durationDefault(options.MaxConnectionLifetime, time.Hour)
	configured.MaxConnLifetimeJitter = durationDefault(options.MaxConnectionLifetimeJitter, 5*time.Minute)
	configured.MaxConnIdleTime = durationDefault(options.MaxConnectionIdleTime, 30*time.Minute)
	configured.HealthCheckPeriod = durationDefault(options.HealthCheckPeriod, time.Minute)
	configured.ConnConfig.ConnectTimeout = durationDefault(options.ConnectTimeout, 10*time.Second)
	applicationName := strings.TrimSpace(options.ApplicationName)
	if applicationName == "" {
		applicationName = "ridu"
	}
	if strings.ContainsRune(applicationName, '\x00') || len(applicationName) > 64 {
		return nil, fmt.Errorf("application name must contain at most 64 non-NUL bytes")
	}
	configured.ConnConfig.RuntimeParams["application_name"] = applicationName
	for name, setting := range map[string]struct {
		value    time.Duration
		fallback time.Duration
	}{
		"statement_timeout":                   {options.StatementTimeout, 60 * time.Second},
		"lock_timeout":                        {options.LockTimeout, 10 * time.Second},
		"idle_in_transaction_session_timeout": {options.IdleInTransactionSessionTimeout, 60 * time.Second},
	} {
		value := durationDefault(setting.value, setting.fallback)
		if value%time.Millisecond != 0 {
			return nil, fmt.Errorf("%s must use whole milliseconds", name)
		}
		configured.ConnConfig.RuntimeParams[name] = strconv.FormatInt(value.Milliseconds(), 10)
	}
	return configured, nil
}

func securePostgresTransport(configured *pgxpool.Config) bool {
	if configured == nil || configured.ConnConfig == nil || configured.ConnConfig.TLSConfig == nil {
		return false
	}
	for _, fallback := range configured.ConnConfig.Fallbacks {
		if fallback == nil || fallback.TLSConfig == nil {
			return false
		}
	}
	return true
}

func durationDefault(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	if value < 0 {
		return 0
	}
	return value
}

func (backend *Store) Close() {
	if backend.uploadLockPool != nil {
		backend.uploadLockPool.Close()
	}
	if backend.pool != nil {
		backend.pool.Close()
	}
}
func (backend *Store) Ping(ctx context.Context) error {
	if backend.pool == nil {
		return fmt.Errorf("PostgreSQL document pool is unavailable")
	}
	if err := backend.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL document pool: %w", err)
	}
	if backend.uploadLockPool == nil {
		return fmt.Errorf("PostgreSQL upload-lock pool is unavailable")
	}
	if err := backend.uploadLockPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL upload-lock pool: %w", err)
	}
	return nil
}

// Ready proves connectivity and that the latest complete immutable migration
// targets the exact manifest embedded in this process. In-progress migration work is
// rejected even if an older completed digest happens to match.
func (backend *Store) Ready(ctx context.Context, manifest schema.Manifest) error {
	return backend.ready(ctx, manifest, nil)
}

// ReadyWithMigrationHistory proves ordinary readiness and exact agreement
// with the complete ordered migration history embedded in the executable.
func (backend *Store) ReadyWithMigrationHistory(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest string) error {
	return backend.ready(ctx, manifest, &expectedHistoryDigest)
}

func (backend *Store) ready(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest *string) error {
	if err := backend.Ping(ctx); err != nil {
		return err
	}
	expected, err := ridumigration.DigestManifest(manifest)
	if err != nil {
		return fmt.Errorf("digest expected manifest: %w", err)
	}
	var ledgerExists bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_migrations') IS NOT NULL`).Scan(&ledgerExists); err != nil {
		return fmt.Errorf("inspect migration ledger: %w", err)
	}
	if !ledgerExists {
		return fmt.Errorf("immutable migration ledger is missing")
	}
	var actual, plannerVersion string
	// Artifact names begin with a fixed-width UTC timestamp and are the immutable
	// history order. Database clocks can move backwards, so applied_at must not
	// decide which manifest is current.
	if err := backend.pool.QueryRow(ctx, `SELECT to_digest, planner_version FROM ridu_migrations ORDER BY name DESC LIMIT 1`).Scan(&actual, &plannerVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("immutable migration ledger has no applied migration")
		}
		return fmt.Errorf("read migration ledger: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("database manifest digest %s does not match executable digest %s", actual, expected)
	}
	if expectedHistoryDigest != nil {
		if err := backend.validateMigrationHistory(ctx, *expectedHistoryDigest); err != nil {
			return err
		}
	}
	if plannerVersion == atlasVersionV1 && len(ridumigration.AuthIdentityResources(manifest.Snapshot())) != 0 {
		return fmt.Errorf("database authentication identities require the PostgreSQL planner %s canonicalization artifact", AtlasVersion)
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
	database := stdlib.OpenDBFromPool(backend.pool)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("inspect PostgreSQL physical state: %w", err)
	}
	defer connection.Close()
	if err := verifyPostgresPhysicalState(ctx, connection, &manifest, postgresPlannerTargetContract(plannerVersion)); err != nil {
		return fmt.Errorf("database physical state: %w", err)
	}
	return nil
}

func (backend *Store) validateMigrationHistory(ctx context.Context, expected string) error {
	rows, err := backend.pool.Query(ctx, `SELECT name, artifact_digest FROM ridu_migrations ORDER BY name`)
	if err != nil {
		return fmt.Errorf("read ordered PostgreSQL migration history: %w", err)
	}
	defer rows.Close()
	identities := make([]ridumigration.ArtifactIdentity, 0)
	for rows.Next() {
		var identity ridumigration.ArtifactIdentity
		if err := rows.Scan(&identity.Name, &identity.Digest); err != nil {
			return fmt.Errorf("read ordered PostgreSQL migration history: %w", err)
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read ordered PostgreSQL migration history: %w", err)
	}
	return validatePostgresMigrationHistory(identities, expected)
}

func validatePostgresMigrationHistory(identities []ridumigration.ArtifactIdentity, expected string) error {
	actual, err := ridumigration.DigestArtifactHistory(identities)
	if err != nil {
		return fmt.Errorf("digest applied PostgreSQL migration history: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("applied PostgreSQL migration history digest %s does not match executable history digest %s", actual, expected)
	}
	return nil
}

func (backend *Store) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	return &documentTransaction{transaction: transaction, tableExists: make(map[string]bool)}, nil
}

func (backend *Store) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, err
	}
	return &documentTransaction{transaction: transaction, tableExists: make(map[string]bool)}, nil
}

type documentTransaction struct {
	transaction pgx.Tx
	tableExists map[string]bool
}

func lockDocumentReferences(ctx context.Context, transaction pgx.Tx, references ...store.DocumentReference) error {
	unique := make(map[string]store.DocumentReference, len(references))
	for _, reference := range references {
		if reference.CollectionID == "" || reference.DocumentID == "" {
			return store.ErrNotFound
		}
		unique[string(reference.CollectionID)+"\x00"+reference.DocumentID] = reference
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		reference := unique[key]
		statement := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = $1 AND %s IS NULL FOR KEY SHARE", quote(collectionTable(reference.CollectionID)), quote("id"), quote("deleted_at"))
		var marker int
		if err := transaction.QueryRow(ctx, statement, reference.DocumentID).Scan(&marker); err != nil {
			return translateError(err)
		}
	}
	return nil
}

func (transaction *documentTransaction) CreateAuthCredential(ctx context.Context, collection schema.Collection, userID string, hash []byte, initiallyVerified bool) error {
	_, err := transaction.transaction.Exec(ctx, `INSERT INTO ridu_auth_credentials (
  collection_id, user_id, password_hash, failed_login_attempts, locked_until, verified
) VALUES ($1, $2, $3, 0, NULL, $4)`, string(collection.ID), userID, hash, initiallyVerified)
	return translateError(err)
}

func (transaction *documentTransaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	id := request.ID
	if id == "" {
		var err error
		id, err = newID(string(request.Collection.ID))
		if err != nil {
			return store.Document{}, err
		}
	}
	if err := store.ValidateDocumentID(id); err != nil {
		return store.Document{}, err
	}
	fields := storedSchemaFields(request.Collection.Fields)
	values := store.CloneValues(request.Values)
	canonicalizePostgresAuthIdentity(request.Collection, values)
	columns := []string{quote("id")}
	placeholders := []string{"$1"}
	arguments := []any{id}
	if !request.CreatedAt.IsZero() {
		columns = append(columns, quote("created_at"))
		arguments = append(arguments, request.CreatedAt)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)))
	}
	if !request.UpdatedAt.IsZero() {
		columns = append(columns, quote("updated_at"))
		arguments = append(arguments, request.UpdatedAt)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)))
	}
	if request.Collection.Versions != nil {
		status := request.Status
		if status == "" {
			status = store.StatusDraft
			if !request.Collection.Versions.Drafts {
				status = store.StatusPublished
			}
		}
		columns = append(columns, quote("_status"), quote("_revision"))
		arguments = append(arguments, status, 1)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)-1), fmt.Sprintf("$%d", len(arguments)))
	}
	for _, field := range fields {
		value, exists := values[field.Name]
		if !exists {
			continue
		}
		if field.Localized {
			localized := value
			if localized.Kind() != store.ValueObject {
				return store.Document{}, fmt.Errorf("localized field %q requires locale-keyed storage", field.Path.String())
			}
			for _, locale := range request.Locales {
				localizedValue, exists := localized.Lookup(string(locale))
				if !exists {
					continue
				}
				encoded, err := databaseValue(field, localizedValue)
				if err != nil {
					return store.Document{}, err
				}
				columns = append(columns, quote(localizedFieldColumn(field.ID, locale)))
				arguments = append(arguments, encoded)
				placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)))
			}
			continue
		}
		encoded, err := databaseValue(field, value)
		if err != nil {
			return store.Document{}, err
		}
		columns = append(columns, quote(fieldColumn(field.ID)))
		arguments = append(arguments, encoded)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)))
	}
	statement := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
		quote(collectionTable(request.Collection.ID)), strings.Join(columns, ", "), strings.Join(placeholders, ", "), selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	if err := transaction.replaceDocumentReferences(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func (transaction *documentTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	predicate, arguments, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	fields := fieldsForRead(request)
	statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s", selectColumns(request.Collection, fields, request.Locales), quote(collectionTable(request.Collection.ID)), predicate)
	switch request.Lock {
	case store.LockNone:
	case store.LockReference:
		statement += " FOR SHARE"
	case store.LockMutation:
		statement += " FOR UPDATE"
	default:
		return store.Document{}, fmt.Errorf("unsupported document lock mode %q", request.Lock)
	}
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	documents, err := transaction.populate(ctx, []store.Document{document}, request)
	if err != nil {
		return store.Document{}, err
	}
	return projectDocument(documents[0], request.Select), nil
}

func (transaction *documentTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	predicate, arguments, err := requestPredicate(request, false)
	if err != nil {
		return store.Page{}, err
	}
	var total int
	countStatement := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", quote(collectionTable(request.Collection.ID)), predicate)
	if err := transaction.transaction.QueryRow(ctx, countStatement, arguments...).Scan(&total); err != nil {
		return store.Page{}, translateError(err)
	}
	page, limit, offset, _ := store.ListPageBounds(request.Page, request.Limit, total)
	fields := fieldsForRead(request)
	order, err := sortClause(request)
	if err != nil {
		return store.Page{}, err
	}
	arguments = append(arguments, limit, offset)
	statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d",
		selectColumns(request.Collection, fields, request.Locales), quote(collectionTable(request.Collection.ID)), predicate, order, len(arguments)-1, len(arguments))
	rows, err := transaction.transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return store.Page{}, translateError(err)
	}
	defer rows.Close()
	var documents []store.Document
	for rows.Next() {
		document, err := scanDocument(rows, request.Collection, fields)
		if err != nil {
			return store.Page{}, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return store.Page{}, err
	}
	documents, err = transaction.populate(ctx, documents, request)
	if err != nil {
		return store.Page{}, err
	}
	for index := range documents {
		documents[index] = projectDocument(documents[index], request.Select)
	}
	return store.Page{Documents: documents, Page: page, Limit: limit, Total: total}, nil
}

func (transaction *documentTransaction) Distinct(ctx context.Context, request store.DistinctRequest) (store.DistinctPage, error) {
	if err := store.ValidateDistinctRequest(request); err != nil {
		return store.DistinctPage{}, err
	}
	documentRequest := store.Request{
		Collection: request.Collection, Filter: request.Filter, Access: request.Access,
		PublishedOnly: request.PublishedOnly, Deletion: request.Deletion,
		Locales: request.Locales, LocaleChain: request.LocaleChain,
	}
	predicate, arguments, err := requestPredicate(documentRequest, false)
	if err != nil {
		return store.DistinctPage{}, err
	}
	compiler := predicateCompiler{collection: request.Collection, localeChain: request.LocaleChain}
	column, err := compiler.column(request.Field)
	if err != nil {
		return store.DistinctPage{}, err
	}
	resolved, err := resolvePredicatePath(request.Collection, request.Field)
	if err != nil {
		return store.DistinctPage{}, err
	}
	if scalarFieldValueKind(resolved.leaf) == query.ValueString {
		column = "(" + column + ` COLLATE "C")`
	}
	distinctQuery := fmt.Sprintf("SELECT DISTINCT %s AS distinct_value FROM %s WHERE %s", column, quote(collectionTable(request.Collection.ID)), predicate)
	var total int
	if err := transaction.transaction.QueryRow(ctx, "SELECT count(*) FROM ("+distinctQuery+") AS distinct_values", arguments...).Scan(&total); err != nil {
		return store.DistinctPage{}, translateError(err)
	}
	page, limit, offset, _ := store.ListPageBounds(request.Page, request.Limit, total)
	arguments = append(arguments, limit, offset)
	statement := fmt.Sprintf("%s ORDER BY distinct_value ASC NULLS FIRST LIMIT $%d OFFSET $%d", distinctQuery, len(arguments)-1, len(arguments))
	rows, err := transaction.transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return store.DistinctPage{}, translateError(err)
	}
	defer rows.Close()
	values := make([]store.Value, 0, min(limit, total))
	for rows.Next() {
		switch resolved.leaf.Type {
		case schema.FieldTypeNumber:
			var raw *float64
			if err := rows.Scan(&raw); err != nil {
				return store.DistinctPage{}, translateError(err)
			}
			if raw == nil {
				values = append(values, store.Null())
			} else {
				values = append(values, store.Number(*raw))
			}
		case schema.FieldTypeCheckbox:
			var raw *bool
			if err := rows.Scan(&raw); err != nil {
				return store.DistinctPage{}, translateError(err)
			}
			if raw == nil {
				values = append(values, store.Null())
			} else {
				values = append(values, store.Boolean(*raw))
			}
		default:
			var raw *string
			if err := rows.Scan(&raw); err != nil {
				return store.DistinctPage{}, translateError(err)
			}
			if raw == nil {
				values = append(values, store.Null())
			} else {
				values = append(values, store.String(*raw))
			}
		}
	}
	if err := rows.Err(); err != nil {
		return store.DistinctPage{}, translateError(err)
	}
	return store.DistinctPage{Values: values, Page: page, Limit: limit, Total: total}, nil
}

func (transaction *documentTransaction) ListWindow(ctx context.Context, request store.Request) (store.Window, error) {
	statement, arguments, fields, err := listWindowQuery(request)
	if err != nil {
		return store.Window{}, err
	}
	rows, err := transaction.transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return store.Window{}, translateError(err)
	}
	defer rows.Close()
	documents := make([]store.Document, 0, request.Limit+1)
	for rows.Next() {
		document, scanError := scanDocument(rows, request.Collection, fields)
		if scanError != nil {
			return store.Window{}, scanError
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return store.Window{}, err
	}
	hasMore := len(documents) > request.Limit
	if hasMore {
		documents = documents[:request.Limit]
	}
	documents, err = transaction.populate(ctx, documents, request)
	if err != nil {
		return store.Window{}, err
	}
	for index := range documents {
		documents[index] = projectDocument(documents[index], request.Select)
	}
	return store.Window{Documents: documents, HasMore: hasMore}, nil
}

func listWindowQuery(request store.Request) (string, []any, []schema.Field, error) {
	if err := store.ValidateListWindowRequest(request); err != nil {
		return "", nil, nil, err
	}
	predicate, arguments, err := requestPredicate(request, false)
	if err != nil {
		return "", nil, nil, err
	}
	compiler := predicateCompiler{collection: request.Collection, localeChain: request.LocaleChain}
	column, err := compiler.column(request.IndexWindow.Path)
	if err != nil {
		return "", nil, nil, err
	}
	lowerParameter := len(arguments) + 1
	upperParameter := lowerParameter + 1
	limitParameter := upperParameter + 1
	predicate = fmt.Sprintf("(%s) AND %s >= $%d AND %s < $%d", predicate, column, lowerParameter, column, upperParameter)
	arguments = append(arguments, request.IndexWindow.LowerBound, request.IndexWindow.UpperBound, request.Limit+1)
	fields := fieldsForRead(request)
	statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s ASC LIMIT $%d",
		selectColumns(request.Collection, fields, request.Locales), quote(collectionTable(request.Collection.ID)), predicate, column, limitParameter)
	return statement, arguments, fields, nil
}

func (transaction *documentTransaction) ResolveFilteredSelection(ctx context.Context, request store.FilteredSelectionRequest) (store.FilteredSelection, error) {
	if request.Limit < 1 {
		return store.FilteredSelection{}, fmt.Errorf("filtered selection limit must be positive")
	}
	predicate, arguments, err := requestPredicate(store.Request{
		Collection: request.Collection,
		Filter:     request.Filter,
		Access:     request.Access,
		Deletion:   request.Deletion,
		Locales:    request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
	}, false)
	if err != nil {
		return store.FilteredSelection{}, err
	}
	arguments = append(arguments, request.Limit+1)
	statement := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY %s ASC LIMIT $%d",
		quote("id"), quote(collectionTable(request.Collection.ID)), predicate, quote("id"), len(arguments),
	)
	rows, err := transaction.transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return store.FilteredSelection{}, translateError(err)
	}
	defer rows.Close()
	ids := make([]string, 0, request.Limit+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return store.FilteredSelection{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return store.FilteredSelection{}, err
	}
	overflow := len(ids) > request.Limit
	sort.Strings(ids)
	if overflow {
		ids = ids[:request.Limit]
	}
	return store.FilteredSelection{IDs: ids, Overflow: overflow}, nil
}

func fieldsForRead(request store.Request) []schema.Field {
	if request.Select == nil {
		return storedSchemaFields(request.Collection.Fields)
	}
	wanted := make(map[string]bool, len(request.Select)+len(request.Populate))
	for _, path := range request.Select {
		segments := path.Segments()
		if len(segments) != 0 {
			wanted[segments[0]] = true
		}
	}
	for _, population := range request.Populate {
		segments := population.Path.Segments()
		if len(segments) != 0 {
			wanted[segments[0]] = true
		}
	}
	var result []schema.Field
	for _, field := range request.Collection.Fields {
		if field.Category != schema.FieldCategoryPresentation && wanted[field.Name] {
			result = append(result, field)
		}
	}
	return result
}

func sortClause(request store.Request) (string, error) {
	terms := append([]query.Sort(nil), request.Sort...)
	hasID := false
	for _, term := range terms {
		hasID = hasID || term.Path.String() == "id"
	}
	if !hasID {
		id, _ := query.NewPath("id")
		terms = append(terms, query.Sort{Path: id, Direction: query.Ascending})
	}
	compiler := predicateCompiler{collection: request.Collection, localeChain: request.LocaleChain}
	parts := make([]string, len(terms))
	for index, term := range terms {
		column, err := compiler.column(term.Path)
		if err != nil {
			return "", err
		}
		direction := "ASC"
		if term.Direction == query.Descending {
			direction = "DESC"
		}
		parts[index] = column + " " + direction
	}
	return strings.Join(parts, ", "), nil
}

func (transaction *documentTransaction) populate(ctx context.Context, documents []store.Document, request store.Request) ([]store.Document, error) {
	if len(request.Populate) == 0 {
		return documents, nil
	}
	populationBudget := request.PopulationBudget
	if populationBudget == nil {
		populationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
		request.PopulationBudget = populationBudget
	}
	for _, population := range request.Populate {
		field, found := populationwalk.FieldAtPath(request.Collection.Fields, population.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			return nil, fmt.Errorf("population field %q is not a relationship", population.Path.String())
		}
		populated := make(map[schema.StableID]map[string]store.Document)
		for _, relationshipTarget := range relationshipTargets(relationship) {
			target, exists := request.Collections[relationshipTarget.CollectionID]
			if !exists {
				return nil, fmt.Errorf("relationship target %q is unavailable", relationshipTarget.CollectionID)
			}
			ids := collectRelationshipIDs(documents, request.Collection.Fields, population.Path, relationship, relationshipTarget.CollectionSlug, request)
			if len(ids) == 0 {
				continue
			}
			targetFields := storedSchemaFields(target.Fields)
			if population.Select != nil && population.Depth <= 1 {
				targetFields = fieldsForRead(store.Request{Collection: target, Select: population.Select})
			}
			predicate := fmt.Sprintf("%s = ANY($1) AND %s IS NULL", quote("id"), quote("deleted_at"))
			arguments := []any{ids}
			if request.PublishedOnly && target.Versions != nil {
				predicate += " AND " + quote("_status") + " = 'published'"
			}
			if access := request.PopulationAccess[target.ID]; access != nil {
				compiler := predicateCompiler{collection: target, next: 1, arguments: arguments, localeChain: request.LocaleChain}
				compiled, compileError := compileAccessPredicate(&compiler, *access, request.AllLocales, request.Locales)
				if compileError != nil {
					return nil, compileError
				}
				predicate += " AND (" + compiled + ")"
				arguments = compiler.arguments
			}
			statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s", selectColumns(target, targetFields, request.Locales), quote(collectionTable(target.ID)), predicate)
			rows, err := transaction.transaction.Query(ctx, statement, arguments...)
			if err != nil {
				return nil, translateError(err)
			}
			byID := make(map[string]store.Document, len(ids))
			var targetDocuments []store.Document
			for rows.Next() {
				document, err := scanDocument(rows, target, targetFields)
				if err != nil {
					rows.Close()
					return nil, err
				}
				targetDocuments = append(targetDocuments, document)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
			if population.Depth > 1 {
				targetDocuments, err = transaction.populate(ctx, targetDocuments, store.Request{
					Collection: target, Collections: request.Collections, Populate: populationwalk.DepthPopulations(target, population.Depth-1), PopulationAccess: request.PopulationAccess,
					PopulationBudget: populationBudget, PublishedOnly: request.PublishedOnly, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
				})
				if err != nil {
					return nil, err
				}
			}
			for _, document := range targetDocuments {
				selection := localization.Selection{Configured: request.Locales, Chain: request.LocaleChain, All: request.AllLocales}
				if len(selection.Chain) != 0 {
					selection.Locale = selection.Chain[0]
				}
				document = localization.ProjectDocument(document, target.Fields, selection)
				byID[document.ID] = projectDocument(document, population.Select)
			}
			populated[relationshipTarget.CollectionID] = byID
		}
		var populationError error
		for index := range documents {
			mapped, _ := populationwalk.MapAtPath(request.Collection.Fields, documents[index].Values, population.Path, populationwalk.LocaleSelection{
				All: request.AllLocales, Chain: request.LocaleChain,
			}, func(_ schema.Field, candidate store.Value) store.Value {
				if populationError != nil {
					return candidate
				}
				return populateRelationshipValue(candidate, relationship, populated, func(document store.Document) bool {
					populationError = populationBudget.ConsumeDocument(document)
					return populationError == nil
				})
			})
			if populationError != nil {
				return nil, populationError
			}
			documents[index].Values = mapped
		}
	}
	return documents, nil
}

func relationshipTargets(relationship *schema.RelationshipField) []schema.RelationshipTarget {
	if relationship.Polymorphic {
		return append([]schema.RelationshipTarget(nil), relationship.Targets...)
	}
	return []schema.RelationshipTarget{{CollectionID: relationship.CollectionID, CollectionSlug: relationship.CollectionSlug}}
}

func collectRelationshipIDs(documents []store.Document, fields []schema.Field, path query.Path, relationship *schema.RelationshipField, slug schema.CollectionSlug, request store.Request) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, document := range documents {
		populationwalk.VisitAtPath(fields, document.Values, path, populationwalk.LocaleSelection{
			All: request.AllLocales, Chain: request.LocaleChain,
		}, func(_ schema.Field, value store.Value) {
			values := []store.Value{value}
			if relationship.HasMany {
				values, _ = value.CopyList()
			}
			for _, reference := range values {
				id := ""
				if relationship.Polymorphic {
					if reference.Kind() != store.ValueObject {
						continue
					}
					relationTo, _ := reference.Get("relationTo").StringValue()
					if relationTo != string(slug) {
						continue
					}
					id, _ = reference.Get("id").StringValue()
				} else {
					id, _ = reference.StringValue()
				}
				if id != "" && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		})
	}
	sort.Strings(ids)
	return ids
}

func populateRelationshipValue(value store.Value, relationship *schema.RelationshipField, populated map[schema.StableID]map[string]store.Document, consume func(store.Document) bool) store.Value {
	populate := func(reference store.Value) store.Value {
		if !relationship.Polymorphic {
			id, valid := reference.StringValue()
			if !valid {
				return reference
			}
			if document, found := populated[relationship.CollectionID][id]; found && consume(document) {
				return store.Populated(document)
			}
			return reference
		}
		object, valid := reference.CopyObject()
		if !valid {
			return reference
		}
		slug, _ := object["relationTo"].StringValue()
		id, _ := object["id"].StringValue()
		for _, target := range relationship.Targets {
			if string(target.CollectionSlug) == slug {
				if document, found := populated[target.CollectionID][id]; found && consume(document) {
					object["id"] = store.Populated(document)
				}
				break
			}
		}
		return store.Object(object)
	}
	if !relationship.HasMany {
		return populate(value)
	}
	items, valid := value.CopyList()
	if !valid {
		return value
	}
	for index, item := range items {
		items[index] = populate(item)
	}
	return store.List(items...)
}

func projectDocument(document store.Document, selection []query.Path) store.Document {
	if selection == nil {
		return document
	}
	values := make(store.Values)
	for _, path := range selection {
		segments := path.Segments()
		if len(segments) == 1 {
			if value, exists := document.Values[segments[0]]; exists {
				values[segments[0]] = value
			}
		}
	}
	document.Values = values
	return document
}

func (transaction *documentTransaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	assignments := []string{fmt.Sprintf("%s = now()", quote("updated_at"))}
	values := store.CloneValues(request.Values)
	canonicalizePostgresAuthIdentity(request.Collection, values)
	arguments := make([]any, 0, len(values)+1)
	fields := storedSchemaFields(request.Collection.Fields)
	for _, field := range fields {
		value, exists := values[field.Name]
		if !exists && !request.ReplaceValues {
			continue
		}
		if field.Localized {
			localized := value
			if exists && localized.Kind() != store.ValueObject {
				return store.Document{}, fmt.Errorf("localized field %q requires locale-keyed storage", field.Path.String())
			}
			if !exists && !request.ReplaceValues {
				return store.Document{}, fmt.Errorf("localized field %q requires locale-keyed storage", field.Path.String())
			}
			for _, locale := range request.Locales {
				localizedValue, exists := localized.Lookup(string(locale))
				if !exists && !request.ReplaceValues {
					continue
				}
				if !exists {
					localizedValue = store.Null()
				}
				encoded, err := databaseValue(field, localizedValue)
				if err != nil {
					return store.Document{}, err
				}
				arguments = append(arguments, encoded)
				assignments = append(assignments, fmt.Sprintf("%s = $%d", quote(localizedFieldColumn(field.ID, locale)), len(arguments)))
			}
			continue
		}
		if !exists {
			value = store.Null()
		}
		encoded, err := databaseValue(field, value)
		if err != nil {
			return store.Document{}, err
		}
		arguments = append(arguments, encoded)
		assignments = append(assignments, fmt.Sprintf("%s = $%d", quote(fieldColumn(field.ID)), len(arguments)))
	}
	if request.Collection.Versions != nil {
		assignments = append(assignments, quote("_revision")+" = "+quote("_revision")+" + 1")
		if request.Status != nil {
			arguments = append(arguments, *request.Status)
			assignments = append(assignments, fmt.Sprintf("%s = $%d", quote("_status"), len(arguments)))
		}
	}
	predicate, predicateArguments, err := requestPredicateFrom(request.Request, true, len(arguments))
	if err != nil {
		return store.Document{}, err
	}
	arguments = append(arguments, predicateArguments...)
	statement := fmt.Sprintf("UPDATE %s SET %s WHERE %s RETURNING %s", quote(collectionTable(request.Collection.ID)), strings.Join(assignments, ", "), predicate, selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields)
	if errors.Is(err, pgx.ErrNoRows) && request.ExpectedRevision > 0 {
		probe := request.Request
		probe.ExpectedRevision = 0
		visiblePredicate, visibleArguments, predicateError := requestPredicate(probe, true)
		if predicateError != nil {
			return store.Document{}, predicateError
		}
		var visible bool
		probeStatement := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s)", quote(collectionTable(request.Collection.ID)), visiblePredicate)
		if probeError := transaction.transaction.QueryRow(ctx, probeStatement, visibleArguments...).Scan(&visible); probeError != nil {
			return store.Document{}, translateError(probeError)
		}
		if visible {
			return store.Document{}, store.ErrConflict
		}
		return store.Document{}, store.ErrNotFound
	}
	if err != nil {
		return store.Document{}, translateError(err)
	}
	if err := transaction.replaceDocumentReferences(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func canonicalizePostgresAuthIdentity(collection schema.Collection, values store.Values) {
	if collection.Auth == nil || values == nil {
		return
	}
	identity, exists := values[collection.Auth.IdentityField]
	if !exists {
		return
	}
	text, valid := identity.StringValue()
	if valid {
		values[collection.Auth.IdentityField] = store.String(store.CanonicalAuthIdentity(text))
	}
}

func (transaction *documentTransaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	predicate, arguments, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	fields := storedSchemaFields(request.Collection.Fields)
	statement := fmt.Sprintf("DELETE FROM %s WHERE %s RETURNING %s", quote(collectionTable(request.Collection.ID)), predicate, selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	if err := transaction.deleteDocumentReferences(ctx, store.DocumentReference{CollectionID: request.Collection.ID, DocumentID: request.ID}); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

type referenceDeleteMatch struct {
	entry      referenceindex.Entry
	field      schema.Field
	root       schema.Field
	collection schema.Collection
}

func (transaction *documentTransaction) ApplyReferenceDelete(ctx context.Context, request store.ReferenceDeleteRequest) error {
	if request.Target.CollectionID == "" || request.Target.DocumentID == "" {
		return fmt.Errorf("reference delete requires a target collection and document ID")
	}
	ignored := make(map[store.DocumentReference]struct{}, len(request.IgnoreOwners))
	for _, owner := range request.IgnoreOwners {
		if owner == request.Target {
			ignored[owner] = struct{}{}
			continue
		}
		if _, exists := request.Collections[owner.CollectionID]; !exists {
			return fmt.Errorf("ignored reference owner collection %q is unavailable", owner.CollectionID)
		}
		var stillExists bool
		statement := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s = $1)", quote(collectionTable(owner.CollectionID)), quote("id"))
		if err := transaction.transaction.QueryRow(ctx, statement, owner.DocumentID).Scan(&stillExists); err != nil {
			return translateError(err)
		}
		if !stillExists {
			ignored[owner] = struct{}{}
		}
	}
	rows, err := transaction.transaction.Query(ctx, `SELECT
  owner_collection_id, owner_document_id, field_id, locale, occurrence
FROM ridu_document_references
WHERE target_collection_id = $1 AND target_document_id = $2
ORDER BY owner_collection_id, owner_document_id, field_id, locale, occurrence
FOR UPDATE`, string(request.Target.CollectionID), request.Target.DocumentID)
	if err != nil {
		return translateError(err)
	}
	var matches []referenceDeleteMatch
	for rows.Next() {
		entry := referenceindex.Entry{Target: request.Target}
		if err := rows.Scan(&entry.Owner.CollectionID, &entry.Owner.DocumentID, &entry.FieldID, &entry.Locale, &entry.Occurrence); err != nil {
			rows.Close()
			return translateError(err)
		}
		if _, skip := ignored[entry.Owner]; skip {
			continue
		}
		collection, exists := request.Collections[entry.Owner.CollectionID]
		if !exists {
			rows.Close()
			return fmt.Errorf("reference owner collection %q is unavailable", entry.Owner.CollectionID)
		}
		field, root, exists := referenceindex.FindReferenceField(collection, entry.FieldID)
		if !exists {
			rows.Close()
			return fmt.Errorf("reference owner field %q is unavailable", entry.FieldID)
		}
		matches = append(matches, referenceDeleteMatch{entry: entry, field: field, root: root, collection: collection})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	rows.Close()

	constraintSet := make(map[store.ReferenceConstraint]struct{})
	for _, match := range matches {
		action := schema.ReferenceDeleteNullify
		if match.field.Relationship != nil {
			action = match.field.Relationship.OnDelete
		} else if match.field.Upload != nil {
			action = match.field.Upload.OnDelete
		}
		switch action {
		case schema.ReferenceDeleteNullify:
		case schema.ReferenceDeleteRestrict:
			constraintSet[store.ReferenceConstraint{OwnerCollectionID: match.entry.Owner.CollectionID, FieldID: match.entry.FieldID}] = struct{}{}
		default:
			return fmt.Errorf("reference owner field %q has unsupported delete action %q", match.entry.FieldID, action)
		}
	}
	if len(constraintSet) != 0 {
		constraints := make([]store.ReferenceConstraint, 0, len(constraintSet))
		for constraint := range constraintSet {
			constraints = append(constraints, constraint)
		}
		sort.Slice(constraints, func(left, right int) bool {
			if constraints[left].OwnerCollectionID != constraints[right].OwnerCollectionID {
				return constraints[left].OwnerCollectionID < constraints[right].OwnerCollectionID
			}
			return constraints[left].FieldID < constraints[right].FieldID
		})
		return &store.DeleteRestrictedError{Constraints: constraints}
	}

	type rootMutation struct {
		owner      store.DocumentReference
		collection schema.Collection
		root       schema.Field
		locale     schema.LocaleCode
	}
	mutations := make(map[string]rootMutation)
	for _, match := range matches {
		locale := schema.LocaleCode("")
		if match.root.Localized {
			locale = match.entry.Locale
			if locale == "" {
				return fmt.Errorf("localized reference field %q has an unscoped derived index row", match.entry.FieldID)
			}
		}
		key := string(match.entry.Owner.CollectionID) + "\x00" + match.entry.Owner.DocumentID + "\x00" + string(match.root.ID) + "\x00" + string(locale)
		mutations[key] = rootMutation{owner: match.entry.Owner, collection: match.collection, root: match.root, locale: locale}
	}
	keys := make([]string, 0, len(mutations))
	for key := range mutations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		mutation := mutations[key]
		column := fieldColumn(mutation.root.ID)
		if mutation.locale != "" {
			column = localizedFieldColumn(mutation.root.ID, mutation.locale)
		}
		statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1 FOR UPDATE", quote(column), quote(collectionTable(mutation.owner.CollectionID)), quote("id"))
		value, found, err := scanReferenceRootValue(transaction.transaction.QueryRow(ctx, statement, mutation.owner.DocumentID), mutation.root)
		if err != nil {
			return err
		}
		if !found {
			// Remove stale rows only after every restrict decision was planned.
			if err := transaction.deleteDocumentReferences(ctx, mutation.owner); err != nil {
				return err
			}
			continue
		}
		updated, changed, referenceErr := referenceindex.NullifyField(mutation.root, value, request.Target, mutation.locale)
		if referenceErr != nil {
			return referenceErr
		}
		if !changed {
			return fmt.Errorf("reference index for owner collection %q is inconsistent with current values", mutation.owner.CollectionID)
		}
		encoded, err := databaseValue(mutation.root, updated)
		if err != nil {
			return err
		}
		update := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2", quote(collectionTable(mutation.owner.CollectionID)), quote(column), quote("id"))
		if _, err := transaction.transaction.Exec(ctx, update, encoded, mutation.owner.DocumentID); err != nil {
			return translateError(err)
		}
		if err := transaction.replaceReferenceRootEntries(ctx, mutation.owner, mutation.root, updated, mutation.locale); err != nil {
			return err
		}
	}
	return nil
}

func scanReferenceRootValue(row pgx.Row, field schema.Field) (store.Value, bool, error) {
	switch columnType(field) {
	case "jsonb":
		var encoded []byte
		if err := row.Scan(&encoded); errors.Is(err, pgx.ErrNoRows) {
			return store.Value{}, false, nil
		} else if err != nil {
			return store.Value{}, false, translateError(err)
		}
		if encoded == nil {
			return store.Null(), true, nil
		}
		var value store.Value
		if err := json.Unmarshal(encoded, &value); err != nil {
			return store.Value{}, false, fmt.Errorf("decode reference root %s: %w", field.Path, err)
		}
		return value, true, nil
	case "double precision":
		var value *float64
		if err := row.Scan(&value); errors.Is(err, pgx.ErrNoRows) {
			return store.Value{}, false, nil
		} else if err != nil {
			return store.Value{}, false, translateError(err)
		}
		if value == nil {
			return store.Null(), true, nil
		}
		return store.Number(*value), true, nil
	case "boolean":
		var value *bool
		if err := row.Scan(&value); errors.Is(err, pgx.ErrNoRows) {
			return store.Value{}, false, nil
		} else if err != nil {
			return store.Value{}, false, translateError(err)
		}
		if value == nil {
			return store.Null(), true, nil
		}
		return store.Boolean(*value), true, nil
	default:
		var value *string
		if err := row.Scan(&value); errors.Is(err, pgx.ErrNoRows) {
			return store.Value{}, false, nil
		} else if err != nil {
			return store.Value{}, false, translateError(err)
		}
		if value == nil {
			return store.Null(), true, nil
		}
		return store.String(*value), true, nil
	}
}

func (transaction *documentTransaction) replaceReferenceRootEntries(ctx context.Context, owner store.DocumentReference, root schema.Field, value store.Value, locale schema.LocaleCode) error {
	fieldIDs := referenceindex.ReferenceFieldIDs(root)
	if len(fieldIDs) == 0 {
		return nil
	}
	ids := make([]string, len(fieldIDs))
	for index, id := range fieldIDs {
		ids[index] = string(id)
	}
	statement := `DELETE FROM ridu_document_references
WHERE owner_collection_id = $1 AND owner_document_id = $2 AND field_id = ANY($3)`
	arguments := []any{string(owner.CollectionID), owner.DocumentID, ids}
	if root.Localized {
		statement += " AND locale = $4"
		arguments = append(arguments, string(locale))
	}
	if _, err := transaction.transaction.Exec(ctx, statement, arguments...); err != nil {
		return translateError(err)
	}
	insert := `INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id,
  target_collection_id, target_document_id, locale, occurrence
) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	entries, referenceErr := referenceindex.CollectField(owner, root, value, locale)
	if referenceErr != nil {
		return referenceErr
	}
	for _, entry := range entries {
		if _, err := transaction.transaction.Exec(ctx, insert,
			string(entry.Owner.CollectionID), entry.Owner.DocumentID, string(entry.FieldID),
			string(entry.Target.CollectionID), entry.Target.DocumentID, string(entry.Locale), entry.Occurrence,
		); err != nil {
			return translateError(err)
		}
	}
	return nil
}

func (transaction *documentTransaction) DeleteDocumentState(ctx context.Context, reference store.DocumentReference) error {
	if reference.CollectionID == "" || reference.DocumentID == "" {
		return fmt.Errorf("document state cleanup requires collection and document IDs")
	}
	statements := []struct {
		table string
		query string
	}{
		{"ridu_versions", `DELETE FROM ridu_versions WHERE collection_id = $1 AND document_id = $2`},
		{"ridu_tasks", `DELETE FROM ridu_tasks WHERE (target_collection_id = $1 AND target_document_id = $2) OR (requested_by_collection_id = $1 AND requested_by_document_id = $2)`},
		{"ridu_document_locks", `DELETE FROM ridu_document_locks WHERE (collection_id = $1 AND document_id = $2) OR (owner_collection_id = $1 AND owner_id = $2)`},
		{"ridu_auth_tokens", `DELETE FROM ridu_auth_tokens WHERE collection_id = $1 AND user_id = $2`},
		{"ridu_auth_sessions", `DELETE FROM ridu_auth_sessions WHERE collection_id = $1 AND user_id = $2`},
		{"ridu_auth_api_keys", `DELETE FROM ridu_auth_api_keys WHERE collection_id = $1 AND user_id = $2`},
		{"ridu_auth_credentials", `DELETE FROM ridu_auth_credentials WHERE collection_id = $1 AND user_id = $2`},
		{"ridu_preferences", `DELETE FROM ridu_preferences WHERE collection_id = $1 AND user_id = $2`},
	}
	for _, statement := range statements {
		exists, known := transaction.tableExists[statement.table]
		if !known {
			if err := transaction.transaction.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, statement.table).Scan(&exists); err != nil {
				return err
			}
			transaction.tableExists[statement.table] = exists
		}
		if !exists {
			continue
		}
		if _, err := transaction.transaction.Exec(ctx, statement.query, string(reference.CollectionID), reference.DocumentID); err != nil {
			return err
		}
	}
	return nil
}

func (transaction *documentTransaction) replaceDocumentReferences(ctx context.Context, collection schema.Collection, document store.Document) error {
	owner := store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}
	if err := transaction.deleteDocumentReferences(ctx, owner); err != nil {
		return err
	}
	statement := `INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id,
  target_collection_id, target_document_id, locale, occurrence
) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	entries, referenceErr := referenceindex.Collect(collection, document)
	if referenceErr != nil {
		return referenceErr
	}
	for _, entry := range entries {
		if _, err := transaction.transaction.Exec(ctx, statement,
			string(entry.Owner.CollectionID), entry.Owner.DocumentID, string(entry.FieldID),
			string(entry.Target.CollectionID), entry.Target.DocumentID, string(entry.Locale), entry.Occurrence,
		); err != nil {
			return translateError(err)
		}
	}
	return nil
}

func (transaction *documentTransaction) deleteDocumentReferences(ctx context.Context, owner store.DocumentReference) error {
	_, err := transaction.transaction.Exec(ctx, `DELETE FROM ridu_document_references WHERE owner_collection_id = $1 AND owner_document_id = $2`, string(owner.CollectionID), owner.DocumentID)
	return translateError(err)
}

func (transaction *documentTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	request.Deletion = store.DeletionActive
	predicate, arguments, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	fields := storedSchemaFields(request.Collection.Fields)
	statement := fmt.Sprintf("UPDATE %s SET %s = now(), %s = now() WHERE %s RETURNING %s", quote(collectionTable(request.Collection.ID)), quote("deleted_at"), quote("updated_at"), predicate, selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	return document, nil
}

func (transaction *documentTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	request.Deletion = store.DeletionTrash
	predicate, arguments, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	fields := storedSchemaFields(request.Collection.Fields)
	statement := fmt.Sprintf("UPDATE %s SET %s = NULL, %s = now() WHERE %s RETURNING %s", quote(collectionTable(request.Collection.ID)), quote("deleted_at"), quote("updated_at"), predicate, selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	return document, nil
}

func (transaction *documentTransaction) SaveVersion(ctx context.Context, collection schema.Collection, document store.Document, maximum int) (store.Version, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return store.Version{}, fmt.Errorf("encode version snapshot: %w", err)
	}
	statement := "INSERT INTO " + quote("ridu_versions") + " (" + quote("collection_id") + ", " + quote("document_id") + ", " + quote("revision") + ", " + quote("status") + ", " + quote("snapshot") + ") VALUES ($1, $2, $3, $4, $5) ON CONFLICT (" + quote("collection_id") + ", " + quote("document_id") + ", " + quote("revision") + ") DO UPDATE SET " + quote("status") + " = EXCLUDED." + quote("status") + ", " + quote("snapshot") + " = EXCLUDED." + quote("snapshot") + " RETURNING " + quote("created_at")
	version := store.Version{ID: document.ID + ":" + fmt.Sprint(document.Revision), DocumentID: document.ID, Revision: document.Revision, Status: document.Status, Snapshot: store.CloneDocument(document)}
	if err := transaction.transaction.QueryRow(ctx, statement, collection.ID, document.ID, document.Revision, document.Status, encoded).Scan(&version.CreatedAt); err != nil {
		return store.Version{}, translateError(err)
	}
	if maximum > 0 {
		prune := "DELETE FROM " + quote("ridu_versions") + " WHERE " + quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2 AND " + quote("revision") + " NOT IN (SELECT " + quote("revision") + " FROM " + quote("ridu_versions") + " WHERE " + quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2 ORDER BY " + quote("revision") + " DESC LIMIT $3)"
		if _, err := transaction.transaction.Exec(ctx, prune, collection.ID, document.ID, maximum); err != nil {
			return store.Version{}, translateError(err)
		}
	}
	return version, nil
}

func (transaction *documentTransaction) ListVersions(ctx context.Context, request store.VersionRequest) ([]store.Version, error) {
	collection, documentID, access := request.Collection, request.DocumentID, request.Access
	arguments := []any{collection.ID, documentID}
	predicate := quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2"
	if access != nil {
		compiler := predicateCompiler{collection: collection, next: len(arguments), arguments: arguments, snapshot: true, localeChain: request.LocaleChain}
		compiled, err := compileAccessPredicate(&compiler, *access, request.AllLocales, request.Locales)
		if err != nil {
			return nil, err
		}
		predicate += " AND (" + compiled + ")"
		arguments = compiler.arguments
	}
	statement := "SELECT " + quote("revision") + ", " + quote("status") + ", " + quote("snapshot") + ", " + quote("created_at") + " FROM " + quote("ridu_versions") + " WHERE " + predicate + " ORDER BY " + quote("revision") + " DESC"
	rows, err := transaction.transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var versions []store.Version
	for rows.Next() {
		var version store.Version
		var encoded []byte
		version.DocumentID = documentID
		if err := rows.Scan(&version.Revision, &version.Status, &encoded, &version.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &version.Snapshot); err != nil {
			return nil, fmt.Errorf("decode version snapshot: %w", err)
		}
		version.ID = documentID + ":" + fmt.Sprint(version.Revision)
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (transaction *documentTransaction) FindVersion(ctx context.Context, collection schema.Collection, documentID string, revision int) (store.Version, error) {
	statement := "SELECT " + quote("status") + ", " + quote("snapshot") + ", " + quote("created_at") + " FROM " + quote("ridu_versions") + " WHERE " + quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2 AND " + quote("revision") + " = $3"
	version := store.Version{ID: documentID + ":" + fmt.Sprint(revision), DocumentID: documentID, Revision: revision}
	var encoded []byte
	if err := transaction.transaction.QueryRow(ctx, statement, collection.ID, documentID, revision).Scan(&version.Status, &encoded, &version.CreatedAt); err != nil {
		return store.Version{}, translateError(err)
	}
	if err := json.Unmarshal(encoded, &version.Snapshot); err != nil {
		return store.Version{}, fmt.Errorf("decode version snapshot: %w", err)
	}
	return version, nil
}

func (transaction *documentTransaction) Commit(ctx context.Context) error {
	return translateError(transaction.transaction.Commit(ctx))
}
func (transaction *documentTransaction) Rollback(ctx context.Context) error {
	return rollbackPostgresTransaction(ctx, transaction.transaction)
}

type rowScanner interface{ Scan(...any) error }

func scanDocument(row rowScanner, collection schema.Collection, fields []schema.Field) (store.Document, error) {
	var document store.Document
	destinations, finish := documentDestinations(&document, collection, fields)
	if err := row.Scan(destinations...); err != nil {
		return store.Document{}, err
	}
	if err := finish(); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func documentDestinations(document *store.Document, collection schema.Collection, fields []schema.Field) ([]any, func() error) {
	destinations := []any{&document.ID, &document.CreatedAt, &document.UpdatedAt, &document.DeletedAt}
	if collection.Versions != nil {
		destinations = append(destinations, &document.Status, &document.Revision)
	}
	fieldValues := make([]any, len(fields))
	for index, field := range fields {
		if field.Localized {
			var value []byte
			fieldValues[index] = &value
			destinations = append(destinations, &value)
			continue
		}
		if isJSONStoredField(field) {
			var value []byte
			fieldValues[index] = &value
			destinations = append(destinations, &value)
			continue
		}
		switch field.Type {
		case schema.FieldTypeRelationship:
			var value *string
			fieldValues[index] = &value
			destinations = append(destinations, &value)
		case schema.FieldTypeUpload:
			var value *string
			fieldValues[index] = &value
			destinations = append(destinations, &value)
		case schema.FieldTypeNumber:
			var value *float64
			fieldValues[index] = &value
			destinations = append(destinations, &value)
		case schema.FieldTypeCheckbox:
			var value *bool
			fieldValues[index] = &value
			destinations = append(destinations, &value)
		default:
			var value *string
			fieldValues[index] = &value
			destinations = append(destinations, &value)
		}
	}
	return destinations, func() error {
		document.Values = make(store.Values, len(fields))
		for index, field := range fields {
			switch value := fieldValues[index].(type) {
			case **string:
				if *value == nil {
					document.Values[field.Name] = store.Null()
				} else {
					document.Values[field.Name] = store.String(**value)
				}
			case *[]byte:
				if *value == nil {
					document.Values[field.Name] = store.Null()
				} else {
					var decoded store.Value
					if err := json.Unmarshal(*value, &decoded); err != nil {
						return fmt.Errorf("decode JSON field %s: %w", field.Name, err)
					}
					document.Values[field.Name] = decoded
				}
			case **float64:
				if *value == nil {
					document.Values[field.Name] = store.Null()
				} else {
					document.Values[field.Name] = store.Number(**value)
				}
			case **bool:
				if *value == nil {
					document.Values[field.Name] = store.Null()
				} else {
					document.Values[field.Name] = store.Boolean(**value)
				}
			}
		}
		return nil
	}
}

func requestPredicate(request store.Request, requireID bool) (string, []any, error) {
	return requestPredicateFrom(request, requireID, 0)
}

func requestPredicateFrom(request store.Request, requireID bool, offset int) (string, []any, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return "", nil, err
	}
	compiler := predicateCompiler{collection: request.Collection, next: offset, localeChain: request.LocaleChain}
	var predicates []string
	if requireID {
		if request.ID == "" {
			return "", nil, fmt.Errorf("document ID is required")
		}
		compiler.arguments = append(compiler.arguments, request.ID)
		compiler.next++
		predicates = append(predicates, fmt.Sprintf("%s = $%d", quote("id"), compiler.next))
	}
	if request.PublishedOnly && request.Collection.Versions != nil {
		predicates = append(predicates, quote("_status")+" = 'published'")
	}
	switch request.Deletion {
	case store.DeletionAll:
	case store.DeletionTrash:
		predicates = append(predicates, quote("deleted_at")+" IS NOT NULL")
	default:
		predicates = append(predicates, quote("deleted_at")+" IS NULL")
	}
	if request.ExpectedRevision > 0 {
		compiler.arguments = append(compiler.arguments, request.ExpectedRevision)
		compiler.next++
		predicates = append(predicates, fmt.Sprintf("%s = $%d", quote("_revision"), compiler.next))
	}
	if request.Filter != nil {
		compiler.callerFilter = true
		compiled, err := compiler.compile(*request.Filter)
		compiler.callerFilter = false
		if err != nil {
			return "", nil, err
		}
		predicates = append(predicates, compiled)
	}
	if request.Access != nil {
		compiled, err := compileAccessPredicate(&compiler, *request.Access, request.AllLocales, request.Locales)
		if err != nil {
			return "", nil, err
		}
		predicates = append(predicates, compiled)
	}
	if len(predicates) == 0 {
		return "TRUE", compiler.arguments, nil
	}
	return strings.Join(predicates, " AND "), compiler.arguments, nil
}

func compileAccessPredicate(compiler *predicateCompiler, access query.Node, allLocales bool, locales []schema.LocaleCode) (string, error) {
	if !allLocales || len(locales) == 0 {
		return compiler.compile(access)
	}
	originalChain := compiler.localeChain
	defer func() { compiler.localeChain = originalChain }()
	predicates := make([]string, 0, len(locales))
	for _, locale := range locales {
		compiler.localeChain = []schema.LocaleCode{locale}
		compiled, err := compiler.compile(access)
		if err != nil {
			return "", err
		}
		predicates = append(predicates, "("+compiled+")")
	}
	return strings.Join(predicates, " AND "), nil
}

type predicateCompiler struct {
	collection   schema.Collection
	next         int
	arguments    []any
	snapshot     bool
	localeChain  []schema.LocaleCode
	callerFilter bool
}

func (compiler *predicateCompiler) compile(node query.Node) (string, error) {
	switch node.Kind {
	case query.ExpressionComparison:
		if node.Comparison == nil {
			return "", fmt.Errorf("comparison node is missing its comparison")
		}
		resolved, err := resolvePredicatePath(compiler.collection, node.Comparison.Path)
		if err != nil {
			if compiler.callerFilter {
				return "", primitivefield.UnsupportedPath(compiler.collection.Fields, node.Comparison.Path, err)
			}
			return "", err
		}
		if err := primitivefield.ValidateComparison(resolved.leaf, *node.Comparison); err != nil {
			return "", err
		}
		resolved.snapshot = compiler.snapshot
		resolved.localeChain = compiler.localeChain
		if resolved.many {
			return compiler.compileManyComparison(resolved, *node.Comparison)
		}
		return compiler.compilePathComparison(resolved, false, *node.Comparison)
	case query.ExpressionAnd, query.ExpressionOr:
		if len(node.Children) < 2 {
			return "", fmt.Errorf("%s predicate requires at least two children", node.Kind)
		}
		children := make([]string, len(node.Children))
		for index, child := range node.Children {
			compiled, err := compiler.compile(child)
			if err != nil {
				return "", err
			}
			children[index] = "(" + compiled + ")"
		}
		separator := " AND "
		if node.Kind == query.ExpressionOr {
			separator = " OR "
		}
		return strings.Join(children, separator), nil
	case query.ExpressionNot:
		if len(node.Children) != 1 {
			return "", fmt.Errorf("not predicate requires one child")
		}
		child, err := compiler.compile(node.Children[0])
		return "NOT (COALESCE((" + child + "), FALSE))", err
	default:
		return "", fmt.Errorf("unsupported predicate kind %q", node.Kind)
	}
}

func (compiler *predicateCompiler) column(path query.Path) (string, error) {
	resolved, err := resolvePredicatePath(compiler.collection, path)
	if err != nil {
		return "", err
	}
	if primitivefield.IsList(resolved.leaf) {
		return "", fmt.Errorf("primitive list %q cannot be sorted", path.String())
	}
	if resolved.many {
		return "", fmt.Errorf("sort field %q traverses a repeated field", path.String())
	}
	resolved.localeChain = compiler.localeChain
	return resolved.scalarColumn(), nil
}

func (compiler *predicateCompiler) compileScalarComparison(column, stringGuard string, field schema.Field, comparison query.Comparison) (string, error) {
	if comparison.Operator == query.OperatorExists {
		exists, _ := comparison.Value.BooleanValue()
		if exists {
			return column + " IS NOT NULL", nil
		}
		return column + " IS NULL", nil
	}
	if comparison.Operator == query.OperatorIn {
		compiled, _, err := compiler.compileInComparison(column, compatibleScalarValues(field, comparison.Value.Values()))
		return compiled, err
	}
	if comparison.Operator == query.OperatorContains || comparison.Operator == query.OperatorLike {
		if !supportsStringPredicate(field) {
			return "FALSE", nil
		}
		text, _ := comparison.Value.StringValue()
		words := []string{text}
		if comparison.Operator == query.OperatorLike {
			words = strings.Fields(text)
		}
		if len(words) == 0 {
			predicate := column + " IS NOT NULL"
			if stringGuard != "" {
				predicate = "(" + stringGuard + " AND " + predicate + ")"
			}
			return predicate, nil
		}
		predicates := make([]string, len(words))
		for index, word := range words {
			compiler.arguments = append(compiler.arguments, "%"+escapeLike(word)+"%")
			compiler.next++
			predicates[index] = fmt.Sprintf("%s ILIKE $%d ESCAPE E'\\\\'", column, compiler.next)
		}
		predicate := "(" + strings.Join(predicates, " AND ") + ")"
		if stringGuard != "" {
			predicate = "(" + stringGuard + " AND " + predicate + ")"
		}
		return predicate, nil
	}
	if comparison.Value.Kind() == query.ValueNull {
		if comparison.Operator == query.OperatorNotEqual {
			return column + " IS NOT NULL", nil
		}
		return column + " IS NULL", nil
	}
	if expected := scalarFieldValueKind(field); expected == "" || comparison.Value.Kind() != expected {
		if comparison.Operator == query.OperatorNotEqual {
			return "TRUE", nil
		}
		return "FALSE", nil
	}
	placeholder, err := compiler.operand(comparison.Value)
	if err != nil {
		return "", err
	}
	if comparison.Operator == query.OperatorNotEqual {
		return fmt.Sprintf("(%s IS NULL OR %s <> %s)", column, column, placeholder), nil
	}
	operators := map[query.Operator]string{
		query.OperatorEqual:       "=",
		query.OperatorGreaterThan: ">", query.OperatorGreaterThanEqual: ">=",
		query.OperatorLessThan: "<", query.OperatorLessThanEqual: "<=",
	}
	operator := operators[comparison.Operator]
	if operator == "" {
		return "", fmt.Errorf("unsupported comparison operator %q", comparison.Operator)
	}
	if comparison.Value.Kind() == query.ValueString && comparison.Operator != query.OperatorEqual {
		// Go string ordering is byte-lexicographic. Use PostgreSQL's permanent C
		// collation for ordered text predicates so access rules and reference
		// admission do not change with the database's deployment locale.
		column = "(" + column + ` COLLATE "C")`
	}
	return fmt.Sprintf("%s %s %s", column, operator, placeholder), nil
}

func (compiler *predicateCompiler) compilePathComparison(path predicatePath, row bool, comparison query.Comparison) (string, error) {
	if path.root.ID == "createdAt" || path.root.ID == "updatedAt" {
		return compiler.compileTimestampComparison(path.scalarColumn(), comparison)
	}
	if isJSONStoredField(path.leaf) {
		raw := path.scalarJSONColumn()
		if row {
			raw = path.rowJSONColumn()
		}
		return compiler.compileJSONComparison(raw, path.leaf, comparison)
	}
	column, stringGuard := path.scalarComparisonColumn(comparison.Operator)
	if row {
		column, stringGuard = path.rowComparisonColumn(comparison.Operator)
	}
	return compiler.compileScalarComparison(column, stringGuard, path.leaf, comparison)
}

func (compiler *predicateCompiler) compileTimestampComparison(column string, comparison query.Comparison) (string, error) {
	switch comparison.Operator {
	case query.OperatorExists:
		exists, _ := comparison.Value.BooleanValue()
		if exists {
			return "TRUE", nil
		}
		return "FALSE", nil
	case query.OperatorContains, query.OperatorLike:
		return "FALSE", nil
	case query.OperatorIn:
		var placeholders []string
		for _, value := range comparison.Value.Values() {
			if value.Kind() == query.ValueNull {
				continue
			}
			placeholder, err := compiler.timestampOperand(value)
			if err != nil {
				continue
			}
			placeholders = append(placeholders, placeholder)
		}
		if len(placeholders) == 0 {
			return "FALSE", nil
		}
		return column + " IN (" + strings.Join(placeholders, ", ") + ")", nil
	}
	if comparison.Value.Kind() == query.ValueNull {
		if comparison.Operator == query.OperatorNotEqual {
			return "TRUE", nil
		}
		return "FALSE", nil
	}
	placeholder, err := compiler.timestampOperand(comparison.Value)
	if err != nil {
		return "FALSE", nil
	}
	operator := map[query.Operator]string{
		query.OperatorEqual:            "=",
		query.OperatorNotEqual:         "<>",
		query.OperatorGreaterThan:      ">",
		query.OperatorGreaterThanEqual: ">=",
		query.OperatorLessThan:         "<",
		query.OperatorLessThanEqual:    "<=",
	}[comparison.Operator]
	if operator == "" {
		return "", fmt.Errorf("unsupported timestamp comparison operator %q", comparison.Operator)
	}
	return column + " " + operator + " " + placeholder, nil
}

func (compiler *predicateCompiler) compileJSONComparison(raw string, field schema.Field, comparison query.Comparison) (string, error) {
	if primitivefield.IsList(field) {
		if err := primitivefield.ValidateComparison(field, comparison); err != nil {
			return "", err
		}
		if comparison.Operator == query.OperatorIn {
			predicates := make([]string, 0, len(comparison.Value.Values()))
			cast := "::text"
			if field.Type == schema.FieldTypeNumberList {
				cast = "::double precision"
			}
			for _, item := range comparison.Value.Values() {
				placeholder, err := compiler.operand(item)
				if err != nil {
					return "", err
				}
				predicates = append(predicates, "COALESCE("+raw+" @> jsonb_build_array("+placeholder+cast+"), FALSE)")
			}
			if len(predicates) == 0 {
				return "FALSE", nil
			}
			return "(" + strings.Join(predicates, " OR ") + ")", nil
		}
	}

	if comparison.Operator == query.OperatorExists {
		exists, _ := comparison.Value.BooleanValue()
		present := "(" + raw + " IS NOT NULL AND " + raw + " <> 'null'::jsonb)"
		if exists {
			return present, nil
		}
		return "NOT " + present, nil
	}
	if comparison.Operator == query.OperatorIn {
		values := comparison.Value.Values()
		if len(values) == 0 {
			return "FALSE", nil
		}
		predicates := make([]string, 0, len(values))
		for _, value := range values {
			predicate, err := compiler.compileJSONEqual(raw, value)
			if err != nil {
				return "", err
			}
			predicates = append(predicates, predicate)
		}
		if len(predicates) == 1 {
			return predicates[0], nil
		}
		return "(" + strings.Join(predicates, " OR ") + ")", nil
	}
	if comparison.Operator == query.OperatorEqual || comparison.Operator == query.OperatorNotEqual {
		equal, err := compiler.compileJSONEqual(raw, comparison.Value)
		if err != nil {
			return "", err
		}
		if comparison.Operator == query.OperatorNotEqual {
			return "NOT (" + equal + ")", nil
		}
		return equal, nil
	}
	if comparison.Operator == query.OperatorContains || comparison.Operator == query.OperatorLike {
		if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
			if comparison.Operator == query.OperatorLike {
				return "FALSE", nil
			}
			placeholder, err := compiler.operand(comparison.Value)
			if err != nil {
				return "", err
			}
			return "COALESCE(" + raw + " @> jsonb_build_array(" + placeholder + "::text), FALSE)", nil
		}
		column := jsonTypedColumn(raw, query.ValueString)
		return compiler.compileScalarComparison(column, "", field, comparison)
	}
	if comparison.Operator == query.OperatorGreaterThan || comparison.Operator == query.OperatorGreaterThanEqual || comparison.Operator == query.OperatorLessThan || comparison.Operator == query.OperatorLessThanEqual {
		column := jsonTypedColumn(raw, comparison.Value.Kind())
		if comparison.Value.Kind() == query.ValueString {
			column = "(" + column + ` COLLATE "C")`
		}
		placeholder, err := compiler.operand(comparison.Value)
		if err != nil {
			return "", err
		}
		operators := map[query.Operator]string{
			query.OperatorGreaterThan: ">", query.OperatorGreaterThanEqual: ">=",
			query.OperatorLessThan: "<", query.OperatorLessThanEqual: "<=",
		}
		return "COALESCE((" + column + " " + operators[comparison.Operator] + " " + placeholder + "), FALSE)", nil
	}
	return "", fmt.Errorf("unsupported JSON comparison operator %q", comparison.Operator)
}

func (compiler *predicateCompiler) compileJSONEqual(raw string, expected query.Value) (string, error) {
	if expected.Kind() == query.ValueNull {
		return "(" + raw + " IS NULL OR " + raw + " = 'null'::jsonb)", nil
	}
	column := jsonTypedColumn(raw, expected.Kind())
	placeholder, err := compiler.operand(expected)
	if err != nil {
		return "", err
	}
	return "COALESCE((" + column + " = " + placeholder + "), FALSE)", nil
}

func jsonTypedColumn(raw string, kind query.ValueKind) string {
	jsonType := ""
	cast := ""
	switch kind {
	case query.ValueString:
		jsonType = "string"
	case query.ValueNumber:
		jsonType, cast = "number", "::double precision"
	case query.ValueBoolean:
		jsonType, cast = "boolean", "::boolean"
	default:
		return "NULL"
	}
	return "(CASE WHEN jsonb_typeof(" + raw + ") = '" + jsonType + "' THEN (" + raw + " #>> '{}')" + cast + " END)"
}

func comparisonIncludesNull(values []query.Value) bool {
	for _, value := range values {
		if value.Kind() == query.ValueNull {
			return true
		}
	}
	return false
}

func (compiler *predicateCompiler) compileManyComparison(resolved predicatePath, comparison query.Comparison) (string, error) {
	array := safeJSONArray(resolved.arrayColumn())
	var blockCondition string
	if resolved.blockType != "" {
		compiler.arguments = append(compiler.arguments, resolved.blockType)
		compiler.next++
		blockCondition = fmt.Sprintf("ridu_item.value ->> 'blockType' = $%d", compiler.next)
	}
	exists := func(condition string) string {
		conditions := make([]string, 0, 2)
		if condition != "" {
			conditions = append(conditions, condition)
		}
		if blockCondition != "" {
			conditions = append(conditions, blockCondition)
		}
		where := ""
		if len(conditions) != 0 {
			where = " WHERE " + strings.Join(conditions, " AND ")
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(%s) AS ridu_item(value)%s)", array, where)
	}
	present := resolved.rowPresenceColumn()
	if comparison.Operator == query.OperatorIn {
		var inner string
		var err error
		includesNull := comparisonIncludesNull(comparison.Value.Values())
		if isJSONStoredField(resolved.leaf) {
			inner, err = compiler.compileJSONComparison(resolved.rowJSONColumn(), resolved.leaf, comparison)
		} else {
			inner, _, err = compiler.compileInComparison(resolved.rowColumn(), compatibleScalarValues(resolved.leaf, comparison.Value.Values()))
		}
		if err != nil {
			return "", err
		}
		matching := exists("(" + present + ") AND (" + inner + ")")
		if !includesNull {
			return matching, nil
		}
		return "(NOT " + exists(present) + " OR " + matching + ")", nil
	}
	if comparison.Value.Kind() == query.ValueNull && (comparison.Operator == query.OperatorEqual || comparison.Operator == query.OperatorNotEqual) {
		nullValue := "(" + present + ") AND (" + resolved.rowNullPredicate() + ")"
		equal := "(NOT " + exists(present) + " OR " + exists(nullValue) + ")"
		if comparison.Operator == query.OperatorNotEqual {
			return "NOT " + equal, nil
		}
		return equal, nil
	}
	negate := false
	if comparison.Operator == query.OperatorNotEqual {
		comparison.Operator = query.OperatorEqual
		negate = true
	} else if comparison.Operator == query.OperatorExists {
		exists, _ := comparison.Value.BooleanValue()
		comparison.Value = query.Boolean(true)
		negate = !exists
	}
	inner, err := compiler.compilePathComparison(resolved, true, comparison)
	if err != nil {
		return "", err
	}
	predicate := exists(inner)
	if negate {
		return "NOT " + predicate, nil
	}
	return predicate, nil
}

func (compiler *predicateCompiler) compileInComparison(column string, values []query.Value) (string, bool, error) {
	includesNull := false
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		if value.Kind() == query.ValueNull {
			includesNull = true
			continue
		}
		placeholder, err := compiler.operand(value)
		if err != nil {
			return "", false, err
		}
		placeholders = append(placeholders, placeholder)
	}
	parts := make([]string, 0, 2)
	if includesNull {
		parts = append(parts, column+" IS NULL")
	}
	if len(placeholders) != 0 {
		parts = append(parts, fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")))
	}
	if len(parts) == 0 {
		return "FALSE", false, nil
	}
	if len(parts) == 1 {
		return parts[0], includesNull, nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", includesNull, nil
}

type predicatePath struct {
	root                schema.Field
	leaf                schema.Field
	jsonSegments        []string
	jsonLocalizedAfter  []int
	arrayPath           []string
	arrayLocalizedAfter []int
	rowPath             []string
	rowLocalizedAfter   []int
	blockType           string
	many                bool
	snapshot            bool
	localeChain         []schema.LocaleCode
}

func (path predicatePath) scalarComparisonColumn(operator query.Operator) (string, string) {
	if (operator == query.OperatorContains || operator == query.OperatorLike) && isJSONStoredField(path.leaf) {
		raw := path.scalarJSONColumn()
		return raw + " #>> '{}'", "COALESCE(jsonb_typeof(" + raw + ") = 'string', FALSE)"
	}
	return path.scalarColumn(), ""
}

func (path predicatePath) scalarColumn() string {
	if path.root.ID == "id" && path.root.Name == "id" {
		if path.snapshot {
			return quote("snapshot") + " ->> 'ID'"
		}
		return quote("id")
	}
	if path.root.ID == "createdAt" && path.root.Name == "createdAt" {
		if path.snapshot {
			return "(" + quote("snapshot") + " ->> 'CreatedAt')::timestamptz"
		}
		return quote("created_at")
	}
	if path.root.ID == "updatedAt" && path.root.Name == "updatedAt" {
		if path.snapshot {
			return "(" + quote("snapshot") + " ->> 'UpdatedAt')::timestamptz"
		}
		return quote("updated_at")
	}
	if path.root.ID == "_status" && path.root.Name == "_status" {
		if path.snapshot {
			return quote("snapshot") + " ->> 'Status'"
		}
		return quote("_status")
	}
	if path.snapshot {
		if path.root.Localized && len(path.localeChain) != 0 {
			if len(path.jsonSegments) == 0 {
				return coalescedJSONText(quote("snapshot"), path.localeChain, func(locale schema.LocaleCode) []string {
					return []string{"Values", path.root.Name, string(locale)}
				}, path.leaf)
			}
			containers := make([]string, 0, len(path.localeChain))
			for _, locale := range path.localeChain {
				containers = append(containers, quote("snapshot")+" #> '{Values,"+path.root.Name+","+string(locale)+"}'")
			}
			column := coalescedJSONValue(containers) + " #>> '{" + strings.Join(path.jsonSegments, ",") + "}'"
			return castJSONScalar(column, path.leaf.Type, true)
		}
		if len(path.jsonLocalizedAfter) != 0 && len(path.localeChain) != 0 {
			return localizedScalarColumn(quote("snapshot"), append([]string{"Values", path.root.Name}, path.jsonSegments...), offsetBoundaries(path.jsonLocalizedAfter, 2), path.localeChain, path.leaf)
		}
		segments := append([]string{"Values", path.root.Name}, path.jsonSegments...)
		column := quote("snapshot") + " #>> '{" + strings.Join(segments, ",") + "}'"
		return castJSONScalar(column, path.leaf.Type, true)
	}
	if path.root.Localized && len(path.localeChain) != 0 {
		if len(path.jsonSegments) != 0 {
			containers := make([]string, 0, len(path.localeChain))
			for _, locale := range path.localeChain {
				containers = append(containers, quote(localizedFieldColumn(path.root.ID, locale)))
			}
			column := coalescedJSONValue(containers) + " #>> '{" + strings.Join(path.jsonSegments, ",") + "}'"
			return castJSONScalar(column, path.leaf.Type, true)
		}
		columns := make([]string, 0, len(path.localeChain))
		for index, locale := range path.localeChain {
			column := quote(localizedFieldColumn(path.root.ID, locale))
			columns = append(columns, fallbackScalarExpression(column, path.leaf, index < len(path.localeChain)-1))
		}
		column := columns[0]
		if len(columns) > 1 {
			column = "COALESCE(" + strings.Join(columns, ", ") + ")"
		}
		return castJSONScalar(column, path.leaf.Type, false)
	}
	column := quote(fieldColumn(path.root.ID))
	if len(path.jsonLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		return localizedScalarColumn(column, path.jsonSegments, path.jsonLocalizedAfter, path.localeChain, path.leaf)
	}
	if len(path.jsonSegments) != 0 {
		column += " #>> '{" + strings.Join(path.jsonSegments, ",") + "}'"
	}
	return castJSONScalar(column, path.leaf.Type, len(path.jsonSegments) != 0)
}

func (path predicatePath) arrayColumn() string {
	if path.snapshot {
		if path.root.Localized && len(path.localeChain) != 0 {
			containers := make([]string, 0, len(path.localeChain))
			for _, locale := range path.localeChain {
				containers = append(containers, quote("snapshot")+" #> '{Values,"+path.root.Name+","+string(locale)+"}'")
			}
			return jsonValueAt(coalescedJSONValue(containers), path.arrayPath)
		}
		if len(path.arrayLocalizedAfter) != 0 && len(path.localeChain) != 0 {
			return localizedJSONColumn(quote("snapshot"), append([]string{"Values", path.root.Name}, path.arrayPath...), offsetBoundaries(path.arrayLocalizedAfter, 2), path.localeChain)
		}
		segments := append([]string{"Values", path.root.Name}, path.arrayPath...)
		return quote("snapshot") + " #> '{" + strings.Join(segments, ",") + "}'"
	}
	column := quote(fieldColumn(path.root.ID))
	if path.root.Localized && len(path.localeChain) != 0 {
		containers := make([]string, 0, len(path.localeChain))
		for _, locale := range path.localeChain {
			containers = append(containers, quote(localizedFieldColumn(path.root.ID, locale)))
		}
		return jsonValueAt(coalescedJSONValue(containers), path.arrayPath)
	}
	if len(path.arrayLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		return localizedJSONColumn(column, path.arrayPath, path.arrayLocalizedAfter, path.localeChain)
	}
	if len(path.arrayPath) != 0 {
		column += " #> '{" + strings.Join(path.arrayPath, ",") + "}'"
	}
	return column
}

func (path predicatePath) rowColumn() string {
	column := "ridu_item.value"
	if len(path.rowLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		return localizedScalarColumn(column, path.rowPath, path.rowLocalizedAfter, path.localeChain, path.leaf)
	}
	if len(path.rowPath) != 0 {
		column += " #>> '{" + strings.Join(path.rowPath, ",") + "}'"
	}
	return castJSONScalar(column, path.leaf.Type, true)
}

func (path predicatePath) rowComparisonColumn(operator query.Operator) (string, string) {
	if (operator == query.OperatorContains || operator == query.OperatorLike) && isJSONStoredField(path.leaf) {
		raw := path.rowJSONColumn()
		return raw + " #>> '{}'", "COALESCE(jsonb_typeof(" + raw + ") = 'string', FALSE)"
	}
	return path.rowColumn(), ""
}

func (path predicatePath) scalarJSONColumn() string {
	if path.snapshot {
		if path.root.Localized && len(path.localeChain) != 0 {
			if len(path.jsonSegments) == 0 {
				candidates := make([]string, 0, len(path.localeChain))
				for _, locale := range path.localeChain {
					candidates = append(candidates, quote("snapshot")+" #> '{Values,"+path.root.Name+","+string(locale)+"}'")
				}
				return coalescedLocalizedJSONScalar(candidates)
			}
			containers := make([]string, 0, len(path.localeChain))
			for _, locale := range path.localeChain {
				containers = append(containers, quote("snapshot")+" #> '{Values,"+path.root.Name+","+string(locale)+"}'")
			}
			return jsonValueAt(coalescedJSONValue(containers), path.jsonSegments)
		}
		segments := append([]string{"Values", path.root.Name}, path.jsonSegments...)
		if len(path.jsonLocalizedAfter) != 0 && len(path.localeChain) != 0 {
			boundaries := offsetBoundaries(path.jsonLocalizedAfter, 2)
			if boundaries[0] == len(segments) {
				return localizedJSONScalar(quote("snapshot"), segments, boundaries[0], path.localeChain)
			}
			return localizedJSONColumn(quote("snapshot"), segments, boundaries, path.localeChain)
		}
		return quote("snapshot") + " #> '{" + strings.Join(segments, ",") + "}'"
	}
	if path.root.Localized && len(path.localeChain) != 0 {
		candidates := make([]string, 0, len(path.localeChain))
		for _, locale := range path.localeChain {
			candidates = append(candidates, quote(localizedFieldColumn(path.root.ID, locale)))
		}
		if len(path.jsonSegments) == 0 {
			return coalescedLocalizedJSONScalar(candidates)
		}
		return jsonValueAt(coalescedJSONValue(candidates), path.jsonSegments)
	}
	column := quote(fieldColumn(path.root.ID))
	if len(path.jsonLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		if path.jsonLocalizedAfter[0] == len(path.jsonSegments) {
			return localizedJSONScalar(column, path.jsonSegments, path.jsonLocalizedAfter[0], path.localeChain)
		}
		return localizedJSONColumn(column, path.jsonSegments, path.jsonLocalizedAfter, path.localeChain)
	}
	return jsonValueAt(column, path.jsonSegments)
}

func (path predicatePath) rowJSONColumn() string {
	if len(path.rowLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		if path.rowLocalizedAfter[0] == len(path.rowPath) {
			return localizedJSONScalar("ridu_item.value", path.rowPath, path.rowLocalizedAfter[0], path.localeChain)
		}
		return localizedJSONColumn("ridu_item.value", path.rowPath, path.rowLocalizedAfter, path.localeChain)
	}
	return jsonValueAt("ridu_item.value", path.rowPath)
}

func (path predicatePath) rowPresenceColumn() string {
	if len(path.rowLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		if path.rowLocalizedAfter[0] == len(path.rowPath) {
			return path.rowColumn() + " IS NOT NULL"
		}
		return localizedJSONColumn("ridu_item.value", path.rowPath, path.rowLocalizedAfter, path.localeChain) + " IS NOT NULL"
	}
	if len(path.rowPath) == 0 {
		return "ridu_item.value IS NOT NULL"
	}
	return "ridu_item.value #> '{" + strings.Join(path.rowPath, ",") + "}' IS NOT NULL"
}

func (path predicatePath) rowNullPredicate() string {
	if isJSONStoredField(path.leaf) {
		return path.rowJSONColumn() + " = 'null'::jsonb"
	}
	return path.rowColumn() + " IS NULL"
}

func localizedScalarColumn(column string, segments []string, localizedAfter []int, locales []schema.LocaleCode, field schema.Field) string {
	boundary := localizedAfter[0]
	if boundary == len(segments) {
		return coalescedJSONText(column, locales, func(locale schema.LocaleCode) []string {
			localized := append([]string(nil), segments...)
			return append(localized, string(locale))
		}, field)
	}
	container, remainder := localizedContainer(column, segments, boundary, locales)
	if len(remainder) != 0 {
		container += " #>> '{" + strings.Join(remainder, ",") + "}'"
	}
	return castJSONScalar(container, field.Type, true)
}

func localizedJSONColumn(column string, segments []string, localizedAfter []int, locales []schema.LocaleCode) string {
	container, remainder := localizedContainer(column, segments, localizedAfter[0], locales)
	return jsonValueAt(container, remainder)
}

func localizedContainer(column string, segments []string, boundary int, locales []schema.LocaleCode) (string, []string) {
	containers := make([]string, 0, len(locales))
	for _, locale := range locales {
		path := append(append([]string(nil), segments[:boundary]...), string(locale))
		containers = append(containers, column+" #> '{"+strings.Join(path, ",")+"}'")
	}
	return coalescedJSONValue(containers), segments[boundary:]
}

func jsonValueAt(column string, segments []string) string {
	if len(segments) == 0 {
		return column
	}
	return column + " #> '{" + strings.Join(segments, ",") + "}'"
}

func offsetBoundaries(boundaries []int, offset int) []int {
	result := make([]int, len(boundaries))
	for index, boundary := range boundaries {
		result[index] = boundary + offset
	}
	return result
}

func coalescedJSONValue(expressions []string) string {
	values := make([]string, len(expressions))
	for index, expression := range expressions {
		values[index] = "NULLIF(" + expression + ", 'null'::jsonb)"
	}
	if len(values) == 1 {
		return values[0]
	}
	return "COALESCE(" + strings.Join(values, ", ") + ")"
}

func localizedJSONScalar(column string, segments []string, boundary int, locales []schema.LocaleCode) string {
	candidates := make([]string, 0, len(locales))
	for _, locale := range locales {
		path := append(append([]string(nil), segments[:boundary]...), string(locale))
		candidates = append(candidates, column+" #> '{"+strings.Join(path, ",")+"}'")
	}
	return coalescedLocalizedJSONScalar(candidates)
}

func coalescedLocalizedJSONScalar(expressions []string) string {
	values := make([]string, len(expressions))
	for index, expression := range expressions {
		value := "NULLIF(" + expression + ", 'null'::jsonb)"
		if index < len(expressions)-1 {
			value = "NULLIF(" + value + ", '\"\"'::jsonb)"
		}
		values[index] = value
	}
	if len(values) == 1 {
		return values[0]
	}
	return "COALESCE(" + strings.Join(values, ", ") + ")"
}

func safeJSONArray(expression string) string {
	value := "COALESCE(NULLIF(" + expression + ", 'null'::jsonb), '[]'::jsonb)"
	return "CASE WHEN jsonb_typeof(" + value + ") = 'array' THEN " + value + " ELSE '[]'::jsonb END"
}

func coalescedJSONText(column string, locales []schema.LocaleCode, segments func(schema.LocaleCode) []string, field schema.Field) string {
	values := make([]string, 0, len(locales))
	for index, locale := range locales {
		values = append(values, fallbackScalarExpression(column+" #>> '{"+strings.Join(segments(locale), ",")+"}'", field, index < len(locales)-1))
	}
	value := values[0]
	if len(values) > 1 {
		value = "COALESCE(" + strings.Join(values, ", ") + ")"
	}
	return castJSONScalar(value, field.Type, true)
}

func fallbackScalarExpression(expression string, field schema.Field, hasFallback bool) string {
	if !hasFallback {
		return expression
	}
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeDate, schema.FieldTypeSelect, schema.FieldTypeRadio:
		return "NULLIF(" + expression + ", '')"
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && !field.Relationship.HasMany && !field.Relationship.Polymorphic {
			return "NULLIF(" + expression + ", '')"
		}
	case schema.FieldTypeUpload:
		if field.Upload != nil && !field.Upload.HasMany {
			return "NULLIF(" + expression + ", '')"
		}
	default:
	}
	return expression
}

func supportsStringPredicate(field schema.Field) bool {
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeDate, schema.FieldTypeRadio, schema.FieldTypeJSON, schema.FieldTypePlugin:
		return true
	case schema.FieldTypeSelect:
		return field.Select == nil || !field.Select.HasMany
	case schema.FieldTypeRelationship:
		return field.Relationship != nil && !field.Relationship.HasMany && !field.Relationship.Polymorphic
	case schema.FieldTypeUpload:
		return field.Upload != nil && !field.Upload.HasMany
	default:
		return false
	}
}

func isJSONStoredField(field schema.Field) bool {
	switch field.Type {
	case schema.FieldTypeTextList, schema.FieldTypeNumberList, schema.FieldTypeJSON, schema.FieldTypePlugin, schema.FieldTypeGroup, schema.FieldTypeArray, schema.FieldTypeBlocks, schema.FieldTypePoint:
		return true
	case schema.FieldTypeSelect:
		return field.Select != nil && field.Select.HasMany
	case schema.FieldTypeRelationship:
		return field.Relationship != nil && (field.Relationship.HasMany || field.Relationship.Polymorphic)
	case schema.FieldTypeUpload:
		return field.Upload != nil && field.Upload.HasMany
	default:
		return false
	}
}

func scalarFieldValueKind(field schema.Field) query.ValueKind {
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeDate, schema.FieldTypeRadio:
		return query.ValueString
	case schema.FieldTypeSelect:
		if field.Select == nil || !field.Select.HasMany {
			return query.ValueString
		}
	case schema.FieldTypeNumber:
		return query.ValueNumber
	case schema.FieldTypeCheckbox:
		return query.ValueBoolean
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && !field.Relationship.HasMany && !field.Relationship.Polymorphic {
			return query.ValueString
		}
	case schema.FieldTypeUpload:
		if field.Upload != nil && !field.Upload.HasMany {
			return query.ValueString
		}
	}
	return ""
}

func compatibleScalarValues(field schema.Field, values []query.Value) []query.Value {
	kind := scalarFieldValueKind(field)
	compatible := make([]query.Value, 0, len(values))
	for _, value := range values {
		if value.Kind() == query.ValueNull || value.Kind() == kind {
			compatible = append(compatible, value)
		}
	}
	return compatible
}

func castJSONScalar(column string, fieldType schema.FieldType, extracted bool) string {
	if !extracted {
		return column
	}
	if fieldType == schema.FieldTypeNumber {
		return "(" + column + ")::double precision"
	}
	if fieldType == schema.FieldTypeCheckbox {
		return "(" + column + ")::boolean"
	}
	return column
}

func resolvePredicatePath(collection schema.Collection, path query.Path) (predicatePath, error) {
	segments := path.Segments()
	if len(segments) == 1 && segments[0] == "id" {
		field := schema.Field{ID: "id", Name: "id", Type: schema.FieldTypeText}
		return predicatePath{root: field, leaf: field}, nil
	}
	if len(segments) == 1 && (segments[0] == "createdAt" || segments[0] == "updatedAt") {
		field := schema.Field{ID: schema.StableID(segments[0]), Name: segments[0], Type: schema.FieldTypeDate}
		return predicatePath{root: field, leaf: field}, nil
	}
	if len(segments) == 1 && segments[0] == "_status" && collection.Versions != nil {
		field := schema.Field{ID: "_status", Name: "_status", Type: schema.FieldTypeText}
		return predicatePath{root: field, leaf: field}, nil
	}
	if len(segments) == 0 {
		return predicatePath{}, fmt.Errorf("predicate path is empty")
	}
	var root *schema.Field
	for index := range collection.Fields {
		if collection.Fields[index].Name == segments[0] {
			root = &collection.Fields[index]
			break
		}
	}
	if root == nil {
		return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
	}
	resolved := predicatePath{root: *root, leaf: *root}
	current := root
	for index := 1; index < len(segments); index++ {
		segment := segments[index]
		if current.Type == schema.FieldTypeJSON {
			// JSON fields intentionally have no authored child schema, but callers
			// may still address a bounded path within their opaque value. Framework
			// upload delivery relies on this for generated image-size object keys.
			resolved.jsonSegments = append(resolved.jsonSegments, segments[index:]...)
			resolved.leaf = schema.Field{ID: current.ID, Name: segments[len(segments)-1], Type: schema.FieldTypeJSON}
			return resolved, nil
		}
		if current.Type == schema.FieldTypeBlocks {
			if current.Blocks == nil || resolved.many {
				return predicatePath{}, fmt.Errorf("predicate path %q traverses unsupported repeated fields", path.String())
			}
			resolved.many = true
			resolved.blockType = segment
			if len(resolved.jsonSegments) > 0 {
				resolved.arrayPath = append([]string(nil), resolved.jsonSegments...)
				resolved.arrayLocalizedAfter = append([]int(nil), resolved.jsonLocalizedAfter...)
			}
			var block *schema.BlockType
			for blockIndex := range current.Blocks.ResolvedTypes() {
				if current.Blocks.ResolvedTypes()[blockIndex].Slug == segment {
					block = &current.Blocks.ResolvedTypes()[blockIndex]
					break
				}
			}
			if block == nil || index+1 >= len(segments) {
				return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
			}
			index++
			segment = segments[index]
			current = fieldNamed(block.ResolvedFields(), segment)
		} else {
			if current.Type == schema.FieldTypeArray {
				if resolved.many {
					return predicatePath{}, fmt.Errorf("predicate path %q traverses unsupported repeated fields", path.String())
				}
				resolved.many = true
				if len(resolved.jsonSegments) > 0 {
					resolved.arrayPath = append([]string(nil), resolved.jsonSegments...)
					resolved.arrayLocalizedAfter = append([]int(nil), resolved.jsonLocalizedAfter...)
				}
			}
			if current.Nested == nil {
				return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
			}
			current = fieldNamed(current.Nested.ResolvedFields(), segment)
		}
		if current == nil {
			return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
		}
		resolved.leaf = *current
		if resolved.many {
			resolved.rowPath = append(resolved.rowPath, segment)
			if current.Localized {
				resolved.rowLocalizedAfter = append(resolved.rowLocalizedAfter, len(resolved.rowPath))
			}
		} else {
			resolved.jsonSegments = append(resolved.jsonSegments, segment)
			if current.Localized {
				resolved.jsonLocalizedAfter = append(resolved.jsonLocalizedAfter, len(resolved.jsonSegments))
			}
		}
	}
	return resolved, nil
}

func fieldNamed(fields []schema.Field, name string) *schema.Field {
	for index := range fields {
		if fields[index].Name == name {
			return &fields[index]
		}
	}
	return nil
}

func (compiler *predicateCompiler) operand(value query.Value) (string, error) {
	var argument any
	switch value.Kind() {
	case query.ValueString:
		argument, _ = value.StringValue()
	case query.ValueNumber:
		argument, _ = value.NumberValue()
	case query.ValueBoolean:
		argument, _ = value.BooleanValue()
	case query.ValueNull:
		argument = nil
	default:
		return "", fmt.Errorf("PostgreSQL predicates support scalar and null operands")
	}
	compiler.arguments = append(compiler.arguments, argument)
	compiler.next++
	return fmt.Sprintf("$%d", compiler.next), nil
}

func (compiler *predicateCompiler) timestampOperand(value query.Value) (string, error) {
	text, ok := value.StringValue()
	if !ok {
		return "", fmt.Errorf("PostgreSQL timestamp predicates require RFC3339 string operands")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return "", fmt.Errorf("parse PostgreSQL timestamp predicate: %w", err)
	}
	compiler.arguments = append(compiler.arguments, timestamp)
	compiler.next++
	return fmt.Sprintf("$%d", compiler.next), nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func selectColumns(collection schema.Collection, fields []schema.Field, locales []schema.LocaleCode) string {
	columns := []string{quote("id"), quote("created_at"), quote("updated_at"), quote("deleted_at")}
	if collection.Versions != nil {
		columns = append(columns, quote("_status"), quote("_revision"))
	}
	for _, field := range fields {
		if !field.Localized {
			columns = append(columns, quote(fieldColumn(field.ID)))
			continue
		}
		pairs := make([]string, 0, len(locales)*2)
		for _, locale := range locales {
			pairs = append(pairs, "'"+strings.ReplaceAll(string(locale), "'", "''")+"'", quote(localizedFieldColumn(field.ID, locale)))
		}
		columns = append(columns, "jsonb_strip_nulls(jsonb_build_object("+strings.Join(pairs, ", ")+")) AS "+quote(fieldColumn(field.ID)))
	}
	return strings.Join(columns, ", ")
}

func storedSchemaFields(fields []schema.Field) []schema.Field {
	stored := make([]schema.Field, 0, len(fields))
	for _, field := range fields {
		if field.Category != schema.FieldCategoryPresentation {
			stored = append(stored, field)
		}
	}
	return stored
}

func databaseValue(field schema.Field, value store.Value) (any, error) {
	if isJSONStoredField(field) {
		if value.Kind() == store.ValueNull {
			return nil, nil
		}
		return json.Marshal(value)
	}
	switch value.Kind() {
	case store.ValueNull:
		return nil, nil
	case store.ValueString:
		text, _ := value.StringValue()
		return text, nil
	case store.ValueNumber:
		number, _ := value.NumberValue()
		return number, nil
	case store.ValueBoolean:
		boolean, _ := value.BooleanValue()
		return boolean, nil
	default:
		return nil, fmt.Errorf("unsupported store value kind %q", value.Kind())
	}
}

func collectionTable(id schema.StableID) string { return "z_c_" + identifierHash(string(id)) }
func fieldColumn(id schema.StableID) string     { return "f_" + identifierHash(string(id)) }
func localizedFieldColumn(id schema.StableID, locale schema.LocaleCode) string {
	return "f_" + identifierHash(string(id)+"\x00"+string(locale))
}

func identifierHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func quote(identifier string) string { return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"` }

func newID(prefix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate document ID: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(random[:]), nil
}

func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", // unique_violation
			"40001", // serialization_failure
			"40P01": // deadlock_detected
			return store.ErrConflict
		}
	}
	return err
}

var _ store.Store = (*Store)(nil)
var _ store.ReadinessStore = (*Store)(nil)
var _ store.MigrationReadinessStore = (*Store)(nil)
var _ store.Transaction = (*documentTransaction)(nil)
var _ store.DistinctTransaction = (*documentTransaction)(nil)
var _ store.AuthTransaction = (*documentTransaction)(nil)
var _ store.AuthUnlockTransaction = (*documentTransaction)(nil)
