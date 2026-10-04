package operation

import (
	"context"
	"errors"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
)

// ReadUploadOwner proves object membership through ordinary collection read
// access. Object keys and configured size keys are framework-owned lookup
// constraints, even when their metadata fields are hidden from document reads.
func (engine *Engine) ReadUploadOwner(ctx context.Context, key string, request Request) (Result, error) {
	collection, exists := engine.collections[request.Collection]
	if !exists || collection.Schema.Upload == nil {
		return Result{}, &Error{Code: "not_found", Status: 404, Message: "upload collection was not found"}
	}
	predicates := []query.Expression{query.Equal("objectKey", key)}
	for _, size := range collection.Schema.Upload.ImageSizes {
		sizeKey, err := query.NewPath("sizes", size.Name, "objectKey")
		if err != nil {
			return Result{}, err
		}
		predicates = append(predicates, query.Equal(sizeKey, key))
	}
	filter := query.Or(predicates...).Node()
	request.Operation, request.ID, request.Limit = operation.Read, "", 1
	request.internalFilter = &filter
	return engine.Execute(ctx, request)
}

// ReadUploadMetadata returns canonical storage metadata only to the upload
// coordinator. Update combines read and update predicates; duplicate uses the
// same source-head and retained-locale read decisions as Execute. Response
// field redaction must never substitute a rendition for the private source.
func (engine *Engine) ReadUploadMetadata(ctx context.Context, request Request) (document store.Document, err error) {
	collection, exists := engine.collections[request.Collection]
	if !exists || collection.Schema.Upload == nil {
		return store.Document{}, &Error{Code: "not_found", Status: 404, Message: "upload collection was not found"}
	}
	selection, err := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if err != nil {
		return store.Document{}, &Error{Code: "bad_locale", Status: 400, Message: err.Error(), Cause: err}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return store.Document{}, transactionAdmissionError("begin upload metadata transaction", err)
	}
	if ownsTransaction {
		defer func() {
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback upload metadata transaction", err, rollbackError)
			}
		}()
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	operationContext := Context{
		Context: transactionContext, Collection: collection.Schema, ID: request.ID,
		Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection, System: request.System,
		Locale: selection.Locale, AllLocales: selection.All,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...),
	}
	var access *query.Node
	permissions := []operation.Kind{operation.Read}
	if request.Operation == operation.Update {
		permissions = append(permissions, operation.Update)
	}
	for _, kind := range permissions {
		operationContext.Operation = kind
		if kind == operation.Update {
			operationContext.Data = store.CloneValues(request.Data)
		}
		decision, accessError := authorize(collection, operationContext)
		if accessError != nil {
			return store.Document{}, capabilityAccessError("upload access rule failed", accessError)
		}
		if decision.Kind == Deny {
			return store.Document{}, &Error{Code: "access_denied", Status: 403, Message: "editing this upload is not permitted"}
		}
		if decision.Access != nil {
			if access == nil {
				access = decision.Access
			} else {
				access = &query.Node{Kind: query.ExpressionAnd, Children: []query.Node{*access, *decision.Access}}
			}
		}
	}
	publishedOnly := false
	if request.Operation == operation.Duplicate {
		publishedOnly, err = engine.duplicateSourcePublishedOnly(collection, operationContext, selection.Configured)
		if err != nil {
			return store.Document{}, err
		}
	}
	document, err = state.transaction.Find(transactionContext, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: request.ID,
		Access: access, Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
		PublishedOnly: publishedOnly,
	})
	if err != nil {
		return store.Document{}, translateStoreError(err)
	}
	if request.Operation == operation.Duplicate {
		for _, locale := range selection.Configured {
			localeContext := operationContext
			localeContext.Operation, localeContext.Data = operation.Read, store.Values{}
			localeContext.Locale, localeContext.AllLocales = locale, true
			decision, accessError := authorize(collection, localeContext)
			if accessError != nil {
				return store.Document{}, &Error{Code: "access_failed", Status: 500, Message: "source locale read access rule failed", Cause: accessError}
			}
			if decision.Kind == Deny {
				return store.Document{}, &Error{Code: "access_denied", Status: 403, Message: "every retained source locale must be readable"}
			}
			if _, accessError := state.transaction.Find(transactionContext, store.Request{
				Collection: collection.Schema, Collections: engine.schemas, ID: request.ID, Access: decision.Access,
				Locales: selection.Configured, LocaleChain: []schema.LocaleCode{locale}, PublishedOnly: publishedOnly,
			}); accessError != nil {
				if errors.Is(accessError, store.ErrNotFound) {
					return store.Document{}, &Error{Code: "access_denied", Status: 403, Message: "every retained source locale must be readable"}
				}
				return store.Document{}, translateStoreError(accessError)
			}
		}
	}
	return document, nil
}

// duplicateSourcePublishedOnly is shared by the storage preparation and the
// operation engine, so both copy and revision fencing refer to the same head.
func (engine *Engine) duplicateSourcePublishedOnly(collection Collection, readContext Context, locales []schema.LocaleCode) (bool, error) {
	readsDrafts, err := engine.readsDrafts(collection, readContext)
	if err != nil {
		return false, err
	}
	for _, locale := range locales {
		if !readsDrafts {
			break
		}
		localeContext := readContext
		localeContext.Locale, localeContext.AllLocales = locale, true
		readsDrafts, err = engine.readsDrafts(collection, localeContext)
		if err != nil {
			return false, err
		}
	}
	return !readsDrafts, nil
}
