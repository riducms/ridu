package core

import (
	"context"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// HookContext supplies the request, values, and local API to a collection or
// global hook. Change entries in Data before saving, or values in Document to
// change the response after saving. A Hook returns only an error.
type HookContext struct {
	// Context carries cancellation, deadlines, and the active transaction.
	// Pass it to Local calls that must succeed or roll back with this operation.
	Context context.Context
	// Operation names the document operation, such as Create, Update, or Read,
	// not the hook phase. Shared hooks should check it before running write-only work.
	Operation operation.Kind
	// CollectionID is the collection's stable resource ID, not its slug.
	// It is empty for a global operation.
	CollectionID schema.StableID
	// GlobalID is the global's stable resource ID, not its slug.
	// It is empty for a collection operation.
	GlobalID schema.StableID
	// Actor is the authenticated document, or nil for an anonymous operation.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Data contains values at this phase: submitted input in BeforeValidate,
	// then the completed document in BeforeChange and BeforeOperation on writes.
	// Change map entries to change stored values; assigning a new map to Data
	// does not replace the engine's values. Changes after saving are not persisted.
	Data store.Values
	// Document is the response document when available. AfterRead receives each
	// document in a list separately. Changes to Document.Values affect the response,
	// not stored data, and remain subject to field read access rules.
	Document *store.Document
	// Original is a detached copy of the saved document before an update, delete,
	// or duplication. Changing it does not change storage.
	Original *store.Document
	// Local performs nested reads and writes through the normal operation engine.
	// Pass Context to reuse the transaction, and pass Actor, ActorCollection, and
	// locale options explicitly when the nested operation represents the same user.
	// Standalone reads reject nested writes; AfterCommit starts new transactions.
	Local *LocalAPI
	// Error is set only for after-error hooks and preserves the original failure.
	// Returning nil from AfterError does not make the failed operation succeed.
	Error error
	// Locale is the selected content language. Write hooks use the write locale;
	// AfterRead uses the response locale, whose values may include fallback text.
	Locale schema.LocaleCode
	// AllLocales reports whether localized values contain maps keyed by locale.
	// It describes this hook's values, which may differ from the requested response.
	AllLocales bool
}

// Hook runs during a collection or global lifecycle phase. Its context supplies
// the values and local API for that phase. Return nil to continue or an error to
// stop; an error before commit rolls back the transaction. AfterCommit errors
// report a failed side effect after the document has already committed.
type Hook func(HookContext) error

// AfterCommitEffect describes work whose document transaction has committed.
// A dispatcher calls Run to perform it. The function itself is not a durable job;
// use a registered task with serializable input for retryable background work.
type AfterCommitEffect struct {
	// Operation identifies the committed operation.
	Operation operation.Kind
	// CollectionID identifies the affected collection.
	CollectionID schema.StableID
	// GlobalID identifies the affected global, when applicable.
	GlobalID schema.StableID
	// DocumentID identifies the affected document; it is empty for an effect
	// without a single document, such as a list read.
	DocumentID string
	// Run calls the hook outside its original transaction. Pass the effect's
	// execution context to control cancellation and deadlines.
	Run func(context.Context) error
}

// AfterCommitDispatcher controls delivery of committed effects. Without a custom
// dispatcher, hooks run synchronously after commit. A dispatcher error is reported
// as a committed hook failure and cannot roll back the document.
type AfterCommitDispatcher interface {
	Dispatch(context.Context, AfterCommitEffect) error
}

// CollectionHooks configures resource-level callbacks through Collection.Hooks
// or Global.Hooks. Use BeforeChange to change values before a save, AfterRead to
// change a response without changing storage, and AfterCommit for effects that
// must run only after a successful commit. Each list runs in declaration order.
// Resource hooks run before field hooks in the same phase, except AfterCommit,
// where field hooks run first. Globals do not support BeforeDuplicate,
// BeforeDelete, or AfterDelete.
type CollectionHooks struct {
	// BeforeDuplicate runs after the source is access-checked and copied but before validation.
	BeforeDuplicate []Hook
	// BeforeValidate runs before field hooks and built-in checks. On updates,
	// Data may contain only the submitted fields. This shared phase also runs for
	// reads and deletes; check Operation when changing write input.
	BeforeValidate []Hook
	// BeforeChange runs after initial built-in validation for create, duplicate,
	// update, publish, and unpublish. Changes to Data are checked again afterward;
	// custom field validators run after the write hooks finish.
	BeforeChange []Hook
	// BeforeOperation runs before the store operation, including reads and deletes.
	// On writes it follows BeforeChange; changed values are validated before saving.
	BeforeOperation []Hook
	// BeforeRead runs before one or many documents are read.
	BeforeRead []Hook
	// BeforeDelete runs after the original document is loaded but before deletion.
	BeforeDelete []Hook
	// AfterChange runs after create, duplicate, update, publish, or unpublish has
	// saved the document, but before commit. Returning an error rolls back the save.
	AfterChange []Hook
	// AfterRead runs for each response document, including mutation responses,
	// after computed fields resolve and before field read access removes values.
	AfterRead []Hook
	// AfterDelete runs inside the transaction after deletion or trashing.
	AfterDelete []Hook
	// AfterOperation runs after the store operation, including reads and deletes,
	// but before response processing and commit. Errors can still roll back writes.
	AfterOperation []Hook
	// AfterError observes a failure in this resource through Error. It cannot
	// suppress that failure; its own error is added to the returned error.
	AfterError []Hook
	// AfterCommit runs after a successful commit, including read transactions.
	// Guard write-only effects with Operation. It runs outside the transaction;
	// failures cannot roll back saved data, and later effects still run.
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
	// AfterError observes failures after resource error hooks, including failures
	// before a collection or global could be identified. It cannot suppress them.
	AfterError []Hook
}

func (hooks RootHooks) clone() RootHooks {
	hooks.AfterError = append([]Hook(nil), hooks.AfterError...)
	return hooks
}
