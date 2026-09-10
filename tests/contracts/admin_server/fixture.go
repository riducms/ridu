package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/formbuilder"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/plugins/seo"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/dynamicdefaults"
	embeddedplugin "github.com/riducms/ridu/tests/contracts/embedded_plugin"
	"github.com/riducms/ridu/tests/contracts/issuetargets"
	"github.com/riducms/ridu/tests/contracts/livevalidation"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
	"github.com/riducms/ridu/tests/contracts/unifiedfields"
)

const (
	roleAdministrator = "administrator"
	roleEditor        = "editor"
	roleContributor   = "contributor"
	roleAPIOnly       = "api-only"
)

func fixtureConfig(uploadStorage storage.Backend) ridu.Config {
	return withBlockReferenceFixture(ridu.Config{
		Name: "Ridu editorial kitchen sink",
		NameTranslations: map[string]string{
			"fr": "Cuisine éditoriale Ridu",
			"ar": "مطبخ ريدو التحريري",
		},
		Admin: ridu.AdminConfig{
			User: "users",
			Localization: ridu.AdminLocalizationConfig{
				Languages: []ridu.AdminLanguage{
					{
						Code:              "en",
						Label:             "English",
						LabelTranslations: map[string]string{"fr": "Anglais", "ar": "الإنجليزية"},
					},
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
			embeddedplugin.Plugin{},
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
				PaymentProcessors:     []field.Option{{Value: "fixture", Label: "Fixture processor"}},
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
			unifiedfields.Collection(),
			issuetargets.Collection(),
			dynamicdefaults.Collection(),
			livevalidation.Collection(),
			primitiveListCollection(),
			mediaCollection,
			postsCollection(),
			foldersCollection,
			categoriesCollection,
			pagesCollection,
			outlineCollection,
			richTextBlocksCollection(),
			richTextBlockPagesCollection(),
			eventsCollection,
			editorialNotesCollection,
			redirectsCollection,
			payloadOnlyCapabilitiesCollection,
		},
		Globals: []ridu.Global{
			siteSettingsGlobal,
			livevalidation.Global(),
		},
	})
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
		Collections: []ridu.Collection{
			{
				Slug:   "users",
				Labels: ridu.CollectionLabels{Singular: "User", Plural: "Users"},
				Admin:  ridu.CollectionAdmin{UseAsTitle: "name"},
				Auth:   true,
				Fields: field.Fields{
					field.Text("name").
						Required().
						Admin(field.Admin{Description: "The name shown in the admin."}),
					field.Email("email").
						Required().
						Unique().
						Admin(field.Admin{Description: "The identity used to sign in."}),
					field.Text("role").
						Access(field.Access{
							Create: func(ctx operation.AccessContext) (bool, error) {
								return ctx.Actor.ID != "", nil
							},
						}).
						Required().
						Default("administrator"),
				},
				Access: ridu.CollectionAccess{
					Admin:  allowAuthenticated,
					Read:   allowAuthenticated,
					Update: allowAuthenticated,
					Delete: allowAuthenticated,
				},
			},
		},
	}
}

var siteSettingsGlobal = ridu.Global{
	Slug:     "site-settings",
	Label:    "Site settings",
	Versions: true,
	Admin: ridu.GlobalAdmin{
		Group:       "Comparison reference",
		Description: "Site-wide identity, announcements, and navigation.",
	},
	VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10, AutosaveInterval: 30 * time.Second},
	Fields: field.Fields{
		field.Text("siteName").
			Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{trimString}}).
			Label("Site name").
			Required(),
		field.Textarea("announcement").
			Localized().
			Admin(field.Admin{Description: "A global announcement shown across the site."}),
		field.Array("navigation", field.Fields{
			field.Text("label").Required(),
			field.Relationship("page", "pages").Required(),
		}),
	},
	Access: ridu.GlobalAccess{Read: allowAuthenticated, Update: allowRoles(roleAdministrator, roleEditor)},
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
	Fields: field.Fields{
		field.Text("name").
			Required().
			Admin(field.Admin{Description: "The name shown on authored content."}),
		field.Text("email").
			Required().
			Unique().
			Admin(field.Admin{Description: "The identity used to sign in."}),
		field.Row(field.Fields{
			field.Select("role").Options(
				field.Option{Value: roleAdministrator, Label: "Administrator"},
				field.Option{Value: roleEditor, Label: "Editor"},
				field.Option{Value: roleContributor, Label: "Contributor"},
				field.Option{Value: roleAPIOnly, Label: "API only"}).
				Access(field.Access{
					Create: allowUserRoleCreate,
					Update: allowUserRoleUpdate,
				}).
				Required().
				Default(roleContributor).
				Admin(field.Admin{Columns: 6}),
			field.Email("contactEmail").Label("Public contact email").Admin(field.Admin{Columns: 6}),
		}),
		field.Tabs(field.Fields{
			field.NamedTab("profile", "Profile", field.Fields{
				field.Textarea("bio").Admin(field.Admin{Description: "Short biography displayed on author pages."}),
				field.Row(field.Fields{
					field.Text("location").Admin(field.Admin{Columns: 6}),
					field.Text("timezone").Default("Europe/London").Admin(field.Admin{Columns: 6}),
				}),
				field.Checkbox("availableForReview").Label("Available for review").Default(true),
			}),
			field.UnnamedTab("Security", field.Fields{
				field.Textarea("privateNotes").Access(field.Access{
					Create: allowFieldRoles(roleAdministrator),
					Read:   allowFieldRoles(roleAdministrator),
					Update: allowFieldRoles(roleAdministrator),
				}).Admin(field.Admin{Description: "Visible only to administrators."}),
			}),
		}),
	},
	Access: ridu.CollectionAccess{
		Admin:  allowRoles(roleAdministrator, roleEditor, roleContributor),
		Create: allowRoles(roleAdministrator, roleEditor),
		Read:   allowAuthenticated,
		Update: allowSelfOrRoles(roleAdministrator, roleEditor),
		Delete: allowRoles(roleAdministrator),
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
	Fields: field.Fields{
		field.Text("alt").
			Label("Alt text").
			Required().
			Admin(field.Admin{Description: "Describe the image for people who cannot see it."}),
		field.Textarea("caption").Admin(field.Admin{Description: "Optional editorial caption."}),
		field.Row(field.Fields{
			field.Relationship("credit", "users").Admin(field.Admin{Columns: 6}),
			field.Select("kind").Options(
				field.Option{Value: "image", Label: "image"},
				field.Option{Value: "illustration", Label: "illustration"},
				field.Option{Value: "screenshot", Label: "screenshot"}).
				Default("image").
				Admin(field.Admin{Columns: 6}),
		}),
		field.Array("tags", field.Fields{
			field.Text("label").Required(),
		}),
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
	Fields: field.Fields{
		field.Text("name").Required(),
		field.Slug("slug", "name").Admin(field.Admin{
			Description: "Generated from the category name until an author supplies a manual route slug.",
		}),
		field.Virtual("displayLabel", field.ValueString, func(
			ctx operation.ReadContext,
		) (operation.Value[store.Value], error) {
			name, _ := ctx.Root.Get("name").StringValue()
			return operation.Present(store.String("Category · " + name)), nil
		}).
			Label("Computed label").
			Admin(field.Admin{Description: "Resolved by executable Go configuration and never stored."}),
		field.Join("posts", "posts", "category").
			Label("Posts in this category").
			Limit(5).
			DefaultColumns("title", "status", "author", "updatedAt").
			DefaultSort("title"),
		field.Tabs(field.Fields{
			field.NamedTab("seo", "SEO", field.Fields{
				field.Text("title"),
				field.Textarea("description"),
			}),
		}),
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
	Fields: field.Fields{
		field.Text("name").Required().Unique(),
		field.Relationship("parent", "folders").Admin(field.Admin{Description: "Optional parent folder."}),
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
		Fields: field.Fields{
			field.Text("title").
				Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{trimString}}).
				Required().
				Localized().
				Admin(field.Admin{
					LabelTranslations: map[string]string{
						"fr": "Titre",
						"ar": "العنوان",
					},
					Description: "Primary label used throughout the admin.",
					DescriptionTranslations: map[string]string{
						"fr": "Libellé principal utilisé dans toute l’administration.",
						"ar": "العنوان الرئيسي المستخدم في لوحة الإدارة.",
					},
				}),
			field.Text("slug").
				Unique().
				Admin(field.Admin{Description: "Authored separately because this fixture's title is localized."}),
			field.Textarea("summary").
				Required().
				Localized().
				Admin(field.Admin{Description: "A concise introduction for listings and previews."}),
			field.Row(field.Fields{
				field.Relationship("author", "users").Admin(field.Admin{Columns: 6}),
				field.Relationship("category", "categories").Admin(field.Admin{Columns: 6}),
			}),
			field.Row(field.Fields{
				field.Relationship("folder", "folders").Admin(field.Admin{Columns: 6}),
				field.Relationship("parent", "posts").Admin(field.Admin{
					Columns:     6,
					Description: "Optional parent used by the hierarchy view.",
				}),
			}),
			field.Row(field.Fields{
				field.Select("status").Options(
					field.Option{Value: "draft", LabelTranslations: map[string]string{
						"fr": "Brouillon",
						"ar": "مسودة",
					}},
					field.Option{Value: "published", LabelTranslations: map[string]string{
						"fr": "Publié",
						"ar": "منشور",
					}}).
					Access(field.Access{
						Create: allowDraftEditorialStatus,
						Update: allowFieldRoles(roleAdministrator, roleEditor),
					}).
					Default("draft").
					Admin(field.Admin{Columns: 4}),
				field.Checkbox("featured").Default(false).Admin(field.Admin{Columns: 4}),
				field.Number("readingMinutes").Label("Reading time (minutes)").Default(5).Admin(field.Admin{Columns: 4}),
			}),
			field.Date("publishDate").
				Label("Publication date").
				Format(field.DateTime).
				Admin(field.Admin{
					VisibleWhen: field.Equal(field.Root("status"), "published"),
					Description: "Conditional presentation is not authorization.",
				}),
			field.Upload("cover", "media").Admin(field.Admin{Description: "Select the lead image used in post previews."}),
			field.Uploads("gallery", "media").Label("Image gallery"),
			richtext.Field("content").Localized().Admin(field.Admin{Description: "Write the main body of the post."}),
			field.Relationships("collaborators", "users"),
			field.Relationships("relatedPosts", "posts").
				Label("Related posts").
				Admin(field.Admin{Description: "A conventional has-many relationship used by the browser contract."}),
			field.Relationship("publishedPage", "pages").
				Label("Published page").
				FilterOptionRules(
					field.OptionFilterValue("_status", field.FilterEquals, "published"),
				).
				Admin(field.Admin{
					Description: "A literal server-enforced option filter excludes draft pages.",
				}),
			field.Relationships("sameCategoryPosts", "posts").
				Label("Posts from this category").
				FilterOptionRules(
					field.OptionFilter("category", field.FilterEquals, "category"),
					field.OptionFilter("seo.title", field.FilterEquals, "seo.title"),
				).
				Admin(field.Admin{
					Description: "Options share the current category and nested SEO title.",
				}),
			field.PolymorphicRelationship("relatedContent", "posts", "pages").
				Label("Related content").
				FilterOptionRules(
					field.OptionFilterFor("posts", "category", field.FilterEquals, "category"),
					field.OptionFilterFor(
						"pages",
						"navigation.showInHeader",
						field.FilterEquals,
						"featured",
					),
				).
				Admin(field.Admin{
					Description: "Polymorphic options use target-specific predicates from current document values.",
				}),
			field.Tabs(field.Fields{
				field.NamedTab("seo", "SEO", field.Fields{
					field.Text("title").Admin(field.Admin{Columns: 6}),
					field.Text("canonicalURL").Label("Canonical URL").Admin(field.Admin{Columns: 6}),
					field.Textarea("description"),
					field.Upload("socialImage", "media").Label("Social image"),
				}),
			}),
			field.Tabs(field.Fields{
				field.UnnamedTab("Related", field.Fields{
					field.Array("links", field.Fields{
						field.Row(field.Fields{
							field.Text("label").Required().Admin(field.Admin{Columns: 5}),
							field.Text("url").Required().Admin(field.Admin{Columns: 7}),
						}),
						field.Checkbox("newWindow").Label("Open in a new window").Default(false),
					}),
				}),
				field.UnnamedTab("Layout", field.Fields{
					field.Blocks("layout",
						field.Block{
							Slug: "callout",
							Fields: field.Fields{
								field.Select("tone").Options(
									field.Option{Value: "note", Label: "note"},
									field.Option{Value: "warning", Label: "warning"},
									field.Option{Value: "success", Label: "success"}).
									Default("note"),
								field.Textarea("body").Required(),
							},
						},
						field.Block{
							Slug: "quote",
							Fields: field.Fields{
								field.Textarea("quote").Required(),
								field.Text("attribution"),
							},
						},
						field.Block{
							Slug:   "related-posts",
							Labels: field.BlockLabels{Singular: "Related posts"},
							Fields: field.Fields{
								field.Relationships("posts", "posts").Required(),
							},
						},
					),
				}),
				field.UnnamedTab("Advanced", field.Fields{
					field.JSON("metadata").Admin(field.Admin{Description: "Arbitrary JSON exercises the raw JSON editor."}),
					field.Textarea("internalNotes").Access(field.Access{
						Read:   allowFieldRoles(roleAdministrator),
						Create: allowFieldRoles(roleAdministrator, roleEditor),
						Update: allowFieldRoles(roleAdministrator, roleEditor),
					}).Admin(field.Admin{
						Description: "Administrators can read this field; editors may write it but receive a redacted response.",
					}),
					field.Relationship("lastEditedBy", "users").Label("Last edited by").Admin(field.Admin{ReadOnly: true}),
				}),
			}),
		},
		Access: ridu.CollectionAccess{
			Create: allowAuthenticated,
			Read:   postReadAccess,
			Update: allowOwnedOrRoles("author", roleAdministrator, roleEditor),
			Delete: allowOwnedOrRoles("author", roleAdministrator),
		},
		Hooks: ridu.CollectionHooks{
			BeforeDuplicate: []ridu.Hook{preparePostDuplicate},
			BeforeChange:    []ridu.Hook{recordLastEditor},
		},
	}
}

func preparePostDuplicate(ctx ridu.HookContext) error {
	if ctx.Operation != operation.Duplicate {
		return nil
	}
	title, _ := ctx.Data["title"].StringValue()
	ctx.Data["title"] = store.String("Copy of " + title)
	delete(ctx.Data, "slug")
	return nil
}

var pagesCollection = ridu.Collection{
	Slug:   "pages",
	Labels: ridu.CollectionLabels{Singular: "Page", Plural: "Pages"},
	Admin: ridu.CollectionAdmin{
		Group:       "Editorial",
		Description: "Structured, versioned pages assembled from blocks.",
	},
	Versions:      true,
	VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10, AutosaveInterval: 30 * time.Second},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Slug("slug", "title"),
		field.Select("template").Options(
			field.Option{Value: "default", Label: "default"},
			field.Option{Value: "landing", Label: "landing"},
			field.Option{Value: "legal", Label: "legal"}).
			Default("default"),
		field.Array("notes", field.Fields{
			field.Text("heading").Admin(field.Admin{Editor: field.Component("app:text")}),
			richtext.Field("body"),
		}).Admin(field.Admin{RowLabelPath: "heading"}),
		field.Blocks("layout",
			field.Block{Admin: field.BlockAdmin{RowLabelPath: "heading"}, Slug: "hero",
				Fields: field.Fields{
					field.Text("heading").
						Access(field.Access{Update: func(ctx operation.AccessContext) (bool, error) {
							locked, _ := ctx.Siblings.Get("headingLocked").BooleanValue()
							return !locked, nil
						}}).
						Required().
						Admin(field.Admin{
							Editor: field.Component(
								"app:capturedText",
								store.Object(store.Values{"capture": store.Boolean(true)}),
							),
						}),
					field.Checkbox("headingLocked").
						Access(field.Access{Update: allowFieldRoles(roleAdministrator)}),
					field.Textarea("lede"),
					field.Upload("image", "media"),
					field.Array("links", field.Fields{
						field.Text("label").
							Required().
							Admin(field.Admin{Editor: field.Component("app:text")}),
					}),
				},
			},

			field.Block{
				Slug: "rich-text",
				Fields: field.Fields{
					richtext.Field("body").Required(),
				},
			},
			field.Block{
				Slug: "featured-post",
				Fields: field.Fields{
					field.Relationship("post", "posts").Required(),
				},
			},
		).
			Required().
			MinRows(1).
			MaxRows(4),
		field.Tabs(field.Fields{
			field.NamedTab("navigation", "Navigation", field.Fields{
				field.Text("label"),
				field.Checkbox("showInHeader").Label("Show in header").Default(false),
				field.Checkbox("showInFooter").Label("Show in footer").Default(false),
			}),
		}),
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
	Fields: field.Fields{
		field.Text("name").Required(),
		field.Row(field.Fields{
			field.Date("startsAt").Label("Starts at").Required().Format(field.DateTime).Admin(field.Admin{Columns: 6}),
			field.Date("endsAt").Label("Ends at").Format(field.DateTime).Admin(field.Admin{Columns: 6}),
		}),
		field.Row(field.Fields{
			field.Number("capacity").Default(50).Admin(field.Admin{Columns: 6}),
			field.Checkbox("online").Default(false).Admin(field.Admin{Columns: 6}),
		}),
		field.Text("venue").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("online"), false)}),
		field.Text("meetingURL").
			Label("Meeting URL").
			Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("online"), true)}),
		field.Email("contact").Label("Contact email").Required(),
		field.Array("schedule", field.Fields{
			field.Date("time").Required().Format(field.DateTime),
			field.Text("title").Required(),
			field.Relationship("speaker", "users"),
		}),
		field.JSON("registrationSettings"),
	},
	Access: staffManagedPublicReadAccess(),
}

var editorialNotesCollection = ridu.Collection{
	Slug:   "editorial-notes",
	Labels: ridu.CollectionLabels{Singular: "Editorial note", Plural: "Editorial notes"},
	Admin:  ridu.CollectionAdmin{Group: "Workflow", Description: "Private notes scoped by ownership and role."},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Relationship("owner", "users").Required(),
		field.Textarea("note").Required(),
		field.Textarea("confidentialDetails").Access(field.Access{
			Read:   allowFieldRoles(roleAdministrator),
			Create: allowFieldRoles(roleAdministrator),
			Update: allowFieldRoles(roleAdministrator),
		}).Label("Confidential details"),
	},
	Access: ridu.CollectionAccess{
		Create: allowAuthenticated,
		Read:   allowOwnedOrRoles("owner", roleAdministrator, roleEditor),
		Update: allowOwnedOrRoles("owner", roleAdministrator, roleEditor),
		Delete: allowOwnedOrRoles("owner", roleAdministrator),
	},
}

var redirectsCollection = ridu.Collection{
	Slug:   "redirects",
	Labels: ridu.CollectionLabels{Singular: "Redirect", Plural: "Redirects"},
	Admin:  ridu.CollectionAdmin{Group: "Operations", Description: "Path redirects managed by the editorial team."},
	Fields: field.Fields{
		field.Text("from").Label("From path").Required().Unique(),
		field.Text("to").Label("Destination").Required(),
		field.Select("type").Options(
			field.Option{Value: "permanent", Label: "permanent"},
			field.Option{Value: "temporary", Label: "temporary"}).
			Default("permanent"),
		field.Checkbox("enabled").Default(true),
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
	Admin: ridu.CollectionAdmin{
		Group:       "Comparison reference",
		Description: "Less common field and presentation contracts exercised together.",
	},
	Fields: field.Fields{
		field.Text("title").
			Required().
			Admin(field.Admin{
				Placeholder: "Name this field showcase",
				PlaceholderTranslations: map[string]string{
					"fr": "Nommez cette vitrine de champs",
					"ar": "سمّ عرض الحقول هذا",
				},
			}),
		field.Code("code").Admin(field.Admin{
			CodeLanguage: "typescript",
			Description:  "A monospaced source editor with a language hint.",
		}),
		field.MultiSelect("audiences", "administrators", "editors", "reviewers").Label("Audiences").Required().Default("editors"),
		field.Radio("priority", "low", "normal", "high").Required().Default("normal").Admin(field.Admin{Sidebar: true}),
		field.Point("location").Admin(field.Admin{Sidebar: true, Description: "Longitude and latitude, in that order."}),
		field.UI("fieldGuide").
			Label("Field guide").
			Admin(field.Admin{
				Description: "This content is presentation-only and never enters APIs, storage, or generated document types.",
			}),
		field.Collapsible("advancedSettings", field.Fields{
			field.Text("internalName").Label("Internal name").Admin(field.Admin{Hidden: true}),
			field.Textarea("implementationNotes").Admin(field.Admin{Placeholder: "Record implementation details"}),
		}).Admin(field.Admin{InitiallyCollapsed: true}),
		field.Array("team", field.Fields{
			field.Text("displayName").Required(),
			field.Email("email"),
			field.Radio("role").Options(
				field.Option{Value: "author", Label: "author"},
				field.Option{Value: "reviewer", Label: "reviewer"}).
				Default("reviewer"),
		}).MinRows(1).MaxRows(3).Admin(field.Admin{RowLabelPath: "displayName"}),
		field.Virtual("fieldSummary", field.ValueString, func(
			ctx operation.ReadContext,
		) (operation.Value[store.Value], error) {
			title, _ := ctx.Root.Get("title").StringValue()
			priority, _ := ctx.Root.Get("priority").StringValue()
			return operation.Present(store.String(title + " · " + priority)), nil
		}).
			Label("Computed summary").
			Admin(field.Admin{
				Sidebar:     true,
				Description: "A read-only value derived from the stored title and priority.",
			}),
		field.Array("curriculumNodes", field.Fields{
			field.Select("nodeType").Options(
				field.Option{Value: "lesson", Label: "lesson"},
				field.Option{Value: "question", Label: "question"}),
			field.Text("lesson"),
			field.Text("question"),
			field.Number("orderIndex"),
		}).
			Label("Curriculum nodes").
			Admin(field.Admin{
				RowLabel: field.Component(
					"app:compositeRowLabel",
					store.Object(store.Values{
						"kind":     store.String("typedOrder"),
						"typePath": store.String("nodeType"),
						"targets": store.Object(store.Values{
							"lesson":   store.String("lesson"),
							"question": store.String("question"),
						}),
						"orderPath": store.String("orderIndex"),
					}),
				),
			}),
		field.Blocks("questionParts",
			field.Block{
				Slug: "choice",
				Fields: field.Fields{
					field.Text("optionKey"),
					field.Text("label"),
				},
			},
			field.Block{
				Slug: "reorder",
				Fields: field.Fields{
					field.Text("optionKey"),
					field.Text("label"),
				},
			},
		).
			Label("Question parts").
			Admin(field.Admin{
				RowLabel: field.Component(
					"app:compositeRowLabel",
					store.Object(store.Values{
						"kind":      store.String("keyLabel"),
						"keyPath":   store.String("optionKey"),
						"labelPath": store.String("label"),
					}),
				),
			}),
	},

	Access: staffManagedPublicReadAccess(),
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

func allowFieldRoles(roles ...string) field.AccessRule {
	return func(ctx operation.AccessContext) (bool, error) {
		return fieldActorHasRole(ctx.Actor, roles...), nil
	}
}

func allowDraftEditorialStatus(ctx operation.AccessContext) (bool, error) {
	if fieldActorHasRole(ctx.Actor, roleAdministrator, roleEditor) {
		return true, nil
	}
	status, valid := ctx.Siblings.String("status")
	return valid && status == "draft", nil
}

func allowUserRoleCreate(ctx operation.AccessContext) (bool, error) {
	if fieldActorHasRole(ctx.Actor, roleAdministrator) {
		return true, nil
	}
	role, valid := ctx.Siblings.String("role")
	return fieldActorHasRole(ctx.Actor, roleEditor) && valid && role == roleContributor, nil
}

func allowUserRoleUpdate(ctx operation.AccessContext) (bool, error) {
	if fieldActorHasRole(ctx.Actor, roleAdministrator) {
		return true, nil
	}
	next, nextValid := ctx.Siblings.String("role")
	current, currentValid := ctx.Prior.String("role")
	return nextValid && currentValid && next == current, nil
}

func fieldActorHasRole(actor operation.Actor, roles ...string) bool {
	if actor.ID == "" {
		return false
	}
	role, valid := actor.Data.String("role")
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

func trimString(_ operation.WriteContext, value operation.Value[store.Value]) (operation.Change[store.Value], error) {
	if raw, present := value.Get(); present {
		if text, valid := raw.StringValue(); valid {
			return operation.Replace(operation.Present(store.String(strings.TrimSpace(text)))), nil
		}
	}
	return operation.Keep[store.Value](), nil
}

func recordLastEditor(ctx ridu.HookContext) error {
	if ctx.Actor != nil && (ctx.Operation == operation.Create || ctx.Operation == operation.Update) {
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

var outlineCollection = ridu.Collection{
	Slug:     "outlines",
	Labels:   ridu.CollectionLabels{Singular: "Outline", Plural: "Outlines"},
	Versions: true,
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Checkbox("bodyLocked").Access(field.Access{Update: allowFieldRoles(roleAdministrator)}),
		embeddedplugin.Field("body", field.Block{Slug: "card", Fields: field.Fields{
			field.Text("title").Access(field.Access{Update: func(ctx operation.AccessContext) (bool, error) {
				locked, _ := ctx.Siblings.Get("locked").BooleanValue()
				return !locked, nil
			}}).Required().Localized(),
			field.Checkbox("locked").Access(field.Access{Update: allowFieldRoles(roleAdministrator)}),
			field.Group("settings", field.Fields{
				field.Text("caption").Default("Default caption"),
			}),
		}}).Access(field.Access{Update: func(ctx operation.AccessContext) (bool, error) {
			locked, _ := ctx.Siblings.Get("bodyLocked").BooleanValue()
			return !locked, nil
		}}),
	},
	Access: ridu.CollectionAccess{
		Create: allowRoles(roleAdministrator, roleEditor),
		Read:   allowEveryone,
		Update: allowRoles(roleAdministrator, roleEditor),
		Delete: allowRoles(roleAdministrator),
	},
}

func primitiveListCollection() ridu.Collection {
	collection := primitivelists.Collection()
	collection.VersionConfig = ridu.VersionConfig{Drafts: true, MaxPerDocument: 20}
	deny := func(operation.AccessContext) (bool, error) { return false, nil }
	collection.Fields = append(collection.Fields,
		field.TextList("readOnlyPoints").
			Default("Fixed", "Fixed").
			Admin(field.Admin{ReadOnly: true}),
		field.NumberList("lockedSizes").
			Default(0, 8).
			Access(field.Access{Update: allowFieldRoles(roleAdministrator)}),
		field.TextList("privatePoints").
			Default("Hidden").
			Access(field.Access{Read: deny}),
		field.TextList("editorPoints").
			Label("Editor points").
			Admin(field.Admin{Editor: field.Component("app:primitiveText")}),
		field.TextList("serverPoints").
			Label("Server points").
			MaxLength(20).
			Hooks(field.Hooks[[]string]{
				BeforeChange: []field.Transform[[]string]{
					func(_ operation.WriteContext, value operation.Value[[]string]) (operation.Change[[]string], error) {
						if items, present := value.Get(); present && len(items) > 0 && items[0] == "expand" {
							return operation.Replace(operation.Present([]string{strings.Repeat("x", 21)})), nil
						}
						return operation.Keep[[]string](), nil
					},
				},
			}),
	)
	return collection
}
