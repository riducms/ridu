package sqlite

import (
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// A date-and-time value saved without milliseconds, or with an offset, is
// stored in the same form query.DateTime writes, so equality and keyset
// ordering hold within one second.
func TestSQLiteDateTimeQueriesMatchValuesSavedInAnyForm(t *testing.T) {
	ctx := t.Context()
	config := ridu.Config{Name: "SQLite date-time queries", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Text("title"), field.Date("publishedAt").Format(field.DateTime).Index()},
	}}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	for title, publishedAt := range map[string]string{
		"without milliseconds": "2026-10-09T12:34:32Z",
		"with milliseconds":    "2026-10-09T12:34:32.000Z",
		"with an offset":       "2026-10-09T14:34:32.500+02:00",
		"earlier":              "2026-10-09T12:34:31Z",
	} {
		if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String(title), "publishedAt": store.String(publishedAt)}, ridu.MutationOptions{System: true}); err != nil {
			t.Fatal(err)
		}
	}
	path, err := query.NewPath("publishedAt")
	if err != nil {
		t.Fatal(err)
	}
	second := time.Date(2026, 10, 9, 12, 34, 32, 0, time.UTC)
	titles := func(where query.Expression) []string {
		t.Helper()
		page, err := application.Local().List(ctx, "posts", ridu.ListOptions{Where: where, Sort: []query.Sort{{Path: path, Direction: query.Descending}}, System: true})
		if err != nil {
			t.Fatal(err)
		}
		var found []string
		for _, document := range page.Documents {
			title, _ := document.Values["title"].StringValue()
			found = append(found, title)
		}
		return found
	}
	if found := titles(query.Equal(path, query.DateTime(second))); len(found) != 2 {
		t.Fatalf("posts at %s = %v", second, found)
	}
	if found := titles(query.LessThan(path, query.DateTime(second.Add(500*time.Millisecond)))); len(found) != 3 || found[2] != "earlier" {
		t.Fatalf("posts before %s = %v", second.Add(500*time.Millisecond), found)
	}
	if found := titles(nil); len(found) != 4 || found[0] != "with an offset" || found[3] != "earlier" {
		t.Fatalf("posts newest first = %v", found)
	}
}
