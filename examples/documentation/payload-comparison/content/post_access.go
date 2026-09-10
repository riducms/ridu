package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

func ownPosts(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
	if ctx.Actor == nil {
		return ridu.Deny(), nil
	}
	author, err := query.NewPath("author")
	if err != nil {
		return ridu.Deny(), err
	}
	// Check ownership in the database operation, without a prior read.
	return ridu.Where(
		query.Equal(author, query.String(ctx.Actor.ID)),
	), nil
}
