package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riducms/ridu/store"
)

func TestTranslateErrorClassifiesRetryableTransactionAbortsAsConflicts(t *testing.T) {
	for _, code := range []string{"23505", "40001", "40P01"} {
		t.Run(code, func(t *testing.T) {
			err := translateError(&pgconn.PgError{Code: code})
			if !errors.Is(err, store.ErrConflict) {
				t.Fatalf("SQLSTATE %s translated to %v", code, err)
			}
		})
	}

	original := &pgconn.PgError{Code: "23503"}
	if err := translateError(original); err != original {
		t.Fatalf("unclassified PostgreSQL error = %v, want original", err)
	}
}
