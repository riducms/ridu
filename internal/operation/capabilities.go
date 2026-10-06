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

// CapabilitiesRequest evaluates non-secret operation and field access for an
// actor. Data is a prospective create/update snapshot. TrashOnly evaluates a
// deleted document for restore and permanent-delete actions.
type CapabilitiesRequest struct {
	Collection      string
	ID              string
	Data            store.Values
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	TrashOnly       bool
	Locale          string
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

type OperationCapabilities struct {
	Admin           bool
	Create          bool
	Read            bool
	ReadVersions    bool
	Update          bool
	Delete          bool
	Duplicate       bool
	Publish         bool
	Unpublish       bool
	RestoreDeleted  bool
	DeletePermanent bool
	SelectAll       bool
	Unlock          bool
}

type FieldCapabilities struct {
	Read   bool
	Create bool
	Update bool
}

// AccessCapabilities is the evaluated access of an actor and optional
// document or input snapshot.
type AccessCapabilities struct {
	Operations OperationCapabilities
	// Fields holds field capabilities by runtime path for each located value,
	// and by canonical path for each resource field and each block placement
	// with located values.
	Fields map[string]FieldCapabilities
	// BlockFields holds, by block slug and definition-relative path, the
	// capabilities of a block definition's field at a placement without
	// located values, such as a row the client has yet to add. It is
	// evaluated once per definition field rather than once per placement.
	BlockFields map[string]map[string]FieldCapabilities
}

// Capabilities evaluates access without running validation, hooks, mutations,
// or commits. Filtered decisions remain predicates on store reads so a
// document-specific true result has the same authorization semantics as the
// corresponding operation.
func (engine *Engine) Capabilities(ctx context.Context, request CapabilitiesRequest) (result AccessCapabilities, err error) {
	collection, exists := engine.collections[request.Collection]
	if !exists {
		return AccessCapabilities{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", request.Collection)}
	}
	if request.TrashOnly && !collection.Schema.Capabilities.Trash {
		return AccessCapabilities{}, &Error{Code: "bad_operation", Status: 400, Message: "trash capabilities require a trash-enabled collection"}
	}
	selection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if localeError != nil {
		return AccessCapabilities{}, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return AccessCapabilities{}, transactionAdmissionError("begin capability transaction", err)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback capability transaction", err, rollbackError)
			}
		}()
	}

	operationContext := Context{
		Context: transactionContext, Collection: collection.Schema, ID: request.ID,
		Actor: cloneDocumentPointer(request.Actor), Data: store.CloneValues(request.Data),
		ActorCollection: request.ActorCollection,
		Locale:          selection.Locale, AllLocales: selection.All,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...),
	}
	deletion := store.DeletionActive
	if request.TrashOnly {
		deletion = store.DeletionTrash
	}

	var document *store.Document
	if request.ID != "" {
		operationContext.Operation = operation.Read
		readDecision, accessError := authorize(collection, operationContext)
		if accessError != nil {
			return AccessCapabilities{}, accessRuleError("read access rule failed", accessError)
		}
		if readDecision.Kind == Deny {
		} else {
			found, findError := state.transaction.Find(transactionContext, store.Request{
				Collection: collection.Schema, Collections: engine.schemas, ID: request.ID,
				Access: readDecision.Access, Deletion: deletion, Locales: selection.Configured,
				LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if findError == nil {
				projected := localization.ProjectDocument(found, collection.Schema.Fields, selection)
				document = cloneDocumentPointer(&projected)
			} else if !errors.Is(findError, store.ErrNotFound) {
				return AccessCapabilities{}, translateStoreError(fmt.Errorf("read capability document: %w", findError))
			}
		}
	}

	return engine.capabilitiesInTransaction(state.transaction, collection, operationContext, document, deletion, request.TrashOnly, selection)
}

// PublicationTarget loads only the metadata needed to schedule a publish or
// unpublish. The selected lifecycle access predicate remains attached to the
// same atomic store read, so scheduling does not accidentally require Read.
func (engine *Engine) PublicationTarget(ctx context.Context, request CapabilitiesRequest, kind operation.Kind) (document store.Document, err error) {
	if kind != operation.Publish && kind != operation.Unpublish {
		return store.Document{}, &Error{Code: "bad_operation", Status: 400, Message: "publication target requires publish or unpublish"}
	}
	collection, exists := engine.collections[request.Collection]
	if !exists || collection.Schema.Versions == nil {
		return store.Document{}, &Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if kind == operation.Unpublish && !collection.Schema.Versions.Drafts {
		return store.Document{}, &Error{Code: "bad_operation", Status: 400, Message: "collection does not support drafts"}
	}
	selection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if localeError != nil {
		return store.Document{}, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return store.Document{}, transactionAdmissionError("begin publication target read", err)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
				err = rollbackOperationError("rollback publication target read", err, rollbackError)
			}
		}()
	}
	operationContext := Context{
		Context: transactionContext, Operation: kind, Collection: collection.Schema, ID: request.ID,
		Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection,
		Locale: selection.Locale, AllLocales: selection.All,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...),
	}
	decision, accessError := authorize(collection, operationContext)
	if accessError != nil {
		return store.Document{}, accessRuleError("publication access rule failed", accessError)
	}
	if decision.Kind == Deny {
		return store.Document{}, &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	document, err = state.transaction.Find(transactionContext, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: request.ID,
		Access: decision.Access, Deletion: store.DeletionActive, Select: []query.Path{},
		Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
	})
	if err != nil {
		return store.Document{}, translateStoreError(err)
	}
	return document, nil
}

// MutatePublicationTarget runs one framework-owned callback after the selected
// lifecycle access predicate has matched a mutation-locked document and before
// that write transaction commits.
func (engine *Engine) MutatePublicationTarget(ctx context.Context, request CapabilitiesRequest, kind operation.Kind, mutation TransactionMutation) (err error) {
	if kind != operation.Publish && kind != operation.Unpublish {
		return &Error{Code: "bad_operation", Status: 400, Message: "publication target requires publish or unpublish"}
	}
	if mutation == nil {
		return &Error{Code: "bad_operation", Status: 400, Message: "publication target mutation is required"}
	}
	collection, exists := engine.collections[request.Collection]
	if !exists || collection.Schema.Versions == nil {
		return &Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if kind == operation.Unpublish && !collection.Schema.Versions.Drafts {
		return &Error{Code: "bad_operation", Status: 400, Message: "collection does not support drafts"}
	}
	selection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if localeError != nil {
		return &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionWrite)
	if err != nil {
		return transactionAdmissionError("begin publication target mutation", err)
	}
	if state.rollbackOnly != nil {
		return transactionAbortedOperationError(state.rollbackOnly)
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
			if !state.finished {
				if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
					err = rollbackOperationError("rollback publication target mutation", err, rollbackError)
				}
			}
		}()
	}
	operationContext := Context{
		Context: transactionContext, Operation: kind, Collection: collection.Schema, ID: request.ID,
		Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection,
		Locale: selection.Locale, AllLocales: selection.All,
		Locales: append([]schema.LocaleCode(nil), selection.Configured...),
	}
	decision, accessError := authorize(collection, operationContext)
	if accessError != nil {
		return accessRuleError("publication access rule failed", accessError)
	}
	if decision.Kind == Deny {
		return &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	document, findError := state.transaction.Find(transactionContext, store.Request{
		Collection: collection.Schema, Collections: engine.schemas, ID: request.ID,
		Access: decision.Access, Deletion: store.DeletionActive, Lock: store.LockMutation, Select: []query.Path{},
		Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
	})
	if errors.Is(findError, store.ErrNotFound) {
		return &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	if findError != nil {
		return translateStoreError(findError)
	}
	if mutationError := mutation(transactionContext, state.transaction, collection.Schema, document); mutationError != nil {
		return translateStoreError(mutationError)
	}
	// The callback holds the raw transaction, so treat the target as written.
	state.writes.record(store.DocumentReference{CollectionID: collection.Schema.ID, DocumentID: document.ID})
	if !ownsTransaction {
		return nil
	}
	if commitError := engine.commitTransaction(ctx, state); commitError != nil {
		return commitAttemptedError(commitError)
	}
	return nil
}

func (engine *Engine) capabilitiesInTransaction(transaction store.Transaction, collection Collection, operationContext Context, document *store.Document, deletion store.DeletionMode, trashOnly bool, selection localization.Selection) (AccessCapabilities, error) {
	operations, err := engine.operationCapabilities(transaction, collection, operationContext, document, deletion, trashOnly, selection)
	if err != nil {
		return AccessCapabilities{}, err
	}
	if operationContext.ID != "" && document == nil && !collection.Schema.Capabilities.Global && !hasDocumentCapability(operations) {
		return AccessCapabilities{}, &Error{Code: "not_found", Status: 404, Message: "document was not found"}
	}
	capabilities, err := fieldCapabilities(collection, operationContext, document, operations)
	if err != nil {
		return AccessCapabilities{}, err
	}
	capabilities.Operations = operations
	return capabilities, nil
}

func (engine *Engine) collectionPageAccess(transaction store.Transaction, collection Collection, base Context, page store.Page, readDecision Decision, deletion store.DeletionMode, trashOnly, publishedOnly bool, selection localization.Selection) (*CollectionPageAccess, error) {
	collectionAccess, err := engine.capabilitiesInTransaction(transaction, collection, base, nil, deletion, trashOnly, selection)
	if err != nil {
		return nil, err
	}
	documents := make(map[string]AccessCapabilities, len(page.Documents))
	for _, listed := range page.Documents {
		if err := base.Context.Err(); err != nil {
			return nil, err
		}
		document, findError := transaction.Find(base.Context, store.Request{
			Collection: collection.Schema, Collections: engine.schemas, ID: listed.ID,
			Access: readDecision.Access, Deletion: deletion, PublishedOnly: publishedOnly,
			Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
		})
		if findError != nil {
			return nil, translateStoreError(fmt.Errorf("read collection list capability document: %w", findError))
		}
		document = localization.ProjectDocument(document, collection.Schema.Fields, selection)
		operationContext := base
		operationContext.ID = document.ID
		operationContext.Data = store.CloneValues(document.Values)
		access, capabilityError := engine.capabilitiesInTransaction(transaction, collection, operationContext, &document, deletion, trashOnly, selection)
		if capabilityError != nil {
			return nil, capabilityError
		}
		documents[document.ID] = access
	}
	return &CollectionPageAccess{Collection: collectionAccess, Documents: documents}, nil
}

func (engine *Engine) operationCapabilities(transaction store.Transaction, collection Collection, base Context, document *store.Document, deletion store.DeletionMode, trashOnly bool, selection localization.Selection) (OperationCapabilities, error) {
	allowed := func(kind operation.Kind, data store.Values, targetDeletion store.DeletionMode) (bool, error) {
		operationContext := base
		operationContext.Operation = kind
		operationContext.Data = store.CloneValues(data)
		operationContext.Original = cloneDocumentPointer(document)
		decision, err := authorize(collection, operationContext)
		if err != nil {
			return false, accessRuleError("access rule failed", err)
		}
		if decision.Kind == Deny || (kind == operation.Create || kind == operation.Admin) && decision.Kind == Where {
			return false, nil
		}
		if base.ID == "" || kind == operation.Create || kind == operation.Admin {
			return decision.Kind == Allow || decision.Kind == Where, nil
		}
		if kind == operation.ReadVersions {
			versionTransaction, ok := transaction.(store.VersionTransaction)
			if !ok {
				return false, &Error{Code: "store_failed", Status: 500, Message: "version-enabled collection requires store.VersionTransaction"}
			}
			total, versionError := versionTransaction.CountVersions(base.Context, store.VersionRequest{
				Collection: collection.Schema, DocumentID: base.ID, Access: decision.Access,
				Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if versionError != nil {
				return false, translateStoreError(fmt.Errorf("evaluate filtered version capability: %w", versionError))
			}
			return total != 0, nil
		}
		if decision.Kind == Allow {
			if collection.Schema.Capabilities.Global && (kind == operation.Read || kind == operation.Update) {
				return true, nil
			}
			if document != nil {
				return true, nil
			}
			// A resource may deliberately permit a mutation while denying Read.
			// Probe only existence in that mutation's deletion scope so capability
			// results match the operation that the actor can actually execute.
			_, findError := transaction.Find(base.Context, store.Request{
				Collection: collection.Schema, Collections: engine.schemas, ID: base.ID,
				Deletion: targetDeletion, Locales: selection.Configured,
				LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if errors.Is(findError, store.ErrNotFound) {
				return false, nil
			}
			if findError != nil {
				return false, translateStoreError(fmt.Errorf("evaluate mutation capability existence: %w", findError))
			}
			return true, nil
		}
		_, findError := transaction.Find(base.Context, store.Request{
			Collection: collection.Schema, Collections: engine.schemas, ID: base.ID,
			Access: decision.Access, Deletion: targetDeletion, Locales: selection.Configured,
			LocaleChain: selection.Chain, AllLocales: selection.All,
		})
		if errors.Is(findError, store.ErrNotFound) {
			return false, nil
		}
		if findError != nil {
			return false, translateStoreError(fmt.Errorf("evaluate filtered capability: %w", findError))
		}
		return true, nil
	}

	data := store.CloneValues(base.Data)
	var err error
	if len(data) == 0 && document != nil {
		data = store.CloneValues(document.Values)
	}
	admin := false
	if collection.Schema.Capabilities.Auth {
		admin, err = allowed(operation.Admin, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	create := false
	if !collection.Schema.Capabilities.Global && !selection.All {
		create, err = allowed(operation.Create, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	read := document != nil
	if base.ID == "" || collection.Schema.Capabilities.Global && document == nil {
		read, err = allowed(operation.Read, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	readVersions := false
	if collection.Schema.Versions != nil {
		readVersions, err = allowed(operation.ReadVersions, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	update, deleteAllowed := false, false
	if !trashOnly {
		if !selection.All {
			update, err = allowed(operation.Update, data, store.DeletionActive)
			if err != nil {
				return OperationCapabilities{}, err
			}
		}
		deleteAllowed, err = allowed(operation.Delete, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	duplicate := false
	if !trashOnly && !selection.All && base.ID != "" && document != nil && !collection.Schema.Capabilities.Global {
		duplicateData := store.CloneValues(document.Values)
		for name, value := range base.Data {
			duplicateData[name] = value
		}
		duplicate, err = allowed(operation.Duplicate, duplicateData, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	restoreDeleted, deletePermanent := false, false
	if trashOnly && base.ID != "" {
		restoreDeleted, err = allowed(operation.RestoreDeleted, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
		deletePermanent, err = allowed(operation.DeletePermanent, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	versioned := collection.Schema.Versions != nil
	publish, unpublish := false, false
	if versioned && !trashOnly {
		publish, err = allowed(operation.Publish, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
		if collection.Schema.Versions.Drafts {
			unpublish, err = allowed(operation.Unpublish, data, store.DeletionActive)
			if err != nil {
				return OperationCapabilities{}, err
			}
		}
	}
	unlock := false
	if collection.Schema.DocumentLock != nil && !trashOnly && base.ID != "" && document != nil {
		unlock, err = allowed(operation.Unlock, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	return OperationCapabilities{
		Admin: admin, Create: create, Read: read, ReadVersions: readVersions,
		Update: update, Delete: deleteAllowed, Duplicate: duplicate,
		Publish: publish, Unpublish: unpublish,
		RestoreDeleted: restoreDeleted, DeletePermanent: deletePermanent,
		SelectAll: base.ID == "" && !collection.Schema.Capabilities.Global,
		Unlock:    unlock,
	}, nil
}

func hasDocumentCapability(operations OperationCapabilities) bool {
	return operations.Read || operations.ReadVersions || operations.Update || operations.Delete || operations.Duplicate ||
		operations.Publish || operations.Unpublish || operations.RestoreDeleted || operations.DeletePermanent || operations.Unlock
}

// fieldCapabilities evaluates the field access rules of every ruled binding at
// each of its locations. A resource's own field also has an entry at its
// canonical path: the conjunction of its locations, or a schema-level
// fallback without any. A block definition's field has a conjunction entry at
// each placement with locations and one fallback for every other placement,
// in BlockFields, so the result follows the document and the definitions
// rather than every placement of the schema.
func fieldCapabilities(collection Collection, base Context, document *store.Document, operations OperationCapabilities) (AccessCapabilities, error) {
	result := AccessCapabilities{Fields: make(map[string]FieldCapabilities, len(collection.Bindings))}
	data := store.CloneValues(base.Data)
	if len(data) == 0 && document != nil {
		data = store.CloneValues(document.Values)
	}
	// Attached rules use their resolved binding, including optional scalar
	// occurrences whose current logical value is empty. Presentation capability
	// discovery must not depend on whether an adapter materialized that key.
	bound := base
	bound.Data, bound.Document = data, nil
	bound.Original = document
	dataRoot := lazyObject{values: data}
	var documentRoot lazyObject
	if document != nil {
		documentRoot.values = document.Values
	}
	views := callbackViews{}
	paths := collection.runtimePlan().bindingPaths
	ruled := func(index int) bool {
		rules := collection.Bindings[index].Access
		return rules.Read != nil || rules.Create != nil || rules.Update != nil
	}
	// One walk of each snapshot finds every ruled binding's locations.
	dataLocations := lazyBindingLocations{collection: collection, root: &dataRoot, allLocales: base.AllLocales, includeMissing: true, want: ruled}
	documentLocations := lazyBindingLocations{collection: collection, root: &documentRoot, allLocales: base.AllLocales, includeMissing: true, want: ruled}
	// fallback evaluates a binding's rules without a concrete row or object
	// occurrence. Existing admin capability contracts still need it. Do not
	// fabricate a repeated identity or borrow root values as siblings.
	fallback := func(index int, path string) (FieldCapabilities, error) {
		binding := collection.Bindings[index]
		ctx := bound
		ctx.bound = boundField{field: &collection.Bindings[index].Field}
		ctx.RuntimePath, ctx.SchemaOccurrenceID = path, binding.ID
		ctx.Siblings, ctx.Prior = store.Value{}, store.Value{}
		return evaluateBoundFieldCapabilities(binding.Access, ctx, operations)
	}
	for index, binding := range collection.Bindings {
		rules := binding.Access
		if !ruled(index) {
			continue
		}
		locations := dataLocations.get(index)
		previous := map[string]fieldLocation{}
		if document != nil {
			previous = indexFieldLocations(documentLocations.get(index))
		}
		// placements holds the conjunction of each placement's locations.
		placements := map[string]FieldCapabilities{}
		for _, location := range locations {
			views.root = dataRoot.value()
			ctx := scopedBindingContext(bound, binding, location, previous, &views)
			capability, err := evaluateBoundFieldCapabilities(rules, ctx, operations)
			if err != nil {
				return AccessCapabilities{}, err
			}
			result.Fields[location.runtimePath] = capability
			placement := location.canonical()
			canonical, seen := placements[placement]
			if !seen {
				canonical = FieldCapabilities{Read: true, Create: true, Update: true}
			}
			canonical.Read = canonical.Read && capability.Read
			canonical.Create = canonical.Create && capability.Create
			canonical.Update = canonical.Update && capability.Update
			placements[placement] = canonical
		}
		for path, canonical := range placements {
			result.Fields[path] = canonical
		}
		if binding.Block == "" {
			path := paths[index]
			if _, located := placements[path]; !located {
				canonical, err := fallback(index, path)
				if err != nil {
					return AccessCapabilities{}, err
				}
				result.Fields[path] = canonical
			}
			continue
		}
		// One fallback serves every placement of the definition's field.
		canonical, err := fallback(index, binding.Field.Path.String())
		if err != nil {
			return AccessCapabilities{}, err
		}
		if result.BlockFields == nil {
			result.BlockFields = map[string]map[string]FieldCapabilities{}
		}
		if result.BlockFields[binding.Block] == nil {
			result.BlockFields[binding.Block] = map[string]FieldCapabilities{}
		}
		result.BlockFields[binding.Block][binding.Field.Path.String()] = canonical
	}
	return result, nil
}

func evaluateBoundFieldCapabilities(rules FieldRules, ctx Context, operations OperationCapabilities) (FieldCapabilities, error) {
	evaluate := func(kind operation.Kind, rule FieldAccess, fallback bool) (bool, error) {
		if !fallback || rule == nil {
			return fallback, nil
		}
		ctx.Operation = kind
		allowed, err := rule(ctx)
		if err != nil {
			return false, accessRuleError("field access rule failed", err)
		}
		return allowed, nil
	}
	read, err := evaluate(operation.Read, rules.Read, true)
	if err != nil {
		return FieldCapabilities{}, err
	}
	create, err := evaluate(operation.Create, rules.Create, operations.Create)
	if err != nil {
		return FieldCapabilities{}, err
	}
	update, err := evaluate(operation.Update, rules.Update, operations.Update)
	if err != nil {
		return FieldCapabilities{}, err
	}
	return FieldCapabilities{Read: read, Create: create, Update: update}, nil
}
