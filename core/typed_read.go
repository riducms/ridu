package core

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// TypedAllLocalesCollection binds the generated all-locales output model.
// Ordinary single-locale reads and writes use TypedCollection.
type TypedAllLocalesCollection[Document any] struct {
	slug string
}

// NewTypedAllLocalesCollection constructs an all-locales generated read definition.
func NewTypedAllLocalesCollection[Document any](slug string) TypedAllLocalesCollection[Document] {
	return TypedAllLocalesCollection[Document]{slug: slug}
}

// With binds the generated all-locales definition to the operation engine.
func (definition TypedAllLocalesCollection[Document]) With(local *LocalAPI) BoundTypedAllLocalesCollection[Document] {
	return BoundTypedAllLocalesCollection[Document]{definition: definition, local: local}
}

// BoundTypedAllLocalesCollection reads documents with locale maps in localized fields.
type BoundTypedAllLocalesCollection[Document any] struct {
	definition TypedAllLocalesCollection[Document]
	local      *LocalAPI
}

// TypedReadOptions controls population/projection without changing the generated
// definition's single-locale or all-locales output shape.
type TypedReadOptions struct {
	// Draft explicitly includes draft documents when true or restricts reads to
	// published documents when false. Nil preserves the Local API default.
	Draft *bool
	// Select restricts returned document fields.
	Select []query.Path
	// Populate expands configured relationship paths.
	Populate []query.Population
	// OutputFields limits computed and inverse-join resolution without
	// projecting away stored fields. Nil resolves all output fields; a non-nil
	// empty slice resolves none.
	OutputFields []query.Path
	// Actor is the authenticated document used by access rules.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Locale selects one configured content locale. Empty uses the application default.
	Locale schema.LocaleCode
	// FallbackLocales replaces the locale's configured fallback chain when non-nil.
	FallbackLocales []schema.LocaleCode
	// DisableFallback requires an exact value in Locale.
	DisableFallback bool
}

// TypedListOptions controls a populated list without changing the binding's
// single-locale or all-locales document shape. Nil Select preserves all fields;
// an explicitly empty Select returns only document metadata.
type TypedListOptions struct {
	// Where filters documents before pagination and remains atomic with access predicates.
	Where query.Expression
	// Page is the one-based result page. Zero selects the first page.
	Page int
	// Limit is the maximum documents returned per page.
	Limit int
	// Sort orders results by authored field paths.
	Sort []query.Sort
	// TrashOnly returns deleted documents and is valid only for trash-enabled collections.
	TrashOnly bool
	// Draft explicitly includes draft documents when true or restricts reads to
	// published documents when false. Nil preserves the Local API default.
	Draft *bool
	// Select restricts returned document fields.
	Select []query.Path
	// Populate expands configured relationship paths.
	Populate []query.Population
	// OutputFields limits computed and inverse-join resolution without
	// projecting away stored fields. Nil resolves all output fields; a non-nil
	// empty slice resolves none.
	OutputFields []query.Path
	// Actor is the authenticated document used by access rules.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Locale selects one configured content locale. Empty uses the application default.
	Locale schema.LocaleCode
	// FallbackLocales replaces the locale's configured fallback chain when non-nil.
	FallbackLocales []schema.LocaleCode
	// DisableFallback requires an exact value in Locale.
	DisableFallback bool
}

// Find reads with the locale shape chosen by the generated definition.
func (collection BoundTypedAllLocalesCollection[Document]) Find(ctx context.Context, id string, options TypedReadOptions) (Document, error) {
	document, err := collection.local.Find(ctx, collection.definition.slug, id, options.findOptions(true))
	return decodeTypedDocument[Document](document, err)
}

// List filters and populates documents through the operation engine, using the
// same response shape as Find. A failed document decode returns no partial page.
func (collection BoundTypedAllLocalesCollection[Document]) List(ctx context.Context, options TypedListOptions) (TypedPage[Document], error) {
	return listTypedDocuments[Document](ctx, collection.local, collection.definition.slug, options, true)
}

func listTypedDocuments[Document any](ctx context.Context, local *LocalAPI, slug string, options TypedListOptions, allLocales bool) (TypedPage[Document], error) {
	page, err := local.List(ctx, slug, ListOptions{
		Where: options.Where, Page: options.Page, Limit: options.Limit, Sort: options.Sort,
		Select: options.Select, Populate: options.Populate, OutputFields: options.OutputFields,
		Draft: options.Draft, Actor: options.Actor, ActorCollection: options.ActorCollection,
		TrashOnly: options.TrashOnly, Locale: options.Locale, FallbackLocales: options.FallbackLocales,
		DisableFallback: options.DisableFallback, AllLocales: allLocales,
	})
	if err != nil {
		return TypedPage[Document]{}, err
	}
	documents := make([]Document, len(page.Documents))
	for index, stored := range page.Documents {
		documents[index], err = decodeStoredDocument[Document](stored)
		if err != nil {
			return TypedPage[Document]{}, fmt.Errorf("decode %s list document[%d]: %w", slug, index, err)
		}
	}
	return TypedPage[Document]{Documents: documents, Page: page.Page, Limit: page.Limit, Total: page.Total}, nil
}

// TypedAllLocalesGlobal binds a generated global model with localized value maps.
// Ordinary single-locale reads and writes use TypedGlobal.
type TypedAllLocalesGlobal[Document any] struct{ slug string }

// NewTypedAllLocalesGlobal constructs an all-locales generated global definition.
func NewTypedAllLocalesGlobal[Document any](slug string) TypedAllLocalesGlobal[Document] {
	return TypedAllLocalesGlobal[Document]{slug: slug}
}

// With binds the generated all-locales global definition to the operation engine.
func (definition TypedAllLocalesGlobal[Document]) With(local *LocalAPI) BoundTypedAllLocalesGlobal[Document] {
	return BoundTypedAllLocalesGlobal[Document]{definition: definition, local: local}
}

// BoundTypedAllLocalesGlobal reads a singleton with locale maps in localized fields.
type BoundTypedAllLocalesGlobal[Document any] struct {
	definition TypedAllLocalesGlobal[Document]
	local      *LocalAPI
}

// Find reads the global with all translations and optional population/projection.
func (global BoundTypedAllLocalesGlobal[Document]) Find(ctx context.Context, options TypedReadOptions) (Document, error) {
	document, err := global.local.Global(ctx, global.definition.slug, options.findOptions(true))
	return decodeTypedDocument[Document](document, err)
}

func (options TypedReadOptions) findOptions(allLocales bool) FindOptions {
	return FindOptions{Draft: options.Draft, Select: options.Select, Populate: options.Populate, OutputFields: options.OutputFields, Actor: options.Actor, ActorCollection: options.ActorCollection, Locale: options.Locale, FallbackLocales: options.FallbackLocales, DisableFallback: options.DisableFallback, AllLocales: allLocales}
}
