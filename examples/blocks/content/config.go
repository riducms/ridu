// Package content defines a finite block and rich-text publishing application.
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

// Hero is reused by both pages and campaigns. TypeName affects generated symbols,
// while the stable slug is the stored discriminator.
var Hero = field.Block{
	Slug: "hero", TypeName: "Hero",
	Admin: field.BlockAdmin{RowLabelPath: "heading"},
	Fields: field.Fields{
		field.Text("heading").Required().Localized(),
		field.Group("appearance", field.Fields{
			field.Select("tone", "light", "dark"),
		}),
	},
}

var Content = field.Block{
	Slug: "content", TypeName: "Content",
	Admin: field.BlockAdmin{RowLabelPath: "title"},
	Fields: field.Fields{
		field.Text("title").Localized(),
		richtext.Field("body"),
		field.Array("links", field.Fields{field.Text("label").Required(), field.Text("href").Required()}),
	},
}

var Media = field.Block{
	Slug: "media", TypeName: "Media",
	Admin: field.BlockAdmin{RowLabelPath: "caption"},
	Fields: field.Fields{
		field.Relationship("asset", "assets").Required(),
		field.Text("caption").Localized(),
	},
}

var CTA = field.Block{
	Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, TypeName: "CTA",
	Admin: field.BlockAdmin{RowLabelPath: "label"},
	Fields: field.Fields{
		field.Text("label").Required().Localized(),
		field.Relationship("destination", "pages"),
	},
}

// Callout contains two independent finite editors. The inner editor accepts CTA,
// whose fields do not refer back to Callout, so the schema has no recursion.
var Callout = field.Block{
	Slug: "callout", TypeName: "Callout",
	Admin: field.BlockAdmin{RowLabelPath: "title"},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Textarea("message").Localized(),
		richtext.Field("detail", richtext.Config{Blocks: []field.Block{CTA}}),
		richtext.Field("aside"),
	},
}

// Config is ordinary executable application configuration, independent of storage.
func Config() ridu.Config {
	return ridu.Config{
		Name:    "Blocks reference",
		Plugins: []ridu.Plugin{richtext.New()},
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales:       []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
		},
		Collections: []ridu.Collection{
			{Slug: "assets", Fields: field.Fields{field.Text("title").Required(), field.Text("url").Required()}},
			{Slug: "pages", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{
				field.Text("title").Required(),
				field.Blocks("layout", Hero, Content, Media, CTA).Required().MinRows(1).MaxRows(12),
			}},
			{Slug: "campaigns", Fields: field.Fields{field.Text("title"), field.Blocks("layout", Hero, CTA)}},
			{Slug: "articles", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{
				field.Text("title").Required(),
				richtext.Field("body", richtext.Config{Blocks: []field.Block{Callout, Media, CTA}}),
				richtext.Field("localizedBody", richtext.Config{Blocks: []field.Block{Callout, Media, CTA}}).Localized(),
			}},
		},
	}
}
