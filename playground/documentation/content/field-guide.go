package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
)

func managedByAuthenticatedUsers() ridu.CollectionAccess {
	return ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	}
}

var Media = ridu.Collection{
	Slug:   "media",
	Labels: ridu.CollectionLabels{Singular: "Image", Plural: "Media"},
	Admin: ridu.CollectionAdmin{
		Group:       "Field guide",
		UseAsTitle:  "alt",
		Description: "A focused upload target for the Upload field example.",
	},
	Upload: true,
	UploadConfig: ridu.UploadConfig{
		MaxFileSize: 2 << 20,
		MimeTypes:   []string{"image/png"},
	},
	Fields: field.Fields{field.Text("alt").Label("Alt text").Required(), field.Textarea("caption")},
	Access: managedByAuthenticatedUsers(),
}

var Categories = ridu.Collection{
	Slug:   "categories",
	Labels: ridu.CollectionLabels{Singular: "Category", Plural: "Categories"},
	Admin: ridu.CollectionAdmin{
		Group:       "Field guide",
		UseAsTitle:  "name",
		Description: "A small inverse-relationship example for Join and Virtual fields.",
	},
	Fields: field.Fields{field.Text("name").Required(), field.Slug("slug", "name").Admin(field.Admin{Description: "Generated from the category name."}), field.Virtual("displayLabel", field.ValueString,

		func(ctx operation.ReadContext,

		) (operation.Value[store.
			Value],

			error) {
			name, _ := ctx.Root.Get("name").
				StringValue()
			return operation.Present(store.String("Category · " + name)), nil
		}).Label("Computed label").Admin(field.Admin{Description: "Computed by trusted Go code and never stored."}), field.Join("articles", "articles", "category").Label("Articles in this category").DefaultColumns("title", "status", "updatedAt").DefaultSort("title")},

	Access: managedByAuthenticatedUsers(),
}

var Articles = ridu.Collection{
	Slug:          "articles",
	Labels:        ridu.CollectionLabels{Singular: "Article", Plural: "Articles"},
	Versions:      true,
	VersionConfig: ridu.VersionConfig{Drafts: true},
	Admin: ridu.CollectionAdmin{
		Group:          "Field guide",
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "status", "category"},
		Description:    "Minimal related content used by relationship, upload, localization, and live-preview captures.",
		LivePreview: ridu.LivePreviewConfig{
			URL: "http://127.0.0.1:18082/preview/articles/{id}?title={field:title}",
			Breakpoints: []ridu.PreviewBreakpoint{
				{Name: "mobile", Label: "Mobile", Width: 375, Height: 667},
				{Name: "desktop", Label: "Desktop", Width: 1280, Height: 800},
			},
		},
	},
	Fields: field.Fields{field.Text("title").Required().Localized(), field.Textarea("summary").Localized(), field.Select("status", "draft", "published").Default("draft"), field.Relationship("category", "categories"), field.Upload("cover", "media"), richtext.Field("content")},
	Access: managedByAuthenticatedUsers(),
}

var FieldGuide = ridu.Collection{
	Slug:   "field-guide",
	Labels: ridu.CollectionLabels{Singular: "Field example", Plural: "Field examples"},
	Admin: ridu.CollectionAdmin{
		Group:          "Field guide",
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "status", "featured"},
		Description:    "One deliberately small document used to photograph every built-in field.",
	},
	Fields: field.Fields{field.Text("title").Required().Admin(field.Admin{Description: "A short, searchable string."}), field.Textarea("summary").Admin(field.Admin{Description: "A multi-line plain-text introduction."}), field.Email("contactEmail").Label("Contact email"), field.Number("priority").Min(0).Max(10).Step(1), field.Date("publishedAt").Label("Publication date").Format(field.DateTime), field.Checkbox("featured").Label("Feature this example"), field.MultiSelect("status").Options(
		field.Option{Value: "draft", Label: "Draft"},
		field.Option{Value: "review", Label: "In review"},
		field.Option{Value: "published", Label: "Published"}), field.Radio("tone", "neutral", "friendly", "urgent"), field.Point("location").Admin(field.Admin{Description: "Longitude and latitude, in that order."}), field.JSON("metadata").Admin(field.Admin{Description: "Structured data without a fixed nested model."}), field.Code("source").Admin(field.Admin{CodeLanguage: "typescript", Description: "Source text is stored, never executed."}), field.Slug("slug", "title").Admin(field.Admin{Description: "Generated from Title until an author overrides it."}), field.Group("seo", field.Fields{field.Text("title").Label("SEO title"), field.Textarea("description").Label("Meta description")}).Label("Search preview"), field.Array("links", field.Fields{field.Text("label").Required(), field.Text("url").Required()}).MinRows(1).MaxRows(4).Admin(field.Admin{RowLabelPath: "label"}), field.Blocks("layout", field.Block{Key: "callout", Label: "Callout", Fields: field.Fields{field.Select("tone", "note", "warning"), field.Textarea("body")}}, field.Block{Key: "quote", Label: "Quote", Fields: field.Fields{field.Textarea("quote"), field.Text("attribution")}}).Label("Page layout"), field.Relationship("author", "users").Admin(field.Admin{Description: "Select one document from Users."}), field.Upload("cover", "media").Admin(field.Admin{Description: "Select one document from the upload-enabled Media collection."}), field.Row(field.Fields{field.Text("firstName").Label("First name").Admin(field.Admin{Columns: 6}), field.Text("lastName").Label("Last name").Admin(field.Admin{Columns: 6})}), field.Collapsible("advanced", field.Fields{field.Text("internalName").Label("Internal name"), field.Textarea("editorNotes").Label("Editor notes")}).Admin(field.Admin{InitiallyCollapsed: false}), field.Tabs(field.Fields{field.UnnamedTab("Content", field.Fields{field.Text("tabIntroduction").Label("Introduction")}), field.NamedTab("settings", "Settings", field.Fields{field.Text("theme").Label("Theme")})}), field.UI("guidance").Label("Author guidance").Admin(field.Admin{Description: "Presentation-only help never enters storage or generated document types."}), field.Virtual("displayLabel", field.ValueString,

		func(ctx operation.ReadContext,

		) (operation.Value[store.
			Value],

			error) {
			title, _ := ctx.Root.Get("title").
				StringValue()
			return operation.Present(store.String("Field example · " + title)), nil
		}).Label("Computed label").Admin(field.Admin{Description: "A read-only value resolved by the application."}), richtext.Field("content").Label("Rich text").Admin(field.Admin{Description: "The official paired field-plugin example."}),
	},

	Access: managedByAuthenticatedUsers(),
}
