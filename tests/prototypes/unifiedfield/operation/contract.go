// Package operation is isolated Gate-1 callback-contract scaffolding. It has no
// executor, transaction handle, resource policy, or application coordinator.
// Nothing in this package is wired into the production operation engine.
package operation

import (
	"context"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Kind uses the existing core.Operation vocabulary. These three values are the
// representative subset needed by this probe; no production type is moved.
type Kind string

const (
	Create Kind = "create"
	Read   Kind = "read"
	Update Kind = "update"
)

// ID is the logical write type of the prototype's singular relationship.
// It is deliberately distinct from a populated read value and from any.
type ID string

// Actor identifies both an authenticated document and its owning auth collection.
// An empty ID denotes an anonymous callback. Data is an immutable snapshot.
type Actor struct {
	ID         ID
	Collection schema.CollectionSlug
	Data       View
}

// Context is the smallest callback view used by this prototype. Root, Siblings,
// and Prior expose snapshots without a mutable operation document. A future
// private adapter supplies these views at the existing lifecycle checkpoints.
// Occurrence binding and schema-aware scalar normalization are not implemented.
type Context struct {
	Context      context.Context
	Operation    Kind
	CollectionID schema.StableID
	GlobalID     schema.StableID
	ID           ID
	Actor        Actor
	Locale       schema.LocaleCode
	Root         View
	Siblings     View
	Prior        View
	Local        Reader
}

// ValidationContext exposes the final-candidate callback view. This is a type
// boundary only; the prototype does not schedule or invoke validation.
type ValidationContext Context

// WriteContext is the raw-input and logical-write callback view.
type WriteContext Context

// ReadContext is separate because authorized output can be populated and have a
// different logical type from a write value.
type ReadContext Context

// AccessContext is a field authorization view, without collection predicates.
type AccessContext Context

// Reader is only the bound lookup exercised by the external validation probe.
// A future private adapter must bind actor, locale, access and the active
// transaction; callbacks cannot select privilege overrides. The interface does
// not establish execution or transaction behavior in this compile-only module.
type Reader interface {
	FindByID(context.Context, schema.CollectionSlug, ID) (store.Document, error)
}

// View is a prototype read-only snapshot over the existing finite store.Value
// vocabulary. It is not an alternate document or JSON model. It does not resolve
// field shapes or establish a schema-aware production callback boundary.
type View struct {
	object store.Value
}

// Snapshot detaches the supplied mutable map through store.Object.
func Snapshot(values store.Values) View { return View{object: store.Object(values)} }

// Get returns an immutable finite value. An absent key returns Null in this
// shape-agnostic prototype view. Production scalar normalization, sparse plugin
// data and structural presence require the later schema-aware adapter; this
// method is not evidence for that runtime behavior. Raw submitted presence is
// carried separately by Value[store.Value].
func (view View) Get(name string) store.Value {
	return view.object.Get(name)
}

// String reads a typed sibling/root/prior value without a type assertion.
func (view View) String(name string) (string, bool) { return view.Get(name).StringValue() }

// Value carries optional logical values. Its zero value is empty. For raw
// submitted input, empty means omitted while Present(store.Null()) means an
// explicit null; the finite store.Value also retains malformed logical input.
// For typed scalar callbacks, empty is the portable empty state, not a promise
// of durable Missing-versus-Null storage. This probe uses immutable T values;
// this generic carrier does not deep-clone arbitrary application-owned T.
type Value[T any] struct {
	value   T
	present bool
}

func Present[T any](value T) Value[T] { return Value[T]{value: value, present: true} }
func Empty[T any]() Value[T]          { return Value[T]{} }
func (value Value[T]) Get() (T, bool) { return value.value, value.present }

// Change has an explicit keep-or-replace contract. The zero value means keep.
// Replacing with Empty clears the callback's logical value; it does not create
// an external patch command or a durable scalar removal encoding.
type Change[T any] struct {
	value   Value[T]
	replace bool
}

func Keep[T any]() Change[T]                           { return Change[T]{} }
func Replace[T any](value Value[T]) Change[T]          { return Change[T]{value: value, replace: true} }
func (change Change[T]) Replacement() (Value[T], bool) { return change.value, change.replace }

// Issue is scoped to the current field. A zero Path addresses that field;
// nonempty paths address descendants relative to it. No resolved owner path is
// stored here. Query paths already provide validated, detached segments.
type Issue struct {
	Code    string
	Message string
	Path    query.Path
}

// ReferenceOutput is the logical read type of a singular relationship. The ID
// remains available whether or not the authorized document was populated.
// Populated data reuses store.Value rather than an application-generated type.
type ReferenceOutput struct {
	id       ID
	document store.Value
}

func Unpopulated(id ID) ReferenceOutput { return ReferenceOutput{id: id} }

// Populated snapshots the supplied document and takes its ID as the reference
// identity, preventing contradictory caller-supplied ID/document pairs.
func Populated(document store.Document) ReferenceOutput {
	return ReferenceOutput{id: ID(document.ID), document: store.Populated(document)}
}

func (output ReferenceOutput) ID() ID { return output.id }

// Document returns a detached document when population is present.
func (output ReferenceOutput) Document() (store.Document, bool) {
	return output.document.CopyDocument()
}
