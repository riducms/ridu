package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLitePragmasApplyToEveryPooledConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := OpenWithConfig(ctx, Config{
		Path: filepath.Join(t.TempDir(), "pragmas.sqlite"), BusyTimeout: 137 * time.Millisecond, MaxConnections: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	connections := make([]*sql.Conn, 4)
	for index := range connections {
		connections[index], err = backend.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer connections[index].Close()
	}
	for index, connection := range connections {
		var foreignKeys, busyTimeout, synchronous int
		var journalMode string
		if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || busyTimeout != 137 || synchronous != 2 || journalMode != "wal" {
			t.Fatalf("connection %d pragmas = foreign_keys:%d busy_timeout:%d synchronous:%d journal_mode:%s", index, foreignKeys, busyTimeout, synchronous, journalMode)
		}
	}
}

func TestSQLiteRejectsBusyTimeoutAboveSQLiteLimit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "busy-timeout.sqlite")
	backend, err := OpenWithConfig(context.Background(), Config{
		Path: path, BusyTimeout: maxBusyTimeout + time.Millisecond,
	})
	if backend != nil || err == nil || !strings.Contains(err.Error(), "cannot exceed 2147483647 milliseconds") {
		t.Fatalf("oversized busy timeout = %#v, %v", backend, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("oversized busy timeout created database: %v", statErr)
	}
}

func TestSQLiteMemoryDatabaseIsSharedAcrossItsPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := OpenWithConfig(ctx, Config{Path: ":memory:", MaxConnections: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})); err != nil {
		t.Fatal(err)
	}

	first, err := backend.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := backend.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := first.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES ('posts', 'one', 1, 1, '{}')`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.QueryRowContext(ctx, `SELECT count(*) FROM ridu_documents`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("second pooled connection sees %d documents, want 1", count)
	}
}

func TestSQLiteMemoryAnchorSurvivesIdlePoolClosure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := OpenWithConfig(ctx, Config{Path: ":memory:", MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES ('posts', 'one', 1, 1, '{}')`); err != nil {
		t.Fatal(err)
	}

	backend.db.SetMaxIdleConns(0)
	if open := backend.db.Stats().OpenConnections; open != 1 {
		t.Fatalf("open connections after idle-pool closure = %d, want the memory anchor only", open)
	}
	var count int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM ridu_documents`).Scan(&count); err != nil {
		t.Fatalf("query after idle-pool closure: %v", err)
	}
	if count != 1 {
		t.Fatalf("documents after idle-pool closure = %d, want 1", count)
	}
}

func TestSQLiteSameStoreWriterWaitHonorsContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitContext, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := backend.Begin(waitContext); !errors.Is(err, context.DeadlineExceeded) {
		_ = transaction.Rollback(ctx)
		t.Fatalf("contended Begin error = %v", err)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteOperationReadsOverlapWhileWriterGateIsHeld(t *testing.T) {
	ctx := context.Background()
	collection := schema.Collection{
		ID: "collection-posts", Slug: "posts",
		Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
	}
	manifest := schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "read concurrency"},
		Collections: []schema.Collection{collection},
	})
	backend, err := OpenWithConfig(ctx, Config{
		Path: filepath.Join(t.TempDir(), "read-concurrency.sqlite"), MaxConnections: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	engine, err := operationengine.New(operationengine.Config{
		Store: backend,
		Collections: []operationengine.Collection{{
			Schema: collection,
			Hooks: operationengine.Hooks{BeforeOperation: []operationengine.Hook{func(hookContext operationengine.Context) error {
				if hookContext.Operation == operationengine.Read {
					entered <- struct{}{}
					<-release
				}
				return nil
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	writer, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())

	type readResult struct {
		result operationengine.Result
		err    error
	}
	results := make(chan readResult, 2)
	for range 2 {
		go func() {
			readContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, readError := engine.Execute(readContext, operationengine.Request{
				Operation: operationengine.Read, Collection: "posts", Page: 1, Limit: 10,
			})
			results <- readResult{result: result, err: readError}
		}()
	}

	for count := 0; count < 2; count++ {
		select {
		case <-entered:
		case early := <-results:
			close(release)
			t.Fatalf("read completed before both snapshots overlapped: %v", early.err)
		case <-time.After(time.Second):
			close(release)
			t.Fatal("read waited behind SQLite's process-local writer gate")
		}
	}
	close(release)
	for range 2 {
		completed := <-results
		if completed.err != nil {
			t.Fatal(completed.err)
		}
		if completed.result.Page == nil || completed.result.Page.Total != 0 {
			t.Fatalf("read page = %#v, want an empty page", completed.result.Page)
		}
	}
}

func TestSQLiteIndependentStoreRemainsReusableAfterContendedBeginCancellation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "contended.sqlite")
	first, err := OpenWithConfig(ctx, Config{Path: path, BusyTimeout: 100 * time.Millisecond, MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := OpenWithConfig(ctx, Config{Path: path, BusyTimeout: 100 * time.Millisecond, MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := first.Migrate(ctx, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})); err != nil {
		t.Fatal(err)
	}

	held, err := first.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blockedContext, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if blocked, err := second.Begin(blockedContext); err == nil {
		_ = blocked.Rollback(ctx)
		_ = held.Rollback(ctx)
		t.Fatal("independent writer unexpectedly began while the file writer was held")
	}
	if err := held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	retryContext, retryCancel := context.WithTimeout(ctx, time.Second)
	defer retryCancel()
	retry, err := second.Begin(retryContext)
	if err != nil {
		t.Fatalf("begin after contended cancellation: %v", err)
	}
	raw := retry.(*documentTransaction)
	if _, err := raw.connection.ExecContext(retryContext, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES ('posts', 'after-contention', 1, 1, '{}')`); err != nil {
		_ = retry.Rollback(ctx)
		t.Fatalf("write after contended cancellation: %v", err)
	}
	if err := retry.Commit(retryContext); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteCanceledRollbackDoesNotReturnAnOpenTransactionToPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := OpenWithConfig(ctx, Config{Path: ":memory:", MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := transaction.Rollback(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("rollback error = %v, want context cancellation", err)
	}

	next, err := backend.Begin(ctx)
	if err != nil {
		t.Fatalf("begin after canceled rollback: %v", err)
	}
	if err := next.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteURICannotOverrideProtectedPragmas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "protected.sqlite")
	for _, query := range []string{
		"_fk=off",
		"_timeout=0",
		"_journal=off",
		"_sync=off",
		"_query_only=1",
		"immutable=1",
		"nolock=1",
		"vfs=unix-none",
		"_pragma=busy_timeout(0)",
		"_pragma=foreign_keys%3Doff",
		"_pragma=main.busy_timeout(0)",
	} {
		backend, err := Open(ctx, "file:"+path+"?"+query)
		if backend != nil {
			_ = backend.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "controlled by sqlite.Config") {
			t.Fatalf("Open(%q) error = %v", query, err)
		}
	}
}

func TestSQLiteRejectsReadOnlyURI(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "readonly.sqlite")
	backend, err := Open(context.Background(), "file:"+path+"?mode=ro")
	if backend != nil {
		_ = backend.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "writable SQLite store") {
		t.Fatalf("Open(mode=ro) error = %v", err)
	}
}

func TestSQLiteRejectsDuplicateModeParameters(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ambiguous-mode.sqlite")
	for _, query := range []string{
		"mode=rwc&mode=memory",
		"mode=rwc&mode=ro",
	} {
		backend, err := Open(context.Background(), "file:"+path+"?"+query)
		if backend != nil {
			_ = backend.Close()
		}
		if err == nil || !strings.Contains(err.Error(), `parameter "mode" must not be specified more than once`) {
			t.Fatalf("Open(%q) error = %v", query, err)
		}
	}
}

func TestSQLiteRejectsExternallySharedMemoryURI(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{
		"file:ridu-shared?mode=memory&cache=shared",
		"file::memory:?cache=shared",
		"file:%3Amemory%3A?cache=shared",
		"file:%3amemory%3a?cache=shared",
		"file:ridu-shared?vfs=memdb",
	} {
		backend, err := Open(context.Background(), uri)
		if backend != nil {
			_ = backend.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "use :memory:") {
			t.Fatalf("Open(%q) error = %v", uri, err)
		}
	}
}

func TestSQLiteRejectsControlCharactersBeforeURIClassification(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "control.sqlite")
	for _, uri := range []string{
		"file:" + path + "?mode=memory%00junk&cache=shared",
		"file:" + path + "?mode%00junk=memory&cache=shared",
		"file:" + path + "?mode=ro%00junk",
		"file:" + path + "%00ignored?mode=rwc",
	} {
		backend, err := Open(context.Background(), uri)
		if backend != nil {
			_ = backend.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "must not contain control characters") {
			t.Fatalf("Open(%q) error = %v", uri, err)
		}
	}
}

func TestSQLiteRejectsEmptyFileURI(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{"file:", "file:?mode=rwc"} {
		backend, err := Open(context.Background(), uri)
		if backend != nil {
			_ = backend.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("Open(%q) error = %v", uri, err)
		}
	}
}

func TestSQLiteWithImmediateRollsBackOnPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := OpenWithConfig(ctx, Config{Path: ":memory:", MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		_ = backend.withImmediate(ctx, func(connection *sql.Conn) error {
			if _, err := connection.ExecContext(ctx, `CREATE TABLE panic_transaction (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			panic("test panic")
		})
	}()
	if !panicked {
		t.Fatal("withImmediate did not propagate the action panic")
	}
	var count int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'panic_transaction'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("panicking transaction committed its schema change")
	}

	retryContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := backend.withImmediate(retryContext, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(retryContext, `CREATE TABLE recovered_transaction (id INTEGER PRIMARY KEY)`)
		return err
	}); err != nil {
		t.Fatalf("writer after panic: %v", err)
	}
}

func TestSQLiteTranslatesOnlyUniquenessConstraintsToConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion})); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES ('posts', 'one', 1, 1, '{}')`
	if _, err := backend.db.ExecContext(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.db.ExecContext(ctx, insert); !errors.Is(translateError(err), store.ErrConflict) {
		t.Fatalf("primary-key error = %v, want conflict", err)
	}
	_, foreignKeyError := backend.db.ExecContext(ctx, `INSERT INTO ridu_preferences
  (collection_id, user_id, key, value_json, updated_at)
VALUES ('users', 'missing', 'theme', '{}', 1)`)
	if foreignKeyError == nil || errors.Is(translateError(foreignKeyError), store.ErrConflict) {
		t.Fatalf("foreign-key error = %v, want unclassified integrity error", foreignKeyError)
	}
	_, checkError := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, values_json)
VALUES ('posts', 'invalid-json', 1, 1, 'not-json')`)
	if checkError == nil || errors.Is(translateError(checkError), store.ErrConflict) {
		t.Fatalf("check error = %v, want unclassified integrity error", checkError)
	}
}
