package operation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// LocalizationOptions selects the locale projection for version reads.
type LocalizationOptions struct {
	Locale          string
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
	// ActorCollection identifies the exact auth collection that owns Actor on
	// specialized version and locale operations.
	ActorCollection schema.CollectionSlug
}

const MaxBatchDocuments = store.MaxListWindowDocuments

type DecisionKind string

const (
	Allow DecisionKind = "allow"
	Deny  DecisionKind = "deny"
	Where DecisionKind = "where"
)

type Decision struct {
	Kind   DecisionKind
	Access *query.Node
}

type Context struct {
	Context             context.Context
	Operation           operation.Kind
	Collection          schema.Collection
	ID                  string
	Actor               *store.Document
	ActorCollection     schema.CollectionSlug
	Data                store.Values
	Value               store.Value
	SiblingData         store.Values
	InputSiblingData    store.Values
	RootData            store.Values
	LiveValidation      bool
	OriginalValue       store.Value
	OriginalSiblingData store.Values
	originalCanonical   *store.Document
	projections         *localization.Projector
	Document            *store.Document
	Original            *store.Document
	FieldPath           string
	OccurrenceID        string
	ValuePresent        bool
	submittedData       store.Values
	submittedAllLocales bool
	submittedLocale     schema.LocaleCode
	RuntimePath         string
	Error               error
	Locale              schema.LocaleCode
	AllLocales          bool
	Locales             []schema.LocaleCode
}

type Access func(Context) (Decision, error)
type Hook func(Context) error
type Computed func(Context, store.Document) (store.Value, error)
type FieldAccess func(Context) (bool, error)
type PluginValidator func(schema.Field, store.Value, string) []schema.Issue
type TransactionMutation func(context.Context, store.Transaction, schema.Collection, store.Document) error

type FieldRules struct {
	Create FieldAccess
	Read   FieldAccess
	Update FieldAccess
}

type Hooks struct {
	BeforeDuplicate []Hook
	BeforeValidate  []Hook
	BeforeChange    []Hook
	BeforeOperation []Hook
	BeforeRead      []Hook
	BeforeDelete    []Hook
	AfterChange     []Hook
	AfterRead       []Hook
	AfterDelete     []Hook
	AfterOperation  []Hook
	AfterError      []Hook
	AfterCommit     []Hook
}

type Collection struct {
	Key      string
	Schema   schema.Collection
	Access   map[operation.Kind]Access
	Hooks    Hooks
	Bindings []FieldBinding
}

type Config struct {
	Collections               []Collection
	Store                     store.Store
	AllowIDOnCreate           bool
	MaxDepth                  int
	PluginValidators          map[string]PluginValidator
	DispatchAfterCommit       func(Context, Hook) error
	BeginPermanentDeleteFence func(context.Context, []PermanentDelete) func(bool)
	CleanupPermanentDeletes   func(context.Context, []PermanentDelete) error
	ValidateUploadImport      func(context.Context, schema.Collection, store.Values) error
	RootAfterError            []Hook
	Localization              *schema.LocalizationSettings
}

type Request struct {
	Operation  operation.Kind
	Collection string
	ID         string
	Data       store.Values
	Filter     query.Expression
	// internalFilter is reserved for engine-derived membership and mutation fences.
	internalFilter  *query.Node
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Page            int
	Limit           int
	IndexWindow     *store.IndexWindow
	Sort            []query.Sort
	Select          []query.Path
	Populate        []query.Population
	// OutputFields limits computed and inverse-join resolution. Nil preserves
	// the local API's historical behavior of resolving every output field; a
	// non-nil empty slice resolves none. This is deliberately separate from
	// Select because computed resolvers may depend on unprojected stored data.
	OutputFields []query.Path
	// Draft overrides versioned read visibility or selects the status written
	// by create/update. Nil preserves the caller's existing behavior.
	Draft            *bool
	ExpectedRevision int
	Status           *store.Status
	ImportID         string
	ImportCreatedAt  time.Time
	ImportUpdatedAt  time.Time
	TrashOnly        bool
	StoragePrepared  bool
	// ValidateUploadObjects re-adopts existing upload keys. The engine locks and
	// verifies every final key inside the write transaction before committing
	// metadata that can make those objects live again.
	ValidateUploadObjects bool
	LocalizationPrepared  bool
	// replaceValues is reserved for restoring one canonical version snapshot.
	// Ordinary updates and publication transitions remain patches.
	replaceValues bool
	// copyLocaleSource preserves source occurrence identities for an explicit
	// locale copy. Independent whole-field translations may reuse the same key
	// for different variants; that is not an in-place schema change.
	copyLocaleSource schema.LocaleCode
	// SkipFieldAccess is reserved for framework-owned initialization that must
	// author the first administrator before an actor exists. Collection access,
	// validation, hooks, and the transaction remain active.
	SkipFieldAccess     bool
	TransactionMutation TransactionMutation
	// TransactionResource binds one framework-owned external resource to the
	// true outer transaction. Commit retains it, a definite rollback cleans it
	// up, and an unknown commit outcome retains it for reconciliation rather
	// than risking a dangling durable row.
	TransactionResource *TransactionResource
	Locale              string
	FallbackLocales     []schema.LocaleCode
	DisableFallback     bool
	AllLocales          bool
}

// TransactionResource coordinates an external resource whose durable system
// cannot participate in the document-store transaction.
type TransactionResource struct {
	Commit   func()
	Rollback func(context.Context) error
	Unknown  func()
	claimed  atomic.Bool
}

// Claimed reports whether the operation engine attached the resource to a
// document transaction. Once claimed, only the true outer transaction may
// finalize it; callers must not perform eager fallback cleanup.
func (resource *TransactionResource) Claimed() bool {
	return resource != nil && resource.claimed.Load()
}

type Result struct {
	Document      *store.Document
	Page          *store.Page
	WindowHasMore bool
}

type Error struct {
	Code    string
	Status  int
	Message string
	Issues  []schema.Issue
	Cause   error
	// Committed reports that the durable transaction succeeded before a later
	// after-commit effect failed. Callers that stage external resources must not
	// roll those resources back when this is true.
	Committed bool
	// CommitAttempted reports that Commit was sent but its durable outcome could
	// not be proven. External resources must be retained and reconciled instead
	// of rolled back when this is true.
	CommitAttempted bool
}

func (operationError *Error) Error() string { return operationError.Message }
func (operationError *Error) Unwrap() error { return operationError.Cause }

func publishedOnly(request Request, collection schema.Collection) bool {
	if request.Operation != operation.Read {
		return false
	}
	if collection.Versions == nil {
		// Draft selection has no meaning for an unversioned root, but anonymous
		// population can still reach versioned targets that must stay published.
		return request.Actor == nil
	}
	if request.Draft != nil {
		return !*request.Draft
	}
	return request.Actor == nil
}

type Engine struct {
	store                     store.Store
	allowIDOnCreate           bool
	collections               map[string]Collection
	schemas                   map[schema.StableID]schema.Collection
	maxDepth                  int
	pluginValidators          map[string]PluginValidator
	dispatchAfterCommit       func(Context, Hook) error
	beginPermanentDeleteFence func(context.Context, []PermanentDelete) func(bool)
	cleanupPermanentDeletes   func(context.Context, []PermanentDelete) error
	validateUploadImport      func(context.Context, schema.Collection, store.Values) error
	rootAfterError            []Hook
	localization              *schema.LocalizationSettings
}

func New(config Config) (*Engine, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("operation engine requires a store")
	}
	if config.MaxDepth <= 0 {
		config.MaxDepth = 32
	}
	engine := &Engine{
		store: config.Store, collections: make(map[string]Collection, len(config.Collections)*2),
		allowIDOnCreate:           config.AllowIDOnCreate,
		schemas:                   make(map[schema.StableID]schema.Collection, len(config.Collections)),
		maxDepth:                  config.MaxDepth,
		pluginValidators:          config.PluginValidators,
		dispatchAfterCommit:       config.DispatchAfterCommit,
		beginPermanentDeleteFence: config.BeginPermanentDeleteFence,
		cleanupPermanentDeletes:   config.CleanupPermanentDeletes,
		validateUploadImport:      config.ValidateUploadImport,
		rootAfterError:            append([]Hook(nil), config.RootAfterError...),
		localization:              cloneLocalization(config.Localization),
	}
	for _, collection := range config.Collections {
		key := collection.Key
		if key == "" {
			key = string(collection.Schema.Slug)
		}
		engine.collections[string(collection.Schema.ID)] = collection
		engine.collections[key] = collection
		engine.schemas[collection.Schema.ID] = collection.Schema
	}
	return engine, nil
}

func (engine *Engine) Execute(ctx context.Context, request Request) (result Result, err error) {
	if _, live := ctx.Value(liveContextKey{}).(*liveEvaluationContext); live {
		return engine.liveExecuteRead(ctx, request)
	}
	submittedData := store.CloneValues(request.Data)
	hasSubmittedChanges := len(submittedData) != 0
	failureContext := Context{
		Context: context.WithoutCancel(ctx), Operation: request.Operation, ID: request.ID,
		Actor: cloneDocumentPointer(request.Actor), ActorCollection: request.ActorCollection, Data: store.CloneValues(request.Data),
	}
	var resourceAfterError []Hook
	defer func() {
		if err == nil {
			return
		}
		failureContext.Error = err
		if hookFailure := runHooks(resourceAfterError, failureContext); hookFailure != nil {
			err = errors.Join(err, hookError("resource after error", hookFailure))
		}
		if hookFailure := runHooks(engine.rootAfterError, failureContext); hookFailure != nil {
			err = errors.Join(err, hookError("root after error", hookFailure))
		}
	}()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	stack, _ := ctx.Value(operationStackKey{}).([]operationFrame)
	frame := operationFrame{kind: request.Operation, collection: request.Collection, id: request.ID}
	if len(stack) >= engine.maxDepth || repeatedFrame(stack, frame) >= 4 {
		return Result{}, &Error{Code: "operation_recursion", Status: 409, Message: "operation recursion limit exceeded"}
	}
	ctx = context.WithValue(ctx, operationStackKey{}, append(append([]operationFrame(nil), stack...), frame))
	collection, exists := engine.collections[request.Collection]
	if !exists {
		return Result{}, &Error{Code: "unknown_collection", Status: 404, Message: fmt.Sprintf("collection %q was not found", request.Collection)}
	}
	if createIDError := engine.prepareCreateID(&request); createIDError != nil {
		return Result{}, createIDError
	}
	request.Data = store.CloneValues(request.Data)
	if request.Operation != operation.Read {
		if identityError := prepareRowIdentities(collection.Schema.Fields, request.Data, request.LocalizationPrepared, false); identityError != nil {
			return Result{}, identityError
		}
	}
	submittedData = store.CloneValues(request.Data)
	hasSubmittedChanges = len(submittedData) != 0
	failureContext.ID = request.ID
	failureContext.Data = store.CloneValues(request.Data)
	selection, localeError := localization.Resolve(engine.localization, request.Locale, request.FallbackLocales, request.DisableFallback, request.AllLocales)
	if localeError != nil {
		return Result{}, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	if selection.All && !request.LocalizationPrepared && (request.Operation == operation.Create || request.Operation == operation.Duplicate || request.Operation == operation.Update) {
		return Result{}, &Error{Code: "bad_locale", Status: 400, Message: "all-locales mode is read-only; write one locale at a time or use copy-to-locale"}
	}
	// Each Execute owns its projections, even when nested calls share a
	// transaction. Clear retained source/output branches on every exit.
	projections := localization.NewProjector(collection.Schema.Fields)
	defer projections.Clear()
	var localizationPreparedData store.Values
	preparedValidationSelection := selection
	hookSelection := selection
	if (request.Operation == operation.Publish || request.Operation == operation.Unpublish) && hookSelection.All {
		hookSelection.All = false
		if len(hookSelection.Chain) != 0 {
			hookSelection.Locale = hookSelection.Chain[0]
			hookSelection.Chain = hookSelection.Chain[:1]
		}
	}
	if request.LocalizationPrepared {
		localizationPreparedData = store.CloneValues(request.Data)
		if preparedValidationSelection.All {
			preparedValidationSelection.All = false
			if len(preparedValidationSelection.Chain) != 0 {
				preparedValidationSelection.Locale = preparedValidationSelection.Chain[0]
			}
		}
		hookSelection = preparedValidationSelection
		// A restored snapshot is already canonical. Fallback is for reads and must
		// not become an explicit translation when hooks and validation write it back.
		preparedValidationSelection = exactUpdateSelection(preparedValidationSelection)
		request.Data = projections.Values(request.Data, preparedValidationSelection)
		failureContext.Data = store.CloneValues(request.Data)
	}
	failureContext.Locale, failureContext.AllLocales = hookSelection.Locale, hookSelection.All
	failureContext.Collection = collection.Schema
	resourceAfterError = collection.Hooks.AfterError
	if request.Operation != operation.Create && request.Operation != operation.Duplicate && request.Operation != operation.Read && request.Operation != operation.Update && request.Operation != operation.Delete && request.Operation != operation.RestoreDeleted && request.Operation != operation.DeletePermanent && request.Operation != operation.Publish && request.Operation != operation.Unpublish {
		return Result{}, &Error{Code: "bad_operation", Status: 400, Message: fmt.Sprintf("operation %q is not supported", request.Operation)}
	}
	if collection.Schema.Capabilities.Global && (request.Operation == operation.Create || request.Operation == operation.Duplicate || request.Operation == operation.Delete || request.Operation == operation.RestoreDeleted || request.Operation == operation.DeletePermanent || request.Operation == operation.Read && request.ID == "") {
		return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "global singletons support read, update, publish, and unpublish operations"}
	}
	if request.TrashOnly && (request.Operation != operation.Read || !collection.Schema.Capabilities.Trash) {
		return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "trash queries require a trash-enabled collection read"}
	}
	if request.Draft != nil && request.Operation != operation.Read && request.Operation != operation.Create {
		return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "draft mode supports only reads and creates; use publish or unpublish to change status"}
	}
	if request.Draft != nil && collection.Schema.Versions == nil && request.Operation != operation.Read {
		return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "draft mode requires a version-enabled collection"}
	}
	if request.Draft != nil && collection.Schema.Versions != nil && request.Operation != operation.Read {
		if *request.Draft && !collection.Schema.Versions.Drafts {
			return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "collection does not support drafts"}
		}
		status := store.StatusPublished
		if *request.Draft {
			status = store.StatusDraft
		}
		request.Status = &status
	}
	if collection.Schema.Capabilities.Upload && !request.StoragePrepared {
		if request.Operation == operation.Create && request.ImportID == "" || request.Operation == operation.Duplicate {
			return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "upload collections require the storage-aware create or duplicate operation"}
		}
		if (request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish) &&
			hasSubmittedChanges && includesUploadMetadata(collection.Schema.Fields, submittedData) {
			return Result{}, &Error{Code: "field_access_denied", Status: 403, Message: "upload storage metadata is server-owned"}
		}
	}
	intent := transactionWrite
	if request.Operation == operation.Read {
		intent = transactionReadOnly
	}
	state, ownsTransaction, err := engine.transaction(ctx, intent)
	if err != nil {
		return Result{}, transactionAdmissionError("begin operation transaction", err)
	}
	if state.rollbackOnly != nil {
		return Result{}, transactionAbortedOperationError(state.rollbackOnly)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	state.addTransactionResource(request.TransactionResource)
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
				rollbackError := engine.rollbackTransactionState(ctx, state)
				if rollbackError != nil {
					err = rollbackOperationError("rollback operation transaction", err, rollbackError)
				}
			}
		}()
	}

	operationContext := Context{
		projections:     projections,
		Context:         transactionContext,
		Operation:       request.Operation,
		Collection:      collection.Schema,
		ID:              request.ID,
		Actor:           cloneDocumentPointer(request.Actor),
		ActorCollection: request.ActorCollection,
		Data:            store.CloneValues(request.Data),
		Locale:          selection.Locale,
		AllLocales:      selection.All,
		Locales:         append([]schema.LocaleCode(nil), selection.Configured...),
	}
	normalizeAuthIdentity(collection.Schema, operationContext.Data)
	accessAllLocales := selection.All
	decision := Decision{Kind: Allow}
	var submittedUpdateDecision *Decision
	if request.Operation != operation.Duplicate {
		if request.LocalizationPrepared {
			decision, err = authorizePreparedLocalization(collection, operationContext, localizationPreparedData, selection.Configured)
			accessAllLocales = len(selection.Configured) != 0
		} else {
			decision, err = authorize(collection, operationContext)
		}
		if err != nil {
			return Result{}, &Error{Code: "access_failed", Status: 500, Message: "access rule failed", Cause: err}
		}
		if decision.Kind == Deny || request.Operation == operation.Create && decision.Kind == Where {
			return Result{}, &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
		}
		if (request.Operation == operation.Publish || request.Operation == operation.Unpublish) && hasSubmittedChanges {
			updateContext := operationContext
			updateContext.Operation = operation.Update
			var updateDecision Decision
			if request.LocalizationPrepared {
				updateDecision, err = authorizePreparedLocalization(collection, updateContext, localizationPreparedData, selection.Configured)
			} else {
				updateDecision, err = authorize(collection, updateContext)
			}
			if err != nil {
				return Result{}, &Error{Code: "access_failed", Status: 500, Message: "update access rule failed", Cause: err}
			}
			if updateDecision.Kind == Deny {
				return Result{}, &Error{Code: "access_denied", Status: 403, Message: "editing during a publication transition is not permitted"}
			}
			submittedUpdateDecision = &updateDecision
		}
		if request.Operation == operation.Create && request.Draft != nil && !*request.Draft {
			publishContext := operationContext
			publishContext.Operation = operation.Publish
			publishDecision, publishError := authorize(collection, publishContext)
			if publishError != nil {
				return Result{}, &Error{Code: "access_failed", Status: 500, Message: "publish access rule failed", Cause: publishError}
			}
			if publishDecision.Kind != Allow {
				return Result{}, &Error{Code: "access_denied", Status: 403, Message: "publishing on create is not permitted"}
			}
		}
	}
	if err := authorizeQuery(collection, request.Filter, request.Sort); err != nil {
		return Result{}, err
	}
	if request.IndexWindow != nil {
		if err := authorizeQueryPath(collection, request.IndexWindow.Path); err != nil {
			return Result{}, err
		}
	}
	originalMissing := false
	var originalCanonical store.Values
	var originalCanonicalDocument *store.Document
	var originalVisible store.Values
	var originalIdentity store.Values
	var duplicateCanonical store.Values
	var duplicateVisible store.Values
	var duplicateReadContext Context
	if request.Operation == operation.Duplicate || request.Operation == operation.Update || request.Operation == operation.Delete || request.Operation == operation.RestoreDeleted || request.Operation == operation.DeletePermanent || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		deletion := store.DeletionActive
		if request.Operation == operation.RestoreDeleted || request.Operation == operation.DeletePermanent {
			deletion = store.DeletionTrash
		}
		findAccess := decision.Access
		if request.Operation == operation.Duplicate {
			readContext := operationContext
			readContext.Operation, readContext.Data = operation.Read, store.Values{}
			readDecision, accessError := authorize(collection, readContext)
			if accessError != nil {
				return Result{}, &Error{Code: "access_failed", Status: 500, Message: "source read access rule failed", Cause: accessError}
			}
			if readDecision.Kind == Deny {
				return Result{}, &Error{Code: "access_denied", Status: 403, Message: "source document may not be read"}
			}
			findAccess = readDecision.Access
			duplicateReadContext = readContext
		}
		original, findError := state.transaction.Find(transactionContext, store.Request{
			Collection: collection.Schema, Collections: engine.schemas, ID: request.ID, Access: findAccess, Deletion: deletion,
			Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: accessAllLocales, Lock: store.LockMutation,
		})
		if findError != nil {
			if collection.Schema.Capabilities.Global && request.Operation == operation.Update && decision.Kind == Allow && errors.Is(findError, store.ErrNotFound) {
				originalMissing = true
			} else {
				return Result{}, translateStoreError(findError)
			}
		} else {
			canonicalDocument := store.CloneDocument(original)
			if recoveryError := unknownBlockRecoveryError(collection.Schema.Fields, canonicalDocument.Values, true); recoveryError != nil {
				return Result{}, recoveryError
			}
			if request.Operation == operation.Update && collection.Schema.Versions != nil &&
				(original.Status == store.StatusPublished || request.Status != nil) {
				return Result{}, &Error{Code: "publish_required", Status: 409, Message: "published documents and status transitions require the publish lifecycle"}
			}
			if submittedUpdateDecision != nil {
				if _, accessError := state.transaction.Find(transactionContext, store.Request{
					Collection: collection.Schema, Collections: engine.schemas, ID: request.ID,
					Access: submittedUpdateDecision.Access, Deletion: deletion,
					Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: accessAllLocales, Lock: store.LockMutation,
				}); accessError != nil {
					if errors.Is(accessError, store.ErrNotFound) {
						return Result{}, &Error{Code: "access_denied", Status: 403, Message: "editing during a publication transition is not permitted"}
					}
					return Result{}, translateStoreError(accessError)
				}
			}
			if request.Operation == operation.Duplicate {
				// The canonical source contains every locale, so each retained locale
				// must independently pass collection Read access before it can be
				// copied. Keep the source row locked across these checks.
				for _, locale := range selection.Configured {
					localeContext := duplicateReadContext
					localeContext.Locale, localeContext.AllLocales = locale, true
					localeDecision, accessError := authorize(collection, localeContext)
					if accessError != nil {
						return Result{}, &Error{Code: "access_failed", Status: 500, Message: "source locale read access rule failed", Cause: accessError}
					}
					if localeDecision.Kind == Deny {
						return Result{}, &Error{Code: "access_denied", Status: 403, Message: "every retained source locale must be readable"}
					}
					if _, accessError := state.transaction.Find(transactionContext, store.Request{
						Collection: collection.Schema, Collections: engine.schemas, ID: request.ID, Access: localeDecision.Access,
						Deletion: deletion, Locales: selection.Configured, LocaleChain: []schema.LocaleCode{locale}, Lock: store.LockMutation,
					}); accessError != nil {
						if errors.Is(accessError, store.ErrNotFound) {
							return Result{}, &Error{Code: "access_denied", Status: 403, Message: "every retained source locale must be readable"}
						}
						return Result{}, translateStoreError(accessError)
					}
				}
				// A duplicate may change values that field-read rules depend on (for
				// example, ownership). Strip source-hidden values while the rule still
				// sees the source document, and do so for every retained locale, before
				// those values can become input to the new document.
				sourceContext := duplicateReadContext
				sourceContext.Document = &canonicalDocument
				sourceContext.ID = canonicalDocument.ID
				sourceContext.AllLocales = len(selection.Configured) != 0
				sourceResult := Result{Document: &canonicalDocument}
				if redactError := engine.redactResult(collection, sourceContext, &sourceResult); redactError != nil {
					return Result{}, redactError
				}
			}
			originalCanonical = store.CloneValues(canonicalDocument.Values)
			originalCanonicalDocument = &canonicalDocument
			operationContext.originalCanonical = originalCanonicalDocument
			visibleSelection := selection
			if request.LocalizationPrepared || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
				visibleSelection = hookSelection
			}
			visibleOriginal := projections.Document(canonicalDocument, visibleSelection)
			originalVisible = store.CloneValues(visibleOriginal.Values)
			operationContext.Original = cloneDocumentPointer(&visibleOriginal)
			if request.Operation == operation.Delete || request.Operation == operation.DeletePermanent || request.Operation == operation.RestoreDeleted {
				request.Data, operationContext.Data = store.CloneValues(visibleOriginal.Values), store.CloneValues(visibleOriginal.Values)
			} else if request.Operation == operation.Publish || request.Operation == operation.Unpublish {
				values := store.CloneValues(request.Data)
				if !request.replaceValues {
					// Status-only calls still present the complete current document to
					// lifecycle hooks. Publish-with-data overlays submitted edits so the
					// same atomic operation can validate, authorize, hook, persist, and
					// expose the edited document without an ordinary update first.
					values = store.CloneValues(visibleOriginal.Values)
					for name, value := range request.Data {
						values[name] = value
					}
				}
				request.Data, operationContext.Data = values, store.CloneValues(values)
			}
			if request.Operation == operation.Duplicate {
				values := store.CloneValues(visibleOriginal.Values)
				for name, value := range request.Data {
					values[name] = value
				}
				candidate, candidateError := duplicateCanonicalValues(collection.Schema.Fields, originalCanonical, visibleOriginal.Values, values, selection)
				if candidateError != nil {
					return Result{}, candidateError
				}
				// Merge selected-locale overrides while source keys still correlate,
				// then rekey the complete copied tree exactly once across all locales.
				if identityError := prepareRowIdentities(collection.Schema.Fields, candidate, true, true); identityError != nil {
					return Result{}, identityError
				}
				duplicateCanonical = store.CloneValues(candidate)
				duplicateVisible = projections.Values(candidate, visibleSelection)
				// Projection only visits schema fields; retain unknown submitted
				// properties for the ordinary validator to reject (including id).
				knownFields := make(map[string]bool, len(collection.Schema.Fields))
				for _, field := range collection.Schema.Fields {
					knownFields[field.Name] = true
				}
				for name, value := range request.Data {
					if !knownFields[name] {
						duplicateVisible[name] = value
					}
				}
				request.Data, operationContext.Data = store.CloneValues(duplicateVisible), store.CloneValues(duplicateVisible)

				createLocales := selection.Configured
				if len(createLocales) == 0 {
					createLocales = []schema.LocaleCode{""}
				}
				for _, locale := range createLocales {
					createContext := operationContext
					createContext.Locale = locale
					createContext.AllLocales = len(selection.Configured) != 0
					createContext.Data = store.CloneValues(candidate)
					if locale != "" {
						createContext.Data = projections.Values(candidate, localization.Selection{Locale: locale, Chain: []schema.LocaleCode{locale}, Configured: selection.Configured})
					}
					localeDecision, accessError := authorize(collection, createContext)
					if accessError != nil {
						return Result{}, &Error{Code: "access_failed", Status: 500, Message: "access rule failed", Cause: accessError}
					}
					if localeDecision.Kind != Allow {
						return Result{}, &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted for every retained locale"}
					}
				}
				decision = Decision{Kind: Allow}
			}
		}
		if (request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish) &&
			hasSubmittedChanges && collection.Schema.Auth != nil && collection.Schema.Auth.VerifyEmail &&
			authIdentityChanges(collection.Schema, original.Values, submittedData) {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: []schema.Issue{{Code: "immutable_verified_identity", Path: collection.Schema.Auth.IdentityField, Message: "verified auth identities cannot be changed in place"}}}
		}
	}
	operationContext.Locale, operationContext.AllLocales = hookSelection.Locale, hookSelection.All
	operationContext.submittedData = store.CloneValues(submittedData)
	operationContext.submittedAllLocales = request.LocalizationPrepared
	operationContext.submittedLocale = selection.Locale
	if request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		// A fallback occurrence belongs to another whole-field translation. Only
		// the exact write locale can establish a retained occurrence's identity.
		identitySelection := exactUpdateSelection(hookSelection)
		if request.copyLocaleSource != "" {
			identitySelection.Locale = request.copyLocaleSource
			identitySelection.Chain = []schema.LocaleCode{request.copyLocaleSource}
		}
		originalIdentity = projections.Values(originalCanonical, identitySelection)
		if identityError := validateBlockTypeIdentity(collection.Schema.Fields, originalIdentity, operationContext.Data, false); identityError != nil {
			return Result{}, identityError
		}
	}
	if request.Operation == operation.Duplicate {
		if err := runIdentityHooks(collection.Hooks.BeforeDuplicate, operationContext); err != nil {
			return Result{}, hookError("before duplicate", err)
		}
		if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeDuplicate }, true); err != nil {
			return Result{}, hookError("field before duplicate", err)
		}
	}
	if request.Operation == operation.Read {
		if err := runHooks(collection.Hooks.BeforeRead, operationContext); err != nil {
			return Result{}, hookError("before read", err)
		}
	}
	if request.Operation == operation.Delete || request.Operation == operation.DeletePermanent {
		if err := runHooks(collection.Hooks.BeforeDelete, operationContext); err != nil {
			return Result{}, hookError("before delete", err)
		}
		if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeDelete }); err != nil {
			return Result{}, hookError("field before delete", err)
		}
	}

	if err := runIdentityHooks(collection.Hooks.BeforeValidate, operationContext); err != nil {
		return Result{}, hookError("before validation", err)
	}
	if request.Operation != operation.Read {
		if identityError := prepareRowIdentities(collection.Schema.Fields, operationContext.Data, false, false); identityError != nil {
			return Result{}, identityError
		}
	}
	if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeValidate }, true); err != nil {
		return Result{}, hookError("field before validation", err)
	}
	if request.Operation != operation.Read {
		if identityError := prepareRowIdentities(collection.Schema.Fields, operationContext.Data, false, false); identityError != nil {
			return Result{}, identityError
		}
	}
	normalizeSlugFields(collection.Schema.Fields, operationContext.Data, submittedData, operationContext.Original, request.Operation)
	request.Data = store.CloneValues(operationContext.Data)
	if request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		if identityError := validateBlockTypeIdentity(collection.Schema.Fields, originalIdentity, request.Data, false); identityError != nil {
			return Result{}, identityError
		}
	}
	mutationValidation := validationOptions{requireMissing: request.Operation == operation.Create || request.Operation == operation.Duplicate || originalMissing}
	defaults := defaultResults{}
	if request.LocalizationPrepared {
		// Retained snapshot occurrences may intentionally lack this translation.
		mutationValidation.previous = projections.Values(localizationPreparedData, preparedValidationSelection)
	}
	if !request.LocalizationPrepared && (request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish) {
		if !originalMissing {
			mutationValidation.previous = projections.Values(originalCanonical, exactUpdateSelection(hookSelection))
		}
		completed, mergeError := completeUpdateForValidation(collection.Schema.Fields, originalCanonical, request.Data, hookSelection, projections)
		if mergeError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: mergeError}
		}
		request.Data = completed
	}
	if request.Operation == operation.Create || request.Operation == operation.Duplicate || request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		normalizeAuthIdentity(collection.Schema, request.Data)
		initialized, defaultError := initializeDynamicDefaults(collection, operationContext, request.Data, mutationValidation, defaults)
		if defaultError != nil {
			return Result{}, defaultError
		}
		request.Data = initialized
		validated, validationIssues := validateWithOptions(collection.Schema.Fields, request.Data, mutationValidation, engine.pluginValidators)
		validationIssues = append(validationIssues, validateAuthIdentity(collection.Schema, validated)...)
		correlatePrimitiveListIssues(collection.Schema, request.Data, operationContext, validationIssues)
		if len(validationIssues) != 0 {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: validationIssues}
		}
		request.Data = validated
		operationContext.Data = store.CloneValues(validated)
	}
	fieldAuthorizationContext := operationContext
	fieldAuthorizationBefore := store.Values(nil)
	fieldAuthorizationAfter := request.Data
	if request.Operation == operation.Duplicate {
		candidate, candidateError := duplicateCanonicalValues(collection.Schema.Fields, duplicateCanonical, duplicateVisible, request.Data, selection)
		if candidateError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized duplicate validation failed", Cause: candidateError}
		}
		fieldAuthorizationContext.Data = store.CloneValues(candidate)
		fieldAuthorizationContext.Original = cloneDocumentPointer(originalCanonicalDocument)
		fieldAuthorizationContext.Locale = ""
		fieldAuthorizationContext.AllLocales = len(selection.Configured) != 0
		fieldAuthorizationAfter = candidate
	} else if request.LocalizationPrepared {
		preparedPatch, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, preparedValidationSelection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		fieldAuthorizationContext.Data = localization.MergeStoragePatch(collection.Schema.Fields, localizationPreparedData, preparedPatch)
		fieldAuthorizationContext.Original = cloneDocumentPointer(originalCanonicalDocument)
		fieldAuthorizationContext.Locale = ""
		fieldAuthorizationContext.AllLocales = len(selection.Configured) != 0
		fieldAuthorizationBefore = store.CloneValues(originalCanonical)
		fieldAuthorizationAfter = fieldAuthorizationContext.Data
	} else if request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		writeSelection := selection
		if request.Operation == operation.Publish || request.Operation == operation.Unpublish {
			writeSelection = hookSelection
		}
		if writeSelection.Locale != "" && !writeSelection.All {
			writeSelection.Chain = []schema.LocaleCode{writeSelection.Locale}
		}
		storagePatch, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, writeSelection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		candidateCanonical := localization.MergeStoragePatch(collection.Schema.Fields, originalCanonical, storagePatch)
		fieldAuthorizationAfter = projections.Values(candidateCanonical, writeSelection)
		if originalCanonicalDocument != nil {
			fieldAuthorizationBefore = projections.Values(originalCanonicalDocument.Values, writeSelection)
		}
	}
	fieldAuthorizationOperation := request.Operation
	if fieldAuthorizationOperation == operation.Publish || fieldAuthorizationOperation == operation.Unpublish {
		fieldAuthorizationOperation = operation.Update
	}
	if !request.SkipFieldAccess {
		if err := authorizeBoundFields(collection, fieldAuthorizationContext, fieldAuthorizationOperation, fieldAuthorizationBefore, fieldAuthorizationAfter, request.LocalizationPrepared); err != nil {
			return Result{}, err
		}
	}

	storeSelection := appendOptionalPaths(request.Select)
	if request.Select != nil && selectedVirtualOutput(collection.Schema.Fields, request.OutputFields) {
		// Virtual resolvers do not declare dependencies. Fetch the complete stored
		// document for trusted resolver input, then restore the caller's projection
		// immediately after output resolution.
		storeSelection = nil
	}
	storeRequest := store.Request{
		Collection:       collection.Schema,
		Collections:      engine.schemas,
		ID:               request.ID,
		Access:           decision.Access,
		Page:             request.Page,
		Limit:            request.Limit,
		Sort:             append([]query.Sort(nil), request.Sort...),
		IndexWindow:      cloneIndexWindow(request.IndexWindow),
		Select:           storeSelection,
		Populate:         append([]query.Population(nil), request.Populate...),
		PopulationAccess: make(map[schema.StableID]*query.Node),
		PublishedOnly:    publishedOnly(request, collection.Schema),
		ExpectedRevision: request.ExpectedRevision,
		Locales:          append([]schema.LocaleCode(nil), selection.Configured...),
		LocaleChain:      append([]schema.LocaleCode(nil), selection.Chain...),
		AllLocales:       accessAllLocales,
	}
	if request.TrashOnly {
		storeRequest.Deletion = store.DeletionTrash
	}
	if populationError := engine.preparePopulations(collection, operationContext, selection, &storeRequest); populationError != nil {
		return Result{}, populationError
	}
	if len(storeRequest.Populate) != 0 {
		storeRequest.PopulationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
	}
	if request.Filter != nil {
		node := request.Filter.Node()
		storeRequest.Filter = &node
	}
	if request.internalFilter != nil {
		if storeRequest.Filter == nil {
			storeRequest.Filter = request.internalFilter
		} else {
			node := query.Node{Kind: query.ExpressionAnd, Children: []query.Node{*request.internalFilter, *storeRequest.Filter}}
			storeRequest.Filter = &node
		}
	}
	if changesDocument(request.Operation) {
		if err := runIdentityHooks(collection.Hooks.BeforeChange, operationContext); err != nil {
			return Result{}, hookError("before change", err)
		}
		if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeChange }, true, true); err != nil {
			return Result{}, hookError("field before change", err)
		}
	}
	if err := runIdentityHooks(collection.Hooks.BeforeOperation, operationContext); err != nil {
		return Result{}, hookError("before operation", err)
	}
	if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeOperation }, true, true); err != nil {
		return Result{}, hookError("field before operation", err)
	}
	if request.Operation != operation.Read {
		if identityError := prepareRowIdentities(collection.Schema.Fields, operationContext.Data, false, false); identityError != nil {
			return Result{}, identityError
		}
	}
	normalizeSlugFields(collection.Schema.Fields, operationContext.Data, submittedData, operationContext.Original, request.Operation)
	request.Data = store.CloneValues(operationContext.Data)
	if request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		if identityError := validateBlockTypeIdentity(collection.Schema.Fields, originalIdentity, request.Data, false); identityError != nil {
			return Result{}, identityError
		}
	}
	if !request.LocalizationPrepared && (request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish) {
		completed, mergeError := completeUpdateForValidation(collection.Schema.Fields, originalCanonical, request.Data, hookSelection, projections)
		if mergeError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: mergeError}
		}
		request.Data = completed
	}
	if request.Operation == operation.Create || request.Operation == operation.Duplicate || request.Operation == operation.Update || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		normalizeAuthIdentity(collection.Schema, request.Data)
		initialized, defaultError := initializeDynamicDefaults(collection, operationContext, request.Data, mutationValidation, defaults)
		if defaultError != nil {
			return Result{}, defaultError
		}
		request.Data = initialized
		validated, validationIssues := validateWithOptions(collection.Schema.Fields, request.Data, mutationValidation, engine.pluginValidators)
		validationIssues = append(validationIssues, validateAuthIdentity(collection.Schema, validated)...)
		correlatePrimitiveListIssues(collection.Schema, request.Data, operationContext, validationIssues)
		if len(validationIssues) != 0 {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "document validation failed after before-operation hooks", Issues: validationIssues}
		}
		request.Data, operationContext.Data = validated, store.CloneValues(validated)
	}
	referenceData := request.Data
	referenceSelection := hookSelection
	if request.Operation == operation.Duplicate {
		candidate, candidateError := duplicateCanonicalValues(collection.Schema.Fields, duplicateCanonical, duplicateVisible, request.Data, selection)
		if candidateError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized duplicate validation failed", Cause: candidateError}
		}
		referenceData = candidate
		if len(selection.Configured) != 0 {
			referenceSelection = localization.Selection{All: true, Configured: append([]schema.LocaleCode(nil), selection.Configured...)}
		}
	} else if request.LocalizationPrepared {
		preparedPatch, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, preparedValidationSelection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		localizationPreparedData = localization.MergeStoragePatch(collection.Schema.Fields, localizationPreparedData, preparedPatch)
		referenceData = localizationPreparedData
		if len(selection.Configured) != 0 {
			referenceSelection = localization.Selection{
				All: true, Chain: append([]schema.LocaleCode(nil), selection.Chain...),
				Configured: append([]schema.LocaleCode(nil), selection.Configured...),
			}
		}
	} else if request.Operation == operation.Create {
		// A create writes one selected locale but establishes the canonical
		// document observed by every configured locale. Validate that stored
		// candidate through every effective fallback view before it can commit.
		candidate, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, selection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		referenceData = candidate
		if len(selection.Configured) != 0 {
			referenceSelection = localization.Selection{
				All: true, Configured: append([]schema.LocaleCode(nil), selection.Configured...),
			}
		}
	} else if request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		// A status transition exposes or retains one canonical document, not just
		// the locale projected into hooks. Validate the final hook-mutated locale
		// together with every untouched persisted locale before changing status.
		statusChanges := changedValues(originalVisible, request.Data)
		statusPatch, storageError := localization.StoragePatch(collection.Schema.Fields, statusChanges, hookSelection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		referenceData = localization.MergeStoragePatch(collection.Schema.Fields, originalCanonical, statusPatch)
		if len(selection.Configured) != 0 {
			referenceSelection = localization.Selection{
				All: true, Configured: append([]schema.LocaleCode(nil), selection.Configured...),
			}
		}
	} else if request.Operation == operation.Update {
		// Reference filters are predicates over the final document, not only the
		// submitted patch. Rebuild the canonical post-hook candidate so changing a
		// source field revalidates unchanged references, including retained locale
		// values and omitted protected children of structured fields.
		updatePatch, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, selection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		updatePatch = localization.MergeStorageUpdate(collection.Schema.Fields, originalCanonical, updatePatch)
		referenceData = localization.MergeStoragePatch(collection.Schema.Fields, originalCanonical, updatePatch)
		if len(selection.Configured) != 0 {
			referenceSelection = localization.Selection{
				All: true, Configured: append([]schema.LocaleCode(nil), selection.Configured...),
			}
		}
	} else if request.Operation == operation.RestoreDeleted {
		// Restoring makes the complete trashed document visible again. Validate its
		// canonical references in the same locked transaction before resurrection.
		referenceData = store.CloneValues(originalCanonical)
		if len(selection.Configured) != 0 {
			referenceSelection = localization.Selection{
				All: true, Configured: append([]schema.LocaleCode(nil), selection.Configured...),
			}
		}
	}
	if changesDocument(request.Operation) {
		// Final candidate admission and additive rules follow the existing second
		// codec checkpoint. Ordinary writes inspect only the exact write locale;
		// canonical copy/restore operations inspect each persisted occurrence.
		finalContext := operationContext
		finalSelection := exactUpdateSelection(hookSelection)
		if request.Operation == operation.Duplicate || request.LocalizationPrepared {
			finalSelection = referenceSelection
		}
		finalContext.Data = projections.Values(referenceData, finalSelection)
		finalContext.Document = nil
		finalContext.Locale, finalContext.AllLocales = finalSelection.Locale, finalSelection.All
		if !request.SkipFieldAccess {
			admissionContext := operationContext
			admissionContext.Data, admissionContext.Document = referenceData, nil
			admissionContext.AllLocales = referenceSelection.All
			if err := authorizeBoundFields(collection, admissionContext, fieldAuthorizationOperation, originalCanonical, referenceData, request.LocalizationPrepared); err != nil {
				return Result{}, err
			}
		}
		if err := validateBoundFields(collection, finalContext); err != nil {
			return Result{}, err
		}
	}
	validateExistingUploadObjects := collection.Schema.Capabilities.Upload &&
		(request.Operation == operation.Create && request.ImportID != "" || request.ValidateUploadObjects)
	if validateExistingUploadObjects {
		keys, keyError := importedUploadObjectKeys(referenceData)
		if keyError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "adopted upload object metadata is invalid", Cause: keyError}
		}
		locker, supported := state.transaction.(store.UploadObjectLocker)
		if !supported {
			locker, supported = engine.store.(store.UploadObjectLocker)
		}
		if !supported {
			return Result{}, &Error{Code: "store_failed", Status: 500, Message: "upload object adoption requires store.UploadObjectLocker"}
		}
		if lockError := state.lockUploadObjects(transactionContext, locker, keys); lockError != nil {
			return Result{}, &Error{Code: "store_failed", Status: 500, Message: "lock adopted upload objects", Cause: lockError}
		}
		if engine.validateUploadImport == nil {
			return Result{}, &Error{Code: "store_failed", Status: 500, Message: "upload object validation is unavailable"}
		}
		if validationError := engine.validateUploadImport(transactionContext, collection.Schema, referenceData); validationError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "adopted upload objects are invalid", Cause: validationError}
		}
	}
	if request.Operation == operation.Create || request.Operation == operation.Duplicate || request.Operation == operation.Update || request.Operation == operation.RestoreDeleted || request.Operation == operation.Publish || request.Operation == operation.Unpublish {
		referenceIssues, referenceError := engine.validateDocumentReferences(operationContext, state.transaction, referenceData, referenceSelection)
		if referenceError != nil {
			return Result{}, referenceError
		}
		if len(referenceIssues) != 0 {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "document reference validation failed", Issues: referenceIssues}
		}
		slugIssues, slugError := validateSlugUniqueness(transactionContext, state.transaction, collection.Schema, engine.schemas, referenceData, request.ID, selection.Configured)
		if slugError != nil {
			return Result{}, &Error{Code: "store_failed", Status: 500, Message: "validate slug uniqueness", Cause: slugError}
		}
		if len(slugIssues) != 0 {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "document validation failed", Issues: slugIssues}
		}
	}
	hardDelete := request.Operation == operation.DeletePermanent || request.Operation == operation.Delete && !collection.Schema.Capabilities.Trash
	if hardDelete {
		target := store.DocumentReference{CollectionID: collection.Schema.ID, DocumentID: request.ID}
		ignoreOwners := append([]store.DocumentReference(nil), state.referenceDeleteDeletedOwners...)
		ignoresTarget := false
		for _, owner := range ignoreOwners {
			ignoresTarget = ignoresTarget || owner == target
		}
		if !ignoresTarget {
			ignoreOwners = append(ignoreOwners, target)
		}
		if err := state.transaction.ApplyReferenceDelete(transactionContext, store.ReferenceDeleteRequest{
			Target:       target,
			Collections:  engine.schemas,
			IgnoreOwners: ignoreOwners,
		}); err != nil {
			return Result{}, translateStoreError(err)
		}
	}
	switch request.Operation {
	case operation.Create:
		status := store.Status("")
		if request.Status != nil {
			status = *request.Status
		}
		storageValues, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, selection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		createID := request.ImportID
		if createID == "" {
			createID = request.ID
		}
		document, storeError := state.transaction.Create(transactionContext, store.CreateRequest{Collection: collection.Schema, ID: createID, Values: storageValues, Status: status, CreatedAt: request.ImportCreatedAt, UpdatedAt: request.ImportUpdatedAt, Locales: selection.Configured})
		result.Document, err = documentResult(document, storeError)
	case operation.Duplicate:
		storageValues, storageError := duplicateCanonicalValues(collection.Schema.Fields, duplicateCanonical, duplicateVisible, request.Data, selection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		document, storeError := state.transaction.Create(transactionContext, store.CreateRequest{Collection: collection.Schema, Values: storageValues, Locales: selection.Configured})
		result.Document, err = documentResult(document, storeError)
	case operation.Read:
		if request.ID == "" {
			if request.IndexWindow != nil {
				if validationError := validateIndexWindow(collection.Schema, request, decision); validationError != nil {
					return Result{}, validationError
				}
				windowTransaction, supportsWindows := state.transaction.(store.WindowTransaction)
				if !supportsWindows {
					return Result{}, &Error{Code: "store_failed", Status: 500, Message: "list windows require store.WindowTransaction"}
				}
				window, storeError := windowTransaction.ListWindow(transactionContext, storeRequest)
				if storeError != nil {
					return Result{}, translateStoreError(storeError)
				}
				result.Page = &store.Page{Documents: window.Documents, Page: 1, Limit: request.Limit, Total: -1}
				result.WindowHasMore = window.HasMore
			} else {
				page, storeError := state.transaction.List(transactionContext, storeRequest)
				result.Page, err = pageResult(page, storeError)
			}
		} else {
			document, storeError := state.transaction.Find(transactionContext, storeRequest)
			if collection.Schema.Capabilities.Global && decision.Kind == Allow && errors.Is(storeError, store.ErrNotFound) {
				values, _ := validate(collection.Schema.Fields, store.Values{}, true, engine.pluginValidators)
				status := store.StatusPublished
				if collection.Schema.Versions != nil && collection.Schema.Versions.Drafts {
					status = store.StatusDraft
				}
				document := store.Document{ID: request.ID, Status: status, Values: values}
				projectDocumentValues(&document, storeRequest.Select)
				result.Document = &document
			} else {
				result.Document, err = documentResult(document, storeError)
			}
		}
	case operation.Update:
		var document store.Document
		var storeError error
		if originalMissing {
			status := store.StatusPublished
			if request.Status != nil {
				status = *request.Status
			} else if collection.Schema.Versions != nil && collection.Schema.Versions.Drafts {
				status = store.StatusDraft
			}
			storageValues, storageError := localization.StoragePatch(collection.Schema.Fields, request.Data, selection)
			if storageError != nil {
				return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
			}
			document, storeError = state.transaction.Create(transactionContext, store.CreateRequest{Collection: collection.Schema, ID: request.ID, Values: storageValues, Status: status, Locales: selection.Configured})
		} else {
			storageValues := localizationPreparedData
			if !request.LocalizationPrepared {
				var storageError error
				storageValues, storageError = localization.StoragePatch(collection.Schema.Fields, request.Data, selection)
				if storageError != nil {
					return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
				}
				storageValues = localization.MergeStorageUpdate(collection.Schema.Fields, originalCanonical, storageValues)
			}
			document, storeError = state.transaction.Update(transactionContext, store.UpdateRequest{Request: storeRequest, Values: storageValues, Status: request.Status, ReplaceValues: request.replaceValues})
		}
		result.Document, err = documentResult(document, storeError)
	case operation.Publish, operation.Unpublish:
		if collection.Schema.Versions == nil {
			return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "collection does not support versions"}
		}
		status := store.StatusPublished
		if request.Operation == operation.Unpublish {
			status = store.StatusDraft
		}
		statusChanges := changedValues(originalVisible, request.Data)
		if !request.LocalizationPrepared {
			for name := range submittedData {
				statusChanges[name] = request.Data[name]
			}
		}
		storageValues, storageError := localization.StoragePatch(collection.Schema.Fields, statusChanges, hookSelection)
		if storageError != nil {
			return Result{}, &Error{Code: "validation", Status: 422, Message: "localized document validation failed", Cause: storageError}
		}
		if request.LocalizationPrepared {
			storageValues = localization.MergeStoragePatch(collection.Schema.Fields, localizationPreparedData, storageValues)
		} else {
			storageValues = localization.MergeStorageUpdate(collection.Schema.Fields, originalCanonical, storageValues)
		}
		document, storeError := state.transaction.Update(transactionContext, store.UpdateRequest{Request: storeRequest, Values: storageValues, Status: &status, ReplaceValues: request.replaceValues})
		result.Document, err = documentResult(document, storeError)
	case operation.Delete:
		var document store.Document
		var storeError error
		if collection.Schema.Capabilities.Trash {
			document, storeError = state.transaction.Trash(transactionContext, storeRequest)
		} else {
			document, storeError = state.transaction.Delete(transactionContext, storeRequest)
		}
		result.Document, err = documentResult(document, storeError)
	case operation.RestoreDeleted:
		if !collection.Schema.Capabilities.Trash {
			return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "collection does not support trash"}
		}
		storeRequest.Deletion = store.DeletionTrash
		document, storeError := state.transaction.Restore(transactionContext, storeRequest)
		result.Document, err = documentResult(document, storeError)
	case operation.DeletePermanent:
		if !collection.Schema.Capabilities.Trash {
			return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "collection does not support trash"}
		}
		storeRequest.Deletion = store.DeletionTrash
		document, storeError := state.transaction.Delete(transactionContext, storeRequest)
		result.Document, err = documentResult(document, storeError)
	}
	if err != nil {
		return Result{}, err
	}
	// Version snapshots own canonical stored values, never a transport-specific
	// population shape. Save the raw write result before mutation responses are
	// re-read with populated relationship documents.
	if collection.Schema.Versions != nil && result.Document != nil && recordsVersion(request.Operation) {
		versionTransaction, supportsVersions := state.transaction.(store.VersionTransaction)
		if !supportsVersions {
			return Result{}, &Error{Code: "store_failed", Status: 500, Message: "version-enabled collection requires store.VersionTransaction"}
		}
		if _, err := versionTransaction.SaveVersion(transactionContext, collection.Schema, *result.Document, collection.Schema.Versions.MaxPerDocument); err != nil {
			return Result{}, translateStoreError(err)
		}
	}
	mutationResponseRequest := func(projection localization.Selection) store.Request {
		responseRequest := storeRequest
		if len(responseRequest.Populate) != 0 {
			responseRequest.PopulationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
		}
		responseRequest.ID = result.Document.ID
		responseRequest.Filter = nil
		responseRequest.Access = nil
		responseRequest.PublishedOnly = false
		responseRequest.ExpectedRevision = 0
		responseRequest.Locales = append([]schema.LocaleCode(nil), projection.Configured...)
		responseRequest.LocaleChain = append([]schema.LocaleCode(nil), projection.Chain...)
		responseRequest.AllLocales = projection.All
		if request.Operation == operation.Delete {
			responseRequest.Deletion = store.DeletionTrash
		} else {
			responseRequest.Deletion = store.DeletionActive
		}
		return responseRequest
	}
	// Mutation transports such as GraphQL can request relationship population
	// for the returned document. Re-read the just-written row through the same
	// transaction so adapters apply their ordinary bounded population plan and
	// target access predicates without opening a second transaction. The root
	// mutation was already authorized above; only related documents are subject
	// to their read predicates here. A hard-deleted row can no longer be read.
	if request.Operation != operation.Read && result.Document != nil && len(request.Populate) != 0 &&
		request.Operation != operation.DeletePermanent &&
		!(request.Operation == operation.Delete && !collection.Schema.Capabilities.Trash) {
		responseRequest := mutationResponseRequest(selection)
		populated, populationError := state.transaction.Find(transactionContext, responseRequest)
		if populationError != nil {
			return Result{}, translateStoreError(populationError)
		}
		result.Document = &populated
	}
	// Hook documents use the operation's write locale even when the response
	// requests every locale. Capture that projection only after response
	// population so unchanged hooks cannot discard populated relationships.
	if result.Document != nil {
		if recoveryError := engine.validateBlockResponse(collection, result.Document, selection.All, true); recoveryError != nil {
			return Result{}, recoveryError
		}
	}
	if result.Page != nil {
		for index := range result.Page.Documents {
			if recoveryError := engine.validateBlockResponse(collection, &result.Page.Documents[index], selection.All, true); recoveryError != nil {
				return Result{}, recoveryError
			}
		}
	}
	var hookDocumentBase *store.Document
	if result.Document != nil {
		hookSource := result.Document
		if len(request.Populate) != 0 &&
			(hookSelection.Locale != selection.Locale || hookSelection.All != selection.All) &&
			request.Operation != operation.DeletePermanent &&
			!(request.Operation == operation.Delete && !collection.Schema.Capabilities.Trash) {
			// Root projection cannot reinterpret already-populated target documents
			// because their field schemas belong to another collection. Re-read the
			// just-written row with the hook locale so the store projects every
			// populated level through its own schema before write hooks observe it.
			hookPopulated, populationError := state.transaction.Find(transactionContext, mutationResponseRequest(hookSelection))
			if populationError != nil {
				return Result{}, translateStoreError(populationError)
			}
			if recoveryError := engine.validateBlockResponse(collection, &hookPopulated, hookSelection.All, true); recoveryError != nil {
				return Result{}, recoveryError
			}
			hookSource = &hookPopulated
		}
		hookDocument := projections.Document(*hookSource, hookSelection)
		hookDocumentBase = &hookDocument
	}
	if hardDelete {
		deletedOwner := store.DocumentReference{
			CollectionID: collection.Schema.ID,
			DocumentID:   request.ID,
		}
		if err := state.transaction.DeleteDocumentState(transactionContext, deletedOwner); err != nil {
			return Result{}, translateStoreError(err)
		}
		state.referenceDeleteDeletedOwners = append(state.referenceDeleteDeletedOwners, deletedOwner)
		state.permanentDeletes = append(state.permanentDeletes, PermanentDelete{
			Collection: collection.Schema,
			DocumentID: request.ID,
			Original:   store.CloneDocument(*originalCanonicalDocument),
		})
	}
	if request.TransactionMutation != nil {
		if result.Document == nil || !changesDocument(request.Operation) {
			return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "transaction mutation requires a document write"}
		}
		if err := request.TransactionMutation(transactionContext, state.transaction, collection.Schema, *result.Document); err != nil {
			return Result{}, translateStoreError(err)
		}
	}
	if result.Document != nil {
		projected := projections.Document(*result.Document, selection)
		result.Document = &projected
	}
	if result.Page != nil {
		for index := range result.Page.Documents {
			result.Page.Documents[index] = projections.Document(result.Page.Documents[index], selection)
		}
	}
	if hookDocumentBase != nil {
		document := store.CloneDocument(*hookDocumentBase)
		operationContext.Document = &document
	}
	divergentHookProjection := hookSelection.Locale != selection.Locale || hookSelection.All != selection.All
	if changesDocument(request.Operation) {
		if err := runHooks(collection.Hooks.AfterChange, operationContext); err != nil {
			return Result{}, hookError("after change", err)
		}
		if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.AfterChange }); err != nil {
			return Result{}, hookError("field after change", err)
		}
		if !divergentHookProjection && result.Document != nil && operationContext.Document != nil {
			document := store.CloneDocument(*operationContext.Document)
			result.Document = &document
		}
	}
	if request.Operation == operation.Delete || request.Operation == operation.DeletePermanent {
		if err := runHooks(collection.Hooks.AfterDelete, operationContext); err != nil {
			return Result{}, hookError("after delete", err)
		}
		if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.AfterDelete }); err != nil {
			return Result{}, hookError("field after delete", err)
		}
	}
	if err := runHooks(collection.Hooks.AfterOperation, operationContext); err != nil {
		return Result{}, hookError("after operation", err)
	}
	if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.AfterOperation }); err != nil {
		return Result{}, hookError("field after operation", err)
	}
	if result.Document != nil && operationContext.Document != nil {
		if divergentHookProjection && hookDocumentBase != nil {
			hookChanges := changedValues(hookDocumentBase.Values, operationContext.Document.Values)
			hookPatch, storageError := localization.StoragePatch(collection.Schema.Fields, hookChanges, hookSelection)
			if storageError != nil {
				return Result{}, &Error{Code: "validation", Status: 422, Message: "localized hook response mutation failed", Cause: storageError}
			}
			document := store.CloneDocument(*result.Document)
			document.Values = localization.MergeStoragePatch(collection.Schema.Fields, document.Values, hookPatch)
			result.Document = &document
		} else {
			document := store.CloneDocument(*operationContext.Document)
			result.Document = &document
		}
	}
	queueBoundAfterCommit(state, collection, operationContext)
	for _, hook := range collection.Hooks.AfterCommit {
		deferredContext := operationContext
		deferredContext.projections = nil
		state.afterCommit = append(state.afterCommit, deferredHook{hook: hook, context: deferredContext})
	}
	responseContext := operationContext
	responseContext.Locale, responseContext.AllLocales = selection.Locale, selection.All
	if err := engine.resolveOutputFields(state.transaction, collection, responseContext, selection, request.OutputFields, &result); err != nil {
		return Result{}, err
	}
	projectSelectedResult(collection.Schema.Fields, request.Select, request.OutputFields, &result)
	if err := runReadHooks(collection, responseContext, &result); err != nil {
		return Result{}, err
	}
	if err := engine.redactResult(collection, responseContext, &result); err != nil {
		return Result{}, err
	}

	if !ownsTransaction {
		return result, nil
	}
	if err := engine.commitTransaction(ctx, state); err != nil {
		return Result{}, commitAttemptedError(err)
	}
	afterCommitError := engine.dispatchCommittedEffects(ctx, state)
	if afterCommitError != nil {
		return Result{}, committedHookError(afterCommitError)
	}
	return result, nil
}

// ExecuteBatch runs ordinary operations in one transaction. Every item uses
// the same access, validation, hook, version, and redaction pipeline as Execute.
func (engine *Engine) ExecuteBatch(ctx context.Context, requests []Request) (results []Result, err error) {
	if len(requests) == 0 || len(requests) > MaxBatchDocuments {
		return nil, &Error{Code: "bad_request", Status: 400, Message: fmt.Sprintf("bulk operations require between 1 and %d documents", MaxBatchDocuments)}
	}
	seenTargets := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		if request.ID == "" {
			continue
		}
		target := request.Collection + "\x00" + request.ID
		if _, duplicate := seenTargets[target]; duplicate {
			return nil, &Error{Code: "bad_request", Status: 400, Message: "bulk operations require unique document IDs"}
		}
		seenTargets[target] = struct{}{}
	}
	intent := transactionReadOnly
	for _, request := range requests {
		if request.Operation != operation.Read {
			intent = transactionWrite
			break
		}
	}
	state, ownsTransaction, err := engine.transaction(ctx, intent)
	if err != nil {
		return nil, transactionAdmissionError("begin bulk operation transaction", err)
	}
	if state.rollbackOnly != nil {
		return nil, transactionAbortedOperationError(state.rollbackOnly)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if !state.finished {
				if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
					err = rollbackOperationError("rollback bulk operation transaction", err, rollbackError)
				}
			}
		}()
	}
	results = make([]Result, len(requests))
	for index, request := range requests {
		results[index], err = engine.Execute(transactionContext, request)
		if err != nil {
			return nil, err
		}
	}
	if !ownsTransaction {
		return results, nil
	}
	if err := engine.commitTransaction(ctx, state); err != nil {
		return nil, commitAttemptedError(err)
	}
	afterCommitError := engine.dispatchCommittedEffects(ctx, state)
	if afterCommitError != nil {
		return nil, committedHookError(afterCommitError)
	}
	return results, nil
}

func normalizeAuthIdentity(collection schema.Collection, values store.Values) {
	if collection.Auth == nil || values == nil {
		return
	}
	value, exists := values[collection.Auth.IdentityField]
	if !exists {
		return
	}
	identity, valid := value.StringValue()
	if !valid {
		return
	}
	values[collection.Auth.IdentityField] = store.String(store.CanonicalAuthIdentity(identity))
}

func includesUploadMetadata(fields []schema.Field, values store.Values) bool {
	for _, field := range fields {
		if field.Category == schema.FieldCategoryUpload {
			if _, submitted := values[field.Name]; submitted {
				return true
			}
		}
	}
	return false
}

func importedUploadObjectKeys(values store.Values) ([]string, error) {
	unique := make(map[string]struct{})
	if key, valid := values["objectKey"].StringValue(); valid && key != "" {
		unique[key] = struct{}{}
	}
	if sizes := values["sizes"]; sizes.Kind() == store.ValueObject {
		if sizes.Len() > store.MaxUploadReferenceCandidates-1 {
			return nil, fmt.Errorf("upload metadata contains more than %d image variants", store.MaxUploadReferenceCandidates-1)
		}
		for _, metadata := range sizes.Entries() {
			if metadata.Kind() != store.ValueObject {
				continue
			}
			if key, valid := metadata.Get("objectKey").StringValue(); valid && key != "" {
				unique[key] = struct{}{}
			}
		}
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("upload object metadata is missing")
	}
	if len(unique) > store.MaxUploadReferenceCandidates {
		return nil, fmt.Errorf("upload metadata contains more than %d object keys", store.MaxUploadReferenceCandidates)
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func validateAuthIdentity(collection schema.Collection, values store.Values) []schema.Issue {
	if collection.Auth == nil {
		return nil
	}
	value, exists := values[collection.Auth.IdentityField]
	if !exists {
		return nil
	}
	identity, valid := value.StringValue()
	if !valid || identity == "" {
		return nil
	}
	address, err := mail.ParseAddress(identity)
	if err != nil || address.Address != identity {
		return []schema.Issue{{Code: "email", Path: collection.Auth.IdentityField, Message: "must be a valid email address"}}
	}
	return nil
}

func authIdentityChanges(collection schema.Collection, original, incoming store.Values) bool {
	value, submitted := incoming[collection.Auth.IdentityField]
	if !submitted {
		return false
	}
	next, nextValid := value.StringValue()
	previous, previousValid := original[collection.Auth.IdentityField].StringValue()
	return nextValid && previousValid && store.CanonicalAuthIdentity(next) != store.CanonicalAuthIdentity(previous)
}

func recordsVersion(operationKind operation.Kind) bool {
	switch operationKind {
	case operation.Create, operation.Duplicate, operation.Update, operation.Publish, operation.Unpublish:
		return true
	default:
		return false
	}
}

// changedValues keeps duplicate hooks and caller overrides locale-scoped while
// allowing the untouched canonical source to retain every authored locale.
func changedValues(before, after store.Values) store.Values {
	changed := store.Values{}
	for name, value := range after {
		if previous, exists := before[name]; !exists || !reflect.DeepEqual(previous, value) {
			changed[name] = value
		}
	}
	return changed
}

func duplicateCanonicalValues(fields []schema.Field, canonical, visible, after store.Values, selection localization.Selection) (store.Values, error) {
	changed := changedValues(visible, after)
	storagePatch, err := localization.StoragePatch(fields, changed, selection)
	if err != nil {
		return nil, err
	}
	values := localization.MergeStoragePatch(fields, canonical, storagePatch)
	removeDuplicateValues(fields, visible, after, selection, values)
	return values, nil
}

func removeDuplicateValues(fields []schema.Field, before, after store.Values, selection localization.Selection, values store.Values) {
	localized := make(map[string]bool, len(fields))
	for _, field := range fields {
		localized[field.Name] = field.Localized
	}
	for name := range before {
		if _, retained := after[name]; retained {
			continue
		}
		if !localized[name] || selection.All || selection.Locale == "" {
			delete(values, name)
			continue
		}
		byLocale, valid := values[name].CopyObject()
		if !valid {
			delete(values, name)
			continue
		}
		delete(byLocale, string(selection.Locale))
		if len(byLocale) == 0 {
			delete(values, name)
		} else {
			values[name] = store.Object(byLocale)
		}
	}
}

func (engine *Engine) Versions(ctx context.Context, collectionName, documentID string, actor *store.Document, localeOptions ...LocalizationOptions) (versions []store.Version, err error) {
	return engine.readVersions(ctx, collectionName, documentID, 0, actor, localeOptions...)
}

// readVersions runs one ReadVersions lifecycle. When revision is positive it
// narrows the access-filtered result before resolving output fields and hooks,
// so a single-version read cannot trigger side effects or failures from an
// unrelated retained revision.
func (engine *Engine) readVersions(ctx context.Context, collectionName, documentID string, revision int, actor *store.Document, localeOptions ...LocalizationOptions) (versions []store.Version, err error) {
	collection, exists := engine.collections[collectionName]
	if !exists || collection.Schema.Versions == nil {
		return nil, &Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	var options LocalizationOptions
	if len(localeOptions) != 0 {
		options = localeOptions[len(localeOptions)-1]
	}
	selection, localeError := localization.Resolve(engine.localization, options.Locale, options.FallbackLocales, options.DisableFallback, options.AllLocales)
	if localeError != nil {
		return nil, &Error{Code: "bad_locale", Status: 400, Message: localeError.Error(), Cause: localeError}
	}
	stack, _ := ctx.Value(operationStackKey{}).([]operationFrame)
	frame := operationFrame{kind: operation.ReadVersions, collection: collectionName, id: documentID}
	if len(stack) >= engine.maxDepth || repeatedFrame(stack, frame) >= 4 {
		return nil, &Error{Code: "operation_recursion", Status: 409, Message: "operation recursion limit exceeded"}
	}
	ctx = context.WithValue(ctx, operationStackKey{}, append(append([]operationFrame(nil), stack...), frame))
	state, ownsTransaction, err := engine.transaction(ctx, transactionReadOnly)
	if err != nil {
		return nil, transactionAdmissionError("begin version read transaction", err)
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	if ownsTransaction {
		defer func() {
			if !state.finished {
				if rollbackError := engine.rollbackTransactionState(ctx, state); rollbackError != nil {
					err = rollbackOperationError("rollback version read transaction", err, rollbackError)
				}
			}
		}()
	}
	operationContext := Context{Context: transactionContext, Operation: operation.ReadVersions, Collection: collection.Schema, ID: documentID, Actor: cloneDocumentPointer(actor), ActorCollection: options.ActorCollection, Data: store.Values{}, Locale: selection.Locale, AllLocales: selection.All, Locales: append([]schema.LocaleCode(nil), selection.Configured...)}
	defer func() {
		if err == nil {
			return
		}
		failureContext := operationContext
		failureContext.Error = err
		if hookFailure := runHooks(collection.Hooks.AfterError, failureContext); hookFailure != nil {
			err = errors.Join(err, hookError("resource after error", hookFailure))
		}
		if hookFailure := runHooks(engine.rootAfterError, failureContext); hookFailure != nil {
			err = errors.Join(err, hookError("root after error", hookFailure))
		}
	}()
	decision, err := authorize(collection, operationContext)
	if err != nil {
		return nil, &Error{Code: "access_failed", Status: 500, Message: "access rule failed", Cause: err}
	}
	if decision.Kind == Deny {
		return nil, &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	if err := runHooks(collection.Hooks.BeforeRead, operationContext); err != nil {
		return nil, hookError("before read", err)
	}
	if err := runIdentityHooks(collection.Hooks.BeforeValidate, operationContext); err != nil {
		return nil, hookError("before validation", err)
	}
	if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeValidate }, true); err != nil {
		return nil, hookError("field before validation", err)
	}
	if err := runIdentityHooks(collection.Hooks.BeforeOperation, operationContext); err != nil {
		return nil, hookError("before operation", err)
	}
	if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.BeforeOperation }, true, true); err != nil {
		return nil, hookError("field before operation", err)
	}
	versionTransaction, ok := state.transaction.(store.VersionTransaction)
	if !ok {
		return nil, &Error{Code: "store_failed", Status: 500, Message: "version-enabled collection requires store.VersionTransaction"}
	}
	versions, err = versionTransaction.ListVersions(transactionContext, store.VersionRequest{
		Collection: collection.Schema, DocumentID: documentID, Access: decision.Access,
		Locales: selection.Configured, LocaleChain: selection.Chain, AllLocales: selection.All,
	})
	if err != nil {
		return nil, translateStoreError(err)
	}
	if revision > 0 {
		found := -1
		for index := range versions {
			if versions[index].Revision == revision {
				found = index
				break
			}
		}
		if found < 0 {
			return nil, &Error{Code: "not_found", Status: 404, Message: "version was not found"}
		}
		versions = []store.Version{versions[found]}
	}
	if err := runHooks(collection.Hooks.AfterOperation, operationContext); err != nil {
		return nil, hookError("after operation", err)
	}
	if err := runFieldHooks(collection, operationContext, func(hooks Hooks) []Hook { return hooks.AfterOperation }); err != nil {
		return nil, hookError("field after operation", err)
	}
	queueBoundAfterCommit(state, collection, operationContext)
	for _, hook := range collection.Hooks.AfterCommit {
		deferredContext := operationContext
		deferredContext.projections = nil
		state.afterCommit = append(state.afterCommit, deferredHook{hook: hook, context: deferredContext})
	}
	for index := range versions {
		if recoveryError := unknownBlockRecoveryError(collection.Schema.Fields, versions[index].Snapshot.Values, true); recoveryError != nil {
			return nil, recoveryError
		}
		versions[index].Snapshot = localization.ProjectDocument(versions[index].Snapshot, collection.Schema.Fields, selection)
		versionContext := operationContext
		versionContext.Operation = operation.Read
		versionContext.Document = &versions[index].Snapshot
		versionResult := Result{Document: &versions[index].Snapshot}
		if err := engine.resolveOutputFields(state.transaction, collection, versionContext, selection, nil, &versionResult); err != nil {
			return nil, err
		}
		if err := runReadHooks(collection, versionContext, &versionResult); err != nil {
			return nil, err
		}
		if err := engine.redactResult(collection, versionContext, &versionResult); err != nil {
			return nil, err
		}
	}
	if ownsTransaction {
		if err := engine.commitTransaction(ctx, state); err != nil {
			return nil, commitAttemptedError(err)
		}
		if err := engine.dispatchDeferredHooks(ctx, state.afterCommit); err != nil {
			return nil, committedHookError(err)
		}
	}
	return versions, nil
}

// Version returns one authorized snapshot revision.
func (engine *Engine) Version(ctx context.Context, collectionName, documentID string, revision int, actor *store.Document, localeOptions ...LocalizationOptions) (store.Version, error) {
	if revision < 1 {
		return store.Version{}, &Error{Code: "bad_request", Status: 400, Message: "version revision must be a positive integer"}
	}
	versions, err := engine.readVersions(ctx, collectionName, documentID, revision, actor, localeOptions...)
	if err != nil {
		return store.Version{}, err
	}
	return versions[0], nil
}

func (engine *Engine) Restore(ctx context.Context, collectionName, documentID string, revision, expectedRevision int, draft bool, actor *store.Document, localeOptions ...LocalizationOptions) (Result, error) {
	return engine.RestorePopulated(ctx, collectionName, documentID, revision, expectedRevision, draft, actor, nil, nil, localeOptions...)
}

// RestorePopulated restores a revision and applies a bounded relationship
// population plan to the returned document in the update transaction.
func (engine *Engine) RestorePopulated(ctx context.Context, collectionName, documentID string, revision, expectedRevision int, draft bool, actor *store.Document, populations []query.Population, outputFields []query.Path, localeOptions ...LocalizationOptions) (result Result, err error) {
	collection, exists := engine.collections[collectionName]
	if !exists || collection.Schema.Versions == nil {
		return Result{}, &Error{Code: "not_found", Status: 404, Message: "versioned collection was not found"}
	}
	if draft && !collection.Schema.Versions.Drafts {
		return Result{}, &Error{Code: "bad_operation", Status: 400, Message: "collection does not support drafts"}
	}
	var options LocalizationOptions
	if len(localeOptions) != 0 {
		options = localeOptions[len(localeOptions)-1]
	}
	versionSelection, err := localization.Resolve(engine.localization, options.Locale, options.FallbackLocales, options.DisableFallback, options.AllLocales)
	if err != nil {
		return Result{}, &Error{Code: "bad_locale", Status: 400, Message: err.Error(), Cause: err}
	}
	if engine.localization != nil {
		versionSelection, err = localization.Resolve(engine.localization, "", nil, false, true)
		if err != nil {
			return Result{}, &Error{Code: "bad_locale", Status: 400, Message: err.Error(), Cause: err}
		}
	}
	state, ownsTransaction, err := engine.transaction(ctx, transactionWrite)
	if err != nil {
		return Result{}, transactionAdmissionError("begin version restore transaction", err)
	}
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
				err = rollbackOperationError("rollback version restore transaction", err, rollbackError)
			}
		}()
	}
	versionTransaction, ok := state.transaction.(store.VersionTransaction)
	if !ok {
		return Result{}, &Error{Code: "store_failed", Status: 500, Message: "version-enabled collection requires store.VersionTransaction"}
	}
	transactionContext := context.WithValue(ctx, transactionKey{}, state)
	versionContext := Context{
		Context: transactionContext, Operation: operation.ReadVersions, Collection: collection.Schema,
		ID: documentID, Actor: cloneDocumentPointer(actor), ActorCollection: options.ActorCollection, Locale: versionSelection.Locale, AllLocales: versionSelection.All,
		Locales: append([]schema.LocaleCode(nil), versionSelection.Configured...),
	}
	var decision Decision
	if versionSelection.All {
		decision, err = authorizePreparedLocalization(collection, versionContext, store.Values{}, versionSelection.Configured)
	} else {
		decision, err = authorize(collection, versionContext)
	}
	if err != nil {
		return Result{}, &Error{Code: "access_failed", Status: 500, Message: "version access rule failed", Cause: err}
	}
	if decision.Kind == Deny {
		return Result{}, &Error{Code: "access_denied", Status: 403, Message: "operation is not permitted"}
	}
	versions, err := versionTransaction.ListVersions(transactionContext, store.VersionRequest{
		Collection: collection.Schema, DocumentID: documentID, Access: decision.Access,
		Locales: versionSelection.Configured, LocaleChain: versionSelection.Chain, AllLocales: versionSelection.All,
	})
	if err != nil {
		return Result{}, translateStoreError(err)
	}
	var version store.Version
	for _, candidate := range versions {
		if candidate.Revision == revision {
			version = candidate
			break
		}
	}
	if version.Revision == 0 {
		return Result{}, &Error{Code: "not_found", Status: 404, Message: "version was not found"}
	}
	if recoveryError := unknownBlockRecoveryError(collection.Schema.Fields, version.Snapshot.Values, true); recoveryError != nil {
		return Result{}, recoveryError
	}

	status := version.Status
	if draft {
		status = store.StatusDraft
	}
	operationKind := operation.Publish
	if status == store.StatusDraft {
		operationKind = operation.Unpublish
	}
	result, err = engine.Execute(transactionContext, Request{
		Operation: operationKind, Collection: collectionName, ID: documentID, Data: version.Snapshot.Values,
		Actor: actor, ActorCollection: options.ActorCollection, ExpectedRevision: expectedRevision, Status: &status,
		Populate:        append([]query.Population(nil), populations...),
		OutputFields:    appendOptionalPaths(outputFields),
		StoragePrepared: collection.Schema.Capabilities.Upload, ValidateUploadObjects: collection.Schema.Capabilities.Upload, LocalizationPrepared: true,
		replaceValues: true,
		Locale:        options.Locale, FallbackLocales: options.FallbackLocales,
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	})
	if err != nil {
		return Result{}, err
	}
	if ownsTransaction {
		if err := engine.commitTransaction(ctx, state); err != nil {
			return Result{}, commitAttemptedError(err)
		}
		if err := engine.dispatchDeferredHooks(ctx, state.afterCommit); err != nil {
			return Result{}, committedHookError(err)
		}
	}
	return result, nil
}

func relationshipSchemaTargets(relationship *schema.RelationshipField) []schema.RelationshipTarget {
	if relationship.Polymorphic {
		return relationship.Targets
	}
	return []schema.RelationshipTarget{{CollectionID: relationship.CollectionID, CollectionSlug: relationship.CollectionSlug}}
}

type operationStackKey struct{}
type operationFrame struct {
	kind           operation.Kind
	collection, id string
}

func repeatedFrame(stack []operationFrame, frame operationFrame) int {
	count := 0
	for _, existing := range stack {
		if existing == frame {
			count++
		}
	}
	return count
}

func removedFieldLocations(changed, after []fieldLocation) []fieldLocation {
	retained := make(map[string]bool, len(after))
	for _, location := range after {
		retained[location.identity] = true
	}
	removed := make([]fieldLocation, 0)
	for _, location := range changed {
		if !retained[location.identity] {
			removed = append(removed, location)
		}
	}
	return removed
}

func changedFieldLocations(before, after []fieldLocation) []fieldLocation {
	beforeByIdentity := make(map[string]fieldLocation, len(before))
	afterByIdentity := make(map[string]fieldLocation, len(after))
	identities := make(map[string]bool, len(before)+len(after))
	for _, location := range before {
		beforeByIdentity[location.identity] = location
		identities[location.identity] = true
	}
	for _, location := range after {
		afterByIdentity[location.identity] = location
		identities[location.identity] = true
	}
	ordered := make([]string, 0, len(identities))
	for identity := range identities {
		ordered = append(ordered, identity)
	}
	sort.Strings(ordered)
	changed := make([]fieldLocation, 0, len(ordered))
	for _, identity := range ordered {
		previous, existed := beforeByIdentity[identity]
		next, remains := afterByIdentity[identity]
		if existed && remains && reflect.DeepEqual(previous.value, next.value) {
			continue
		}
		if remains {
			changed = append(changed, next)
			continue
		}
		previous.value = store.Null()
		changed = append(changed, previous)
	}
	return changed
}

func (engine *Engine) redactResult(collection Collection, operationContext Context, result *Result) error {
	var redact func(Collection, Context, *store.Document) error
	redact = func(current Collection, currentContext Context, document *store.Document) error {
		currentContext.ID = document.ID
		if recoveryError := unknownBlockRecoveryError(current.Schema.Fields, document.Values, currentContext.AllLocales); recoveryError != nil {
			return recoveryError
		}
		if err := redactBoundFields(current, currentContext, document); err != nil {
			return err
		}
		var nestedError error
		document.Values = population.MapPopulatedDocuments(current.Schema.Fields, document.Values, currentContext.AllLocales, func(targetID schema.StableID, locale schema.LocaleCode, populated store.Document) store.Document {
			if nestedError != nil {
				return populated
			}
			target, available := engine.collections[string(targetID)]
			if !available {
				return populated
			}
			targetContext := currentContext
			// A projector is bound to one collection's root schema.
			targetContext.projections = nil
			if locale != "" {
				targetContext.Locale = locale
			}
			targetContext.Collection, targetContext.ID, targetContext.Document = target.Schema, populated.ID, &populated
			targetContext.Data, targetContext.Original = store.Values{}, nil
			nestedError = redact(target, targetContext, &populated)
			return populated
		})
		if nestedError != nil {
			return nestedError
		}
		pruneLocalizationSources(document)
		return nil
	}
	if result.Document != nil {
		if err := redact(collection, operationContext, result.Document); err != nil {
			if failure, ok := err.(*Error); ok && failure.Code == "block_recovery_required" {
				return failure
			}
			return &Error{Code: "access_failed", Status: 500, Message: "field access rule failed", Cause: err}
		}
	}
	if result.Page != nil {
		for index := range result.Page.Documents {
			if err := redact(collection, operationContext, &result.Page.Documents[index]); err != nil {
				if failure, ok := err.(*Error); ok && failure.Code == "block_recovery_required" {
					return failure
				}
				return &Error{Code: "access_failed", Status: 500, Message: "field access rule failed", Cause: err}
			}
		}
	}
	return nil
}

func deleteLocalizationSources(sources map[string]schema.LocaleCode, path string) {
	for candidate := range sources {
		if candidate == path || strings.HasPrefix(candidate, path+".") {
			delete(sources, candidate)
		}
	}
}

func pruneLocalizationSources(document *store.Document) {
	for path := range document.LocalizationSources {
		if !valueExistsAtRuntimePath(document.Values, path) {
			delete(document.LocalizationSources, path)
		}
	}
	if len(document.LocalizationSources) == 0 {
		document.LocalizationSources = nil
	}
}

func valueExistsAtRuntimePath(values store.Values, path string) bool {
	segments := strings.Split(path, ".")
	current, exists := values[segments[0]]
	if !exists {
		return false
	}
	for _, segment := range segments[1:] {
		if current.Kind() == store.ValueObject {
			current, exists = current.Lookup(segment)
		} else {
			index, err := strconv.Atoi(segment)
			if err != nil {
				return false
			}
			current, exists = current.ListItem(index)
		}
		if !exists {
			return false
		}
	}
	return true
}

func fieldRelationship(field *schema.Field) *schema.RelationshipField {
	if field == nil {
		return nil
	}
	if field.Relationship != nil {
		return field.Relationship
	}
	if field.Upload != nil {
		return &schema.RelationshipField{CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug, HasMany: field.Upload.HasMany}
	}
	if field.Join != nil {
		return &schema.RelationshipField{CollectionID: field.Join.CollectionID, CollectionSlug: field.Join.CollectionSlug, HasMany: true}
	}
	return nil
}

func (engine *Engine) resolveOutputFields(transaction store.Transaction, collection Collection, operationContext Context, selection localization.Selection, selected []query.Path, result *Result) error {
	resolve := func(document *store.Document) error {
		for _, candidate := range collection.Schema.Fields {
			if !outputFieldSelected(candidate.Path, selected) {
				continue
			}
			switch candidate.Type {
			case schema.FieldTypeVirtual:
				var resolver Computed
				resolverContext := operationContext
				for _, binding := range collection.Bindings {
					if binding.Field.ID == candidate.ID && binding.Computed != nil {
						resolver = binding.Computed
						resolverContext.Document, resolverContext.Data = document, document.Values
						locations := fieldLocationsAtPath(collection.Schema.Fields, document.Values, candidate.Path.String(), operationContext.AllLocales, true)
						if len(locations) != 0 {
							resolverContext = scopedBindingContext(resolverContext, binding, locations[0], nil)
						}
						break
					}
				}
				if resolver == nil {
					return &Error{Code: "computed_failed", Status: 500, Message: fmt.Sprintf("virtual field %q has no resolver", candidate.Path.String())}
				}
				value, err := resolver(resolverContext, store.CloneDocument(*document))
				if err != nil {
					return &Error{Code: "computed_failed", Status: 500, Message: fmt.Sprintf("compute virtual field %q", candidate.Path.String()), Cause: err}
				}
				if !validVirtualValue(candidate, value) {
					return &Error{Code: "invalid_computed_value", Status: 500, Message: fmt.Sprintf("virtual field %q returned %q, expected %q", candidate.Path.String(), value.Kind(), candidate.Virtual.ValueType)}
				}
				document.Values[candidate.Name] = value
			case schema.FieldTypeJoin:
				target, exists := engine.collections[string(candidate.Join.CollectionID)]
				if !exists {
					return &Error{Code: "computed_failed", Status: 500, Message: fmt.Sprintf("join field %q targets an unavailable collection", candidate.Path.String())}
				}
				joinContext := operationContext
				joinContext.projections = nil
				joinContext.Operation, joinContext.Collection, joinContext.ID = operation.Read, target.Schema, ""
				joinContext.Data, joinContext.Value, joinContext.SiblingData = store.Values{}, store.Value{}, nil
				joinContext.Document, joinContext.Original = nil, nil
				joinContext.FieldPath, joinContext.RuntimePath, joinContext.Error = "", "", nil
				decision, err := authorize(target, joinContext)
				if err != nil {
					return &Error{Code: "access_failed", Status: 500, Message: "join access rule failed", Cause: err}
				}
				if decision.Kind == Deny {
					document.Values[candidate.Name] = store.List()
					continue
				}
				filter := query.Equal(candidate.Join.On, query.String(document.ID)).Node()
				page, err := transaction.List(operationContext.Context, store.Request{
					Collection: target.Schema, Collections: engine.schemas, Filter: &filter, Access: decision.Access,
					Page: 1, Limit: candidate.Join.Limit, Sort: joinDefaultSort(candidate.Join.DefaultSort),
					PublishedOnly: operationContext.Actor == nil && target.Schema.Versions != nil,
					Locales:       append([]schema.LocaleCode(nil), selection.Configured...), LocaleChain: append([]schema.LocaleCode(nil), selection.Chain...), AllLocales: selection.All,
				})
				if err != nil {
					return translateStoreError(err)
				}
				items := make([]store.Value, len(page.Documents))
				for index, related := range page.Documents {
					if err := engine.validateBlockResponse(target, &related, selection.All, true); err != nil {
						return err
					}
					projected := localization.ProjectDocument(related, target.Schema.Fields, selection)
					targetResult := Result{Document: &projected}
					if err := runReadHooks(target, joinContext, &targetResult); err != nil {
						return err
					}
					items[index] = store.Populated(*targetResult.Document)
				}
				document.Values[candidate.Name] = store.List(items...)
			}
		}
		return nil
	}
	if result.Document != nil {
		if err := resolve(result.Document); err != nil {
			return err
		}
	}
	if result.Page != nil {
		for index := range result.Page.Documents {
			if err := resolve(&result.Page.Documents[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func outputFieldSelected(path query.Path, selected []query.Path) bool {
	if selected == nil {
		return true
	}
	for _, candidate := range selected {
		if candidate.String() == path.String() {
			return true
		}
	}
	return false
}

func selectedVirtualOutput(fields []schema.Field, selected []query.Path) bool {
	for _, field := range fields {
		if field.Type == schema.FieldTypeVirtual && outputFieldSelected(field.Path, selected) {
			return true
		}
	}
	return false
}

func projectSelectedResult(fields []schema.Field, stored, output []query.Path, result *Result) {
	if stored == nil {
		return
	}
	selected := appendOptionalPaths(stored)
	for _, field := range fields {
		if (field.Type == schema.FieldTypeJoin || field.Type == schema.FieldTypeVirtual) && outputFieldSelected(field.Path, output) {
			selected = append(selected, field.Path)
		}
	}
	if result.Document != nil {
		projectDocumentValues(result.Document, selected)
	}
	if result.Page != nil {
		for index := range result.Page.Documents {
			projectDocumentValues(&result.Page.Documents[index], selected)
		}
	}
}

func projectDocumentValues(document *store.Document, selected []query.Path) {
	if selected == nil {
		return
	}
	values := make(store.Values, len(selected))
	for _, path := range selected {
		segments := path.Segments()
		if len(segments) != 1 {
			continue
		}
		if value, exists := document.Values[segments[0]]; exists {
			values[segments[0]] = value
		}
	}
	document.Values = store.CloneValues(values)
}

func appendOptionalPaths(paths []query.Path) []query.Path {
	if paths == nil {
		return nil
	}
	return append([]query.Path{}, paths...)
}

func cloneIndexWindow(window *store.IndexWindow) *store.IndexWindow {
	if window == nil {
		return nil
	}
	path, err := query.ParsePath(window.Path.String())
	if err != nil {
		return &store.IndexWindow{LowerBound: window.LowerBound, UpperBound: window.UpperBound}
	}
	return &store.IndexWindow{Path: path, LowerBound: window.LowerBound, UpperBound: window.UpperBound}
}

func validateIndexWindow(collection schema.Collection, request Request, decision Decision) *Error {
	window := request.IndexWindow
	if request.Limit < 1 || request.Limit > MaxBatchDocuments {
		return &Error{Code: "bad_request", Status: 400, Message: fmt.Sprintf("list windows require a limit between 1 and %d", MaxBatchDocuments)}
	}
	if window == nil || window.Path.String() == "" || window.LowerBound == "" || window.UpperBound == "" || window.LowerBound >= window.UpperBound {
		return &Error{Code: "bad_request", Status: 400, Message: "list windows require a valid non-empty half-open index range"}
	}
	if decision.Kind != Allow {
		return &Error{Code: "bad_request", Status: 400, Message: "list windows require collection read access to resolve to allow"}
	}
	if request.Filter != nil || len(request.Sort) != 0 || len(request.Populate) != 0 || request.Page != 0 || request.TrashOnly || request.Draft != nil {
		return &Error{Code: "bad_request", Status: 400, Message: "list windows do not support filters, custom sorting, population, paging, trash, or draft selection"}
	}
	if collection.Capabilities.Trash || collection.Capabilities.Versions {
		return &Error{Code: "bad_request", Status: 400, Message: "list windows require an unversioned collection without trash"}
	}
	segments := window.Path.Segments()
	if len(segments) != 1 {
		return &Error{Code: "bad_request", Status: 400, Message: "list window indexes must be direct fields"}
	}
	for _, candidate := range collection.Fields {
		if candidate.Name != segments[0] {
			continue
		}
		if candidate.Category != schema.FieldCategoryScalar || candidate.Type != schema.FieldTypeText || candidate.Localized || !candidate.Unique || !candidate.Index {
			return &Error{Code: "bad_request", Status: 400, Message: "list window indexes must be unique, indexed, non-localized text fields"}
		}
		return nil
	}
	return &Error{Code: "bad_request", Status: 400, Message: fmt.Sprintf("list window index field %q is not defined", window.Path.String())}
}

func joinDefaultSort(raw string) []query.Sort {
	direction := query.Ascending
	pathName := raw
	if strings.HasPrefix(pathName, "-") {
		direction = query.Descending
		pathName = strings.TrimPrefix(pathName, "-")
	}
	if pathName == "" {
		return nil
	}
	path, err := query.ParsePath(pathName)
	if err != nil {
		return nil
	}
	sort, err := query.NewSort(path, direction)
	if err != nil {
		return nil
	}
	return []query.Sort{sort}
}

func validVirtualValue(field schema.Field, value store.Value) bool {
	if value.Kind() == store.ValueNull {
		return !field.Required
	}
	if field.Virtual == nil {
		return false
	}
	switch field.Virtual.ValueType {
	case schema.ValueTypeString:
		return value.Kind() == store.ValueString
	case schema.ValueTypeNumber:
		return value.Kind() == store.ValueNumber
	case schema.ValueTypeBoolean:
		return value.Kind() == store.ValueBoolean
	case schema.ValueTypeJSON:
		return value.Kind() == store.ValueString || value.Kind() == store.ValueNumber || value.Kind() == store.ValueBoolean || value.Kind() == store.ValueObject || value.Kind() == store.ValueList || value.Kind() == store.ValueNull
	default:
		return false
	}
}

func runReadHooks(collection Collection, operationContext Context, result *Result) error {
	run := func(document *store.Document) error {
		readContext := operationContext
		readContext.ID = document.ID
		readContext.Data = document.Values
		readContext.Document = document
		if err := runHooks(collection.Hooks.AfterRead, readContext); err != nil {
			return hookError("after read", err)
		}
		if err := runFieldHooks(collection, readContext, func(hooks Hooks) []Hook { return hooks.AfterRead }); err != nil {
			return hookError("field after read", err)
		}
		for _, field := range collection.Schema.Fields {
			if field.Type != schema.FieldTypeVirtual {
				continue
			}
			if value, present := document.Values[field.Name]; present && !validVirtualValue(field, value) {
				return &Error{Code: "invalid_computed_value", Status: 500, Message: fmt.Sprintf("virtual field %q has invalid %q output after read hooks; expected %q", field.Path.String(), value.Kind(), field.Virtual.ValueType)}
			}
		}
		return validateReadOutput(collection.Schema.Fields, collection.Schema.Fields, document.Values, readContext.AllLocales)
	}
	if result.Document != nil {
		return run(result.Document)
	}
	if result.Page != nil {
		for index := range result.Page.Documents {
			if err := run(&result.Page.Documents[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

// Selection/redaction may omit a property, but a read transform cannot return
// explicit null for a field whose generated output contract is nonnullable.
// Primitive lists also retain their declared element type and finite numbers.
// Authoring length/range rules remain write validation, not output formatting rules.
func validateReadOutput(fields, root []schema.Field, values store.Values, allLocales bool) error {
	for _, field := range fields {
		if field.Required || primitivefield.IsList(field) {
			for _, location := range fieldLocationsAtPath(root, values, field.Path.String(), allLocales) {
				if field.Required && location.value.Kind() == store.ValueNull {
					return &Error{Code: "invalid_field_output", Status: 500, Message: fmt.Sprintf("required field %q returned null after read hooks", location.runtimePath)}
				}
				if primitivefield.IsList(field) {
					if err := validatePrimitiveListReadOutput(field, location.value, location.runtimePath); err != nil {
						return err
					}
				}
			}
		}
		if err := validateReadOutput(schema.ChildFields(field), root, values, allLocales); err != nil {
			return err
		}
	}
	return nil
}

func changesDocument(kind operation.Kind) bool {
	return kind == operation.Create || kind == operation.Duplicate || kind == operation.Update || kind == operation.Publish || kind == operation.Unpublish
}

func valueAtPath(fields []schema.Field, values store.Values, path string, allLocales bool) (store.Value, bool) {
	locations := fieldLocationsAtPath(fields, values, path, allLocales)
	if len(locations) == 0 {
		return store.Value{}, false
	}
	return locations[0].value, true
}

type fieldLocation struct {
	value           store.Value
	siblings        store.Value
	runtimePath     string
	parentPath      string
	fields          []schema.Field
	identity        string
	bindingIdentity string
	locale          schema.LocaleCode
}

func fieldLocationSiblings(fields []schema.Field, values store.Value, allLocales bool, locale schema.LocaleCode) store.Value {
	if !allLocales || locale == "" {
		return values
	}
	object, _ := values.CopyObject()
	projected := localization.ProjectDocument(store.Document{Values: object}, fields, localization.Selection{
		Locale: locale, Chain: []schema.LocaleCode{locale},
	})
	return store.Object(projected.Values)
}

func fieldLocationsAtPath(fields []schema.Field, values store.Values, path string, allLocales bool, includeMissing ...bool) []fieldLocation {
	return collectFieldLocations(fields, store.Object(values), path, "", "", "", allLocales, "", includeMissing...)
}

func collectFieldLocations(fields []schema.Field, values store.Value, path, runtimePrefix, identityPrefix, bindingPrefix string, allLocales bool, locale schema.LocaleCode, includeMissing ...bool) []fieldLocation {
	for _, field := range fields {
		canonicalPath := field.Path.String()
		if path != canonicalPath && !strings.HasPrefix(path, canonicalPath+".") {
			continue
		}
		value, exists := values.Lookup(field.Name)
		if !exists && !(path == canonicalPath && len(includeMissing) > 0 && includeMissing[0]) {
			return nil
		}
		runtimePath := joinFieldPath(runtimePrefix, field.Name)
		identityPath := joinFieldPath(identityPrefix, field.Name)
		bindingPath := joinFieldPath(bindingPrefix, field.Name)
		if allLocales && field.Localized {
			if value.Kind() != store.ValueObject {
				return nil
			}
			locales := make([]string, 0, value.Len())
			for code := range value.Entries() {
				locales = append(locales, code)
			}
			sort.Strings(locales)
			var locations []fieldLocation
			for _, code := range locales {
				localizedPath := joinFieldPath(runtimePath, code)
				localizedIdentity := joinFieldPath(identityPath, code)
				localizedLocale := schema.LocaleCode(code)
				if path == canonicalPath {
					locations = append(locations, fieldLocation{
						parentPath: runtimePrefix, fields: fields, value: value.Get(code), siblings: fieldLocationSiblings(fields, values, allLocales, localizedLocale),
						runtimePath: localizedPath, identity: localizedIdentity, bindingIdentity: bindingPath, locale: localizedLocale,
					})
					continue
				}
				locations = append(locations, collectFieldDescendantLocations(field, value.Get(code), path, localizedPath, localizedIdentity, bindingPath, allLocales, localizedLocale, includeMissing...)...)
			}
			return locations
		}
		if path == canonicalPath {
			return []fieldLocation{{parentPath: runtimePrefix, fields: fields, value: value, siblings: fieldLocationSiblings(fields, values, allLocales, locale), runtimePath: runtimePath, identity: identityPath, bindingIdentity: bindingPath, locale: locale}}
		}
		return collectFieldDescendantLocations(field, value, path, runtimePath, identityPath, bindingPath, allLocales, locale, includeMissing...)
	}
	return nil
}

func collectFieldDescendantLocations(field schema.Field, value store.Value, path, runtimePath, identityPath, bindingPath string, allLocales bool, locale schema.LocaleCode, includeMissing ...bool) []fieldLocation {
	switch field.Type {
	case schema.FieldTypePlugin:
		var locations []fieldLocation
		err := embedded.Visit(field, value, runtimePath, nil, func(occurrence embedded.ReadOccurrence) error {
			locations = append(locations, collectFieldLocations(occurrence.Fields, occurrence.Payload, path, occurrence.RuntimePath, joinFieldPath(identityPath, occurrence.Identity), joinFieldPath(bindingPath, occurrence.Identity), allLocales, locale, includeMissing...)...)
			return nil
		})
		if err != nil {
			return nil
		} // Operation preflight rejects malformed envelopes.
		return locations
	case schema.FieldTypeGroup:
		if value.Kind() != store.ValueObject || field.Nested == nil {
			return nil
		}
		return collectFieldLocations(field.Nested.ResolvedFields(), value, path, runtimePath, identityPath, bindingPath, allLocales, locale, includeMissing...)
	case schema.FieldTypeArray:
		if value.Kind() != store.ValueList || field.Nested == nil {
			return nil
		}
		var locations []fieldLocation
		keyOccurrences := make(map[string]int, value.Len())
		index := -1
		for item := range value.Elements() {
			index++
			if item.Kind() != store.ValueObject {
				continue
			}
			rowIdentity := fmt.Sprintf("#%d", index)
			if key, valid := item.Get("_key").StringValue(); valid && key != "" {
				rowIdentity = keyedRowIdentity(key, keyOccurrences[key])
				keyOccurrences[key]++
			}
			locations = append(locations, collectFieldLocations(
				field.Nested.ResolvedFields(), item, path, fmt.Sprintf("%s.%d", runtimePath, index), joinFieldPath(identityPath, rowIdentity), joinFieldPath(bindingPath, rowIdentity), allLocales, locale, includeMissing...,
			)...)
		}
		return locations
	case schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList || field.Blocks == nil {
			return nil
		}
		var locations []fieldLocation
		keyOccurrences := make(map[string]int, value.Len())
		index := -1
		for item := range value.Elements() {
			index++
			if item.Kind() != store.ValueObject {
				continue
			}
			blockType, valid := item.Get("blockType").StringValue()
			if !valid {
				continue
			}
			for _, block := range field.Blocks.ResolvedTypes() {
				if block.Slug != blockType {
					continue
				}
				rowIdentity := fmt.Sprintf("#%d", index)
				if key, valid := item.Get("_key").StringValue(); valid && key != "" {
					rowIdentity = keyedRowIdentity(key, keyOccurrences[key])
					keyOccurrences[key]++
				}
				locations = append(locations, collectFieldLocations(
					block.ResolvedFields(), item, path, fmt.Sprintf("%s.%d", runtimePath, index), joinFieldPath(identityPath, rowIdentity), joinFieldPath(bindingPath, rowIdentity), allLocales, locale, includeMissing...,
				)...)
				break
			}
		}
		return locations
	default:
		return nil
	}
}

func keyedRowIdentity(key string, occurrence int) string {
	// Length-prefix the key so crafted keys cannot collide with the occurrence
	// suffix. Validation rejects duplicate keys on authored writes; retaining an
	// occurrence also keeps field authorization fail-closed for malformed rows
	// injected directly by an adapter.
	return fmt.Sprintf("@%d:%s#%d", len(key), key, occurrence)
}

func deleteValueAtRuntimePath(values store.Values, path string) {
	deleteValueAtRuntimeSegments(values, strings.Split(path, "."))
}

func deleteValueAtRuntimeSegments(values store.Values, segments []string) {
	if len(segments) == 0 {
		return
	}
	if len(segments) == 1 {
		delete(values, segments[0])
		return
	}
	value, exists := values[segments[0]]
	if !exists {
		return
	}
	if object, valid := value.CopyObject(); valid {
		deleteValueAtRuntimeSegments(object, segments[1:])
		values[segments[0]] = store.Object(object)
		return
	}
	items, valid := value.CopyList()
	if !valid || len(segments) < 3 {
		return
	}
	index, err := strconv.Atoi(segments[1])
	if err != nil || index < 0 || index >= len(items) {
		return
	}
	object, valid := items[index].CopyObject()
	if !valid {
		return
	}
	deleteValueAtRuntimeSegments(object, segments[2:])
	items[index] = store.Object(object)
	values[segments[0]] = store.List(items...)
}

func joinFieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

type transactionKey struct{}

type transactionIntent uint8

const (
	// Keep the zero value write-capable so transaction states constructed by
	// specialized mutation paths remain conservative.
	transactionWrite transactionIntent = iota
	transactionReadOnly
)

var errMutationInReadOnlyTransaction = errors.New("mutation cannot run inside a read-only operation transaction")

type transactionState struct {
	transaction                  store.Transaction
	intent                       transactionIntent
	afterCommit                  []deferredHook
	permanentDeletes             []PermanentDelete
	referenceDeleteDeletedOwners []store.DocumentReference
	transactionResources         []*TransactionResource
	uploadObjectLockReleases     []func()
	uploadObjectLockedKeys       map[string]struct{}
	rollbackOnly                 error
	finished                     bool
}

// PermanentDelete identifies one document whose durable deletion must be
// fenced with lifecycle-sensitive in-process state such as preview grants.
type PermanentDelete struct {
	Collection schema.Collection
	DocumentID string
	Original   store.Document
}
type deferredHook struct {
	hook    Hook
	context Context
}

func (engine *Engine) commitTransaction(ctx context.Context, state *transactionState) (err error) {
	if state.finished {
		return fmt.Errorf("transaction is already finished")
	}
	if state.rollbackOnly != nil {
		cause := state.rollbackOnly
		rollbackError := engine.rollbackTransactionState(ctx, state)
		return &transactionAbortedError{cause: cause, rollbackError: rollbackError}
	}
	state.finished = true
	defer state.releaseUploadObjectLocks()
	var releaseFence func(bool)
	if len(state.permanentDeletes) != 0 && engine.beginPermanentDeleteFence != nil {
		releaseFence = engine.beginPermanentDeleteFence(ctx, append([]PermanentDelete(nil), state.permanentDeletes...))
	}
	commitAttempted := false
	if releaseFence != nil {
		defer func() { releaseFence(commitAttempted) }()
	}
	commitAttempted = true
	if err := state.transaction.Commit(ctx); err != nil {
		state.finalizeTransactionResourcesUnknown()
		return err
	}
	state.finalizeTransactionResourcesCommitted()
	return nil
}

func (engine *Engine) rollbackTransactionState(ctx context.Context, state *transactionState) error {
	if state.finished {
		return nil
	}
	state.finished = true
	defer state.releaseUploadObjectLocks()
	if err := rollbackTransaction(ctx, state.transaction); err != nil {
		state.finalizeTransactionResourcesUnknown()
		return err
	}
	return state.finalizeTransactionResourcesRollback(ctx)
}

func (state *transactionState) addTransactionResource(resource *TransactionResource) {
	if resource == nil {
		return
	}
	if !resource.claimed.CompareAndSwap(false, true) {
		return
	}
	state.transactionResources = append(state.transactionResources, resource)
}

func (state *transactionState) markRollbackOnly(err error) {
	if err != nil && state.rollbackOnly == nil {
		state.rollbackOnly = err
	}
}

func (state *transactionState) finalizeTransactionResourcesCommitted() {
	for index := len(state.transactionResources) - 1; index >= 0; index-- {
		if commit := state.transactionResources[index].Commit; commit != nil {
			commit()
		}
	}
	state.transactionResources = nil
}

func (state *transactionState) finalizeTransactionResourcesUnknown() {
	for index := len(state.transactionResources) - 1; index >= 0; index-- {
		if unknown := state.transactionResources[index].Unknown; unknown != nil {
			unknown()
		}
	}
	state.transactionResources = nil
}

func (state *transactionState) finalizeTransactionResourcesRollback(ctx context.Context) error {
	var result error
	for index := len(state.transactionResources) - 1; index >= 0; index-- {
		if rollback := state.transactionResources[index].Rollback; rollback != nil {
			result = errors.Join(result, rollback(ctx))
		}
	}
	state.transactionResources = nil
	if result != nil {
		return &transactionResourceRollbackError{cause: result}
	}
	return nil
}

func (state *transactionState) releaseUploadObjectLocks() {
	for index := len(state.uploadObjectLockReleases) - 1; index >= 0; index-- {
		state.uploadObjectLockReleases[index]()
	}
	state.uploadObjectLockReleases = nil
	state.uploadObjectLockedKeys = nil
}

func (state *transactionState) lockUploadObjects(ctx context.Context, locker store.UploadObjectLocker, keys []string) error {
	unique := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			return fmt.Errorf("upload object lock key is empty")
		}
		if _, held := state.uploadObjectLockedKeys[key]; !held {
			unique[key] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(unique))
	for key := range unique {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	release, err := locker.LockUploadObjects(ctx, ordered)
	if err != nil {
		return err
	}
	if release == nil {
		return fmt.Errorf("upload object locker returned a nil release function")
	}
	if state.uploadObjectLockedKeys == nil {
		state.uploadObjectLockedKeys = make(map[string]struct{}, len(ordered))
	}
	for _, key := range ordered {
		state.uploadObjectLockedKeys[key] = struct{}{}
	}
	state.uploadObjectLockReleases = append(state.uploadObjectLockReleases, release)
	return nil
}

func (engine *Engine) dispatchDeferredHooks(ctx context.Context, hooks []deferredHook) error {
	var result error
	for _, deferred := range hooks {
		deferred.context.Context = context.WithValue(ctx, transactionKey{}, (*transactionState)(nil))
		dispatchError := engine.dispatchDeferredHook(deferred)
		if dispatchError != nil {
			result = errors.Join(result, dispatchError)
		}
	}
	return result
}

func (engine *Engine) dispatchDeferredHook(deferred deferredHook) (err error) {
	defer func() {
		if recover() != nil {
			// A hook or dispatcher is untrusted application code. Keep its panic
			// value out of public errors and let the remaining committed effects run.
			err = errors.New("after commit effect panicked")
		}
	}()
	if engine.dispatchAfterCommit != nil {
		return engine.dispatchAfterCommit(deferred.context, deferred.hook)
	}
	return deferred.hook(deferred.context)
}

func (engine *Engine) dispatchCommittedEffects(ctx context.Context, state *transactionState) error {
	result := engine.dispatchDeferredHooks(ctx, state.afterCommit)
	if len(state.permanentDeletes) == 0 || engine.cleanupPermanentDeletes == nil {
		return result
	}
	deletes := make([]PermanentDelete, len(state.permanentDeletes))
	for index, deleted := range state.permanentDeletes {
		deletes[index] = deleted
		deletes[index].Original = store.CloneDocument(deleted.Original)
	}
	return errors.Join(result, engine.runPermanentDeleteCleanup(context.WithoutCancel(ctx), deletes))
}

func (engine *Engine) runPermanentDeleteCleanup(ctx context.Context, deletes []PermanentDelete) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("committed permanent-delete cleanup panicked")
		}
	}()
	return engine.cleanupPermanentDeletes(ctx, deletes)
}

func (engine *Engine) transaction(ctx context.Context, intent transactionIntent) (*transactionState, bool, error) {
	if existing, _ := ctx.Value(transactionKey{}).(*transactionState); existing != nil {
		if existing.intent == transactionReadOnly && intent == transactionWrite {
			return nil, false, errMutationInReadOnlyTransaction
		}
		return existing, false, nil
	}
	var transaction store.Transaction
	var err error
	if intent == transactionReadOnly {
		if snapshotStore, ok := engine.store.(store.SnapshotStore); ok {
			transaction, err = snapshotStore.BeginSnapshot(ctx)
		} else {
			transaction, err = engine.store.Begin(ctx)
		}
	} else {
		transaction, err = engine.store.Begin(ctx)
	}
	if err != nil {
		return nil, false, err
	}
	return &transactionState{transaction: transaction, intent: intent}, true, nil
}

func requireWritableTransactionContext(ctx context.Context) error {
	if existing, _ := ctx.Value(transactionKey{}).(*transactionState); existing != nil && existing.intent == transactionReadOnly {
		return errMutationInReadOnlyTransaction
	}
	return nil
}

func authorize(collection Collection, ctx Context) (Decision, error) {
	kind := ctx.Operation
	if kind == operation.Duplicate {
		kind = operation.Create
	} else if kind == operation.RestoreDeleted || kind == operation.DeletePermanent {
		kind = operation.Delete
	}
	rule := collection.Access[kind]
	if rule == nil {
		return Decision{Kind: Allow}, nil
	}
	if ctx.AllLocales && ctx.Locale == "" && len(ctx.Locales) != 0 {
		var combined Decision
		for index, locale := range ctx.Locales {
			current := ctx
			current.Locale = locale
			decision, err := evaluateAccessRule(rule, current)
			if err != nil {
				return Decision{}, err
			}
			if index == 0 {
				combined = decision
				continue
			}
			if decision.Kind != combined.Kind || decision.Kind == Where && !reflect.DeepEqual(decision.Access, combined.Access) {
				return Decision{Kind: Deny}, nil
			}
		}
		return combined, nil
	}
	return evaluateAccessRule(rule, ctx)
}

func evaluateAccessRule(rule Access, ctx Context) (Decision, error) {
	decision, err := rule(ctx)
	if err != nil {
		return Decision{}, err
	}
	if decision.Kind == "" {
		return Decision{Kind: Deny}, nil
	}
	return decision, nil
}

func authorizePreparedLocalization(collection Collection, base Context, canonical store.Values, locales []schema.LocaleCode) (Decision, error) {
	if len(locales) == 0 {
		base.Data = store.CloneValues(canonical)
		return authorize(collection, base)
	}
	var combined Decision
	for index, locale := range locales {
		selection := localization.Selection{
			Locale: locale, Chain: []schema.LocaleCode{locale},
			Configured: append([]schema.LocaleCode(nil), locales...),
		}
		current := base
		current.Data = projectValues(base.projections, collection.Schema.Fields, canonical, selection)
		current.Locale = locale
		current.AllLocales = true
		decision, err := authorize(collection, current)
		if err != nil {
			return Decision{}, err
		}
		if index == 0 {
			combined = decision
			continue
		}
		if decision.Kind != combined.Kind || decision.Kind == Where && !reflect.DeepEqual(decision.Access, combined.Access) {
			return Decision{Kind: Deny}, nil
		}
	}
	return combined, nil
}

func denyAllAccessPredicate() *query.Node {
	id, _ := query.NewPath("id")
	expression, _ := query.And(
		query.Equal(id, query.String("__ridu_access_denied_a__")),
		query.Equal(id, query.String("__ridu_access_denied_b__")),
	)
	node := expression.Node()
	return &node
}

func runHooks(hooks []Hook, ctx Context) error {
	for _, hook := range hooks {
		if err := ctx.Context.Err(); err != nil {
			return err
		}
		if err := hook(ctx); err != nil {
			return err
		}
	}
	return nil
}

func documentResult(document store.Document, err error) (*store.Document, error) {
	if err == nil {
		cloned := store.CloneDocument(document)
		return &cloned, nil
	}
	return nil, translateStoreError(err)
}

func pageResult(page store.Page, err error) (*store.Page, error) {
	if err != nil {
		return nil, translateStoreError(err)
	}
	page.Documents = cloneDocuments(page.Documents)
	return &page, nil
}

type transactionAbortedError struct {
	cause         error
	rollbackError error
}

type transactionResourceRollbackError struct{ cause error }

func (failure *transactionResourceRollbackError) Error() string {
	return "external transaction resource cleanup failed"
}

func (failure *transactionResourceRollbackError) Unwrap() error { return failure.cause }

func (failure *transactionAbortedError) Error() string {
	return "transaction was rolled back because a nested operation failed"
}

func (failure *transactionAbortedError) Unwrap() []error {
	if failure.rollbackError == nil {
		return []error{failure.cause}
	}
	if failure.cause == nil {
		return []error{failure.rollbackError}
	}
	return []error{failure.cause, failure.rollbackError}
}

func transactionAbortedOperationError(cause error) error {
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	var operationError *Error
	if errors.As(cause, &operationError) {
		cloned := *operationError
		return &cloned
	}
	return &Error{
		Code: "transaction_aborted", Status: 409,
		Message: "transaction was rolled back because a nested operation failed",
		Cause:   cause,
	}
}

func commitAttemptedError(err error) error {
	var aborted *transactionAbortedError
	if errors.As(err, &aborted) {
		if aborted.rollbackError != nil {
			return rollbackOperationError("rollback after a nested operation failure could not be completed", aborted.cause, aborted.rollbackError)
		}
		return transactionAbortedOperationError(aborted.cause)
	}
	translated := translateStoreError(err)
	var operationError *Error
	if errors.As(translated, &operationError) {
		cloned := *operationError
		cloned.CommitAttempted = true
		return &cloned
	}
	return translated
}

func rollbackOperationError(message string, operationError, rollbackError error) error {
	code := "store_failed"
	var resourceError *transactionResourceRollbackError
	if errors.As(rollbackError, &resourceError) {
		code = "storage_failed"
	}
	return &Error{
		Code: code, Status: 500, Message: message,
		Cause: errors.Join(operationError, rollbackError),
	}
}

func translateStoreError(err error) error {
	var unsupportedListQuery *primitivefield.UnsupportedQueryError
	if errors.As(err, &unsupportedListQuery) {
		return &Error{Code: "bad_query", Status: 400, Message: unsupportedListQuery.Error(), Issues: []schema.Issue{{Code: "unsupported_path", Path: unsupportedListQuery.Path.String(), Message: unsupportedListQuery.Error()}}, Cause: err}
	}
	var recovery *store.SchemaRecoveryError
	if errors.As(err, &recovery) {
		return &Error{Code: "block_recovery_required", Status: 409, Message: "This document requires block schema recovery. Restore the missing block schema or migrate the stored document before using it.", Issues: append([]schema.Issue(nil), recovery.Issues...), Cause: err}
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &Error{Code: "not_found", Status: 404, Message: "document was not found", Cause: err}
	case errors.Is(err, store.ErrAuthInitialized):
		return &Error{Code: "access_denied", Status: 403, Message: "auth collection is already initialized", Cause: err}
	case errors.Is(err, store.ErrDeleteRestricted):
		return &Error{Code: "delete_restricted", Status: 409, Message: "document deletion is restricted by current references", Cause: err}
	case errors.Is(err, store.ErrConflict):
		return &Error{Code: "conflict", Status: 409, Message: "document conflicts with an existing value", Cause: err}
	case errors.Is(err, store.ErrPopulationLimit):
		return &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population materializes more than %d related documents", store.MaxPopulationMaterializedDocuments), Cause: err}
	default:
		return &Error{Code: "store_failed", Status: 500, Message: "document store operation failed", Cause: err}
	}
}

func transactionAdmissionError(message string, err error) error {
	if errors.Is(err, errMutationInReadOnlyTransaction) {
		return &Error{
			Code: "transaction_read_only", Status: 409,
			Message: "a mutation cannot run inside a read-only operation transaction",
			Cause:   err,
		}
	}
	return &Error{Code: "store_failed", Status: 500, Message: message, Cause: err}
}

func hookError(phase string, err error) error {
	var embeddedFailure *embedded.Error
	if errors.As(err, &embeddedFailure) {
		return embeddedOperationError(err, false)
	}
	var validationError *schema.ValidationError
	if errors.As(err, &validationError) {
		return &Error{
			Code:    "validation",
			Status:  422,
			Message: "document validation failed during " + phase,
			Issues:  append([]schema.Issue(nil), validationError.Issues...),
			Cause:   err,
		}
	}
	return &Error{Code: "hook_failed", Status: 500, Message: phase + " hook failed", Cause: err}
}

func committedHookError(err error) error {
	return &Error{Code: "hook_failed", Status: 500, Message: "after commit hook failed", Cause: err, Committed: true}
}

func cloneDocuments(documents []store.Document) []store.Document {
	cloned := make([]store.Document, len(documents))
	for index, document := range documents {
		cloned[index] = store.CloneDocument(document)
	}
	return cloned
}

func cloneDocumentPointer(document *store.Document) *store.Document {
	if document == nil {
		return nil
	}
	cloned := store.CloneDocument(*document)
	return &cloned
}

func cloneLocalization(settings *schema.LocalizationSettings) *schema.LocalizationSettings {
	if settings == nil {
		return nil
	}
	cloned := *settings
	cloned.Locales = make([]schema.Locale, len(settings.Locales))
	for index, locale := range settings.Locales {
		cloned.Locales[index] = locale
		cloned.Locales[index].FallbackLocales = append([]schema.LocaleCode(nil), locale.FallbackLocales...)
	}
	return &cloned
}
