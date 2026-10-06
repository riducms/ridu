package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func openPipelineStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	backend, err := OpenWithConfig(t.Context(), PoolConfig{
		DatabaseURL: databaseURL, AllowInsecureTransport: true, ApplicationName: "ridu-pipeline-test", MaxConnections: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	return backend
}

// requireReleased proves the pool holds no acquired connection and no
// server session of this pool is left inside a transaction. The pool closes a
// discarded connection asynchronously, so both conditions are awaited.
func requireReleased(t *testing.T, backend *Store) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		acquired := backend.pool.Stat().AcquiredConns()
		open := 0
		if acquired == 0 {
			if err := backend.pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity
WHERE application_name = 'ridu-pipeline-test' AND pid <> pg_backend_pid() AND state <> 'idle'`).Scan(&open); err != nil {
				t.Fatal(err)
			}
			if open == 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("acquired connections = %d and %d pipeline-test sessions are not idle", acquired, open)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func beginPipelineTest(t *testing.T, backend *Store, begin string) *pipelinedTransaction {
	t.Helper()
	transaction, err := beginPipelined(t.Context(), backend.pool, begin)
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func TestPipelinedTransactionWithoutStatementsNeverBegins(t *testing.T) {
	backend := openPipelineStore(t)
	for _, finish := range []func(*pipelinedTransaction) error{
		func(transaction *pipelinedTransaction) error { return transaction.Commit(t.Context()) },
		func(transaction *pipelinedTransaction) error { return transaction.Rollback(t.Context()) },
	} {
		transaction := beginPipelineTest(t, backend, beginReadCommitted)
		if status := transaction.connection.Conn().PgConn().TxStatus(); status != 'I' {
			t.Fatalf("transaction status before a statement = %q, want idle", status)
		}
		if err := finish(transaction); err != nil {
			t.Fatal(err)
		}
		requireReleased(t, backend)
	}
}

func TestPipelinedTransactionFirstStatementJoinsTheTransaction(t *testing.T) {
	backend := openPipelineStore(t)
	first := map[string]func(*pipelinedTransaction) (string, error){
		"exec": func(transaction *pipelinedTransaction) (string, error) {
			_, err := transaction.Exec(t.Context(), `CREATE TEMPORARY TABLE ridu_pipeline_probe (value integer) ON COMMIT DROP`)
			return "", err
		},
		"query": func(transaction *pipelinedTransaction) (string, error) {
			rows, err := transaction.Query(t.Context(), `SELECT current_setting('transaction_isolation') FROM generate_series(1, 3)`)
			if err != nil {
				return "", err
			}
			defer rows.Close()
			var isolation string
			count := 0
			for rows.Next() {
				if err := rows.Scan(&isolation); err != nil {
					return "", err
				}
				count++
			}
			if count != 3 {
				return "", errors.New("query lost rows")
			}
			return isolation, rows.Err()
		},
		"query-row": func(transaction *pipelinedTransaction) (string, error) {
			var isolation string
			err := transaction.QueryRow(t.Context(), `SELECT current_setting('transaction_isolation')`).Scan(&isolation)
			return isolation, err
		},
		"send-batch": func(transaction *pipelinedTransaction) (string, error) {
			batch := &pgx.Batch{}
			batch.Queue(`SELECT current_setting('transaction_isolation')`)
			batch.Queue(`SELECT current_setting('transaction_read_only')`)
			results := transaction.SendBatch(t.Context(), batch)
			var isolation, readOnly string
			if err := results.QueryRow().Scan(&isolation); err != nil {
				_ = results.Close()
				return "", err
			}
			if err := results.QueryRow().Scan(&readOnly); err != nil {
				_ = results.Close()
				return "", err
			}
			return isolation + "/" + readOnly, results.Close()
		},
	}
	for name, run := range first {
		t.Run(name, func(t *testing.T) {
			begin := beginRepeatableReadOnly
			if name == "exec" {
				begin = beginReadCommitted
			}
			transaction := beginPipelineTest(t, backend, begin)
			observed, err := run(transaction)
			if err != nil {
				t.Fatal(err)
			}
			if name != "exec" && !strings.HasPrefix(observed, "repeatable read") {
				t.Fatalf("first statement ran outside the snapshot: %q", observed)
			}
			if name == "send-batch" && observed != "repeatable read/on" {
				t.Fatalf("batch ran outside the read-only snapshot: %q", observed)
			}
			if status := transaction.connection.Conn().PgConn().TxStatus(); status != 'T' {
				t.Fatalf("transaction status after first statement = %q, want in transaction", status)
			}
			if name == "exec" {
				// The temporary table exists only inside this transaction.
				if _, err := transaction.Exec(t.Context(), `INSERT INTO ridu_pipeline_probe VALUES (1)`); err != nil {
					t.Fatalf("second statement left the transaction: %v", err)
				}
			}
			var started, later time.Time
			if err := transaction.QueryRow(t.Context(), `SELECT now()`).Scan(&started); err != nil {
				t.Fatal(err)
			}
			if err := transaction.QueryRow(t.Context(), `SELECT now()`).Scan(&later); err != nil {
				t.Fatal(err)
			}
			if !later.Equal(started) {
				t.Fatalf("statements used different transactions: %v != %v", started, later)
			}
			if err := transaction.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			requireReleased(t, backend)
		})
	}
}

func TestPipelinedTransactionBeginFailureSkipsTheStatement(t *testing.T) {
	backend := openPipelineStore(t)
	if _, err := backend.pool.Exec(t.Context(), `CREATE TABLE IF NOT EXISTS ridu_pipeline_begin_probe (value integer)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = backend.pool.Exec(context.Background(), `DROP TABLE IF EXISTS ridu_pipeline_begin_probe`)
	})
	if _, err := backend.pool.Exec(t.Context(), `TRUNCATE ridu_pipeline_begin_probe`); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func(*pipelinedTransaction) error{
		"exec": func(transaction *pipelinedTransaction) error {
			_, err := transaction.Exec(t.Context(), `INSERT INTO ridu_pipeline_begin_probe VALUES (1)`)
			return err
		},
		"query-row": func(transaction *pipelinedTransaction) error {
			var value int
			return transaction.QueryRow(t.Context(), `INSERT INTO ridu_pipeline_begin_probe VALUES (1) RETURNING value`).Scan(&value)
		},
		"send-batch": func(transaction *pipelinedTransaction) error {
			batch := &pgx.Batch{}
			batch.Queue(`INSERT INTO ridu_pipeline_begin_probe VALUES (1)`)
			results := transaction.SendBatch(t.Context(), batch)
			_, err := results.Exec()
			return errors.Join(err, results.Close())
		},
	} {
		t.Run(name, func(t *testing.T) {
			transaction := beginPipelineTest(t, backend, "begin isolation level unknown")
			err := run(transaction)
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "42601" {
				t.Fatalf("first statement after failed BEGIN = %v, want the BEGIN syntax error", err)
			}
			if transaction.begun {
				t.Fatal("failed BEGIN marked the transaction begun")
			}
			if err := transaction.Rollback(t.Context()); err != nil {
				t.Fatalf("rollback after failed BEGIN: %v", err)
			}
			requireReleased(t, backend)
			var rows int
			if err := backend.pool.QueryRow(t.Context(), `SELECT count(*) FROM ridu_pipeline_begin_probe`).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if rows != 0 {
				t.Fatalf("statement after failed BEGIN wrote %d rows", rows)
			}
		})
	}
}

func TestPipelinedTransactionFirstStatementFailureAbortsTheTransaction(t *testing.T) {
	backend := openPipelineStore(t)
	for name, run := range map[string]func(*pipelinedTransaction) error{
		"exec": func(transaction *pipelinedTransaction) error {
			_, err := transaction.Exec(t.Context(), `SELECT 1 / (random() * 0)::integer`)
			return err
		},
		"query": func(transaction *pipelinedTransaction) error {
			rows, err := transaction.Query(t.Context(), `SELECT 1 / (random() * 0)::integer`)
			if err != nil {
				return err
			}
			for rows.Next() {
			}
			rows.Close()
			return rows.Err()
		},
		"query-row": func(transaction *pipelinedTransaction) error {
			var value int
			return transaction.QueryRow(t.Context(), `SELECT 1 / (random() * 0)::integer`).Scan(&value)
		},
		"send-batch": func(transaction *pipelinedTransaction) error {
			batch := &pgx.Batch{}
			batch.Queue(`SELECT 1 / (random() * 0)::integer`)
			batch.Queue(`SELECT 1`)
			results := transaction.SendBatch(t.Context(), batch)
			_, err := results.Exec()
			return errors.Join(err, results.Close())
		},
	} {
		for _, finish := range []string{"rollback", "commit"} {
			t.Run(name+"-"+finish, func(t *testing.T) {
				transaction := beginPipelineTest(t, backend, beginReadCommitted)
				var postgresError *pgconn.PgError
				if err := run(transaction); !errors.As(err, &postgresError) || postgresError.Code != "22012" {
					t.Fatalf("first statement error = %v, want division by zero", err)
				}
				if _, err := transaction.Exec(t.Context(), `SELECT 1`); !errors.As(err, &postgresError) || postgresError.Code != "25P02" {
					t.Fatalf("statement after failure = %v, want aborted transaction", err)
				}
				if finish == "commit" {
					if err := transaction.Commit(t.Context()); !errors.Is(err, pgx.ErrTxCommitRollback) {
						t.Fatalf("commit of aborted transaction = %v, want rollback", err)
					}
				} else if err := transaction.Rollback(t.Context()); err != nil {
					t.Fatal(err)
				}
				requireReleased(t, backend)
			})
		}
	}
}

func TestPipelinedTransactionCancellationReleasesTheConnection(t *testing.T) {
	backend := openPipelineStore(t)
	for _, begunFirst := range []bool{false, true} {
		transaction := beginPipelineTest(t, backend, beginReadCommitted)
		if begunFirst {
			if _, err := transaction.Exec(t.Context(), `SELECT 1`); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		_, err := transaction.Exec(ctx, `SELECT pg_sleep(5)`)
		cancel()
		if err == nil {
			t.Fatal("cancelled statement succeeded")
		}
		_ = transaction.Rollback(context.Background())
		requireReleased(t, backend)
		// The pool still serves new transactions after discarding the
		// interrupted connection.
		next := beginPipelineTest(t, backend, beginRepeatableReadOnly)
		var one int
		if err := next.QueryRow(t.Context(), `SELECT 1`).Scan(&one); err != nil || one != 1 {
			t.Fatalf("transaction after cancellation = %d, %v", one, err)
		}
		if err := next.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		requireReleased(t, backend)
	}
}

func TestPipelinedTransactionRejectsUseAfterItEnds(t *testing.T) {
	backend := openPipelineStore(t)
	for _, begun := range []bool{false, true} {
		transaction := beginPipelineTest(t, backend, beginReadCommitted)
		if begun {
			if _, err := transaction.Exec(t.Context(), `SELECT 1`); err != nil {
				t.Fatal(err)
			}
		}
		if err := transaction.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(t.Context(), `SELECT 1`); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("exec after commit = %v", err)
		}
		if _, err := transaction.Query(t.Context(), `SELECT 1`); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("query after commit = %v", err)
		}
		var one int
		if err := transaction.QueryRow(t.Context(), `SELECT 1`).Scan(&one); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("query row after commit = %v", err)
		}
		if err := transaction.SendBatch(t.Context(), &pgx.Batch{}).Close(); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("batch after commit = %v", err)
		}
		if err := transaction.Commit(t.Context()); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("second commit = %v", err)
		}
		if err := transaction.Rollback(t.Context()); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("rollback after commit = %v", err)
		}
		requireReleased(t, backend)
	}
}
