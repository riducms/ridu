// Package livevalidation exercises opt-in feedback using the same business rules
// as authoritative saves, through ordinary and plugin-owned field editors.
package livevalidation

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	embeddedplugin "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func Config() core.Config {
	return core.Config{
		Name:    "Live validation",
		Plugins: []core.Plugin{richtext.New(), embeddedplugin.Plugin{}},
		Localization: core.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []core.Locale{
				{Code: "en", Label: "English"},
				{
					Code:            "fr",
					Label:           "French",
					FallbackLocales: []schema.LocaleCode{"en"},
				},
			},
		},
		Collections: []core.Collection{Collection()},
		Globals:     []core.Global{Global()},
	}
}

func Global() core.Global {
	sku := SKU("sku")
	allow := func(core.AccessContext) (core.AccessDecision, error) { return core.Allow(), nil }
	return core.Global{
		Slug:  "validation-settings",
		Label: "Validation settings",
		Access: core.GlobalAccess{
			Read:   allow,
			Update: allow,
		},
		Fields: field.Fields{
			field.Text("supplier"),
			sku,
			richtext.Field("body", richtext.Config{
				Blocks: []field.Block{
					{
						Slug: "card",

						Fields: field.Fields{
							field.Text("supplier"),
							sku,
						},
					},
				},
			}),
		},
	}
}

func Collection() core.Collection {
	sku := SKU("sku")
	links := field.Array("links", field.Fields{
		field.Text("url").Label("URL"),
		field.Text("supplier"),
		SupplierCodes("supplierCodes"),
		PackSizes("packSizes"),
	}).
		Validate(func(_ operation.ValidationContext, value operation.Value[store.Value]) ([]operation.Issue, error) {
			return linkIssues(operation.At(), value), nil
		}).
		LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[store.Value]) ([]operation.Issue, error) {
			return linkIssues(operation.At(), value), nil
		})
	// Containers validate their aggregate descendants; embedded items use the
	// same link rule directly without running duplicate checks at both levels.
	children := field.Fields{
		field.Text("supplier"),
		sku,
		SupplierCodes("supplierCodes"),
		PackSizes("packSizes"),
		links.ReplaceValidators().ReplaceLiveValidators(),
	}
	card := field.Block{Slug: "card", Fields: children}
	note := field.Block{Slug: "note", Fields: children}
	embeddedCard := field.Block{
		Slug: "card",

		Fields: field.Fields{
			field.Text("supplier"),
			sku,
			SupplierCodes("supplierCodes"),
			PackSizes("packSizes"),
			SupplierCodes("customCodes").Label("Custom codes").Admin(field.Admin{
				Editor: field.Component("app:primitiveText"),
			}),
			links,
		},
	}
	sections := field.Array("sections", children).
		Validate(func(_ operation.ValidationContext, value operation.Value[store.Value]) ([]operation.Issue, error) {
			return rowIssues(value, false), nil
		}).
		LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[store.Value]) ([]operation.Issue, error) {
			return rowIssues(value, false), nil
		})
	content := field.Blocks("content", card, note).
		Validate(func(_ operation.ValidationContext, value operation.Value[store.Value]) ([]operation.Issue, error) {
			return rowIssues(value, true), nil
		}).
		LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[store.Value]) ([]operation.Issue, error) {
			return rowIssues(value, true), nil
		})
	allow := func(core.AccessContext) (core.AccessDecision, error) { return core.Allow(), nil }
	return core.Collection{
		Slug: "live-validation",
		Labels: core.CollectionLabels{
			Singular: "Live validation",
			Plural:   "Live validation",
		},
		Admin:  core.CollectionAdmin{UseAsTitle: "title"},
		Access: core.CollectionAccess{Create: allow, Read: allow, Update: allow, Delete: allow},
		Fields: field.Fields{
			field.Text("title").Required(),
			Confirmation("confirmed"),
			field.Text("supplier"),
			sku,
			sku.Rename("customSKU").Label("Custom SKU").Admin(field.Admin{
				Editor:      field.Component("app:text"),
				Description: "Use the selected supplier prefix in this custom editor.",
			}),
			sku.Rename("localizedSKU").Label("Localized SKU").Localized(),
			SupplierCodes("supplierCodes"),
			PackSizes("packSizes"),
			SupplierCodes("customCodes").Label("Custom codes").Admin(field.Admin{
				Editor: field.Component("app:primitiveText"),
			}),
			SupplierCodes("localizedCodes").Label("Localized codes").Localized(),
			PackSizes("localizedSizes").Label("Localized sizes").Localized(),
			sections,
			content,
			richtext.Field("body", richtext.Config{
				Blocks: []field.Block{embeddedCard},
			}),
			richtext.Field("localizedBody", richtext.Config{
				Blocks: []field.Block{embeddedCard},
			}).Localized(),
			embeddedplugin.Field("outline", embeddedCard),
		},
	}
}

func Confirmation(name string) field.CheckboxField {
	validate := func(value operation.Value[bool]) []operation.Issue {
		confirmed, present := value.Get()
		if present && confirmed {
			return nil
		}
		return []operation.Issue{{
			Code:    "confirmation_required",
			Message: "Confirm this document before saving",
		}}
	}
	return field.Checkbox(name).Label("Confirmed").Default(true).
		Admin(field.Admin{Description: "Confirms that this document is ready for review."}).
		Validate(func(_ operation.ValidationContext, value operation.Value[bool]) ([]operation.Issue, error) {
			return validate(value), nil
		}).
		LiveValidate(func(_ operation.LiveValidationContext, value operation.Value[bool]) ([]operation.Issue, error) {
			return validate(value), nil
		})
}

// SKU keeps the read-only business rule independent from either callback phase.
func SKU(name string) field.TextField {
	return field.Text(name).Label("SKU").
		Validate(func(ctx operation.ValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
			return CheckSKU(ctx.Siblings, value)
		}).
		LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
			return CheckSKU(ctx.Siblings, value)
		})
}

func CheckSKU(siblings operation.View, value operation.Value[string]) ([]operation.Issue, error) {
	sku, present := value.Get()
	if !present || sku == "" {
		return nil, nil
	}
	if sku == "server-error" {
		return nil, fmt.Errorf("private supplier service credential: do not expose")
	}
	supplier, _ := siblings.String("supplier")
	prefix := map[string]string{"acme": "A-", "globex": "G-"}[supplier]
	if prefix != "" && !strings.HasPrefix(sku, prefix) {
		return []operation.Issue{
			{
				Code:    "supplier_sku",
				Message: "SKU must start with " + prefix + " for the selected supplier",
			},
		}, nil
	}
	return nil, nil
}

// SupplierCodes reuses one business rule for an entire ordered primitive list.
// Positional messages describe the current snapshot; no primitive item has an ID.
func SupplierCodes(name string) field.TextListField {
	return field.TextList(name).Label("Supplier codes").
		Validate(func(ctx operation.ValidationContext, value operation.Value[[]string]) ([]operation.Issue, error) {
			return checkSupplierCodes(ctx.Siblings, value), nil
		}).
		LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[[]string]) ([]operation.Issue, error) {
			return checkSupplierCodes(ctx.Siblings, value), nil
		})
}

func checkSupplierCodes(siblings operation.View, value operation.Value[[]string]) []operation.Issue {
	codes, _ := value.Get()
	supplier, _ := siblings.String("supplier")
	prefix := map[string]string{"acme": "A-", "globex": "G-"}[supplier]
	if prefix == "" {
		return nil
	}
	for index, code := range codes {
		if !strings.HasPrefix(code, prefix) {
			return []operation.Issue{{
				Code:    "supplier_codes",
				Message: fmt.Sprintf("Item %d: code must start with %s for the selected supplier", index+1, prefix),
			}}
		}
	}
	return nil
}

// PackSizes demonstrates typed numbers and a sibling-dependent server rule.
func PackSizes(name string) field.NumberListField {
	return field.NumberList(name).Label("Pack sizes").
		Validate(func(ctx operation.ValidationContext, value operation.Value[[]float64]) ([]operation.Issue, error) {
			return checkPackSizes(ctx.Siblings, value), nil
		}).
		LiveValidate(func(ctx operation.LiveValidationContext, value operation.Value[[]float64]) ([]operation.Issue, error) {
			return checkPackSizes(ctx.Siblings, value), nil
		})
}

func checkPackSizes(siblings operation.View, value operation.Value[[]float64]) []operation.Issue {
	sizes, _ := value.Get()
	supplier, _ := siblings.String("supplier")
	maximum, constrained := map[string]float64{"acme": 100, "globex": 10}[supplier]
	if !constrained {
		return nil
	}
	for index, size := range sizes {
		if size < 0 || size > maximum {
			return []operation.Issue{{
				Code:    "supplier_pack_sizes",
				Message: fmt.Sprintf("Item %d: pack size must be between 0 and %g for the selected supplier", index+1, maximum),
			}}
		}
	}
	return nil
}

func rowIssues(value operation.Value[store.Value], blocks bool) []operation.Issue {
	input, _ := value.Get()
	rows, _ := input.CopyList()
	var issues []operation.Issue
	for _, row := range rows {
		object, _ := row.CopyObject()
		key, _ := object["_key"].StringValue()
		target := operation.At().Row(key)
		if blocks {
			blockType, _ := object["blockType"].StringValue()
			target = operation.At().Block(key, blockType)
		}
		if sku, _ := object["sku"].StringValue(); sku == "invalid" {
			issues = append(issues, operation.Issue{
				Code:    "row_sku",
				Message: "Choose a different row SKU",
				Target:  target.Field("sku"),
			})
		}
		issues = append(issues, linkIssues(target.Field("links"), operation.Present(object["links"]))...)
	}
	return issues
}

func linkIssues(target operation.IssueTarget, value operation.Value[store.Value]) []operation.Issue {
	input, _ := value.Get()
	links, _ := input.CopyList()
	var issues []operation.Issue
	for _, link := range links {
		object, _ := link.CopyObject()
		if url, _ := object["url"].StringValue(); url == "invalid" {
			key, _ := object["_key"].StringValue()
			issues = append(issues, operation.Issue{
				Code:    "url",
				Message: "Choose a different URL",
				Target:  target.Row(key).Field("url"),
			})
		}
	}
	return issues
}
