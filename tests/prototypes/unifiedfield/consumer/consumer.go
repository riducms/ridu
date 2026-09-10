// Package consumer is an application/plugin outside the prototype field,
// operation and core packages. It uses only their public contracts.
package consumer

import (
	"strings"

	"example.com/ridu-gate1/core"
	"example.com/ridu-gate1/field"
	"example.com/ridu-gate1/operation"
	"github.com/riducms/ridu/store"
)

// These assignments compile only if shared fluent methods retain each facade's
// specialized method set and concrete return type.
var (
	_ field.TextField         = field.Text("title").Required().Label("Title").MaxLength(120)
	_ field.NumberField       = field.Number("priority").Required().Label("Priority").Min(0)
	_ field.RelationshipField = field.Relationship("owner", "users").
		Required().Label("Owner").Access(field.Access{Read: Allow}).
		Hooks(field.Hooks[operation.ID]{BeforeChange: []field.Transform[operation.ID]{NormalizeOwner}}).
		ReadHooks(field.ReadHooks[operation.ReferenceOutput]{AfterRead: []field.OutputTransform[operation.ReferenceOutput]{OwnerOutput}})
	_ field.Validator[string]       = SupplierExists
	_ field.RawTransform            = TrimRaw
	_ field.Transform[string]       = Prefix
	_ field.OutputTransform[string] = UppercaseOutput
)

// SupplierExists demonstrates one bounded, access-aware lookup capability.
// The prototype never binds or runs it through an engine. A fake reader in the
// compile/ownership test proves callers need no LocalAPI or private types.
func SupplierExists(c operation.ValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
	if _, present := value.Get(); !present {
		return nil, nil
	}
	supplier, present := c.Siblings.String("supplier")
	if !present {
		return []operation.Issue{{Code: "supplier_required", Message: "Choose a supplier."}}, nil
	}
	document, err := c.Local.FindByID(c.Context, "suppliers", operation.ID(supplier))
	if err != nil {
		return nil, err
	}
	enabled, _ := document.Values["enabled"].BooleanValue()
	if !enabled {
		return []operation.Issue{{Code: "supplier_disabled", Message: "Choose an enabled supplier."}}, nil
	}
	return nil, nil
}

// TrimRaw accepts finite raw input, including malformed logical string values.
// It does not cast raw input to string or implement the later typed admission.
func TrimRaw(_ operation.WriteContext, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
	raw, present := value.Get()
	if !present {
		return operation.Keep[store.Value](), nil
	}
	text, valid := raw.StringValue()
	if !valid {
		return operation.Keep[store.Value](), nil
	}
	return operation.Replace(operation.Present(store.String(strings.TrimSpace(text)))), nil
}

func Prefix(_ operation.WriteContext, value operation.Value[string]) (operation.Change[string], error) {
	text, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	return operation.Replace(operation.Present("link:" + text)), nil
}

func UppercaseOutput(_ operation.ReadContext, value operation.Value[string]) (operation.Change[string], error) {
	text, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	return operation.Replace(operation.Present(strings.ToUpper(text))), nil
}

func NormalizeOwner(_ operation.WriteContext, value operation.Value[operation.ID]) (operation.Change[operation.ID], error) {
	id, present := value.Get()
	if !present {
		return operation.Keep[operation.ID](), nil
	}
	return operation.Replace(operation.Present(operation.ID(strings.TrimSpace(string(id))))), nil
}

// OwnerOutput can inspect a populated document without generated application
// types. Its typed return is intentionally incompatible with the write hook.
func OwnerOutput(_ operation.ReadContext, value operation.Value[operation.ReferenceOutput]) (operation.Change[operation.ReferenceOutput], error) {
	reference, present := value.Get()
	if !present {
		return operation.Keep[operation.ReferenceOutput](), nil
	}
	if document, populated := reference.Document(); populated {
		document.Values["label"] = store.String("Owner " + string(reference.ID()))
		return operation.Replace(operation.Present(operation.Populated(document))), nil
	}
	return operation.Replace(operation.Present(operation.Unpopulated(reference.ID()))), nil
}

func Allow(operation.AccessContext) (bool, error) { return true, nil }

func Authenticated(c operation.AccessContext) (bool, error) {
	return c.Actor.ID != "", nil
}

func LinkNotBlank(_ operation.ValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
	text, present := value.Get()
	if present && text == "" {
		return []operation.Issue{{Code: "link_blank", Message: "Supply link text."}}, nil
	}
	return nil, nil
}

func reservedLink(_ operation.ValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
	text, present := value.Get()
	if present && text == "reserved" {
		return []operation.Issue{{Code: "reserved_link", Message: "Use another link."}}, nil
	}
	return nil, nil
}

func suffix(_ operation.WriteContext, value operation.Value[string]) (operation.Change[string], error) {
	text, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	return operation.Replace(operation.Present(text + ":decorated")), nil
}

func keepGroup(_ operation.WriteContext, _ operation.Value[store.Value]) (operation.Change[store.Value], error) {
	return operation.Keep[store.Value](), nil
}

// Link deliberately implements only enough authoring structure and attachments
// for factory composition; it is not a complete link value or runtime feature.
func Link(name string) field.GroupField {
	return field.Group(name, field.Fields{
		field.Text("kind"),
		field.Text("href").Label("Destination").MaxLength(120).
			Validate(LinkNotBlank).
			Access(field.Access{Create: Allow, Read: Allow, Update: Allow}).
			Hooks(field.Hooks[string]{
				BeforeValidate: []field.RawTransform{TrimRaw},
				BeforeChange:   []field.Transform[string]{Prefix},
			}).
			ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{UppercaseOutput}}).
			Admin(field.Admin{
				Description: "Choose a destination.",
				Editor:      field.Component("link-input", store.Object(store.Values{"placeholder": store.String("Destination")})),
				Labels:      map[string]string{"en": "Destination"},
				Extensions:  map[string]store.Value{"consumer:hint": store.String("preserve")},
				VisibleWhen: field.Eq(field.Sibling("kind"), store.String("external")),
			}),
	}).Label("Link").
		Access(field.Access{Read: Allow, Update: Authenticated}).
		Hooks(field.Hooks[store.Value]{BeforeChange: []field.Transform[store.Value]{keepGroup}}).
		Admin(field.Admin{
			Description: "Reusable link.",
			Labels:      map[string]string{"en": "Link"},
			VisibleWhen: field.Eq(field.Root("tenant"), store.String("site")),
		})
}

// Configuration proves heterogeneous composition and the upper core -> field
// package edge without requiring a resolver or executing runtime callbacks.
func Configuration() core.Config {
	primary := Link("primary")
	secondary := Link("secondary").Label("Footer link").EditAdmin(func(admin *field.Admin) {
		admin.Description = "Footer destination."
	})
	return core.Config{Fields: field.Fields{
		field.Text("title").Required().Label("Title").MaxLength(120),
		field.Number("priority"),
		field.Group("meta", field.Fields{field.Text("description")}),
		field.Text("tenant"),
		field.Text("sku").Validate(SupplierExists),
		primary,
		secondary,
	}}
}

// Decorate is a plugin/application graph edit using public typed drafts. The
// decorator receives the original concrete node with all attachments intact.
// It never reconstructs a node from getters or asserts its private type.
func Decorate(graph field.Fields) (field.Fields, error) {
	return graph.Edit(func(root *field.ChildrenDraft) error {
		return root.EditChildren("primary", func(children *field.ChildrenDraft) error {
			if err := children.EditText("href", func(text field.TextField) field.TextField {
				return text.MaxLength(256).
					EditAdmin(func(admin *field.Admin) { admin.Description = "Refined destination." }).
					Validate(reservedLink).
					AppendHooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{suffix}}).
					RestrictAccess(field.Access{Update: Authenticated})
			}); err != nil {
				return err
			}
			if err := children.Insert(2, field.Number("weight").Min(0)); err != nil {
				return err
			}
			return children.Replace("kind", field.Text("kind").Label("Link kind"))
		})
	})
}
