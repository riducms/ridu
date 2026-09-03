package core

import (
	"context"
	"time"

	"github.com/riducms/ridu/store"
)

const transactionCleanupTimeout = 5 * time.Second

// rollbackTransaction gives cleanup a short lifetime independent of the
// request that caused it. A canceled request context must not prevent a store
// from returning its transaction resources to a pool.
func rollbackTransaction(ctx context.Context, transaction store.Transaction) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), transactionCleanupTimeout)
	defer cancel()
	return transaction.Rollback(cleanupContext)
}
