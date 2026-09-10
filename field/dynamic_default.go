package field

import (
	"slices"

	"github.com/riducms/ridu/operation"
)

func setDefaultFrom[T any](d View, callback DefaultFunc[T]) View {
	d = clearDefaultSettings(d)
	d = withPolicies[T, T](d, func(p *typedPolicies[T, T]) { p.defaultFrom = callback })
	if callback == nil {
		d.issues = append(d.issues, Issue{Code: "nil_field_callback", Path: "defaultFrom", Message: "DefaultFrom requires a non-nil callback; use Default for a literal initial value"})
	}
	return d
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f TextField) DefaultFrom(callback DefaultFunc[string]) TextField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f TextField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f CodeField) DefaultFrom(callback DefaultFunc[string]) CodeField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f CodeField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f TextareaField) DefaultFrom(callback DefaultFunc[string]) TextareaField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f TextareaField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f EmailField) DefaultFrom(callback DefaultFunc[string]) EmailField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f EmailField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f DateField) DefaultFrom(callback DefaultFunc[string]) DateField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f DateField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f NumberField) DefaultFrom(callback DefaultFunc[float64]) NumberField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f NumberField) DefaultCallback() DefaultFunc[float64] {
	return policies[float64, float64](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f CheckboxField) DefaultFrom(callback DefaultFunc[bool]) CheckboxField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f CheckboxField) DefaultCallback() DefaultFunc[bool] {
	return policies[bool, bool](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f SelectField) DefaultFrom(callback DefaultFunc[string]) SelectField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f SelectField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f RadioField) DefaultFrom(callback DefaultFunc[string]) RadioField {
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f RadioField) DefaultCallback() DefaultFunc[string] {
	return policies[string, string](f.definition).defaultFrom
}

// DefaultFrom supplies a server-side initial value for an eligible omitted field.
// It replaces a literal or dynamic default; the original field is unchanged.
// See DefaultFunc and operation.DefaultContext for initialization semantics.
func (f MultiSelectField) DefaultFrom(callback DefaultFunc[[]string]) MultiSelectField {
	if callback != nil {
		original := callback
		callback = func(ctx operation.DefaultContext) (operation.Value[[]string], error) {
			value, err := original(ctx)
			if options, present := value.Get(); present {
				return operation.Present(slices.Clone(options)), err
			}
			return operation.Empty[[]string](), err
		}
	}
	f.definition = setDefaultFrom(f.definition, callback)
	return f
}

// DefaultCallback returns the configured dynamic default, or nil for a literal
// default or no default. Callback closure captures remain application-owned.
func (f MultiSelectField) DefaultCallback() DefaultFunc[[]string] {
	return policies[[]string, []string](f.definition).defaultFrom
}
