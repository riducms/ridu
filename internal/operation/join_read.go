package operation

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
)

// ListJoin keeps configured inverse-relation membership out of caller query
// admission. The source document and join field must be readable, and the target
// still runs through Execute with its own collection and field access rules.
func (engine *Engine) ListJoin(ctx context.Context, collection, id, field string, request Request) (result Result, err error) {
	source, exists := engine.collections[collection]
	if !exists || source.Schema.Capabilities.Global {
		return Result{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", collection)}
	}
	join := configuredJoinField(source.Schema, field)
	if join == nil || join.Join == nil || id == "" {
		return Result{}, &Error{Code: "bad_request", Status: 400, Message: "join read requires a source document and configured join field"}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return Result{}, transactionAdmissionError("begin join read transaction", err)
	}
	ctx = context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback join read transaction", err, rollbackError)
			}
		}()
	}
	// Use ordinary document redaction, including value-aware rules on the join
	// itself. A capabilities check on an unresolved join cannot establish this.
	visible, err := engine.Execute(ctx, Request{
		Operation: operation.Read, Collection: collection, ID: id,
		Actor: request.Actor, ActorCollection: request.ActorCollection,
		OutputFields: []query.Path{join.Path},
		Locale:       request.Locale, FallbackLocales: request.FallbackLocales,
		DisableFallback: request.DisableFallback, AllLocales: request.AllLocales,
	})
	if err != nil {
		return Result{}, err
	}
	if visible.Document == nil {
		return Result{}, &Error{Code: "not_found", Status: 404, Message: "join source was not found"}
	}
	if _, readable := visible.Document.Values[join.Name]; !readable {
		return Result{}, &Error{Code: "field_access_denied", Status: 403, Message: "join field is not readable"}
	}
	if request.Limit <= 0 || join.Join.Limit > 0 && request.Limit > join.Join.Limit {
		request.Limit = join.Join.Limit
	}
	request.Operation, request.Collection, request.ID = operation.Read, string(join.Join.CollectionID), ""
	filter := query.Equal(join.Join.On, query.String(id)).Node()
	request.internalFilter = &filter
	return engine.Execute(ctx, request)
}
