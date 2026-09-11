package field

import (
	"encoding/json"
	"slices"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func newNode(kind Kind, name string) View { return View{nodeData: nodeData{kind: kind, name: name}} }

// TextField configures a Text, Code, or Textarea string field.
type TextField struct{ nodeView }

// Text creates a single-line text field.
func Text(name string) TextField                 { d := newNode(KindText, name); return TextField{nodeView{d}} }
func (f TextField) Rename(name string) TextField { f.definition.name = name; return f }
func (f TextField) Label(value string) TextField { f.definition.label = value; return f }
func (f TextField) LabelTranslations(values map[string]string) TextField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f TextField) Admin(value Admin) TextField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f TextField) EditAdmin(edit func(*Admin)) TextField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f TextField) Private(namespace string, value store.Value) TextField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f TextField) Access(value Access) TextField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f TextField) RestrictAccess(value Access) TextField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f TextField) Required(values ...bool) TextField {
	f.definition = require(f.definition, values)
	return f
}
func (f TextField) Localized(values ...bool) TextField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f TextField) Validate(value Validator[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f TextField) ReplaceValidators(values ...Validator[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f TextField) Hooks(value Hooks[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f TextField) AppendHooks(value Hooks[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f TextField) PrependHooks(value Hooks[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f TextField) Validators() []Validator[string] {
	return slices.Clone(policies[string, string](f.definition).validators)
}
func (f TextField) HookPolicy() Hooks[string] {
	return cloneHooks(policies[string, string](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f TextField) ReplaceAfterRead(callbacks ...OutputTransform[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f TextField) AfterRead(callbacks ...OutputTransform[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = append(slices.Clone(p.afterRead), callbacks...) })
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f TextField) AfterReadHooks() []OutputTransform[string] {
	return slices.Clone(policies[string, string](f.definition).afterRead)
}
func (f TextField) Unique(values ...bool) TextField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f TextField) Index(values ...bool) TextField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f TextField) MinLength(value int) TextField { f.definition.minLength = &value; return f }
func (f TextField) MaxLength(value int) TextField { f.definition.maxLength = &value; return f }
func (f TextField) Default(value string) TextField {
	f.definition = setDefault(f.definition, value)
	return f
}

// Code creates a text field with a code editor.
func Code(name string) TextField { d := newNode(KindCode, name); return TextField{nodeView{d}} }

// Textarea creates a multi-line text field.
func Textarea(name string) TextField {
	d := newNode(KindTextarea, name)
	return TextField{nodeView{d}}
}

// EmailField configures a field that stores and validates an email address.
type EmailField struct{ nodeView }

// Email creates a field that stores and validates an email address.
func Email(name string) EmailField                 { d := newNode(KindEmail, name); return EmailField{nodeView{d}} }
func (f EmailField) Rename(name string) EmailField { f.definition.name = name; return f }
func (f EmailField) Label(value string) EmailField { f.definition.label = value; return f }
func (f EmailField) LabelTranslations(values map[string]string) EmailField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f EmailField) Admin(value Admin) EmailField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f EmailField) EditAdmin(edit func(*Admin)) EmailField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f EmailField) Private(namespace string, value store.Value) EmailField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f EmailField) Access(value Access) EmailField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f EmailField) RestrictAccess(value Access) EmailField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f EmailField) Required(values ...bool) EmailField {
	f.definition = require(f.definition, values)
	return f
}
func (f EmailField) Localized(values ...bool) EmailField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f EmailField) Validate(value Validator[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f EmailField) ReplaceValidators(values ...Validator[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f EmailField) Hooks(value Hooks[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f EmailField) AppendHooks(value Hooks[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f EmailField) PrependHooks(value Hooks[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f EmailField) Validators() []Validator[string] {
	return slices.Clone(policies[string, string](f.definition).validators)
}
func (f EmailField) HookPolicy() Hooks[string] {
	return cloneHooks(policies[string, string](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f EmailField) ReplaceAfterRead(callbacks ...OutputTransform[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f EmailField) AfterRead(callbacks ...OutputTransform[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = append(slices.Clone(p.afterRead), callbacks...) })
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f EmailField) AfterReadHooks() []OutputTransform[string] {
	return slices.Clone(policies[string, string](f.definition).afterRead)
}
func (f EmailField) Unique(values ...bool) EmailField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f EmailField) Index(values ...bool) EmailField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f EmailField) Default(value string) EmailField {
	f.definition = setDefault(f.definition, value)
	return f
}

// DateField configures a field for a date, time, or timestamp.
type DateField struct{ nodeView }

// Date creates a field for a date, time, or timestamp.
func Date(name string) DateField                 { d := newNode(KindDate, name); return DateField{nodeView{d}} }
func (f DateField) Rename(name string) DateField { f.definition.name = name; return f }

// Format selects whether the field accepts a date, timestamp, or local time.
// DateOnly is the default. Admin display settings do not change the stored format.
func (f DateField) Format(value DateFormat) DateField {
	f.definition.dateFormat = value
	return f
}

func (f DateField) Label(value string) DateField { f.definition.label = value; return f }
func (f DateField) LabelTranslations(values map[string]string) DateField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f DateField) Admin(value Admin) DateField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f DateField) EditAdmin(edit func(*Admin)) DateField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f DateField) Private(namespace string, value store.Value) DateField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f DateField) Access(value Access) DateField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f DateField) RestrictAccess(value Access) DateField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f DateField) Required(values ...bool) DateField {
	f.definition = require(f.definition, values)
	return f
}
func (f DateField) Localized(values ...bool) DateField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f DateField) Validate(value Validator[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f DateField) ReplaceValidators(values ...Validator[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f DateField) Hooks(value Hooks[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f DateField) AppendHooks(value Hooks[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f DateField) PrependHooks(value Hooks[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f DateField) Validators() []Validator[string] {
	return slices.Clone(policies[string, string](f.definition).validators)
}
func (f DateField) HookPolicy() Hooks[string] {
	return cloneHooks(policies[string, string](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f DateField) ReplaceAfterRead(callbacks ...OutputTransform[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f DateField) AfterRead(callbacks ...OutputTransform[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = append(slices.Clone(p.afterRead), callbacks...) })
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f DateField) AfterReadHooks() []OutputTransform[string] {
	return slices.Clone(policies[string, string](f.definition).afterRead)
}
func (f DateField) Unique(values ...bool) DateField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f DateField) Index(values ...bool) DateField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f DateField) Default(value string) DateField {
	f.definition = setDefault(f.definition, value)
	return f
}

// NumberField configures a number field.
type NumberField struct{ nodeView }

// Number creates a number field.
func Number(name string) NumberField                 { d := newNode(KindNumber, name); return NumberField{nodeView{d}} }
func (f NumberField) Rename(name string) NumberField { f.definition.name = name; return f }
func (f NumberField) Label(value string) NumberField { f.definition.label = value; return f }
func (f NumberField) LabelTranslations(values map[string]string) NumberField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f NumberField) Admin(value Admin) NumberField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f NumberField) EditAdmin(edit func(*Admin)) NumberField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f NumberField) Private(namespace string, value store.Value) NumberField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f NumberField) Access(value Access) NumberField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f NumberField) RestrictAccess(value Access) NumberField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f NumberField) Required(values ...bool) NumberField {
	f.definition = require(f.definition, values)
	return f
}
func (f NumberField) Localized(values ...bool) NumberField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f NumberField) Validate(value Validator[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f NumberField) ReplaceValidators(values ...Validator[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f NumberField) Hooks(value Hooks[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f NumberField) AppendHooks(value Hooks[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f NumberField) PrependHooks(value Hooks[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f NumberField) Validators() []Validator[float64] {
	return slices.Clone(policies[float64, float64](f.definition).validators)
}
func (f NumberField) HookPolicy() Hooks[float64] {
	return cloneHooks(policies[float64, float64](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f NumberField) ReplaceAfterRead(callbacks ...OutputTransform[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f NumberField) AfterRead(callbacks ...OutputTransform[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f NumberField) AfterReadHooks() []OutputTransform[float64] {
	return slices.Clone(policies[float64, float64](f.definition).afterRead)
}
func (f NumberField) Unique(values ...bool) NumberField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f NumberField) Index(values ...bool) NumberField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f NumberField) Min(value float64) NumberField  { f.definition.minimum = &value; return f }
func (f NumberField) Max(value float64) NumberField  { f.definition.maximum = &value; return f }
func (f NumberField) Step(value float64) NumberField { f.definition.step = &value; return f }
func (f NumberField) Default(value float64) NumberField {
	f.definition = setDefault(f.definition, value)
	return f
}

// CheckboxField configures a true-or-false checkbox field.
type CheckboxField struct{ nodeView }

// Checkbox creates a true-or-false checkbox field.
func Checkbox(name string) CheckboxField {
	d := newNode(KindCheckbox, name)
	return CheckboxField{nodeView{d}}
}
func (f CheckboxField) Rename(name string) CheckboxField { f.definition.name = name; return f }
func (f CheckboxField) Label(value string) CheckboxField { f.definition.label = value; return f }
func (f CheckboxField) LabelTranslations(values map[string]string) CheckboxField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f CheckboxField) Admin(value Admin) CheckboxField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f CheckboxField) EditAdmin(edit func(*Admin)) CheckboxField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f CheckboxField) Private(namespace string, value store.Value) CheckboxField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f CheckboxField) Access(value Access) CheckboxField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f CheckboxField) RestrictAccess(value Access) CheckboxField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f CheckboxField) Required(values ...bool) CheckboxField {
	f.definition = require(f.definition, values)
	return f
}
func (f CheckboxField) Localized(values ...bool) CheckboxField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f CheckboxField) Validate(value Validator[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f CheckboxField) ReplaceValidators(values ...Validator[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f CheckboxField) Hooks(value Hooks[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f CheckboxField) AppendHooks(value Hooks[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f CheckboxField) PrependHooks(value Hooks[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f CheckboxField) Validators() []Validator[bool] {
	return slices.Clone(policies[bool, bool](f.definition).validators)
}
func (f CheckboxField) HookPolicy() Hooks[bool] {
	return cloneHooks(policies[bool, bool](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f CheckboxField) ReplaceAfterRead(callbacks ...OutputTransform[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f CheckboxField) AfterRead(callbacks ...OutputTransform[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.afterRead = append(slices.Clone(p.afterRead), callbacks...) })
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f CheckboxField) AfterReadHooks() []OutputTransform[bool] {
	return slices.Clone(policies[bool, bool](f.definition).afterRead)
}
func (f CheckboxField) Unique(values ...bool) CheckboxField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f CheckboxField) Index(values ...bool) CheckboxField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f CheckboxField) Default(value bool) CheckboxField {
	f.definition = setDefault(f.definition, value)
	return f
}

// JSONField configures a field for JSON data.
type JSONField struct{ nodeView }

// JSON creates a field for JSON data.
func JSON(name string) JSONField                 { d := newNode(KindJSON, name); return JSONField{nodeView{d}} }
func (f JSONField) Rename(name string) JSONField { f.definition.name = name; return f }
func (f JSONField) Label(value string) JSONField { f.definition.label = value; return f }
func (f JSONField) LabelTranslations(values map[string]string) JSONField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f JSONField) Admin(value Admin) JSONField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f JSONField) EditAdmin(edit func(*Admin)) JSONField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f JSONField) Private(namespace string, value store.Value) JSONField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f JSONField) Access(value Access) JSONField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f JSONField) RestrictAccess(value Access) JSONField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f JSONField) Required(values ...bool) JSONField {
	f.definition = require(f.definition, values)
	return f
}
func (f JSONField) Localized(values ...bool) JSONField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f JSONField) Validate(value Validator[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f JSONField) ReplaceValidators(values ...Validator[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f JSONField) Hooks(value Hooks[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f JSONField) AppendHooks(value Hooks[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f JSONField) PrependHooks(value Hooks[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f JSONField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f JSONField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f JSONField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f JSONField) AfterRead(callbacks ...OutputTransform[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f JSONField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}

// PointField configures a field for a geographic point.
type PointField struct{ nodeView }

// Point creates a field for a geographic point.
func Point(name string) PointField                 { d := newNode(KindPoint, name); return PointField{nodeView{d}} }
func (f PointField) Rename(name string) PointField { f.definition.name = name; return f }
func (f PointField) Label(value string) PointField { f.definition.label = value; return f }
func (f PointField) LabelTranslations(values map[string]string) PointField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f PointField) Admin(value Admin) PointField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f PointField) EditAdmin(edit func(*Admin)) PointField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f PointField) Private(namespace string, value store.Value) PointField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f PointField) Access(value Access) PointField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f PointField) RestrictAccess(value Access) PointField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f PointField) Required(values ...bool) PointField {
	f.definition = require(f.definition, values)
	return f
}
func (f PointField) Localized(values ...bool) PointField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f PointField) Validate(value Validator[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f PointField) ReplaceValidators(values ...Validator[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f PointField) Hooks(value Hooks[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f PointField) AppendHooks(value Hooks[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f PointField) PrependHooks(value Hooks[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f PointField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f PointField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f PointField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f PointField) AfterRead(callbacks ...OutputTransform[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f PointField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}

// SelectField configures a field for choosing one value from a fixed list.
type SelectField struct{ nodeView }

// Select creates a field for choosing one value from a fixed list.
// Values are stored exactly as given; display labels are generated during resolution.
// Use Options to configure custom labels or translations.
func Select(name string, values ...string) SelectField {
	d := newNode(KindSelect, name)
	d.options = optionsFromValues(values)
	return SelectField{nodeView{d}}
}
func (f SelectField) Rename(name string) SelectField { f.definition.name = name; return f }
func (f SelectField) Label(value string) SelectField { f.definition.label = value; return f }
func (f SelectField) LabelTranslations(values map[string]string) SelectField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f SelectField) Admin(value Admin) SelectField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f SelectField) EditAdmin(edit func(*Admin)) SelectField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f SelectField) Private(namespace string, value store.Value) SelectField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f SelectField) Access(value Access) SelectField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f SelectField) RestrictAccess(value Access) SelectField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f SelectField) Required(values ...bool) SelectField {
	f.definition = require(f.definition, values)
	return f
}
func (f SelectField) Localized(values ...bool) SelectField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f SelectField) Validate(value Validator[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f SelectField) ReplaceValidators(values ...Validator[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f SelectField) Hooks(value Hooks[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f SelectField) AppendHooks(value Hooks[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f SelectField) PrependHooks(value Hooks[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f SelectField) Validators() []Validator[string] {
	return slices.Clone(policies[string, string](f.definition).validators)
}
func (f SelectField) HookPolicy() Hooks[string] {
	return cloneHooks(policies[string, string](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f SelectField) ReplaceAfterRead(callbacks ...OutputTransform[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f SelectField) AfterRead(callbacks ...OutputTransform[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.afterRead = append(slices.Clone(p.afterRead), callbacks...) })
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f SelectField) AfterReadHooks() []OutputTransform[string] {
	return slices.Clone(policies[string, string](f.definition).afterRead)
}
func (f SelectField) Unique(values ...bool) SelectField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f SelectField) Index(values ...bool) SelectField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f SelectField) Default(value string) SelectField {
	f.definition = setDefault(f.definition, value)
	return f
}

// Options replaces the option list with options that can include labels and translations.
func (f SelectField) Options(values ...Option) SelectField {
	f.definition.options = cloneOptions(values)
	return f
}

// Radio creates a field for choosing one value with radio buttons.
// Values are stored exactly as given; display labels are generated during resolution.
// Use Options to configure custom labels or translations.
func Radio(name string, values ...string) SelectField {
	d := newNode(KindRadio, name)
	d.options = optionsFromValues(values)
	return SelectField{nodeView{d}}
}

// MultiSelectField configures a field for choosing several values from a fixed list.
type MultiSelectField struct{ nodeView }

// MultiSelect creates a field for choosing several values from a fixed list.
// Values are stored exactly as given; display labels are generated during resolution.
// Use Options to configure custom labels or translations.
func MultiSelect(name string, values ...string) MultiSelectField {
	d := newNode(KindSelect, name)
	d.options = optionsFromValues(values)
	d.selectMany = true
	return MultiSelectField{nodeView{d}}
}
func (f MultiSelectField) Rename(name string) MultiSelectField { f.definition.name = name; return f }
func (f MultiSelectField) Label(value string) MultiSelectField { f.definition.label = value; return f }
func (f MultiSelectField) LabelTranslations(values map[string]string) MultiSelectField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f MultiSelectField) Admin(value Admin) MultiSelectField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f MultiSelectField) EditAdmin(edit func(*Admin)) MultiSelectField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f MultiSelectField) Private(namespace string, value store.Value) MultiSelectField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f MultiSelectField) Access(value Access) MultiSelectField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f MultiSelectField) RestrictAccess(value Access) MultiSelectField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f MultiSelectField) Required(values ...bool) MultiSelectField {
	f.definition = require(f.definition, values)
	return f
}
func (f MultiSelectField) Localized(values ...bool) MultiSelectField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f MultiSelectField) Validate(value Validator[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.validators = append(slices.Clone(p.validators), value) })
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f MultiSelectField) ReplaceValidators(values ...Validator[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f MultiSelectField) Hooks(value Hooks[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f MultiSelectField) AppendHooks(value Hooks[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f MultiSelectField) PrependHooks(value Hooks[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f MultiSelectField) Validators() []Validator[[]string] {
	return slices.Clone(policies[[]string, []string](f.definition).validators)
}
func (f MultiSelectField) HookPolicy() Hooks[[]string] {
	return cloneHooks(policies[[]string, []string](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f MultiSelectField) ReplaceAfterRead(callbacks ...OutputTransform[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f MultiSelectField) AfterRead(callbacks ...OutputTransform[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f MultiSelectField) AfterReadHooks() []OutputTransform[[]string] {
	return slices.Clone(policies[[]string, []string](f.definition).afterRead)
}

// Options replaces the option list with options that can include labels and translations.
func (f MultiSelectField) Options(values ...Option) MultiSelectField {
	f.definition.options = cloneOptions(values)
	return f
}
func (f MultiSelectField) Default(values ...string) MultiSelectField {
	f.definition = clearDefaultSettings(f.definition)
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.defaultFrom = nil })
	f.definition.selectDefaults = slices.Clone(values)
	return f
}

// RelationshipField configures a link to one document in the target collection.
type RelationshipField struct{ nodeView }

// Relationship creates a link to one document in the target collection.
func Relationship(name string, target schema.CollectionSlug) RelationshipField {
	d := newNode(KindRelationship, name)
	d.relationTo = []string{string(target)}
	return RelationshipField{nodeView{d}}
}
func (f RelationshipField) Rename(name string) RelationshipField { f.definition.name = name; return f }
func (f RelationshipField) Label(value string) RelationshipField {
	f.definition.label = value
	return f
}
func (f RelationshipField) LabelTranslations(values map[string]string) RelationshipField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f RelationshipField) Admin(value Admin) RelationshipField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f RelationshipField) EditAdmin(edit func(*Admin)) RelationshipField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f RelationshipField) Private(namespace string, value store.Value) RelationshipField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f RelationshipField) Access(value Access) RelationshipField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f RelationshipField) RestrictAccess(value Access) RelationshipField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f RelationshipField) Required(values ...bool) RelationshipField {
	f.definition = require(f.definition, values)
	return f
}
func (f RelationshipField) Localized(values ...bool) RelationshipField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f RelationshipField) Validate(value Validator[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f RelationshipField) ReplaceValidators(values ...Validator[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f RelationshipField) Hooks(value Hooks[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f RelationshipField) AppendHooks(value Hooks[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f RelationshipField) PrependHooks(value Hooks[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.hooks = prependHooks(p.hooks, value)
	})
	return f
}
func (f RelationshipField) Validators() []Validator[operation.ID] {
	return slices.Clone(policies[operation.ID, operation.ReferenceOutput](f.definition).validators)
}
func (f RelationshipField) HookPolicy() Hooks[operation.ID] {
	return cloneHooks(policies[operation.ID, operation.ReferenceOutput](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f RelationshipField) ReplaceAfterRead(callbacks ...OutputTransform[operation.ReferenceOutput]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f RelationshipField) AfterRead(callbacks ...OutputTransform[operation.ReferenceOutput]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f RelationshipField) AfterReadHooks() []OutputTransform[operation.ReferenceOutput] {
	return slices.Clone(policies[operation.ID, operation.ReferenceOutput](f.definition).afterRead)
}
func (f RelationshipField) Unique(values ...bool) RelationshipField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f RelationshipField) Index(values ...bool) RelationshipField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f RelationshipField) OnDelete(value ReferenceDeleteAction) RelationshipField {
	f.definition.referenceDeleteAction = value
	return f
}
func (f RelationshipField) FilterOptionRules(rules ...RelationshipFilterRule) RelationshipField {
	f.definition.relationshipFilters = cloneRelationshipFilterRules(rules)
	return f
}

// RelationshipsField configures links to several documents in the target collection.
type RelationshipsField struct{ nodeView }

// Relationships creates links to several documents in the target collection.
func Relationships(name string, target schema.CollectionSlug) RelationshipsField {
	d := newNode(KindRelationship, name)
	d.relationTo = []string{string(target)}
	d.relationMany = true
	return RelationshipsField{nodeView{d}}
}
func (f RelationshipsField) Rename(name string) RelationshipsField {
	f.definition.name = name
	return f
}
func (f RelationshipsField) Label(value string) RelationshipsField {
	f.definition.label = value
	return f
}
func (f RelationshipsField) LabelTranslations(values map[string]string) RelationshipsField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f RelationshipsField) Admin(value Admin) RelationshipsField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f RelationshipsField) EditAdmin(edit func(*Admin)) RelationshipsField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f RelationshipsField) Private(namespace string, value store.Value) RelationshipsField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f RelationshipsField) Access(value Access) RelationshipsField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f RelationshipsField) RestrictAccess(value Access) RelationshipsField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f RelationshipsField) Required(values ...bool) RelationshipsField {
	f.definition = require(f.definition, values)
	return f
}
func (f RelationshipsField) Localized(values ...bool) RelationshipsField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f RelationshipsField) Validate(value Validator[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f RelationshipsField) ReplaceValidators(values ...Validator[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.validators = slices.Clone(values)
	})
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f RelationshipsField) Hooks(value Hooks[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f RelationshipsField) AppendHooks(value Hooks[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.hooks = appendHooks(p.hooks, value)
	})
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f RelationshipsField) PrependHooks(value Hooks[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.hooks = prependHooks(p.hooks, value)
	})
	return f
}
func (f RelationshipsField) Validators() []Validator[[]operation.ID] {
	return slices.Clone(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).validators)
}
func (f RelationshipsField) HookPolicy() Hooks[[]operation.ID] {
	return cloneHooks(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f RelationshipsField) ReplaceAfterRead(callbacks ...OutputTransform[[]operation.ReferenceOutput]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.afterRead = slices.Clone(callbacks)
	})
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f RelationshipsField) AfterRead(callbacks ...OutputTransform[[]operation.ReferenceOutput]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f RelationshipsField) AfterReadHooks() []OutputTransform[[]operation.ReferenceOutput] {
	return slices.Clone(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).afterRead)
}
func (f RelationshipsField) OnDelete(value ReferenceDeleteAction) RelationshipsField {
	f.definition.referenceDeleteAction = value
	return f
}
func (f RelationshipsField) FilterOptionRules(rules ...RelationshipFilterRule) RelationshipsField {
	f.definition.relationshipFilters = cloneRelationshipFilterRules(rules)
	return f
}

// UploadField configures a link to one file in the target upload collection.
type UploadField struct{ nodeView }

// Upload creates a link to one file in the target upload collection.
func Upload(name string, target schema.CollectionSlug) UploadField {
	d := newNode(KindUpload, name)
	d.relationTo = []string{string(target)}
	return UploadField{nodeView{d}}
}
func (f UploadField) Rename(name string) UploadField { f.definition.name = name; return f }
func (f UploadField) Label(value string) UploadField { f.definition.label = value; return f }
func (f UploadField) LabelTranslations(values map[string]string) UploadField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f UploadField) Admin(value Admin) UploadField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f UploadField) EditAdmin(edit func(*Admin)) UploadField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f UploadField) Private(namespace string, value store.Value) UploadField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f UploadField) Access(value Access) UploadField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f UploadField) RestrictAccess(value Access) UploadField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f UploadField) Required(values ...bool) UploadField {
	f.definition = require(f.definition, values)
	return f
}
func (f UploadField) Localized(values ...bool) UploadField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f UploadField) Validate(value Validator[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f UploadField) ReplaceValidators(values ...Validator[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f UploadField) Hooks(value Hooks[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f UploadField) AppendHooks(value Hooks[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f UploadField) PrependHooks(value Hooks[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.hooks = prependHooks(p.hooks, value)
	})
	return f
}
func (f UploadField) Validators() []Validator[operation.ID] {
	return slices.Clone(policies[operation.ID, operation.ReferenceOutput](f.definition).validators)
}
func (f UploadField) HookPolicy() Hooks[operation.ID] {
	return cloneHooks(policies[operation.ID, operation.ReferenceOutput](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f UploadField) ReplaceAfterRead(callbacks ...OutputTransform[operation.ReferenceOutput]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f UploadField) AfterRead(callbacks ...OutputTransform[operation.ReferenceOutput]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f UploadField) AfterReadHooks() []OutputTransform[operation.ReferenceOutput] {
	return slices.Clone(policies[operation.ID, operation.ReferenceOutput](f.definition).afterRead)
}
func (f UploadField) Unique(values ...bool) UploadField {
	f.definition.unique = len(values) == 0 || values[len(values)-1]
	return f
}
func (f UploadField) Index(values ...bool) UploadField {
	f.definition.index = len(values) == 0 || values[len(values)-1]
	return f
}
func (f UploadField) OnDelete(value ReferenceDeleteAction) UploadField {
	f.definition.referenceDeleteAction = value
	return f
}
func (f UploadField) FilterOptionRules(rules ...RelationshipFilterRule) UploadField {
	f.definition.relationshipFilters = cloneRelationshipFilterRules(rules)
	return f
}

// UploadsField configures links to several files in the target upload collection.
type UploadsField struct{ nodeView }

// Uploads creates links to several files in the target upload collection.
func Uploads(name string, target schema.CollectionSlug) UploadsField {
	d := newNode(KindUpload, name)
	d.relationTo = []string{string(target)}
	d.relationMany = true
	return UploadsField{nodeView{d}}
}
func (f UploadsField) Rename(name string) UploadsField { f.definition.name = name; return f }
func (f UploadsField) Label(value string) UploadsField { f.definition.label = value; return f }
func (f UploadsField) LabelTranslations(values map[string]string) UploadsField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f UploadsField) Admin(value Admin) UploadsField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f UploadsField) EditAdmin(edit func(*Admin)) UploadsField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f UploadsField) Private(namespace string, value store.Value) UploadsField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f UploadsField) Access(value Access) UploadsField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f UploadsField) RestrictAccess(value Access) UploadsField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f UploadsField) Required(values ...bool) UploadsField {
	f.definition = require(f.definition, values)
	return f
}
func (f UploadsField) Localized(values ...bool) UploadsField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f UploadsField) Validate(value Validator[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f UploadsField) ReplaceValidators(values ...Validator[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.validators = slices.Clone(values)
	})
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f UploadsField) Hooks(value Hooks[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f UploadsField) AppendHooks(value Hooks[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.hooks = appendHooks(p.hooks, value)
	})
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f UploadsField) PrependHooks(value Hooks[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.hooks = prependHooks(p.hooks, value)
	})
	return f
}
func (f UploadsField) Validators() []Validator[[]operation.ID] {
	return slices.Clone(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).validators)
}
func (f UploadsField) HookPolicy() Hooks[[]operation.ID] {
	return cloneHooks(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f UploadsField) ReplaceAfterRead(callbacks ...OutputTransform[[]operation.ReferenceOutput]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.afterRead = slices.Clone(callbacks)
	})
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f UploadsField) AfterRead(callbacks ...OutputTransform[[]operation.ReferenceOutput]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f UploadsField) AfterReadHooks() []OutputTransform[[]operation.ReferenceOutput] {
	return slices.Clone(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).afterRead)
}
func (f UploadsField) OnDelete(value ReferenceDeleteAction) UploadsField {
	f.definition.referenceDeleteAction = value
	return f
}
func (f UploadsField) FilterOptionRules(rules ...RelationshipFilterRule) UploadsField {
	f.definition.relationshipFilters = cloneRelationshipFilterRules(rules)
	return f
}

// PolymorphicRelationshipField configures a link to one document from any of the target collections.
type PolymorphicRelationshipField struct{ nodeView }

// PolymorphicRelationship creates a link to one document from any of the target collections.
func PolymorphicRelationship(name string, targets ...schema.CollectionSlug) PolymorphicRelationshipField {
	d := newNode(KindRelationship, name)
	d.relationTo = make([]string, len(targets))
	for i, v := range targets {
		d.relationTo[i] = string(v)
	}
	distinct := make(map[schema.CollectionSlug]bool, len(targets))
	for _, target := range targets {
		distinct[target] = true
	}
	if len(distinct) < 2 {
		d.issues = append(d.issues, Issue{Code: "invalid_polymorphic_targets", Path: "relationTo", Message: "polymorphic relationships require at least two distinct target collections"})
	}
	return PolymorphicRelationshipField{nodeView{d}}
}
func (f PolymorphicRelationshipField) Rename(name string) PolymorphicRelationshipField {
	f.definition.name = name
	return f
}
func (f PolymorphicRelationshipField) Label(value string) PolymorphicRelationshipField {
	f.definition.label = value
	return f
}
func (f PolymorphicRelationshipField) LabelTranslations(values map[string]string) PolymorphicRelationshipField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f PolymorphicRelationshipField) Admin(value Admin) PolymorphicRelationshipField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f PolymorphicRelationshipField) EditAdmin(edit func(*Admin)) PolymorphicRelationshipField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f PolymorphicRelationshipField) Private(namespace string, value store.Value) PolymorphicRelationshipField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f PolymorphicRelationshipField) Access(value Access) PolymorphicRelationshipField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f PolymorphicRelationshipField) RestrictAccess(value Access) PolymorphicRelationshipField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f PolymorphicRelationshipField) Required(values ...bool) PolymorphicRelationshipField {
	f.definition = require(f.definition, values)
	return f
}
func (f PolymorphicRelationshipField) Localized(values ...bool) PolymorphicRelationshipField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f PolymorphicRelationshipField) Validate(value Validator[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f PolymorphicRelationshipField) ReplaceValidators(values ...Validator[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f PolymorphicRelationshipField) Hooks(value Hooks[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f PolymorphicRelationshipField) AppendHooks(value Hooks[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f PolymorphicRelationshipField) PrependHooks(value Hooks[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f PolymorphicRelationshipField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f PolymorphicRelationshipField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f PolymorphicRelationshipField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f PolymorphicRelationshipField) AfterRead(callbacks ...OutputTransform[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f PolymorphicRelationshipField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f PolymorphicRelationshipField) OnDelete(value ReferenceDeleteAction) PolymorphicRelationshipField {
	f.definition.referenceDeleteAction = value
	return f
}
func (f PolymorphicRelationshipField) FilterOptionRules(rules ...RelationshipFilterRule) PolymorphicRelationshipField {
	f.definition.relationshipFilters = cloneRelationshipFilterRules(rules)
	return f
}

// PolymorphicRelationshipsField configures links to documents from any of the target collections.
type PolymorphicRelationshipsField struct{ nodeView }

// PolymorphicRelationships creates links to documents from any of the target collections.
func PolymorphicRelationships(name string, targets ...schema.CollectionSlug) PolymorphicRelationshipsField {
	d := newNode(KindRelationship, name)
	d.relationTo = make([]string, len(targets))
	for i, v := range targets {
		d.relationTo[i] = string(v)
	}
	d.relationMany = true
	distinct := make(map[schema.CollectionSlug]bool, len(targets))
	for _, target := range targets {
		distinct[target] = true
	}
	if len(distinct) < 2 {
		d.issues = append(d.issues, Issue{Code: "invalid_polymorphic_targets", Path: "relationTo", Message: "polymorphic relationships require at least two distinct target collections"})
	}
	return PolymorphicRelationshipsField{nodeView{d}}
}
func (f PolymorphicRelationshipsField) Rename(name string) PolymorphicRelationshipsField {
	f.definition.name = name
	return f
}
func (f PolymorphicRelationshipsField) Label(value string) PolymorphicRelationshipsField {
	f.definition.label = value
	return f
}
func (f PolymorphicRelationshipsField) LabelTranslations(values map[string]string) PolymorphicRelationshipsField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f PolymorphicRelationshipsField) Admin(value Admin) PolymorphicRelationshipsField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f PolymorphicRelationshipsField) EditAdmin(edit func(*Admin)) PolymorphicRelationshipsField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f PolymorphicRelationshipsField) Private(namespace string, value store.Value) PolymorphicRelationshipsField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f PolymorphicRelationshipsField) Access(value Access) PolymorphicRelationshipsField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f PolymorphicRelationshipsField) RestrictAccess(value Access) PolymorphicRelationshipsField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f PolymorphicRelationshipsField) Required(values ...bool) PolymorphicRelationshipsField {
	f.definition = require(f.definition, values)
	return f
}
func (f PolymorphicRelationshipsField) Localized(values ...bool) PolymorphicRelationshipsField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f PolymorphicRelationshipsField) Validate(value Validator[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f PolymorphicRelationshipsField) ReplaceValidators(values ...Validator[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f PolymorphicRelationshipsField) Hooks(value Hooks[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f PolymorphicRelationshipsField) AppendHooks(value Hooks[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f PolymorphicRelationshipsField) PrependHooks(value Hooks[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f PolymorphicRelationshipsField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f PolymorphicRelationshipsField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f PolymorphicRelationshipsField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f PolymorphicRelationshipsField) AfterRead(callbacks ...OutputTransform[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f PolymorphicRelationshipsField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f PolymorphicRelationshipsField) OnDelete(value ReferenceDeleteAction) PolymorphicRelationshipsField {
	f.definition.referenceDeleteAction = value
	return f
}
func (f PolymorphicRelationshipsField) FilterOptionRules(rules ...RelationshipFilterRule) PolymorphicRelationshipsField {
	f.definition.relationshipFilters = cloneRelationshipFilterRules(rules)
	return f
}

// GroupField configures a nested object containing the supplied fields.
type GroupField struct{ nodeView }

// Group creates a nested object containing the supplied fields.
func Group(name string, children Fields) GroupField {
	d := newNode(KindGroup, name)
	d.fields = children.Snapshot()
	return GroupField{nodeView{d}}
}
func (f GroupField) Rename(name string) GroupField { f.definition.name = name; return f }
func (f GroupField) Label(value string) GroupField { f.definition.label = value; return f }
func (f GroupField) LabelTranslations(values map[string]string) GroupField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f GroupField) Admin(value Admin) GroupField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f GroupField) EditAdmin(edit func(*Admin)) GroupField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f GroupField) Private(namespace string, value store.Value) GroupField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f GroupField) Access(value Access) GroupField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f GroupField) RestrictAccess(value Access) GroupField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f GroupField) Required(values ...bool) GroupField {
	f.definition = require(f.definition, values)
	return f
}
func (f GroupField) Localized(values ...bool) GroupField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f GroupField) Validate(value Validator[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f GroupField) ReplaceValidators(values ...Validator[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f GroupField) Hooks(value Hooks[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f GroupField) AppendHooks(value Hooks[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f GroupField) PrependHooks(value Hooks[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f GroupField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f GroupField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f GroupField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f GroupField) AfterRead(callbacks ...OutputTransform[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f GroupField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f GroupField) EditChildren(edit func(*ChildrenDraft) error) (GroupField, error) {
	children, err := f.Children().Edit(edit)
	if err != nil {
		return f, err
	}
	f.definition.fields = children
	return f, nil
}

// ArrayField configures an ordered list of rows containing the supplied fields.
type ArrayField struct{ nodeView }

// Array creates an ordered list of rows containing the supplied fields.
func Array(name string, children Fields) ArrayField {
	d := newNode(KindArray, name)
	d.fields = children.Snapshot()
	return ArrayField{nodeView{d}}
}
func (f ArrayField) Rename(name string) ArrayField { f.definition.name = name; return f }
func (f ArrayField) Label(value string) ArrayField { f.definition.label = value; return f }
func (f ArrayField) LabelTranslations(values map[string]string) ArrayField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f ArrayField) Admin(value Admin) ArrayField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f ArrayField) EditAdmin(edit func(*Admin)) ArrayField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f ArrayField) Private(namespace string, value store.Value) ArrayField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f ArrayField) Access(value Access) ArrayField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f ArrayField) RestrictAccess(value Access) ArrayField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f ArrayField) Required(values ...bool) ArrayField {
	f.definition = require(f.definition, values)
	return f
}
func (f ArrayField) Localized(values ...bool) ArrayField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f ArrayField) Validate(value Validator[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f ArrayField) ReplaceValidators(values ...Validator[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f ArrayField) Hooks(value Hooks[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f ArrayField) AppendHooks(value Hooks[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f ArrayField) PrependHooks(value Hooks[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f ArrayField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f ArrayField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f ArrayField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f ArrayField) AfterRead(callbacks ...OutputTransform[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f ArrayField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f ArrayField) MinRows(value int) ArrayField { f.definition.minRows = value; return f }
func (f ArrayField) MaxRows(value int) ArrayField { f.definition.maxRows = value; return f }
func (f ArrayField) EditChildren(edit func(*ChildrenDraft) error) (ArrayField, error) {
	children, err := f.Children().Edit(edit)
	if err != nil {
		return f, err
	}
	f.definition.fields = children
	return f, nil
}

// BlocksField configures an ordered list of rows chosen from the supplied block types.
type BlocksField struct{ nodeView }

// Blocks creates an ordered list of rows chosen from the supplied block types.
func Blocks(name string, blocks ...Block) BlocksField {
	d := newNode(KindBlocks, name)
	d.blocks = cloneBlocks(blocks)
	return BlocksField{nodeView{d}}
}
func (f BlocksField) Rename(name string) BlocksField { f.definition.name = name; return f }
func (f BlocksField) Label(value string) BlocksField { f.definition.label = value; return f }
func (f BlocksField) LabelTranslations(values map[string]string) BlocksField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f BlocksField) Admin(value Admin) BlocksField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f BlocksField) EditAdmin(edit func(*Admin)) BlocksField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f BlocksField) Private(namespace string, value store.Value) BlocksField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f BlocksField) Access(value Access) BlocksField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f BlocksField) RestrictAccess(value Access) BlocksField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f BlocksField) Required(values ...bool) BlocksField {
	f.definition = require(f.definition, values)
	return f
}
func (f BlocksField) Localized(values ...bool) BlocksField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f BlocksField) Validate(value Validator[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f BlocksField) ReplaceValidators(values ...Validator[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f BlocksField) Hooks(value Hooks[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f BlocksField) AppendHooks(value Hooks[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f BlocksField) PrependHooks(value Hooks[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f BlocksField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f BlocksField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f BlocksField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f BlocksField) AfterRead(callbacks ...OutputTransform[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f BlocksField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f BlocksField) MinRows(value int) BlocksField { f.definition.minRows = value; return f }
func (f BlocksField) MaxRows(value int) BlocksField { f.definition.maxRows = value; return f }

// PluginField configures a field provided by the named Go plugin.
type PluginField struct{ nodeView }

// Plugin creates a field provided by the named Go plugin.
func Plugin(name, key string, config json.RawMessage) PluginField {
	d := newNode(KindPlugin, name)
	d.pluginKey = key
	d.pluginConfig = append(json.RawMessage(nil), config...)
	return PluginField{nodeView{d}}
}
func (f PluginField) Rename(name string) PluginField { f.definition.name = name; return f }
func (f PluginField) Label(value string) PluginField { f.definition.label = value; return f }
func (f PluginField) LabelTranslations(values map[string]string) PluginField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f PluginField) Admin(value Admin) PluginField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f PluginField) EditAdmin(edit func(*Admin)) PluginField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f PluginField) Private(namespace string, value store.Value) PluginField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f PluginField) Access(value Access) PluginField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f PluginField) RestrictAccess(value Access) PluginField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}
func (f PluginField) Required(values ...bool) PluginField {
	f.definition = require(f.definition, values)
	return f
}
func (f PluginField) Localized(values ...bool) PluginField {
	f.definition = localize(f.definition, values)
	return f
}

// Validate appends one authoritative save validator.
func (f PluginField) Validate(value Validator[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.validators = append(slices.Clone(p.validators), value)
	})
	return f
}

// ReplaceValidators replaces all authoritative save validators.
func (f PluginField) ReplaceValidators(values ...Validator[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.validators = slices.Clone(values) })
	return f
}

// Hooks replaces the complete field lifecycle hook group.
func (f PluginField) Hooks(value Hooks[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = cloneHooks(value) })
	return f
}

// AppendHooks appends supplied callbacks after existing callbacks in each phase.
func (f PluginField) AppendHooks(value Hooks[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = appendHooks(p.hooks, value) })
	return f
}

// PrependHooks inserts supplied callbacks before existing callbacks in each phase.
func (f PluginField) PrependHooks(value Hooks[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.hooks = prependHooks(p.hooks, value) })
	return f
}
func (f PluginField) Validators() []Validator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).validators)
}
func (f PluginField) HookPolicy() Hooks[store.Value] {
	return cloneHooks(policies[store.Value, store.Value](f.definition).hooks)
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f PluginField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f PluginField) AfterRead(callbacks ...OutputTransform[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f PluginField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f PluginField) CollectionReferenceKeys(keys ...string) PluginField {
	f.definition.pluginReferenceKeys = slices.Clone(keys)
	return f
}
func (f PluginField) EmbeddedTrees(trees ...EmbeddedTree) PluginField {
	f.definition.pluginTrees = cloneEmbeddedTrees(trees)
	return f
}

// LayoutField arranges fields in the admin without adding a stored value.
type LayoutField struct{ nodeView }

func (f LayoutField) Rename(name string) LayoutField { f.definition.name = name; return f }
func (f LayoutField) Label(value string) LayoutField { f.definition.label = value; return f }
func (f LayoutField) LabelTranslations(values map[string]string) LayoutField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f LayoutField) Admin(value Admin) LayoutField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f LayoutField) EditAdmin(edit func(*Admin)) LayoutField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f LayoutField) Private(namespace string, value store.Value) LayoutField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// OutputField configures a computed response value that cannot be set by a write request.
type OutputField struct{ nodeView }

func (f OutputField) Rename(name string) OutputField { f.definition.name = name; return f }
func (f OutputField) Label(value string) OutputField { f.definition.label = value; return f }
func (f OutputField) LabelTranslations(values map[string]string) OutputField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f OutputField) Admin(value Admin) OutputField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f OutputField) EditAdmin(edit func(*Admin)) OutputField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f OutputField) Private(namespace string, value store.Value) OutputField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}

// Access replaces the complete field access policy.
func (f OutputField) Access(value Access) OutputField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess combines supplied rules with existing access rules; both must allow.
func (f OutputField) RestrictAccess(value Access) OutputField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f OutputField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) OutputField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f OutputField) AfterRead(callbacks ...OutputTransform[store.Value]) OutputField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
func (f OutputField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}
func (f OutputField) Resolver() Resolver[store.Value] {
	return policies[store.Value, store.Value](f.definition).resolver
}

// JoinField configures a list of documents that refer back to this document.
type JoinField struct{ nodeView }

// Join creates a list of documents that refer back to this document.
func Join(name, collection, on string) JoinField {
	d := newNode(KindJoin, name)
	d.joinCollection = collection
	d.joinOn = on
	d.joinLimit = 10
	d.joinAllowCreate = true
	return JoinField{nodeView{d}}
}
func (f JoinField) Rename(name string) JoinField { f.definition.name = name; return f }
func (f JoinField) Label(value string) JoinField { f.definition.label = value; return f }
func (f JoinField) LabelTranslations(values map[string]string) JoinField {
	f.definition.admin.LabelTranslations = cloneTranslations(values)
	return f
}

// Admin replaces the complete admin presentation policy.
func (f JoinField) Admin(value Admin) JoinField {
	f.definition = f.definition.setAdmin(value)
	return f
}

// EditAdmin updates selected admin settings while preserving the rest.
func (f JoinField) EditAdmin(edit func(*Admin)) JoinField {
	f.definition = f.definition.setAdmin(editAdmin(f.AdminPolicy(), edit))
	return f
}
func (f JoinField) Private(namespace string, value store.Value) JoinField {
	f.definition = f.definition.setPrivate(namespace, value)
	return f
}
func (f JoinField) Limit(value int) JoinField { f.definition.joinLimit = value; return f }
func (f JoinField) DefaultColumns(paths ...string) JoinField {
	f.definition.joinDefaultColumns = slices.Clone(paths)
	return f
}
func (f JoinField) DefaultSort(value string) JoinField {
	f.definition.joinDefaultSort = value
	return f
}
func (f JoinField) AllowCreate(value bool) JoinField { f.definition.joinAllowCreate = value; return f }

// NamedTab groups fields in an admin tab and stores them in a named object.
func NamedTab(name, label string, children Fields) GroupField {
	f := Group(name, children).Label(label)
	f.definition = f.definition.withGraph(func(p *graphPolicies) { p.namedTab = true })
	return f
}

// UnnamedTab groups fields in an admin tab without nesting their stored values.
func UnnamedTab(label string, children Fields) LayoutField {
	d := newNode(KindTabs, "")
	d.fields = children.Snapshot()
	d.label = label
	d = d.withGraph(func(p *graphPolicies) { p.unnamedTab = true })
	return LayoutField{nodeView{d}}
}

// Tabs displays named and unnamed tabs together in the admin.
func Tabs(children Fields) LayoutField {
	d := newNode(KindTabs, "")
	d.fields = children.Snapshot()
	return LayoutField{nodeView{d}}
}

// Row displays fields side by side without nesting their stored values.
func Row(children Fields) LayoutField {
	d := newNode(KindRow, "")
	d.fields = children.Snapshot()
	return LayoutField{nodeView{d}}
}

// Collapsible displays fields beneath a heading the author can expand or collapse.
func Collapsible(name string, children Fields) LayoutField {
	d := newNode(KindCollapsible, name)
	d.fields = children.Snapshot()
	return LayoutField{nodeView{d}}
}

// UI adds a custom component to the form without storing a value.
func UI(name string) LayoutField { return LayoutField{nodeView{newNode(KindUI, name)}} }

// Virtual adds a computed field at the document root, using resolver to return its value.
func Virtual(name string, kind ValueType, resolver Resolver[store.Value]) OutputField {
	d := newNode(KindVirtual, name)
	d.valueType = kind
	d = withPolicies[store.Value, store.Value](d, func(p *typedPolicies[store.Value, store.Value]) { p.resolver = resolver })
	return OutputField{nodeView{d}}
}

// Access replaces the read authorization group on an inverse output join.
func (f JoinField) Access(value Access) JoinField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = value })
	return f
}

// RestrictAccess conjoins the supplied join output access policy.
func (f JoinField) RestrictAccess(value Access) JoinField {
	f.definition = f.definition.withGraph(func(g *graphPolicies) { g.access = restrictAccess(g.access, value) })
	return f
}

// ReplaceAfterRead replaces response transforms; no callbacks clears them.
func (f JoinField) ReplaceAfterRead(callbacks ...OutputTransform[store.Value]) JoinField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.afterRead = slices.Clone(callbacks) })
	return f
}

// AfterRead appends response transforms in order; no callbacks does nothing.
func (f JoinField) AfterRead(callbacks ...OutputTransform[store.Value]) JoinField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.afterRead = append(slices.Clone(p.afterRead), callbacks...)
	})
	return f
}

// AfterReadHooks returns a detached response-transform slice.
// AfterReadHooks returns a detached response-transform slice.
func (f JoinField) AfterReadHooks() []OutputTransform[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).afterRead)
}

// EditBlocks edits a detached block declaration slice and publishes it only
// when the edit succeeds. Field-owned behavior and presentation are preserved.
func (f BlocksField) EditBlocks(edit func(*[]Block) error) (BlocksField, error) {
	if len(f.definition.blockReferences) > 0 {
		return f, &EditError{Code: "referenced_block_immutable", Name: f.Name()}
	}
	blocks := cloneBlocks(f.definition.blocks)
	if edit != nil {
		if err := edit(&blocks); err != nil {
			return f, err
		}
	}
	f.definition.blocks = cloneBlocks(blocks)
	return f, nil
}
