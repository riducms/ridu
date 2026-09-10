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

type FilteredSelectionRequest struct {
	Collection      string
	Filter          query.Expression
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	TrashOnly       bool
	Locale          string
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

type FilteredSelectionItem struct {
	ID           string
	Capabilities AccessCapabilities
}

type FilteredSelectionResult struct {
	Items []FilteredSelectionItem
}

// ResolveFilteredSelection freezes one bounded, read-visible ID set and its
// exact per-document capabilities without running hooks or mutations.
func (engine *Engine) ResolveFilteredSelection(ctx context.Context, request FilteredSelectionRequest) (result FilteredSelectionResult, err error) {
	collection, exists := engine.collections[request.Collection]
	if !exists {
		return FilteredSelectionResult{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", request.Collection)}
	}
	localeSelection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if localeError != nil {
		return FilteredSelectionResult{}, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	if request.TrashOnly && !collection.Schema.Capabilities.Trash {
		return FilteredSelectionResult{}, &Error{Code: "bad_operation", Status: 400, Message: "trash selection requires a trash-enabled collection"}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return FilteredSelectionResult{}, transactionAdmissionError("begin filtered selection transaction", err)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback filtered selection transaction", err, rollbackError)
			}
		}()
	}

	operationContext := Context{
		Context: transactionContext, Operation: operation.Read, Collection: collection.Schema,
		Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection, Locale: localeSelection.Locale, AllLocales: localeSelection.All,
		Locales: append([]schema.LocaleCode(nil), localeSelection.Configured...),
	}
	readDecision, accessError := authorize(collection, operationContext)
	if accessError != nil {
		return FilteredSelectionResult{}, capabilityAccessError("read access rule failed", accessError)
	}
	if readDecision.Kind == Deny {
		return FilteredSelectionResult{Items: []FilteredSelectionItem{}}, nil
	}
	if err := authorizeQuery(collection, request.Filter, nil); err != nil {
		return FilteredSelectionResult{}, err
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
	selection, selectionError := state.transaction.ResolveFilteredSelection(transactionContext, store.FilteredSelectionRequest{
		Collection: collection.Schema,
		Filter:     filter,
		Access:     readDecision.Access,
		Deletion:   deletion,
		Limit:      MaxBatchDocuments,
		Locales:    localeSelection.Configured, LocaleChain: localeSelection.Chain, AllLocales: localeSelection.All,
	})
	if selectionError != nil {
		if errors.Is(selectionError, context.Canceled) || errors.Is(selectionError, context.DeadlineExceeded) {
			return FilteredSelectionResult{}, selectionError
		}
		return FilteredSelectionResult{}, &Error{Code: "store_failed", Status: 500, Message: "resolve filtered selection", Cause: selectionError}
	}
	if selection.Overflow {
		return FilteredSelectionResult{}, &Error{
			Code: "selection_too_large", Status: 422,
			Message: fmt.Sprintf("filtered selection supports at most %d documents; narrow the current filters", MaxBatchDocuments),
		}
	}

	items := make([]FilteredSelectionItem, len(selection.IDs))
	for index, id := range selection.IDs {
		if err := ctx.Err(); err != nil {
			return FilteredSelectionResult{}, err
		}
		document, findError := state.transaction.Find(transactionContext, store.Request{
			Collection: collection.Schema, Collections: engine.schemas, ID: id,
			Filter: filter, Access: readDecision.Access, Deletion: deletion,
			Locales: localeSelection.Configured, LocaleChain: localeSelection.Chain, AllLocales: localeSelection.All,
		})
		if errors.Is(findError, store.ErrNotFound) {
			return FilteredSelectionResult{}, &Error{Code: "conflict", Status: 409, Message: "filtered selection changed while it was being resolved"}
		}
		if findError != nil {
			if errors.Is(findError, context.Canceled) || errors.Is(findError, context.DeadlineExceeded) {
				return FilteredSelectionResult{}, findError
			}
			return FilteredSelectionResult{}, &Error{Code: "store_failed", Status: 500, Message: "read filtered selection document", Cause: findError}
		}
		document = localization.ProjectDocument(document, collection.Schema.Fields, localeSelection)
		itemContext := operationContext
		itemContext.ID = id
		itemContext.Data = store.CloneValues(document.Values)
		capabilities, capabilityError := engine.operationCapabilities(state.transaction, collection, itemContext, &document, deletion, request.TrashOnly, localeSelection)
		if capabilityError != nil {
			return FilteredSelectionResult{}, capabilityError
		}
		fields, fieldError := fieldCapabilities(collection, itemContext, &document, capabilities)
		if fieldError != nil {
			return FilteredSelectionResult{}, fieldError
		}
		items[index] = FilteredSelectionItem{
			ID: id, Capabilities: AccessCapabilities{Operations: capabilities, Fields: fields},
		}
	}
	return FilteredSelectionResult{Items: items}, nil
}
