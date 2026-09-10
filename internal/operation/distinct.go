package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const distinctFrameKind operation.Kind = "distinct"

// DistinctRequest describes one access-checked local distinct read. The field
// itself remains deliberately limited by store.ValidateDistinctRequest.
type DistinctRequest struct {
	Collection      string
	Field           query.Path
	Filter          query.Expression
	Page            int
	Limit           int
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Draft           *bool
	TrashOnly       bool
	Locale          string
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
}

// Distinct reads unique scalar values through the collection read rule and a
// single atomic adapter query. It is intentionally separate from Execute: the
// initial contract is a Local API query, not a new document lifecycle or
// general aggregation operation.
func (engine *Engine) Distinct(ctx context.Context, request DistinctRequest) (result store.DistinctPage, err error) {
	if err := ctx.Err(); err != nil {
		return store.DistinctPage{}, err
	}
	stack, _ := ctx.Value(operationStackKey{}).([]operationFrame)
	frame := operationFrame{kind: distinctFrameKind, collection: request.Collection, id: request.Field.String()}
	if len(stack) >= engine.maxDepth || repeatedFrame(stack, frame) >= 4 {
		return store.DistinctPage{}, &Error{Code: "operation_recursion", Status: 409, Message: "operation recursion limit exceeded"}
	}
	ctx = context.WithValue(ctx, operationStackKey{}, append(append([]operationFrame(nil), stack...), frame))
	collection, exists := engine.collections[request.Collection]
	if !exists {
		return store.DistinctPage{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", request.Collection)}
	}
	if collection.Schema.Capabilities.Global {
		return store.DistinctPage{}, &Error{Code: "bad_operation", Status: 400, Message: "global singletons do not support distinct reads"}
	}
	if request.TrashOnly && !collection.Schema.Capabilities.Trash {
		return store.DistinctPage{}, &Error{Code: "bad_operation", Status: 400, Message: "trash distinct reads require a trash-enabled collection"}
	}
	if validationError := store.ValidateDistinctRequest(store.DistinctRequest{Collection: collection.Schema, Field: request.Field}); validationError != nil {
		return store.DistinctPage{}, &Error{Code: "bad_query", Status: 400, Message: validationError.Error(), Cause: validationError}
	}
	selection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, false)
	if localeError != nil {
		return store.DistinctPage{}, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return store.DistinctPage{}, transactionAdmissionError("begin distinct transaction", err)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback distinct transaction", err, rollbackError)
			}
		}()
	}

	operationContext := Context{
		Context: transactionContext, Operation: operation.Read, Collection: collection.Schema,
		Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection,
		Locale: selection.Locale, Locales: append([]schema.LocaleCode(nil), selection.Configured...),
	}
	decision, accessError := authorize(collection, operationContext)
	if accessError != nil {
		var operationError *Error
		if errors.As(accessError, &operationError) && operationError.Code == "operation_recursion" {
			return store.DistinctPage{}, operationError
		}
		return store.DistinctPage{}, &Error{Code: "access_failed", Status: 500, Message: "read access rule failed", Cause: accessError}
	}
	if decision.Kind == Deny {
		return store.DistinctPage{}, &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	if err := authorizeQuery(collection, request.Filter, nil, request.Field); err != nil {
		return store.DistinctPage{}, err
	}
	distinctTransaction, supported := state.transaction.(store.DistinctTransaction)
	if !supported {
		return store.DistinctPage{}, &Error{Code: "store_failed", Status: 500, Message: "distinct reads require store.DistinctTransaction"}
	}
	var filter *query.Node
	if request.Filter != nil {
		node := request.Filter.Node()
		filter = &node
	}
	deletion := store.DeletionActive
	if request.TrashOnly {
		deletion = store.DeletionTrash
	}
	result, err = distinctTransaction.Distinct(transactionContext, store.DistinctRequest{
		Collection: collection.Schema, Field: request.Field, Filter: filter, Access: decision.Access,
		Page: request.Page, Limit: request.Limit,
		PublishedOnly: publishedOnly(Request{Operation: operation.Read, Actor: request.Actor, Draft: request.Draft}, collection.Schema),
		Deletion:      deletion, Locales: selection.Configured, LocaleChain: selection.Chain,
	})
	if err != nil {
		return store.DistinctPage{}, translateStoreError(err)
	}
	return result, nil
}
