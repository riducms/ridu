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
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/internal/localnet"
	"github.com/riducms/ridu/internal/membership"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/querypath"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type Store struct {
	pool           *pgxpool.Pool
	uploadLockPool *pgxpool.Pool
	uploadLockWait time.Duration
	readiness      readinessCache
}

// PoolConfig defines bounded production connection and PostgreSQL session
// defaults. Zero values select Ridu's defaults; negative duration values
// explicitly disable the corresponding timeout.
type PoolConfig struct {
	DatabaseURL string
	// AllowInsecureTransport explicitly permits plaintext PostgreSQL
	// connections to a remote host. Loopback and Unix-socket databases never
	// need it. Keep this false in production; it exists for development
	// databases whose transport is secured outside PostgreSQL.
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
		return nil, fmt.Errorf("PostgreSQL transport to a remote host must require TLS; configure sslmode=require or stronger. Loopback and Unix-socket databases need no TLS. For another plaintext database secured outside PostgreSQL, allow it explicitly: set AllowInsecureTransport, RIDU_ALLOW_INSECURE_DATABASE=true for Ridu projects, or pass --allow-insecure-database to the CLI")
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

// securePostgresTransport requires TLS on every endpoint, including sslmode
// fallbacks, that leaves this machine. Loopback and Unix-socket endpoints may
// be plaintext: nothing off the host can observe them.
func securePostgresTransport(configured *pgxpool.Config) bool {
	if configured == nil || configured.ConnConfig == nil {
		return false
	}
	primary := &pgconn.FallbackConfig{Host: configured.ConnConfig.Host, TLSConfig: configured.ConnConfig.TLSConfig}
	for _, endpoint := range append([]*pgconn.FallbackConfig{primary}, configured.ConnConfig.Fallbacks...) {
		if endpoint == nil || (endpoint.TLSConfig == nil && !localnet.Host(endpoint.Host)) {
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

func (backend *Store) Begin(ctx context.Context) (store.Transaction, error) {
	transaction, err := beginPipelined(ctx, backend.pool, beginReadCommitted)
	if err != nil {
		return nil, err
	}
	return &documentTransaction{transaction: transaction, tableExists: make(map[string]bool)}, nil
}

func (backend *Store) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := beginPipelined(ctx, backend.pool, beginRepeatableReadOnly)
	if err != nil {
		return nil, err
	}
	return &documentTransaction{transaction: transaction, tableExists: make(map[string]bool)}, nil
}

type documentTransaction struct {
	transaction documentConnection
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
	fields := storedSchemaFields(request.Collection)
	values := store.CloneValues(request.Values)
	canonicalizePostgresAuthIdentity(request.Collection, values)
	if err := transaction.lockUniqueUnion(ctx, request.Collection, request.Locales, values); err != nil {
		return store.Document{}, err
	}
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
		columns = append(columns, quote("_status"))
		arguments = append(arguments, status)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)))
	}
	if request.Collection.Versions != nil || request.Collection.Upload != nil {
		columns = append(columns, quote("_revision"))
		arguments = append(arguments, 1)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(arguments)))
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
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields, request.Locales)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	// The insert claimed this ID, and every owner removal deletes its index
	// rows, so the new row has none yet.
	entries, err := referenceindex.Collect(request.Collection, document)
	if err != nil {
		return store.Document{}, err
	}
	if err := transaction.insertDocumentReferences(ctx, store.DocumentReference{CollectionID: request.Collection.ID, DocumentID: document.ID}, entries, false); err != nil {
		return store.Document{}, err
	}
	if request.Collection.Versions != nil && document.Status == store.StatusPublished {
		if err := transaction.putPublishedHead(ctx, request.Collection, document, request.Locales); err != nil {
			return store.Document{}, err
		}
		if request.Collection.Versions.Drafts {
			document.PublishedRevision = document.Revision
		}
	}
	if err := transaction.checkUniqueUnion(ctx, request.Collection, request.Locales, document); err != nil {
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
	lock := ""
	switch request.Lock {
	case store.LockNone:
	case store.LockReference:
		lock = " FOR SHARE"
	case store.LockMutation:
		lock = " FOR UPDATE"
	default:
		return store.Document{}, fmt.Errorf("unsupported document lock mode %q", request.Lock)
	}
	var document store.Document
	if lock != "" && readsPublishedMetadata(request) {
		document, err = transaction.findLockedWorking(ctx, request, fields, predicate, lock, arguments)
	} else {
		statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", readColumns(request, fields), readSource(request), predicate, lock)
		document, err = scanRequestedDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request, fields)
		err = translateError(err)
	}
	if err != nil {
		return store.Document{}, err
	}
	documents, err := transaction.populate(ctx, []store.Document{document}, request)
	if err != nil {
		return store.Document{}, err
	}
	return projectDocument(documents[0], request.Select), nil
}

// listStatements compiles a list request's count statement and its page
// statement. The page statement's final two parameters are LIMIT and OFFSET,
// following the shared predicate arguments. A SkipTotal request has no count
// statement.
func listStatements(request store.Request) (count, page string, arguments []any, fields []schema.Field, err error) {
	predicate, arguments, err := requestPredicate(request, false)
	if err != nil {
		return "", "", nil, nil, err
	}
	order, err := sortClause(request)
	if err != nil {
		return "", "", nil, nil, err
	}
	fields = fieldsForRead(request)
	if !request.SkipTotal {
		count = fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", readSource(request), predicate)
	}
	page = fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d",
		readColumns(request, fields), readSource(request), predicate, order, len(arguments)+1, len(arguments)+2)
	return count, page, arguments, fields, nil
}

func (transaction *documentTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	countStatement, statement, arguments, fields, err := listStatements(request)
	if err != nil {
		return store.Page{}, err
	}
	// The page window does not depend on the total: a page past the last row
	// reads no rows, exactly as the total-clamped bounds do. The count and the
	// page therefore travel in one pipeline and, in a snapshot, share its view.
	// Without a total, one extra row proves whether a next page exists.
	pageNumber, limit, offset := store.UncountedPageBounds(request.Page, request.Limit)
	var page store.Page
	var documents []store.Document
	if request.SkipTotal {
		rows, queryError := transaction.transaction.Query(ctx, statement, append(slices.Clip(arguments), limit+1, offset)...)
		if queryError != nil {
			return store.Page{}, translateError(queryError)
		}
		documents, err = scanListRows(rows, request, fields)
		if err != nil {
			return store.Page{}, err
		}
		page = store.Page{Page: pageNumber, Limit: limit}
		documents, page.HasNextPage = store.TrimUncountedPage(documents, limit)
	} else {
		batch := &pgx.Batch{}
		batch.Queue(countStatement, arguments...)
		batch.Queue(statement, append(slices.Clip(arguments), limit, offset)...)
		results := transaction.transaction.SendBatch(ctx, batch)
		var total int
		documents, total, err = scanListResults(results, request, fields)
		if closeErr := results.Close(); err == nil && closeErr != nil {
			err = translateError(closeErr)
		}
		if err != nil {
			return store.Page{}, err
		}
		page = store.CountedPage(nil, request.Page, request.Limit, total)
	}
	documents, err = transaction.populate(ctx, documents, request)
	if err != nil {
		return store.Page{}, err
	}
	for index := range documents {
		documents[index] = projectDocument(documents[index], request.Select)
	}
	page.Documents = documents
	return page, nil
}

// scanListResults reads a list pipeline's count and page results.
func scanListResults(results pgx.BatchResults, request store.Request, fields []schema.Field) ([]store.Document, int, error) {
	var total int
	if err := results.QueryRow().Scan(&total); err != nil {
		return nil, 0, translateError(err)
	}
	rows, err := results.Query()
	if err != nil {
		return nil, 0, translateError(err)
	}
	documents, err := scanListRows(rows, request, fields)
	if err != nil {
		return nil, 0, err
	}
	return documents, total, nil
}

// scanListRows reads and closes one list page's rows.
func scanListRows(rows pgx.Rows, request store.Request, fields []schema.Field) ([]store.Document, error) {
	defer rows.Close()
	var documents []store.Document
	for rows.Next() {
		document, err := scanRequestedDocument(rows, request, fields)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, translateError(err)
	}
	return documents, nil
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
	distinctQuery := fmt.Sprintf("SELECT DISTINCT %s AS distinct_value FROM %s WHERE %s", column, readSource(documentRequest), predicate)
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
		document, scanError := scanRequestedDocument(rows, request, fields)
		if scanError != nil {
			return store.Window{}, scanError
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return store.Window{}, err
	}
	rows.Close()
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
		readColumns(request, fields), readSource(request), predicate, column, limitParameter)
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
		return storedSchemaFields(request.Collection)
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
		terms = append(terms, query.Asc("id"))
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
			targetFields := storedSchemaFields(target)
			publishedOnly := request.PublishedOnly
			if selected, exists := request.PopulationPublishedOnly[target.ID]; exists {
				publishedOnly = selected
			}
			targetRequest := store.Request{Collection: target, PublishedOnly: publishedOnly, Locales: request.Locales}
			if population.Select != nil && population.Depth <= 1 {
				targetFields = fieldsForRead(store.Request{Collection: target, Select: population.Select})
			}
			predicate := fmt.Sprintf("%s = ANY($1) AND %s IS NULL", quote("id"), quote("deleted_at"))
			arguments := []any{ids}
			if access := request.PopulationAccess[target.ID]; access != nil {
				compiler := predicateCompiler{collection: target, next: 1, arguments: arguments, localeChain: request.LocaleChain}
				compiled, compileError := compileAccessPredicate(&compiler, *access, request.AllLocales, request.Locales)
				if compileError != nil {
					return nil, compileError
				}
				predicate += " AND (" + compiled + ")"
				arguments = compiler.arguments
			}
			statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s", readColumns(targetRequest, targetFields), readSource(targetRequest), predicate)
			rows, err := transaction.transaction.Query(ctx, statement, arguments...)
			if err != nil {
				return nil, translateError(err)
			}
			byID := make(map[string]store.Document, len(ids))
			var targetDocuments []store.Document
			for rows.Next() {
				document, err := scanRequestedDocument(rows, targetRequest, targetFields)
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
					PopulationBudget: populationBudget, PublishedOnly: publishedOnly, PopulationPublishedOnly: request.PopulationPublishedOnly,
					Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
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
	if request.Collection.Versions != nil && !request.Collection.Versions.Drafts &&
		(request.Intent == store.WriteIntentSaveDraft || request.Intent == store.WriteIntentDiscardDraft) {
		return store.Document{}, fmt.Errorf("draft write intent requires a draft-enabled collection")
	}
	if request.Collection.Versions != nil && request.Collection.Versions.Drafts && request.Intent == store.WriteIntentDefault {
		return store.Document{}, fmt.Errorf("write intent is required for a draft-enabled collection")
	}
	current, err := request.LockedCurrent()
	if err != nil {
		return store.Document{}, err
	}
	if request.ExpectedRevision > 0 && current.Revision != request.ExpectedRevision {
		return store.Document{}, store.ErrConflict
	}
	hasLive := store.HasLiveHead(request.Collection, current)
	liveRevision := current.PublishedRevision
	if request.Intent == store.WriteIntentDiscardDraft {
		// Only a discard needs the live content; every other intent derives
		// the live state from the locked working read.
		live, found, err := transaction.publishedHead(ctx, request.Collection, current.ID, request.Locales)
		if err != nil {
			return store.Document{}, err
		}
		if !found || !live.HasDraftChanges {
			return store.Document{}, store.ErrConflict
		}
		request.Values = store.CloneValues(live.Values)
		request.ReplaceValues = true
		liveRevision = live.Revision
	}
	assignments := []string{quote("updated_at") + " = " + nextUpdatedAt}
	values := store.CloneValues(request.Values)
	canonicalizePostgresAuthIdentity(request.Collection, values)
	candidateValues := store.CloneValues(current.Values)
	arguments := make([]any, 0, len(values)+1)
	fields := storedSchemaFields(request.Collection)
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
			candidateLocalized, _ := candidateValues[field.Name].CopyObject()
			if candidateLocalized == nil {
				candidateLocalized = make(store.Values)
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
				candidateLocalized[string(locale)] = localizedValue
			}
			candidateValues[field.Name] = store.Object(candidateLocalized)
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
		candidateValues[field.Name] = value
	}
	if request.Collection.Versions != nil || request.Collection.Upload != nil {
		assignments = append(assignments, quote("_revision")+" = "+quote("_revision")+" + 1")
	}
	if request.Collection.Versions != nil {
		var status *store.Status
		switch request.Intent {
		case store.WriteIntentDefault:
		case store.WriteIntentSaveDraft:
			next := store.StatusDraft
			if hasLive {
				next = store.StatusPublished
			}
			status = &next
		case store.WriteIntentPublish:
			next := store.StatusPublished
			status = &next
		case store.WriteIntentUnpublish:
			if !hasLive {
				return store.Document{}, store.ErrConflict
			}
			next := store.StatusDraft
			status = &next
		case store.WriteIntentDiscardDraft:
			next := store.StatusPublished
			status = &next
		default:
			return store.Document{}, fmt.Errorf("unsupported write intent %q", request.Intent)
		}
		if status != nil {
			arguments = append(arguments, *status)
			assignments = append(assignments, fmt.Sprintf("%s = $%d", quote("_status"), len(arguments)))
		}
	} else if request.Intent != store.WriteIntentDefault {
		return store.Document{}, fmt.Errorf("write intent requires a versioned collection")
	}
	predicate, predicateArguments, err := requestPredicateFrom(request.Request, true, len(arguments))
	if err != nil {
		return store.Document{}, err
	}
	arguments = append(arguments, predicateArguments...)
	// The row must still be the stored version Current describes. Every write
	// advances updated_at, and revision when the table has one, so a stale or
	// caller-built Current matches no row and becomes a conflict instead of a
	// write derived from the wrong state.
	arguments = append(arguments, current.UpdatedAt)
	predicate += fmt.Sprintf(" AND %s = $%d", quote("updated_at"), len(arguments))
	if request.Collection.Versions != nil || request.Collection.Upload != nil {
		arguments = append(arguments, current.Revision)
		predicate += fmt.Sprintf(" AND %s = $%d", quote("_revision"), len(arguments))
	}
	if err := transaction.lockUniqueUnion(ctx, request.Collection, request.Locales, candidateValues); err != nil {
		return store.Document{}, err
	}
	statement := fmt.Sprintf("UPDATE %s SET %s WHERE %s RETURNING %s", quote(collectionTable(request.Collection.ID)), strings.Join(assignments, ", "), predicate, selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields, request.Locales)
	if errors.Is(err, pgx.ErrNoRows) {
		// A visible row that matched nothing changed after Current was read or
		// has another revision than the expected one.
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
	if err := transaction.updateDocumentReferences(ctx, request.Collection, current, document); err != nil {
		return store.Document{}, err
	}
	if request.Collection.Versions != nil {
		// liveRevision becomes the resulting live revision; zero means no live head.
		pending := false
		if !hasLive {
			liveRevision = 0
		}
		switch request.Intent {
		case store.WriteIntentDefault:
			if document.Status == store.StatusPublished {
				if err := transaction.putPublishedHead(ctx, request.Collection, document, request.Locales); err != nil {
					return store.Document{}, err
				}
				liveRevision = document.Revision
			} else {
				if err := transaction.deletePublishedHead(ctx, request.Collection, document.ID); err != nil {
					return store.Document{}, err
				}
				liveRevision = 0
			}
		case store.WriteIntentSaveDraft:
			if hasLive {
				if err := transaction.setPublishedPending(ctx, request.Collection, document.ID, true); err != nil {
					return store.Document{}, err
				}
				pending = true
			}
		case store.WriteIntentPublish:
			if err := transaction.putPublishedHead(ctx, request.Collection, document, request.Locales); err != nil {
				return store.Document{}, err
			}
			liveRevision = document.Revision
		case store.WriteIntentUnpublish:
			if err := transaction.deletePublishedHead(ctx, request.Collection, document.ID); err != nil {
				return store.Document{}, err
			}
			liveRevision = 0
		case store.WriteIntentDiscardDraft:
			if err := transaction.setPublishedPending(ctx, request.Collection, document.ID, false); err != nil {
				return store.Document{}, err
			}
		}
		if request.Collection.Versions.Drafts {
			document.PublishedRevision, document.HasDraftChanges = liveRevision, pending
		}
	}
	if err := transaction.checkUniqueUnion(ctx, request.Collection, request.Locales, document); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

// nextUpdatedAt advances updated_at on every document write. now() is the
// transaction's start time, so a later write in the same transaction moves the
// value forward by a microsecond instead: updated_at strictly increases with
// each write and, with revision, identifies the stored version of a row.
const nextUpdatedAt = `GREATEST(now(), "updated_at" + interval '1 microsecond')`

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
	fields := storedSchemaFields(request.Collection)
	// Deleting the working row deletes its live row through the foreign key;
	// its working and live index rows leave in the same statement.
	arguments = append(arguments, string(request.Collection.ID))
	statement := fmt.Sprintf(`WITH removed AS (DELETE FROM %s WHERE %s RETURNING %s), unindexed AS (
DELETE FROM ridu_document_references AS reference USING removed
WHERE reference.owner_collection_id = $%d AND reference.owner_document_id = removed.id)
SELECT * FROM removed`, quote(collectionTable(request.Collection.ID)), predicate, selectColumns(request.Collection, fields, request.Locales), len(arguments))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields, request.Locales)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	return document, nil
}

type referenceDeleteMatch struct {
	entry      referenceindex.Entry
	published  bool
	field      schema.Field
	root       schema.Field
	collection schema.Collection
}

func (transaction *documentTransaction) ApplyReferenceDelete(ctx context.Context, request store.ReferenceDeleteRequest) error {
	if request.Target.CollectionID == "" || request.Target.DocumentID == "" {
		return fmt.Errorf("reference delete requires a target collection and document ID")
	}
	// An ignored owner is skipped once it no longer exists. Removing a row
	// removes its index rows in the same statement, so a removed owner has no
	// rows to skip; only the target's references to itself are ignored here.
	ignored := make(map[store.DocumentReference]struct{}, 1)
	for _, owner := range request.IgnoreOwners {
		if owner == request.Target {
			ignored[owner] = struct{}{}
			continue
		}
		if _, exists := request.Collections[owner.CollectionID]; !exists {
			return fmt.Errorf("ignored reference owner collection %q is unavailable", owner.CollectionID)
		}
	}
	rows, err := transaction.transaction.Query(ctx, `SELECT
  owner_collection_id, owner_document_id, field_id, locale, occurrence, published_head
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
		var published bool
		if err := rows.Scan(&entry.Owner.CollectionID, &entry.Owner.DocumentID, &entry.FieldID, &entry.Locale, &entry.Occurrence, &published); err != nil {
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
		matches = append(matches, referenceDeleteMatch{entry: entry, published: published, field: field, root: root, collection: collection})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	rows.Close()

	pending := referenceindex.PendingSet(request.PendingOwners)
	constraintSet := make(map[store.ReferenceConstraint]struct{})
	for _, match := range matches {
		blocks, err := referenceindex.BlocksDelete(referenceindex.DeleteAction(match.field), match.entry.Owner, pending)
		if err != nil {
			return fmt.Errorf("reference owner field %q: %w", match.entry.FieldID, err)
		}
		if blocks {
			constraintSet[store.ReferenceConstraint{OwnerCollectionID: match.entry.Owner.CollectionID, FieldID: match.entry.FieldID}] = struct{}{}
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

	// Working and live heads share one physical layout. A live owner's
	// nullified reference is the same typed column update on its live table.
	type rootMutation struct {
		owner      store.DocumentReference
		collection schema.Collection
		root       schema.Field
		locale     schema.LocaleCode
		published  bool
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
		key := fmt.Sprintf("%t\x00%s\x00%s\x00%s\x00%s", match.published, match.entry.Owner.CollectionID, match.entry.Owner.DocumentID, match.root.ID, locale)
		mutations[key] = rootMutation{owner: match.entry.Owner, collection: match.collection, root: match.root, locale: locale, published: match.published}
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
		table := collectionTable(mutation.owner.CollectionID)
		if mutation.published {
			table = publishedCollectionTable(mutation.owner.CollectionID)
		}
		statement := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1 FOR UPDATE", quote(column), quote(table), quote("id"))
		value, found, err := scanReferenceRootValue(transaction.transaction.QueryRow(ctx, statement, mutation.owner.DocumentID), mutation.root)
		if err != nil {
			return err
		}
		if !found {
			// Every path that removes a working or live row removes its index
			// rows in the same transaction, so an index row always has its
			// owner row. One without it is corruption, not a state to repair.
			head := "working"
			if mutation.published {
				head = "live"
			}
			return fmt.Errorf("reference index row for %s/%s has no stored %s owner row", mutation.owner.CollectionID, mutation.owner.DocumentID, head)
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
		update := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2", quote(table), quote(column), quote("id"))
		if _, err := transaction.transaction.Exec(ctx, update, encoded, mutation.owner.DocumentID); err != nil {
			return translateError(err)
		}
		if err := transaction.replaceReferenceRootEntries(ctx, mutation.owner, mutation.root, updated, mutation.locale, mutation.published); err != nil {
			return err
		}
	}
	return nil
}

// CascadeOwners lists current owners that reference the target through a
// cascade field, locking their index rows like ApplyReferenceDelete.
func (transaction *documentTransaction) CascadeOwners(ctx context.Context, request store.ReferenceDeleteRequest) ([]store.DocumentReference, error) {
	if request.Target.CollectionID == "" || request.Target.DocumentID == "" {
		return nil, fmt.Errorf("cascade lookup requires a target collection and document ID")
	}
	rows, err := transaction.transaction.Query(ctx, `SELECT
  owner_collection_id, owner_document_id, field_id, locale, occurrence
FROM ridu_document_references
WHERE target_collection_id = $1 AND target_document_id = $2
ORDER BY owner_collection_id, owner_document_id, field_id, locale, occurrence
FOR UPDATE`, string(request.Target.CollectionID), request.Target.DocumentID)
	if err != nil {
		return nil, translateError(err)
	}
	var entries []referenceindex.Entry
	for rows.Next() {
		entry := referenceindex.Entry{Target: request.Target}
		if err := rows.Scan(&entry.Owner.CollectionID, &entry.Owner.DocumentID, &entry.FieldID, &entry.Locale, &entry.Occurrence); err != nil {
			rows.Close()
			return nil, translateError(err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, translateError(err)
	}
	rows.Close()
	return referenceindex.CascadeOwners(entries, request.Collections, referenceindex.PendingSet(request.PendingOwners))
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
		if err := value.UnmarshalJSON(encoded); err != nil {
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

func (transaction *documentTransaction) replaceReferenceRootEntries(ctx context.Context, owner store.DocumentReference, root schema.Field, value store.Value, locale schema.LocaleCode, published bool) error {
	fieldIDs := referenceindex.ReferenceFieldIDs(owner.CollectionID, root)
	if len(fieldIDs) == 0 {
		return nil
	}
	ids := make([]string, len(fieldIDs))
	for index, id := range fieldIDs {
		ids[index] = string(id)
	}
	statement := `DELETE FROM ridu_document_references
WHERE owner_collection_id = $1 AND owner_document_id = $2 AND field_id = ANY($3) AND published_head = $4`
	arguments := []any{string(owner.CollectionID), owner.DocumentID, ids, published}
	if root.Localized {
		statement += " AND locale = $5"
		arguments = append(arguments, string(locale))
	}
	if _, err := transaction.transaction.Exec(ctx, statement, arguments...); err != nil {
		return translateError(err)
	}
	entries, referenceErr := referenceindex.CollectField(owner, root, value, locale)
	if referenceErr != nil {
		return referenceErr
	}
	return transaction.insertDocumentReferences(ctx, owner, entries, published)
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

// The reference index is derived state. Every adapter write of a document
// row maintains its rows in the same transaction: Create inserts them, Update
// and ApplyReferenceDelete replace them when references change, Delete removes
// them with the owner, publication replaces the live head's rows, and
// unpublication removes them with the live row. Trash and restore change no
// values. Resource retirement removes every row owned by or targeting the
// retired resources before its tables are dropped. Migrations that change
// reference topology rebuild the whole index (StepBackfillReferences), as does
// development synchronization, and data transforms write through these same
// methods. The index therefore always equals Collect of the stored rows: an
// update whose references are unchanged leaves it untouched, Create needs no
// replacement, and an index row without its owner row is corruption.

// replaceDocumentReferences makes the working row's index rows exactly the
// document's references.
func (transaction *documentTransaction) replaceDocumentReferences(ctx context.Context, collection schema.Collection, document store.Document) error {
	entries, err := referenceindex.Collect(collection, document)
	if err != nil {
		return err
	}
	return transaction.replaceReferenceSet(ctx, store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}, entries, false)
}

// updateDocumentReferences maintains the working row's index after an update
// from current, the locked row before the write.
func (transaction *documentTransaction) updateDocumentReferences(ctx context.Context, collection schema.Collection, current, document store.Document) error {
	before, err := referenceindex.Collect(collection, current)
	if err != nil {
		return err
	}
	after, err := referenceindex.Collect(collection, document)
	if err != nil {
		return err
	}
	if slices.Equal(before, after) {
		return nil
	}
	return transaction.replaceReferenceSet(ctx, store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}, after, false)
}

// insertDocumentReferences indexes rows that have no index rows yet, such as
// a row this transaction just inserted.
func (transaction *documentTransaction) insertDocumentReferences(ctx context.Context, owner store.DocumentReference, entries []referenceindex.Entry, published bool) error {
	if len(entries) == 0 {
		return nil
	}
	arguments, err := referenceSetArguments(owner, entries, published)
	if err != nil {
		return err
	}
	_, err = transaction.transaction.Exec(ctx, `INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id,
  target_collection_id, target_document_id, locale, occurrence, published_head
) SELECT $1, $2, entry.field_id, entry.target_collection_id, entry.target_document_id, entry.locale, entry.occurrence, $8
FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::integer[])
  AS entry(field_id, target_collection_id, target_document_id, locale, occurrence)`, arguments...)
	return translateError(err)
}

// replaceReferenceSet makes one owner's working or live index rows exactly
// entries in one statement: it deletes rows outside the set and inserts the
// missing ones. The deleted and inserted rows are disjoint, and the caller
// holds the owner's row lock, so no other statement changes these rows.
func (transaction *documentTransaction) replaceReferenceSet(ctx context.Context, owner store.DocumentReference, entries []referenceindex.Entry, published bool) error {
	arguments, err := referenceSetArguments(owner, entries, published)
	if err != nil {
		return err
	}
	_, err = transaction.transaction.Exec(ctx, `WITH entry AS (
  SELECT * FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::integer[])
    AS entry(field_id, target_collection_id, target_document_id, locale, occurrence)
), removed AS (
  DELETE FROM ridu_document_references AS reference
  WHERE reference.owner_collection_id = $1 AND reference.owner_document_id = $2 AND reference.published_head = $8
    AND NOT EXISTS (SELECT 1 FROM entry WHERE entry.field_id = reference.field_id
      AND entry.target_collection_id = reference.target_collection_id
      AND entry.target_document_id = reference.target_document_id
      AND entry.locale = reference.locale AND entry.occurrence = reference.occurrence)
)
INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id,
  target_collection_id, target_document_id, locale, occurrence, published_head
) SELECT $1, $2, field_id, target_collection_id, target_document_id, locale, occurrence, $8 FROM entry
ON CONFLICT DO NOTHING`, arguments...)
	return translateError(err)
}

// referenceSetArguments transposes one owner's entries into the parameters of
// the set statements: owner, five column arrays, and the head flag.
func referenceSetArguments(owner store.DocumentReference, entries []referenceindex.Entry, published bool) ([]any, error) {
	fields := make([]string, len(entries))
	targetCollections := make([]string, len(entries))
	targetDocuments := make([]string, len(entries))
	locales := make([]string, len(entries))
	occurrences := make([]int32, len(entries))
	for index, entry := range entries {
		if entry.Owner != owner {
			return nil, fmt.Errorf("reference index entry owner %s/%s does not match %s/%s", entry.Owner.CollectionID, entry.Owner.DocumentID, owner.CollectionID, owner.DocumentID)
		}
		if entry.Occurrence < 0 || entry.Occurrence > math.MaxInt32 {
			return nil, fmt.Errorf("reference index occurrence %d is out of range", entry.Occurrence)
		}
		fields[index] = string(entry.FieldID)
		targetCollections[index] = string(entry.Target.CollectionID)
		targetDocuments[index] = entry.Target.DocumentID
		locales[index] = string(entry.Locale)
		occurrences[index] = int32(entry.Occurrence)
	}
	return []any{string(owner.CollectionID), owner.DocumentID, fields, targetCollections, targetDocuments, locales, occurrences, published}, nil
}

func (transaction *documentTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	request.Deletion = store.DeletionActive
	predicate, arguments, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	fields := storedSchemaFields(request.Collection)
	table := quote(collectionTable(request.Collection.ID))
	statement := fmt.Sprintf("UPDATE %s SET %s = now(), %s = %s WHERE %s RETURNING %s", table, quote("deleted_at"), quote("updated_at"), nextUpdatedAt,
		predicate, selectColumns(request.Collection, fields, request.Locales))
	if request.Collection.Versions == nil {
		document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields, request.Locales)
		return document, translateError(err)
	}
	// Mirror trash state into the live row, so live reads and live uniqueness
	// observe it without consulting the working row. The mirror is a second
	// statement in the same pipeline: if trash waited for the working row lock,
	// only a statement that starts afterwards sees a live row committed
	// during the wait. It copies the working row's state, so it is idempotent
	// when trash matched nothing, and returns the live state trash keeps.
	batch := &pgx.Batch{}
	batch.Queue(statement, arguments...)
	batch.Queue(fmt.Sprintf(`UPDATE %s AS live SET deleted_at = working.deleted_at FROM %s AS working
WHERE live.id = $1 AND working.id = live.id RETURNING live._revision, live.has_draft_changes`, quote(publishedCollectionTable(request.Collection.ID)), table), request.ID)
	results := transaction.transaction.SendBatch(ctx, batch)
	document, err := scanDocument(results.QueryRow(), request.Collection, fields, request.Locales)
	if err == nil {
		err = scanLiveState(results.QueryRow(), &document)
	}
	if closeErr := results.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return store.Document{}, translateError(err)
	}
	if !readsPublishedMetadata(request) {
		document.PublishedRevision, document.HasDraftChanges = 0, false
	}
	return document, nil
}

func (transaction *documentTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if err := transaction.lockUniqueUnion(ctx, request.Collection, request.Locales); err != nil {
		return store.Document{}, err
	}
	request.Deletion = store.DeletionTrash
	predicate, arguments, err := requestPredicate(request, true)
	if err != nil {
		return store.Document{}, err
	}
	fields := storedSchemaFields(request.Collection)
	statement := fmt.Sprintf("UPDATE %s SET %s = NULL, %s = %s WHERE %s RETURNING %s", quote(collectionTable(request.Collection.ID)), quote("deleted_at"), quote("updated_at"), nextUpdatedAt, predicate, selectColumns(request.Collection, fields, request.Locales))
	document, err := scanDocument(transaction.transaction.QueryRow(ctx, statement, arguments...), request.Collection, fields, request.Locales)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	candidates := []store.Document{document}
	if live, found, err := transaction.restorePublishedHead(ctx, request.Collection, document.ID, request.Locales); err != nil {
		return store.Document{}, err
	} else if found {
		candidates = append(candidates, live)
		if readsPublishedMetadata(request) {
			candidates[0].PublishedRevision, candidates[0].HasDraftChanges = live.Revision, live.HasDraftChanges
			document = candidates[0]
		}
	}
	if err := transaction.checkUniqueUnion(ctx, request.Collection, request.Locales, candidates...); err != nil {
		return store.Document{}, err
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
	batch := &pgx.Batch{}
	if document.Revision == 1 {
		// Revision 1 starts a document's history: any later revision belongs
		// to an earlier document with the same ID, so discard it in the same
		// round trip. Revision 1 itself is replaced below, keeping a re-save's
		// created_at.
		batch.Queue(`DELETE FROM ridu_versions WHERE collection_id = $1 AND document_id = $2 AND revision > 1`, collection.ID, document.ID)
	}
	batch.Queue(statement, collection.ID, document.ID, document.Revision, document.Status, encoded)
	results := transaction.transaction.SendBatch(ctx, batch)
	if document.Revision == 1 {
		_, err = results.Exec()
	}
	if err == nil {
		err = results.QueryRow().Scan(&version.CreatedAt)
	}
	if closeErr := results.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return store.Version{}, translateError(err)
	}
	// Revisions only increase during a row's lifetime and its history starts
	// at revision 1, so a document holds at most Revision versions.
	if maximum > 0 && document.Revision > maximum {
		prune := "DELETE FROM " + quote("ridu_versions") + " WHERE " + quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2 AND " + quote("revision") + " NOT IN (SELECT " + quote("revision") + " FROM " + quote("ridu_versions") + " WHERE " + quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2 ORDER BY " + quote("revision") + " DESC LIMIT $3)"
		if _, err := transaction.transaction.Exec(ctx, prune, collection.ID, document.ID, maximum); err != nil {
			return store.Version{}, translateError(err)
		}
	}
	return version, nil
}

func (transaction *documentTransaction) ListVersions(ctx context.Context, request store.VersionRequest) ([]store.Version, error) {
	predicate, arguments, err := postgresVersionPredicate(request)
	if err != nil {
		return nil, err
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
		version.DocumentID = request.DocumentID
		if err := rows.Scan(&version.Revision, &version.Status, &encoded, &version.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &version.Snapshot); err != nil {
			return nil, fmt.Errorf("decode version snapshot: %w", err)
		}
		version.ID = request.DocumentID + ":" + fmt.Sprint(version.Revision)
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (transaction *documentTransaction) CountVersions(ctx context.Context, request store.VersionRequest) (int, error) {
	predicate, arguments, err := postgresVersionPredicate(request)
	if err != nil {
		return 0, err
	}
	statement := "SELECT count(*) FROM " + quote("ridu_versions") + " WHERE " + predicate
	var count int
	if err := transaction.transaction.QueryRow(ctx, statement, arguments...).Scan(&count); err != nil {
		return 0, translateError(err)
	}
	return count, nil
}

func postgresVersionPredicate(request store.VersionRequest) (string, []any, error) {
	arguments := []any{request.Collection.ID, request.DocumentID}
	predicate := quote("collection_id") + " = $1 AND " + quote("document_id") + " = $2"
	if request.Access == nil {
		return predicate, arguments, nil
	}
	compiler := predicateCompiler{collection: request.Collection, next: len(arguments), arguments: arguments, snapshot: true, localeChain: request.LocaleChain}
	compiled, err := compileAccessPredicate(&compiler, *request.Access, request.AllLocales, request.Locales)
	if err != nil {
		return "", nil, err
	}
	return predicate + " AND (" + compiled + ")", compiler.arguments, nil
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

func scanDocument(row rowScanner, collection schema.Collection, fields []schema.Field, locales []schema.LocaleCode) (store.Document, error) {
	var document store.Document
	destinations, finish := documentDestinations(&document, collection, fields, locales)
	if err := row.Scan(destinations...); err != nil {
		return store.Document{}, err
	}
	if err := finish(); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

// columnValue receives one physical value column in its native type, so no
// value takes a JSON round trip unless it is stored as JSON.
type columnValue struct {
	kind   columnKind
	bytes  []byte
	text   *string
	number *float64
	flag   *bool
}

type columnKind uint8

const (
	columnText columnKind = iota
	columnJSON
	columnNumber
	columnBoolean
)

func physicalColumnKind(field schema.Field) columnKind {
	switch {
	case isJSONStoredField(field):
		return columnJSON
	case field.Type == schema.FieldTypeNumber:
		return columnNumber
	case field.Type == schema.FieldTypeCheckbox:
		return columnBoolean
	default:
		return columnText
	}
}

func (column *columnValue) destination() any {
	switch column.kind {
	case columnJSON:
		return &column.bytes
	case columnNumber:
		return &column.number
	case columnBoolean:
		return &column.flag
	default:
		return &column.text
	}
}

// value decodes a scanned column. SQL NULL reports absent.
func (column *columnValue) value(field schema.Field) (store.Value, bool, error) {
	switch column.kind {
	case columnJSON:
		if column.bytes == nil {
			return store.Value{}, false, nil
		}
		var decoded store.Value
		if err := decoded.UnmarshalJSON(column.bytes); err != nil {
			return store.Value{}, false, fmt.Errorf("decode JSON field %s: %w", field.Name, err)
		}
		return decoded, true, nil
	case columnNumber:
		if column.number == nil {
			return store.Value{}, false, nil
		}
		return store.Number(*column.number), true, nil
	case columnBoolean:
		if column.flag == nil {
			return store.Value{}, false, nil
		}
		return store.Boolean(*column.flag), true, nil
	default:
		if column.text == nil {
			return store.Value{}, false, nil
		}
		return store.String(*column.text), true, nil
	}
}

// documentDestinations scans the columns selectColumns emits for the same
// fields and locales. A localized field reads one native column per locale and
// becomes a locale-keyed object holding only the locales with a value; with
// none it is an empty object.
func documentDestinations(document *store.Document, collection schema.Collection, fields []schema.Field, locales []schema.LocaleCode) ([]any, func() error) {
	destinations := []any{&document.ID, &document.CreatedAt, &document.UpdatedAt, &document.DeletedAt}
	if collection.Versions != nil {
		destinations = append(destinations, &document.Status)
	}
	if collection.Versions != nil || collection.Upload != nil {
		destinations = append(destinations, &document.Revision)
	}
	count := 0
	for _, field := range fields {
		if field.Localized {
			count += len(locales)
		} else {
			count++
		}
	}
	columns := make([]columnValue, count)
	position := 0
	for _, field := range fields {
		width := 1
		if field.Localized {
			width = len(locales)
		}
		kind := physicalColumnKind(field)
		for offset := 0; offset < width; offset++ {
			columns[position].kind = kind
			destinations = append(destinations, columns[position].destination())
			position++
		}
	}
	return destinations, func() error {
		document.Values = make(store.Values, len(fields))
		position := 0
		for _, field := range fields {
			if !field.Localized {
				value, present, err := columns[position].value(field)
				if err != nil {
					return err
				}
				if !present {
					value = store.Null()
				}
				document.Values[field.Name] = value
				position++
				continue
			}
			localized := make(store.Values, len(locales))
			for _, locale := range locales {
				value, present, err := columns[position].value(field)
				if err != nil {
					return err
				}
				// A JSON null is absent, like SQL NULL: a locale holds a value or
				// is missing from the wrapper.
				if present && value.Kind() != store.ValueNull {
					localized[string(locale)] = value
				}
				position++
			}
			document.Values[field.Name] = store.Object(localized)
		}
		return nil
	}
}

func requestPredicate(request store.Request, requireID bool) (string, []any, error) {
	return requestPredicateFrom(request, requireID, 0)
}

func requestPredicateFrom(request store.Request, requireID bool, offset int) (string, []any, error) {
	if err := membership.ValidateRequest(request); err != nil {
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
		predicates = append(predicates, "("+compiled+")")
	}
	if request.Access != nil {
		compiled, err := compileAccessPredicate(&compiler, *request.Access, request.AllLocales, request.Locales)
		if err != nil {
			return "", nil, err
		}
		predicates = append(predicates, "("+compiled+")")
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
				return "", querypath.Unsupported(node.Comparison.Path, err)
			}
			return "", err
		}
		if err := membership.ValidateComparison(resolved.leaf, *node.Comparison); err != nil {
			return "", err
		}
		resolved.snapshot = compiler.snapshot
		resolved.localeChain = compiler.localeChain
		if resolved.many() {
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
		// The group carries its own parentheses: callers join predicates with
		// AND, and an ungrouped `a OR b` there would read as `(... AND a) OR b`.
		return "(" + strings.Join(children, separator) + ")", nil
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
	if membership.KindOf(resolved.leaf) != membership.None {
		return "", fmt.Errorf("membership field %q cannot be sorted", path.String())
	}
	if resolved.many() {
		return "", fmt.Errorf("sort field %q traverses a repeated field", path.String())
	}
	resolved.snapshot = compiler.snapshot
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
	if kind := membership.KindOf(field); kind != membership.None {
		if err := membership.ValidateComparison(field, comparison); err != nil {
			return "", err
		}
		if comparison.Operator == query.OperatorIn {
			return compiler.compileMembership(raw, field, kind, comparison.Value.Values())
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
		// Only a stored string contains text; a JSON array or object never does.
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

// compileMembership holds when the stored JSON list contains any candidate.
// jsonb containment compares strings and numbers exactly and reference
// objects by both members. A singular polymorphic relationship stores one
// reference object, which contains the candidate object directly.
func (compiler *predicateCompiler) compileMembership(raw string, field schema.Field, kind membership.Kind, candidates []query.Value) (string, error) {
	predicates := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		var item string
		switch kind {
		case membership.References:
			relationTo, id, _ := candidate.ReferenceValue()
			relationToPlaceholder, err := compiler.operand(query.String(relationTo))
			if err != nil {
				return "", err
			}
			idPlaceholder, err := compiler.operand(query.String(id))
			if err != nil {
				return "", err
			}
			item = "jsonb_build_object('relationTo', " + relationToPlaceholder + "::text, 'id', " + idPlaceholder + "::text)"
		case membership.Numbers:
			placeholder, err := compiler.operand(candidate)
			if err != nil {
				return "", err
			}
			item = placeholder + "::double precision"
		default:
			placeholder, err := compiler.operand(candidate)
			if err != nil {
				return "", err
			}
			item = placeholder + "::text"
		}
		contained := "jsonb_build_array(" + item + ")"
		if kind == membership.References && !field.Relationship.HasMany {
			contained = item
		}
		predicates = append(predicates, "COALESCE("+raw+" @> "+contained+", FALSE)")
	}
	if len(predicates) == 0 {
		return "FALSE", nil
	}
	return "(" + strings.Join(predicates, " OR ") + ")", nil
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

// compileManyComparison compiles a path through repeated fields. The values a
// path reaches are the leaf values of every row chain: some row at each
// repeated level whose block type matches its path segment. A comparison holds
// when some reached value satisfies it, so it is an EXISTS over the innermost
// rows; negated forms negate the whole EXISTS.
//
// One row source enumerates the rows of a hop of repeated levels. A single
// level expands its array with jsonb_array_elements. Several levels use one
// strict jsonpath query instead of nesting a correlated subquery per level,
// whose per-row rescans cost about six times as much. Fallback between locales
// has no jsonpath form, so a level reached through a localized container
// starts a new hop, read in SQL from the enclosing hop's row.
func (compiler *predicateCompiler) compileManyComparison(resolved predicatePath, comparison query.Comparison) (string, error) {
	hops := resolved.hops()
	sources := make([]string, len(hops))
	blockConditions := make([]string, len(hops))
	for index, hop := range hops {
		enclosing := ""
		if index > 0 {
			enclosing = rowAlias(index-1) + ".value"
		}
		column := resolved.repeatColumn(hop.start, enclosing)
		if hop.start == hop.end {
			sources[index] = "jsonb_array_elements(" + safeJSONArray(column) + ")"
			if blockType := resolved.repeats[hop.start].blockType; blockType != "" {
				compiler.arguments = append(compiler.arguments, blockType)
				compiler.next++
				blockConditions[index] = fmt.Sprintf("%s.value ->> 'blockType' = $%d", rowAlias(index), compiler.next)
			}
			continue
		}
		path, variables, err := resolved.hopJSONPath(hop)
		if err != nil {
			return "", err
		}
		compiler.arguments = append(compiler.arguments, path, variables)
		compiler.next += 2
		sources[index] = fmt.Sprintf("jsonb_path_query(%s, $%d::jsonpath, $%d::jsonb)", column, compiler.next-1, compiler.next)
	}
	exists := func(condition string) string {
		for index := len(hops) - 1; index >= 0; index-- {
			conditions := make([]string, 0, 2)
			if condition != "" {
				conditions = append(conditions, condition)
			}
			if blockConditions[index] != "" {
				conditions = append(conditions, blockConditions[index])
			}
			where := ""
			if len(conditions) != 0 {
				where = " WHERE " + strings.Join(conditions, " AND ")
			}
			condition = fmt.Sprintf("EXISTS (SELECT 1 FROM %s AS %s(value)%s)", sources[index], rowAlias(index), where)
		}
		return condition
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

// predicatePath locates a predicate's leaf in a document. Without repeated
// fields, jsonSegments lead from the root column to the leaf. Otherwise each
// repeats entry is one array or blocks field the path traverses, and rowPath
// leads from a row of the last one to the leaf.
type predicatePath struct {
	root               schema.Field
	leaf               schema.Field
	jsonSegments       []string
	jsonLocalizedAfter []int
	repeats            []predicateRepeat
	rowPath            []string
	rowLocalizedAfter  []int
	snapshot           bool
	localeChain        []schema.LocaleCode
}

// predicateRepeat is one traversed array or blocks field. path leads to it
// from the root column for the first, and from a row of the previous one
// otherwise; blockType binds its rows when it is a blocks field.
type predicateRepeat struct {
	path           []string
	localizedAfter []int
	blockType      string
}

func (path predicatePath) many() bool { return len(path.repeats) != 0 }

// predicateHop is a run of consecutive repeated levels, start to end, whose
// rows one row source enumerates.
type predicateHop struct{ start, end int }

// hops starts a hop at the first repeated field and at each one reached
// through a localized container.
func (path predicatePath) hops() []predicateHop {
	var hops []predicateHop
	for depth, repeat := range path.repeats {
		if depth == 0 || len(repeat.localizedAfter) != 0 {
			hops = append(hops, predicateHop{start: depth, end: depth})
			continue
		}
		hops[len(hops)-1].end = depth
	}
	return hops
}

// rowAlias names the rows of the hop at index in nested EXISTS.
func rowAlias(index int) string {
	if index == 0 {
		return "ridu_item"
	}
	return fmt.Sprintf("ridu_item_%d", index+1)
}

// rowValue is the innermost row, which the leaf's row path starts from.
func (path predicatePath) rowValue() string {
	return rowAlias(len(path.hops())-1) + ".value"
}

// repeatColumn is the JSON array of the repeated field at depth, read from
// the root column or from enclosing, a row of the previous hop.
func (path predicatePath) repeatColumn(depth int, enclosing string) string {
	if depth == 0 {
		return path.arrayColumn()
	}
	repeat := path.repeats[depth]
	if len(repeat.localizedAfter) != 0 && len(path.localeChain) != 0 {
		return localizedJSONColumn(enclosing, repeat.path, repeat.localizedAfter, path.localeChain)
	}
	return jsonValueAt(enclosing, repeat.path)
}

// hopJSONPath enumerates the rows of a hop's last level from the array of its
// first, with each block level bound to its block type through a jsonpath
// variable. Strict mode never wraps a non-array as one row; the filters keep
// every accessor and [*] to values that have them, because a strict
// structural error outside a filter would end the whole query instead of
// skipping the row. A row is an element of an array, like
// jsonb_array_elements, and a key path reaches what #> reaches.
func (path predicatePath) hopJSONPath(hop predicateHop) (string, string, error) {
	keys := func(segments []string) string {
		var result strings.Builder
		for _, segment := range segments {
			encoded, _ := json.Marshal(segment)
			result.WriteString(".")
			result.Write(encoded)
		}
		return result.String()
	}
	var expression strings.Builder
	expression.WriteString("strict $")
	variables := map[string]string{}
	for depth := hop.start; depth <= hop.end; depth++ {
		if depth > hop.start {
			expression.WriteString(keys(path.repeats[depth].path))
		}
		expression.WriteString(` ? (@.type() == "array")[*]`)
		var filters []string
		if blockType := path.repeats[depth].blockType; blockType != "" {
			name := fmt.Sprintf("block%d", depth)
			variables[name] = blockType
			filters = append(filters, `@."blockType" == $`+name)
		}
		if depth < hop.end {
			filters = append(filters, "exists(@"+keys(path.repeats[depth+1].path)+")")
		}
		if len(filters) != 0 {
			expression.WriteString(" ? (" + strings.Join(filters, " && ") + ")")
		}
	}
	encoded, err := json.Marshal(variables)
	if err != nil {
		return "", "", err
	}
	return expression.String(), string(encoded), nil
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

// arrayColumn is the first traversed repeated field's JSON array, read from
// the root column.
func (path predicatePath) arrayColumn() string {
	arrayPath, arrayLocalizedAfter := path.repeats[0].path, path.repeats[0].localizedAfter
	if path.snapshot {
		if path.root.Localized && len(path.localeChain) != 0 {
			containers := make([]string, 0, len(path.localeChain))
			for _, locale := range path.localeChain {
				containers = append(containers, quote("snapshot")+" #> '{Values,"+path.root.Name+","+string(locale)+"}'")
			}
			return jsonValueAt(coalescedJSONValue(containers), arrayPath)
		}
		if len(arrayLocalizedAfter) != 0 && len(path.localeChain) != 0 {
			return localizedJSONColumn(quote("snapshot"), append([]string{"Values", path.root.Name}, arrayPath...), offsetBoundaries(arrayLocalizedAfter, 2), path.localeChain)
		}
		segments := append([]string{"Values", path.root.Name}, arrayPath...)
		return quote("snapshot") + " #> '{" + strings.Join(segments, ",") + "}'"
	}
	column := quote(fieldColumn(path.root.ID))
	if path.root.Localized && len(path.localeChain) != 0 {
		containers := make([]string, 0, len(path.localeChain))
		for _, locale := range path.localeChain {
			containers = append(containers, quote(localizedFieldColumn(path.root.ID, locale)))
		}
		return jsonValueAt(coalescedJSONValue(containers), arrayPath)
	}
	if len(arrayLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		return localizedJSONColumn(column, arrayPath, arrayLocalizedAfter, path.localeChain)
	}
	if len(arrayPath) != 0 {
		column += " #> '{" + strings.Join(arrayPath, ",") + "}'"
	}
	return column
}

func (path predicatePath) rowColumn() string {
	column := path.rowValue()
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
			return localizedJSONScalar(path.rowValue(), path.rowPath, path.rowLocalizedAfter[0], path.localeChain)
		}
		return localizedJSONColumn(path.rowValue(), path.rowPath, path.rowLocalizedAfter, path.localeChain)
	}
	return jsonValueAt(path.rowValue(), path.rowPath)
}

func (path predicatePath) rowPresenceColumn() string {
	if len(path.rowLocalizedAfter) != 0 && len(path.localeChain) != 0 {
		if path.rowLocalizedAfter[0] == len(path.rowPath) {
			return path.rowColumn() + " IS NOT NULL"
		}
		return localizedJSONColumn(path.rowValue(), path.rowPath, path.rowLocalizedAfter, path.localeChain) + " IS NOT NULL"
	}
	if len(path.rowPath) == 0 {
		return path.rowValue() + " IS NOT NULL"
	}
	return path.rowValue() + " #> '{" + strings.Join(path.rowPath, ",") + "}' IS NOT NULL"
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
	// segments and localizedAfter lead from the current scope, the root column
	// or a row of the last repeated field, to the field reached so far.
	var segmentsInScope []string
	var localizedAfter []int
	enterRows := func(blockType string) {
		resolved.repeats = append(resolved.repeats, predicateRepeat{path: segmentsInScope, localizedAfter: localizedAfter, blockType: blockType})
		segmentsInScope, localizedAfter = nil, nil
	}
	for index := 1; index < len(segments); index++ {
		segment := segments[index]
		if current.Type == schema.FieldTypeJSON {
			// JSON fields intentionally have no authored child schema, but callers
			// may still address a bounded path within their opaque value. Framework
			// upload delivery relies on this for generated image-size object keys.
			segmentsInScope = append(segmentsInScope, segments[index:]...)
			resolved.leaf = schema.Field{ID: current.ID, Name: segments[len(segments)-1], Type: schema.FieldTypeJSON}
			break
		}
		switch {
		case current.Type == schema.FieldTypeBlocks && current.Blocks != nil:
			// The predicate needs only field structure, which a registered
			// block's shared definition supplies at every placement.
			block, found := current.Blocks.Definition(segment)
			if !found || index+1 >= len(segments) {
				return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
			}
			enterRows(segment)
			index++
			segment = segments[index]
			current = fieldNamed(block.ResolvedFields(), segment)
		case (current.Type == schema.FieldTypeArray || current.Type == schema.FieldTypeGroup) && current.Nested != nil:
			if current.Type == schema.FieldTypeArray {
				enterRows("")
			}
			current = fieldNamed(current.Nested.ResolvedFields(), segment)
		default:
			return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
		}
		if current == nil {
			return predicatePath{}, fmt.Errorf("predicate field %q is not in collection %q", path.String(), collection.Slug)
		}
		resolved.leaf = *current
		segmentsInScope = append(segmentsInScope, segment)
		if current.Localized {
			localizedAfter = append(localizedAfter, len(segmentsInScope))
		}
	}
	if resolved.many() {
		resolved.rowPath, resolved.rowLocalizedAfter = segmentsInScope, localizedAfter
	} else {
		resolved.jsonSegments, resolved.jsonLocalizedAfter = segmentsInScope, localizedAfter
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
		columns = append(columns, quote("_status"))
	}
	if collection.Versions != nil || collection.Upload != nil {
		columns = append(columns, quote("_revision"))
	}
	for _, field := range fields {
		if !field.Localized {
			columns = append(columns, quote(fieldColumn(field.ID)))
			continue
		}
		// Each locale is its own native column; documentDestinations assembles
		// the locale-keyed value without a JSON round trip.
		for _, locale := range locales {
			columns = append(columns, quote(localizedFieldColumn(field.ID, locale)))
		}
	}
	return strings.Join(columns, ", ")
}

// storedFieldsCache memoizes each collection's stored fields. An entry pins
// the field slice it was derived from, so a matching backing array identifies
// the same immutable resolved definition, and a reloaded definition replaces
// its entry. Entries are bounded by the number of collection IDs.
var storedFieldsCache sync.Map

type storedFieldsEntry struct {
	source []schema.Field
	stored []schema.Field
}

// storedSchemaFields returns a collection's physically stored root fields.
// The result is shared and read-only; its capacity is capped so an append
// always copies.
func storedSchemaFields(collection schema.Collection) []schema.Field {
	fields := collection.Fields
	if cached, found := storedFieldsCache.Load(collection.ID); found {
		entry := cached.(*storedFieldsEntry)
		if len(entry.source) == len(fields) && (len(fields) == 0 || &entry.source[0] == &fields[0]) {
			return entry.stored
		}
	}
	stored := make([]schema.Field, 0, len(fields))
	for _, field := range fields {
		if field.Category != schema.FieldCategoryPresentation {
			stored = append(stored, field)
		}
	}
	stored = stored[:len(stored):len(stored)]
	storedFieldsCache.Store(collection.ID, &storedFieldsEntry{source: fields, stored: stored})
	return stored
}

func databaseValue(field schema.Field, value store.Value) (any, error) {
	if isJSONStoredField(field) {
		if value.Kind() == store.ValueNull {
			return nil, nil
		}
		return value.MarshalJSON()
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

// publishedCollectionTable holds a versioned resource's live rows with the
// working table's physical layout.
func publishedCollectionTable(id schema.StableID) string { return "z_p_" + identifierHash(string(id)) }

// documentTables lists a resource's document tables: the working table and,
// for a versioned resource, its live table. Both share one physical layout.
func documentTables(resource schema.Collection, id schema.StableID) []string {
	if resource.Versions == nil {
		return []string{collectionTable(id)}
	}
	return []string{collectionTable(id), publishedCollectionTable(id)}
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
