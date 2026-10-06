// Package querysemantics verifies that membership filters on set-valued
// fields, text matching inside JSON values and sort admission mean the same
// thing on every store adapter and through every transport.
package querysemantics

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Factory opens a migrated application on the store under test.
type Factory func(*testing.T, ridu.Config) (store.Store, *ridu.App)

// Options records a store's documented, narrower query surface.
type Options struct {
	// JSONSubPathsUnsupported marks a store that rejects paths inside a JSON
	// value with unsupported_path instead of comparing them.
	JSONSubPathsUnsupported bool
}

const (
	people   = "qs-people"
	teams    = "qs-teams"
	media    = "qs-media"
	articles = "qs-articles"
	// Collections whose access rules return predicates outside the contract.
	guardedReads  = "qs-guarded-reads"
	guardedWrites = "qs-guarded-writes"
)

// Run exercises the shared membership, JSON text and sort truth tables.
func Run(t *testing.T, factory Factory, options Options) {
	t.Helper()
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, app := factory(t, configuration(storage))
	fixture := seed(t, app)
	var serverErrors []error
	handler := app.Handler(ridu.HandlerOptions{RequestError: func(event ridu.RequestErrorEvent) {
		serverErrors = append(serverErrors, event.Error)
	}})

	t.Run("membership", func(t *testing.T) {
		for _, test := range fixture.cases() {
			t.Run(test.name, func(t *testing.T) {
				fixture.expect(t, app, test.where, test.locale, test.want)
			})
		}
	})

	t.Run("json-text", func(t *testing.T) {
		for _, test := range []testCase{
			// Contains is a case-insensitive substring of a stored string. A JSON
			// array or object never contains text, even when an item would.
			{name: "contains", where: query.Contains("metadata", "plan"), want: []string{"alpha"}},
			{name: "contains-ignores-case", where: query.Contains("metadata", "LAUNCH p"), want: []string{"alpha"}},
			{name: "like", where: query.Like("metadata", "plan launch"), want: []string{"alpha"}},
			{name: "equals-ignores-array-items", where: query.Equal("metadata", "launch plan"), want: []string{}},
			{name: "in-ignores-array-items", where: query.In("metadata", "launch plan", "x"), want: []string{}},
			{name: "not-contains", where: query.Not(query.Contains("metadata", "plan")), want: []string{"beta", "delta", "gamma"}},
			{name: "exists", where: query.Exists("metadata", true), want: []string{"alpha", "beta", "gamma"}},
		} {
			t.Run(test.name, func(t *testing.T) { fixture.expect(t, app, test.where, "", test.want) })
		}
		for _, test := range []testCase{
			{name: "sub-path-contains", where: query.Contains("metadata.note", "PLAN"), want: []string{"gamma"}},
			// A path does not reach through an array inside a JSON value.
			{name: "sub-path-through-array", where: query.Contains("metadata.items.note", "plan"), want: []string{}},
			{name: "sub-path-through-array-exists", where: query.Exists("metadata.items.note", true), want: []string{}},
		} {
			t.Run(test.name, func(t *testing.T) {
				if !options.JSONSubPathsUnsupported {
					fixture.expect(t, app, test.where, "", test.want)
					return
				}
				_, err := app.Local().List(t.Context(), articles, ridu.ListOptions{Where: test.where})
				badQuery(t, err, "unsupported_path", test.where.Node().Comparison.Path.String())
			})
		}
	})

	t.Run("unsupported-operators-fail-before-storage", func(t *testing.T) {
		ada, red := query.Reference(people, "ada"), query.Reference(teams, "red")
		for _, expression := range []query.Expression{
			query.Equal("tags", "news"),
			query.NotEqual("tags", "news"),
			query.Contains("tags", "news"),
			query.Like("tags", "news"),
			query.Equal("authors", "ada"),
			query.NotEqual("authors", "ada"),
			query.Contains("authors", "ada"),
			query.GreaterThan("gallery", "a"),
			query.In("authors", query.Null()),
			query.In("authors", query.String("ada"), query.Null()),
			query.Equal("details.editors", "ada"),
			query.Equal("rows.people", "grace"),
			query.In("subjects", "ada"),
			query.Equal("subjects", ada),
			query.Equal("subject", ada),
			query.Equal("subject", "ada"),
			query.In("subject", query.Reference(media, "m1")),
			query.In("subjects", query.Reference(people, "")),
			query.In("authors", ada),
			query.Equal("title", red),
			query.In("id", red),
			query.Not(query.Equal("tags", "news")),
		} {
			comparison := comparisonOf(expression)
			for _, options := range []ridu.ListOptions{{Where: expression}, {Where: expression, SkipTotal: true, Locale: "fr"}} {
				_, err := app.Local().List(t.Context(), articles, options)
				badQuery(t, err, "unsupported_operator", comparison.Path.String())
			}
		}
	})

	t.Run("rest-where", func(t *testing.T) {
		for _, test := range []struct {
			where, locale string
			want          int
		}{
			{`{"tags":{"in":["news","sports"]}}`, "", 2},
			{`{"not":{"tags":{"in":["news"]}}}`, "", 3},
			{`{"authors":{"in":["ada"]}}`, "", 1},
			{`{"gallery":{"in":["` + fixture.media + `"]}}`, "", 1},
			{`{"details.editors":{"in":["ada"]}}`, "", 1},
			{`{"subjects":{"in":[{"relationTo":"qs-people","id":"ada"},{"relationTo":"qs-teams","id":"shared"}]}}`, "", 2},
			{`{"subject":{"in":[{"relationTo":"qs-teams","id":"red"}]}}`, "", 1},
			{`{"rows.links":{"in":[{"relationTo":"qs-teams","id":"shared"}]}}`, "", 1},
			{`{"translatedTags":{"in":["sports"]}}`, "fr", 1},
			{`{"subject":{"exists":false}}`, "", 2},
			{`{"tags":{"equals":null}}`, "", 1},
			{`{"metadata":{"contains":"PLAN"}}`, "", 1},
		} {
			target := "?where=" + url.QueryEscape(test.where)
			if test.locale != "" {
				target += "&locale=" + test.locale
			}
			for _, route := range []string{"", "/count"} {
				response := request(handler, "/api/collections/"+articles+route+target)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), fmt.Sprintf(`"totalDocs":%d`, test.want)) {
					t.Errorf("GET %s%s = %d %s, want totalDocs %d", route, target, response.Code, response.Body.String(), test.want)
				}
			}
		}
		for _, test := range []struct{ where, path string }{
			{`{"authors":{"equals":"ada"}}`, "authors"},
			{`{"tags":{"contains":"news"}}`, "tags"},
			{`{"subjects":{"in":["ada"]}}`, "subjects"},
			{`{"subject":{"equals":{"relationTo":"qs-people","id":"ada"}}}`, "subject"},
			{`{"subjects":{"in":[{"relationTo":"qs-media","id":"m1"}]}}`, "subjects"},
			{`{"title":{"in":[{"relationTo":"qs-people","id":"ada"}]}}`, "title"},
		} {
			response := request(handler, "/api/collections/"+articles+"?where="+url.QueryEscape(test.where))
			badQueryHTTP(t, response, "unsupported_operator", test.path)
		}
		response := request(handler, "/api/collections/"+articles+"?where="+url.QueryEscape(`{"subjects":{"in":[{"relationTo":"qs-people","id":"ada","extra":1}]}}`))
		if response.Code != http.StatusBadRequest {
			t.Errorf("a reference with an unknown member = %d %s, want 400", response.Code, response.Body.String())
		}
	})

	t.Run("invalid-access-predicates-fail-closed", func(t *testing.T) {
		// Each rule is checked where it is evaluated, before any store, and
		// fails with a server error naming the collection, rule, operation,
		// path and operator rather than a caller error.
		readID, writeID := fixture.guardedRead, fixture.guardedWrite
		_, err := app.Local().List(t.Context(), guardedReads, ridu.ListOptions{})
		invalidAccessPredicate(t, err, guardedReads, "read", "read", "tags", "equal", "unsupported_operator")
		_, err = app.Local().Find(t.Context(), guardedReads, readID, ridu.FindOptions{})
		invalidAccessPredicate(t, err, guardedReads, "read", "read", "tags", "equal", "unsupported_operator")
		_, err = app.Local().Update(t.Context(), guardedWrites, writeID, store.Values{"title": store.String("changed")}, ridu.MutationOptions{})
		invalidAccessPredicate(t, err, guardedWrites, "update", "update", "owner.name", "equal", "invalid_path")
		_, err = app.Local().Delete(t.Context(), guardedWrites, writeID, ridu.MutationOptions{})
		invalidAccessPredicate(t, err, guardedWrites, "delete", "delete", "authors", "contains", "unsupported_operator")
		// Trusted server code is not subject to the rules, and the failed
		// writes changed nothing.
		document, err := app.Local().Find(t.Context(), guardedWrites, writeID, ridu.FindOptions{System: true})
		if title, _ := document.Values["title"].StringValue(); err != nil || title != "guarded" {
			t.Fatalf("guarded document after rejected writes = %q, %v", title, describe(err))
		}

		for _, test := range []struct {
			method, target, slug, role, path string
		}{
			{http.MethodGet, "/api/collections/" + guardedReads, guardedReads, "read", "tags"},
			{http.MethodGet, "/api/collections/" + guardedReads + "/count", guardedReads, "read", "tags"},
			{http.MethodDelete, "/api/collections/" + guardedWrites + "/" + writeID, guardedWrites, "delete", "authors"},
		} {
			serverErrors = nil
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, "http://ridu.test"+test.target, nil))
			body := response.Body.String()
			if response.Code != http.StatusInternalServerError || strings.Contains(body, `"docs"`) || strings.Contains(body, test.path) {
				t.Errorf("%s %s = %d %s, want a redacted 500", test.method, test.target, response.Code, body)
			}
			if len(serverErrors) != 1 {
				t.Errorf("%s %s reported %d server errors, want 1", test.method, test.target, len(serverErrors))
				continue
			}
			var failure *operation.Error
			if !errors.As(serverErrors[0], &failure) || failure.Code != "invalid_access_predicate" || len(failure.Issues) != 1 || failure.Issues[0].Path != test.path ||
				!strings.Contains(failure.Message, fmt.Sprintf("collection %q %s access rule", test.slug, test.role)) {
				t.Errorf("%s %s server error = %s", test.method, test.target, describe(serverErrors[0]))
			}
		}
		if _, err := app.Local().Find(t.Context(), guardedWrites, writeID, ridu.FindOptions{System: true}); err != nil {
			t.Fatalf("a rejected REST delete removed the document: %s", describe(err))
		}
	})

	t.Run("sorts", func(t *testing.T) {
		for _, name := range []string{
			"details", "metadata", "metadata.note", "body", "translated", "translated.title",
			"tags", "translatedTags", "authors", "subjects", "subject", "gallery", "rows", "rows.label",
		} {
			for _, direction := range []query.Direction{query.Ascending, query.Descending} {
				_, err := app.Local().List(t.Context(), articles, ridu.ListOptions{Sort: []query.Sort{{Path: path(name), Direction: direction}}})
				badQuery(t, err, "unsupported_path", name)
			}
			for _, term := range []string{name, "-" + name} {
				badQueryHTTP(t, request(handler, "/api/collections/"+articles+"?sort="+url.QueryEscape(term)), "unsupported_path", name)
			}
		}
		for _, test := range []struct {
			sort []query.Sort
			want string
		}{
			{[]query.Sort{query.Desc("details.summary"), query.Asc("title")}, "alpha,beta,delta,gamma"},
			{[]query.Sort{query.Asc("details.summary"), query.Desc("title")}, "gamma,delta,beta,alpha"},
		} {
			page, err := app.Local().List(t.Context(), articles, ridu.ListOptions{Sort: test.sort, Limit: 10})
			if err != nil {
				t.Fatalf("sort %v: %s", test.sort, describe(err))
			}
			if got := strings.Join(fixture.ordered(page.Documents), ","); got != test.want {
				t.Errorf("sort %v = %s, want %s", test.sort, got, test.want)
			}
		}
		response := request(handler, "/api/collections/"+articles+"?sort=-details.summary&sort=title")
		if response.Code != http.StatusOK {
			t.Errorf("REST group-leaf sort = %d %s", response.Code, response.Body.String())
		}
	})
}

type testCase struct {
	name   string
	locale schema.LocaleCode
	where  query.Expression
	want   []string
}

type fixture struct {
	ids                       map[string]string
	media                     string
	guardedRead, guardedWrite string
}

func (f fixture) expect(t *testing.T, app *ridu.App, where query.Expression, locale schema.LocaleCode, want []string) {
	t.Helper()
	page, err := app.Local().List(t.Context(), articles, ridu.ListOptions{Where: where, Locale: locale, Limit: 100})
	if err != nil {
		t.Fatalf("list: %s", describe(err))
	}
	got := f.ordered(page.Documents)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") || page.Total == nil || *page.Total != len(want) {
		t.Fatalf("list = %v (total %v), want %v", got, page.Total, want)
	}
	// A one-row page still counts every match.
	page, err = app.Local().List(t.Context(), articles, ridu.ListOptions{Where: where, Locale: locale, Limit: 1})
	if err != nil || page.Total == nil || *page.Total != len(want) {
		t.Fatalf("one-row page total = %v, %v, want %d", page.Total, err, len(want))
	}
}

func (f fixture) ordered(documents []store.Document) []string {
	byID := make(map[string]string, len(f.ids))
	for title, id := range f.ids {
		byID[id] = title
	}
	result := make([]string, 0, len(documents))
	for _, document := range documents {
		result = append(result, byID[document.ID])
	}
	return result
}

func (f fixture) cases() []testCase {
	c := func(name string, where query.Expression, want ...string) testCase {
		return testCase{name: name, where: where, want: want}
	}
	l := func(name string, locale schema.LocaleCode, where query.Expression, want ...string) testCase {
		return testCase{name: name, locale: locale, where: where, want: want}
	}
	person := func(id string) query.Value { return query.Reference(people, id) }
	team := func(id string) query.Value { return query.Reference(teams, id) }
	return []testCase{
		// Has-many select: options are items.
		c("select-in", query.In("tags", "news"), "alpha"),
		c("select-in-any", query.In("tags", "news", "sports"), "alpha", "beta"),
		c("select-in-exact", query.In("tags", "new", "NEWS"), []string{}...),
		c("select-not-in", query.Not(query.In("tags", "news")), "beta", "delta", "gamma"),
		c("select-and-requires-both", query.And(query.In("tags", "news"), query.In("tags", "tech")), "alpha"),
		c("select-exists-includes-empty", query.Exists("tags", true), "alpha", "beta", "gamma"),
		c("select-not-exists", query.Exists("tags", false), "delta"),
		c("select-equals-null", query.Equal("tags", query.Null()), "delta"),
		c("select-not-equals-null", query.NotEqual("tags", query.Null()), "alpha", "beta", "gamma"),
		l("localized-select-en", "en", query.In("translatedTags", "news"), "alpha"),
		l("localized-select-fr", "fr", query.In("translatedTags", "sports"), "alpha"),
		l("localized-select-fr-shadowed", "fr", query.In("translatedTags", "news")),
		l("localized-select-fr-fallback", "fr", query.In("translatedTags", "tech"), "beta"),
		// Has-many relationships and uploads: document IDs are items.
		c("relationship-in", query.In("authors", "ada"), "alpha"),
		c("relationship-in-any", query.In("authors", "grace", "nobody"), "alpha", "beta"),
		c("relationship-not-in", query.Not(query.In("authors", "ada")), "beta", "delta", "gamma"),
		c("relationship-not-exists", query.Exists("authors", false), "delta"),
		c("relationship-in-group", query.In("details.editors", "ada"), "alpha"),
		c("relationship-in-array", query.In("rows.people", "grace"), "alpha"),
		c("relationship-not-in-array", query.Not(query.In("rows.people", "grace")), "beta", "delta", "gamma"),
		c("upload-in", query.In("gallery", f.media), "alpha"),
		c("upload-not-in", query.Not(query.In("gallery", f.media)), "beta", "delta", "gamma"),
		// Polymorphic relationships: {relationTo, id} references are items,
		// so an ID shared by two collections matches only its own.
		c("polymorphic-in", query.In("subjects", person("ada")), "alpha"),
		c("polymorphic-in-other-collection", query.In("subjects", team("shared")), "beta"),
		c("polymorphic-same-id-elsewhere", query.In("subjects", person("shared"))),
		c("polymorphic-in-any", query.In("subjects", person("ada"), team("shared")), "alpha", "beta"),
		c("polymorphic-not-in", query.Not(query.In("subjects", team("red"))), "beta", "delta", "gamma"),
		c("polymorphic-in-array", query.In("rows.links", team("shared")), "alpha"),
		c("polymorphic-singular-in", query.In("subject", person("shared")), "alpha"),
		c("polymorphic-singular-same-id-elsewhere", query.In("subject", team("shared"))),
		c("polymorphic-singular-in-any", query.In("subject", team("red"), person("grace")), "beta"),
		c("polymorphic-singular-not-in", query.Not(query.In("subject", person("shared"))), "beta", "delta", "gamma"),
		c("polymorphic-singular-not-exists", query.Exists("subject", false), "delta", "gamma"),
		c("polymorphic-singular-equals-null", query.Equal("subject", query.Null()), "delta", "gamma"),
		c("or-across-fields", query.Or(query.In("authors", "ada"), query.In("subject", team("red"))), "alpha", "beta"),
	}
}

func configuration(storage *localstorage.Backend) ridu.Config {
	allow := func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
	open := ridu.CollectionAccess{Create: allow, Read: allow, Update: allow}
	return ridu.Config{
		Name: "Query semantics", Storage: storage, StorageNamespace: "query-semantics", AllowIDOnCreate: true,
		Plugins: []ridu.Plugin{richtext.New()},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"},
			{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{Slug: people, Access: open, Fields: field.Fields{field.Text("name")}},
			{Slug: teams, Access: open, Fields: field.Fields{field.Text("name")}},
			{Slug: media, Access: open, Upload: true, UploadConfig: ridu.UploadConfig{MimeTypes: []string{"image/png"}}, Fields: field.Fields{field.Text("alt")}},
			{Slug: articles, Access: open, Fields: field.Fields{
				field.Text("title"),
				field.MultiSelect("tags", "news", "sports", "tech"),
				field.MultiSelect("translatedTags", "news", "sports", "tech").Localized(),
				field.Relationships("authors", people),
				field.PolymorphicRelationships("subjects", people, teams),
				field.PolymorphicRelationship("subject", people, teams),
				field.Uploads("gallery", media),
				field.Group("details", field.Fields{field.Relationships("editors", people), field.Text("summary")}),
				field.Array("rows", field.Fields{
					field.Text("label"),
					field.Relationships("people", people),
					field.PolymorphicRelationships("links", people, teams),
				}),
				field.JSON("metadata"),
				field.Group("translated", field.Fields{field.Text("title")}).Localized(),
				richtext.Field("body"),
			}},
			// Rules returning predicates the query contract rejects: equality on
			// a has-many select, an unknown path, and contains on a has-many
			// relationship.
			{Slug: guardedReads, Access: ridu.CollectionAccess{
				Create: allow, Update: allow,
				Read: where(query.Equal("tags", "news")),
			}, Fields: field.Fields{field.Text("title"), field.MultiSelect("tags", "news", "sports")}},
			{Slug: guardedWrites, Access: ridu.CollectionAccess{
				Create: allow, Read: allow,
				Update: where(query.Equal("owner.name", "ada")),
				Delete: where(query.Contains("authors", "ada")),
			}, Fields: field.Fields{field.Text("title"), field.Relationships("authors", people)}},
		},
	}
}

func where(expression query.Expression) ridu.AccessRule {
	return func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Where(expression), nil }
}

func seed(t *testing.T, app *ridu.App) fixture {
	t.Helper()
	result := fixture{ids: map[string]string{}}
	create := func(slug, id string, values store.Values) string {
		document, err := app.Local().Create(t.Context(), slug, values, ridu.MutationOptions{ID: id, Locale: "en"})
		if err != nil {
			t.Fatalf("create %s %s: %s", slug, id, describe(err))
		}
		return document.ID
	}
	for _, id := range []string{"ada", "grace", "shared"} {
		create(people, id, store.Values{"name": store.String(id)})
	}
	for _, id := range []string{"red", "shared"} {
		create(teams, id, store.Values{"name": store.String(id)})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	upload, err := app.Upload(t.Context(), media, ridu.UploadInput{Filename: "pixel.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	result.media = upload.ID

	texts := func(values ...string) store.Value {
		items := make([]store.Value, len(values))
		for index, value := range values {
			items[index] = store.String(value)
		}
		return store.List(items...)
	}
	reference := func(collection, id string) store.Value {
		return store.Object(store.Values{"relationTo": store.String(collection), "id": store.String(id)})
	}
	row := func(key string, values store.Values) store.Value {
		values["_key"] = store.String(key)
		return store.Object(values)
	}
	documents := map[string]store.Values{
		"alpha": {
			"tags": texts("news", "tech"), "translatedTags": texts("news"),
			"authors": texts("ada", "grace"), "gallery": texts(result.media),
			"subjects": store.List(reference(people, "ada"), reference(teams, "red")),
			"subject":  reference(people, "shared"),
			"details":  store.Object(store.Values{"editors": texts("ada"), "summary": store.String("d")}),
			"rows": store.List(row("alpha-row", store.Values{
				"label": store.String("a"), "people": texts("grace"), "links": store.List(reference(teams, "shared")),
			})),
			"metadata":   store.String("Launch PLAN"),
			"translated": store.Object(store.Values{"title": store.String("Alpha")}),
		},
		"beta": {
			"tags": texts("sports"), "translatedTags": texts("tech"),
			"authors": texts("grace"), "gallery": texts(),
			"subjects": store.List(reference(teams, "shared")),
			"subject":  reference(teams, "red"),
			"details":  store.Object(store.Values{"editors": texts(), "summary": store.String("c")}),
			"rows":     store.List(row("beta-row", store.Values{"label": store.String("b"), "people": texts(), "links": store.List()})),
			"metadata": store.List(store.String("launch plan"), store.String("x")),
		},
		"gamma": {
			"tags": texts(), "authors": texts(), "subjects": store.List(),
			"details": store.Object(store.Values{"summary": store.String("a")}),
			"metadata": store.Object(store.Values{
				"note":  store.String("Launch plan"),
				"items": store.List(store.Object(store.Values{"note": store.String("launch plan")})),
			}),
		},
		"delta": {"details": store.Object(store.Values{"summary": store.String("b")})},
	}
	for _, title := range []string{"alpha", "beta", "gamma", "delta"} {
		values := documents[title]
		values["title"] = store.String(title)
		result.ids[title] = create(articles, "", values)
	}
	trusted := func(slug string, values store.Values) string {
		document, err := app.Local().Create(t.Context(), slug, values, ridu.MutationOptions{System: true})
		if err != nil {
			t.Fatalf("create %s: %s", slug, describe(err))
		}
		return document.ID
	}
	result.guardedRead = trusted(guardedReads, store.Values{"title": store.String("guarded"), "tags": texts("news")})
	result.guardedWrite = trusted(guardedWrites, store.Values{"title": store.String("guarded"), "authors": texts("ada")})
	// A French list shadows the English one; beta falls back to English.
	if _, err := app.Local().Update(t.Context(), articles, result.ids["alpha"], store.Values{"translatedTags": texts("sports")}, ridu.MutationOptions{Locale: "fr"}); err != nil {
		t.Fatalf("translate alpha: %s", describe(err))
	}
	return result
}

func comparisonOf(expression query.Expression) query.Comparison {
	node := expression.Node()
	for node.Comparison == nil {
		node = node.Children[0]
	}
	return *node.Comparison
}

func path(value string) query.Path {
	parsed, err := query.ParsePath(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func badQuery(t *testing.T, err error, code, path string) {
	t.Helper()
	var failure *operation.Error
	if !errors.As(err, &failure) || failure.Status != http.StatusBadRequest || failure.Code != "bad_query" || len(failure.Issues) != 1 || failure.Issues[0].Code != code || failure.Issues[0].Path != path {
		t.Errorf("query %s error = %s, want bad_query (400) with a %s issue", path, describe(err), code)
	}
}

// invalidAccessPredicate expects the structured server error for an access
// rule whose predicate the query contract rejects.
func invalidAccessPredicate(t *testing.T, err error, collection, role, kind, path, operator, issue string) {
	t.Helper()
	var failure *operation.Error
	if !errors.As(err, &failure) || failure.Status != http.StatusInternalServerError || failure.Code != "invalid_access_predicate" ||
		len(failure.Issues) != 1 || failure.Issues[0].Code != issue || failure.Issues[0].Path != path {
		t.Errorf("%s %s rule error = %s, want invalid_access_predicate (500) with a %s issue at %s", collection, role, describe(err), issue, path)
		return
	}
	for _, fragment := range []string{
		fmt.Sprintf("collection %q %s access rule", collection, role),
		fmt.Sprintf("during the %s operation", kind),
		fmt.Sprintf("operator %q on %q", operator, path),
	} {
		if !strings.Contains(failure.Message, fragment) {
			t.Errorf("%s %s rule error %q does not name %s", collection, role, failure.Message, fragment)
		}
	}
}

func badQueryHTTP(t *testing.T, response *httptest.ResponseRecorder, code, path string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code   string         `json:"code"`
			Issues []schema.Issue `json:"issues"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
	if response.Code != http.StatusBadRequest || envelope.Error.Code != "bad_request" || len(envelope.Error.Issues) != 1 || envelope.Error.Issues[0].Code != code || envelope.Error.Issues[0].Path != path {
		t.Errorf("REST %s = %d %s, want bad_request (400) with a %s issue", path, response.Code, response.Body.String(), code)
	}
}

func describe(err error) string {
	var failure *operation.Error
	if errors.As(err, &failure) {
		encoded, _ := json.Marshal(failure.Issues)
		return fmt.Sprintf("%d %s %s %s (cause: %v)", failure.Status, failure.Code, failure.Message, encoded, failure.Cause)
	}
	return fmt.Sprint(err)
}

func request(handler http.Handler, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://ridu.test"+target, nil))
	return response
}
