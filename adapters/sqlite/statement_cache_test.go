package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func openStatementCacheDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open(sqliteDriverName, filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database with cached statements: %v", err)
		}
	})
	if _, err := database.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 5; id++ {
		if _, err := database.Exec(`INSERT INTO items (id, name) VALUES (?, ?)`, id, fmt.Sprintf("item-%d", id)); err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func cachedConnection(t *testing.T, connection *sql.Conn) *statementCachingConn {
	t.Helper()
	var cache *statementCachingConn
	if err := connection.Raw(func(driverConnection any) error {
		cache = driverConnection.(*statementCachingConn)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return cache
}

func cachedStatementFor(t *testing.T, cache *statementCachingConn, query string) *cachedStatement {
	t.Helper()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	element, ok := cache.statements[query]
	if !ok {
		return nil
	}
	return element.Value.(*cachedStatement)
}

func TestStatementCacheReusesOneCompiledStatementPerQuery(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cache := cachedConnection(t, connection)
	const lookup = `SELECT name FROM items WHERE id = ?`
	var first *cachedStatement
	for id := 1; id <= 5; id++ {
		var name string
		if err := connection.QueryRowContext(ctx, lookup, id).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != fmt.Sprintf("item-%d", id) {
			t.Fatalf("lookup %d = %q", id, name)
		}
		entry := cachedStatementFor(t, cache, lookup)
		if entry == nil || entry.busy {
			t.Fatalf("lookup %d left cache entry %#v", id, entry)
		}
		if first == nil {
			first = entry
		} else if entry.statement != first.statement {
			t.Fatalf("lookup %d compiled a new statement", id)
		}
	}
}

func TestStatementCacheRunsANestedUseOfTheSameQuery(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	const all = `SELECT id FROM items ORDER BY id`
	rows, err := connection.QueryContext(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	// Close open rows before the connection, or a failure would wait on them forever.
	defer rows.Close()
	var outer []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		outer = append(outer, id)
		if len(outer) > 5 {
			t.Fatalf("outer rows restarted: %v", outer)
		}
		// The cached statement is still stepping the outer rows; reusing it would reset them.
		inner, err := connection.QueryContext(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		defer inner.Close()
		count := 0
		for inner.Next() {
			count++
		}
		if err := inner.Close(); err != nil {
			t.Fatal(err)
		}
		if count != 5 {
			t.Fatalf("nested query read %d rows, want 5", count)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(outer) != "[1 2 3 4 5]" {
		t.Fatalf("outer rows = %v", outer)
	}
}

func TestStatementCacheStaysBoundedAndKeepsStatementsInUse(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cache := cachedConnection(t, connection)
	const open = `SELECT id FROM items ORDER BY id`
	rows, err := connection.QueryContext(ctx, open)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("open query returned no rows")
	}
	for index := 0; index < sqliteStatementCacheSize+10; index++ {
		var value int
		if err := connection.QueryRowContext(ctx, fmt.Sprintf(`SELECT %d`, index)).Scan(&value); err != nil || value != index {
			t.Fatalf("distinct query %d = %d, %v", index, value, err)
		}
	}
	if entry := cachedStatementFor(t, cache, open); entry == nil || !entry.busy {
		t.Fatalf("statement with open rows was evicted: %#v", entry)
	}
	read := 1
	for rows.Next() {
		read++
	}
	if err := rows.Close(); err != nil || read != 5 {
		t.Fatalf("open rows after eviction read %d rows, %v", read, err)
	}
	var value int
	if err := connection.QueryRowContext(ctx, `SELECT 0`).Scan(&value); err != nil || value != 0 {
		t.Fatalf("evicted query = %d, %v", value, err)
	}
	cache.mu.Lock()
	size := cache.recent.Len()
	cache.mu.Unlock()
	if size > sqliteStatementCacheSize {
		t.Fatalf("cache holds %d statements, bound is %d", size, sqliteStatementCacheSize)
	}
}

func TestStatementCacheFollowsSchemaChangesFromAnotherConnection(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	reader, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	const everything = `SELECT * FROM items WHERE id = 1`
	columns := func() int {
		rows, err := reader.QueryContext(ctx, everything)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		names, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		return len(names)
	}
	if got := columns(); got != 2 {
		t.Fatalf("columns before change = %d", got)
	}
	writer, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `ALTER TABLE items ADD COLUMN extra TEXT`); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if got := columns(); got != 3 {
		t.Fatalf("cached statement kept the old schema: %d columns", got)
	}
}

func TestStatementCacheReusesAStatementAfterItFails(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	const insert = `INSERT INTO items (id, name) VALUES (?, ?)`
	if _, err := connection.ExecContext(ctx, insert, 6, "item-1"); err == nil {
		t.Fatal("duplicate name was accepted")
	}
	if _, err := connection.ExecContext(ctx, insert, 6, "item-6"); err != nil {
		t.Fatalf("statement after a failed execution: %v", err)
	}
	var count int
	if err := connection.QueryRowContext(ctx, `SELECT count(*) FROM items`).Scan(&count); err != nil || count != 6 {
		t.Fatalf("items = %d, %v", count, err)
	}
}

// paddedQuery returns SELECT value with a comment that brings its SQL text to size bytes.
func paddedQuery(value, size int) string {
	query := fmt.Sprintf("SELECT %d /*", value)
	return query + strings.Repeat("x", size-len(query)-2) + "*/"
}

func TestStatementCacheLeavesLargeStatementsUncached(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cache := cachedConnection(t, connection)
	large := paddedQuery(7, sqliteStatementCacheMaxQueryBytes+1)
	for attempt := 0; attempt < 2; attempt++ {
		var value int
		if err := connection.QueryRowContext(ctx, large).Scan(&value); err != nil || value != 7 {
			t.Fatalf("large statement = %d, %v", value, err)
		}
	}
	if entry := cachedStatementFor(t, cache, large); entry != nil {
		t.Fatal("a statement over the size limit was cached")
	}
}

func TestStatementCacheKeepsItsSQLWithinBudget(t *testing.T) {
	ctx := context.Background()
	database := openStatementCacheDatabase(t)
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cache := cachedConnection(t, connection)
	size := sqliteStatementCacheMaxQueryBytes - 100
	for index := 0; index < 2*sqliteStatementCacheBudgetBytes/size; index++ {
		var value int
		if err := connection.QueryRowContext(ctx, paddedQuery(index, size)).Scan(&value); err != nil || value != index {
			t.Fatalf("statement %d = %d, %v", index, value, err)
		}
	}
	cache.mu.Lock()
	queryBytes, count := cache.queryBytes, cache.recent.Len()
	total := 0
	for element := cache.recent.Front(); element != nil; element = element.Next() {
		total += len(element.Value.(*cachedStatement).query)
	}
	cache.mu.Unlock()
	if queryBytes != total || queryBytes > sqliteStatementCacheBudgetBytes || count == 0 {
		t.Fatalf("cache holds %d statements with %d bytes of SQL (tracked %d), budget %d", count, total, queryBytes, sqliteStatementCacheBudgetBytes)
	}
}
