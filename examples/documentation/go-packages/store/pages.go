package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

var Pages = ridu.Collection{
	Slug: "pages",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Textarea("summary"),
		field.Group("seo", field.Fields{
			field.Text("title"),
		}),
		field.Array("links", field.Fields{
			field.Text("label").Required(),
			field.Text("url").Required(),
		}),
	},
}

func CreatePage(
	ctx context.Context,
	app *ridu.App,
	options ridu.MutationOptions,
) (store.Document, error) {
	return app.Local().Create(ctx, "pages", store.Values{
		"title":   store.String("Home"),
		"summary": store.String("Start here"),
		"seo": store.Object(store.Values{
			"title": store.String("Welcome to Acme"),
		}),
		"links": store.List(store.Object(store.Values{
			// Ridu adds this new row's _key when it saves the page.
			"label": store.String("About"),
			"url":   store.String("/about"),
		})),
	}, options)
}
