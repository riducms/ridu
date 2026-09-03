package core

import (
	"context"

	"github.com/riducms/ridu/store"
)

// TypedGlobal is a generated singleton definition that can be bound to an
// application's LocalAPI without creating a privileged data path.
type TypedGlobal[Document, Update any] struct {
	slug string
}

// NewTypedGlobal constructs a generated typed global definition.
func NewTypedGlobal[Document, Update any](slug string) TypedGlobal[Document, Update] {
	return TypedGlobal[Document, Update]{slug: slug}
}

// Slug returns the global's public API address.
func (global TypedGlobal[Document, Update]) Slug() string { return global.slug }

// With binds a generated global definition to a running application.
func (global TypedGlobal[Document, Update]) With(local *LocalAPI) BoundTypedGlobal[Document, Update] {
	return BoundTypedGlobal[Document, Update]{definition: global, local: local}
}

// BoundTypedGlobal is a typed view of one singleton on a LocalAPI.
type BoundTypedGlobal[Document, Update any] struct {
	definition TypedGlobal[Document, Update]
	local      *LocalAPI
}

func (global BoundTypedGlobal[Document, Update]) Find(ctx context.Context, actor *store.Document) (Document, error) {
	document, err := global.local.Global(ctx, global.definition.slug, actor)
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update]) Update(ctx context.Context, input Update, expectedRevision int, actor *store.Document) (Document, error) {
	values, err := typedInputValues(input)
	if err != nil {
		return *new(Document), err
	}
	document, err := global.local.UpdateGlobal(ctx, global.definition.slug, values, expectedRevision, actor)
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update]) Publish(ctx context.Context, expectedRevision int, actor *store.Document) (Document, error) {
	document, err := global.local.PublishGlobal(ctx, global.definition.slug, expectedRevision, actor)
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update]) Unpublish(ctx context.Context, expectedRevision int, actor *store.Document) (Document, error) {
	document, err := global.local.UnpublishGlobal(ctx, global.definition.slug, expectedRevision, actor)
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update]) Restore(ctx context.Context, revision, expectedRevision int, actor *store.Document) (Document, error) {
	document, err := global.local.RestoreGlobal(ctx, global.definition.slug, revision, expectedRevision, actor)
	return decodeTypedDocument[Document](document, err)
}

func (global BoundTypedGlobal[Document, Update]) RestoreAsDraft(ctx context.Context, revision, expectedRevision int, actor *store.Document) (Document, error) {
	document, err := global.local.RestoreGlobalAsDraft(ctx, global.definition.slug, revision, expectedRevision, actor)
	return decodeTypedDocument[Document](document, err)
}
