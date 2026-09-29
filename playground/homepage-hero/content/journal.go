package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

func anyone(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }

// Media is public so the journal frontend can show banner images.
var Media = ridu.Collection{
	Slug:   "media",
	Labels: ridu.CollectionLabels{Singular: "Image", Plural: "Media"},
	Admin:  ridu.CollectionAdmin{UseAsTitle: "alt"},
	Upload: true,
	UploadConfig: ridu.UploadConfig{
		MaxFileSize: 8 << 20,
		MimeTypes:   []string{"image/jpeg", "image/png", "image/webp"},
	},
	Fields: field.Fields{
		field.Text("alt").Label("Alt text").Required(),
	},
	Access: ridu.CollectionAccess{
		Read:   anyone,
		Create: authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
}

var Posts = ridu.Collection{
	Slug:          "posts",
	Labels:        ridu.CollectionLabels{Singular: "Post", Plural: "Posts"},
	Versions:      true,
	VersionConfig: ridu.VersionConfig{Drafts: true},
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "updatedAt"},
		LivePreview: ridu.LivePreviewConfig{
			URL: "http://127.0.0.1:18090/journal/{id}",
		},
	},
	Fields: field.Fields{
		field.Tabs(field.Fields{
			field.UnnamedTab("Content", field.Fields{
				field.Text("title").Label("Post Title").Required(),
				field.Upload("banner", "media").Label("Banner Image").Required(),
				richtext.Field("content", richtext.Config{Admin: richtext.Admin{
					FixedToolbar: true,
					HideGutter:   true,
				}}).Required(),
			}),
			field.UnnamedTab("Appearance", field.Fields{
				field.Select("layout", "editorial", "wide").Default("editorial"),
				field.Select("accent", "rose", "violet", "amber").Default("rose"),
			}),
			field.UnnamedTab("SEO", field.Fields{
				field.Text("metaTitle").Label("Meta title"),
				field.Textarea("metaDescription").Label("Meta description"),
			}),
		}),
	},
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
}
