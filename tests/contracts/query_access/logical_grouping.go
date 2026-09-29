package queryaccess

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// An Or predicate from an access rule or a caller filter must stay one
// group. Joined to the other predicates without its own parentheses, SQL
// precedence reads `deleted AND filter AND a OR b` as `(... AND a) OR b`,
// so rows matching b escape the caller's filter, the ID lookup and the access
// rule itself.
func logicalGrouping(t *testing.T, factory Factory) {
	_, app := factory(t, ridu.Config{Name: "Logical predicate grouping", Collections: []ridu.Collection{{
		Slug:   "memberships",
		Fields: field.Fields{field.Text("learner"), field.Text("league"), field.Text("status")},
		Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Where(query.Or(query.Equal(path("learner"), "ada"), query.In(path("league"), "l1", "l2"))), nil
		}},
	}}})
	ids := map[string]string{}
	for _, row := range []struct{ name, learner, league, status string }{
		{"ada-l9", "ada", "l9", "active"},
		{"ben-l1", "ben", "l1", "left"},
		{"cy-l1", "cy", "l1", "active"},
		{"cy-l9", "cy", "l9", "left"},
	} {
		document, err := app.Local().Create(t.Context(), "memberships", store.Values{
			"learner": store.String(row.learner), "league": store.String(row.league), "status": store.String(row.status),
		}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ids[row.name] = document.ID
	}
	names := func(documents []store.Document) []string {
		byID := map[string]string{}
		for name, id := range ids {
			byID[id] = name
		}
		result := make([]string, 0, len(documents))
		for _, document := range documents {
			result = append(result, byID[document.ID])
		}
		sort.Strings(result)
		return result
	}
	expect := func(label string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}

	page, err := app.Local().List(t.Context(), "memberships", ridu.ListOptions{Where: query.Equal(path("status"), "left")})
	if err != nil {
		t.Fatal(err)
	}
	expect("filtered list under an Or access rule", names(page.Documents), "ben-l1")
	if page.Total != 1 {
		t.Fatalf("filtered total = %d, want 1", page.Total)
	}

	handler := app.Handler(ridu.HandlerOptions{})
	response := request(handler, http.MethodGet, "/api/collections/memberships/count?where="+url.QueryEscape(`{"status":{"equals":"left"}}`), "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"totalDocs":1`) {
		t.Fatalf("REST filtered count under an Or access rule: %d %s", response.Code, response.Body.String())
	}

	// A caller's own Or must not widen the access rule either.
	page, err = app.Local().List(t.Context(), "memberships", ridu.ListOptions{Where: query.Or(query.Equal(path("learner"), "cy"), query.Equal(path("status"), "nobody"))})
	if err != nil {
		t.Fatal(err)
	}
	expect("caller Or under an Or access rule", names(page.Documents), "cy-l1")

	// A hidden document stays hidden when looked up by ID.
	_, err = app.Local().Find(t.Context(), "memberships", ids["cy-l9"], ridu.FindOptions{})
	var failure *operation.Error
	if !errors.As(err, &failure) || failure.Status != http.StatusNotFound {
		t.Fatalf("find hidden membership = %v, want not found", err)
	}
	found, err := app.Local().Find(t.Context(), "memberships", ids["ben-l1"], ridu.FindOptions{})
	if err != nil || found.ID != ids["ben-l1"] {
		t.Fatalf("find visible membership = %#v, %v", found, err)
	}
}
