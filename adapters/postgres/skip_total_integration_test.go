package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// A SkipTotal list must not count: its only document statement is the page,
// which reads one overflow row beyond the limit.
func TestPostgresSkipTotalListExecutesNoCount(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{field.Text("title"), field.Number("rank").Index()}, ridu.LocalizationConfig{})
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for index, title := range []string{"first", "second", "third"} {
		if _, err := write.Create(ctx, store.CreateRequest{
			Collection: collection, ID: title, Status: store.StatusPublished,
			Values: store.Values{"title": store.String(title), "rank": store.Number(float64(index))},
		}); err != nil {
			_ = write.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	recorder := &statementRecorder{}
	config := backend.pool.Config()
	config.ConnConfig.Tracer = recorder
	traced, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	original := backend.pool
	backend.pool = traced
	t.Cleanup(func() {
		backend.pool = original
		traced.Close()
	})

	rank := []query.Sort{query.Asc("rank")}
	for _, test := range []struct {
		name      string
		request   store.Request
		documents int
		next      bool
		counts    int
		readLimit int
	}{
		{name: "published first page", request: store.Request{Collection: collection, PublishedOnly: true, Sort: rank, Page: 1, Limit: 2, SkipTotal: true}, documents: 2, next: true, readLimit: 3},
		{name: "published last page", request: store.Request{Collection: collection, PublishedOnly: true, Sort: rank, Page: 2, Limit: 2, SkipTotal: true}, documents: 1, readLimit: 3},
		{name: "working page", request: store.Request{Collection: collection, Sort: rank, Page: 1, Limit: 3, SkipTotal: true}, documents: 3, readLimit: 4},
		{name: "counted page", request: store.Request{Collection: collection, PublishedOnly: true, Sort: rank, Page: 1, Limit: 2}, documents: 2, next: true, counts: 1, readLimit: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			read, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			recorder.reset()
			page, err := read.List(ctx, test.request)
			statements := recorder.take()
			if rollbackError := read.Rollback(ctx); err == nil {
				err = rollbackError
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Documents) != test.documents || page.HasNextPage != test.next || (page.Total == nil) != test.request.SkipTotal {
				t.Fatalf("page = documents %d next %t total %v, want %d/%t, total only when counted", len(page.Documents), page.HasNextPage, page.Total, test.documents, test.next)
			}
			counts, pages := 0, 0
			for _, statement := range statements {
				switch {
				case strings.Contains(statement.sql, "count("):
					counts++
				case strings.Contains(statement.sql, " LIMIT $"):
					pages++
					if limit := statement.args[len(statement.args)-2]; limit != test.readLimit {
						t.Fatalf("page statement LIMIT = %v, want %d", limit, test.readLimit)
					}
				}
			}
			if counts != test.counts || pages != 1 {
				t.Fatalf("list ran %d count and %d page statements, want %d and 1: %#v", counts, pages, test.counts, statements)
			}
		})
	}
}

type recordedStatement struct {
	sql  string
	args []any
}

// statementRecorder records every statement a pool sends, including each
// statement of a pipelined batch.
type statementRecorder struct {
	mu         sync.Mutex
	statements []recordedStatement
}

func (recorder *statementRecorder) record(sql string, args []any) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.statements = append(recorder.statements, recordedStatement{sql: sql, args: append([]any(nil), args...)})
}

func (recorder *statementRecorder) reset() { recorder.take() }

func (recorder *statementRecorder) take() []recordedStatement {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	statements := recorder.statements
	recorder.statements = nil
	return statements
}

func (recorder *statementRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	recorder.record(data.SQL, data.Args)
	return ctx
}

func (*statementRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (*statementRecorder) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

func (recorder *statementRecorder) TraceBatchQuery(_ context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	recorder.record(data.SQL, data.Args)
}

func (*statementRecorder) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}
