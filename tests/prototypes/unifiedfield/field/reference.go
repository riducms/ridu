package field

import "github.com/riducms/ridu/store"

type ReferenceScope string

const (
	SiblingScope ReferenceScope = "sibling"
	RootScope    ReferenceScope = "root"
)

// Reference is an authoring selector, not an occurrence or runtime path.
// This probe supports one local name only, with no parsing or resolution.
type Reference struct {
	scope ReferenceScope
	name  string
}

func Sibling(name string) Reference       { return Reference{scope: SiblingScope, name: name} }
func Root(name string) Reference          { return Reference{scope: RootScope, name: name} }
func (r Reference) Scope() ReferenceScope { return r.scope }
func (r Reference) Name() string          { return r.name }

// Condition only proves that policies can carry a symbolic reference and an
// existing finite operand. There is deliberately no evaluator or binder.
type Condition struct {
	reference Reference
	value     store.Value
}

func Eq(reference Reference, value store.Value) Condition {
	return Condition{reference: reference, value: value}
}
func (c Condition) Reference() Reference { return c.reference }
func (c Condition) Operand() store.Value { return c.value }
