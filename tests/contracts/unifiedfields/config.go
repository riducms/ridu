// Package unifiedfields is the shared Gate 4 generation, transport and admin fixture.
package unifiedfields

import (
	"strings"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Config resolves without runtime services. Tests construct it with an isolated store.
func Config() core.Config {
	return core.Config{
		Name:         "Unified field contracts",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}}},
		Plugins:      []core.Plugin{richtext.New()},
		Collections: []core.Collection{
			{Slug: "users", Fields: field.Fields{field.Text("name").Required()}},
			Collection(),
		},
	}
}

// Collection is also mounted in the real Go-backed admin fixture.
func Collection() core.Collection {
	accent := field.Text("accent").Label("Accent").Admin(field.Admin{
		Editor: field.Component("app:capturedText", store.Object(store.Values{"capture": store.Boolean(true)})),
	})
	sku := field.Text("sku").Label("SKU").Validate(validateSKU).
		Access(field.Access{Create: writableSKU, Update: writableSKU}).
		Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{normalizeSKU}})
	rowLabel := field.Component("app:unifiedRowLabel", store.Object(store.Values{"prefix": store.String("Row")}))
	label := field.Text("label").Admin(field.Admin{Editor: field.Component("app:text")})
	products := field.Array("products", field.Fields{label, sku, accent}).Admin(field.Admin{RowLabel: rowLabel})
	card := field.Block{Slug: "card", Fields: field.Fields{label, sku, accent, products}}
	// The same block carries its behavior and presentation into both hosts.
	embedded := richtext.Field("body", richtext.Config{Blocks: []field.Block{card}})
	return core.Collection{
		Slug:   "unified-articles",
		Labels: core.CollectionLabels{Singular: "Unified article", Plural: "Unified articles"},
		Admin:  core.CollectionAdmin{Group: "Contracts"},
		Fields: field.Fields{
			field.Text("title").Required(), sku,
			field.Text("defaulted").Required().Default("Ready"),
			field.Group("meta", field.Fields{field.Text("description"), accent}),
			field.Array("sections", field.Fields{label, sku, accent, products}).Admin(field.Admin{RowLabel: rowLabel}),
			field.Blocks("content", card, field.Block{Slug: "note", Fields: field.Fields{label, accent}}).Admin(field.Admin{RowLabel: rowLabel}),
			field.Relationship("author", "users"), accent,
			accent.Rename("localizedAccent").Label("Localized accent").Localized(),
			field.Text("localizedTitle").Localized(),
			field.Group("localizedMeta", field.Fields{field.Text("description").Localized()}),
			field.Text("privateNote").Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }}),
			field.Text("presentationHidden").Admin(field.Admin{Hidden: true}),
			embedded,
			field.Virtual("summary", field.ValueString, func(ctx operation.Context) (operation.Value[store.Value], error) {
				title, _ := ctx.Root.Get("title").StringValue()
				return operation.Present(store.String("Article: " + title)), nil
			}),
		},
	}
}

func writableSKU(ctx operation.Context) (bool, error) {
	value, _ := ctx.Root.Get("title").StringValue()
	return value != "Locked", nil
}

func normalizeSKU(_ operation.Context, value operation.Value[string]) (operation.Change[string], error) {
	if text, present := value.Get(); present {
		return operation.Replace(operation.Present(strings.ToUpper(strings.TrimSpace(text)))), nil
	}
	return operation.Keep[string](), nil
}

func validateSKU(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
	if text, present := value.Get(); present && !strings.HasPrefix(text, "SKU-") {
		return []operation.Issue{{Code: "sku", Message: "SKU must start with SKU-"}}, nil
	}
	return nil, nil
}
