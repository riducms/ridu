package core

import (
	"context"
	"fmt"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// LocalAPI enters the same operation engine used by HTTP and jobs.
type LocalAPI struct {
	engine *operationengine.Engine
}

// ListOptions controls filtering, pagination, projection, and authorization for a list read.
type ListOptions struct {
	// Where filters documents before pagination and remains atomic with access predicates.
	Where query.Expression
	// Page is the one-based result page. Zero selects the first page.
	Page int
	// Limit is the maximum documents returned per page.
	Limit int
	// Sort orders results by authored field paths.
	Sort []query.Sort
	// Select restricts returned document fields.
	Select []query.Path
	// Populate expands configured relationship paths.
	Populate []query.Population
	// OutputFields limits computed and inverse-join resolution without
	// projecting away stored fields. Nil resolves all output fields; a non-nil
	// empty slice resolves none.
	OutputFields []query.Path
	// Draft explicitly includes draft documents when true or restricts reads to
	// published documents when false. Nil preserves the Local API default.
	Draft *bool
	// Actor is the authenticated document used by access rules.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// TrashOnly returns deleted documents and is valid only for trash-enabled collections.
	TrashOnly bool
	// Locale selects one configured content locale. Empty uses the application default.
	Locale schema.LocaleCode
	// FallbackLocales replaces the locale's configured fallback chain when non-nil.
	FallbackLocales []schema.LocaleCode
	// DisableFallback requires an exact value in Locale.
	DisableFallback bool
	// AllLocales returns locale-keyed values for localized fields.
	AllLocales bool
}

// DistinctOptions controls one access-checked unique-value read. The initial
// contract intentionally supports one direct singular stored field and normal
// ascending field order; it is not a general aggregation API.
type DistinctOptions struct {
	// Field names the authored scalar, singular relationship, singular upload,
	// or document ID whose unique values should be returned.
	Field query.Path
	// Where remains atomic with the collection read-access predicate.
	Where query.Expression
	// Page is one-based. Zero selects the first page.
	Page int
	// Limit follows ordinary Ridu list bounds and defaults.
	Limit int
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Draft follows List semantics for versioned collections.
	Draft *bool
	// TrashOnly returns values from deleted documents and requires trash support.
	TrashOnly       bool
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
}

// ListWindowOptions controls a count-free bounded unique-index range read for
// background work. Index must name one direct, unique, indexed text field.
// LowerBound is inclusive and UpperBound is exclusive. Unlike ListOptions it
// has no arbitrary filter, access predicate, page offset, or relationship
// population. Production adapters use the unique index to fetch only Limit
// plus one overflow sentinel and do not compute a total.
type ListWindowOptions struct {
	Index           query.Path
	LowerBound      string
	UpperBound      string
	Limit           int
	Select          []query.Path
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

// LocaleOptions selects localized content for single-document operations.
type LocaleOptions struct {
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

// FindOptions controls projection, relationship population, localization, and
// authorization for one document read.
type FindOptions struct {
	// Select is nil for all authored fields. A non-nil empty slice returns only
	// document metadata.
	Select   []query.Path
	Populate []query.Population
	// OutputFields limits computed and inverse-join resolution independently
	// from Select. Nil resolves all; a non-nil empty slice resolves none.
	OutputFields []query.Path
	// Draft explicitly includes draft documents when true or restricts reads to
	// published documents when false. Nil preserves the Local API default.
	Draft           *bool
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	TrashOnly       bool
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

// MutationOptions controls authorization, optimistic concurrency,
// localization, and selection-driven population for one document write.
// Populate affects only the returned document; it never changes validation or
// storage behavior.
type MutationOptions struct {
	// ID supplies a caller-owned document ID for create operations when
	// Config.AllowIDOnCreate is enabled. Other mutations ignore it.
	ID               string
	Actor            *store.Document
	ActorCollection  schema.CollectionSlug
	ExpectedRevision int
	Populate         []query.Population
	// OutputFields limits computed and inverse-join resolution in the returned
	// document. Nil resolves all; a non-nil empty slice resolves none.
	OutputFields []query.Path
	// Draft selects draft (true) or published (false) status for versioned
	// creates. Updates preserve status so publish and unpublish hooks cannot be bypassed.
	Draft           *bool
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

// CapabilityOptions describes one side-effect-free access evaluation.
type CapabilityOptions struct {
	Data            store.Values
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	TrashOnly       bool
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
	AllLocales      bool
}

// OperationCapabilities is a non-secret permission summary. It deliberately
// excludes executable rules and filtered-access predicates.
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

type OperationError = operationengine.Error

// ImportOptions preserves source identity and timestamps during a migration.
type ImportOptions struct {
	// ID preserves the source document identity.
	ID string
	// Status preserves the source draft or published status.
	Status store.Status
	// CreatedAt preserves the source creation timestamp.
	CreatedAt time.Time
	// UpdatedAt preserves the source modification timestamp.
	UpdatedAt time.Time
}

// JoinMutationResult reports the refreshed source document and applied deltas
// from one atomic inverse relationship mutation.
type JoinMutationResult struct {
	Document store.Document
	Added    int
	Removed  int
}

func (local *LocalAPI) Create(ctx context.Context, collection string, values store.Values, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.CreateWithOptions(ctx, collection, values, mutationOptions(actor, 0, localeOptions))
}

// CreateWithOptions creates a document and applies the requested bounded
// population plan to the response inside the write transaction.
func (local *LocalAPI) CreateWithOptions(ctx context.Context, collection string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Create, Collection: collection, Data: values}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) createStoragePrepared(ctx context.Context, collection string, values store.Values, actor *store.Document, resource *operationengine.TransactionResource, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.createStoragePreparedWithOptions(ctx, collection, values, mutationOptions(actor, 0, localeOptions), resource)
}

func (local *LocalAPI) createStoragePreparedWithOptions(ctx context.Context, collection string, values store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Create, Collection: collection, Data: values, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) createWithTransactionMutation(ctx context.Context, collection string, values store.Values, actor *store.Document, mutation operationengine.TransactionMutation, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.createWithTransactionMutationOptions(ctx, collection, values, mutationOptions(actor, 0, localeOptions), mutation)
}

func (local *LocalAPI) createWithTransactionMutationOptions(ctx context.Context, collection string, values store.Values, options MutationOptions, mutation operationengine.TransactionMutation) (store.Document, error) {
	return local.createWithTransactionMutationAndFieldAccess(ctx, collection, values, options, mutation, false)
}

func (local *LocalAPI) createWithTransactionMutationAndFieldAccess(ctx context.Context, collection string, values store.Values, options MutationOptions, mutation operationengine.TransactionMutation, skipFieldAccess bool) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Create, Collection: collection, Data: values,
		TransactionMutation: mutation, SkipFieldAccess: skipFieldAccess,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// Duplicate creates a new document from an access-checked source. Overrides
// are applied before create validation and duplicate-aware hooks run.
func (local *LocalAPI) Duplicate(ctx context.Context, collection, id string, overrides store.Values, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.DuplicateWithOptions(ctx, collection, id, overrides, mutationOptions(actor, 0, localeOptions))
}

// DuplicateWithOptions duplicates a document and populates the returned copy
// inside the write transaction.
func (local *LocalAPI) DuplicateWithOptions(ctx context.Context, collection, id string, overrides store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Duplicate, Collection: collection, ID: id, Data: overrides}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) duplicateStoragePrepared(ctx context.Context, collection, id string, overrides store.Values, actor *store.Document, resource *operationengine.TransactionResource, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.duplicateStoragePreparedWithOptions(ctx, collection, id, overrides, mutationOptions(actor, 0, localeOptions), resource)
}

func (local *LocalAPI) duplicateStoragePreparedWithOptions(ctx context.Context, collection, id string, overrides store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Duplicate, Collection: collection, ID: id, Data: overrides, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// Import creates a document with a migration-owned stable ID while still
// running ordinary access, validation, hooks, transaction, and version logic.
func (local *LocalAPI) Import(ctx context.Context, collection string, values store.Values, options ImportOptions, actor *store.Document) (store.Document, error) {
	if err := store.ValidateDocumentID(options.ID); err != nil {
		return store.Document{}, &operationengine.Error{
			Code: "validation", Status: 422, Message: "document validation failed",
			Issues: []schema.Issue{{Code: "invalid_document_id", Path: "id", Message: "migration document ID is invalid: " + err.Error()}},
			Cause:  err,
		}
	}
	result, err := local.engine.Execute(ctx, operationengine.Request{Operation: operationengine.Create, Collection: collection, ImportID: options.ID, ImportCreatedAt: options.CreatedAt, ImportUpdatedAt: options.UpdatedAt, Data: values, Status: &options.Status, Actor: actor})
	return requiredDocument(result, err)
}

func (local *LocalAPI) Find(ctx context.Context, collection, id string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	options := FindOptions{Actor: actor}
	if len(localeOptions) > 0 {
		selected := localeOptions[len(localeOptions)-1]
		options.Locale = selected.Locale
		options.FallbackLocales = append([]schema.LocaleCode(nil), selected.FallbackLocales...)
		options.DisableFallback = selected.DisableFallback
		options.AllLocales = selected.AllLocales
	}
	return local.FindWithOptions(ctx, collection, id, options)
}

// FindWithOptions reads one document through the operation engine with a
// transport-owned projection and population plan.
func (local *LocalAPI) FindWithOptions(ctx context.Context, collection, id string, options FindOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Read, Collection: collection, ID: id, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Select: options.Select, Populate: options.Populate, OutputFields: cloneOptionalPaths(options.OutputFields), Draft: cloneOptionalBool(options.Draft), TrashOnly: options.TrashOnly,
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	}
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) List(ctx context.Context, collection string, options ListOptions) (store.Page, error) {
	result, err := local.engine.Execute(ctx, operationengine.Request{
		Operation: operationengine.Read, Collection: collection, Filter: options.Where,
		Page: options.Page, Limit: options.Limit, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Sort: options.Sort, Select: options.Select, Populate: options.Populate, OutputFields: cloneOptionalPaths(options.OutputFields), Draft: cloneOptionalBool(options.Draft), TrashOnly: options.TrashOnly,
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	})
	if err != nil {
		return store.Page{}, err
	}
	if result.Page == nil {
		return store.Page{}, fmt.Errorf("operation engine returned no page")
	}
	return *result.Page, nil
}

// Distinct returns paginated unique values for one direct field. Collection
// access and Where are composed in the adapter query, and field read access is
// checked before any value is selected.
func (local *LocalAPI) Distinct(ctx context.Context, collection string, options DistinctOptions) (store.DistinctPage, error) {
	return local.engine.Distinct(ctx, operationengine.DistinctRequest{
		Collection: collection, Field: options.Field, Filter: options.Where,
		Page: options.Page, Limit: options.Limit, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Draft: cloneOptionalBool(options.Draft), TrashOnly: options.TrashOnly,
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback,
	})
}

// ListWindow reads at most Limit documents from one unique-index range without
// a total count or offset. Collection read access must resolve to Allow so an
// adapter can preserve the physical index bound. The configured store must
// implement the optional store.WindowTransaction capability.
func (local *LocalAPI) ListWindow(ctx context.Context, collection string, options ListWindowOptions) (store.Window, error) {
	result, err := local.engine.Execute(ctx, operationengine.Request{
		Operation: operationengine.Read, Collection: collection,
		Limit: options.Limit, IndexWindow: &store.IndexWindow{Path: options.Index, LowerBound: options.LowerBound, UpperBound: options.UpperBound}, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Select: options.Select,
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	})
	if err != nil {
		return store.Window{}, err
	}
	if result.Page == nil {
		return store.Window{}, fmt.Errorf("operation engine returned no window")
	}
	return store.Window{Documents: result.Page.Documents, HasMore: result.WindowHasMore}, nil
}

// Capabilities evaluates collection, document, and field access without
// running hooks, validation, or a mutation.
func (local *LocalAPI) Capabilities(ctx context.Context, collection, id string, options CapabilityOptions) (AccessCapabilities, error) {
	result, err := local.engine.Capabilities(ctx, operationengine.CapabilitiesRequest{
		Collection: collection, ID: id, Data: store.CloneValues(options.Data), Actor: options.Actor, ActorCollection: options.ActorCollection,
		TrashOnly: options.TrashOnly, Locale: string(options.Locale),
		FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	})
	if err != nil {
		return AccessCapabilities{}, err
	}
	fields := make(map[string]FieldCapabilities, len(result.Fields))
	for path, capabilities := range result.Fields {
		fields[path] = FieldCapabilities{Read: capabilities.Read, Create: capabilities.Create, Update: capabilities.Update}
	}
	operations := result.Operations
	return AccessCapabilities{Operations: OperationCapabilities{
		Admin: operations.Admin, Create: operations.Create, Read: operations.Read, ReadVersions: operations.ReadVersions,
		Update: operations.Update, Delete: operations.Delete, Duplicate: operations.Duplicate,
		Publish: operations.Publish, Unpublish: operations.Unpublish, RestoreDeleted: operations.RestoreDeleted,
		DeletePermanent: operations.DeletePermanent, SelectAll: operations.SelectAll, Unlock: operations.Unlock,
	}, Fields: fields}, nil
}

func (local *LocalAPI) Update(ctx context.Context, collection, id string, values store.Values, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.UpdateWithOptions(ctx, collection, id, values, mutationOptions(actor, 0, localeOptions))
}

// UpdateWithOptions updates a document and applies the requested bounded
// population plan to the response inside the write transaction.
func (local *LocalAPI) UpdateWithOptions(ctx context.Context, collection, id string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Update, Collection: collection, ID: id, Data: values}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func applyLocaleOptions(request *operationengine.Request, options []LocaleOptions) {
	if len(options) == 0 {
		return
	}
	selected := options[len(options)-1]
	request.Locale = string(selected.Locale)
	request.FallbackLocales = append([]schema.LocaleCode(nil), selected.FallbackLocales...)
	request.DisableFallback = selected.DisableFallback
	request.AllLocales = selected.AllLocales
}

func mutationOptions(actor *store.Document, expectedRevision int, localeOptions []LocaleOptions) MutationOptions {
	options := MutationOptions{Actor: actor, ExpectedRevision: expectedRevision}
	if len(localeOptions) == 0 {
		return options
	}
	selected := localeOptions[len(localeOptions)-1]
	options.Locale = selected.Locale
	options.FallbackLocales = append([]schema.LocaleCode(nil), selected.FallbackLocales...)
	options.DisableFallback = selected.DisableFallback
	options.AllLocales = selected.AllLocales
	return options
}

func applyMutationOptions(request *operationengine.Request, options MutationOptions) {
	if request.Operation == operationengine.Create && options.ID != "" {
		request.ID = options.ID
	}
	request.Actor = options.Actor
	request.ActorCollection = options.ActorCollection
	request.ExpectedRevision = options.ExpectedRevision
	request.Populate = append([]query.Population(nil), options.Populate...)
	request.OutputFields = cloneOptionalPaths(options.OutputFields)
	request.Draft = cloneOptionalBool(options.Draft)
	request.Locale = string(options.Locale)
	request.FallbackLocales = append([]schema.LocaleCode(nil), options.FallbackLocales...)
	request.DisableFallback = options.DisableFallback
	request.AllLocales = options.AllLocales
}

func cloneOptionalPaths(paths []query.Path) []query.Path {
	if paths == nil {
		return nil
	}
	return append([]query.Path{}, paths...)
}

func cloneOptionalBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func operationLocaleOptions(options []LocaleOptions) operationengine.LocalizationOptions {
	if len(options) == 0 {
		return operationengine.LocalizationOptions{}
	}
	selected := options[len(options)-1]
	return operationengine.LocalizationOptions{
		Locale: string(selected.Locale), FallbackLocales: append([]schema.LocaleCode(nil), selected.FallbackLocales...),
		DisableFallback: selected.DisableFallback, AllLocales: selected.AllLocales,
	}
}

func versionFindOptions(actor *store.Document, localeOptions []LocaleOptions) FindOptions {
	options := FindOptions{Actor: actor}
	if len(localeOptions) == 0 {
		return options
	}
	selected := localeOptions[len(localeOptions)-1]
	options.Locale = selected.Locale
	options.FallbackLocales = append([]schema.LocaleCode(nil), selected.FallbackLocales...)
	options.DisableFallback = selected.DisableFallback
	options.AllLocales = selected.AllLocales
	return options
}

func versionLocalizationOptions(options FindOptions) operationengine.LocalizationOptions {
	return operationengine.LocalizationOptions{
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales, ActorCollection: options.ActorCollection,
	}
}

func (local *LocalAPI) UpdateRevision(ctx context.Context, collection, id string, values store.Values, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.UpdateWithOptions(ctx, collection, id, values, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) updateStoragePrepared(ctx context.Context, collection, id string, values store.Values, expectedRevision int, actor *store.Document, resource *operationengine.TransactionResource) (store.Document, error) {
	return local.updateStoragePreparedWithOptions(ctx, collection, id, values, MutationOptions{Actor: actor, ExpectedRevision: expectedRevision}, resource)
}

func (local *LocalAPI) updateStoragePreparedWithOptions(ctx context.Context, collection, id string, values store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Update, Collection: collection, ID: id, Data: values, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) publishStoragePreparedWithOptions(ctx context.Context, collection, id string, values store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Publish, Collection: collection, ID: id, Data: values, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// MutateJoin atomically applies explicit additions and removals to one
// configured inverse relationship. Deltas avoid treating a limited join view
// as the complete relation set.
func (local *LocalAPI) MutateJoin(ctx context.Context, collection, id, field string, additions, removals []string, actor *store.Document, localeOptions ...LocaleOptions) (JoinMutationResult, error) {
	return local.MutateJoinWithOptions(ctx, collection, id, field, additions, removals, mutationOptions(actor, 0, localeOptions))
}

// MutateJoinWithOptions preserves the exact authenticated collection identity
// while applying inverse relationship deltas.
func (local *LocalAPI) MutateJoinWithOptions(ctx context.Context, collection, id, field string, additions, removals []string, options MutationOptions) (JoinMutationResult, error) {
	request := operationengine.JoinMutationRequest{
		Collection: collection, ID: id, Field: field,
		Additions: append([]string(nil), additions...), Removals: append([]string(nil), removals...), Actor: options.Actor, ActorCollection: options.ActorCollection,
	}
	request.Locale = string(options.Locale)
	request.FallbackLocales = append([]schema.LocaleCode(nil), options.FallbackLocales...)
	request.DisableFallback = options.DisableFallback
	request.AllLocales = options.AllLocales
	result, err := local.engine.MutateJoin(ctx, request)
	if err != nil {
		return JoinMutationResult{}, err
	}
	return JoinMutationResult{Document: result.Document, Added: result.Added, Removed: result.Removed}, nil
}

func (local *LocalAPI) Publish(ctx context.Context, collection, id string, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.PublishWithOptions(ctx, collection, id, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) PublishWithOptions(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Publish, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// PublishChanges atomically applies values and publishes the resulting document
// through both update access and the publish access rule and lifecycle.
func (local *LocalAPI) PublishChanges(ctx context.Context, collection, id string, values store.Values, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.PublishChangesWithOptions(ctx, collection, id, values, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) PublishChangesWithOptions(ctx context.Context, collection, id string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Publish, Collection: collection, ID: id, Data: values}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) Unpublish(ctx context.Context, collection, id string, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.UnpublishWithOptions(ctx, collection, id, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) UnpublishWithOptions(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Unpublish, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// CopyLocale copies readable localized values between two locales while
// enforcing source read and destination update access independently. A copy
// into published content also requires and runs the publish lifecycle.
func (local *LocalAPI) CopyLocale(ctx context.Context, collection, id string, source, target schema.LocaleCode, expectedRevision int, actor *store.Document) (store.Document, error) {
	return local.CopyLocaleWithOptions(ctx, collection, id, source, target, MutationOptions{Actor: actor, ExpectedRevision: expectedRevision})
}

// CopyLocaleWithOptions preserves the exact authenticated collection identity
// for both the source read and destination mutation lifecycle.
func (local *LocalAPI) CopyLocaleWithOptions(ctx context.Context, collection, id string, source, target schema.LocaleCode, options MutationOptions) (store.Document, error) {
	return local.engine.CopyLocale(ctx, collection, id, source, target, options.ExpectedRevision, options.Actor, operationengine.LocalizationOptions{ActorCollection: options.ActorCollection})
}

func (local *LocalAPI) Versions(ctx context.Context, collection, id string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Version, error) {
	return local.VersionsWithOptions(ctx, collection, id, versionFindOptions(actor, localeOptions))
}

// VersionsWithOptions preserves the exact authenticated collection identity
// while reading retained revisions.
func (local *LocalAPI) VersionsWithOptions(ctx context.Context, collection, id string, options FindOptions) ([]store.Version, error) {
	return local.engine.Versions(ctx, collection, id, options.Actor, versionLocalizationOptions(options))
}

// Version returns one authorized retained revision.
func (local *LocalAPI) Version(ctx context.Context, collection, id string, revision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Version, error) {
	return local.VersionWithOptions(ctx, collection, id, revision, versionFindOptions(actor, localeOptions))
}

// VersionWithOptions preserves the exact authenticated collection identity
// while reading one retained revision.
func (local *LocalAPI) VersionWithOptions(ctx context.Context, collection, id string, revision int, options FindOptions) (store.Version, error) {
	return local.engine.Version(ctx, collection, id, revision, options.Actor, versionLocalizationOptions(options))
}

func (local *LocalAPI) Restore(ctx context.Context, collection, id string, revision, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.RestoreVersionWithOptions(ctx, collection, id, revision, false, mutationOptions(actor, expectedRevision, localeOptions))
}

// RestoreAsDraft restores a retained revision while explicitly keeping the
// current document unpublished.
func (local *LocalAPI) RestoreAsDraft(ctx context.Context, collection, id string, revision, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.RestoreVersionWithOptions(ctx, collection, id, revision, true, mutationOptions(actor, expectedRevision, localeOptions))
}

// RestoreVersionWithOptions restores a retained revision and populates the
// returned document in the update transaction.
func (local *LocalAPI) RestoreVersionWithOptions(ctx context.Context, collection, id string, revision int, draft bool, options MutationOptions) (store.Document, error) {
	result, err := local.engine.RestorePopulated(ctx, collection, id, revision, options.ExpectedRevision, draft, options.Actor, options.Populate, options.OutputFields, operationengine.LocalizationOptions{
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales, ActorCollection: options.ActorCollection,
	})
	return requiredDocument(result, err)
}

func (local *LocalAPI) Delete(ctx context.Context, collection, id string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.DeleteWithOptions(ctx, collection, id, mutationOptions(actor, 0, localeOptions))
}

func (local *LocalAPI) DeleteWithOptions(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.Delete, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// RestoreDeleted moves a trashed document back into ordinary collection reads.
func (local *LocalAPI) RestoreDeleted(ctx context.Context, collection, id string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.RestoreDeletedWithOptions(ctx, collection, id, mutationOptions(actor, 0, localeOptions))
}

func (local *LocalAPI) RestoreDeletedWithOptions(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.RestoreDeleted, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// DeletePermanent irreversibly removes a document that is already in trash.
func (local *LocalAPI) DeletePermanent(ctx context.Context, collection, id string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.DeletePermanentWithOptions(ctx, collection, id, mutationOptions(actor, 0, localeOptions))
}

func (local *LocalAPI) DeletePermanentWithOptions(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operationengine.DeletePermanent, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// BulkUpdate atomically applies the same partial values to every document.
func (local *LocalAPI) BulkUpdate(ctx context.Context, collection string, ids []string, values store.Values, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operationengine.Update, values, actor, localeOptions...)
}

// BulkPublish atomically publishes every selected versioned document.
func (local *LocalAPI) BulkPublish(ctx context.Context, collection string, ids []string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operationengine.Publish, nil, actor, localeOptions...)
}

// BulkUnpublish atomically returns every selected versioned document to draft.
func (local *LocalAPI) BulkUnpublish(ctx context.Context, collection string, ids []string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operationengine.Unpublish, nil, actor, localeOptions...)
}

// BulkDelete atomically deletes or trashes every selected document.
func (local *LocalAPI) BulkDelete(ctx context.Context, collection string, ids []string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operationengine.Delete, nil, actor, localeOptions...)
}

// BulkRestoreDeleted atomically restores selected documents from trash.
func (local *LocalAPI) BulkRestoreDeleted(ctx context.Context, collection string, ids []string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operationengine.RestoreDeleted, nil, actor, localeOptions...)
}

// BulkDeletePermanent atomically permanently deletes selected trashed documents.
func (local *LocalAPI) BulkDeletePermanent(ctx context.Context, collection string, ids []string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operationengine.DeletePermanent, nil, actor, localeOptions...)
}

// EmptyTrash permanently deletes every accessible trashed document in one
// bounded atomic batch.
func (local *LocalAPI) EmptyTrash(ctx context.Context, collection string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	listOptions := ListOptions{Page: 1, Limit: 100, TrashOnly: true, Actor: actor}
	if len(localeOptions) > 0 {
		selected := localeOptions[len(localeOptions)-1]
		listOptions.Locale = selected.Locale
		listOptions.FallbackLocales = append([]schema.LocaleCode(nil), selected.FallbackLocales...)
		listOptions.DisableFallback = selected.DisableFallback
		listOptions.AllLocales = selected.AllLocales
	}
	page, err := local.List(ctx, collection, listOptions)
	if err != nil {
		return nil, err
	}
	if page.Total == 0 {
		return []store.Document{}, nil
	}
	if page.Total > 100 {
		return nil, &operationengine.Error{Code: "bad_request", Status: 400, Message: "empty trash supports at most 100 documents per atomic operation"}
	}
	ids := make([]string, len(page.Documents))
	for index, document := range page.Documents {
		ids[index] = document.ID
	}
	return local.BulkDeletePermanent(ctx, collection, ids, actor, localeOptions...)
}

func (local *LocalAPI) bulk(ctx context.Context, collection string, ids []string, kind operationengine.Kind, values store.Values, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Document, error) {
	requests := make([]operationengine.Request, len(ids))
	for index, id := range ids {
		requests[index] = operationengine.Request{Operation: kind, Collection: collection, ID: id, Data: store.CloneValues(values), Actor: actor}
		applyLocaleOptions(&requests[index], localeOptions)
	}
	results, err := local.engine.ExecuteBatch(ctx, requests)
	if err != nil {
		return nil, err
	}
	documents := make([]store.Document, len(results))
	for index, result := range results {
		if result.Document == nil {
			return nil, fmt.Errorf("bulk operation engine returned no document at index %d", index)
		}
		documents[index] = *result.Document
	}
	return documents, nil
}

// Global reads one singleton through the same access, hook, validation, and
// transaction engine used by collections.
func (local *LocalAPI) Global(ctx context.Context, slug string, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	options := FindOptions{Actor: actor}
	if len(localeOptions) != 0 {
		selected := localeOptions[len(localeOptions)-1]
		options.Locale = selected.Locale
		options.FallbackLocales = append([]schema.LocaleCode(nil), selected.FallbackLocales...)
		options.DisableFallback = selected.DisableFallback
		options.AllLocales = selected.AllLocales
	}
	return local.GlobalWithOptions(ctx, slug, options)
}

// GlobalWithOptions reads a singleton with transport-owned projection and
// population through the ordinary operation engine.
func (local *LocalAPI) GlobalWithOptions(ctx context.Context, slug string, options FindOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Read, Collection: "global:" + slug, ID: slug, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Select: options.Select, Populate: options.Populate, OutputFields: cloneOptionalPaths(options.OutputFields), Draft: cloneOptionalBool(options.Draft),
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	}
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// UpdateGlobal creates the singleton on its first update and updates it thereafter.
func (local *LocalAPI) UpdateGlobal(ctx context.Context, slug string, values store.Values, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.UpdateGlobalWithOptions(ctx, slug, values, mutationOptions(actor, expectedRevision, localeOptions))
}

// UpdateGlobalWithOptions updates a singleton and populates its response in
// the same write transaction.
func (local *LocalAPI) UpdateGlobalWithOptions(ctx context.Context, slug string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Update, Collection: "global:" + slug, ID: slug,
		Data: values,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// PublishGlobal publishes a versioned global.
func (local *LocalAPI) PublishGlobal(ctx context.Context, slug string, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.PublishGlobalWithOptions(ctx, slug, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) PublishGlobalWithOptions(ctx context.Context, slug string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Publish, Collection: "global:" + slug, ID: slug,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// PublishGlobalChanges atomically applies values and publishes the resulting
// global through both update access and the publish access rule and lifecycle.
func (local *LocalAPI) PublishGlobalChanges(ctx context.Context, slug string, values store.Values, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.PublishGlobalChangesWithOptions(ctx, slug, values, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) PublishGlobalChangesWithOptions(ctx context.Context, slug string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Publish, Collection: "global:" + slug, ID: slug, Data: values,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// UnpublishGlobal moves a versioned global back to draft status.
func (local *LocalAPI) UnpublishGlobal(ctx context.Context, slug string, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.UnpublishGlobalWithOptions(ctx, slug, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) UnpublishGlobalWithOptions(ctx context.Context, slug string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operationengine.Unpublish, Collection: "global:" + slug, ID: slug,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// CopyGlobalLocale copies readable localized singleton values between locales.
func (local *LocalAPI) CopyGlobalLocale(ctx context.Context, slug string, source, target schema.LocaleCode, expectedRevision int, actor *store.Document) (store.Document, error) {
	return local.CopyGlobalLocaleWithOptions(ctx, slug, source, target, MutationOptions{Actor: actor, ExpectedRevision: expectedRevision})
}

func (local *LocalAPI) CopyGlobalLocaleWithOptions(ctx context.Context, slug string, source, target schema.LocaleCode, options MutationOptions) (store.Document, error) {
	return local.engine.CopyLocale(ctx, "global:"+slug, slug, source, target, options.ExpectedRevision, options.Actor, operationengine.LocalizationOptions{ActorCollection: options.ActorCollection})
}

// GlobalVersions returns retained revisions for a versioned global.
func (local *LocalAPI) GlobalVersions(ctx context.Context, slug string, actor *store.Document, localeOptions ...LocaleOptions) ([]store.Version, error) {
	return local.GlobalVersionsWithOptions(ctx, slug, versionFindOptions(actor, localeOptions))
}

func (local *LocalAPI) GlobalVersionsWithOptions(ctx context.Context, slug string, options FindOptions) ([]store.Version, error) {
	return local.engine.Versions(ctx, "global:"+slug, slug, options.Actor, versionLocalizationOptions(options))
}

// GlobalVersion returns one authorized retained singleton revision.
func (local *LocalAPI) GlobalVersion(ctx context.Context, slug string, revision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Version, error) {
	return local.GlobalVersionWithOptions(ctx, slug, revision, versionFindOptions(actor, localeOptions))
}

func (local *LocalAPI) GlobalVersionWithOptions(ctx context.Context, slug string, revision int, options FindOptions) (store.Version, error) {
	return local.engine.Version(ctx, "global:"+slug, slug, revision, options.Actor, versionLocalizationOptions(options))
}

// RestoreGlobal restores a retained global revision.
func (local *LocalAPI) RestoreGlobal(ctx context.Context, slug string, revision, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.RestoreGlobalVersionWithOptions(ctx, slug, revision, false, mutationOptions(actor, expectedRevision, localeOptions))
}

// RestoreGlobalAsDraft restores a retained global revision without publishing it.
func (local *LocalAPI) RestoreGlobalAsDraft(ctx context.Context, slug string, revision, expectedRevision int, actor *store.Document, localeOptions ...LocaleOptions) (store.Document, error) {
	return local.RestoreGlobalVersionWithOptions(ctx, slug, revision, true, mutationOptions(actor, expectedRevision, localeOptions))
}

func (local *LocalAPI) RestoreGlobalVersionWithOptions(ctx context.Context, slug string, revision int, draft bool, options MutationOptions) (store.Document, error) {
	result, err := local.engine.RestorePopulated(ctx, "global:"+slug, slug, revision, options.ExpectedRevision, draft, options.Actor, options.Populate, options.OutputFields, operationengine.LocalizationOptions{
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales, ActorCollection: options.ActorCollection,
	})
	return requiredDocument(result, err)
}

func requiredDocument(result operationengine.Result, err error) (store.Document, error) {
	if err != nil {
		return store.Document{}, err
	}
	if result.Document == nil {
		return store.Document{}, fmt.Errorf("operation engine returned no document")
	}
	return *result.Document, nil
}
