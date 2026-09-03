package core

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/store"
)

// PruneExpiredAuth removes one bounded batch of expired sessions and API keys.
// Production Execute workers call this automatically for stores that implement
// store.AuthMaintenanceStore; the explicit method is useful to dedicated worker
// processes and adapter conformance tests.
func (application *App) PruneExpiredAuth(ctx context.Context, limit int) (store.AuthPruneResult, error) {
	if err := store.ValidateAuthPruneBatch(limit); err != nil {
		return store.AuthPruneResult{}, err
	}
	if application.authMaintenance == nil {
		return store.AuthPruneResult{}, fmt.Errorf("store does not support expired authentication maintenance")
	}
	return application.authMaintenance.PruneExpiredAuth(ctx, limit)
}
