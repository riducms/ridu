package operation

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// CopyLocale copies readable localized values from one exact locale into a
// different locale through the lifecycle appropriate to the current status.
// Copying into a published document is itself a live edit, so it requires both
// update and publish access and runs publish hooks atomically.
func (engine *Engine) CopyLocale(ctx context.Context, collectionName, documentID string, source, target schema.LocaleCode, expectedRevision int, actor *store.Document, authorizationOptions ...LocalizationOptions) (document store.Document, err error) {
	actorCollection := schema.CollectionSlug("")
	if len(authorizationOptions) != 0 {
		actorCollection = authorizationOptions[len(authorizationOptions)-1].ActorCollection
	}
	if source == "" || target == "" || source == target {
		return store.Document{}, &Error{Code: "bad_locale", Status: 400, Message: "source and target must be different configured locales"}
	}
	collection, exists := engine.collections[collectionName]
	if !exists {
		return store.Document{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", collectionName)}
	}
	sourceSelection, err := localization.Resolve(engine.localization, string(source), nil, true, false)
	if err != nil {
		return store.Document{}, &Error{Code: "bad_locale", Status: 400, Message: err.Error(), Cause: err}
	}
	if _, err := localization.Resolve(engine.localization, string(target), nil, true, false); err != nil {
		return store.Document{}, &Error{Code: "bad_locale", Status: 400, Message: err.Error(), Cause: err}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionWrite)
	if err != nil {
		return store.Document{}, transactionAdmissionError("begin copy-locale transaction", err)
	}
	transaction := state.transaction
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if !state.finished {
				if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
					err = rollbackOperationError("rollback copy-locale transaction", err, rollbackError)
				}
			}
		}()
	}

	// Lock the access-visible source before reading values. Without this fence a
	// concurrent update can land between the source read and target write and be
	// silently overwritten by the copied structured value.
	readRequest := Request{
		Operation: Read, Collection: collectionName, ID: documentID, Actor: actor, ActorCollection: actorCollection,
		Locale: string(source), DisableFallback: true,
	}
	readContext := Context{
		Context: transactionContext, Operation: Read, Collection: collection.Schema,
		ID: documentID, Actor: cloneDocumentPointer(actor), ActorCollection: actorCollection, Data: store.Values{},
		Locale: sourceSelection.Locale, AllLocales: false,
		Locales: append([]schema.LocaleCode(nil), sourceSelection.Configured...),
	}
	readDecision, err := authorize(collection, readContext)
	if err != nil {
		return store.Document{}, &Error{Code: "access_failed", Status: 500, Message: "source read access rule failed", Cause: err}
	}
	if readDecision.Kind == Deny {
		return store.Document{}, &Error{Code: "access_denied", Status: 403, Message: "source document may not be read"}
	}
	if _, err := transaction.Find(transactionContext, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: documentID,
		Access: readDecision.Access, PublishedOnly: publishedOnly(readRequest, collection.Schema),
		Locales: sourceSelection.Configured, LocaleChain: sourceSelection.Chain, Lock: store.LockMutation,
	}); err != nil {
		return store.Document{}, translateStoreError(err)
	}
	read, err := engine.Execute(transactionContext, readRequest)
	if err != nil {
		return store.Document{}, err
	}
	if read.Document == nil {
		return store.Document{}, &Error{Code: "not_found", Status: 404, Message: "document was not found"}
	}
	if issues := localization.CopyLocaleIssues(collection.Schema.Fields, read.Document.Values); len(issues) != 0 {
		return store.Document{}, &Error{Code: "validation", Status: 422, Message: "localized rows require stable keys before they can be copied", Issues: issues}
	}
	values := localization.LocalizedValues(collection.Schema.Fields, read.Document.Values)
	operation := Update
	if collection.Schema.Versions != nil && read.Document.Status == store.StatusPublished {
		operation = Publish
	}
	updated, err := engine.Execute(transactionContext, Request{
		Operation: operation, Collection: collectionName, ID: documentID, Data: values,
		ExpectedRevision: expectedRevision, Actor: actor, ActorCollection: actorCollection, Locale: string(target), DisableFallback: true,
	})
	if err != nil {
		return store.Document{}, err
	}
	if updated.Document == nil {
		return store.Document{}, fmt.Errorf("operation engine returned no copied document")
	}
	if !ownsTransaction {
		return *updated.Document, nil
	}
	if err := engine.commitTransaction(ctx, state); err != nil {
		return store.Document{}, commitAttemptedError(err)
	}
	if err := engine.dispatchDeferredHooks(ctx, state.afterCommit); err != nil {
		return store.Document{}, committedHookError(err)
	}
	return *updated.Document, nil
}
