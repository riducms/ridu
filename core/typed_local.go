package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// TypedCollection is a generated collection definition that can be bound to
// an application's LocalAPI. It adds compile-time application models without
// creating a privileged data path.
type TypedCollection[Document, Create, Update any] struct {
	slug string
}

// NewTypedCollection constructs a generated typed collection definition.
func NewTypedCollection[Document, Create, Update any](slug string) TypedCollection[Document, Create, Update] {
	return TypedCollection[Document, Create, Update]{slug: slug}
}

// Slug returns the collection's public API address.
func (collection TypedCollection[Document, Create, Update]) Slug() string { return collection.slug }

// With binds a generated collection definition to a running application.
func (collection TypedCollection[Document, Create, Update]) With(local *LocalAPI) BoundTypedCollection[Document, Create, Update] {
	return BoundTypedCollection[Document, Create, Update]{definition: collection, local: local}
}

// BoundTypedCollection is a typed view of one collection on a LocalAPI.
type BoundTypedCollection[Document, Create, Update any] struct {
	definition TypedCollection[Document, Create, Update]
	local      *LocalAPI
}

// TypedPage is the typed equivalent of store.Page.
type TypedPage[Document any] struct {
	Documents []Document
	Page      int
	Limit     int
	Total     int
}

// TypedListOptions is the subset of ListOptions whose response shape can be
// represented by a generated Go document model. Population and all-locale
// reads intentionally remain on LocalAPI: both change field value shapes at
// runtime and therefore cannot be decoded into one static Document type.
type TypedListOptions struct {
	Where           query.Expression
	Page            int
	Limit           int
	Sort            []query.Sort
	Select          []query.Path
	OutputFields    []query.Path
	Draft           *bool
	Actor           *store.Document
	ActorCollection schema.CollectionSlug
	TrashOnly       bool
	Locale          schema.LocaleCode
	FallbackLocales []schema.LocaleCode
	DisableFallback bool
}

func (collection BoundTypedCollection[Document, Create, Update]) Create(ctx context.Context, input Create, actor *store.Document) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.Create(ctx, collection.definition.slug, values, actor)
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update]) Import(ctx context.Context, input Create, options ImportOptions, actor *store.Document) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.Import(ctx, collection.definition.slug, values, options, actor)
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update]) Find(ctx context.Context, id string, actor *store.Document) (Document, error) {
	document, err := collection.local.Find(ctx, collection.definition.slug, id, actor)
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update]) List(ctx context.Context, options TypedListOptions) (TypedPage[Document], error) {
	page, err := collection.local.List(ctx, collection.definition.slug, ListOptions{
		Where: options.Where, Page: options.Page, Limit: options.Limit, Sort: options.Sort,
		Select: options.Select, OutputFields: options.OutputFields, Draft: options.Draft,
		Actor: options.Actor, ActorCollection: options.ActorCollection, TrashOnly: options.TrashOnly,
		Locale: options.Locale, FallbackLocales: options.FallbackLocales, DisableFallback: options.DisableFallback,
	})
	if err != nil {
		return TypedPage[Document]{}, err
	}
	documents := make([]Document, len(page.Documents))
	for index, stored := range page.Documents {
		documents[index], err = decodeStoredDocument[Document](stored)
		if err != nil {
			return TypedPage[Document]{}, err
		}
	}
	return TypedPage[Document]{Documents: documents, Page: page.Page, Limit: page.Limit, Total: page.Total}, nil
}

func (collection BoundTypedCollection[Document, Create, Update]) Update(ctx context.Context, id string, input Update, actor *store.Document) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.Update(ctx, collection.definition.slug, id, values, actor)
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update]) UpdateRevision(ctx context.Context, id string, input Update, expectedRevision int, actor *store.Document) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := collection.local.UpdateRevision(ctx, collection.definition.slug, id, values, expectedRevision, actor)
	return decodeTypedDocument[Document](document, err)
}

func (collection BoundTypedCollection[Document, Create, Update]) Delete(ctx context.Context, id string, actor *store.Document) (Document, error) {
	document, err := collection.local.Delete(ctx, collection.definition.slug, id, actor)
	return decodeTypedDocument[Document](document, err)
}

func typedInputValues[Input any](input Input) (store.Values, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode typed collection input: %w", err)
	}
	var values store.Values
	if err := json.Unmarshal(encoded, &values); err != nil {
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

func decodeStoredDocument[Document any](stored store.Document) (Document, error) {
	encodedValues := make(map[string]json.RawMessage, len(stored.Values)+5)
	for name, value := range stored.Values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return *new(Document), fmt.Errorf("encode typed document field %q: %w", name, err)
		}
		encodedValues[name] = encoded
	}
	encodedValues["id"], _ = json.Marshal(stored.ID)
	encodedValues["createdAt"], _ = json.Marshal(stored.CreatedAt.Format(time.RFC3339Nano))
	encodedValues["updatedAt"], _ = json.Marshal(stored.UpdatedAt.Format(time.RFC3339Nano))
	if stored.Status != "" {
		encodedValues["_status"], _ = json.Marshal(stored.Status)
		encodedValues["_revision"], _ = json.Marshal(stored.Revision)
	}
	encoded, err := json.Marshal(encodedValues)
	if err != nil {
		return *new(Document), fmt.Errorf("encode typed document: %w", err)
	}
	var document Document
	if err := json.Unmarshal(encoded, &document); err != nil {
		return *new(Document), fmt.Errorf("decode typed document: %w", err)
	}
	return document, nil
}
