package core

import (
	"context"
	"fmt"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
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
	// Paths with a Read rule on the field or an ancestor are not queryable.
	Where query.Expression
	// Page is the one-based result page. Zero selects the first page.
	Page int
	// Limit is the maximum documents returned per page.
	Limit int
	// Sort orders results by authored field paths without Read rules on the field or ancestors.
	// Container sorts also require all descendants to have no Read rules.
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
// authorization for one document read. History reads consume only actor and
// locale controls; they do not project, populate, or filter retained revisions.
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
// storage behavior. CopyLocale and CopyGlobalLocale consume only Actor,
// ActorCollection and ExpectedRevision; their locales remain positional.
// MutateJoin consumes only actor and locale controls. Restore consumes actor,
// expected revision, population, output selection and locale controls.
// ID and Draft are create-only controls; publication uses its named actions.
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

// BulkOptions controls the actor and localization of an atomic batch or empty-trash operation.
// Bulk actions do not consume per-document revisions, population, draft status, or caller IDs.
type BulkOptions struct {
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
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
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
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

// Create creates a document and applies the requested bounded
// population plan to the response inside the write transaction.
func (local *LocalAPI) Create(ctx context.Context, collection string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Create, Collection: collection, Data: values}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) createStoragePrepared(ctx context.Context, collection string, values store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Create, Collection: collection, Data: values, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) createWithTransactionMutation(ctx context.Context, collection string, values store.Values, options MutationOptions, mutation operationengine.TransactionMutation) (store.Document, error) {
	return local.createWithTransactionMutationAndFieldAccess(ctx, collection, values, options, mutation, false)
}

func (local *LocalAPI) createWithTransactionMutationAndFieldAccess(ctx context.Context, collection string, values store.Values, options MutationOptions, mutation operationengine.TransactionMutation, skipFieldAccess bool) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Create, Collection: collection, Data: values,
		TransactionMutation: mutation, SkipFieldAccess: skipFieldAccess,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// Duplicate duplicates a document and populates the returned copy
// inside the write transaction.
func (local *LocalAPI) Duplicate(ctx context.Context, collection, id string, overrides store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Duplicate, Collection: collection, ID: id, Data: overrides}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) duplicateStoragePrepared(ctx context.Context, collection, id string, overrides store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Duplicate, Collection: collection, ID: id, Data: overrides, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// Import creates a document with a migration-owned stable ID while still
// running ordinary access, validation, hooks, transaction, and version logic.
func (local *LocalAPI) Import(ctx context.Context, collection string, values store.Values, options ImportOptions) (store.Document, error) {
	if err := store.ValidateDocumentID(options.ID); err != nil {
		return store.Document{}, &operationengine.Error{
			Code: "validation", Status: 422, Message: "document validation failed",
			Issues: []schema.Issue{{Code: "invalid_document_id", Path: "id", Message: "migration document ID is invalid: " + err.Error()}},
			Cause:  err,
		}
	}
	result, err := local.engine.Execute(ctx, operationengine.Request{Operation: operation.Create, Collection: collection, ImportID: options.ID, ImportCreatedAt: options.CreatedAt, ImportUpdatedAt: options.UpdatedAt, Data: values, Status: &options.Status, Actor: options.Actor, ActorCollection: options.ActorCollection})
	return requiredDocument(result, err)
}

// Find reads one document through the operation engine with a
// transport-owned projection and population plan.
func (local *LocalAPI) Find(ctx context.Context, collection, id string, options FindOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Read, Collection: collection, ID: id, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Select: options.Select, Populate: options.Populate, OutputFields: cloneOptionalPaths(options.OutputFields), Draft: cloneOptionalBool(options.Draft), TrashOnly: options.TrashOnly,
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	}
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) List(ctx context.Context, collection string, options ListOptions) (store.Page, error) {
	result, err := local.engine.Execute(ctx, listRequest(collection, options))
	if err != nil {
		return store.Page{}, err
	}
	if result.Page == nil {
		return store.Page{}, fmt.Errorf("operation engine returned no page")
	}
	return *result.Page, nil
}

// ListJoin reads a configured inverse join through source and target read access.
// Only the engine derives the membership predicate; caller Where and Sort keep
// ordinary List field-query restrictions even when the backing relation is private.
func (local *LocalAPI) ListJoin(ctx context.Context, collection, id, field string, options ListOptions) (store.Page, error) {
	result, err := local.engine.ListJoin(ctx, collection, id, field, listRequest("", options))
	if err != nil {
		return store.Page{}, err
	}
	if result.Page == nil {
		return store.Page{}, fmt.Errorf("operation engine returned no join page")
	}
	return *result.Page, nil
}

func listRequest(collection string, options ListOptions) operationengine.Request {
	return operationengine.Request{
		Operation: operation.Read, Collection: collection, Filter: options.Where,
		Page: options.Page, Limit: options.Limit, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Sort: options.Sort, Select: options.Select, Populate: options.Populate, OutputFields: cloneOptionalPaths(options.OutputFields), Draft: cloneOptionalBool(options.Draft), TrashOnly: options.TrashOnly,
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	}
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
		Operation: operation.Read, Collection: collection,
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

// Update updates a document and applies the requested bounded
// population plan to the response inside the write transaction.
func (local *LocalAPI) Update(ctx context.Context, collection, id string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Update, Collection: collection, ID: id, Data: values}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func applyMutationOptions(request *operationengine.Request, options MutationOptions) {
	if request.Operation == operation.Create && options.ID != "" {
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

func versionLocalizationOptions(options FindOptions) operationengine.LocalizationOptions {
	return operationengine.LocalizationOptions{
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales, ActorCollection: options.ActorCollection,
	}
}

func (local *LocalAPI) updateStoragePrepared(ctx context.Context, collection, id string, values store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Update, Collection: collection, ID: id, Data: values, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) publishStoragePrepared(ctx context.Context, collection, id string, values store.Values, options MutationOptions, resource *operationengine.TransactionResource) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Publish, Collection: collection, ID: id, Data: values, StoragePrepared: true, TransactionResource: resource}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// MutateJoin preserves the exact authenticated collection identity
// while applying inverse relationship deltas.
func (local *LocalAPI) MutateJoin(ctx context.Context, collection, id, field string, additions, removals []string, options MutationOptions) (JoinMutationResult, error) {
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

func (local *LocalAPI) Publish(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Publish, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) PublishChanges(ctx context.Context, collection, id string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Publish, Collection: collection, ID: id, Data: values}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) Unpublish(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Unpublish, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// CopyLocale preserves the exact authenticated collection identity
// for both the source read and destination mutation lifecycle.
func (local *LocalAPI) CopyLocale(ctx context.Context, collection, id string, source, target schema.LocaleCode, options MutationOptions) (store.Document, error) {
	return local.engine.CopyLocale(ctx, collection, id, source, target, options.ExpectedRevision, options.Actor, operationengine.LocalizationOptions{ActorCollection: options.ActorCollection})
}

// Versions preserves the exact authenticated collection identity
// while reading retained revisions.
func (local *LocalAPI) Versions(ctx context.Context, collection, id string, options FindOptions) ([]store.Version, error) {
	return local.engine.Versions(ctx, collection, id, options.Actor, versionLocalizationOptions(options))
}

// Version preserves the exact authenticated collection identity
// while reading one retained revision.
func (local *LocalAPI) Version(ctx context.Context, collection, id string, revision int, options FindOptions) (store.Version, error) {
	return local.engine.Version(ctx, collection, id, revision, options.Actor, versionLocalizationOptions(options))
}

func (local *LocalAPI) Restore(ctx context.Context, collection, id string, revision int, options MutationOptions) (store.Document, error) {
	return local.restoreVersion(ctx, collection, id, revision, false, options)
}

// RestoreAsDraft restores a retained revision while explicitly keeping the
// current document unpublished.
func (local *LocalAPI) RestoreAsDraft(ctx context.Context, collection, id string, revision int, options MutationOptions) (store.Document, error) {
	return local.restoreVersion(ctx, collection, id, revision, true, options)
}

// RestoreVersion restores a retained revision and populates the
// returned document in the update transaction.
func (local *LocalAPI) restoreVersion(ctx context.Context, collection, id string, revision int, draft bool, options MutationOptions) (store.Document, error) {
	result, err := local.engine.RestorePopulated(ctx, collection, id, revision, options.ExpectedRevision, draft, options.Actor, options.Populate, options.OutputFields, operationengine.LocalizationOptions{
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales, ActorCollection: options.ActorCollection,
	})
	return requiredDocument(result, err)
}

func (local *LocalAPI) Delete(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.Delete, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) RestoreDeleted(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.RestoreDeleted, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) DeletePermanent(ctx context.Context, collection, id string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{Operation: operation.DeletePermanent, Collection: collection, ID: id}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// BulkUpdate atomically applies the same partial values to every document.
func (local *LocalAPI) BulkUpdate(ctx context.Context, collection string, ids []string, values store.Values, options BulkOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operation.Update, values, options)
}

// BulkPublish atomically publishes every selected versioned document.
func (local *LocalAPI) BulkPublish(ctx context.Context, collection string, ids []string, options BulkOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operation.Publish, nil, options)
}

// BulkUnpublish atomically returns every selected versioned document to draft.
func (local *LocalAPI) BulkUnpublish(ctx context.Context, collection string, ids []string, options BulkOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operation.Unpublish, nil, options)
}

// BulkDelete atomically deletes or trashes every selected document.
func (local *LocalAPI) BulkDelete(ctx context.Context, collection string, ids []string, options BulkOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operation.Delete, nil, options)
}

// BulkRestoreDeleted atomically restores selected documents from trash.
func (local *LocalAPI) BulkRestoreDeleted(ctx context.Context, collection string, ids []string, options BulkOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operation.RestoreDeleted, nil, options)
}

// BulkDeletePermanent atomically permanently deletes selected trashed documents.
func (local *LocalAPI) BulkDeletePermanent(ctx context.Context, collection string, ids []string, options BulkOptions) ([]store.Document, error) {
	return local.bulk(ctx, collection, ids, operation.DeletePermanent, nil, options)
}

// EmptyTrash permanently deletes every accessible trashed document in one
// bounded atomic batch.
func (local *LocalAPI) EmptyTrash(ctx context.Context, collection string, options BulkOptions) ([]store.Document, error) {
	listOptions := ListOptions{Page: 1, Limit: 100, TrashOnly: true,
		Actor: options.Actor, ActorCollection: options.ActorCollection,
		Locale: options.Locale, FallbackLocales: options.FallbackLocales,
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
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
	return local.BulkDeletePermanent(ctx, collection, ids, options)
}

func (local *LocalAPI) bulk(ctx context.Context, collection string, ids []string, kind operation.Kind, values store.Values, options BulkOptions) ([]store.Document, error) {
	requests := make([]operationengine.Request, len(ids))
	for index, id := range ids {
		requests[index] = operationengine.Request{Operation: kind, Collection: collection, ID: id, Data: store.CloneValues(values), Actor: options.Actor, ActorCollection: options.ActorCollection,
			Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
			DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
		}
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

// Global reads a singleton with transport-owned projection and
// population through the ordinary operation engine.
func (local *LocalAPI) Global(ctx context.Context, slug string, options FindOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Read, Collection: "global:" + slug, ID: slug, Actor: options.Actor, ActorCollection: options.ActorCollection,
		Select: options.Select, Populate: options.Populate, OutputFields: cloneOptionalPaths(options.OutputFields), Draft: cloneOptionalBool(options.Draft),
		Locale: string(options.Locale), FallbackLocales: append([]schema.LocaleCode(nil), options.FallbackLocales...),
		DisableFallback: options.DisableFallback, AllLocales: options.AllLocales,
	}
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

// UpdateGlobal updates a singleton and populates its response in
// the same write transaction.
func (local *LocalAPI) UpdateGlobal(ctx context.Context, slug string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Update, Collection: "global:" + slug, ID: slug,
		Data: values,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) PublishGlobal(ctx context.Context, slug string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Publish, Collection: "global:" + slug, ID: slug,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) PublishGlobalChanges(ctx context.Context, slug string, values store.Values, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Publish, Collection: "global:" + slug, ID: slug, Data: values,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) UnpublishGlobal(ctx context.Context, slug string, options MutationOptions) (store.Document, error) {
	request := operationengine.Request{
		Operation: operation.Unpublish, Collection: "global:" + slug, ID: slug,
	}
	applyMutationOptions(&request, options)
	result, err := local.engine.Execute(ctx, request)
	return requiredDocument(result, err)
}

func (local *LocalAPI) CopyGlobalLocale(ctx context.Context, slug string, source, target schema.LocaleCode, options MutationOptions) (store.Document, error) {
	return local.engine.CopyLocale(ctx, "global:"+slug, slug, source, target, options.ExpectedRevision, options.Actor, operationengine.LocalizationOptions{ActorCollection: options.ActorCollection})
}

func (local *LocalAPI) GlobalVersions(ctx context.Context, slug string, options FindOptions) ([]store.Version, error) {
	return local.engine.Versions(ctx, "global:"+slug, slug, options.Actor, versionLocalizationOptions(options))
}

func (local *LocalAPI) GlobalVersion(ctx context.Context, slug string, revision int, options FindOptions) (store.Version, error) {
	return local.engine.Version(ctx, "global:"+slug, slug, revision, options.Actor, versionLocalizationOptions(options))
}

// RestoreGlobal restores a retained global revision.
func (local *LocalAPI) RestoreGlobal(ctx context.Context, slug string, revision int, options MutationOptions) (store.Document, error) {
	return local.restoreGlobalVersion(ctx, slug, revision, false, options)
}

// RestoreGlobalAsDraft restores a retained global revision without publishing it.
func (local *LocalAPI) RestoreGlobalAsDraft(ctx context.Context, slug string, revision int, options MutationOptions) (store.Document, error) {
	return local.restoreGlobalVersion(ctx, slug, revision, true, options)
}

func (local *LocalAPI) restoreGlobalVersion(ctx context.Context, slug string, revision int, draft bool, options MutationOptions) (store.Document, error) {
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
