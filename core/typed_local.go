package core

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// TypedCollection is a generated collection definition that can be bound to
// an application's LocalAPI. It adds compile-time application models without
// creating a privileged data path.
type TypedCollection[Document, Create, Update, Draft any] struct {
	slug string
}

// NewTypedCollection constructs a generated typed collection definition.
func NewTypedCollection[Document, Create, Update, Draft any](slug string) TypedCollection[Document, Create, Update, Draft] {
	return TypedCollection[Document, Create, Update, Draft]{slug: slug}
}

// Slug returns the collection's public API address.
func (collection TypedCollection[Document, Create, Update, Draft]) Slug() string {
	return collection.slug
}

// With binds a generated collection definition to a running application.
func (collection TypedCollection[Document, Create, Update, Draft]) With(local *LocalAPI) BoundTypedCollection[Document, Create, Update, Draft] {
	return BoundTypedCollection[Document, Create, Update, Draft]{definition: collection, local: local}
}

// BoundTypedCollection is a typed view of one collection on a LocalAPI.
type BoundTypedCollection[Document, Create, Update, Draft any] struct {
	definition TypedCollection[Document, Create, Update, Draft]
	local      *LocalAPI
}

// TypedPage is the typed equivalent of store.Page.
type TypedPage[Document any] struct {
	Documents []Document
	Page      int
	Limit     int
	Total     int
}

func (collection BoundTypedCollection[Document, Create, Update, Draft]) Create(ctx context.Context, input Create, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.Create(ctx, collection.definition.slug, values, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

// CreateDraft saves an editorially incomplete draft through the same engine as Create.
func (collection BoundTypedCollection[Document, Create, Update, Draft]) CreateDraft(ctx context.Context, input Draft, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	mutation := options.mutationOptions()
	draft := true
	mutation.Draft = &draft
	document, err := collection.local.Create(ctx, collection.definition.slug, values, mutation)
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update, Draft]) Import(ctx context.Context, input Create, options ImportOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.Import(ctx, collection.definition.slug, values, options)
	return decodeTypedDocument[Document](document, err)
}

// Find reads one locale with optional population and projection.
func (collection BoundTypedCollection[Document, Create, Update, Draft]) Find(ctx context.Context, id string, options TypedReadOptions) (Document, error) {
	document, err := collection.local.Find(ctx, collection.definition.slug, id, options.findOptions(false))
	return decodeTypedDocument[Document](document, err)
}

// List filters, paginates and populates one locale through the operation engine.
// A failed decode returns an error and no partial page.
func (collection BoundTypedCollection[Document, Create, Update, Draft]) List(ctx context.Context, options TypedListOptions) (TypedPage[Document], error) {
	return listTypedDocuments[Document](ctx, collection.local, collection.definition.slug, options, false)
}

func (collection BoundTypedCollection[Document, Create, Update, Draft]) Update(ctx context.Context, id string, input Update, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.Update(ctx, collection.definition.slug, id, values, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

// SaveDraft stages incomplete working values without modifying the live head.
func (collection BoundTypedCollection[Document, Create, Update, Draft]) SaveDraft(ctx context.Context, id string, input Draft, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	mutation := options.mutationOptions()
	draft := true
	mutation.Draft = &draft
	document, err := collection.local.Update(ctx, collection.definition.slug, id, values, mutation)
	return decodeTypedDocument[Document](document, err)
}

// DiscardDraft resets pending working values to the published document.
func (collection BoundTypedCollection[Document, Create, Update, Draft]) DiscardDraft(ctx context.Context, id string, options TypedMutationOptions) (Document, error) {
	document, err := collection.local.DiscardDraft(ctx, collection.definition.slug, id, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update, Draft]) Delete(ctx context.Context, id string, options TypedMutationOptions) (Document, error) {
	document, err := collection.local.Delete(ctx, collection.definition.slug, id, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

func typedInputValues[Input any](input Input) (store.Values, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode typed collection input: %w", err)
	}
	var values store.Values
	if err := values.UnmarshalJSON(encoded); err != nil {
		return nil, fmt.Errorf("convert typed collection input: %w", err)
	}
	return values, nil
}

func decodeTypedDocument[Document any](stored store.Document, err error) (Document, error) {
	if err != nil {
		return *new(Document), err
	}
	return decodeStoredDocument[Document](stored)
}

// decodeStoredDocument encodes the document once and decodes it into the typed
// model. Metadata is added after the fields, so it wins a name collision.
func decodeStoredDocument[Document any](stored store.Document) (Document, error) {
	values := make(store.Values, len(stored.Values)+5)
	maps.Copy(values, stored.Values)
	values["id"] = store.String(stored.ID)
	values["createdAt"] = store.String(stored.CreatedAt.Format(time.RFC3339Nano))
	values["updatedAt"] = store.String(stored.UpdatedAt.Format(time.RFC3339Nano))
	if stored.Status != "" {
		values["_status"] = store.String(string(stored.Status))
	}
	if stored.Revision > 0 {
		values["_revision"] = store.Number(float64(stored.Revision))
	}
	if stored.PublishedRevision > 0 {
		values["_publishedRevision"] = store.Number(float64(stored.PublishedRevision))
		values["_hasDraftChanges"] = store.Boolean(stored.HasDraftChanges)
	}
	encoded, err := values.MarshalJSON()
	if err != nil {
		return *new(Document), fmt.Errorf("encode typed document: %w", err)
	}
	var document Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		return *new(Document), fmt.Errorf("decode typed document: %w", err)
	}
	return document, nil
}

// TypedMutationOptions controls a generated mutation whose response is always single-locale.
// The semantic action consumes the same settings as its LocalAPI equivalent.
type TypedMutationOptions struct {
	ID              string
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	// System skips access rules for trusted server code; see MutationOptions.System.
	System           bool
	ExpectedRevision int
	Populate         []query.Population
	OutputFields     []query.Path
	Draft            *bool
	Locale           schema.LocaleCode
	FallbackLocales  []schema.LocaleCode
	DisableFallback  bool
}

func (options TypedMutationOptions) mutationOptions() MutationOptions {
	return MutationOptions{ID: options.ID, Actor: options.Actor, ActorCollection: options.ActorCollection, System: options.System,
		ExpectedRevision: options.ExpectedRevision, Populate: options.Populate, OutputFields: options.OutputFields,
		Draft: options.Draft, Locale: options.Locale, FallbackLocales: options.FallbackLocales,
		DisableFallback: options.DisableFallback}
}
