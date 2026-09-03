package operation

import (
	"context"
	"time"

	"github.com/riducms/ridu/store"
)

const rollbackTimeout = 5 * time.Second

// rollbackTransaction must outlive a canceled request context. PostgreSQL
// cannot return a connection to the pool cleanly until ROLLBACK completes;
// repeatedly attempting cleanup with an already-canceled context churns the
// pool precisely when timeouts are elevated.
func rollbackTransaction(ctx context.Context, transaction store.Transaction) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	return transaction.Rollback(cleanupContext)
}
