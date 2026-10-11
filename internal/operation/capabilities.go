package operation

import (
	"context"
	"errors"
	"fmt"
	"reflect"

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
	operations, err := engine.operationCapabilities(transaction, collection, operationContext, document, deletion, trashOnly, selection, nil)
	if err != nil {
		return AccessCapabilities{}, err
	}
	if operationContext.ID != "" && document == nil && !collection.Schema.Capabilities.Global && !hasDocumentCapability(*operations) {
		return AccessCapabilities{}, &Error{Code: "not_found", Status: 404, Message: "document was not found"}
	}
	capabilities, err := fieldCapabilities(collection, operationContext, document, *operations)
	if err != nil {
		return AccessCapabilities{}, err
	}
	capabilities.Operations = *operations
	return capabilities, nil
}

func (engine *Engine) collectionPageAccess(transaction store.Transaction, collection Collection, base Context, page store.Page, readDecision Decision, deletion store.DeletionMode, trashOnly, publishedOnly bool, selection localization.Selection) (*CollectionPageAccess, error) {
	collectionAccess, err := engine.capabilitiesInTransaction(transaction, collection, base, nil, deletion, trashOnly, selection)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(page.Documents))
	for index, listed := range page.Documents {
		ids[index] = listed.ID
	}
	documents, missing, err := engine.documentCapabilities(transaction, collection, base, ids, store.Request{
		Access: readDecision.Access, Deletion: deletion, PublishedOnly: publishedOnly,
	}, trashOnly, selection)
	if err != nil {
		return nil, err
	}
	if len(missing) != 0 {
		return nil, translateStoreError(fmt.Errorf("read collection list capability document %q: %w", missing[0], store.ErrNotFound))
	}
	return &CollectionPageAccess{Collection: collectionAccess, Documents: documents}, nil
}

// documentCapabilities evaluates the access of many documents with a bounded
// number of store reads: one read of their complete documents per hundred, and
// one per hundred for each distinct filtered decision, instead of reads for
// every document. read supplies the document read's access, filter, deletion
// and draft scope. missing lists the IDs that read no longer finds.
func (engine *Engine) documentCapabilities(transaction store.Transaction, collection Collection, base Context, ids []string, read store.Request, trashOnly bool, selection localization.Selection) (map[string]AccessCapabilities, []string, error) {
	read.Collection, read.Collections = collection.Schema, engine.schemas
	read.Locales, read.LocaleChain, read.AllLocales = selection.Configured, selection.Chain, selection.All
	stored, err := engine.documentsByID(transaction, base.Context, ids, read)
	if err != nil {
		return nil, nil, capabilityReadError("read capability documents", err)
	}
	type evaluated struct {
		context    Context
		document   store.Document
		operations *OperationCapabilities
	}
	documents := make([]evaluated, 0, len(ids))
	var missing []string
	var checks []capabilityCheck
	for _, id := range ids {
		if err := base.Context.Err(); err != nil {
			return nil, nil, err
		}
		document, found := stored[id]
		if !found {
			missing = append(missing, id)
			continue
		}
		document = localization.ProjectDocument(document, collection.Schema.Fields, selection)
		operationContext := base
		operationContext.ID = id
		operationContext.Data = store.CloneValues(document.Values)
		documents = append(documents, evaluated{context: operationContext, document: document})
	}
	for index := range documents {
		current := &documents[index]
		current.operations, err = engine.operationCapabilities(transaction, collection, current.context, &current.document, read.Deletion, trashOnly, selection, &checks)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := engine.resolveCapabilityChecks(transaction, base.Context, collection, checks, selection); err != nil {
		return nil, nil, err
	}
	result := make(map[string]AccessCapabilities, len(documents))
	for _, current := range documents {
		// Field rules see the resolved operations, as for a single document.
		capabilities, err := fieldCapabilities(collection, current.context, &current.document, *current.operations)
		if err != nil {
			return nil, nil, err
		}
		capabilities.Operations = *current.operations
		result[current.document.ID] = capabilities
	}
	return result, missing, nil
}

// capabilityReadError keeps cancellation recognizable to callers.
func capabilityReadError(action string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return translateStoreError(fmt.Errorf("%s: %w", action, err))
}

// capabilityBatchSize bounds the IDs one batched capability read names.
const capabilityBatchSize = MaxBatchDocuments

// documentsByID reads the complete documents that request matches among ids,
// in reads of at most capabilityBatchSize IDs.
func (engine *Engine) documentsByID(transaction store.Transaction, ctx context.Context, ids []string, request store.Request) (map[string]store.Document, error) {
	documents := make(map[string]store.Document, len(ids))
	for start := 0; start < len(ids); start += capabilityBatchSize {
		chunk := ids[start:min(start+capabilityBatchSize, len(ids))]
		chunkRequest := request
		chunkRequest.Filter = idFilter(chunk, request.Filter)
		chunkRequest.Page, chunkRequest.Limit, chunkRequest.SkipTotal = 1, len(chunk), true
		page, err := transaction.List(ctx, chunkRequest)
		if err != nil {
			return nil, err
		}
		for _, document := range page.Documents {
			documents[document.ID] = document
		}
	}
	return documents, nil
}

// idFilter matches the documents among ids that also match filter, if any.
func idFilter(ids []string, filter *query.Node) *query.Node {
	node := query.In("id", ids...).Node()
	if filter != nil {
		node = query.Node{Kind: query.ExpressionAnd, Children: []query.Node{node, *filter}}
	}
	return &node
}

// capabilityCheck is a filtered decision about one document, answered with
// the other checks that share its predicate and deletion scope. allowed is set
// once the check is resolved.
type capabilityCheck struct {
	id       string
	access   *query.Node
	deletion store.DeletionMode
	allowed  *bool
}

// resolveCapabilityChecks answers checks with one metadata-only read per
// hundred documents for each distinct predicate and deletion scope. A
// predicate matches a document exactly when a single filtered read would.
func (engine *Engine) resolveCapabilityChecks(transaction store.Transaction, ctx context.Context, collection Collection, checks []capabilityCheck, selection localization.Selection) error {
	type group struct {
		access   *query.Node
		deletion store.DeletionMode
		members  []int
	}
	var groups []group
	for index, check := range checks {
		matched := false
		for candidate := range groups {
			if groups[candidate].deletion == check.deletion && reflect.DeepEqual(*groups[candidate].access, *check.access) {
				groups[candidate].members = append(groups[candidate].members, index)
				matched = true
				break
			}
		}
		if !matched {
			groups = append(groups, group{access: check.access, deletion: check.deletion, members: []int{index}})
		}
	}
	for _, group := range groups {
		ids := make([]string, 0, len(group.members))
		seen := make(map[string]bool, len(group.members))
		for _, member := range group.members {
			if id := checks[member].id; !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		matching, err := engine.documentsByID(transaction, ctx, ids, store.Request{
			Collection: collection.Schema, Collections: engine.schemas, Access: group.access,
			Deletion: group.deletion, Select: []query.Path{},
			Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
		})
		if err != nil {
			return capabilityReadError("evaluate filtered capabilities", err)
		}
		for _, member := range group.members {
			_, *checks[member].allowed = matching[checks[member].id]
		}
	}
	return nil
}

// operationCapabilities evaluates every operation for one document or, without
// an ID, for the collection. A filtered decision about a document is a store
// read; with checks it is queued there instead, and its capability stays false
// until resolveCapabilityChecks answers it.
func (engine *Engine) operationCapabilities(transaction store.Transaction, collection Collection, base Context, document *store.Document, deletion store.DeletionMode, trashOnly bool, selection localization.Selection, checks *[]capabilityCheck) (*OperationCapabilities, error) {
	result := &OperationCapabilities{SelectAll: base.ID == "" && !collection.Schema.Capabilities.Global}
	evaluate := func(target *bool, kind operation.Kind, data store.Values, targetDeletion store.DeletionMode) error {
		operationContext := base
		operationContext.Operation = kind
		operationContext.Data = store.CloneValues(data)
		operationContext.Original = cloneDocumentPointer(document)
		decision, err := authorize(collection, operationContext)
		if err != nil {
			return accessRuleError("access rule failed", err)
		}
		if decision.Kind == Deny || (kind == operation.Create || kind == operation.Admin) && decision.Kind == Where {
			*target = false
			return nil
		}
		if base.ID == "" || kind == operation.Create || kind == operation.Admin {
			*target = decision.Kind == Allow || decision.Kind == Where
			return nil
		}
		if kind == operation.ReadVersions {
			versionTransaction, ok := transaction.(store.VersionTransaction)
			if !ok {
				return &Error{Code: "store_failed", Status: 500, Message: "version-enabled collection requires store.VersionTransaction"}
			}
			total, versionError := versionTransaction.CountVersions(base.Context, store.VersionRequest{
				Collection: collection.Schema, DocumentID: base.ID, Access: decision.Access,
				Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if versionError != nil {
				return translateStoreError(fmt.Errorf("evaluate filtered version capability: %w", versionError))
			}
			*target = total != 0
			return nil
		}
		if decision.Kind == Allow {
			if collection.Schema.Capabilities.Global && (kind == operation.Read || kind == operation.Update) || document != nil {
				*target = true
				return nil
			}
			// A resource may deliberately permit a mutation while denying Read.
			// Probe only existence in that mutation's deletion scope so capability
			// results match the operation that the actor can actually execute.
			_, findError := transaction.Find(base.Context, store.Request{
				Collection: collection.Schema, Collections: engine.schemas, ID: base.ID,
				Deletion: targetDeletion, Locales: selection.Configured,
				LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if findError != nil && !errors.Is(findError, store.ErrNotFound) {
				return translateStoreError(fmt.Errorf("evaluate mutation capability existence: %w", findError))
			}
			*target = findError == nil
			return nil
		}
		if checks != nil {
			*target = false
			*checks = append(*checks, capabilityCheck{id: base.ID, access: decision.Access, deletion: targetDeletion, allowed: target})
			return nil
		}
		_, findError := transaction.Find(base.Context, store.Request{
			Collection: collection.Schema, Collections: engine.schemas, ID: base.ID,
			Access: decision.Access, Deletion: targetDeletion, Locales: selection.Configured,
			LocaleChain: selection.Chain, AllLocales: selection.All,
		})
		if findError != nil && !errors.Is(findError, store.ErrNotFound) {
			return translateStoreError(fmt.Errorf("evaluate filtered capability: %w", findError))
		}
		*target = findError == nil
		return nil
	}

	data := store.CloneValues(base.Data)
	if len(data) == 0 && document != nil {
		data = store.CloneValues(document.Values)
	}
	type step struct {
		applies  bool
		target   *bool
		kind     operation.Kind
		data     store.Values
		deletion store.DeletionMode
	}
	steps := []step{
		{collection.Schema.Capabilities.Auth, &result.Admin, operation.Admin, data, deletion},
		{!collection.Schema.Capabilities.Global && !selection.All, &result.Create, operation.Create, data, store.DeletionActive},
		{base.ID == "" || collection.Schema.Capabilities.Global && document == nil, &result.Read, operation.Read, data, deletion},
		{collection.Schema.Versions != nil, &result.ReadVersions, operation.ReadVersions, data, deletion},
		{!trashOnly && !selection.All, &result.Update, operation.Update, data, store.DeletionActive},
		{!trashOnly, &result.Delete, operation.Delete, data, store.DeletionActive},
	}
	result.Read = document != nil
	if !trashOnly && !selection.All && base.ID != "" && document != nil && !collection.Schema.Capabilities.Global {
		duplicateData := store.CloneValues(document.Values)
		for name, value := range base.Data {
			duplicateData[name] = value
		}
		steps = append(steps, step{true, &result.Duplicate, operation.Duplicate, duplicateData, store.DeletionActive})
	}
	trashed := trashOnly && base.ID != ""
	versioned := collection.Schema.Versions != nil && !trashOnly
	steps = append(steps,
		step{trashed, &result.RestoreDeleted, operation.RestoreDeleted, data, deletion},
		step{trashed, &result.DeletePermanent, operation.DeletePermanent, data, deletion},
		step{versioned, &result.Publish, operation.Publish, data, store.DeletionActive},
		step{versioned && collection.Schema.Versions.Drafts, &result.Unpublish, operation.Unpublish, data, store.DeletionActive},
		step{collection.Schema.DocumentLock != nil && !trashOnly && base.ID != "" && document != nil, &result.Unlock, operation.Unlock, data, store.DeletionActive},
	)
	for _, step := range steps {
		if !step.applies {
			continue
		}
		if err := evaluate(step.target, step.kind, step.data, step.deletion); err != nil {
			return nil, err
		}
	}
	return result, nil
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
