package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
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

type AccessCapabilities struct {
	Operations OperationCapabilities
	Fields     map[string]FieldCapabilities
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
			return AccessCapabilities{}, capabilityAccessError("read access rule failed", accessError)
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

	operations, err := engine.operationCapabilities(state.transaction, collection, operationContext, document, deletion, request.TrashOnly, selection)
	if err != nil {
		return AccessCapabilities{}, err
	}
	if request.ID != "" && document == nil && !collection.Schema.Capabilities.Global && !hasDocumentCapability(operations) {
		return AccessCapabilities{}, &Error{Code: "not_found", Status: 404, Message: "document was not found"}
	}
	fields, err := fieldCapabilities(collection, operationContext, document, operations)
	if err != nil {
		return AccessCapabilities{}, err
	}
	return AccessCapabilities{Operations: operations, Fields: fields}, nil
}

func (engine *Engine) operationCapabilities(transaction store.Transaction, collection Collection, base Context, document *store.Document, deletion store.DeletionMode, trashOnly bool, selection localization.Selection) (OperationCapabilities, error) {
	allowed := func(kind operation.Kind, data store.Values, targetDeletion store.DeletionMode) (bool, error) {
		operationContext := base
		operationContext.Operation = kind
		operationContext.Data = store.CloneValues(data)
		operationContext.Original = cloneDocumentPointer(document)
		decision, err := authorize(collection, operationContext)
		if err != nil {
			return false, capabilityAccessError("access rule failed", err)
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
			versions, versionError := versionTransaction.ListVersions(base.Context, store.VersionRequest{
				Collection: collection.Schema, DocumentID: base.ID, Access: decision.Access,
				Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if versionError != nil {
				return false, translateStoreError(fmt.Errorf("evaluate filtered version capability: %w", versionError))
			}
			return len(versions) != 0, nil
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
		unpublish, err = allowed(operation.Unpublish, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
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

func fieldCapabilities(collection Collection, base Context, document *store.Document, operations OperationCapabilities) (map[string]FieldCapabilities, error) {
	result := make(map[string]FieldCapabilities, len(collection.Bindings))
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
	for _, binding := range collection.Bindings {
		rules := binding.Access
		if rules.Read == nil && rules.Create == nil && rules.Update == nil {
			continue
		}
		path := binding.Field.Path.String()
		locations := fieldLocationsAtPath(collection.Schema.Fields, data, path, base.AllLocales, true)
		previous := map[string]fieldLocation{}
		if document != nil {
			previous = indexFieldLocations(fieldLocationsAtPath(collection.Schema.Fields, document.Values, path, base.AllLocales, true))
		}
		canonical := FieldCapabilities{Read: true, Create: true, Update: true}
		if len(locations) == 0 {
			// There is no concrete row/object occurrence yet. Existing admin
			// capability contracts still need a schema-level fallback. Do not
			// fabricate a repeated identity or borrow root values as siblings.
			fallback := bound
			fallback.FieldPath, fallback.RuntimePath = path, path
			fallback.SiblingData, fallback.OriginalSiblingData = nil, nil
			var err error
			canonical, err = evaluateBoundFieldCapabilities(rules, fallback, operations)
			if err != nil {
				return nil, err
			}
		}
		for _, location := range locations {
			ctx := scopedBindingContext(bound, binding, location, previous)
			capability, err := evaluateBoundFieldCapabilities(rules, ctx, operations)
			if err != nil {
				return nil, err
			}
			result[location.runtimePath] = capability
			canonical.Read = canonical.Read && capability.Read
			canonical.Create = canonical.Create && capability.Create
			canonical.Update = canonical.Update && capability.Update
		}
		result[path] = canonical
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
			return false, capabilityAccessError("field access rule failed", err)
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

func capabilityAccessError(message string, cause error) error {
	return &Error{Code: "access_failed", Status: 500, Message: message, Cause: cause}
}
