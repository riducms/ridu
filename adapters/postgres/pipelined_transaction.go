package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// documentConnection is the statement surface of one document transaction.
// Document transactions never open savepoints, copy rows, or use large
// objects, so the pipelined implementation provides exactly these methods.
type documentConnection interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
	Commit(context.Context) error
	Rollback(context.Context) error
}

const (
	beginReadCommitted      = "begin"
	beginRepeatableReadOnly = "begin isolation level repeatable read read only"
)

// pipelinedTransaction sends BEGIN in the same round trip as the
// transaction's first statement. PostgreSQL takes a transaction's snapshot at
// its first statement rather than at BEGIN, and every lock is taken by a
// statement, so deferring BEGIN changes no isolation or locking guarantee.
//
// BEGIN and the first statement travel as one extended-protocol pipeline
// ending in a single Sync. If BEGIN fails, PostgreSQL skips the statement and
// the connection returns to idle. If the statement fails, the transaction is
// open but aborted, exactly as after a failed statement in an ordinary
// transaction, and Rollback ends it. A transaction that issues no statement
// never contacts the server. Commit and Rollback always release the pooled
// connection; the pool destroys a connection that is closed, busy, or still
// inside a transaction, such as after a cancellation interrupted the protocol.
type pipelinedTransaction struct {
	connection *pgxpool.Conn
	begin      string
	begun      bool
	closed     bool
}

func beginPipelined(ctx context.Context, pool *pgxpool.Pool, begin string) (*pipelinedTransaction, error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &pipelinedTransaction{connection: connection, begin: begin}, nil
}

// sendWithBegin prepends BEGIN to batch and consumes its result. On success
// the returned results are positioned at the batch's first query and the
// caller must close them; on failure they are already closed.
func (transaction *pipelinedTransaction) sendWithBegin(ctx context.Context, batch *pgx.Batch) (pgx.BatchResults, error) {
	pipeline := &pgx.Batch{QueuedQueries: make([]*pgx.QueuedQuery, 0, len(batch.QueuedQueries)+1)}
	pipeline.Queue(transaction.begin)
	pipeline.QueuedQueries = append(pipeline.QueuedQueries, batch.QueuedQueries...)
	results := transaction.connection.SendBatch(ctx, pipeline)
	if _, err := results.Exec(); err != nil {
		// A failed BEGIN makes PostgreSQL skip the rest of the pipeline; Close
		// drains it and reports the same failure.
		_ = results.Close()
		return nil, err
	}
	transaction.begun = true
	return results, nil
}

func singleStatement(statement string, arguments []any) *pgx.Batch {
	batch := &pgx.Batch{}
	batch.Queue(statement, arguments...)
	return batch
}

func (transaction *pipelinedTransaction) Exec(ctx context.Context, statement string, arguments ...any) (pgconn.CommandTag, error) {
	if transaction.closed {
		return pgconn.CommandTag{}, pgx.ErrTxClosed
	}
	if transaction.begun {
		return transaction.connection.Exec(ctx, statement, arguments...)
	}
	results, err := transaction.sendWithBegin(ctx, singleStatement(statement, arguments))
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := results.Exec()
	closeErr := results.Close()
	if err != nil {
		return tag, err
	}
	return tag, closeErr
}

func (transaction *pipelinedTransaction) Query(ctx context.Context, statement string, arguments ...any) (pgx.Rows, error) {
	if transaction.closed {
		return nil, pgx.ErrTxClosed
	}
	if transaction.begun {
		return transaction.connection.Query(ctx, statement, arguments...)
	}
	results, err := transaction.sendWithBegin(ctx, singleStatement(statement, arguments))
	if err != nil {
		return nil, err
	}
	rows, err := results.Query()
	if err != nil {
		_ = results.Close()
		return nil, err
	}
	return &pipelinedRows{Rows: rows, results: results}, nil
}

func (transaction *pipelinedTransaction) QueryRow(ctx context.Context, statement string, arguments ...any) pgx.Row {
	if transaction.begun && !transaction.closed {
		return transaction.connection.QueryRow(ctx, statement, arguments...)
	}
	rows, err := transaction.Query(ctx, statement, arguments...)
	return pipelinedRow{rows: rows, err: err}
}

// SendBatch sends batch in the transaction; before the first statement BEGIN
// joins the same pipeline.
func (transaction *pipelinedTransaction) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	if transaction.closed {
		return failedBatch{err: pgx.ErrTxClosed}
	}
	if transaction.begun {
		return transaction.connection.SendBatch(ctx, batch)
	}
	results, err := transaction.sendWithBegin(ctx, batch)
	if err != nil {
		return failedBatch{err: err}
	}
	return results
}

func (transaction *pipelinedTransaction) Commit(ctx context.Context) error {
	if transaction.closed {
		return pgx.ErrTxClosed
	}
	transaction.closed = true
	defer transaction.connection.Release()
	if !transaction.begun {
		return nil
	}
	tag, err := transaction.connection.Exec(ctx, "commit")
	if err != nil {
		return err
	}
	if tag.String() == "ROLLBACK" {
		return pgx.ErrTxCommitRollback
	}
	return nil
}

func (transaction *pipelinedTransaction) Rollback(ctx context.Context) error {
	if transaction.closed {
		return pgx.ErrTxClosed
	}
	transaction.closed = true
	defer transaction.connection.Release()
	if !transaction.begun {
		return nil
	}
	_, err := transaction.connection.Exec(ctx, "rollback")
	return err
}

// pipelinedRows finishes the BEGIN pipeline once its result rows are
// consumed or closed, so the connection is free for the next statement.
type pipelinedRows struct {
	pgx.Rows
	results  pgx.BatchResults
	finished bool
	err      error
}

func (rows *pipelinedRows) finish() {
	if rows.finished {
		return
	}
	rows.finished = true
	rows.Rows.Close()
	rows.err = rows.results.Close()
}

func (rows *pipelinedRows) Next() bool {
	if rows.finished {
		return false
	}
	if rows.Rows.Next() {
		return true
	}
	rows.finish()
	return false
}

func (rows *pipelinedRows) Close() { rows.finish() }

func (rows *pipelinedRows) Err() error {
	if err := rows.Rows.Err(); err != nil {
		return err
	}
	return rows.err
}

// pipelinedRow scans the first row like pgx's QueryRow.
type pipelinedRow struct {
	rows pgx.Rows
	err  error
}

func (row pipelinedRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()
	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return pgx.ErrNoRows
	}
	if err := row.rows.Scan(destinations...); err != nil {
		return err
	}
	row.rows.Close()
	return row.rows.Err()
}

type failedBatch struct{ err error }

func (batch failedBatch) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, batch.err }
func (batch failedBatch) Query() (pgx.Rows, error)         { return nil, batch.err }
func (batch failedBatch) QueryRow() pgx.Row                { return pipelinedRow{err: batch.err} }
func (batch failedBatch) Close() error                     { return batch.err }

var _ documentConnection = (*pipelinedTransaction)(nil)
