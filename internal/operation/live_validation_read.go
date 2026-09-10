package operation

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type liveContextKey struct{}
type liveEvaluationContext struct {
	locale schema.LocaleCode
	reads  atomic.Int32
}

// LiveRead is the advisory callback reader. It shares the original snapshot and
// actor but does not run lifecycle hooks, output resolvers, or fallback reads.
func (engine *Engine) LiveRead(ctx context.Context, request CapabilitiesRequest) (store.Document, error) {
	if request.ID == "" || len(request.ID) > 512 {
		return store.Document{}, liveBadRequest("reader requires a collection document ID")
	}
	result, err := engine.liveExecuteRead(ctx, Request{Operation: operation.Read, Collection: request.Collection, ID: request.ID, Actor: request.Actor, ActorCollection: request.ActorCollection, Locale: request.Locale, DisableFallback: engine.localization != nil})
	if err != nil {
		return store.Document{}, err
	}
	return *result.Document, nil
}

// Resource access rules retain their ordinary Local API Find/List lookups in
// this private mode. Its supported reads are exact-locale stored snapshots;
// lifecycle hooks, computed fields and population never execute implicitly.
func (engine *Engine) liveExecuteRead(ctx context.Context, request Request) (result Result, err error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if request.Operation != operation.Read {
		return result, transactionAdmissionError("advisory evaluation cannot mutate documents", errMutationInReadOnlyTransaction)
	}
	if request.IndexWindow != nil || len(request.Populate) > 0 || len(request.OutputFields) > 0 || request.AllLocales || len(request.FallbackLocales) > 0 || request.TrashOnly || request.internalFilter != nil || request.Limit > 100 || len(request.ID) > 512 {
		return result, liveBadRequest("advisory access lookups support exact-locale stored Find/List reads of at most 100 documents without population or computed output")
	}
	live, _ := ctx.Value(liveContextKey{}).(*liveEvaluationContext)
	if live != nil && request.Locale != "" && request.Locale != string(live.locale) {
		return result, liveBadRequest("advisory reader cannot change the exact locale")
	}
	if live != nil {
		request.Locale = string(live.locale)
	}
	selection, localeError := localization.Resolve(engine.localization, request.Locale, nil, engine.localization != nil, false)
	if localeError != nil {
		return result, &Error{Code: "bad_locale", Status: 400, Message: "reader locale is unavailable", Cause: localeError}
	}
	if selection.All {
		return result, liveBadRequest("advisory reader requires one exact locale")
	}
	selection = exactUpdateSelection(selection)
	if live == nil {
		live = &liveEvaluationContext{locale: selection.Locale}
		ctx = context.WithValue(ctx, liveContextKey{}, live)
	}
	if live.reads.Add(1) > 128 {
		return result, liveBadRequest("advisory evaluation exceeds its read lookup limit")
	}
	stack, _ := ctx.Value(operationStackKey{}).([]operationFrame)
	frame := operationFrame{kind: operation.Read, collection: request.Collection, id: request.ID}
	if len(stack) >= engine.maxDepth || repeatedFrame(stack, frame) >= 4 {
		return result, &Error{Code: "operation_recursion", Status: 409, Message: "operation recursion limit exceeded"}
	}
	ctx = context.WithValue(ctx, operationStackKey{}, append(append([]operationFrame(nil), stack...), frame))
	collection, exists := engine.collections[request.Collection]
	if !exists {
		return result, &Error{Code: "unknown_collection", Status: 404, Message: "collection was not found"}
	}
	if collection.Schema.Capabilities.Global && request.ID == "" {
		return result, liveBadRequest("global reads require the singleton ID")
	}
	if err := authorizeQuery(collection, request.Filter, request.Sort); err != nil {
		return result, err
	}
	state, owns, beginError := engine.transaction(ctx, transactionReadOnly)
	if beginError != nil {
		return result, transactionAdmissionError("begin advisory read", beginError)
	}
	ctx = context.WithValue(ctx, transactionKey{}, state)
	if owns {
		defer func() {
			if rollback := engine.rollbackTransactionState(ctx, state); rollback != nil {
				err = rollbackOperationError("rollback advisory read", err, rollback)
			}
		}()
	}
	base := Context{LiveValidation: true, Context: ctx, Operation: operation.Read, Collection: collection.Schema, ID: request.ID, Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection, Locale: selection.Locale, Locales: selection.Configured}
	decision, accessError := authorize(collection, base)
	if accessError != nil {
		return result, capabilityAccessError("reader access rule failed", accessError)
	}
	if decision.Kind == Deny {
		return result, liveDenied()
	}
	storeRequest := store.Request{Collection: collection.Schema, Collections: engine.schemas, ID: request.ID, Access: decision.Access, Deletion: store.DeletionActive, Locales: selection.Configured, LocaleChain: selection.Chain, PublishedOnly: publishedOnly(request, collection.Schema), Page: request.Page, Limit: request.Limit, Sort: append([]query.Sort(nil), request.Sort...)}
	if request.Filter != nil {
		node := request.Filter.Node()
		storeRequest.Filter = &node
	}
	if request.ID != "" {
		document, findError := state.transaction.Find(ctx, storeRequest)
		if findError != nil {
			if !(collection.Schema.Capabilities.Global && decision.Kind == Allow && errors.Is(findError, store.ErrNotFound)) {
				return result, translateStoreError(findError)
			}
			document = store.Document{ID: request.ID, Values: store.Values{}}
		}
		result.Document = &document
	} else {
		page, listError := state.transaction.List(ctx, storeRequest)
		if listError != nil {
			return result, translateStoreError(listError)
		}
		result.Page = &page
	}
	project := func(document *store.Document) error {
		projected, err := localization.ProjectDocumentChecked(*document, collection.Schema.Fields, selection)
		if err != nil {
			return liveOperational(err)
		}
		*document = projected
		if err := redactBoundFields(collection, base, document); err != nil {
			return capabilityAccessError("reader field access rule failed", err)
		}
		projectDocumentValues(document, request.Select)
		return nil
	}
	if result.Document != nil {
		if err := project(result.Document); err != nil {
			return Result{}, err
		}
	}
	if result.Page != nil {
		for i := range result.Page.Documents {
			if err := project(&result.Page.Documents[i]); err != nil {
				return Result{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
