package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Membership filters compile to exact SQL for every set-valued kind, through
// groups, localized containers, arrays and blocks. The matcher remains the
// oracle: stored values include shapes the manifest no longer describes, which
// must match exactly what the matcher matches.
func TestSQLiteMembershipFiltersCompileToExactSQL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "SQLite membership",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{Slug: "people", Fields: field.Fields{field.Text("name")}},
			{Slug: "teams", Fields: field.Fields{field.Text("name")}},
			{Slug: "media", Upload: true, Fields: field.Fields{field.Text("alt")}},
			{Slug: "posts", Fields: field.Fields{
				field.TextList("tags"),
				field.NumberList("sizes"),
				field.MultiSelect("topics", "news", "sports", "tech"),
				field.MultiSelect("translatedTopics", "news", "sports", "tech").Localized(),
				field.Relationships("authors", "people"),
				field.Uploads("gallery", "media"),
				field.PolymorphicRelationships("subjects", "people", "teams"),
				field.PolymorphicRelationship("subject", "people", "teams"),
				field.Group("details", field.Fields{field.Relationships("editors", "people")}),
				field.Group("translated", field.Fields{field.TextList("labels")}).Localized(),
				field.Array("rows", field.Fields{
					field.Relationships("people", "people"),
					field.PolymorphicRelationships("links", "people", "teams"),
					field.TextList("labels").Localized(),
				}),
				field.Blocks("layout",
					field.Block{Slug: "cards", Fields: field.Fields{field.NumberList("sizes")}},
					field.Block{Slug: "quote", Fields: field.Fields{field.NumberList("sizes")}},
				),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	posts := sqliteCollectionBySlug(t, manifest, "posts")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	// Raw rows keep shapes the operation engine would reject: the matcher
	// defines what each of them means.
	documents := map[string]string{
		"alpha": `{"tags":["news","tech"],"sizes":[1,2.5],"topics":["news"],"translatedTopics":{"en":["news"],"fr":["sports"]},
			"authors":["ada","grace"],"gallery":["m1"],
			"subjects":[{"relationTo":"people","id":"ada"},{"relationTo":"teams","id":"red"}],"subject":{"relationTo":"people","id":"shared"},
			"details":{"editors":["ada"]},"translated":{"en":{"labels":["x"]},"fr":{"labels":["y"]}},
			"rows":[{"people":["grace"],"links":[{"relationTo":"teams","id":"shared"}],"labels":{"en":["row"]}}],
			"layout":[{"blockType":"cards","sizes":[3]},{"blockType":"quote","sizes":[4]}]}`,
		"beta": `{"tags":["sports"],"sizes":[2],"topics":["sports"],"translatedTopics":{"en":["tech"]},
			"authors":["grace"],"gallery":[],"subjects":[{"relationTo":"teams","id":"shared"}],"subject":{"relationTo":"teams","id":"red"},
			"details":{"editors":[]},"translated":{"fr":null,"en":{"labels":["x"]}},
			"rows":[{"people":[],"links":[],"labels":{"fr":[],"en":["row"]}},{"people":["ada"]}],
			"layout":[{"blockType":"quote","sizes":[3]}]}`,
		"empty": `{"tags":[],"sizes":[],"topics":[],"translatedTopics":{"en":[],"fr":[]},"authors":[],"gallery":[],"subjects":[],
			"details":{},"translated":{"en":{"labels":[]}},"rows":[],"layout":[]}`,
		"nulls": `{"tags":null,"sizes":null,"topics":null,"translatedTopics":{"en":null,"fr":null},"authors":null,"gallery":null,"subjects":null,"subject":null,
			"details":null,"translated":null,"rows":[{"people":null,"links":null,"labels":null}],"layout":[{"blockType":"cards","sizes":null}]}`,
		"missing": `{}`,
		"shapes": `{"tags":"news","sizes":{"a":1},"topics":[["news"],{"news":"news"},null,"tech"],"translatedTopics":{"fr":"","en":["news"]},
			"authors":[1,"ada",true],"gallery":"m1",
			"subjects":{"relationTo":"people","id":"ada"},"subject":[{"relationTo":"teams","id":"red"},{"relationTo":"people","id":5}],
			"details":[{"editors":["ada"]}],"translated":{"en":"x"},
			"rows":{"0":{"people":["grace"]}},
			"layout":[{"blockType":7,"sizes":[3]},"cards",{"blockType":"quote","sizes":"4"},{"blockType":"cards","sizes":[3.0,"3"]}]}`,
		"lookalikes": `{"tags":["NEWS","new","wholesale",1],"sizes":[1.0000001,"1",true,9007199254740993],"authors":[["ada"],{"id":"ada"}],
			"subjects":[{"relationTo":"people","id":{"a":1}},{"relationTo":["people"],"id":"ada"},{"relationTo":"people","id":"grace","extra":1}],
			"rows":["grace",{"people":"grace","links":{"relationTo":"teams","id":"shared"}}],
			"layout":{"blockType":"cards","sizes":[3]}}`,
	}
	if err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		now := encodeTime(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC))
		for id, values := range documents {
			if _, err := connection.ExecContext(ctx, `INSERT INTO ridu_documents (
  collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json
) VALUES (?, ?, ?, ?, NULL, '', 0, json(?))`, string(posts.ID), id, now, now, values); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := loadCollectionDocumentRecords(ctx, backend.db, posts)
	if err != nil {
		t.Fatal(err)
	}

	person := func(id string) query.Value { return query.Reference("people", id) }
	team := func(id string) query.Value { return query.Reference("teams", id) }
	for _, test := range []struct {
		name  string
		where query.Expression
	}{
		{"text list in", query.In("tags", "news")},
		{"text list in any", query.In("tags", "news", "sports", "1")},
		{"text list not in", query.Not(query.In("tags", "news"))},
		{"text list exists", query.Exists("tags", true)},
		{"text list not exists", query.Exists("tags", false)},
		{"text list equals null", query.Equal("tags", query.Null())},
		{"text list not equals null", query.NotEqual("tags", query.Null())},
		{"text list empty candidates", query.In("tags", []string{}...)},
		{"number list in", query.In("sizes", 1, 2.5)},
		{"number list not in", query.Not(query.In("sizes", 1))},
		// Go reads 2^53+1 as the float64 2^53.
		{"number list in float64 space", query.In("sizes", 9007199254740992)},
		{"select in", query.In("topics", "news")},
		{"select not exists", query.Exists("topics", false)},
		{"localized select in", query.In("translatedTopics", "news")},
		{"localized select not in", query.Not(query.In("translatedTopics", "sports"))},
		{"localized select exists", query.Exists("translatedTopics", true)},
		{"localized select equals null", query.Equal("translatedTopics", query.Null())},
		{"relationship in", query.In("authors", "ada")},
		{"relationship not in", query.Not(query.In("authors", "ada", "grace"))},
		{"upload in", query.In("gallery", "m1")},
		{"upload exists", query.Exists("gallery", true)},
		{"polymorphic in", query.In("subjects", person("ada"))},
		{"polymorphic in any", query.In("subjects", person("ada"), team("shared"), person("grace"))},
		// An object member is JSON text to json_extract, never a string ID.
		{"polymorphic object id", query.In("subjects", person(`{"a":1}`))},
		{"polymorphic not in", query.Not(query.In("subjects", team("red")))},
		{"polymorphic exists", query.Exists("subjects", true)},
		{"polymorphic singular in", query.In("subject", person("shared"))},
		{"polymorphic singular in any", query.In("subject", team("red"), person("grace"))},
		{"polymorphic singular not in", query.Not(query.In("subject", person("shared")))},
		{"polymorphic singular not exists", query.Exists("subject", false)},
		{"polymorphic singular equals null", query.Equal("subject", query.Null())},
		{"group relationship in", query.In("details.editors", "ada")},
		{"group relationship equals null", query.Equal("details.editors", query.Null())},
		{"localized group in", query.In("translated.labels", "x")},
		{"localized group not exists", query.Exists("translated.labels", false)},
		{"array relationship in", query.In("rows.people", "grace", "ada")},
		{"array relationship not in", query.Not(query.In("rows.people", "grace"))},
		{"array relationship exists", query.Exists("rows.people", true)},
		{"array relationship not exists", query.Exists("rows.people", false)},
		{"array relationship equals null", query.Equal("rows.people", query.Null())},
		{"array relationship not equals null", query.NotEqual("rows.people", query.Null())},
		{"array polymorphic in", query.In("rows.links", team("shared"))},
		{"array localized list in", query.In("rows.labels", "row")},
		{"array localized list exists", query.Exists("rows.labels", true)},
		{"block number list in", query.In("layout.cards.sizes", 3)},
		{"other block number list in", query.In("layout.quote.sizes", 3, 4)},
		{"block number list not exists", query.Exists("layout.quote.sizes", false)},
		{"block number list equals null", query.Equal("layout.cards.sizes", query.Null())},
		{"or across kinds", query.Or(query.In("authors", "ada"), query.In("subject", team("red")))},
		{"and across kinds", query.And(query.In("tags", "news"), query.Not(query.In("sizes", 2)))},
		{"not of and", query.Not(query.And(query.In("topics", "news"), query.Exists("rows.people", true)))},
	} {
		for _, chain := range [][]schema.LocaleCode{{"en"}, {"fr"}, {"fr", "en"}} {
			t.Run(test.name+"/"+sqliteLocaleChainKey(chain), func(t *testing.T) {
				node := test.where.Node()
				request := store.Request{
					Collection: posts, Filter: &node, Page: 1, Limit: 100,
					Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: chain,
				}
				assertSQLiteMembershipNative(t, ctx, backend, request, stored)
				// Access rules for all-locale reads hold in every locale.
				request.Filter, request.Access, request.AllLocales = nil, &node, true
				assertSQLiteMembershipNative(t, ctx, backend, request, stored)
			})
		}
	}
}

func assertSQLiteMembershipNative(t *testing.T, ctx context.Context, backend *Store, request store.Request, stored []store.Document) {
	t.Helper()
	predicate := sqliteRequestPredicate(request)
	defer predicate.close()
	if !predicate.exact || predicate.release != nil || strings.Contains(predicate.clause, sqliteDocumentMatcherFunction) {
		t.Fatalf("membership predicate left native SQL: %s", predicate.clause)
	}
	var want []string
	for _, document := range stored {
		if documentMatchesRequest(document, request) {
			want = append(want, document.ID)
		}
	}
	sort.Strings(want)
	// List re-checks returned rows with the matcher, which would hide an
	// overly broad clause, so read the clause's own matches first.
	rows, err := backend.db.QueryContext(ctx, "SELECT id FROM ridu_documents WHERE "+predicate.clause+" ORDER BY id", predicate.arguments...)
	if err != nil {
		t.Fatalf("%v: %s", err, predicate.clause)
	}
	defer rows.Close()
	var selected []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		selected = append(selected, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(selected, ",") != strings.Join(want, ",") {
		t.Fatalf("SQL selected %v, matcher matched %v: %s", selected, want, predicate.clause)
	}
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	page, err := read.List(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, document := range page.Documents {
		listed = append(listed, document.ID)
	}
	if strings.Join(listed, ",") != strings.Join(want, ",") || page.Total == nil || *page.Total != len(want) {
		t.Fatalf("list returned %v (total %v), matcher matched %v", listed, page.Total, want)
	}
}
