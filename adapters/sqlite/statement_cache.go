package sqlite

import (
	"container/list"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
)

// sqliteDriverName is the database/sql driver every Store opens. It wraps the modernc driver
// registered as "sqlite", whose connections carry the adapter's matcher function, so that each
// pooled connection keeps the statements it compiles. The modernc driver compiles and finalizes
// a fresh statement for every ExecContext and QueryContext call; deleting a document compiles
// a cascade program for every foreign key that references it, several times the cost of running
// the compiled statement.
const sqliteDriverName = "ridu-sqlite"

// The cache bounds what one connection retains by statement count and SQL text, since a compiled
// statement grows with its SQL: a filter with long `in` lists compiles to megabytes. The adapter's
// hot statements stay well under the per-statement limit; larger ones compile per call, as every
// statement did before, and a cache miss costs exactly that.
const (
	sqliteStatementCacheSize          = 64
	sqliteStatementCacheMaxQueryBytes = 4 << 10
	sqliteStatementCacheBudgetBytes   = 64 << 10
)

func init() {
	registered, err := sql.Open("sqlite", "")
	if err != nil {
		panic(err)
	}
	inner := registered.Driver()
	if err := registered.Close(); err != nil {
		panic(err)
	}
	sql.Register(sqliteDriverName, statementCachingDriver{inner: inner})
}

type statementCachingDriver struct{ inner driver.Driver }

func (d statementCachingDriver) Open(name string) (driver.Conn, error) {
	connection, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	for _, required := range []bool{
		implements[driver.ExecerContext](connection), implements[driver.QueryerContext](connection),
		implements[driver.ConnPrepareContext](connection), implements[driver.ConnBeginTx](connection),
	} {
		if !required {
			_ = connection.Close()
			return nil, errors.New("SQLite driver connection lacks a context-aware database/sql interface")
		}
	}
	return &statementCachingConn{Conn: connection, statements: make(map[string]*list.Element), recent: list.New()}, nil
}

func implements[T any](value any) bool {
	_, ok := value.(T)
	return ok
}

// statementCachingConn reuses one compiled statement per SQL text. database/sql uses a
// connection from one goroutine at a time; the mutex only guards the cache against the pool
// closing an idle connection.
type statementCachingConn struct {
	driver.Conn
	mu         sync.Mutex
	statements map[string]*list.Element
	recent     *list.List // front is most recently used
	queryBytes int        // SQL text of the cached statements
}

type cachedStatement struct {
	query     string
	statement driver.Stmt
	// busy marks a statement whose rows are still open. Reusing it would reset the cursor the
	// caller is reading, so a nested use of the same SQL text gets a statement of its own.
	busy bool
}

// acquire returns a statement for query and the function that ends this use of it.
func (c *statementCachingConn) acquire(ctx context.Context, query string) (driver.Stmt, func(), error) {
	c.mu.Lock()
	if element, cached := c.statements[query]; cached {
		entry := element.Value.(*cachedStatement)
		if !entry.busy {
			entry.busy = true
			c.recent.MoveToFront(element)
			c.mu.Unlock()
			return entry.statement, c.releaser(entry), nil
		}
		c.mu.Unlock()
		return c.prepareTransient(ctx, query)
	}
	c.mu.Unlock()
	if len(query) > sqliteStatementCacheMaxQueryBytes {
		return c.prepareTransient(ctx, query)
	}
	statement, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	entry := &cachedStatement{query: query, statement: statement, busy: true}
	c.mu.Lock()
	c.statements[query] = c.recent.PushFront(entry)
	c.queryBytes += len(query)
	evicted := c.evictLocked()
	c.mu.Unlock()
	closeStatements(evicted)
	return statement, c.releaser(entry), nil
}

func (c *statementCachingConn) prepareTransient(ctx context.Context, query string) (driver.Stmt, func(), error) {
	statement, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	return statement, func() { _ = statement.Close() }, nil
}

func (c *statementCachingConn) releaser(entry *cachedStatement) func() {
	return func() { c.release(entry) }
}

func (c *statementCachingConn) release(entry *cachedStatement) {
	c.mu.Lock()
	entry.busy = false
	c.mu.Unlock()
}

// evictLocked removes least recently used idle statements beyond the cache bounds. A statement
// with open rows stays until a later eviction finds it idle.
func (c *statementCachingConn) evictLocked() []driver.Stmt {
	var evicted []driver.Stmt
	for element := c.recent.Back(); element != nil && (c.recent.Len() > sqliteStatementCacheSize || c.queryBytes > sqliteStatementCacheBudgetBytes); {
		previous := element.Prev()
		if entry := element.Value.(*cachedStatement); !entry.busy {
			c.recent.Remove(element)
			delete(c.statements, entry.query)
			c.queryBytes -= len(entry.query)
			evicted = append(evicted, entry.statement)
		}
		element = previous
	}
	return evicted
}

// closeStatements finalizes statements the cache no longer holds. SQLite's finalize reports the
// error of the statement's last execution, which its caller already received, so it is ignored.
func closeStatements(statements []driver.Stmt) {
	for _, statement := range statements {
		_ = statement.Close()
	}
}

func (c *statementCachingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	statement, release, err := c.acquire(ctx, query)
	if err != nil {
		return nil, err
	}
	defer release()
	return statement.(driver.StmtExecContext).ExecContext(ctx, args)
}

func (c *statementCachingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	statement, release, err := c.acquire(ctx, query)
	if err != nil {
		return nil, err
	}
	rows, err := statement.(driver.StmtQueryContext).QueryContext(ctx, args)
	if err != nil {
		release()
		return nil, err
	}
	return &cachedStatementRows{Rows: rows, release: release}, nil
}

func (c *statementCachingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
}

func (c *statementCachingConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}

func (c *statementCachingConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *statementCachingConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *statementCachingConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

// Close finalizes the cached statements first: SQLite cannot close a connection that still has
// unfinalized statements.
func (c *statementCachingConn) Close() error {
	c.mu.Lock()
	statements := make([]driver.Stmt, 0, c.recent.Len())
	for element := c.recent.Front(); element != nil; element = element.Next() {
		statements = append(statements, element.Value.(*cachedStatement).statement)
	}
	c.statements, c.recent, c.queryBytes = map[string]*list.Element{}, list.New(), 0
	c.mu.Unlock()
	closeStatements(statements)
	return c.Conn.Close()
}

// cachedStatementRows hands the statement back once its rows are closed; modernc resets a
// prepared statement's cursor on Close instead of finalizing it.
type cachedStatementRows struct {
	driver.Rows
	release func()
	closed  bool
}

func (r *cachedStatementRows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.Rows.Close()
	r.release()
	return err
}
