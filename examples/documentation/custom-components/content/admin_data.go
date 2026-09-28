package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

type PostSummaryInput struct {
	Search string `json:"q"`
}

type PostSummaryData struct {
	Total  int    `json:"total"`
	Search string `json:"search"`
	Locale string `json:"locale"`
}

var PostSummary = ridu.NewAdminLoader("post-summary", loadPostSummary)

func loadPostSummary(
	ctx ridu.AdminLoadContext,
	input PostSummaryInput,
) (PostSummaryData, error) {
	// We need the count, not a page full of post content.
	options := ridu.ListOptions{Limit: 1}
	if input.Search != "" {
		options.Where = query.Contains(query.Field("title"), input.Search)
	}
	posts, err := ctx.List("posts", options)
	if err != nil {
		return PostSummaryData{}, err
	}
	return PostSummaryData{
		Total: posts.Total, Search: input.Search,
		Locale: string(ctx.Locale()),
	}, nil
}
