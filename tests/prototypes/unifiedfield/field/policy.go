// Package field is isolated Gate 1 authoring scaffolding, not the Ridu field API.
// It stores representative declarations and never resolves or executes them.
package field

import (
	"maps"
	"slices"

	"example.com/ridu-gate1/operation"
	"github.com/riducms/ridu/store"
)

type Validator[T any] func(operation.ValidationContext, operation.Value[T]) ([]operation.Issue, error)
type RawTransform func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error)
type Transform[T any] func(operation.WriteContext, operation.Value[T]) (operation.Change[T], error)
type OutputTransform[T any] func(operation.ReadContext, operation.Value[T]) (operation.Change[T], error)
type AccessRule func(operation.AccessContext) (bool, error)

// Hooks samples two existing write phases; it does not define the complete
// replacement lifecycle. Typed output deliberately lives in ReadHooks instead.
type Hooks[T any] struct {
	BeforeValidate []RawTransform
	BeforeChange   []Transform[T]
}

type ReadHooks[T any] struct {
	AfterRead []OutputTransform[T]
}

func cloneHooks[T any](h Hooks[T]) Hooks[T] {
	h.BeforeValidate = slices.Clone(h.BeforeValidate)
	h.BeforeChange = slices.Clone(h.BeforeChange)
	return h
}

func appendHooks[T any](base, extra Hooks[T]) Hooks[T] {
	base = cloneHooks(base)
	base.BeforeValidate = append(base.BeforeValidate, extra.BeforeValidate...)
	base.BeforeChange = append(base.BeforeChange, extra.BeforeChange...)
	return base
}

func cloneReadHooks[T any](h ReadHooks[T]) ReadHooks[T] {
	h.AfterRead = slices.Clone(h.AfterRead)
	return h
}

// Access replaces the entire policy group when passed to a field's Access
// method. A nil member is unspecified. RestrictAccess conjoins only specified
// members, preserving the other operations.
type Access struct {
	Create AccessRule
	Read   AccessRule
	Update AccessRule
}

func restrictAccess(base, extra Access) Access {
	return Access{Create: and(base.Create, extra.Create), Read: and(base.Read, extra.Read), Update: and(base.Update, extra.Update)}
}

func and(base, extra AccessRule) AccessRule {
	if extra == nil {
		return base
	}
	if base == nil {
		return extra
	}
	return func(c operation.AccessContext) (bool, error) {
		allowed, err := base(c)
		if err != nil || !allowed {
			return allowed, err
		}
		return extra(c)
	}
}

// ComponentRef avoids a constructor/type name collision with Component.
// Config uses the existing immutable finite value, not arbitrary mutable props.
type ComponentRef struct {
	Key    string
	Config store.Value
}

func Component(key string, config store.Value) ComponentRef {
	return ComponentRef{Key: key, Config: config}
}

// Admin is a detached policy input/draft. Setters snapshot both map containers;
// store.Value already owns its nested containers. Zero members in Admin replace
// the previous settings; EditAdmin preserves members the callback does not edit.
type Admin struct {
	Description string
	ReadOnly    bool
	Editor      ComponentRef
	Labels      map[string]string
	Extensions  map[string]store.Value
	VisibleWhen Condition
}

func cloneAdmin(a Admin) Admin {
	a.Labels = maps.Clone(a.Labels)
	a.Extensions = maps.Clone(a.Extensions)
	return a
}

func editAdmin(a Admin, edit func(*Admin)) Admin {
	draft := cloneAdmin(a)
	edit(&draft)
	// Snapshot again: the author may retain the draft or inject a borrowed map.
	return cloneAdmin(draft)
}
