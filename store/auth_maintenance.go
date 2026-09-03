package store

import (
	"context"
	"fmt"
)

// MaxAuthPruneBatch is the largest expired-record batch one maintenance cycle
// may remove from each auth record family.
const MaxAuthPruneBatch = 500

// AuthPruneResult reports expired durable credentials removed by one bounded
// maintenance cycle.
type AuthPruneResult struct {
	Sessions int
	APIKeys  int
}

// Total returns the complete number of expired records removed by the cycle.
func (result AuthPruneResult) Total() int { return result.Sessions + result.APIKeys }

// ValidateAuthPruneBatch bounds direct adapter calls as well as framework
// worker configuration.
func ValidateAuthPruneBatch(limit int) error {
	if limit < 1 || limit > MaxAuthPruneBatch {
		return fmt.Errorf("auth prune batch limit must be between 1 and %d", MaxAuthPruneBatch)
	}
	return nil
}

// AuthMaintenanceStore optionally removes expired durable authentication state.
// Implementations must use their authoritative clock and remove at most limit
// sessions and at most limit API keys per call. Concurrent callers must not
// count or delete the same record twice, and active credentials must never be
// selected.
type AuthMaintenanceStore interface {
	PruneExpiredAuth(context.Context, int) (AuthPruneResult, error)
}
