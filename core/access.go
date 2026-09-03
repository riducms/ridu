package core

import (
	"context"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// FieldAccessContext is evaluated inside the operation transaction for one
// authored field path. Returning false prevents writes or redacts reads.
type FieldAccessContext struct {
	// Context is the request-scoped cancellation and deadline context.
	Context context.Context
	// Operation is the create, read, update, or delete being evaluated.
	Operation Operation
	// CollectionID is the stable identity of the containing collection.
	CollectionID schema.StableID
	// GlobalID is the stable identity of the containing global, when applicable.
	GlobalID schema.StableID
	// ID is the current document ID. It is empty during creation.
	ID string
	// Path is the authored path of the field being evaluated.
	Path string
	// RuntimePath identifies the concrete value occurrence, including array or
	// block indexes. It equals Path for non-repeating fields.
	RuntimePath string
	// Actor is the authenticated document, or nil for an anonymous request.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Data contains incoming values for a write operation.
	Data store.Values
	// Value is the submitted value for a write or stored value for a read.
	Value store.Value
	// SiblingData contains the nearest containing object's values. Mutating this
	// detached snapshot does not mutate the operation input.
	SiblingData store.Values
	// Document is the existing or result document when available.
	Document *store.Document
	// Original is the persisted document before an update when available.
	Original *store.Document
	// Local exposes nested operations through the same transaction and access
	// pipeline. Rules must avoid recursively invoking themselves without a guard.
	Local *LocalAPI
	// Locale is the selected content locale. It is empty only when localization is disabled.
	Locale schema.LocaleCode
	// AllLocales reports that locale-keyed values were requested.
	AllLocales bool
}

// FieldAccessRule authorizes one field operation; false redacts reads and rejects writes.
type FieldAccessRule func(FieldAccessContext) (bool, error)

// FieldAccess groups independently configurable field-level rules.
type FieldAccess struct {
	// Create controls whether the field may be supplied during creation.
	Create FieldAccessRule
	// Read controls whether the field is visible in returned documents.
	Read FieldAccessRule
	// Update controls whether the field may be changed.
	Update FieldAccessRule
}

// AccessContext is the stable, transaction-scoped request context supplied to
// collection access rules.
type AccessContext struct {
	// Context is the request-scoped cancellation and deadline context.
	Context context.Context
	// Operation is the collection operation being evaluated.
	Operation Operation
	// CollectionID is the stable identity of the target collection.
	CollectionID schema.StableID
	// GlobalID is the stable identity of the target global, when applicable.
	GlobalID schema.StableID
	// ID is the requested document ID. It is empty for creates and list reads.
	ID string
	// Actor is the authenticated document, or nil for an anonymous request.
	Actor *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	// Data is a detached snapshot of incoming create or update values.
	Data store.Values
	// Local exposes nested operations through the same transaction and access
	// pipeline. Rules must avoid recursively invoking themselves without a guard.
	Local      *LocalAPI
	Locale     schema.LocaleCode
	AllLocales bool
}

// AccessRule returns an allow, deny, or filtered decision.
type AccessRule func(AccessContext) (AccessDecision, error)

// AccessDecisionKind identifies the result of an access rule.
type AccessDecisionKind string

const (
	AccessAllow AccessDecisionKind = "allow"
	AccessDeny  AccessDecisionKind = "deny"
	AccessWhere AccessDecisionKind = "where"
)

// AccessDecision is immutable. Filter returns a detached query expression
// snapshot when the decision is filtered.
type AccessDecision struct {
	kind       AccessDecisionKind
	expression query.Expression
}

// Allow authorizes an operation without a document predicate.
func Allow() AccessDecision { return AccessDecision{kind: AccessAllow} }

// Deny rejects an operation.
func Deny() AccessDecision { return AccessDecision{kind: AccessDeny} }

// Where requires the supplied predicate to remain attached to the atomic store
// operation.
func Where(expression query.Expression) AccessDecision {
	if expression == nil {
		panic("ridu.Where requires a non-nil query expression")
	}
	return AccessDecision{kind: AccessWhere, expression: expression}
}

// Kind reports whether the decision allows, denies, or filters the operation.
func (decision AccessDecision) Kind() AccessDecisionKind { return decision.kind }

// Filter returns a detached predicate for a filtered decision.
func (decision AccessDecision) Filter() (query.Node, bool) {
	if decision.kind != AccessWhere || decision.expression == nil {
		return query.Node{}, false
	}
	return decision.expression.Node(), true
}

// CollectionAccess groups the independently configurable collection rules.
type CollectionAccess struct {
	// Admin controls whether an authenticated user from this auth collection may
	// enter the framework admin. It accepts only Allow or Deny and defaults to
	// Allow when omitted.
	Admin AccessRule
	// Create controls document creation.
	Create AccessRule
	// Read controls individual and list reads and may return a Where decision.
	Read AccessRule
	// ReadVersions controls version-history reads and may return a Where
	// decision. When omitted, Read is used.
	ReadVersions AccessRule
	// Update controls document updates and may return a Where decision.
	Update AccessRule
	// Publish controls publishing and atomic published-document edits. When
	// omitted, Update is used so existing policies remain coherent.
	Publish AccessRule
	// Unpublish controls moving a published document back to draft. When
	// omitted, Update is used.
	Unpublish AccessRule
	// Delete controls document deletion and may return a Where decision.
	Delete AccessRule
	// Unlock controls takeover of another editor's active document lock. When
	// omitted, Update is used so existing collection policies remain coherent.
	Unlock AccessRule
}

// GlobalAccess groups the independently configurable singleton rules. Filtered
// decisions are applied atomically to the persisted singleton or version
// snapshot. A singleton's first update requires Allow because no persisted row
// exists for a filtered decision to authorize.
type GlobalAccess struct {
	// Read controls global reads and may return a Where decision.
	Read AccessRule
	// ReadVersions controls version-history reads and may return a Where decision.
	// When omitted, Read is used.
	ReadVersions AccessRule
	// Update controls global updates and may return a Where decision for an
	// existing singleton.
	Update AccessRule
	// Publish controls publishing and atomic published-global edits. When
	// omitted, Update is used.
	Publish AccessRule
	// Unpublish controls moving a published global back to draft. When omitted,
	// Update is used.
	Unpublish AccessRule
}
