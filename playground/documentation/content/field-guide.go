package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
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
	Fields: []field.Definition{
		field.Text("alt", field.Label("Alt text"), field.Required()),
		field.Textarea("caption"),
	},
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
	Fields: []field.Definition{
		field.Text("name", field.Required()),
		field.Slug("slug", "name", field.Description("Generated from the category name.")),
		field.Virtual("displayLabel", field.ValueString,
			field.Label("Computed label"),
			field.Description("Computed by trusted Go code and never stored."),
		),
		field.Join("articles", "articles", "category",
			field.Label("Articles in this category"),
			field.JoinColumns("title", "status", "updatedAt"),
			field.JoinDefaultSort("title"),
		),
	},
	Computed: map[string]ridu.Computed{
		"displayLabel": func(ctx ridu.ComputedContext) (store.Value, error) {
			name, _ := ctx.Document.Values["name"].StringValue()
			return store.String("Category · " + name), nil
		},
	},
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
	Fields: []field.Definition{
		field.Text("title", field.Required(), field.Localized()),
		field.Textarea("summary", field.Localized()),
		field.Select("status", field.Default("draft"), field.OneOf("draft", "published")),
		field.Relationship("category", field.To("categories")),
		field.Upload("cover", field.To("media")),
		richtext.Field("content"),
	},
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
	Fields: []field.Definition{
		field.Text("title", field.Required(), field.Description("A short, searchable string.")),
		field.Textarea("summary", field.Description("A multi-line plain-text introduction.")),
		field.Email("contactEmail", field.Label("Contact email")),
		field.Number("priority", field.Min(0), field.Max(10), field.Step(1)),
		field.Date("publishedAt", field.Label("Publication date"), field.PickerAppearance(field.DatePickerDayAndTime)),
		field.Checkbox("featured", field.Label("Feature this example")),
		field.Select("status", field.Multiple(), field.Choices(
			field.Choice{Value: "draft", Label: "Draft"},
			field.Choice{Value: "review", Label: "In review"},
			field.Choice{Value: "published", Label: "Published"},
		)),
		field.Radio("tone", field.OneOf("neutral", "friendly", "urgent")),
		field.Point("location", field.Description("Longitude and latitude, in that order.")),
		field.JSON("metadata", field.Description("Structured data without a fixed nested model.")),
		field.Code("source", field.Language("typescript"), field.Description("Source text is stored, never executed.")),
		field.Slug("slug", "title", field.Description("Generated from Title until an author overrides it.")),
		field.Group("seo", field.Label("Search preview"), field.Fields(
			field.Text("title", field.Label("SEO title")),
			field.Textarea("description", field.Label("Meta description")),
		)),
		field.Array("links", field.MinRows(1), field.MaxRows(4), field.RowLabel("label"), field.Fields(
			field.Text("label", field.Required()),
			field.Text("url", field.Required()),
		)),
		field.Blocks("layout", field.Label("Page layout"), field.BlockTypes(
			field.BlockType("callout", "Callout", field.Select("tone", field.OneOf("note", "warning")), field.Textarea("body")),
			field.BlockType("quote", "Quote", field.Textarea("quote"), field.Text("attribution")),
		)),
		field.Relationship("author", field.To("users"), field.Description("Select one document from Users.")),
		field.Upload("cover", field.To("media"), field.Description("Select one document from the upload-enabled Media collection.")),
		field.Row(
			field.Text("firstName", field.Label("First name"), field.Columns(6)),
			field.Text("lastName", field.Label("Last name"), field.Columns(6)),
		),
		field.Collapsible("advanced", false,
			field.Text("internalName", field.Label("Internal name")),
			field.Textarea("editorNotes", field.Label("Editor notes")),
		),
		field.Tabs(
			field.UnnamedTab("Content", field.Text("tabIntroduction", field.Label("Introduction"))),
			field.NamedTab("settings", "Settings", field.Text("theme", field.Label("Theme"))),
		),
		field.UI("guidance", field.Label("Author guidance"), field.Description("Presentation-only help never enters storage or generated document types.")),
		field.Virtual("displayLabel", field.ValueString,
			field.Label("Computed label"),
			field.Description("A read-only value resolved by the application."),
		),
		richtext.Field("content", field.Label("Rich text"), field.Description("The official paired field-plugin example.")),
	},
	Computed: map[string]ridu.Computed{
		"displayLabel": func(ctx ridu.ComputedContext) (store.Value, error) {
			title, _ := ctx.Document.Values["title"].StringValue()
			return store.String("Field example · " + title), nil
		},
	},
	Access: managedByAuthenticatedUsers(),
}
