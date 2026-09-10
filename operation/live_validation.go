package operation

import (
	"context"

	"github.com/riducms/ridu/schema"
)

// LiveValidationContext describes an advisory check of unsaved, possibly
// incomplete input. It is not a completed save candidate: Ridu has run neither
// defaults nor save transforms. Validate still runs independently when saving.
// Checks should be repeatable, read-only and cooperate with cancellation.
type LiveValidationContext struct {
	// Context carries the original request deadline and cancellation.
	Context context.Context
	// Operation is Create for a new collection document, or Update for document
	// edits and globals (including a global's first save).
	Operation Kind
	// CollectionID or GlobalID identifies the owning resource, not its document.
	CollectionID schema.StableID
	GlobalID     schema.StableID
	// ID is the saved document ID, or empty for a new collection document.
	ID ID
	// Actor is the authenticated document and its auth collection.
	Actor Actor
	// Locale selects one exact translation. Live checks never use fallback values
	// as persisted or submitted data and do not accept all-locales requests.
	Locale schema.LocaleCode
	// Root contains readable unsaved document values with omitted update data
	// retained from storage. In a detached embedded editor it is the parent
	// document before Apply; the detached payload is available through Siblings.
	Root View
	// Siblings contains the readable current enclosing object or repeated row.
	// Retained update fields are included, without defaults or write hooks.
	Siblings View
	// Prior is the readable persisted enclosing object or row in this exact
	// locale. Repeated rows match by stable key and Block case, never by index.
	// It is empty for new objects, rows, embedded items and translations.
	Prior View
	// Input contains the submitted enclosing object before retention. Lookup
	// distinguishes omitted properties from explicitly submitted null values.
	// The separate typed Value argument describes the current value after retention.
	Input View
	// Local reads an authorized document in the same read-only transaction, actor
	// and exact locale. It runs no document lifecycle hooks, defaults or computed
	// outputs, and grants no writes. Original cancellation cannot be replaced.
	Local Reader
}
