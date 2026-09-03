package postgres

import (
	"context"
	"time"
)

const transactionCleanupTimeout = 5 * time.Second

type postgresRollbacker interface {
	Rollback(context.Context) error
}

func rollbackPostgresTransaction(ctx context.Context, transaction postgresRollbacker) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionCleanupTimeout)
	defer cancel()
	return transaction.Rollback(cleanupContext)
}
