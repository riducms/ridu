package operation

import (
	"context"
	"errors"
	"fmt"

	"github.com/riducms/ridu/internal/localization"
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
		operationContext.Operation = Read
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
				return AccessCapabilities{}, &Error{Code: "store_failed", Status: 500, Message: "read capability document", Cause: findError}
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
	allowed := func(kind Kind, data store.Values, targetDeletion store.DeletionMode) (bool, error) {
		operationContext := base
		operationContext.Operation = kind
		operationContext.Data = store.CloneValues(data)
		operationContext.Original = cloneDocumentPointer(document)
		decision, err := authorize(collection, operationContext)
		if err != nil {
			return false, capabilityAccessError("access rule failed", err)
		}
		if decision.Kind == Deny || (kind == Create || kind == Admin) && decision.Kind == Where {
			return false, nil
		}
		if base.ID == "" || kind == Create || kind == Admin {
			return decision.Kind == Allow || decision.Kind == Where, nil
		}
		if kind == ReadVersions {
			versionTransaction, ok := transaction.(store.VersionTransaction)
			if !ok {
				return false, &Error{Code: "store_failed", Status: 500, Message: "version-enabled collection requires store.VersionTransaction"}
			}
			versions, versionError := versionTransaction.ListVersions(base.Context, store.VersionRequest{
				Collection: collection.Schema, DocumentID: base.ID, Access: decision.Access,
				Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
			})
			if versionError != nil {
				return false, &Error{Code: "store_failed", Status: 500, Message: "evaluate filtered version capability", Cause: versionError}
			}
			return len(versions) != 0, nil
		}
		if decision.Kind == Allow {
			if collection.Schema.Capabilities.Global && (kind == Read || kind == Update) {
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
				return false, &Error{Code: "store_failed", Status: 500, Message: "evaluate mutation capability existence", Cause: findError}
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
			return false, &Error{Code: "store_failed", Status: 500, Message: "evaluate filtered capability", Cause: findError}
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
		admin, err = allowed(Admin, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	create := false
	if !collection.Schema.Capabilities.Global && !selection.All {
		create, err = allowed(Create, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	read := document != nil
	if base.ID == "" || collection.Schema.Capabilities.Global && document == nil {
		read, err = allowed(Read, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	readVersions := false
	if collection.Schema.Versions != nil {
		readVersions, err = allowed(ReadVersions, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	update, deleteAllowed := false, false
	if !trashOnly {
		if !selection.All {
			update, err = allowed(Update, data, store.DeletionActive)
			if err != nil {
				return OperationCapabilities{}, err
			}
		}
		deleteAllowed, err = allowed(Delete, data, store.DeletionActive)
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
		duplicate, err = allowed(Duplicate, duplicateData, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	restoreDeleted, deletePermanent := false, false
	if trashOnly && base.ID != "" {
		restoreDeleted, err = allowed(RestoreDeleted, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
		deletePermanent, err = allowed(DeletePermanent, data, deletion)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	versioned := collection.Schema.Versions != nil
	publish, unpublish := false, false
	if versioned && !trashOnly {
		publish, err = allowed(Publish, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
		unpublish, err = allowed(Unpublish, data, store.DeletionActive)
		if err != nil {
			return OperationCapabilities{}, err
		}
	}
	unlock := false
	if collection.Schema.DocumentLock != nil && !trashOnly && base.ID != "" && document != nil {
		unlock, err = allowed(Unlock, data, store.DeletionActive)
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
	result := make(map[string]FieldCapabilities, len(collection.Fields))
	data := store.CloneValues(base.Data)
	if len(data) == 0 && document != nil {
		data = store.CloneValues(document.Values)
	}
	for _, path := range sortedFieldRulePaths(collection.Fields) {
		rules := collection.Fields[path]
		locations := fieldLocationsAtPath(collection.Schema.Fields, data, path, base.AllLocales)
		if len(locations) == 0 {
			locations = []fieldLocation{{siblings: store.CloneValues(data), runtimePath: path}}
		}
		canonical := FieldCapabilities{Read: true, Create: true, Update: true}
		for _, location := range locations {
			capability, err := evaluateFieldCapabilities(rules, base, document, operations, path, location)
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

func evaluateFieldCapabilities(rules FieldRules, base Context, document *store.Document, operations OperationCapabilities, path string, location fieldLocation) (FieldCapabilities, error) {
	evaluate := func(kind Kind, rule FieldAccess, fallback bool) (bool, error) {
		if !fallback {
			return false, nil
		}
		if rule == nil {
			return true, nil
		}
		fieldContext := base
		fieldContext.Operation, fieldContext.FieldPath, fieldContext.RuntimePath = kind, path, location.runtimePath
		fieldContext.Value, fieldContext.SiblingData = location.value, store.CloneValues(location.siblings)
		if location.locale != "" {
			fieldContext.Locale = location.locale
		}
		fieldContext.Document = fieldLocaleDocument(document, base.Collection.Fields, base, location.locale)
		fieldContext.Original = cloneDocumentPointer(fieldContext.Document)
		allowed, err := rule(fieldContext)
		if err != nil {
			return false, capabilityAccessError("field access rule failed", err)
		}
		return allowed, nil
	}
	read, err := evaluate(Read, rules.Read, true)
	if err != nil {
		return FieldCapabilities{}, err
	}
	create, err := evaluate(Create, rules.Create, operations.Create)
	if err != nil {
		return FieldCapabilities{}, err
	}
	update, err := evaluate(Update, rules.Update, operations.Update)
	if err != nil {
		return FieldCapabilities{}, err
	}
	return FieldCapabilities{Read: read, Create: create, Update: update}, nil
}

func capabilityAccessError(message string, cause error) error {
	return &Error{Code: "access_failed", Status: 500, Message: message, Cause: cause}
}
