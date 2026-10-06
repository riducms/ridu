package conformance

import (
	"context"

	"github.com/riducms/ridu/store"
)

// Updater is the update surface of a store transaction.
type Updater interface {
	Find(context.Context, store.Request) (store.Document, error)
	Update(context.Context, store.UpdateRequest) (store.Document, error)
}

// LockedUpdate performs a direct store update the way the operation engine
// does: it reads the update's document with Request.CurrentRequest and
// supplies that document as the required Current.
func LockedUpdate(ctx context.Context, transaction Updater, request store.UpdateRequest) (store.Document, error) {
	current, err := transaction.Find(ctx, request.Request.CurrentRequest())
	if err != nil {
		return store.Document{}, err
	}
	request.Current = &current
	return transaction.Update(ctx, request)
}
