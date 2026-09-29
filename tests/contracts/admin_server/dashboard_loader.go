package main

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

type editorialDashboardInput struct {
	Search string `json:"q"`
}

type editorialDashboardData struct {
	Posts      int    `json:"posts"`
	Categories int    `json:"categories"`
	Search     string `json:"search"`
}

var editorialDashboard = ridu.NewAdminLoader("editorial-dashboard", func(ctx ridu.AdminLoadContext, input editorialDashboardInput) (editorialDashboardData, error) {
	options := ridu.ListOptions{Limit: 1}
	if input.Search != "" {
		options.Where = query.Contains("title", input.Search)
	}
	posts, err := ctx.List("posts", options)
	if err != nil {
		return editorialDashboardData{}, err
	}
	categories, err := ctx.List("categories", ridu.ListOptions{Limit: 1})
	if err != nil {
		return editorialDashboardData{}, err
	}
	return editorialDashboardData{Posts: posts.Total, Categories: categories.Total, Search: input.Search}, nil
})
