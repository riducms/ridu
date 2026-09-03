package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/formbuilder"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/plugins/seo"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

const (
	roleAdministrator = "administrator"
	roleEditor        = "editor"
	roleContributor   = "contributor"
	roleAPIOnly       = "api-only"
)

func fixtureConfig(uploadStorage storage.Backend) ridu.Config {
	return ridu.Config{
		Name: "Ridu editorial kitchen sink",
		NameTranslations: map[string]string{
			"fr": "Cuisine éditoriale Ridu",
			"ar": "مطبخ ريدو التحريري",
		},
		Admin: ridu.AdminConfig{
			User: "users",
			Localization: ridu.AdminLocalizationConfig{
				Languages: []ridu.AdminLanguage{
					{Code: "en", Label: "English", LabelTranslations: map[string]string{"fr": "Anglais", "ar": "الإنجليزية"}},
					{Code: "fr", Label: "Français"},
					{Code: "ar", Label: "العربية", RTL: true},
				},
				DefaultLanguage: "en",
				TimeZones: []ridu.AdminTimeZone{
					{ID: "UTC", Label: "UTC"},
					{ID: "Europe/London", Label: "London", LabelTranslations: map[string]string{"fr": "Londres", "ar": "لندن"}},
					{ID: "Europe/Paris", Label: "Paris", LabelTranslations: map[string]string{"ar": "باريس"}},
				},
				DefaultTimeZone: "Europe/London",
			},
		},
		Plugins: []ridu.Plugin{
			adminContractPlugin{},
			richtext.New(),
			seo.New(seo.Config{
				Collections:         []schema.CollectionSlug{"pages"},
				Globals:             []schema.CollectionSlug{"site-settings"},
				UploadsCollection:   "media",
				TabbedUI:            true,
				GenerateTitle:       generateSEOTitle,
				GenerateDescription: generateSEODescription,
				GenerateImage:       generateSEOImage,
				GenerateURL:         generateSEOURL,
			}),
			formbuilder.New(formbuilder.Config{
				EnabledFields: []formbuilder.FieldType{
					formbuilder.FieldText, formbuilder.FieldTextarea, formbuilder.FieldEmail,
					formbuilder.FieldNumber, formbuilder.FieldCheckbox, formbuilder.FieldSelect,
					formbuilder.FieldRadio, formbuilder.FieldDate, formbuilder.FieldCountry,
					formbuilder.FieldState, formbuilder.FieldMessage, formbuilder.FieldUpload,
					formbuilder.FieldPayment,
				},
				UploadCollections:     []schema.CollectionSlug{"media"},
				RedirectRelationships: []schema.CollectionSlug{"pages"},
				PaymentProcessors:     []field.Choice{{Value: "fixture", Label: "Fixture processor"}},
				HandlePayment: func(context formbuilder.PaymentContext) (store.Value, error) {
					return store.Object(store.Values{"processor": store.String("fixture"), "total": store.Number(context.Total)}), nil
				},
			}),
		},
		Storage:          uploadStorage,
		StorageNamespace: "admin-fixture",
		Localization: ridu.LocalizationConfig{
			DefaultLocale: "en",
			Locales: []ridu.Locale{
				{Code: "en", Label: "English"},
				{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
				{Code: "ar", Label: "Arabic", RTL: true, FallbackLocales: []schema.LocaleCode{"en"}},
			},
		},
		Collections: []ridu.Collection{
			usersCollection,
			mediaCollection,
			postsCollection(),
			foldersCollection,
			categoriesCollection,
			pagesCollection,
			eventsCollection,
			editorialNotesCollection,
			redirectsCollection,
			payloadOnlyCapabilitiesCollection,
		},
		Globals: []ridu.Global{siteSettingsGlobal},
	}
}

func generateSEOTitle(context seo.GenerateContext) (string, error) {
	title := firstString(context.Document, "title", "siteName")
	if title == "" {
		return "Ridu editorial kitchen sink", nil
	}
	return title + " | Ridu", nil
}

func generateSEODescription(context seo.GenerateContext) (string, error) {
	title := firstString(context.Document, "title", "siteName")
	if title == "" {
		title = "this editorial experience"
	}
	return "Explore " + title + " in Ridu's faithful Payload-style SEO authoring fixture, including generated metadata and live search previews.", nil
}

func generateSEOImage(context seo.GenerateContext) (string, error) {
	return firstNestedString(context.Document["layout"], "image"), nil
}

func generateSEOURL(context seo.GenerateContext) (string, error) {
	locale := strings.TrimSpace(string(context.Locale))
	if locale == "" {
		locale = "en"
	}
	if context.Collection != nil {
		slug := firstString(context.Document, "slug")
		if slug == "" {
			slug = context.ID
		}
		return "https://riducms.test/" + locale + "/pages/" + slug, nil
	}
	return "https://riducms.test/" + locale, nil
}

func firstString(document map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := document[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNestedString(value any, key string) string {
	switch candidate := value.(type) {
	case []any:
		for _, item := range candidate {
			if result := firstNestedString(item, key); result != "" {
				return result
			}
		}
	case map[string]any:
		if result, ok := candidate[key].(string); ok && result != "" {
			return result
		}
		for _, item := range candidate {
			if result := firstNestedString(item, key); result != "" {
				return result
			}
		}
	}
	return ""
}

func bootstrapFixtureConfig() ridu.Config {
	return ridu.Config{
		Name:  "Ridu first-user setup fixture",
		Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug:   "users",
			Labels: ridu.CollectionLabels{Singular: "User", Plural: "Users"},
			Admin:  ridu.CollectionAdmin{UseAsTitle: "name"},
			Auth:   true,
			Fields: []field.Definition{
				field.Text("name", field.Required(), field.Description("The name shown in the admin.")),
				field.Email("email", field.Required(), field.Unique(), field.Description("The identity used to sign in.")),
				field.Text("role", field.Required(), field.Default("administrator")),
			},
			Access: ridu.CollectionAccess{
				Admin:  allowAuthenticated,
				Read:   allowAuthenticated,
				Update: allowAuthenticated,
				Delete: allowAuthenticated,
			},
			FieldAccess: map[string]ridu.FieldAccess{
				"role": {Create: func(ctx ridu.FieldAccessContext) (bool, error) { return ctx.Actor != nil, nil }},
			},
		}},
	}
}

var siteSettingsGlobal = ridu.Global{
	Slug: "site-settings", Label: "Site settings", Versions: true,
	Admin:         ridu.GlobalAdmin{Group: "Comparison reference", Description: "Site-wide identity, announcements, and navigation."},
	VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10, AutosaveInterval: 30 * time.Second},
	Fields: []field.Definition{
		field.Text("siteName", field.Label("Site name"), field.Required()),
		field.Textarea("announcement", field.Localized(), field.Description("A global announcement shown across the site.")),
		field.Array("navigation", field.Fields(
			field.Text("label", field.Required()),
			field.Relationship("page", field.To("pages"), field.Required()),
		)),
	},
	Access: ridu.GlobalAccess{Read: allowAuthenticated, Update: allowRoles(roleAdministrator, roleEditor)},
	FieldHooks: map[string]ridu.CollectionHooks{
		"siteName": {BeforeValidate: []ridu.Hook{trimStringField("siteName")}},
	},
}

var usersCollection = ridu.Collection{
	Slug:   "users",
	Labels: ridu.CollectionLabels{Singular: "User", Plural: "Users"},
	Admin:  ridu.CollectionAdmin{Group: "Access control", Description: "People who can sign in to the admin."},
	Auth:   true,
	AuthConfig: ridu.AuthConfig{
		SessionDuration: 8 * time.Hour,
		PasswordReset: ridu.PasswordResetConfig{
			Send: func(context.Context, ridu.PasswordResetNotification) error { return nil },
		},
		APIKeys: true,
	},
	Fields: []field.Definition{
		field.Text("name", field.Required(), field.Description("The name shown on authored content.")),
		field.Text("email", field.Required(), field.Unique(), field.Description("The identity used to sign in.")),
		field.Row(
			field.Select("role", field.Required(), field.Default(roleContributor), field.Columns(6), field.Choices(
				field.Choice{Value: roleAdministrator, Label: "Administrator"},
				field.Choice{Value: roleEditor, Label: "Editor"},
				field.Choice{Value: roleContributor, Label: "Contributor"},
				field.Choice{Value: roleAPIOnly, Label: "API only"},
			)),
			field.Email("contactEmail", field.Label("Public contact email"), field.Columns(6)),
		),
		field.Tabs(
			field.NamedTab("profile", "Profile",
				field.Textarea("bio", field.Description("Short biography displayed on author pages.")),
				field.Row(
					field.Text("location", field.Columns(6)),
					field.Text("timezone", field.Columns(6), field.Default("Europe/London")),
				),
				field.Checkbox("availableForReview", field.Label("Available for review"), field.Default(true)),
			),
			field.UnnamedTab("Security",
				field.Textarea("privateNotes", field.Description("Visible only to administrators.")),
			),
		),
	},
	Access: ridu.CollectionAccess{
		Admin:  allowRoles(roleAdministrator, roleEditor, roleContributor),
		Create: allowRoles(roleAdministrator, roleEditor),
		Read:   allowAuthenticated,
		Update: allowSelfOrRoles(roleAdministrator, roleEditor),
		Delete: allowRoles(roleAdministrator),
	},
	FieldAccess: map[string]ridu.FieldAccess{
		"role": {
			Create: allowUserRoleCreate,
			Update: allowUserRoleUpdate,
		},
		"privateNotes": {
			Create: allowFieldRoles(roleAdministrator),
			Read:   allowFieldRoles(roleAdministrator),
			Update: allowFieldRoles(roleAdministrator),
		},
	},
}

var mediaCollection = ridu.Collection{
	Slug:   "media",
	Trash:  true,
	Labels: ridu.CollectionLabels{Singular: "Asset", Plural: "Media"},
	Admin:  ridu.CollectionAdmin{Group: "Content", Description: "Images and reusable media assets."},
	Upload: true,
	UploadConfig: ridu.UploadConfig{
		MaxFileSize: 5 << 20,
		MimeTypes:   []string{"image/png", "image/jpeg"},
		ImageSizes: []ridu.ImageSize{
			{Name: "card", Width: 640, Height: 360, Fit: "cover"},
			{Name: "thumbnail", Width: 160, Height: 160, Fit: "cover"},
		},
	},
	Fields: []field.Definition{
		field.Text("alt", field.Label("Alt text"), field.Required(), field.Description("Describe the image for people who cannot see it.")),
		field.Textarea("caption", field.Description("Optional editorial caption.")),
		field.Row(
			field.Relationship("credit", field.To("users"), field.Columns(6)),
			field.Select("kind", field.Columns(6), field.Default("image"), field.OneOf("image", "illustration", "screenshot")),
		),
		field.Array("tags", field.Fields(field.Text("label", field.Required()))),
	},
	Access: ridu.CollectionAccess{
		Read:   allowEveryone,
		Create: allowRoles(roleAdministrator, roleEditor),
		Update: allowRoles(roleAdministrator, roleEditor),
		Delete: allowRoles(roleAdministrator),
	},
}

var categoriesCollection = ridu.Collection{
	Slug:   "categories",
	Labels: ridu.CollectionLabels{Singular: "Category", Plural: "Categories"},
	Admin:  ridu.CollectionAdmin{Group: "Taxonomy", Description: "Categories used to organize editorial content."},
	Fields: []field.Definition{
		field.Text("name", field.Required()),
		field.Slug("slug", "name", field.Description("Generated from the category name until an author supplies a manual route slug.")),
		field.Virtual("displayLabel", field.ValueString, field.Label("Computed label"), field.Description("Resolved by executable Go configuration and never stored.")),
		field.Join(
			"posts",
			"posts",
			"category",
			field.Label("Posts in this category"),
			field.JoinLimit(5),
			field.JoinColumns("title", "status", "author", "updatedAt"),
			field.JoinDefaultSort("title"),
		),
		field.Tabs(field.NamedTab("seo", "SEO",
			field.Text("title"),
			field.Textarea("description"),
		)),
	},
	Computed: map[string]ridu.Computed{
		"displayLabel": func(ctx ridu.ComputedContext) (store.Value, error) {
			name, _ := ctx.Document.Values["name"].StringValue()
			return store.String("Category · " + name), nil
		},
	},
	Access: staffManagedPublicReadAccess(),
}

var foldersCollection = ridu.Collection{
	Slug:   "folders",
	Labels: ridu.CollectionLabels{Singular: "Folder", Plural: "Folders"},
	Admin: ridu.CollectionAdmin{
		UseAsTitle:  "name",
		Group:       "Content",
		Description: "Reusable editorial folders. Posts reference these records through their configured folder field.",
		ParentField: "parent",
	},
	Fields: []field.Definition{
		field.Text("name", field.Required(), field.Unique()),
		field.Relationship("parent", field.To("folders"), field.Description("Optional parent folder.")),
	},
	Access: staffManagedPublicReadAccess(),
}

func postsCollection() ridu.Collection {
	previewAddress := os.Getenv("RIDU_BROWSER_PREVIEW_ADDRESS")
	if previewAddress == "" {
		previewAddress = "127.0.0.1:18082"
	}
	return ridu.Collection{
		Slug:               "posts",
		Trash:              true,
		LockDocuments:      true,
		DocumentLockConfig: ridu.DocumentLockConfig{Duration: 2 * time.Minute},
		Labels: ridu.CollectionLabels{
			Singular: "Post", Plural: "Posts",
			SingularTranslations: map[string]string{"fr": "Article", "ar": "مقال"},
			PluralTranslations:   map[string]string{"fr": "Articles", "ar": "المقالات"},
		},
		Admin: ridu.CollectionAdmin{
			UseAsTitle:        "title",
			DefaultColumns:    []string{"title", "status", "readingMinutes", "folder"},
			Group:             "Content",
			GroupTranslations: map[string]string{"fr": "Contenu", "ar": "المحتوى"},
			Description:       "Versioned editorial content with folders, hierarchy, access rules, and hooks.",
			DescriptionTranslations: map[string]string{
				"fr": "Contenu éditorial versionné avec dossiers, hiérarchie, accès et hooks.",
				"ar": "محتوى تحريري بإصدارات ومجلدات وتسلسل هرمي وقواعد وصول.",
			},
			FolderField: "folder",
			ParentField: "parent",
			LivePreview: ridu.LivePreviewConfig{
				URL: "http://" + previewAddress + "/preview/posts/{id}?slug={field:slug}",
				Breakpoints: []ridu.PreviewBreakpoint{
					{Name: "mobile", Label: "Mobile", Width: 375, Height: 667},
					{Name: "tablet", Label: "Tablet", Width: 768, Height: 1024},
					{Name: "desktop", Label: "Desktop", Width: 1440, Height: 900},
				},
			},
		},
		Versions:      true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 20, AutosaveInterval: 15 * time.Second},
		Fields: []field.Definition{
			field.Text(
				"title",
				field.LabelTranslations(map[string]string{"fr": "Titre", "ar": "العنوان"}),
				field.Required(),
				field.Localized(),
				field.Description("Primary label used throughout the admin."),
				field.DescriptionTranslations(map[string]string{
					"fr": "Libellé principal utilisé dans toute l’administration.",
					"ar": "العنوان الرئيسي المستخدم في لوحة الإدارة.",
				}),
			),
			field.Text("slug", field.Unique(), field.Description("Authored separately because this fixture's title is localized.")),
			field.Textarea("summary", field.Required(), field.Localized(), field.Description("A concise introduction for listings and previews.")),
			field.Row(
				field.Relationship("author", field.To("users"), field.Columns(6)),
				field.Relationship("category", field.To("categories"), field.Columns(6)),
			),
			field.Row(
				field.Relationship("folder", field.To("folders"), field.Columns(6)),
				field.Relationship("parent", field.To("posts"), field.Columns(6), field.Description("Optional parent used by the hierarchy view.")),
			),
			field.Row(
				field.Select("status", field.Columns(4), field.Default("draft"), field.Choices(
					(field.Choice{Value: "draft", Label: "Draft"}).WithLabelTranslations(map[string]string{"fr": "Brouillon", "ar": "مسودة"}),
					(field.Choice{Value: "published", Label: "Published"}).WithLabelTranslations(map[string]string{"fr": "Publié", "ar": "منشور"}),
				)),
				field.Checkbox("featured", field.Columns(4), field.Default(false)),
				field.Number("readingMinutes", field.Label("Reading time (minutes)"), field.Columns(4), field.Default(5)),
			),
			field.Date("publishDate", field.Label("Publication date"), field.PickerAppearance(field.DatePickerDayAndTime), field.ShowWhen("status", "published"), field.Description("Conditional presentation is not authorization.")),
			field.Upload("cover", field.To("media"), field.Description("Select the lead image used in post previews.")),
			field.Upload("gallery", field.Label("Image gallery"), field.ToMany("media")),
			richtext.Field("content", field.Localized(), field.Description("Write the main body of the post.")),
			field.Relationship("collaborators", field.ToMany("users")),
			field.Relationship("relatedPosts", field.Label("Related posts"), field.ToMany("posts"), field.Description("A conventional has-many relationship used by the browser contract.")),
			field.Relationship(
				"publishedPage",
				field.Label("Published page"),
				field.To("pages"),
				field.FilterOptionRules(field.OptionFilterValue("_status", field.FilterEquals, "published")),
				field.Description("A literal server-enforced option filter excludes draft pages."),
			),
			field.Relationship(
				"sameCategoryPosts",
				field.Label("Posts from this category"),
				field.ToMany("posts"),
				field.FilterOptionRules(field.OptionFilter("category", field.FilterEquals, "category")),
				field.FilterOptionRules(field.OptionFilter("seo.title", field.FilterEquals, "seo.title")),
				field.Description("Options share the current category and nested SEO title."),
			),
			field.Relationship(
				"relatedContent",
				field.Label("Related content"),
				field.ToAny("posts", "pages"),
				field.FilterOptionRules(
					field.OptionFilterFor("posts", "category", field.FilterEquals, "category"),
					field.OptionFilterFor("pages", "navigation.showInHeader", field.FilterEquals, "featured"),
				),
				field.Description("Polymorphic options use target-specific predicates from current document values."),
			),
			field.Tabs(field.NamedTab("seo", "SEO",
				field.Text("title", field.Columns(6)),
				field.Text("canonicalURL", field.Label("Canonical URL"), field.Columns(6)),
				field.Textarea("description"),
				field.Upload("socialImage", field.Label("Social image"), field.To("media")),
			)),
			field.Tabs(
				field.UnnamedTab("Related",
					field.Array("links", field.Fields(
						field.Row(
							field.Text("label", field.Required(), field.Columns(5)),
							field.Text("url", field.Required(), field.Columns(7)),
						),
						field.Checkbox("newWindow", field.Label("Open in a new window"), field.Default(false)),
					)),
				),
				field.UnnamedTab("Layout",
					field.Blocks("layout", field.BlockTypes(
						field.BlockType("callout", "Callout",
							field.Select("tone", field.Default("note"), field.OneOf("note", "warning", "success")),
							field.Textarea("body", field.Required()),
						),
						field.BlockType("quote", "Quote",
							field.Textarea("quote", field.Required()),
							field.Text("attribution"),
						),
						field.BlockType("related-posts", "Related posts",
							field.Relationship("posts", field.ToMany("posts"), field.Required()),
						),
					)),
				),
				field.UnnamedTab("Advanced",
					field.JSON("metadata", field.Description("Arbitrary JSON exercises the raw JSON editor.")),
					field.Textarea("internalNotes", field.Description("Administrators can read this field; editors may write it but receive a redacted response.")),
					field.Relationship("lastEditedBy", field.Label("Last edited by"), field.To("users"), field.ReadOnly()),
				),
			),
		},
		Access: ridu.CollectionAccess{
			Create: allowAuthenticated,
			Read:   postReadAccess,
			Update: allowOwnedOrRoles("author", roleAdministrator, roleEditor),
			Delete: allowOwnedOrRoles("author", roleAdministrator),
		},
		FieldAccess: map[string]ridu.FieldAccess{
			"status": {
				Create: allowDraftEditorialStatus,
				Update: allowFieldRoles(roleAdministrator, roleEditor),
			},
			"internalNotes": {
				Read:   allowFieldRoles(roleAdministrator),
				Create: allowFieldRoles(roleAdministrator, roleEditor),
				Update: allowFieldRoles(roleAdministrator, roleEditor),
			},
		},
		FieldHooks: map[string]ridu.CollectionHooks{
			"title": {BeforeValidate: []ridu.Hook{trimStringField("title")}},
		},
		Hooks: ridu.CollectionHooks{
			BeforeDuplicate: []ridu.Hook{preparePostDuplicate},
			BeforeChange:    []ridu.Hook{recordLastEditor},
		},
	}
}

func preparePostDuplicate(ctx ridu.HookContext) error {
	if ctx.Operation != ridu.OperationDuplicate {
		return nil
	}
	title, _ := ctx.Data["title"].StringValue()
	ctx.Data["title"] = store.String("Copy of " + title)
	delete(ctx.Data, "slug")
	return nil
}

var pagesCollection = ridu.Collection{
	Slug:          "pages",
	Labels:        ridu.CollectionLabels{Singular: "Page", Plural: "Pages"},
	Admin:         ridu.CollectionAdmin{Group: "Editorial", Description: "Structured, versioned pages assembled from blocks."},
	Versions:      true,
	VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10, AutosaveInterval: 30 * time.Second},
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Slug("slug", "title"),
		field.Select("template", field.Default("default"), field.OneOf("default", "landing", "legal")),
		field.Blocks("layout", field.Required(), field.BlockTypes(
			field.BlockType("hero", "Hero",
				field.Text("heading", field.Required()),
				field.Textarea("lede"),
				field.Upload("image", field.To("media")),
			),
			field.BlockType("rich-text", "Rich text", richtext.Field("body", field.Required())),
			field.BlockType("featured-post", "Featured post", field.Relationship("post", field.To("posts"), field.Required())),
		)),
		field.Tabs(field.NamedTab("navigation", "Navigation",
			field.Text("label"),
			field.Checkbox("showInHeader", field.Label("Show in header"), field.Default(false)),
			field.Checkbox("showInFooter", field.Label("Show in footer"), field.Default(false)),
		)),
	},
	Access: ridu.CollectionAccess{
		Create:       allowRoles(roleAdministrator, roleEditor),
		Read:         allowEveryone,
		ReadVersions: allowRoles(roleAdministrator, roleEditor),
		Update:       allowRoles(roleAdministrator, roleEditor),
		Delete:       allowRoles(roleAdministrator),
	},
}

var eventsCollection = ridu.Collection{
	Slug:   "events",
	Labels: ridu.CollectionLabels{Singular: "Event", Plural: "Events"},
	Admin:  ridu.CollectionAdmin{Group: "Engagement", Description: "Scheduled online and in-person events."},
	Fields: []field.Definition{
		field.Text("name", field.Required()),
		field.Row(
			field.Date("startsAt", field.Label("Starts at"), field.PickerAppearance(field.DatePickerDayAndTime), field.Required(), field.Columns(6)),
			field.Date("endsAt", field.Label("Ends at"), field.PickerAppearance(field.DatePickerDayAndTime), field.Columns(6)),
		),
		field.Row(
			field.Number("capacity", field.Columns(6), field.Default(50)),
			field.Checkbox("online", field.Columns(6), field.Default(false)),
		),
		field.Text("venue", field.ShowWhenCondition(field.Sibling("online", field.ConditionEquals, false))),
		field.Text("meetingURL", field.Label("Meeting URL"), field.ShowWhenCondition(field.Sibling("online", field.ConditionEquals, true))),
		field.Email("contact", field.Label("Contact email"), field.Required()),
		field.Array("schedule", field.Fields(
			field.Date("time", field.PickerAppearance(field.DatePickerDayAndTime), field.Required()),
			field.Text("title", field.Required()),
			field.Relationship("speaker", field.To("users")),
		)),
		field.JSON("registrationSettings"),
	},
	Access: staffManagedPublicReadAccess(),
}

var editorialNotesCollection = ridu.Collection{
	Slug:   "editorial-notes",
	Labels: ridu.CollectionLabels{Singular: "Editorial note", Plural: "Editorial notes"},
	Admin:  ridu.CollectionAdmin{Group: "Workflow", Description: "Private notes scoped by ownership and role."},
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Relationship("owner", field.To("users"), field.Required()),
		field.Textarea("note", field.Required()),
		field.Textarea("confidentialDetails", field.Label("Confidential details")),
	},
	Access: ridu.CollectionAccess{
		Create: allowAuthenticated,
		Read:   allowOwnedOrRoles("owner", roleAdministrator, roleEditor),
		Update: allowOwnedOrRoles("owner", roleAdministrator, roleEditor),
		Delete: allowOwnedOrRoles("owner", roleAdministrator),
	},
	FieldAccess: map[string]ridu.FieldAccess{
		"confidentialDetails": {
			Read:   allowFieldRoles(roleAdministrator),
			Create: allowFieldRoles(roleAdministrator),
			Update: allowFieldRoles(roleAdministrator),
		},
	},
}

var redirectsCollection = ridu.Collection{
	Slug:   "redirects",
	Labels: ridu.CollectionLabels{Singular: "Redirect", Plural: "Redirects"},
	Admin:  ridu.CollectionAdmin{Group: "Operations", Description: "Path redirects managed by the editorial team."},
	Fields: []field.Definition{
		field.Text("from", field.Label("From path"), field.Required(), field.Unique()),
		field.Text("to", field.Label("Destination"), field.Required()),
		field.Select("type", field.Default("permanent"), field.OneOf("permanent", "temporary")),
		field.Checkbox("enabled", field.Default(true)),
	},
	Access: ridu.CollectionAccess{
		Create: allowRoles(roleAdministrator, roleEditor),
		Read:   allowRoles(roleAdministrator, roleEditor),
		Update: allowRoles(roleAdministrator, roleEditor),
		Delete: allowRoles(roleAdministrator),
	},
}

var payloadOnlyCapabilitiesCollection = ridu.Collection{
	Slug:   "payload-only-capabilities",
	Labels: ridu.CollectionLabels{Singular: "Field showcase", Plural: "Field showcase"},
	Admin:  ridu.CollectionAdmin{Group: "Comparison reference", Description: "Less common field and presentation contracts exercised together."},
	Fields: []field.Definition{
		field.Text("title", field.Required(), field.Placeholder("Name this field showcase"), field.PlaceholderTranslations(map[string]string{"fr": "Nommez cette vitrine de champs", "ar": "سمّ عرض الحقول هذا"})),
		field.Code("code", field.Language("typescript"), field.Description("A monospaced source editor with a language hint.")),
		field.Select("audiences", field.Label("Audiences"), field.Required(), field.Multiple(), field.DefaultChoices("editors"), field.Choices(
			field.Choice{Value: "administrators", Label: "Administrators"},
			field.Choice{Value: "editors", Label: "Editors"},
			field.Choice{Value: "reviewers", Label: "Reviewers"},
		)),
		field.Radio("priority", field.Required(), field.Default("normal"), field.Sidebar(), field.Choices(
			field.Choice{Value: "low", Label: "Low"},
			field.Choice{Value: "normal", Label: "Normal"},
			field.Choice{Value: "high", Label: "High"},
		)),
		field.Point("location", field.Sidebar(), field.Description("Longitude and latitude, in that order.")),
		field.UI("fieldGuide", field.Label("Field guide"), field.Description("This content is presentation-only and never enters APIs, storage, or generated document types.")),
		field.Collapsible("advancedSettings", true,
			field.Text("internalName", field.Label("Internal name"), field.Hidden()),
			field.Textarea("implementationNotes", field.Placeholder("Record implementation details")),
		),
		field.Array("team", field.MinRows(1), field.MaxRows(3), field.RowLabel("displayName"), field.Fields(
			field.Text("displayName", field.Required()),
			field.Email("email"),
			field.Radio("role", field.Default("reviewer"), field.OneOf("author", "reviewer")),
		)),
		field.Virtual("fieldSummary", field.ValueString, field.Sidebar(), field.Label("Computed summary"), field.Description("A read-only value derived from the stored title and priority.")),
		field.Array("curriculumNodes", field.Label("Curriculum nodes"),
			field.RowLabelComponent("contract-route", "compositeRowLabel", json.RawMessage(`{"kind":"typedOrder","typePath":"nodeType","targets":{"lesson":"lesson","question":"question"},"orderPath":"orderIndex"}`)),
			field.Fields(
				field.Select("nodeType", field.OneOf("lesson", "question")),
				field.Text("lesson"),
				field.Text("question"),
				field.Number("orderIndex"),
			),
		),
		field.Blocks("questionParts", field.Label("Question parts"),
			field.RowLabelComponent("contract-route", "compositeRowLabel", json.RawMessage(`{"kind":"keyLabel","keyPath":"optionKey","labelPath":"label"}`)),
			field.BlockTypes(
				field.BlockType("choice", "Choice", field.Text("optionKey"), field.Text("label")),
				field.BlockType("reorder", "Reorder", field.Text("optionKey"), field.Text("label")),
			),
		),
	},
	Computed: map[string]ridu.Computed{
		"fieldSummary": func(ctx ridu.ComputedContext) (store.Value, error) {
			title, _ := ctx.Document.Values["title"].StringValue()
			priority, _ := ctx.Document.Values["priority"].StringValue()
			return store.String(title + " · " + priority), nil
		},
	},
	Access: staffManagedPublicReadAccess(),
}

type adminContractPlugin struct{}

func (adminContractPlugin) Key() string { return "contract-route" }

func (adminContractPlugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: "0.1.0", GoPackage: "github.com/riducms/ridu/tests/contracts/admin_server",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion, MaximumExclusive: "0.2.0"},
		Admin: &ridu.AdminPluginMetadata{
			Package: "@riducms/admin-contract-fixture", Export: "contractPlugin",
			APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: 1,
			Routes: []string{"plugin-contract"},
		},
	}
}

func staffManagedPublicReadAccess() ridu.CollectionAccess {
	return ridu.CollectionAccess{
		Create: allowRoles(roleAdministrator, roleEditor),
		Read:   allowEveryone,
		Update: allowRoles(roleAdministrator, roleEditor),
		Delete: allowRoles(roleAdministrator),
	}
}

func allowEveryone(ridu.AccessContext) (ridu.AccessDecision, error) {
	return ridu.Allow(), nil
}

func allowAuthenticated(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	return ridu.Allow(), nil
}

func allowRoles(roles ...string) ridu.AccessRule {
	return func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if actorHasRole(ctx.Actor, roles...) {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}
}

func allowSelfOrRoles(roles ...string) ridu.AccessRule {
	return func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if actorHasRole(ctx.Actor, roles...) || ctx.Actor != nil && ctx.ID == ctx.Actor.ID {
			return ridu.Allow(), nil
		}
		return ridu.Deny(), nil
	}
}

func allowOwnedOrRoles(path string, roles ...string) ridu.AccessRule {
	ownerPath := mustPath(path)
	return func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if actorHasRole(ctx.Actor, roles...) {
			return ridu.Allow(), nil
		}
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal(ownerPath, query.String(ctx.Actor.ID))), nil
	}
}

func postReadAccess(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	statusPath := mustPath("status")
	published := query.Equal(statusPath, query.String("published"))
	if ctx.Actor == nil {
		return ridu.Where(published), nil
	}
	if actorHasRole(ctx.Actor, roleAdministrator, roleEditor) {
		return ridu.Allow(), nil
	}
	owned := query.Equal(mustPath("author"), query.String(ctx.Actor.ID))
	visible, err := query.Or(published, owned)
	if err != nil {
		return ridu.Deny(), err
	}
	return ridu.Where(visible), nil
}

func allowFieldRoles(roles ...string) ridu.FieldAccessRule {
	return func(ctx ridu.FieldAccessContext) (bool, error) {
		return actorHasRole(ctx.Actor, roles...), nil
	}
}

func allowDraftEditorialStatus(ctx ridu.FieldAccessContext) (bool, error) {
	if actorHasRole(ctx.Actor, roleAdministrator, roleEditor) {
		return true, nil
	}
	status, valid := ctx.Value.StringValue()
	return valid && status == "draft", nil
}

func allowUserRoleCreate(ctx ridu.FieldAccessContext) (bool, error) {
	if actorHasRole(ctx.Actor, roleAdministrator) {
		return true, nil
	}
	role, valid := ctx.Value.StringValue()
	return actorHasRole(ctx.Actor, roleEditor) && valid && role == roleContributor, nil
}

func allowUserRoleUpdate(ctx ridu.FieldAccessContext) (bool, error) {
	if actorHasRole(ctx.Actor, roleAdministrator) {
		return true, nil
	}
	if ctx.Original == nil {
		return false, nil
	}
	next, nextValid := ctx.Value.StringValue()
	currentValue, exists := ctx.Original.Values["role"]
	current, currentValid := currentValue.StringValue()
	return exists && nextValid && currentValid && next == current, nil
}

func actorHasRole(actor *store.Document, roles ...string) bool {
	if actor == nil {
		return false
	}
	roleValue, exists := actor.Values["role"]
	if !exists {
		return false
	}
	role, valid := roleValue.StringValue()
	if !valid {
		return false
	}
	for _, allowed := range roles {
		if role == allowed {
			return true
		}
	}
	return false
}

func trimStringField(name string) ridu.Hook {
	return func(ctx ridu.HookContext) error {
		value, exists := ctx.Data[name]
		if !exists {
			return nil
		}
		text, valid := value.StringValue()
		if valid {
			ctx.Data[name] = store.String(strings.TrimSpace(text))
		}
		return nil
	}
}

func recordLastEditor(ctx ridu.HookContext) error {
	if ctx.Actor != nil && (ctx.Operation == ridu.OperationCreate || ctx.Operation == ridu.OperationUpdate) {
		ctx.Data["lastEditedBy"] = store.String(ctx.Actor.ID)
	}
	return nil
}

func mustPath(value string) query.Path {
	path, err := query.ParsePath(value)
	if err != nil {
		panic(err)
	}
	return path
}
