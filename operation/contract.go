// Package operation provides the values and context passed to field validators,
// hooks, defaults, and access rules. Use it to read nearby or previously saved fields,
// return validation messages, and replace a field value from a hook.
package operation

import (
	"context"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Kind identifies a document operation at resource and field callback boundaries.
type Kind string

const (
	Create          Kind = "create"
	Duplicate       Kind = "duplicate"
	Admin           Kind = "admin"
	Read            Kind = "read"
	ReadVersions    Kind = "read-versions"
	Update          Kind = "update"
	Delete          Kind = "delete"
	RestoreDeleted  Kind = "restore-deleted"
	DeletePermanent Kind = "delete-permanent"
	Publish         Kind = "publish"
	Unpublish       Kind = "unpublish"
	Unlock          Kind = "unlock"
)

// ID is the logical write value of a singular relationship. It is distinct
// from the potentially populated logical read value.
type ID string

// OccurrenceID is an opaque field-correlation token. A concrete token follows
// stable row keys across reordering and distinguishes independently localized
// values. Combine it with the resource and document IDs when correlating work
// across documents. Application callbacks normally use their value and views
// instead; do not parse the token or construct one from an array index.
type OccurrenceID string

// Actor identifies an authenticated document and its owning auth collection.
// An empty ID denotes an anonymous actor. Data is an immutable snapshot.
type Actor struct {
	ID         ID
	Collection schema.CollectionSlug
	Data       View
}

// Context describes one field callback at its current lifecycle phase.
// Root, Siblings and Prior are detached, immutable snapshots of field values;
// document metadata such as ID is available separately on Context.
// A callback changes its own value only through its return value.
type Context struct {
	// Context carries the operation's cancellation and deadline. Pass it to Local.
	Context context.Context
	// Operation is the enclosing document operation, such as Create or Update,
	// rather than the hook phase currently running.
	Operation Kind
	// CollectionID identifies the collection; it is empty for a global.
	// This is a framework resource identifier, not its authored slug.
	CollectionID schema.StableID
	// GlobalID identifies the global; it is empty for a collection.
	// This is a framework resource identifier, not its authored slug.
	GlobalID schema.StableID
	// OccurrenceID correlates this field value, including its enclosing stable
	// row keys and exact locale where localized. It is not a document ID.
	OccurrenceID OccurrenceID
	// SchemaOccurrenceID groups callbacks for the same configured field across
	// repeated rows and locales. Most application callbacks need neither token.
	SchemaOccurrenceID OccurrenceID
	// ID identifies the document involved in this phase. It can be empty before
	// a newly created document has been assigned an ID.
	ID ID
	// Actor identifies the authenticated document and owning auth collection.
	// Actor.ID is empty for an anonymous operation.
	Actor Actor
	// Locale is the selected content locale, or the exact translation when an
	// all-locales operation visits a localized value. It is empty without localization.
	// On ordinary reads it does not identify a fallback value's source locale.
	Locale schema.LocaleCode
	// AllLocales reports whether Root retains locale-keyed values. A callback
	// visiting one exact translation receives false even during an all-locales read.
	AllLocales bool
	// Root contains root field values at this phase: input during raw hooks,
	// the completed candidate during typed writes and validation, or response
	// values during reads. Read values may include requested population and fallback.
	Root View
	// Siblings contains the current enclosing object or repeated row, including
	// this field. For a root title it is the root; for seo.title it is seo;
	// for variants[_key=A].sku it is row A, regardless of that row's current index.
	Siblings View
	// Prior contains the previous persisted enclosing object or row, including
	// this field. Prior.String("title") reads the previous title, not the whole
	// previous document. Retained rows match by stable key rather than array index.
	// It is empty when no previous enclosing object exists, including create,
	// new rows and standalone reads. Localized writes use the exact prior locale;
	// fallback text never becomes a previously stored translation.
	Prior View
	// Local reads a known document under this callback's actor and exact locale,
	// reusing an active transaction. It does not grant nested write capabilities.
	Local Reader
}

// ValidationContext supplies the completed candidate to a field validator.
// The separate Value argument is this field's typed current value; Root and
// Siblings include retained update values and earlier write transformations.
// Prior remains the persisted enclosing object or row from before the operation.
// Opt-in advisory checks instead receive LiveValidationContext, whose input has
// not passed through defaults or save transformations.
type ValidationContext Context

// DefaultContext supplies the values available when an eligible omitted field
// is initialized. Root and Siblings are snapshots at that checkpoint, not a
// promise of complete, validated values or results from other dynamic defaults.
// Do not use one dynamic default as a dependency of another.
//
// Prior is the previously persisted enclosing object or row, matched by stable
// row identity. It is empty for a new nested scope. A duplicate root can retain
// the source document as Prior; copied rows with new keys have no prior row.
// Locale identifies the exact content locale being initialized; locale fallback is not persisted input. Operation
// can be Update when a new object, repeated row, or translation is initialized.
// ID can be empty before a new document receives its identifier.
//
// Context, Actor, resource identities and Local follow the shared Context
// contract. Local preserves authorization, exact-locale reads, the active
// transaction and cancellation; it does not grant nested write capabilities.
// Defaults run during eligible initialization, not on every field change.
type DefaultContext Context

// WriteContext supplies snapshots to raw and typed field transforms. Raw hooks
// see input at their phase and may receive omitted or malformed values. Typed
// write hooks see a completed candidate and their field's logical value.
type WriteContext Context

// ReadContext supplies the current response view. References may be populated,
// so the callback's logical read type can differ from its write type. Ordinary
// read views may contain locale fallback values; Context has no fallback-source
// map. Final field redaction still applies after response transforms.
type ReadContext Context

// EventContext is the view for field lifecycle observations. Observation
// callbacks do not return mutations to field or ambient document values.
type EventContext Context

// AccessContext supplies surrounding values to a field's boolean access rule.
// Write admission uses the candidate at its authorization checkpoint; read rules
// inspect the current response and decide whether this field may remain visible.
type AccessContext Context

// Reader looks up a known document for a field rule, validator, default or hook. Reads
// enforce the callback's actor and ordinary authorization, use its exact locale
// without fallback, and reuse its active transaction. Passing another context
// can add cancellation but cannot discard the original cancellation or transaction.
// Use a resource hook and LocalAPI when application work needs nested writes;
// field callbacks deliberately receive only the read capability they need.
// During live validation and its authorization checks, reads return stored
// values without lifecycle hooks, defaults, or computed output.
type Reader interface {
	FindByID(context.Context, schema.CollectionSlug, ID) (store.Document, error)
}

// View is an immutable snapshot of one object's field values. Lookup, Get and
// String accept direct child names, not dotted paths. Follow nested objects with
// Get("seo").Get("title"); iterate lists with Get("variants").Elements(). These
// reads share immutable values without copying maps or slices.
type View struct {
	object store.Value
}

// Snapshot detaches the supplied map through store.Object.
func Snapshot(values store.Values) View { return View{object: store.Object(values)} }

// Lookup reads a direct child and reports snapshot membership. During save
// callbacks, empty scalars in Root, Siblings and an existing Prior object normalize to Null, so membership
// does not prove the caller submitted that field. Inspect a raw hook's Value
// argument when omission and explicit null need different normalization.
// LiveValidationContext retains sparse readable snapshots; its Input view
// records submitted membership before retained persisted values are combined.
func (view View) Lookup(name string) (store.Value, bool) {
	return view.object.Lookup(name)
}

// Get reads a direct child, returning Null when it is absent. Use Lookup when
// membership in this snapshot matters. JSON and plugin values retain their own
// object shape rather than acquiring omitted scalar properties.
func (view View) Get(name string) store.Value {
	if value, exists := view.Lookup(name); exists {
		return value
	}
	return store.Null()
}

// String reads a typed sibling, root or prior value without a type assertion.
func (view View) String(name string) (string, bool) { return view.Get(name).StringValue() }

// Value carries an optional logical value. Its zero value is empty. For raw
// input, empty means omitted and Present(store.Null()) means explicit null.
// For typed scalars, empty is the portable empty state, not a durable absence
// encoding. T must follow its own ownership contract; this carrier does not
// deep-clone arbitrary mutable application values.
type Value[T any] struct {
	value   T
	present bool
}

// Present carries a logical value, including an explicit zero value.
func Present[T any](value T) Value[T] { return Value[T]{value: value, present: true} }

// Empty carries no logical value.
func Empty[T any]() Value[T] { return Value[T]{} }

// Get returns the logical value and whether it is present.
func (value Value[T]) Get() (T, bool) { return value.value, value.present }

// Change is an explicit keep-or-replace result. Its zero value means keep.
// Replacing with Empty clears the callback's own logical value; it does not
// create an external patch command or durable scalar removal representation.
type Change[T any] struct {
	value   Value[T]
	replace bool
}

// Keep leaves the callback's logical value unchanged.
func Keep[T any]() Change[T] { return Change[T]{} }

// Replace replaces the callback's own logical value.
func Replace[T any](value Value[T]) Change[T] { return Change[T]{value: value, replace: true} }

// Replacement returns the replacement value and whether replacement is asked.
func (change Change[T]) Replacement() (Value[T], bool) { return change.value, change.replace }

// Issue addresses the current field or a descendant relative to its candidate
// value. A zero Target selects the current field. Ridu supplies resource, field,
// locale, display-path and stable correlation information; callbacks never
// construct occurrence IDs.
type Issue struct {
	Code    string
	Message string
	Target  IssueTarget
}

// ReferenceOutput is the logical read value of a singular relationship. ID is
// available regardless of whether an authorized document was populated.
type ReferenceOutput struct {
	id       ID
	document store.Value
}

// Unpopulated constructs a reference without populated document output.
func Unpopulated(id ID) ReferenceOutput { return ReferenceOutput{id: id} }

// Populated snapshots the document and derives the reference ID from it,
// preventing contradictory caller-supplied ID/document pairs.
func Populated(document store.Document) ReferenceOutput {
	return ReferenceOutput{id: ID(document.ID), document: store.Populated(document)}
}

// ID returns the referenced document identifier.
func (output ReferenceOutput) ID() ID { return output.id }

// Document returns a detached document when population is present.
func (output ReferenceOutput) Document() (store.Document, bool) {
	return output.document.CopyDocument()
}
