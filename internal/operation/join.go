package operation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// JoinMutationRequest describes explicit inverse-relation changes. Additions
// and removals are deltas because a rendered join can be a limited subset of
// all matching target documents.
type JoinMutationRequest struct {
	Collection      string
	ID              string
	Field           string
	Additions       []string
	Removals        []string
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Locale          string
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

// JoinMutationResult reports the refreshed source document and applied deltas.
type JoinMutationResult struct {
	Document store.Document
	Added    int
	Removed  int
}

type joinTargetMutation struct {
	id             string
	addition       bool
	current        store.Document
	readAccess     *query.Node
	observedFilter query.Expression
	values         store.Values
	skip           bool
}

type joinMutationRead struct {
	document store.Document
	access   *query.Node
}

type joinMutationLock struct {
	collection    Collection
	id            string
	access        *query.Node
	filter        query.Expression
	publishedOnly bool
	conflict      bool
}

// MutateJoin applies inverse relation deltas through ordinary target updates
// in one transaction. Target access, field access, validation, hooks, versions,
// and after-commit dispatch remain owned by Execute.
func (engine *Engine) MutateJoin(ctx context.Context, request JoinMutationRequest) (result JoinMutationResult, err error) {
	selection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if localeError != nil {
		return JoinMutationResult{}, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Field) == "" {
		return JoinMutationResult{}, &Error{Code: "bad_request", Status: 400, Message: "join mutation requires a source document and field"}
	}
	source, exists := engine.collections[request.Collection]
	if !exists || source.Schema.Capabilities.Global {
		return JoinMutationResult{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", request.Collection)}
	}
	joinField := configuredJoinField(source.Schema, request.Field)
	if joinField == nil || joinField.Join == nil {
		return JoinMutationResult{}, &Error{Code: "not_found", Status: 404, Message: fmt.Sprintf("join field %q was not found", request.Field)}
	}
	target, exists := engine.collections[string(joinField.Join.CollectionID)]
	if !exists {
		return JoinMutationResult{}, &Error{Code: "store_failed", Status: 500, Message: "join target collection is unavailable"}
	}
	mutations, validationError := normalizedJoinMutations(request.Additions, request.Removals)
	if validationError != nil {
		return JoinMutationResult{}, validationError
	}
	state, ownsTransaction, beginError := engine.transaction(ctx, transactionWrite)
	if beginError != nil {
		return JoinMutationResult{}, transactionAdmissionError("begin join mutation transaction", beginError)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if !ownsTransaction {
		defer func() {
			if err != nil {
				state.markRollbackOnly(err)
			}
		}()
	}
	if ownsTransaction {
		defer func() {
			if state.finished {
				return
			}
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback join mutation transaction", err, rollbackError)
			}
		}()
	}

	visibleSource, readError := engine.Execute(transactionContext, Request{
		Operation: operation.Read, Collection: request.Collection, ID: request.ID, Actor: request.Actor, ActorCollection: request.ActorCollection,
		Locale: request.Locale, FallbackLocales: request.FallbackLocales, DisableFallback: request.DisableFallback, AllLocales: request.AllLocales,
	})
	if readError != nil {
		return JoinMutationResult{}, readError
	}
	if visibleSource.Document == nil {
		return JoinMutationResult{}, &Error{Code: "store_failed", Status: 500, Message: "join source read returned no document"}
	}
	if _, visible := valueAtPath(source.Schema.Fields, visibleSource.Document.Values, request.Field, selection.All); !visible {
		return JoinMutationResult{}, &Error{Code: "field_access_denied", Status: 403, Message: "join field may not be read"}
	}
	sourceRead, findError := engine.findJoinMutationDocument(transactionContext, state.transaction, source, request.ID, request.Actor, request.ActorCollection, selection)
	if findError != nil {
		return JoinMutationResult{}, findError
	}
	locks := []joinMutationLock{{
		collection: source, id: request.ID, access: sourceRead.access,
		publishedOnly: request.Actor == nil && source.Schema.Versions != nil,
	}}
	for index := range mutations {
		mutation := &mutations[index]
		targetRead, findError := engine.findJoinMutationDocument(transactionContext, state.transaction, target, mutation.id, request.Actor, request.ActorCollection, selection)
		if findError != nil {
			return JoinMutationResult{}, findError
		}
		mutation.current, mutation.readAccess = targetRead.document, targetRead.access
		observed, present := valueAtPath(target.Schema.Fields, mutation.current.Values, joinField.Join.On.String(), selection.All)
		currentSource, referencesSource := observed.StringValue()
		if mutation.addition && referencesSource && currentSource == request.ID {
			mutation.skip = true
			continue
		}
		if !mutation.addition && (!referencesSource || currentSource != request.ID) {
			mutation.skip = true
			continue
		}
		mutation.observedFilter = joinObservedValueFilter(joinField.Join.On, observed, present)
		value := store.Null()
		if mutation.addition {
			value = store.String(request.ID)
		}
		mutation.values = valuesAtJoinPath(joinField.Join.On, value)
		locks = append(locks, joinMutationLock{
			collection: target, id: mutation.id, access: mutation.readAccess,
			filter: mutation.observedFilter, publishedOnly: request.Actor == nil && target.Schema.Versions != nil, conflict: true,
		})
	}
	if lockError := engine.lockJoinMutationDocuments(transactionContext, state.transaction, locks, selection); lockError != nil {
		return JoinMutationResult{}, lockError
	}
	for _, mutation := range mutations {
		if mutation.skip {
			continue
		}
		if accessError := engine.preflightJoinTargetUpdate(transactionContext, state.transaction, target, mutation.current, mutation.values, request.Actor, request.ActorCollection, selection); accessError != nil {
			return JoinMutationResult{}, accessError
		}
		operationKind := operation.Update
		if target.Schema.Versions != nil && mutation.current.Status == store.StatusPublished {
			operationKind = operation.Publish
		}
		observedFilter := mutation.observedFilter.Node()
		update, updateError := engine.Execute(transactionContext, Request{
			Operation: operationKind, Collection: string(target.Schema.Slug), ID: mutation.id,
			Data: mutation.values, internalFilter: &observedFilter, Actor: request.Actor, ActorCollection: request.ActorCollection,
			Locale: request.Locale, FallbackLocales: request.FallbackLocales, DisableFallback: request.DisableFallback, AllLocales: request.AllLocales,
		})
		if updateError != nil {
			var operationError *Error
			if errors.As(updateError, &operationError) && operationError.Code == "not_found" {
				return JoinMutationResult{}, &Error{Code: "conflict", Status: 409, Message: "join target changed while relationships were being updated", Cause: updateError}
			}
			return JoinMutationResult{}, updateError
		}
		if update.Document == nil {
			return JoinMutationResult{}, &Error{Code: "store_failed", Status: 500, Message: "join target update returned no document"}
		}
		persisted, findError := state.transaction.Find(transactionContext, store.Request{
			Collection: target.Schema, Collections: engine.schemas, ID: mutation.id,
			Locales: append([]schema.LocaleCode(nil), selection.Configured...), LocaleChain: append([]schema.LocaleCode(nil), selection.Chain...), AllLocales: selection.All,
		})
		if findError != nil {
			return JoinMutationResult{}, translateStoreError(findError)
		}
		finalValue, _ := valueAtPath(target.Schema.Fields, persisted.Values, joinField.Join.On.String(), selection.All)
		finalSource, referencesSource := finalValue.StringValue()
		if mutation.addition && referencesSource && finalSource == request.ID {
			result.Added++
		} else if !mutation.addition && (!referencesSource || finalSource != request.ID) {
			result.Removed++
		}
	}
	refreshed, readError := engine.Execute(transactionContext, Request{
		Operation: operation.Read, Collection: request.Collection, ID: request.ID, Actor: request.Actor, ActorCollection: request.ActorCollection,
		Locale: request.Locale, FallbackLocales: request.FallbackLocales, DisableFallback: request.DisableFallback, AllLocales: request.AllLocales,
	})
	if readError != nil {
		return JoinMutationResult{}, readError
	}
	if refreshed.Document == nil {
		return JoinMutationResult{}, &Error{Code: "store_failed", Status: 500, Message: "join source refresh returned no document"}
	}
	result.Document = store.CloneDocument(*refreshed.Document)
	if ownsTransaction {
		if commitError := engine.commitTransaction(ctx, state); commitError != nil {
			return JoinMutationResult{}, commitAttemptedError(commitError)
		}
		if afterCommitError := engine.dispatchDeferredHooks(ctx, state.afterCommit); afterCommitError != nil {
			return JoinMutationResult{}, committedHookError(afterCommitError)
		}
	}
	return result, nil
}

func configuredJoinField(collection schema.Collection, path string) *schema.Field {
	for index := range collection.Fields {
		candidate := &collection.Fields[index]
		if candidate.Type == schema.FieldTypeJoin && candidate.Path.String() == path {
			return candidate
		}
	}
	return nil
}

func normalizedJoinMutations(additions, removals []string) ([]joinTargetMutation, *Error) {
	if len(additions)+len(removals) == 0 || len(additions)+len(removals) > 100 {
		return nil, &Error{Code: "bad_request", Status: 400, Message: "join mutations require between 1 and 100 target document IDs"}
	}
	seen := make(map[string]bool, len(additions)+len(removals))
	mutations := make([]joinTargetMutation, 0, len(additions)+len(removals))
	appendTargets := func(ids []string, addition bool) *Error {
		for _, id := range ids {
			if strings.TrimSpace(id) == "" || seen[id] {
				return &Error{Code: "bad_request", Status: 400, Message: "join target document IDs must be non-empty, unique, and present in only one delta"}
			}
			seen[id] = true
			mutations = append(mutations, joinTargetMutation{id: id, addition: addition})
		}
		return nil
	}
	if err := appendTargets(additions, true); err != nil {
		return nil, err
	}
	if err := appendTargets(removals, false); err != nil {
		return nil, err
	}
	sort.Slice(mutations, func(left, right int) bool { return mutations[left].id < mutations[right].id })
	return mutations, nil
}

func (engine *Engine) findJoinMutationDocument(ctx context.Context, transaction store.Transaction, collection Collection, id string, actor *store.Document, actorCollection schema.CollectionSlug, selection localization.Selection) (joinMutationRead, error) {
	operationContext := Context{Context: ctx, Operation: operation.Read, Collection: collection.Schema, ID: id, Actor: cloneDocumentPointer(actor), ActorCollection: actorCollection, Data: store.Values{}, Locale: selection.Locale, AllLocales: selection.All, Locales: append([]schema.LocaleCode(nil), selection.Configured...)}
	decision, err := authorize(collection, operationContext)
	if err != nil {
		return joinMutationRead{}, &Error{Code: "access_failed", Status: 500, Message: "join document access rule failed", Cause: err}
	}
	if decision.Kind == Deny {
		return joinMutationRead{}, &Error{Code: "access_denied", Status: 403, Message: "join document may not be read"}
	}
	document, err := transaction.Find(ctx, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: id,
		Access: decision.Access, PublishedOnly: actor == nil && collection.Schema.Versions != nil,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...), LocaleChain: append([]schema.LocaleCode(nil), selection.Chain...), AllLocales: selection.All,
	})
	if err != nil {
		return joinMutationRead{}, translateStoreError(err)
	}
	return joinMutationRead{document: document, access: decision.Access}, nil
}

func (engine *Engine) lockJoinMutationDocuments(ctx context.Context, transaction store.Transaction, locks []joinMutationLock, selection localization.Selection) error {
	sort.SliceStable(locks, func(left, right int) bool {
		leftKey := string(locks[left].collection.Schema.ID) + "\x00" + locks[left].id
		rightKey := string(locks[right].collection.Schema.ID) + "\x00" + locks[right].id
		return leftKey < rightKey
	})
	for _, lock := range locks {
		request := store.Request{
			Collection: lock.collection.Schema, Collections: engine.schemas, ID: lock.id,
			Access: lock.access, PublishedOnly: lock.publishedOnly, Lock: store.LockMutation,
			Locales: append([]schema.LocaleCode(nil), selection.Configured...), LocaleChain: append([]schema.LocaleCode(nil), selection.Chain...), AllLocales: selection.All,
		}
		if lock.filter != nil {
			filter := lock.filter.Node()
			request.Filter = &filter
		}
		if _, err := transaction.Find(ctx, request); err != nil {
			if lock.conflict && errors.Is(err, store.ErrNotFound) {
				return &Error{Code: "conflict", Status: 409, Message: "join target changed while relationships were being updated", Cause: err}
			}
			return translateStoreError(err)
		}
	}
	return nil
}

func (engine *Engine) preflightJoinTargetUpdate(ctx context.Context, transaction store.Transaction, collection Collection, document store.Document, values store.Values, actor *store.Document, actorCollection schema.CollectionSlug, selection localization.Selection) error {
	projected := localization.ProjectDocument(document, collection.Schema.Fields, selection)
	operationContext := Context{
		Context: ctx, Operation: operation.Update, Collection: collection.Schema, ID: document.ID,
		Actor: cloneDocumentPointer(actor), ActorCollection: actorCollection, Data: store.CloneValues(values),
		Original: cloneDocumentPointer(&projected), Locale: selection.Locale, AllLocales: selection.All,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...),
	}
	decision, err := authorize(collection, operationContext)
	if err != nil {
		return &Error{Code: "access_failed", Status: 500, Message: "join target access rule failed", Cause: err}
	}
	if decision.Kind == Deny {
		return &Error{Code: "access_denied", Status: 403, Message: "join target may not be updated"}
	}
	_, err = transaction.Find(ctx, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: document.ID, Access: decision.Access,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...), LocaleChain: append([]schema.LocaleCode(nil), selection.Chain...), AllLocales: selection.All,
	})
	if err != nil {
		return translateStoreError(err)
	}
	return nil
}

func joinObservedValueFilter(path query.Path, value store.Value, present bool) query.Expression {
	if present {
		if current, valid := value.StringValue(); valid {
			return query.Equal(path, query.String(current))
		}
	}
	return query.Equal(path, query.Null())
}

func valuesAtJoinPath(path query.Path, value store.Value) store.Values {
	segments := path.Segments()
	if len(segments) == 0 {
		return store.Values{}
	}
	current := value
	for index := len(segments) - 1; index > 0; index-- {
		current = store.Object(store.Values{segments[index]: current})
	}
	return store.Values{segments[0]: current}
}
