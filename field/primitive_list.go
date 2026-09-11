package field

import (
	"encoding/json"
	"slices"

	"github.com/riducms/ridu/store"
)

// TextListField configures one ordered list of strings. It preserves duplicate
// values and has no per-item fields or persisted row identities.
type TextListField struct{ nodeView }

// TextList creates an ordered primitive list. Required demands a nonempty list;
// MinRows and MaxRows constrain its length independently of per-item constraints.
func TextList(name string) TextListField { return TextListField{nodeView{newNode(KindTextList, name)}} }

func (f TextListField) Rename(name string) TextListField { f.definition.name = name; return f }
func (f TextListField) Label(value string) TextListField { f.definition.label = value; return f }
func (f TextListField) LabelTranslations(values map[string]string) TextListField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f TextListField) Admin(value Admin) TextListField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f TextListField) EditAdmin(edit func(*Admin)) TextListField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f TextListField) Private(namespace string, value store.Value) TextListField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f TextListField) Access(value Access) TextListField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f TextListField) RestrictAccess(value Access) TextListField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f TextListField) Required(values ...bool) TextListField {
	f.definition = require(f.definition, values)
	return f
}
func (f TextListField) Localized(values ...bool) TextListField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f TextListField) Validate(value Validator[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f TextListField) ReplaceValidators(values ...Validator[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f TextListField) Hooks(value Hooks[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f TextListField) AppendHooks(value Hooks[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f TextListField) PrependHooks(value Hooks[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f TextListField) Validators() []Validator[[]string] {
	return slices.Clone(policies[[]string, []string](f.definition).validators)
}
func (f TextListField) HookPolicy() Hooks[[]string] {
	return cloneHooks(policies[[]string, []string](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f TextListField) ReplaceAfterRead(callbacks ...OutputTransform[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f TextListField) AfterRead(callbacks ...OutputTransform[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f TextListField) AfterReadHooks() []OutputTransform[[]string] {
	return slices.Clone(policies[[]string, []string](f.definition).afterRead)
}

// MinRows requires at least value items, including when the list is empty or null.
func (f TextListField) MinRows(value int) TextListField { f.definition.minRows = value; return f }

// MaxRows limits the number of items; zero means no configured maximum.
func (f TextListField) MaxRows(value int) TextListField { f.definition.maxRows = value; return f }

// Default sets a copied literal initial list. With no values it explicitly defaults
// to an empty list. It replaces any DefaultFrom callback.
func (f TextListField) Default(values ...string) TextListField {
	f.definition = setListDefault(f.definition, values)
	return f
}

// DefaultFrom sets a typed server default using the ordinary initialization lifecycle.
func (f TextListField) DefaultFrom(callback DefaultFunc[[]string]) TextListField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured server default without executing it.
func (f TextListField) DefaultCallback() DefaultFunc[[]string] {
	return policies[[]string, []string](f.definition).defaultFrom
}

// MinLength requires each string to contain at least value Unicode code points.
func (f TextListField) MinLength(value int) TextListField { f.definition.minLength = &value; return f }

// MaxLength limits each string's Unicode code point count.
func (f TextListField) MaxLength(value int) TextListField { f.definition.maxLength = &value; return f }

// NumberListField configures one ordered list of finite numbers. It preserves duplicate
// values and has no per-item fields or persisted row identities.
type NumberListField struct{ nodeView }

// NumberList creates an ordered primitive list. Required demands a nonempty list;
// MinRows and MaxRows constrain its length independently of per-item constraints.
func NumberList(name string) NumberListField {
	return NumberListField{nodeView{newNode(KindNumberList, name)}}
}

func (f NumberListField) Rename(name string) NumberListField { f.definition.name = name; return f }
func (f NumberListField) Label(value string) NumberListField { f.definition.label = value; return f }
func (f NumberListField) LabelTranslations(values map[string]string) NumberListField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f NumberListField) Admin(value Admin) NumberListField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f NumberListField) EditAdmin(edit func(*Admin)) NumberListField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f NumberListField) Private(namespace string, value store.Value) NumberListField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f NumberListField) Access(value Access) NumberListField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f NumberListField) RestrictAccess(value Access) NumberListField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f NumberListField) Required(values ...bool) NumberListField {
	f.definition = require(f.definition, values)
	return f
}
func (f NumberListField) Localized(values ...bool) NumberListField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f NumberListField) Validate(value Validator[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f NumberListField) ReplaceValidators(values ...Validator[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f NumberListField) Hooks(value Hooks[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f NumberListField) AppendHooks(value Hooks[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f NumberListField) PrependHooks(value Hooks[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f NumberListField) Validators() []Validator[[]float64] {
	return slices.Clone(policies[[]float64, []float64](f.definition).validators)
}
func (f NumberListField) HookPolicy() Hooks[[]float64] {
	return cloneHooks(policies[[]float64, []float64](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f NumberListField) ReplaceAfterRead(callbacks ...OutputTransform[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f NumberListField) AfterRead(callbacks ...OutputTransform[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f NumberListField) AfterReadHooks() []OutputTransform[[]float64] {
	return slices.Clone(policies[[]float64, []float64](f.definition).afterRead)
}

// MinRows requires at least value items, including when the list is empty or null.
func (f NumberListField) MinRows(value int) NumberListField { f.definition.minRows = value; return f }

// MaxRows limits the number of items; zero means no configured maximum.
func (f NumberListField) MaxRows(value int) NumberListField { f.definition.maxRows = value; return f }

// Default sets a copied literal initial list. With no values it explicitly defaults
// to an empty list. It replaces any DefaultFrom callback.
func (f NumberListField) Default(values ...float64) NumberListField {
	f.definition = setListDefault(f.definition, values)
	return f
}

// DefaultFrom sets a typed server default using the ordinary initialization lifecycle.
func (f NumberListField) DefaultFrom(callback DefaultFunc[[]float64]) NumberListField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured server default without executing it.
func (f NumberListField) DefaultCallback() DefaultFunc[[]float64] {
	return policies[[]float64, []float64](f.definition).defaultFrom
}

// Min sets the inclusive lower bound for every number.
func (f NumberListField) Min(value float64) NumberListField { f.definition.minimum = &value; return f }

// Max sets the inclusive upper bound for every number.
func (f NumberListField) Max(value float64) NumberListField { f.definition.maximum = &value; return f }

func setListDefault[T string | float64](d View, values []T) View {
	d = clearDefaultSettings(d)
	d = withPolicies[[]T, []T](d, func(p *typedPolicies[[]T, []T]) { p.defaultFrom = nil })
	// Always encode a list, including an explicitly supplied nil or empty slice.
	copied := append([]T{}, values...)
	encoded, err := json.Marshal(copied)
	if err != nil {
		d.issues = append(d.issues, Issue{Code: "invalid_default", Path: "default", Message: "list defaults must contain finite primitive values: " + err.Error()})
		return d
	}
	d.defaultValue = &DefaultValue{kind: DefaultList, text: string(encoded)}
	return d
}
