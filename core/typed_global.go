package core

import (
	"context"
)

// TypedGlobal is a generated singleton definition that can be bound to an
// application's LocalAPI without creating a privileged data path.
type TypedGlobal[Document, Update, Draft any] struct {
	slug string
}

// NewTypedGlobal constructs a generated typed global definition.
func NewTypedGlobal[Document, Update, Draft any](slug string) TypedGlobal[Document, Update, Draft] {
	return TypedGlobal[Document, Update, Draft]{slug: slug}
}

// Slug returns the global's public API address.
func (global TypedGlobal[Document, Update, Draft]) Slug() string { return global.slug }

// With binds a generated global definition to a running application.
func (global TypedGlobal[Document, Update, Draft]) With(local *LocalAPI) BoundTypedGlobal[Document, Update, Draft] {
	return BoundTypedGlobal[Document, Update, Draft]{definition: global, local: local}
}

// BoundTypedGlobal is a typed view of one singleton on a LocalAPI.
type BoundTypedGlobal[Document, Update, Draft any] struct {
	definition TypedGlobal[Document, Update, Draft]
	local      *LocalAPI
}

func (global BoundTypedGlobal[Document, Update, Draft]) Find(ctx context.Context, options TypedReadOptions) (Document, error) {
	document, err := global.local.Global(ctx, global.definition.slug, options.findOptions(false))
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update, Draft]) Update(ctx context.Context, input Update, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := global.local.UpdateGlobal(ctx, global.definition.slug, values, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

// SaveDraft stages incomplete working values without changing the live global.
func (global BoundTypedGlobal[Document, Update, Draft]) SaveDraft(ctx context.Context, input Draft, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	mutation := options.mutationOptions()
	draft := true
	mutation.Draft = &draft
	document, err := global.local.UpdateGlobal(ctx, global.definition.slug, values, mutation)
	return decodeTypedDocument[Document](document, err)
}

// DiscardDraft resets pending working values to the published singleton.
func (global BoundTypedGlobal[Document, Update, Draft]) DiscardDraft(ctx context.Context, options TypedMutationOptions) (Document, error) {
	document, err := global.local.DiscardGlobalDraft(ctx, global.definition.slug, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update, Draft]) Publish(ctx context.Context, options TypedMutationOptions) (Document, error) {
	document, err := global.local.PublishGlobal(ctx, global.definition.slug, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

// PublishChanges applies input and publishes the result in one operation. A
// published versioned global changes this way; Update refuses it with
// publish_required.
func (global BoundTypedGlobal[Document, Update, Draft]) PublishChanges(ctx context.Context, input Update, options TypedMutationOptions) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := global.local.PublishGlobalChanges(ctx, global.definition.slug, values, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update, Draft]) Unpublish(ctx context.Context, options TypedMutationOptions) (Document, error) {
	document, err := global.local.UnpublishGlobal(ctx, global.definition.slug, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update, Draft]) Restore(ctx context.Context, revision int, options TypedMutationOptions) (Document, error) {
	document, err := global.local.RestoreGlobal(ctx, global.definition.slug, revision, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update, Draft]) RestoreAsDraft(ctx context.Context, revision int, options TypedMutationOptions) (Document, error) {
	document, err := global.local.RestoreGlobalAsDraft(ctx, global.definition.slug, revision, options.mutationOptions())
	return decodeTypedDocument[Document](document, err)
}
