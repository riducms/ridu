// Package sqlite provides Ridu's official embedded SQLite document store.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gofrs/flock"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	modernsqlite "modernc.org/sqlite"
)

const (
	defaultBusyTimeout = 10 * time.Second
	defaultConnections = 8
	maxBusyTimeout     = time.Duration(1<<31-1) * time.Millisecond
)

// Config controls the bounded connection behavior of the embedded SQLite
// store. Zero values select safe local defaults.
type Config struct {
	// Path is a filesystem path, a file: SQLite URI, or :memory:.
	Path string
	// BusyTimeout bounds how long SQLite waits for another writer. Zero selects
	// ten seconds. Negative values and values above SQLite's signed 32-bit
	// millisecond limit are rejected.
	BusyTimeout time.Duration
	// DisableWAL keeps file databases in their existing journal mode. WAL is
	// enabled by default because it permits readers while the single writer is
	// active. SQLite memory databases ignore WAL mode.
	DisableWAL bool
	// MaxConnections bounds pooled readers. Operation transactions still use
	// BEGIN IMMEDIATE and SQLite's single-writer contract.
	MaxConnections int
}

// Store is one embedded SQLite database. Schema changes are explicit through
// Migrate or immutable artifact application; Open never mutates CMS tables.
type Store struct {
	db       *sql.DB
	anchor   *sql.Conn
	dsn      string
	filePath string
	lockPath string
	now      func() time.Time

	writeGate  chan struct{}
	uploadGate chan struct{}
}

// Open connects to a SQLite file or :memory: database using safe defaults.
func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithConfig(ctx, Config{Path: path})
}

// OpenWithConfig connects to SQLite without creating or changing Ridu's
// schema. Every pooled connection receives the same validated pragmas.
func OpenWithConfig(ctx context.Context, config Config) (*Store, error) {
	if strings.TrimSpace(config.Path) == "" {
		return nil, fmt.Errorf("SQLite path is required")
	}
	if config.BusyTimeout < 0 {
		return nil, fmt.Errorf("SQLite busy timeout cannot be negative")
	}
	if config.BusyTimeout == 0 {
		config.BusyTimeout = defaultBusyTimeout
	}
	if config.BusyTimeout%time.Millisecond != 0 {
		return nil, fmt.Errorf("SQLite busy timeout must use whole milliseconds")
	}
	if config.BusyTimeout > maxBusyTimeout {
		return nil, fmt.Errorf("SQLite busy timeout cannot exceed %d milliseconds", maxBusyTimeout/time.Millisecond)
	}
	if config.MaxConnections < 0 {
		return nil, fmt.Errorf("SQLite maximum connections cannot be negative")
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = defaultConnections
	}

	dsn, filePath, memory, err := sqliteDSN(config.Path)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse SQLite path: %w", err)
	}
	parameters := parsed.Query()
	if err := rejectProtectedSQLiteParameters(parameters); err != nil {
		return nil, err
	}
	parameters.Set("_busy_timeout", strconv.FormatInt(config.BusyTimeout.Milliseconds(), 10))
	parameters.Set("_foreign_keys", "on")
	parameters.Set("_synchronous", "full")
	if !memory && !config.DisableWAL {
		parameters.Set("_journal_mode", "wal")
	}
	parsed.RawQuery = parameters.Encode()
	dsn = parsed.String()

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("configure SQLite: %w", err)
	}
	maxOpenConnections := config.MaxConnections
	if memory {
		// Keep one connection checked out for the Store lifetime. SQLite destroys
		// a shared in-memory database when its final connection closes, so the
		// anchor must sit outside the configured operational pool bound.
		maxOpenConnections++
	}
	database.SetMaxOpenConns(maxOpenConnections)
	database.SetMaxIdleConns(config.MaxConnections)
	if !memory {
		database.SetConnMaxIdleTime(3 * time.Minute)
	}
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect SQLite: %w", sanitizeSQLiteError(err))
	}
	if filePath != "" {
		absolute, pathError := filepath.Abs(filePath)
		if pathError != nil {
			database.Close()
			return nil, fmt.Errorf("resolve SQLite lock identity: %w", pathError)
		}
		filePath, pathError = filepath.EvalSymlinks(absolute)
		if pathError != nil {
			database.Close()
			return nil, fmt.Errorf("resolve SQLite lock identity: %w", pathError)
		}
	}
	var anchor *sql.Conn
	if memory {
		anchor, err = database.Conn(ctx)
		if err != nil {
			database.Close()
			return nil, fmt.Errorf("anchor SQLite memory database: %w", sanitizeSQLiteError(err))
		}
	}
	backend := &Store{
		db: database, anchor: anchor, dsn: dsn, filePath: filePath, now: time.Now,
		writeGate: make(chan struct{}, 1), uploadGate: make(chan struct{}, 1),
	}
	backend.writeGate <- struct{}{}
	backend.uploadGate <- struct{}{}
	if filePath != "" {
		backend.lockPath = filePath + ".ridu-upload.lock"
	}
	return backend, nil
}

func rejectProtectedSQLiteParameters(parameters url.Values) error {
	protected := map[string]struct{}{
		"_busy_timeout": {}, "_timeout": {},
		"_foreign_keys": {}, "_fk": {},
		"_journal_mode": {}, "_journal": {},
		"_synchronous": {}, "_sync": {},
		"_query_only": {},
		"immutable":   {},
		"nolock":      {},
		"vfs":         {},
	}
	for key := range protected {
		if _, exists := parameters[key]; exists {
			return fmt.Errorf("SQLite URI parameter %q is controlled by sqlite.Config", key)
		}
	}
	if _, exists := parameters["_pragma"]; exists {
		return fmt.Errorf("SQLite URI parameter %q is controlled by sqlite.Config", "_pragma")
	}
	mode, err := singleSQLiteURIParameter(parameters, "mode")
	if err != nil {
		return err
	}
	if strings.EqualFold(mode, "ro") {
		return fmt.Errorf("SQLite URI parameter %q must select a writable SQLite store", "mode")
	}
	return nil
}

func singleSQLiteURIParameter(parameters url.Values, name string) (string, error) {
	values := parameters[name]
	if len(values) > 1 {
		return "", fmt.Errorf("SQLite URI parameter %q must not be specified more than once", name)
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

func sqliteDSN(input string) (dsn, filePath string, memory bool, err error) {
	input = strings.TrimSpace(input)
	if err := validateSQLiteURIText("SQLite path", input); err != nil {
		return "", "", false, err
	}
	if input == ":memory:" {
		// A named shared-cache memory database lets pooled connections observe the
		// same contents while remaining private to this Store instance.
		name, idError := newID("ridu")
		if idError != nil {
			return "", "", false, idError
		}
		return "file:" + name + "?mode=memory&cache=shared", "", true, nil
	}
	if strings.HasPrefix(input, "file:") {
		parsed, parseError := url.Parse(input)
		if parseError != nil {
			return "", "", false, fmt.Errorf("parse SQLite URI: %w", parseError)
		}
		target := parsed.Path
		if target == "" {
			target, parseError = url.PathUnescape(parsed.Opaque)
			if parseError != nil {
				return "", "", false, fmt.Errorf("decode SQLite file URI path: %w", parseError)
			}
		}
		if target == "" {
			target = strings.TrimPrefix(strings.SplitN(input, "?", 2)[0], "file:")
		}
		if target == "" {
			return "", "", false, fmt.Errorf("SQLite file URI path is required")
		}
		if err := validateSQLiteURIText("SQLite file URI path", target); err != nil {
			return "", "", false, err
		}
		parameters := parsed.Query()
		if err := validateSQLiteURIParameters(parameters); err != nil {
			return "", "", false, err
		}
		mode, modeError := singleSQLiteURIParameter(parameters, "mode")
		if modeError != nil {
			return "", "", false, modeError
		}
		memory = mode == "memory" || target == ":memory:" || strings.EqualFold(parameters.Get("vfs"), "memdb")
		if memory {
			return "", "", false, fmt.Errorf("shared SQLite memory URIs are unsupported; use :memory: for a private pooled store")
		}
		filePath = target
		return input, filePath, memory, nil
	}
	absolute, pathError := filepath.Abs(input)
	if pathError != nil {
		return "", "", false, fmt.Errorf("resolve SQLite path: %w", pathError)
	}
	return (&url.URL{Scheme: "file", Path: absolute}).String(), absolute, false, nil
}

func validateSQLiteURIParameters(parameters url.Values) error {
	for key, values := range parameters {
		if err := validateSQLiteURIText("SQLite URI parameter name", key); err != nil {
			return err
		}
		for _, value := range values {
			if err := validateSQLiteURIText(fmt.Sprintf("SQLite URI parameter %q", key), value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSQLiteURIText(scope, value string) error {
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters", scope)
		}
	}
	return nil
}

// Close releases every pooled SQLite connection.
func (backend *Store) Close() error {
	if backend == nil || backend.db == nil {
		return nil
	}
	var anchorError error
	if backend.anchor != nil {
		anchorError = backend.anchor.Close()
		backend.anchor = nil
	}
	return errors.Join(anchorError, backend.db.Close())
}

// Ping proves that SQLite can serve a query and that foreign-key enforcement
// is active on the selected pooled connection.
func (backend *Store) Ping(ctx context.Context) error {
	if backend == nil || backend.db == nil {
		return fmt.Errorf("SQLite database is unavailable")
	}
	var foreignKeys int
	if err := backend.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fmt.Errorf("ping SQLite: %w", translateError(err))
	}
	if foreignKeys != 1 {
		return fmt.Errorf("SQLite foreign-key enforcement is disabled")
	}
	return nil
}

// Ready proves connectivity and that the explicitly applied manifest is the
// exact manifest embedded in this process.
func (backend *Store) Ready(ctx context.Context, manifest schema.Manifest) error {
	if err := backend.Ping(ctx); err != nil {
		return err
	}
	return backend.verifyImmutableReadyState(ctx, manifest)
}

// ReadyWithMigrationHistory proves ordinary readiness and exact agreement
// with the complete ordered migration history embedded in the executable.
func (backend *Store) ReadyWithMigrationHistory(ctx context.Context, manifest schema.Manifest, expectedHistoryDigest string) error {
	if err := backend.Ping(ctx); err != nil {
		return err
	}
	return backend.verifyImmutableReadyStateWithHistory(ctx, manifest, expectedHistoryDigest)
}

// Begin opens a write-capable operation transaction. BEGIN IMMEDIATE obtains
// SQLite's honest database-level writer reservation before access and
// relationship checks run, replacing PostgreSQL row locks without pretending
// SQLite has finer lock semantics.
func (backend *Store) Begin(ctx context.Context) (store.Transaction, error) {
	if err := backend.acquireWriteGate(ctx); err != nil {
		return nil, err
	}
	transaction, err := backend.begin(ctx, "BEGIN IMMEDIATE", func() { backend.writeGate <- struct{}{} })
	if err != nil {
		backend.writeGate <- struct{}{}
		return nil, err
	}
	return transaction, nil
}

// BeginSnapshot opens a stable deferred read snapshot without reserving the
// database's single writer. The operation engine uses it for read-only work.
func (backend *Store) BeginSnapshot(ctx context.Context) (store.Transaction, error) {
	transaction, err := backend.begin(ctx, "BEGIN", nil)
	if err != nil {
		return nil, err
	}
	transaction.readOnly = true
	return transaction, nil
}

func (backend *Store) begin(ctx context.Context, statement string, release func()) (*documentTransaction, error) {
	if backend == nil || backend.db == nil {
		return nil, fmt.Errorf("SQLite database is unavailable")
	}
	connection, err := backend.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve SQLite connection: %w", translateError(err))
	}
	if _, err := connection.ExecContext(ctx, statement); err != nil {
		_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		connection.Close()
		return nil, fmt.Errorf("begin SQLite transaction: %w", translateError(err))
	}
	return &documentTransaction{store: backend, connection: connection, release: release}, nil
}

func (backend *Store) withImmediate(ctx context.Context, action func(*sql.Conn) error) (err error) {
	if err := backend.acquireWriteGate(ctx); err != nil {
		return err
	}
	defer func() { backend.writeGate <- struct{}{} }()
	connection, err := backend.db.Conn(ctx)
	if err != nil {
		return translateError(err)
	}
	defer connection.Close()
	if _, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		return translateError(err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err = action(connection); err != nil {
		return err
	}
	if _, err = connection.ExecContext(ctx, "COMMIT"); err != nil {
		return translateError(err)
	}
	committed = true
	return nil
}

func (backend *Store) acquireWriteGate(ctx context.Context) error {
	select {
	case <-backend.writeGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type documentTransaction struct {
	store      *Store
	connection *sql.Conn
	readOnly   bool
	release    func()
	mu         sync.Mutex
	uploadLock *flock.Flock
	uploadGate bool
}

func (transaction *documentTransaction) enter(ctx context.Context, writable bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transaction.mu.Lock()
	if transaction.connection == nil {
		transaction.mu.Unlock()
		return nil, fmt.Errorf("transaction already completed")
	}
	if writable && transaction.readOnly {
		transaction.mu.Unlock()
		return nil, fmt.Errorf("snapshot transaction is read-only")
	}
	return transaction.mu.Unlock, nil
}

func (transaction *documentTransaction) finish(ctx context.Context, statement string) error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.connection == nil {
		return fmt.Errorf("transaction already completed")
	}
	_, result := transaction.connection.ExecContext(ctx, statement)
	if result != nil {
		_, _ = transaction.connection.ExecContext(context.Background(), "ROLLBACK")
	}
	closeError := transaction.connection.Close()
	if result == nil {
		result = closeError
	}
	if transaction.release != nil {
		transaction.release()
	}
	if transaction.uploadLock != nil {
		result = errors.Join(result, transaction.uploadLock.Close())
		transaction.uploadLock = nil
	}
	if transaction.uploadGate {
		transaction.store.uploadGate <- struct{}{}
		transaction.uploadGate = false
	}
	transaction.connection = nil
	return translateError(result)
}

func (transaction *documentTransaction) Commit(ctx context.Context) error {
	return transaction.finish(ctx, "COMMIT")
}

func (transaction *documentTransaction) Rollback(ctx context.Context) error {
	return transaction.finish(ctx, "ROLLBACK")
}

func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	var sqliteError *modernsqlite.Error
	if errors.As(err, &sqliteError) {
		code := sqliteError.Code()
		switch code & 0xff {
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
			return store.ErrConflict
		}
		switch code {
		case 1555, 2067, 2579: // PRIMARYKEY, UNIQUE, ROWID
			return store.ErrConflict
		}
	}
	return err
}

func sanitizeSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	// Driver errors can echo the complete URI. Return the stable SQLite message
	// without wrapping the configured path or URI credentials.
	var sqliteError *modernsqlite.Error
	if errors.As(err, &sqliteError) {
		return fmt.Errorf("SQLite error %d", sqliteError.Code())
	}
	return err
}

func isNoSuchTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func newID(prefix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate %s ID: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(random[:]), nil
}

var _ store.Store = (*Store)(nil)
var _ store.SnapshotStore = (*Store)(nil)
var _ store.HealthStore = (*Store)(nil)
var _ store.ReadinessStore = (*Store)(nil)
var _ store.MigrationReadinessStore = (*Store)(nil)
var _ store.Transaction = (*documentTransaction)(nil)
