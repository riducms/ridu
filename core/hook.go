package core

import (
	"context"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// HookContext contains stable operation identity. Typed document input and
// result contracts arrive with the operation engine rather than using any.
type HookContext struct {
	// Context is the request-scoped cancellation and deadline context.
	Context context.Context
	// Operation is the lifecycle operation currently running.
	Operation Operation
	// CollectionID is the stable identity of the target collection.
	CollectionID schema.StableID
	// GlobalID is the stable identity of the target global, when applicable.
	GlobalID schema.StableID
	// Actor is the authenticated document, or nil for an anonymous operation.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Data contains mutable incoming values before persistence.
	Data store.Values
	// Document is the current operation result when available.
	Document *store.Document
	// Original is the persisted document before an update or delete.
	Original *store.Document
	// Local exposes nested operations through the same operation engine.
	Local *LocalAPI
	// FieldPath is set when a hook is registered for a specific field.
	FieldPath string
	// Error is set only for after-error hooks and preserves the original failure.
	Error      error
	Locale     schema.LocaleCode
	AllLocales bool
}

// Hook is one operation lifecycle callback.
type Hook func(HookContext) error

// AfterCommitEffect is a committed, named effect that a dispatcher may run
// immediately or hand to a durable execution boundary.
type AfterCommitEffect struct {
	// Operation identifies the committed operation.
	Operation Operation
	// CollectionID identifies the affected collection.
	CollectionID schema.StableID
	// GlobalID identifies the affected global, when applicable.
	GlobalID schema.StableID
	// DocumentID identifies the affected document.
	DocumentID string
	// Run performs the post-commit side effect.
	Run func(context.Context) error
}

// AfterCommitDispatcher delivers committed effects immediately or to a durable worker.
type AfterCommitDispatcher interface {
	Dispatch(context.Context, AfterCommitEffect) error
}

// CollectionHooks establishes deterministic hook phases without prematurely
// defining document mutation contracts.
type CollectionHooks struct {
	// BeforeDuplicate runs after the source is access-checked and copied but before validation.
	BeforeDuplicate []Hook
	// BeforeValidate runs before field validation.
	BeforeValidate []Hook
	// BeforeChange runs after validation for create, duplicate, update, publish, and unpublish.
	BeforeChange []Hook
	// BeforeOperation runs after validation but before persistence.
	BeforeOperation []Hook
	// BeforeRead runs before one or many documents are read.
	BeforeRead []Hook
	// BeforeDelete runs after the original document is loaded but before deletion.
	BeforeDelete []Hook
	// AfterChange runs inside the transaction after a changed document is persisted.
	AfterChange []Hook
	// AfterRead runs after computed values resolve and before field redaction.
	AfterRead []Hook
	// AfterDelete runs inside the transaction after deletion or trashing.
	AfterDelete []Hook
	// AfterOperation runs inside the transaction after persistence.
	AfterOperation []Hook
	// AfterError runs when an operation associated with this resource fails.
	AfterError []Hook
	// AfterCommit runs only after the transaction commits successfully.
	AfterCommit []Hook
}

func (hooks CollectionHooks) clone() CollectionHooks {
	hooks.BeforeDuplicate = append([]Hook(nil), hooks.BeforeDuplicate...)
	hooks.BeforeValidate = append([]Hook(nil), hooks.BeforeValidate...)
	hooks.BeforeChange = append([]Hook(nil), hooks.BeforeChange...)
	hooks.BeforeOperation = append([]Hook(nil), hooks.BeforeOperation...)
	hooks.BeforeRead = append([]Hook(nil), hooks.BeforeRead...)
	hooks.BeforeDelete = append([]Hook(nil), hooks.BeforeDelete...)
	hooks.AfterChange = append([]Hook(nil), hooks.AfterChange...)
	hooks.AfterRead = append([]Hook(nil), hooks.AfterRead...)
	hooks.AfterDelete = append([]Hook(nil), hooks.AfterDelete...)
	hooks.AfterOperation = append([]Hook(nil), hooks.AfterOperation...)
	hooks.AfterError = append([]Hook(nil), hooks.AfterError...)
	hooks.AfterCommit = append([]Hook(nil), hooks.AfterCommit...)
	return hooks
}

// RootHooks observes application-wide failures that may happen before a resource resolves.
type RootHooks struct {
	AfterError []Hook
}

func (hooks RootHooks) clone() RootHooks {
	hooks.AfterError = append([]Hook(nil), hooks.AfterError...)
	return hooks
}
