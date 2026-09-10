package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ForceUnlockAuth clears an auth document's private login lockout state in
// the same transaction that evaluates and applies its update access predicate.
// Unlike ordinary CRUD, an omitted update rule fails closed for this privileged
// account-management action.
func (engine *Engine) ForceUnlockAuth(ctx context.Context, collectionName, documentID string, actor *store.Document, actorCollection schema.CollectionSlug) (err error) {
	collection, exists := engine.collections[collectionName]
	if !exists || collection.Schema.Auth == nil || collection.Schema.Auth.MaxLoginAttempts == 0 {
		return &Error{Code: "unknown_auth_collection", Status: 404, Message: fmt.Sprintf("auth collection %q with lockout was not found", collectionName)}
	}
	if collection.Access[operation.Update] == nil {
		return &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	state, ownsTransaction, transactionError := engine.transaction(ctx, transactionWrite)
	if transactionError != nil {
		return transactionAdmissionError("begin account unlock transaction", transactionError)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if !state.finished {
				if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
					err = rollbackOperationError("rollback account unlock transaction", err, rollbackError)
				}
			}
		}()
	}
	operationContext := Context{
		Context: transactionContext, Operation: operation.Update, Collection: collection.Schema, ID: documentID,
		Actor: cloneDocumentPointer(actor), ActorCollection: actorCollection, Data: store.Values{},
	}
	decision, accessError := authorize(collection, operationContext)
	if accessError != nil {
		return &Error{Code: "access_failed", Status: 500, Message: "account unlock access rule failed", Cause: accessError}
	}
	if decision.Kind == Deny {
		return &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	if _, findError := state.transaction.Find(transactionContext, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: documentID,
		Access: decision.Access, Deletion: store.DeletionActive, Lock: store.LockMutation,
	}); findError != nil {
		if errors.Is(findError, store.ErrNotFound) {
			return &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
		}
		return &Error{Code: "store_failed", Status: 500, Message: "authorize account unlock", Cause: findError}
	}
	unlocker, ok := state.transaction.(store.AuthUnlockTransaction)
	if !ok {
		return &Error{Code: "store_failed", Status: 500, Message: "store transaction does not support account unlock"}
	}
	if unlockError := unlocker.ForceUnlockAuth(transactionContext, collection.Schema.ID, documentID); unlockError != nil {
		if errors.Is(unlockError, store.ErrNotFound) {
			return &Error{Code: "not_found", Status: 404, Message: "auth account was not found"}
		}
		return &Error{Code: "store_failed", Status: 500, Message: "unlock account", Cause: unlockError}
	}
	if ownsTransaction {
		if commitError := engine.commitTransaction(ctx, state); commitError != nil {
			return commitAttemptedError(commitError)
		}
	}
	return nil
}
