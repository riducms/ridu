package conformance

import (
	"math"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// runSkipTotal proves that a SkipTotal list is the counted list without its
// total: the same documents, page and limit, and an exact HasNextPage at every
// boundary, after access, caller filters, sorting and head selection.
func runSkipTotal(t *testing.T, factory Factory) {
	fixture := openFixture(t, factory, conformanceManifest())
	createRecords(t, fixture, []recordFixture{
		{id: id(1), state: "public", rank: 1},
		{id: id(2), state: "public", rank: 2},
		{id: id(3), state: "public", rank: 3},
		{id: id(4), state: "public", rank: 4},
		{id: id(5), state: "public", rank: 5},
		{id: id(6), state: "private", rank: 6},
		{id: id(7), state: "public", rank: 0},
	})
	transaction := begin(t, fixture.backend)
	for _, record := range []recordFixture{{id: id(8), state: "public", rank: 10}, {id: id(9), state: "public", rank: 11}} {
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: record.id, Values: recordValues(record.state, record.rank), Status: store.StatusPublished,
		})); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
	}
	commit(t, transaction)

	filter := query.GreaterThanEqual(mustPath("rank"), 1).Node()
	access := query.Equal(mustPath("state"), "public").Node()
	sortTerm, err := query.NewSort(mustPath("rank"), query.Ascending)
	if err != nil {
		t.Fatal(err)
	}
	// Working matches in rank order are 1-5, 8 and 9; published matches are 8 and 9.
	for _, test := range []struct {
		name      string
		published bool
		page      int
		limit     int
		want      []string
		next      bool
		total     int
	}{
		{name: "total-equals-limit", page: 1, limit: 7, want: []string{id(1), id(2), id(3), id(4), id(5), id(8), id(9)}, total: 7},
		{name: "total-is-limit-plus-one", page: 1, limit: 6, want: []string{id(1), id(2), id(3), id(4), id(5), id(8)}, next: true, total: 7},
		{name: "partial-last-page", page: 2, limit: 6, want: []string{id(9)}, total: 7},
		{name: "full-last-page", page: 7, limit: 1, want: []string{id(9)}, total: 7},
		{name: "page-before-last", page: 6, limit: 1, want: []string{id(8)}, next: true, total: 7},
		{name: "past-the-last-page", page: 2, limit: 7, want: []string{}, total: 7},
		{name: "unrepresentable-offset", page: math.MaxInt, limit: 2, want: []string{}, total: 7},
		{name: "published-heads", published: true, page: 1, limit: 1, want: []string{id(8)}, next: true, total: 2},
		{name: "published-last-page", published: true, page: 1, limit: 2, want: []string{id(8), id(9)}, total: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := fixture.request(store.Request{
				Collection: fixture.records, Collections: fixture.collections,
				Filter: &filter, Access: &access, Sort: []query.Sort{sortTerm},
				Page: test.page, Limit: test.limit, PublishedOnly: test.published,
				Select: []query.Path{mustPath("rank")},
			})
			uncountedRequest := request
			uncountedRequest.SkipTotal = true
			read := begin(t, fixture.backend)
			counted, countedError := read.List(t.Context(), request)
			uncounted, uncountedError := read.List(t.Context(), uncountedRequest)
			rollback(t, read)
			if countedError != nil || uncountedError != nil {
				t.Fatalf("list errors: counted %v, uncounted %v", countedError, uncountedError)
			}
			assertDocumentIDs(t, uncounted.Documents, test.want...)
			if uncounted.Total != nil || uncounted.HasNextPage != test.next || uncounted.Page != test.page || uncounted.Limit != test.limit {
				t.Fatalf("uncounted page = total %v next %t page %d limit %d, want no total, next %t, page %d, limit %d",
					uncounted.Total, uncounted.HasNextPage, uncounted.Page, uncounted.Limit, test.next, test.page, test.limit)
			}
			assertDocumentIDs(t, counted.Documents, test.want...)
			if counted.Total == nil || *counted.Total != test.total || counted.HasNextPage != test.next || counted.Page != test.page || counted.Limit != test.limit {
				t.Fatalf("counted page = total %v next %t page %d limit %d, want total %d, next %t, page %d, limit %d",
					counted.Total, counted.HasNextPage, counted.Page, counted.Limit, test.total, test.next, test.page, test.limit)
			}
			for _, document := range uncounted.Documents {
				if _, selected := document.Values["rank"]; !selected || len(document.Values) != 1 {
					t.Fatalf("uncounted page projection = %#v, want rank only", document.Values)
				}
			}
		})
	}
}
