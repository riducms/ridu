package field

import (
	"slices"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// LiveValidator checks a typed unsaved value on explicit admin feedback requests.
// Root and Siblings contain retained input, without save hooks or defaults.
// Reuse business rules with Validator through thin, separately typed adapters.
// A live check never replaces authoritative save validation.
//
// Return operation.Issue values for user-correctable problems, or an error when
// the check could not run. Checks must be repeatable, read-only and respect the
// context's cancellation. The framework cannot prevent side effects in arbitrary
// application Go code. Invalid typed input is skipped before this callback runs.
// Aggregate store.Value callbacks receive raw object/list carriers: their children
// may still be incomplete or malformed. No deep save validation has run.
type LiveValidator[T any] func(operation.LiveValidationContext, operation.Value[T]) ([]operation.Issue, error)

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f TextField) LiveValidate(value LiveValidator[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f TextField) ReplaceLiveValidators(values ...LiveValidator[string]) TextField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f TextField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f CodeField) LiveValidate(value LiveValidator[string]) CodeField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f CodeField) ReplaceLiveValidators(values ...LiveValidator[string]) CodeField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f CodeField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f TextareaField) LiveValidate(value LiveValidator[string]) TextareaField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f TextareaField) ReplaceLiveValidators(values ...LiveValidator[string]) TextareaField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f TextareaField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f EmailField) LiveValidate(value LiveValidator[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f EmailField) ReplaceLiveValidators(values ...LiveValidator[string]) EmailField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f EmailField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f DateField) LiveValidate(value LiveValidator[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f DateField) ReplaceLiveValidators(values ...LiveValidator[string]) DateField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f DateField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f NumberField) LiveValidate(value LiveValidator[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f NumberField) ReplaceLiveValidators(values ...LiveValidator[float64]) NumberField {
	f.definition = withPolicies[float64, float64](f.definition, func(p *typedPolicies[float64, float64]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f NumberField) LiveValidators() []LiveValidator[float64] {
	return slices.Clone(policies[float64, float64](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f CheckboxField) LiveValidate(value LiveValidator[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.liveValidators = append(slices.Clone(p.liveValidators), value) })
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f CheckboxField) ReplaceLiveValidators(values ...LiveValidator[bool]) CheckboxField {
	f.definition = withPolicies[bool, bool](f.definition, func(p *typedPolicies[bool, bool]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f CheckboxField) LiveValidators() []LiveValidator[bool] {
	return slices.Clone(policies[bool, bool](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f JSONField) LiveValidate(value LiveValidator[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f JSONField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) JSONField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f JSONField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f PointField) LiveValidate(value LiveValidator[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f PointField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) PointField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f PointField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f SelectField) LiveValidate(value LiveValidator[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f SelectField) ReplaceLiveValidators(values ...LiveValidator[string]) SelectField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f SelectField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f RadioField) LiveValidate(value LiveValidator[string]) RadioField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f RadioField) ReplaceLiveValidators(values ...LiveValidator[string]) RadioField {
	f.definition = withPolicies[string, string](f.definition, func(p *typedPolicies[string, string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f RadioField) LiveValidators() []LiveValidator[string] {
	return slices.Clone(policies[string, string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f MultiSelectField) LiveValidate(value LiveValidator[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f MultiSelectField) ReplaceLiveValidators(values ...LiveValidator[[]string]) MultiSelectField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f MultiSelectField) LiveValidators() []LiveValidator[[]string] {
	return slices.Clone(policies[[]string, []string](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f RelationshipField) LiveValidate(value LiveValidator[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f RelationshipField) ReplaceLiveValidators(values ...LiveValidator[operation.ID]) RelationshipField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.liveValidators = slices.Clone(values)
	})
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f RelationshipField) LiveValidators() []LiveValidator[operation.ID] {
	return slices.Clone(policies[operation.ID, operation.ReferenceOutput](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f RelationshipsField) LiveValidate(value LiveValidator[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f RelationshipsField) ReplaceLiveValidators(values ...LiveValidator[[]operation.ID]) RelationshipsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.liveValidators = slices.Clone(values)
	})
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f RelationshipsField) LiveValidators() []LiveValidator[[]operation.ID] {
	return slices.Clone(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f UploadField) LiveValidate(value LiveValidator[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f UploadField) ReplaceLiveValidators(values ...LiveValidator[operation.ID]) UploadField {
	f.definition = withPolicies[operation.ID, operation.ReferenceOutput](f.definition, func(p *typedPolicies[operation.ID, operation.ReferenceOutput]) {
		p.liveValidators = slices.Clone(values)
	})
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f UploadField) LiveValidators() []LiveValidator[operation.ID] {
	return slices.Clone(policies[operation.ID, operation.ReferenceOutput](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f UploadsField) LiveValidate(value LiveValidator[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f UploadsField) ReplaceLiveValidators(values ...LiveValidator[[]operation.ID]) UploadsField {
	f.definition = withPolicies[[]operation.ID, []operation.ReferenceOutput](f.definition, func(p *typedPolicies[[]operation.ID, []operation.ReferenceOutput]) {
		p.liveValidators = slices.Clone(values)
	})
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f UploadsField) LiveValidators() []LiveValidator[[]operation.ID] {
	return slices.Clone(policies[[]operation.ID, []operation.ReferenceOutput](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f PolymorphicRelationshipField) LiveValidate(value LiveValidator[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f PolymorphicRelationshipField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) PolymorphicRelationshipField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f PolymorphicRelationshipField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f PolymorphicRelationshipsField) LiveValidate(value LiveValidator[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f PolymorphicRelationshipsField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) PolymorphicRelationshipsField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f PolymorphicRelationshipsField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f GroupField) LiveValidate(value LiveValidator[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f GroupField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) GroupField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f GroupField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f ArrayField) LiveValidate(value LiveValidator[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f ArrayField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) ArrayField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f ArrayField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f BlocksField) LiveValidate(value LiveValidator[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f BlocksField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) BlocksField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f BlocksField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an explicitly opted-in advisory server check. It does not
// register a save validator; use Validate separately for authoritative checks.
func (f PluginField) LiveValidate(value LiveValidator[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces this field's advisory checks, preserving its
// save validators and every unrelated policy. An empty list disables live checks.
func (f PluginField) ReplaceLiveValidators(values ...LiveValidator[store.Value]) PluginField {
	f.definition = withPolicies[store.Value, store.Value](f.definition, func(p *typedPolicies[store.Value, store.Value]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of this field's advisory server checks.
func (f PluginField) LiveValidators() []LiveValidator[store.Value] {
	return slices.Clone(policies[store.Value, store.Value](f.definition).liveValidators)
}

// LiveValidate appends an advisory check for the complete primitive list. Items
// have no separate occurrence identity; Validate remains authoritative on Save.
func (f TextListField) LiveValidate(value LiveValidator[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces only this field's advisory checks. An empty
// list disables live checks while preserving every unrelated policy.
func (f TextListField) ReplaceLiveValidators(values ...LiveValidator[[]string]) TextListField {
	f.definition = withPolicies[[]string, []string](f.definition, func(p *typedPolicies[[]string, []string]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of whole-list advisory checks.
func (f TextListField) LiveValidators() []LiveValidator[[]string] {
	return slices.Clone(policies[[]string, []string](f.definition).liveValidators)
}

// LiveValidate appends an advisory check for the complete primitive list. Items
// have no separate occurrence identity; Validate remains authoritative on Save.
func (f NumberListField) LiveValidate(value LiveValidator[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) {
		p.liveValidators = append(slices.Clone(p.liveValidators), value)
	})
	return f
}

// ReplaceLiveValidators replaces only this field's advisory checks. An empty
// list disables live checks while preserving every unrelated policy.
func (f NumberListField) ReplaceLiveValidators(values ...LiveValidator[[]float64]) NumberListField {
	f.definition = withPolicies[[]float64, []float64](f.definition, func(p *typedPolicies[[]float64, []float64]) { p.liveValidators = slices.Clone(values) })
	return f
}

// LiveValidators returns a detached list of whole-list advisory checks.
func (f NumberListField) LiveValidators() []LiveValidator[[]float64] {
	return slices.Clone(policies[[]float64, []float64](f.definition).liveValidators)
}
