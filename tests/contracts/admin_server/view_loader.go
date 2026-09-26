package main

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
)

var loaderRecords = ridu.Collection{
	Slug:   "loader-records",
	Admin:  ridu.CollectionAdmin{Group: "Contracts", UseAsTitle: "title"},
	Fields: field.Fields{field.Text("title").Required().Localized()},
	Access: staffManagedPublicReadAccess(),
}

var loaderSummary = ridu.Global{
	Slug:   "loader-summary",
	Fields: field.Fields{field.Text("title").Default("Editorial summary")},
}

type editorialViewInput struct {
	Search string `json:"q"`
}
type editorialViewDocument struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type editorialViewData struct {
	Pathname  string                  `json:"pathname"`
	Locale    string                  `json:"locale"`
	Search    string                  `json:"search"`
	Documents []editorialViewDocument `json:"documents"`
}

var editorialView = ridu.NewAdminLoader("editorial-view", func(ctx ridu.AdminLoadContext, input editorialViewInput) (editorialViewData, error) {
	result := editorialViewData{Pathname: ctx.Pathname, Locale: string(ctx.Locale()), Search: input.Search, Documents: []editorialViewDocument{}}
	if id := ctx.RouteParams["document"]; id != "" {
		document, err := ctx.Find("loader-records", id, ridu.FindOptions{})
		if err != nil {
			return result, err
		}
		title, _ := document.Values["title"].StringValue()
		result.Documents = append(result.Documents, editorialViewDocument{ID: document.ID, Title: title})
		return result, nil
	}
	if slug := ctx.RouteParams["global"]; slug != "" {
		document, err := ctx.Global(slug, ridu.FindOptions{})
		if err != nil {
			return result, err
		}
		title, _ := document.Values["title"].StringValue()
		result.Documents = append(result.Documents, editorialViewDocument{ID: document.ID, Title: title})
		return result, nil
	}
	options := ridu.ListOptions{Limit: 20}
	if input.Search != "" {
		title, _ := query.NewPath("title")
		options.Where = query.Contains(title, input.Search)
	}
	page, err := ctx.List("loader-records", options)
	if err != nil {
		return result, err
	}
	for _, document := range page.Documents {
		title, _ := document.Values["title"].StringValue()
		result.Documents = append(result.Documents, editorialViewDocument{ID: document.ID, Title: title})
	}
	return result, nil
})
