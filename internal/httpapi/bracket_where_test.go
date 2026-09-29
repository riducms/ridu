package httpapi

import (
	"net/url"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

// Clients ported from qs-style APIs send where[field][operator]=value. Ridu
// takes one JSON where parameter, and the error shows the caller's own filter
// in that form.
func TestBracketStyleWhereNamesTheJSONForm(t *testing.T) {
	for _, test := range []struct {
		query string
		want  string
	}{
		{"where[status][equals]=left&limit=5", `where={"status":{"equals":"left"}}`},
		{"where[learner][equals]=ada&where[league][not_equals]=l9", `where={"league":{"not_equals":"l9"},"learner":{"equals":"ada"}}`},
		{"where[or][0][status][equals]=left", `where={"status":{"equals":"published"}}`},
	} {
		values, err := url.ParseQuery(test.query)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeListQuery(values, schema.Collection{Slug: "memberships"}, false)
		if err == nil || !strings.Contains(err.Error(), "bracket-style") || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %s", test.query, err, test.want)
		}
	}
}
