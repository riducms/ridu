package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

// A join's default sort is applied by the operation engine as an ordinary
// sort, so configuration rejects every path the engine refuses to order by.
func TestJoinDefaultSortMustBeSortable(t *testing.T) {
	resolve := func(sort string) error {
		_, err := ridu.Resolve(ridu.Config{Name: "Join sorts", Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", "category").DefaultSort(sort)}},
			{Slug: "posts", Fields: field.Fields{
				field.Text("title"),
				field.Relationship("category", "categories"),
				field.MultiSelect("tags", "news", "sports"),
				field.Relationships("authors", "categories"),
				field.JSON("metadata"),
				field.Group("seo", field.Fields{field.Text("title")}),
				field.Array("rows", field.Fields{field.Text("label")}),
			}},
		}})
		return err
	}
	for _, sort := range []string{"title", "-title", "createdAt", "id", "category"} {
		if err := resolve(sort); err != nil {
			t.Errorf("join sort %q rejected: %v", sort, err)
		}
	}
	for _, sort := range []string{"tags", "-authors", "metadata", "seo", "rows", "_status"} {
		err := resolve(sort)
		var validation *schema.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("join sort %q accepted: %v", sort, err)
			continue
		}
		found := false
		for _, issue := range validation.Issues {
			found = found || issue.Code == "invalid_join_sort" && strings.HasSuffix(issue.Path, ".defaultSort") && strings.Contains(issue.Message, "order")
		}
		if !found {
			t.Errorf("join sort %q issues = %#v, want invalid_join_sort at the join's defaultSort", sort, validation.Issues)
		}
	}
}
