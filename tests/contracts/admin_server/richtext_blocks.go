package main

import (
	"os"
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
)

// The browser fixture deliberately uses ordinary reusable block definitions,
// including two finite inner editors. Nothing here extends the operation engine.
func richTextBlockDefinitions() (callout, cta, media field.Block) {
	cta = field.Block{Admin: field.BlockAdmin{RowLabelPath: "label"}, Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{
		field.Text("label").Label("CTA label").Required().Localized(),
		field.Relationship("destination", "pages"),
	}}

	callout = field.Block{Admin: field.BlockAdmin{RowLabelPath: "title"}, Slug: "callout", Fields: field.Fields{
		field.Text("title").Label("Callout title").Required().Validate(func(_ operation.Context, value operation.Value[string]) ([]operation.Issue, error) {
			if text, _ := value.Get(); strings.EqualFold(text, "invalid") {
				return []operation.Issue{{Code: "callout_title", Message: "Use a descriptive callout title"}}, nil
			}
			return nil, nil
		}),
		field.Text("caption").Default("Helpful context"),
		field.Text("translation").Localized(),
		field.Group("appearance", field.Fields{
			field.Select("tone").Options(field.Option{Value: "info", Label: "info"}, field.Option{Value: "warning", Label: "warning"}).Default("info"),
		}),
		field.Array("links", field.Fields{
			field.Text("label").Required(),
			field.Text("href"),
		}),
		richtext.Field("detail", richtext.Config{Blocks: []field.Block{cta}}).Label("Detail"),
		richtext.Field("extraDetail", richtext.Config{Blocks: []field.Block{cta}}).Label("Extra detail"),
		field.Collapsible("advancedContent", field.Fields{
			richtext.Field("advancedDetail", richtext.Config{Blocks: []field.Block{cta}}).Label("Advanced detail"),
		}).Admin(field.Admin{InitiallyCollapsed: true}),
	}}

	media = field.Block{Admin: field.BlockAdmin{RowLabelPath: "caption"}, Slug: "media", Fields: field.Fields{
		field.Upload("asset", "media").Label("Block asset").Required(),
		field.Text("caption").Localized(),
	}}

	return callout, cta, media
}

func richTextBlocksCollection() ridu.Collection {
	previewAddress := os.Getenv("RIDU_BROWSER_PREVIEW_ADDRESS")
	if previewAddress == "" {
		previewAddress = "127.0.0.1:18082"
	}
	callout, cta, media := richTextBlockDefinitions()
	return ridu.Collection{
		Slug: "block-articles", Labels: ridu.CollectionLabels{Singular: "Block article", Plural: "Block articles"},
		Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 20},
		Admin: ridu.CollectionAdmin{Group: "Editorial", LivePreview: ridu.LivePreviewConfig{URL: "http://" + previewAddress + "/preview/block-articles/{id}"}},
		Fields: field.Fields{
			field.Text("title").Required(),
			field.Text("caption"),
			richtext.Field("body", richtext.Config{Blocks: []field.Block{callout, cta, media}}).Label("Body"),
			richtext.Field("localizedBody", richtext.Config{Blocks: []field.Block{callout, cta, media}}).Label("Localized body").Localized(),
		},
		Access: ridu.CollectionAccess{Create: allowRoles(roleAdministrator, roleEditor), Read: allowEveryone, ReadVersions: allowRoles(roleAdministrator, roleEditor), Update: allowRoles(roleAdministrator, roleEditor), Delete: allowRoles(roleAdministrator)},
	}
}

func richTextBlockPagesCollection() ridu.Collection {
	callout, cta, media := richTextBlockDefinitions()
	hero := field.Block{Admin: field.BlockAdmin{RowLabelPath: "heading"}, Slug: "hero", Fields: field.Fields{
		field.Text("heading").Required().Localized(),
	}}
	content := field.Block{Admin: field.BlockAdmin{RowLabelPath: "title"}, Slug: "content", Fields: field.Fields{
		field.Text("title").Localized(),
		richtext.Field("body", richtext.Config{Blocks: []field.Block{callout, cta}}),
	}}
	return ridu.Collection{
		Slug: "block-pages", Labels: ridu.CollectionLabels{Singular: "Block page", Plural: "Block pages"},
		Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 20},
		Admin: ridu.CollectionAdmin{Group: "Editorial"},
		Fields: field.Fields{
			field.Text("title").Required(),
			field.Blocks("layout", hero, content, media, cta).Required().MinRows(1).MaxRows(12),
		},
		Access: ridu.CollectionAccess{Create: allowRoles(roleAdministrator, roleEditor), Read: allowEveryone, ReadVersions: allowRoles(roleAdministrator, roleEditor), Update: allowRoles(roleAdministrator, roleEditor), Delete: allowRoles(roleAdministrator)},
	}
}
