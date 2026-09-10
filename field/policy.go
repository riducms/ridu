package field

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// DefaultFunc supplies a server-side initial value for an eligible omitted field.
// Present supplies a value, including zero values; Empty leaves it without a
// default. An error stops the operation. Returned values still pass validation.
// Defaults do not prefill an unsaved admin form and do not run on ordinary edits
// to an existing scope. Callbacks must not depend on other dynamic defaults or
// perform irreversible side effects: separate requests or retries can run again.
type DefaultFunc[T any] func(operation.DefaultContext) (operation.Value[T], error)

// Validator checks a field before saving, after built-in checks and write hooks.
// Return issues for values the author needs to fix, or an error if the check
// could not run. An issue belongs to this field unless its Target names a child.
type Validator[T any] func(operation.ValidationContext, operation.Value[T]) ([]operation.Issue, error)

// RawTransform checks or changes one field before input is converted to its Go
// type. The context supplies surrounding values; the separate value is this
// field's raw input and may be invalid. Empty means omitted; Present(store.Null())
// means explicit null. Return Keep to leave it unchanged, Replace to change this field,
// or an error to stop. Changing a context snapshot does not change the document.
type RawTransform func(operation.WriteContext, operation.Value[store.Value]) (operation.Change[store.Value], error)

// Transform checks or changes one field's typed value before saving. The context
// includes unchanged values from an update and changes made by earlier hooks.
// Return Keep to leave this field unchanged, Replace(Present(value)) to set it,
// or Replace(Empty[T]()) to clear it. An error stops the operation. Returned
// values still pass through validation and field access checks.
type Transform[T any] func(operation.WriteContext, operation.Value[T]) (operation.Change[T], error)

// OutputTransform changes one field in the response without changing storage.
// Its context supplies response values, which may include related documents and
// locale fallback. Return Keep or Replace, as for a write transform. Field read
// access still runs afterward; visible context data is not permission to expose it.
// An error stops the response and can roll back an uncommitted mutation.
type OutputTransform[T any] func(operation.ReadContext, operation.Value[T]) (operation.Change[T], error)

// Observer runs code at a field lifecycle event. It receives the context and the
// field's value, but cannot return a replacement or mutate the context snapshots.
// Return nil to continue or an error to stop. Before commit, errors roll back the
// operation; an AfterCommit error reports failure after data has already saved.
type Observer[T any] func(operation.EventContext, operation.Value[T]) error

// Resolver calculates a root Virtual field for the response. Read other values
// from its context and return Present(value), Empty[T](), or an error. The value
// must match the declared Virtual type and remains subject to field read access.
type Resolver[T any] func(operation.ReadContext) (operation.Value[T], error)

// AccessRule decides whether one field value may be created, read, or updated.
// Return false during a write to reject the operation, or during a read to omit
// the field from the response. Returning an error stops the operation. Field
// rules cannot return the query predicates supported by collection access rules.
type AccessRule func(operation.AccessContext) (bool, error)

// Hooks registers transforms and event callbacks for one field. Each list runs
// in declaration order for each field occurrence, including nested array rows.
// Collection or global hooks run first in the same phase, except AfterCommit,
// where field callbacks run first. Use ReadHooks for response transformations.
type Hooks[T any] struct {
	// BeforeDuplicate receives raw copied input before validation. It runs only
	// when duplicating a collection document, after the resource hook.
	BeforeDuplicate []RawTransform
	// BeforeValidate receives raw input before built-in validation. It can supply
	// or normalize a value before conversion to T. This shared phase also runs on
	// reads and deletes; check the context's Operation for write-only logic.
	BeforeValidate []RawTransform
	// BeforeChange transforms T after initial built-in checks on create,
	// duplicate, update, publish, and unpublish. Custom validators run later.
	BeforeChange []Transform[T]
	// BeforeOperation transforms T before the store operation, after BeforeChange
	// on writes. This shared phase also runs on reads and deletes.
	BeforeOperation []Transform[T]
	// BeforeDelete observes the saved value before trashing or permanent deletion.
	BeforeDelete []Observer[T]
	// AfterChange observes a saved create, duplicate, update, publish, or unpublish
	// before commit. It cannot replace the field; an error rolls back the save.
	AfterChange []Observer[T]
	// AfterDelete observes a value after trashing or permanent deletion but before
	// commit. An error can still roll back the deletion.
	AfterDelete []Observer[T]
	// AfterOperation observes the result before response processing and commit.
	// It also runs on reads and deletes; check Operation for write-only work.
	AfterOperation []Observer[T]
	// AfterCommit observes the value outside the committed transaction, before
	// resource AfterCommit hooks. It also runs for reads. Errors cannot roll back
	// saved data, and later effects still run.
	AfterCommit []Observer[T]
}

// ReadHooks registers response transformations using the field's output type,
// which can differ from its write type when a relationship is populated.
type ReadHooks[T any] struct {
	// AfterRead transforms each returned field value after resource AfterRead
	// hooks and before final field redaction. It runs for reads and mutation
	// responses, without changing the stored value.
	AfterRead []OutputTransform[T]
}

func cloneHooks[T any](h Hooks[T]) Hooks[T] {
	h.BeforeDuplicate = slices.Clone(h.BeforeDuplicate)
	h.BeforeValidate = slices.Clone(h.BeforeValidate)
	h.BeforeChange = slices.Clone(h.BeforeChange)
	h.BeforeOperation = slices.Clone(h.BeforeOperation)
	h.BeforeDelete = slices.Clone(h.BeforeDelete)
	h.AfterChange = slices.Clone(h.AfterChange)
	h.AfterDelete = slices.Clone(h.AfterDelete)
	h.AfterOperation = slices.Clone(h.AfterOperation)
	h.AfterCommit = slices.Clone(h.AfterCommit)
	return h
}

func appendHooks[T any](base, extra Hooks[T]) Hooks[T] {
	base = cloneHooks(base)
	base.BeforeDuplicate = append(base.BeforeDuplicate, extra.BeforeDuplicate...)
	base.BeforeValidate = append(base.BeforeValidate, extra.BeforeValidate...)
	base.BeforeChange = append(base.BeforeChange, extra.BeforeChange...)
	base.BeforeOperation = append(base.BeforeOperation, extra.BeforeOperation...)
	base.BeforeDelete = append(base.BeforeDelete, extra.BeforeDelete...)
	base.AfterChange = append(base.AfterChange, extra.AfterChange...)
	base.AfterDelete = append(base.AfterDelete, extra.AfterDelete...)
	base.AfterOperation = append(base.AfterOperation, extra.AfterOperation...)
	base.AfterCommit = append(base.AfterCommit, extra.AfterCommit...)
	return base
}

func prependHooks[T any](base, extra Hooks[T]) Hooks[T] { return appendHooks(extra, base) }

func cloneReadHooks[T any](h ReadHooks[T]) ReadHooks[T] {
	h.AfterRead = slices.Clone(h.AfterRead)
	return h
}

func appendReadHooks[T any](base, extra ReadHooks[T]) ReadHooks[T] {
	base = cloneReadHooks(base)
	base.AfterRead = append(base.AfterRead, extra.AfterRead...)
	return base
}

// Access configures who may create, read, or update a field value. Assign it
// with a concrete field's Access method; an omitted rule leaves that operation
// unrestricted. Access replaces the field's complete policy group, while
// RestrictAccess adds only the supplied rules and requires both old and new
// rules to allow the operation.
//
// For example, this field is omitted from anonymous read responses:
//
//	field.Text("internalNotes").Access(field.Access{
//		Read: func(ctx operation.AccessContext) (bool, error) {
//			return ctx.Actor != nil, nil
//		},
//	})
type Access struct {
	// Create controls whether the field may be supplied during document creation.
	Create AccessRule
	// Read controls whether the field is included in API and admin responses.
	Read AccessRule
	// Update controls whether the field may be changed on an existing document.
	Update AccessRule
}

func restrictAccess(base, extra Access) Access {
	return Access{
		Create: andAccess(base.Create, extra.Create),
		Read:   andAccess(base.Read, extra.Read),
		Update: andAccess(base.Update, extra.Update),
	}
}

func andAccess(base, extra AccessRule) AccessRule {
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

// ComponentRef selects a custom admin component and its optional settings.
// It changes the UI without changing how the field's value is stored or typed.
type ComponentRef struct {
	Key             string
	PluginKey       string
	Config          store.Value
	configArguments int
}

// Component selects an application component registered in admin/src/admin.config.ts.
// Optionally pass a settings object; the component registration must provide a
// decoder to check it. Omit settings when the component does not use them.
func Component(key string, config ...store.Value) ComponentRef {
	ref := ComponentRef{Key: key, configArguments: len(config)}
	if len(config) == 1 {
		ref.Config = config[0]
	}
	return ref
}

// PluginComponent selects a component supplied by an installed plugin.
// pluginKey names the Go plugin; key names a component registered by its admin package.
func PluginComponent(pluginKey, key string, config ...store.Value) ComponentRef {
	ref := Component(key, config...)
	ref.PluginKey = pluginKey
	return ref
}

// Err validates static component syntax and finite public object configuration.
// Omitted configuration is allowed; explicitly supplied zero or null values are not objects.
// Registration and supported host checks belong to configuration resolution.
func (c ComponentRef) Err() error {
	if c.configArguments > 1 {
		return fmt.Errorf("component accepts at most one configuration object")
	}
	if c.configArguments == 1 && c.Config.Kind() == "" {
		return fmt.Errorf("supplied component configuration must be an object; omit the argument for a component without settings")
	}
	if c.Key == "" && c.PluginKey == "" && c.Config.Kind() == "" {
		return nil
	}
	if c.PluginKey == "" {
		if !editorReference.MatchString(c.Key) {
			return fmt.Errorf("local component reference %q must be app:name", c.Key)
		}
	} else {
		if !schema.IsValidPluginKey(c.PluginKey) {
			return fmt.Errorf("component plugin %q must be a lowercase plugin key", c.PluginKey)
		}
		if !schema.IsValidAdminPluginExport(c.Key) {
			return fmt.Errorf("plugin component %q must be a JavaScript identifier", c.Key)
		}
	}
	if c.Config.Kind() != "" {
		if c.Config.Kind() != store.ValueObject {
			return fmt.Errorf("component configuration must be a finite JSON object")
		}
		if _, err := json.Marshal(c.Config); err != nil {
			return fmt.Errorf("component configuration must be finite JSON: %w", err)
		}
	}
	return nil
}

// MarshalJSON encodes only public static component data. An unspecified
// reference is null and absent configuration is omitted.
func (c ComponentRef) MarshalJSON() ([]byte, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	if c.Key == "" {
		return []byte("null"), nil
	}
	var config *store.Value
	if c.Config.Kind() != "" {
		value := c.Config
		config = &value
	}
	return json.Marshal(struct {
		Key       string       `json:"key"`
		PluginKey string       `json:"pluginKey,omitempty"`
		Config    *store.Value `json:"config,omitempty"`
	}{c.Key, c.PluginKey, config})
}

// Admin configures how a field is presented and edited in the framework admin.
// It never grants API access or changes storage behavior; use Access for
// authorization. A field's Admin method replaces the complete group, while
// EditAdmin preserves members its callback does not change. Ridu snapshots maps
// on input and output so retained drafts cannot mutate a published field.
type Admin struct {
	Description             string
	DescriptionTranslations map[string]string
	Placeholder             string
	PlaceholderTranslations map[string]string
	ReadOnly                bool
	Hidden                  bool
	Sidebar                 bool
	Columns                 int
	Editor                  ComponentRef
	RowLabel                ComponentRef
	RowLabelPath            string
	RowLabels               RowLabels
	LabelTranslations       map[string]string
	Tab                     string
	TabTranslations         map[string]string
	InitiallyCollapsed      bool
	CodeLanguage            string
	Extensions              map[string]store.Value
	// VisibleWhen is a finite presentation condition; it never grants access.
	VisibleWhen Condition
}

func cloneAdmin(a Admin) Admin {
	a.LabelTranslations = maps.Clone(a.LabelTranslations)
	a.DescriptionTranslations = maps.Clone(a.DescriptionTranslations)
	a.PlaceholderTranslations = maps.Clone(a.PlaceholderTranslations)
	a.TabTranslations = maps.Clone(a.TabTranslations)
	a.RowLabels = cloneRowLabels(a.RowLabels)
	a.Extensions = maps.Clone(a.Extensions)
	a.VisibleWhen = cloneCondition(a.VisibleWhen)
	return a
}

func editAdmin(a Admin, edit func(*Admin)) Admin {
	draft := cloneAdmin(a)
	if edit != nil {
		edit(&draft)
	}
	// Authors can retain a draft or inject borrowed maps, so detach after edit.
	return cloneAdmin(draft)
}
