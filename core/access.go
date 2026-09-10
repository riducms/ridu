package core

import (
	"context"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// AccessContext gives an AccessRule the operation being authorized, the current
// actor, submitted data, locale, and access-controlled Local API. It belongs to
// one operation; nested Local calls can share that operation's transaction when
// they use Context.
type AccessContext struct {
	// Context is the request-scoped cancellation and deadline context.
	Context context.Context
	// Operation is the collection operation being evaluated.
	Operation operation.Kind
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
	Local *LocalAPI
	// Locale is the exact content locale currently being authorized.
	Locale schema.LocaleCode
	// AllLocales reports that the caller requested every locale. Such a request
	// may evaluate the rule once for each configured Locale and requires
	// compatible decisions across them.
	AllLocales bool
}

// AccessRule authorizes one collection or global operation. Assign a rule to
// Collection.Access or Global.Access and return Allow, Deny, or Where. A
// returned error stops the operation.
type AccessRule func(AccessContext) (AccessDecision, error)

// AccessDecisionKind identifies the result of an access rule.
type AccessDecisionKind string

const (
	AccessAllow AccessDecisionKind = "allow"
	AccessDeny  AccessDecisionKind = "deny"
	AccessWhere AccessDecisionKind = "where"
)

// AccessDecision is the outcome returned by an AccessRule. Construct decisions
// with Allow, Deny, or Where; authors normally inspect them only in tests. The
// decision is immutable, and Filter returns a detached query expression snapshot.
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

// CollectionAccess configures authorization for each operation on a Collection.
// Assign it to Collection.Access. Omitted rules allow the operation unless the
// member below documents a fallback to another rule.
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

// GlobalAccess configures read and write authorization for a Global. Assign it
// to Global.Access. Omitted rules allow the operation unless the member below
// documents a fallback. Where decisions are applied atomically to a persisted
// singleton or version; the singleton's first update requires Allow because no
// row exists for a filter to authorize.
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
