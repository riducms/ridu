package field

import (
	"slices"

	"example.com/ridu-gate1/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type Kind string

const (
	TextKind         Kind = "text"
	NumberKind       Kind = "number"
	RelationshipKind Kind = "relationship"
	GroupKind        Kind = "group"
)

// Node erases a facade only at composition. It exposes graph identity and
// children, never fictitious scalar methods. The private snapshot also freezes
// pointer-to-facade inputs into values when entering a Fields container.
type Node interface {
	Name() string
	Kind() Kind
	Children() Fields
	snapshot() Node
}

// Fields is a caller-owned heterogeneous slice, not itself an immutable value.
// Group and graph edits snapshot it. Immutable nodes can safely share backing.
type Fields []Node

func cloneFields(fields Fields) Fields {
	cloned := make(Fields, len(fields))
	for i, node := range fields {
		cloned[i] = node.snapshot()
	}
	return cloned
}

// This unexported attachment is deliberately invisible to public graph editors.
// It stands in for an owner-defined private plugin attachment in ownership tests;
// no general opaque-state cloning or metadata framework is introduced.
type privateAttachment struct {
	value    store.Value
	callback func() string
}

// Only read methods are promoted from nodeData. Fluent setters are concrete
// forwarding methods: promoting a base-returning setter would erase the facade.
type nodeData struct {
	name     string
	label    string
	required bool
	admin    Admin
	access   Access
	private  map[string]privateAttachment
}

func (d nodeData) Name() string         { return d.name }
func (d nodeData) LabelText() string    { return d.label }
func (d nodeData) IsRequired() bool     { return d.required }
func (d nodeData) AdminPolicy() Admin   { return cloneAdmin(d.admin) }
func (d nodeData) AccessPolicy() Access { return d.access }
func (d nodeData) Children() Fields     { return nil }

type TextField struct {
	nodeData
	maxLength  int
	validators []Validator[string]
	hooks      Hooks[string]
	readHooks  ReadHooks[string]
}

func Text(name string) TextField                   { return TextField{nodeData: nodeData{name: name}} }
func (f TextField) Kind() Kind                     { return TextKind }
func (f TextField) snapshot() Node                 { return f }
func (f TextField) Required() TextField            { f.required = true; return f }
func (f TextField) Label(label string) TextField   { f.label = label; return f }
func (f TextField) Rename(name string) TextField   { f.name = name; return f }
func (f TextField) MaxLength(length int) TextField { f.maxLength = length; return f }
func (f TextField) LengthLimit() int               { return f.maxLength }
func (f TextField) Admin(admin Admin) TextField    { f.admin = cloneAdmin(admin); return f }
func (f TextField) EditAdmin(edit func(*Admin)) TextField {
	f.admin = editAdmin(f.admin, edit)
	return f
}
func (f TextField) Access(access Access) TextField { f.access = access; return f }
func (f TextField) RestrictAccess(access Access) TextField {
	f.access = restrictAccess(f.access, access)
	return f
}
func (f TextField) Validate(validator Validator[string]) TextField {
	f.validators = append(slices.Clone(f.validators), validator)
	return f
}
func (f TextField) Validators() []Validator[string]     { return slices.Clone(f.validators) }
func (f TextField) Hooks(hooks Hooks[string]) TextField { f.hooks = cloneHooks(hooks); return f }
func (f TextField) AppendHooks(hooks Hooks[string]) TextField {
	f.hooks = appendHooks(f.hooks, hooks)
	return f
}
func (f TextField) HookPolicy() Hooks[string] { return cloneHooks(f.hooks) }
func (f TextField) ReadHooks(hooks ReadHooks[string]) TextField {
	f.readHooks = cloneReadHooks(hooks)
	return f
}
func (f TextField) ReadHookPolicy() ReadHooks[string] { return cloneReadHooks(f.readHooks) }

// NumberField exists only to prove a second specialized scalar method set and
// heterogeneous composition; no number codec or complete number API is added.
type NumberField struct {
	nodeData
	minimum float64
}

func Number(name string) NumberField                  { return NumberField{nodeData: nodeData{name: name}} }
func (f NumberField) Kind() Kind                      { return NumberKind }
func (f NumberField) snapshot() Node                  { return f }
func (f NumberField) Required() NumberField           { f.required = true; return f }
func (f NumberField) Label(label string) NumberField  { f.label = label; return f }
func (f NumberField) Min(minimum float64) NumberField { f.minimum = minimum; return f }
func (f NumberField) Minimum() float64                { return f.minimum }

// Relationship writes are IDs; read callbacks can instead see a populated
// reference. Neither signature requires generated application document types.
type RelationshipField struct {
	nodeData
	target    schema.CollectionSlug
	hooks     Hooks[operation.ID]
	readHooks ReadHooks[operation.ReferenceOutput]
}

func Relationship(name string, target schema.CollectionSlug) RelationshipField {
	return RelationshipField{nodeData: nodeData{name: name}, target: target}
}
func (f RelationshipField) Kind() Kind                             { return RelationshipKind }
func (f RelationshipField) snapshot() Node                         { return f }
func (f RelationshipField) Required() RelationshipField            { f.required = true; return f }
func (f RelationshipField) Label(label string) RelationshipField   { f.label = label; return f }
func (f RelationshipField) Target() schema.CollectionSlug          { return f.target }
func (f RelationshipField) Access(access Access) RelationshipField { f.access = access; return f }
func (f RelationshipField) Hooks(hooks Hooks[operation.ID]) RelationshipField {
	f.hooks = cloneHooks(hooks)
	return f
}
func (f RelationshipField) HookPolicy() Hooks[operation.ID] { return cloneHooks(f.hooks) }
func (f RelationshipField) ReadHooks(hooks ReadHooks[operation.ReferenceOutput]) RelationshipField {
	f.readHooks = cloneReadHooks(hooks)
	return f
}
func (f RelationshipField) ReadHookPolicy() ReadHooks[operation.ReferenceOutput] {
	return cloneReadHooks(f.readHooks)
}

type GroupField struct {
	nodeData
	children Fields
	hooks    Hooks[store.Value]
}

func Group(name string, children Fields) GroupField {
	return GroupField{nodeData: nodeData{name: name}, children: cloneFields(children)}
}
func (f GroupField) Kind() Kind                    { return GroupKind }
func (f GroupField) snapshot() Node                { return f }
func (f GroupField) Required() GroupField          { f.required = true; return f }
func (f GroupField) Label(label string) GroupField { f.label = label; return f }
func (f GroupField) Rename(name string) GroupField { f.name = name; return f }
func (f GroupField) Admin(admin Admin) GroupField  { f.admin = cloneAdmin(admin); return f }
func (f GroupField) EditAdmin(edit func(*Admin)) GroupField {
	f.admin = editAdmin(f.admin, edit)
	return f
}
func (f GroupField) Access(access Access) GroupField { f.access = access; return f }
func (f GroupField) RestrictAccess(access Access) GroupField {
	f.access = restrictAccess(f.access, access)
	return f
}
func (f GroupField) Hooks(hooks Hooks[store.Value]) GroupField { f.hooks = cloneHooks(hooks); return f }
func (f GroupField) HookPolicy() Hooks[store.Value]            { return cloneHooks(f.hooks) }
func (f GroupField) Children() Fields                          { return cloneFields(f.children) }
func (f GroupField) EditChildren(edit func(*ChildrenDraft) error) (GroupField, error) {
	children, err := f.children.Edit(edit)
	if err != nil {
		return f, err
	}
	f.children = children
	return f, nil
}
