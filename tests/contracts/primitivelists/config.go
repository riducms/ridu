// Package primitivelists exercises ordered primitive lists through public APIs.
package primitivelists

import (
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
)

func Config() core.Config {
	return core.Config{Name: "Primitive lists", Plugins: []core.Plugin{richtext.New()},
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []core.Collection{Collection()},
	}
}

func Collection() core.Collection {
	children := field.Fields{field.TextList("points").MaxLength(120), field.NumberList("sizes").Min(0)}
	card := field.Block{Slug: "card", Fields: children}
	note := field.Block{Slug: "note", Fields: children}
	allow := func(core.AccessContext) (core.AccessDecision, error) { return core.Allow(), nil }
	return core.Collection{Slug: "primitive-products", Labels: core.CollectionLabels{Singular: "Primitive product", Plural: "Primitive products"},
		Versions: true,
		Access:   core.CollectionAccess{Create: allow, Read: allow, ReadVersions: allow, Update: allow, Delete: allow},
		Fields: field.Fields{
			field.Text("title").Required(),
			field.TextList("sellingPoints").Label("Selling points").MinRows(1).MaxRows(8).MaxLength(120),
			field.NumberList("availableSizes").Label("Available sizes").Min(0).MaxRows(20),
			field.TextList("localizedPoints").Label("Localized points").Localized(),
			field.NumberList("localizedSizes").Label("Localized sizes").Localized(),
			field.Group("details", children),
			field.Array("variants", children),
			field.Blocks("content", card, note),
			richtext.Field("body", richtext.Config{Blocks: []field.Block{card}}),
			richtext.Field("localizedBody", richtext.Config{Blocks: []field.Block{card}}).Localized(),
		},
	}
}
