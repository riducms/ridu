package operation

import (
	"context"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestRollbackTransactionOutlivesCanceledRequest(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	transaction := &rollbackContextRecorder{}
	if err := rollbackTransaction(requestContext, transaction); err != nil {
		t.Fatal(err)
	}
	if transaction.canceled {
		t.Fatal("rollback inherited the canceled request context")
	}
}

type rollbackContextRecorder struct {
	store.Transaction
	canceled bool
}

func (transaction *rollbackContextRecorder) Rollback(ctx context.Context) error {
	transaction.canceled = ctx.Err() != nil
	return nil
}
