package field

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Node is any field definition that can be placed in Fields. Authors normally
// create nodes with constructors such as Text, Select, Group, or Array and use
// their concrete fluent methods before adding them to a field list. The interface
// is sealed so Ridu can snapshot every supported field safely.
type Node interface {
	Name() string
	Kind() Kind
	graphSnapshot() View
}

// Fields is the ordered list assigned to ridu.Collection.Fields,
// ridu.Global.Fields, or a nested field constructor. Populate it with Nodes from
// this package's constructors. Ridu snapshots the caller-owned slice during
// construction, editing, and config resolution.
//
// For example:
//
//	fields := field.Fields{
//		field.Text("title").Required(),
//		field.Select("status", "draft", "published"),
//		field.Group("seo", field.Fields{
//			field.Textarea("description"),
//		}),
//	}
type Fields []Node

func (d View) graphSnapshot() View { return d }

// Snapshot captures even pointer-to-facade inputs by value. Invalid nil nodes
// become diagnostic definitions, so composition never panics on caller input.
func Snapshot(n Node) View {
	if n == nil || (reflect.ValueOf(n).Kind() == reflect.Pointer && reflect.ValueOf(n).IsNil()) {
		return View{nodeData: nodeData{issues: []Issue{{Code: "nil_field", Message: "field node must not be nil"}}}}
	}
	return n.graphSnapshot()
}

// Snapshot detaches the mutable slice and freezes pointer facade inputs.
func (f Fields) Snapshot() Fields {
	if f == nil {
		return nil
	}
	result := make(Fields, len(f))
	for i, n := range f {
		result[i] = Snapshot(n)
	}
	return result
}

// PolicySummary is descriptive configuration evidence, never callback identity
// or a dispatch plan. No function address or captured state is serialized.
type PolicySummary struct {
	LiveValidators int            `json:"liveValidators,omitempty"`
	Validators     int            `json:"validators,omitempty"`
	Hooks          map[string]int `json:"hooks,omitempty"`
	CreateAccess   bool           `json:"createAccess,omitempty"`
	ReadAccess     bool           `json:"readAccess,omitempty"`
	UpdateAccess   bool           `json:"updateAccess,omitempty"`
	Resolver       bool           `json:"resolver,omitempty"`
	DynamicDefault bool           `json:"dynamicDefault,omitempty"`
}

type policyBundle interface {
	summary() PolicySummary
	issues() []Issue
}
type typedPolicies[W, R any] struct {
	validators     []Validator[W]
	liveValidators []LiveValidator[W]
	hooks          Hooks[W]
	afterRead      []OutputTransform[R]
	resolver       Resolver[R]
	defaultFrom    DefaultFunc[W]
}

func (p typedPolicies[W, R]) summary() PolicySummary {
	h := p.hooks
	counts := map[string]int{"beforeDuplicate": len(h.BeforeDuplicate), "beforeValidate": len(h.BeforeValidate), "beforeChange": len(h.BeforeChange), "beforeOperation": len(h.BeforeOperation), "beforeDelete": len(h.BeforeDelete), "afterChange": len(h.AfterChange), "afterDelete": len(h.AfterDelete), "afterOperation": len(h.AfterOperation), "afterCommit": len(h.AfterCommit), "afterRead": len(p.afterRead)}
	for key, n := range counts {
		if n == 0 {
			delete(counts, key)
		}
	}
	return PolicySummary{LiveValidators: len(p.liveValidators), Validators: len(p.validators), Hooks: counts, Resolver: p.resolver != nil, DynamicDefault: p.defaultFrom != nil}
}

// graphPolicies is immutable after publication. Edits copy it, replacing only
// changed owned members; unchanged nodes and callback closures are shared.
type graphPolicies struct {
	access     Access
	behavior   policyBundle
	private    map[string]store.Value
	provenance []string
	namedTab   bool
	unnamedTab bool
}

func (d View) graphPolicy() graphPolicies {
	if d.graph == nil {
		return graphPolicies{}
	}
	return *d.graph
}
func (d View) withGraph(edit func(*graphPolicies)) View {
	p := d.graphPolicy()
	edit(&p)
	d.graph = &p
	return d
}
func policies[W, R any](d View) typedPolicies[W, R] {
	if p, ok := d.graphPolicy().behavior.(typedPolicies[W, R]); ok {
		return p
	}
	return typedPolicies[W, R]{}
}
func withPolicies[W, R any](d View, edit func(*typedPolicies[W, R])) View {
	p := policies[W, R](d)
	edit(&p)
	return d.withGraph(func(g *graphPolicies) { g.behavior = p })
}

// AdminPolicy returns a detached complete presentation policy draft.
func (d View) AdminPolicy() Admin    { return cloneAdmin(d.admin) }
func (d View) setAdmin(a Admin) View { d.admin = cloneAdmin(a); return d }

// AccessPolicy returns the immutable function group; closure captures remain author-owned.
func (d View) AccessPolicy() Access { return d.graphPolicy().access }

// BehaviorSummary describes the attached policies without exposing function addresses.
func (d View) BehaviorSummary() PolicySummary {
	g := d.graphPolicy()
	var s PolicySummary
	if g.behavior != nil {
		s = g.behavior.summary()
	}
	s.CreateAccess = g.access.Create != nil
	s.ReadAccess = g.access.Read != nil
	s.UpdateAccess = g.access.Update != nil
	return s
}

// HasBehavior identifies executable policies owned by this field.
func (d View) HasBehavior() bool {
	s := d.BehaviorSummary()
	return s.LiveValidators > 0 || s.Validators > 0 || len(s.Hooks) > 0 || s.CreateAccess || s.ReadAccess || s.UpdateAccess || s.Resolver || s.DynamicDefault
}

// Private returns immutable finite owner metadata, never manifest content.
func (d View) Private(namespace string) (store.Value, bool) {
	v, ok := d.graphPolicy().private[namespace]
	return v, ok
}
func (d View) setPrivate(namespace string, v store.Value) View {
	if namespace == "" {
		d.issues = append(slices.Clone(d.issues), Issue{Code: "invalid_private_namespace", Message: "private attachment namespace must not be empty"})
		return d
	}
	return d.withGraph(func(p *graphPolicies) {
		p.private = maps.Clone(p.private)
		if p.private == nil {
			p.private = map[string]store.Value{}
		}
		p.private[namespace] = v
	})
}

// Provenance returns declaration/transform sources for diagnostics.
func (d View) Provenance() []string { return slices.Clone(d.graphPolicy().provenance) }

// IsNamedTab distinguishes a stored object with tab presentation from layout.
func (d View) IsNamedTab() bool { return d.graphPolicy().namedTab }

// IsUnnamedTab distinguishes a single presentation section from a tab group.
func (d View) IsUnnamedTab() bool { return d.graphPolicy().unnamedTab }

// WithProvenance marks every published node as passing through a transform.
func (f Fields) WithProvenance(source string) Fields {
	out := f.Snapshot()
	for i, n := range out {
		d := Snapshot(n)
		for _, b := range d.Branches() {
			if b.Referenced {
				continue
			}
			d, _ = replaceBranch(d, b.Selector, b.Fields.WithProvenance(source))
		}
		out[i] = d.withGraph(func(p *graphPolicies) { p.provenance = append(slices.Clone(p.provenance), source) })
	}
	return out
}

func rename(d View, name string) View { d.name = name; return d }
func require(d View, values []bool) View {
	d.required = len(values) == 0 || values[len(values)-1]
	return d
}
func localize(d View, values []bool) View {
	d.localized = len(values) == 0 || values[len(values)-1]
	return d
}
func checkName(d View) error {
	if d.Kind() == KindRow || d.Kind() == KindTabs || d.Kind() == KindCollapsible {
		return nil
	}
	if !schema.IsValidFieldName(d.Name()) {
		return fmt.Errorf("invalid field name %q", d.Name())
	}
	return nil
}

// nodeView promotes read methods only. Its carrier is private so callers cannot
// replace the underlying kind or forge a mismatched typed facade.
type nodeView struct{ definition View }

func (v nodeView) Name() string                   { return v.definition.Name() }
func (v nodeView) Kind() Kind                     { return v.definition.Kind() }
func (v nodeView) graphSnapshot() View            { return v.definition }
func (v nodeView) AdminPolicy() Admin             { return v.definition.AdminPolicy() }
func (v nodeView) AccessPolicy() Access           { return v.definition.AccessPolicy() }
func (v nodeView) BehaviorSummary() PolicySummary { return v.definition.BehaviorSummary() }
func (v nodeView) Children() Fields               { return v.definition.fields.Snapshot() }
func (v nodeView) LabelText() string              { return v.definition.Label() }
func (v nodeView) IsRequired() bool               { return v.definition.Required() }
func (v nodeView) IsLocalized() bool              { return v.definition.Localized() }
func (v nodeView) Provenance() []string           { return v.definition.Provenance() }

// issues validates declaration shape only; it never invokes a callback.
func (p typedPolicies[W, R]) issues() []Issue {
	var issues []Issue
	check := func(path string, list any) {
		values := reflect.ValueOf(list)
		for i := 0; i < values.Len(); i++ {
			if values.Index(i).IsNil() {
				issues = append(issues, Issue{Code: "nil_field_callback", Path: fmt.Sprintf("%s[%d]", path, i), Message: "field callback must not be nil"})
			}
		}
	}
	check("validators", p.validators)
	check("liveValidators", p.liveValidators)
	h := reflect.ValueOf(p.hooks)
	ht := h.Type()
	for i := 0; i < h.NumField(); i++ {
		check("hooks."+ht.Field(i).Name, h.Field(i).Interface())
	}
	check("afterRead", p.afterRead)
	return issues
}

func (d View) metadataIssues() []Issue {
	var issues []Issue
	extensions := d.admin.Extensions
	for _, namespace := range slices.Sorted(maps.Keys(extensions)) {
		if namespace == "" {
			issues = append(issues, Issue{Code: "invalid_extension_namespace", Path: "admin.extensions", Message: "public extension namespace must not be empty"})
		}
		if _, err := json.Marshal(extensions[namespace]); err != nil {
			issues = append(issues, Issue{Code: "invalid_extension_value", Path: fmt.Sprintf("admin.extensions[%q]", namespace), Message: "public extension metadata must be finite JSON: " + err.Error()})
		}
	}
	return issues
}
