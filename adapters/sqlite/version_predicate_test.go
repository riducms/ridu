package sqlite

import (
	"encoding/json"
	"reflect"
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

type sqliteVersionPredicateFixture struct {
	backend    *Store
	collection schema.Collection
	sibling    schema.Collection
	versions   []sqliteVersionPredicateSnapshot
}

type sqliteVersionPredicateSnapshot struct {
	collectionID schema.StableID
	version      store.Version
}

type sqliteVersionPredicateMode uint8

const (
	sqliteVersionNative sqliteVersionPredicateMode = iota
	sqliteVersionMatcher
)

func newSQLiteVersionPredicateFixture(t *testing.T, fields field.Fields) *sqliteVersionPredicateFixture {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "SQLite version predicates",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{Slug: "posts", Versions: true, Fields: fields},
			{Slug: "other-posts", Versions: true, Fields: fields},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := Open(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	return &sqliteVersionPredicateFixture{
		backend: backend, collection: sqliteCollectionBySlug(t, manifest, "posts"),
		sibling: sqliteCollectionBySlug(t, manifest, "other-posts"),
	}
}

func sqliteVersionPredicateDocument(revision int, values store.Values) store.Document {
	moment := time.Date(2026, time.January, 2, 12, 0, 0, 123456789, time.UTC)
	return store.Document{ID: "parent", CreatedAt: moment, UpdatedAt: moment, Status: store.StatusDraft, Revision: revision, Values: values}
}

func (fixture *sqliteVersionPredicateFixture) save(t *testing.T, collection schema.Collection, documents ...store.Document) {
	t.Helper()
	transaction, err := fixture.backend.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(t.Context()) }()
	for _, document := range documents {
		version, err := transaction.(store.VersionTransaction).SaveVersion(t.Context(), collection, document, 0)
		if err != nil {
			t.Fatal(err)
		}
		fixture.versions = append(fixture.versions, sqliteVersionPredicateSnapshot{collectionID: collection.ID, version: version})
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (fixture *sqliteVersionPredicateFixture) request(expression query.Expression) store.VersionRequest {
	node := expression.Node()
	return store.VersionRequest{
		Collection: fixture.collection, DocumentID: "parent", Access: &node,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"en"},
	}
}

// Compare both adapter surfaces with the existing snapshot matcher, independently
// of compilation, and with an explicit expected retained revision set.
func assertSQLiteVersionPredicateParity(t *testing.T, fixture *sqliteVersionPredicateFixture, request store.VersionRequest, mode sqliteVersionPredicateMode, want []int) {
	t.Helper()
	predicate := sqliteVersionPredicate(request)
	defer predicate.close()
	if !predicate.exact {
		t.Fatalf("history predicate is not exact: %s", predicate.clause)
	}
	hasMatcher := strings.Contains(predicate.clause, sqliteDocumentMatcherFunction+"(")
	if (predicate.release != nil) != hasMatcher {
		t.Fatalf("matcher registration disagrees with SQL: %s", predicate.clause)
	}
	if mode == sqliteVersionNative && hasMatcher || mode == sqliteVersionMatcher && !hasMatcher {
		t.Fatalf("history predicate has matcher=%t, want mode %d: %s", hasMatcher, mode, predicate.clause)
	}
	var oracle []int
	for _, saved := range fixture.versions {
		if saved.collectionID != request.Collection.ID || saved.version.DocumentID != request.DocumentID {
			continue
		}
		snapshot := store.CloneDocument(saved.version.Snapshot)
		snapshot.Values = currentValues(request.Collection.Fields, snapshot.Values)
		if matchesRequest(snapshot, store.Request{
			Collection: request.Collection, Access: request.Access, Locales: request.Locales,
			LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
		}) {
			oracle = append(oracle, saved.version.Revision)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(oracle)))
	if !reflect.DeepEqual(oracle, want) {
		t.Fatalf("fixture matcher revisions = %v, want %v", oracle, want)
	}
	read, err := fixture.backend.BeginSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(t.Context()) }()
	versions := read.(store.VersionTransaction)
	count, err := versions.CountVersions(t.Context(), request)
	if err != nil || count != len(want) {
		t.Fatalf("history count = %d, %v; want %d", count, err, len(want))
	}
	items, err := versions.ListVersions(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, item := range items {
		got = append(got, item.Revision)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history revisions = %v, want %v", got, want)
	}
}

func TestSQLiteVersionPredicatesKeepScalarAccessNative(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{
		field.Text("owner"), field.Text("note"), field.Number("score"), field.Checkbox("enabled"),
		field.Group("details", field.Fields{field.Text("label"), field.Number("rank")}),
	})
	const large = float64(9223372036854774784)
	values := []store.Values{
		{"owner": store.String("a"), "details": store.Object(store.Values{"label": store.String("alpha"), "rank": store.Number(1)})},
		{"owner": store.String("a"), "note": store.Null(), "score": store.Null(), "enabled": store.Boolean(false), "details": store.Object(store.Values{"label": store.String("beta"), "rank": store.Number(2)})},
		{"owner": store.String("b"), "note": store.Number(42), "score": store.String("42"), "enabled": store.Number(1), "details": store.Object(store.Values{"label": store.String("gamma"), "rank": store.Number(3)})},
		{"owner": store.String("a"), "note": store.String("éclair"), "score": store.Number(large), "enabled": store.Boolean(true), "details": store.Object(store.Values{"label": store.String("delta"), "rank": store.Number(4)})},
		{"owner": store.String("b"), "note": store.String("equal"), "score": store.Number(42), "enabled": store.Boolean(true), "details": store.Null()},
		{"owner": store.String("a"), "note": store.String("other"), "score": store.Number(0), "enabled": store.Boolean(false), "details": store.Object(store.Values{"label": store.String("zeta"), "rank": store.Number(6)})},
	}
	for index, value := range values {
		document := sqliteVersionPredicateDocument(index+1, value)
		if index == 3 || index == 4 {
			document.Status = store.StatusPublished
		}
		fixture.save(t, fixture.collection, document)
	}
	fixture.save(t, fixture.sibling, sqliteVersionPredicateDocument(99, store.Values{"owner": store.String("a")}))
	unrelated := sqliteVersionPredicateDocument(100, store.Values{"owner": store.String("a")})
	unrelated.ID = "unrelated-parent"
	fixture.save(t, fixture.collection, unrelated)

	for _, test := range []struct {
		name       string
		expression query.Expression
		want       []int
	}{
		{"owner", query.Equal("owner", "a"), []int{6, 4, 2, 1}},
		{"group text", query.Equal("details.label", "alpha"), []int{1}},
		{"group number", query.GreaterThan("details.rank", 2), []int{6, 4, 3}},
		{"boolean true rejects numeric one", query.Equal("enabled", true), []int{5, 4}},
		{"boolean false", query.Equal("enabled", false), []int{6, 2}},
		{"number rejects numeric text", query.Equal("score", 42), []int{5}},
		{"large number equality", query.Equal("score", large), []int{4}},
		{"large number inclusive range", query.GreaterThanEqual("score", large), []int{4}},
		{"number zero", query.Equal("score", 0), []int{6}},
		{"number null", query.Equal("score", query.Null()), []int{2, 1}},
		{"text rejects number", query.Equal("note", "42"), nil},
		{"text binary range", query.GreaterThan("note", "m"), []int{6, 4}},
		{"text null", query.Equal("note", query.Null()), []int{2, 1}},
		{"text not equal", query.NotEqual("note", "equal"), []int{6, 4, 3, 2, 1}},
		{"text not null", query.NotEqual("note", query.Null()), []int{6, 5, 4, 3}},
		{"exists", query.Exists("note", true), []int{6, 5, 4, 3}},
		{"missing or null", query.Exists("note", false), []int{2, 1}},
		{"in with null", query.In("note", query.String("equal"), query.Null()), []int{5, 2, 1}},
		{"empty in", query.In("owner", []string{}...), nil},
		{"and", query.And(query.Equal("owner", "a"), query.GreaterThan("score", 0)), []int{4}},
		{"or", query.Or(query.Equal("owner", "b"), query.Equal("note", query.Null())), []int{5, 3, 2, 1}},
		{"not equality", query.Not(query.Equal("note", "equal")), []int{6, 4, 3, 2, 1}},
		{"not range includes absent and wrong types", query.Not(query.GreaterThan("note", "m")), []int{5, 3, 2, 1}},
		{"snapshot id", query.Equal("id", "parent"), []int{6, 5, 4, 3, 2, 1}},
		{"version row id is not snapshot id", query.Equal("id", "parent:6"), nil},
		{"snapshot status", query.Equal("_status", "published"), []int{5, 4}},
		{"snapshot revision", query.In("_revision", 1, 4), []int{4, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSQLiteVersionPredicateParity(t, fixture, fixture.request(test.expression), sqliteVersionNative, test.want)
		})
	}
	for _, test := range []struct {
		name       string
		expression query.Expression
		want       []int
	}{
		{"historical number in text equals", query.Equal("note", 42), []int{3}},
		{"historical number in text not equals", query.NotEqual("note", 42), []int{6, 5, 4, 2, 1}},
		{"historical number in text negated equality", query.Not(query.Equal("note", 42)), []int{6, 5, 4, 2, 1}},
		{"historical number in text range", query.GreaterThan("note", 40), []int{3}},
		{"historical number in text negated range", query.Not(query.GreaterThan("note", 40)), []int{6, 5, 4, 2, 1}},
		{"historical string in number equals", query.Equal("score", "42"), []int{3}},
		{"historical string in number not equals", query.NotEqual("score", "42"), []int{6, 5, 4, 2, 1}},
		{"historical string in number range", query.GreaterThan("score", "4"), []int{3}},
		{"historical string in number negated range", query.Not(query.GreaterThan("score", "4")), []int{6, 5, 4, 2, 1}},
		{"historical number in checkbox equals", query.Equal("enabled", 1), []int{3}},
		{"historical number in checkbox not equals", query.NotEqual("enabled", 1), []int{6, 5, 4, 2, 1}},
		{"historical number in checkbox range", query.GreaterThan("enabled", 0), []int{3}},
		{"text mixed in", query.In("note", query.String("equal"), query.Number(42)), []int{5, 3}},
		{"number mixed in", query.In("score", query.Number(42), query.String("42")), []int{5, 3}},
		{"checkbox mixed in", query.In("enabled", query.Boolean(true), query.Number(1)), []int{5, 4, 3}},
		{"historical kinds in conjunction", query.And(query.Equal("owner", "b"), query.Equal("note", 42)), []int{3}},
		{"historical kinds in disjunction", query.Or(query.Equal("owner", "a"), query.Equal("note", 42)), []int{6, 4, 3, 2, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSQLiteVersionPredicateParity(t, fixture, fixture.request(test.expression), sqliteVersionMatcher, test.want)
		})
	}
	request := fixture.request(query.Equal("owner", "a"))
	request.Access = nil
	assertSQLiteVersionPredicateParity(t, fixture, request, sqliteVersionNative, []int{6, 5, 4, 3, 2, 1})
	request.DocumentID = "missing"
	assertSQLiteVersionPredicateParity(t, fixture, request, sqliteVersionNative, nil)
}

func TestSQLiteVersionPredicatesReadHistoricalBooleanInTextField(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{field.Text("note")})
	fixture.save(t, fixture.collection,
		sqliteVersionPredicateDocument(1, store.Values{"note": store.Boolean(true)}),
		sqliteVersionPredicateDocument(2, store.Values{"note": store.String("true")}),
	)
	for _, test := range []struct {
		name       string
		expression query.Expression
		want       []int
	}{
		{"equal", query.Equal("note", true), []int{1}},
		{"not equal", query.NotEqual("note", true), []int{2}},
		{"negated equality", query.Not(query.Equal("note", true)), []int{2}},
		{"mixed in", query.In("note", query.String("true"), query.Boolean(true)), []int{2, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSQLiteVersionPredicateParity(t, fixture, fixture.request(test.expression), sqliteVersionMatcher, test.want)
		})
	}
	assertSQLiteVersionPredicateParity(t, fixture, fixture.request(query.Equal("note", "true")), sqliteVersionNative, []int{2})
}

func TestSQLiteVersionPredicatesUseSnapshotMetadataPresence(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{field.Text("owner")})
	for _, revision := range []int{-1, 0, 1} {
		document := sqliteVersionPredicateDocument(revision, store.Values{"owner": store.String("a")})
		if revision <= 0 {
			document.Status = ""
		}
		fixture.save(t, fixture.collection, document)
	}
	// Version-row metadata can differ from snapshot metadata. Authorization is
	// defined against the snapshot, including its ID and logical presence rules.
	snapshot := sqliteVersionPredicateDocument(2, store.Values{"owner": store.String("a")})
	snapshot.ID = "snapshot-id"
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.backend.db.ExecContext(t.Context(), `INSERT INTO ridu_versions
  (id, collection_id, document_id, revision, status, snapshot_json, created_at)
VALUES ('outer-version-id', ?, 'parent', 99, 'published', ?, ?)`, string(fixture.collection.ID), string(encoded), encodeTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	fixture.versions = append(fixture.versions, sqliteVersionPredicateSnapshot{collectionID: fixture.collection.ID,
		version: store.Version{DocumentID: "parent", Revision: 99, Status: store.StatusPublished, Snapshot: snapshot}})
	for _, test := range []struct {
		name       string
		expression query.Expression
		want       []int
	}{
		{"snapshot id differs from parent", query.Equal("id", "snapshot-id"), []int{99}},
		{"outer id is ignored", query.Equal("id", "outer-version-id"), nil},
		{"snapshot status overrides row status", query.Equal("_status", "published"), nil},
		{"empty status absent", query.Exists("_status", false), []int{0, -1}},
		{"empty status equals null", query.Equal("_status", query.Null()), []int{0, -1}},
		{"empty status is not empty string", query.Equal("_status", ""), nil},
		{"nonempty status present", query.Exists("_status", true), []int{99, 1}},
		{"nonpositive revision absent", query.Exists("_revision", false), []int{0, -1}},
		{"nonpositive revision equals null", query.Equal("_revision", query.Null()), []int{0, -1}},
		{"snapshot revision overrides row revision", query.Equal("_revision", 2), []int{99}},
		{"row revision is ignored", query.Equal("_revision", 99), nil},
		{"positive revision", query.GreaterThan("_revision", 0), []int{99, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSQLiteVersionPredicateParity(t, fixture, fixture.request(test.expression), sqliteVersionNative, test.want)
		})
	}
}

func TestSQLiteVersionPredicatesPreserveLocaleFallbackAndAllLocales(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{
		field.Text("title").Localized(), field.Group("meta", field.Fields{field.Text("label")}).Localized(),
	})
	english := store.Object(store.Values{"label": store.String("English")})
	french := store.Object(store.Values{"label": store.String("French")})
	values := []store.Values{
		{"title": store.Object(store.Values{"en": store.String("Shared"), "fr": store.String("Partagé")}), "meta": store.Object(store.Values{"en": english, "fr": french})},
		{"title": store.Object(store.Values{"en": store.String("Shared")}), "meta": store.Object(store.Values{"en": english})},
		{"title": store.Object(store.Values{"en": store.String("Shared"), "fr": store.Null()}), "meta": store.Object(store.Values{"en": english, "fr": store.Null()})},
		{"title": store.Object(store.Values{"en": store.String("Shared"), "fr": store.String("")}), "meta": store.Object(store.Values{"en": english, "fr": store.String("")})},
		{"title": store.Object(store.Values{"en": store.String("")}), "meta": store.Object(store.Values{"en": english})},
		{"title": store.Object(store.Values{"en": store.String("Different"), "fr": store.String("Partagé")}), "meta": store.Object(store.Values{"en": store.String("malformed"), "fr": french})},
		{"title": store.String("malformed"), "meta": store.Object(store.Values{"en": english, "fr": store.String("malformed")})},
		{"meta": store.Object(store.Values{"en": store.String(`{"label":"English"}`)})},
	}
	for index, value := range values {
		fixture.save(t, fixture.collection, sqliteVersionPredicateDocument(index+1, value))
	}
	for _, test := range []struct {
		name       string
		expression query.Expression
		chain      []schema.LocaleCode
		all        bool
		mode       sqliteVersionPredicateMode
		want       []int
	}{
		{"English", query.Equal("title", "Shared"), []schema.LocaleCode{"en"}, false, sqliteVersionNative, []int{4, 3, 2, 1}},
		{"fallback skips missing null and empty", query.Equal("title", "Shared"), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionNative, []int{4, 3, 2}},
		{"no fallback", query.Equal("title", query.Null()), []schema.LocaleCode{"fr"}, false, sqliteVersionNative, []int{8, 7, 5, 3, 2}},
		{"final empty remains present", query.Equal("title", ""), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionNative, []int{5}},
		{"fallback existence", query.Exists("title", true), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionNative, []int{6, 5, 4, 3, 2, 1}},
		{"all locales uses no fallback", query.Exists("title", true), []schema.LocaleCode{"en"}, true, sqliteVersionNative, []int{6, 4, 1}},
		{"all locales requires same predicate everywhere", query.Equal("title", "Shared"), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, nil},
		{"all locales absent maps", query.Exists("title", false), []schema.LocaleCode{"en"}, true, sqliteVersionNative, []int{8, 7}},
		{"localized group valid child", query.Equal("meta.label", "French"), []schema.LocaleCode{"fr"}, false, sqliteVersionMatcher, []int{6, 1}},
		{"localized group skips null and empty", query.Equal("meta.label", "English"), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{5, 4, 3, 2}},
		{"localized group selected malformed parent does not fall back", query.Exists("meta.label", false), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{8, 7}},
		{"localized group scalar and JSON-looking scalar cannot supply child", query.Equal("meta.label", "English"), []schema.LocaleCode{"en"}, false, sqliteVersionMatcher, []int{7, 5, 4, 3, 2, 1}},
		{"localized group scalar and JSON-looking scalar child absent", query.Exists("meta.label", false), []schema.LocaleCode{"en"}, false, sqliteVersionMatcher, []int{8, 6}},
		{"all locales localized group child absent", query.Exists("meta.label", false), []schema.LocaleCode{"en"}, true, sqliteVersionMatcher, []int{8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := fixture.request(test.expression)
			request.LocaleChain, request.AllLocales = test.chain, test.all
			assertSQLiteVersionPredicateParity(t, fixture, request, test.mode, test.want)
		})
	}
}

func TestSQLiteVersionPredicatesPreserveHistoricalNonStringLocaleFallback(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{
		field.Number("score").Localized(), field.Checkbox("enabled").Localized(),
	})
	for index, value := range []store.Values{
		{"score": store.Object(store.Values{"fr": store.String(""), "en": store.Number(7)}), "enabled": store.Object(store.Values{"fr": store.String(""), "en": store.Boolean(false)})},
		{"score": store.Object(store.Values{"fr": store.String(""), "en": store.Null()}), "enabled": store.Object(store.Values{"fr": store.String(""), "en": store.Null()})},
		{"score": store.Object(store.Values{"fr": store.String("")}), "enabled": store.Object(store.Values{"fr": store.String("")})},
		{"score": store.Object(store.Values{"fr": store.Number(7), "en": store.Number(7)}), "enabled": store.Object(store.Values{"fr": store.Boolean(false), "en": store.Boolean(false)})},
	} {
		fixture.save(t, fixture.collection, sqliteVersionPredicateDocument(index+1, value))
	}
	for _, test := range []struct {
		name       string
		expression query.Expression
		chain      []schema.LocaleCode
		all        bool
		mode       sqliteVersionPredicateMode
		want       []int
	}{
		{"number fallback skips historical empty text", query.Equal("score", 7), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{4, 1}},
		{"number fallback not equal", query.NotEqual("score", 7), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{3, 2}},
		{"number fallback null", query.Equal("score", query.Null()), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{3, 2}},
		{"number fallback absence", query.Exists("score", false), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{3, 2}},
		{"number fallback presence", query.Exists("score", true), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{4, 1}},
		{"checkbox fallback selects meaningful false", query.Equal("enabled", false), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{4, 1}},
		{"checkbox fallback not equal", query.NotEqual("enabled", false), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{3, 2}},
		{"checkbox fallback null", query.Equal("enabled", query.Null()), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{3, 2}},
		{"checkbox fallback absence", query.Exists("enabled", false), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{3, 2}},
		{"checkbox fallback presence", query.Exists("enabled", true), []schema.LocaleCode{"fr", "en"}, false, sqliteVersionMatcher, []int{4, 1}},
		{"single French number equality", query.Equal("score", 7), []schema.LocaleCode{"fr"}, false, sqliteVersionNative, []int{4}},
		{"single French empty text remains present", query.Exists("score", true), []schema.LocaleCode{"fr"}, false, sqliteVersionNative, []int{4, 3, 2, 1}},
		{"single French checkbox equality", query.Equal("enabled", false), []schema.LocaleCode{"fr"}, false, sqliteVersionNative, []int{4}},
		{"single English number equality", query.Equal("score", 7), []schema.LocaleCode{"en"}, false, sqliteVersionNative, []int{4, 1}},
		{"single English null and missing", query.Equal("enabled", query.Null()), []schema.LocaleCode{"en"}, false, sqliteVersionNative, []int{3, 2}},
		{"all locales number equality", query.Equal("score", 7), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, []int{4}},
		{"all locales checkbox equality", query.Equal("enabled", false), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, []int{4}},
		{"all locales number inequality", query.NotEqual("score", 7), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, []int{3, 2}},
		{"all locales presence", query.Exists("enabled", true), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, []int{4, 1}},
		{"all locales absence", query.Exists("score", false), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, nil},
		{"all locales null", query.Equal("enabled", query.Null()), []schema.LocaleCode{"fr", "en"}, true, sqliteVersionNative, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := fixture.request(test.expression)
			request.LocaleChain, request.AllLocales = test.chain, test.all
			assertSQLiteVersionPredicateParity(t, fixture, request, test.mode, test.want)
		})
	}
}

func TestSQLiteVersionPredicatesRetainComplexMatcherSemantics(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{
		field.Text("owner"), field.Text("title"), field.Array("rows", field.Fields{field.Text("label")}),
		field.TextList("tags"), field.JSON("opaque"),
	})
	for index, value := range []store.Values{
		{"owner": store.String("a"), "title": store.String("ÉCOLE BLEUE"), "rows": store.List(store.Object(store.Values{"label": store.String("team")}), store.Object(store.Values{"label": store.String("private")})), "tags": store.List(store.String("red"), store.String("blue")), "opaque": store.Object(store.Values{"owner": store.String("a")})},
		{"owner": store.String("b"), "title": store.String("école verte"), "rows": store.List(store.Object(store.Values{"label": store.String("other")})), "tags": store.List(store.String("red")), "opaque": store.Object(store.Values{"owner": store.String("b")})},
		{"owner": store.String("a"), "title": store.String("Elsewhere"), "rows": store.List(store.Object(store.Values{"label": store.String("team")})), "tags": store.List(store.String("green")), "opaque": store.Object(store.Values{"owner": store.String("a")})},
		{"owner": store.String("b"), "title": store.String("Other"), "rows": store.List(store.Object(store.Values{"label": store.String("other")})), "tags": store.List(), "opaque": store.Null()},
	} {
		fixture.save(t, fixture.collection, sqliteVersionPredicateDocument(index+1, value))
	}
	for _, test := range []struct {
		name       string
		expression query.Expression
		want       []int
	}{
		{"repeated equality", query.Equal("rows.label", "team"), []int{3, 1}},
		{"repeated not equal requires no equal occurrence", query.NotEqual("rows.label", "team"), []int{4, 2}},
		{"primitive list membership", query.In("tags", "blue"), []int{1}},
		{"primitive empty list exists", query.Exists("tags", true), []int{4, 3, 2, 1}},
		{"opaque JSON", query.Equal("opaque.owner", "a"), []int{3, 1}},
		{"Unicode contains", query.Contains("title", "ÉCOLE"), []int{2, 1}},
		{"Unicode like", query.Like("title", "école BLEUE"), []int{1}},
		{"mixed and", query.And(query.Equal("owner", "a"), query.Contains("title", "ÉCOLE")), []int{1}},
		{"unsupported or cannot narrow to one child", query.Or(query.Equal("owner", "b"), query.Contains("title", "ÉCOLE")), []int{4, 2, 1}},
		{"unsupported not", query.Not(query.Contains("title", "ÉCOLE")), []int{4, 3}},
		{"not of mixed and", query.Not(query.And(query.Equal("owner", "a"), query.Contains("title", "ÉCOLE"))), []int{4, 3, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSQLiteVersionPredicateParity(t, fixture, fixture.request(test.expression), sqliteVersionMatcher, test.want)
		})
	}
	// Opaque plugins share the arbitrary-data path matcher with JSON fields. The
	// adapter schema fixture needs no executable plugin validator to read history.
	request := fixture.request(query.Equal("opaque.owner", "a"))
	request.Collection.Fields = append([]schema.Field(nil), request.Collection.Fields...)
	for index := range request.Collection.Fields {
		if request.Collection.Fields[index].Name == "opaque" {
			request.Collection.Fields[index].Type = schema.FieldTypePlugin
			request.Collection.Fields[index].Category = schema.FieldCategoryPlugin
			request.Collection.Fields[index].Plugin = &schema.PluginField{Key: "version-test"}
		}
	}
	assertSQLiteVersionPredicateParity(t, fixture, request, sqliteVersionMatcher, []int{3, 1})
	read, err := fixture.backend.BeginSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(t.Context()) }()
	for _, expression := range []query.Expression{query.Equal("tags", "blue"), query.In("tags", 1)} {
		request := fixture.request(expression)
		versions := read.(store.VersionTransaction)
		if _, err := versions.CountVersions(t.Context(), request); err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("invalid primitive-list count operand error = %v", err)
		}
		if _, err := versions.ListVersions(t.Context(), request); err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("invalid primitive-list history operand error = %v", err)
		}
	}
}

func TestSQLiteVersionTimestampAccessUsesPreciseSnapshotMatcher(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{field.Text("owner")})
	base := time.Date(2026, time.January, 2, 12, 0, 0, 123456789, time.UTC)
	for index, moment := range []time.Time{base, base.Add(time.Nanosecond), time.Date(2500, time.January, 2, 12, 0, 0, 123456789, time.UTC)} {
		document := sqliteVersionPredicateDocument(index+1, store.Values{"owner": store.String("a")})
		document.CreatedAt, document.UpdatedAt = moment, moment.Add(time.Nanosecond)
		fixture.save(t, fixture.collection, document)
	}
	offset := base.In(time.FixedZone("east", 3600)).Format(time.RFC3339Nano)
	for _, test := range []struct {
		name       string
		expression query.Expression
		want       []int
	}{
		{"offset equality", query.Equal("createdAt", offset), []int{1}},
		{"nanosecond range", query.GreaterThan("createdAt", offset), []int{3, 2}},
		{"updated timestamp is from snapshot", query.Equal("updatedAt", base.Add(time.Nanosecond).Format(time.RFC3339Nano)), []int{1}},
		{"outside UnixNano range", query.Equal("createdAt", "2500-01-02T12:00:00.123456789Z"), []int{3}},
		{"invalid string not equal is false", query.NotEqual("createdAt", "invalid"), nil},
		{"invalid numeric not equal is false", query.NotEqual("createdAt", 0), nil},
		{"exists", query.Exists("createdAt", true), []int{3, 2, 1}},
		{"in", query.In("createdAt", offset, "2500-01-02T12:00:00.123456789Z"), []int{3, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSQLiteVersionPredicateParity(t, fixture, fixture.request(test.expression), sqliteVersionMatcher, test.want)
		})
	}
}

func TestSQLiteNativeVersionCountDoesNotDecodeUnrelatedValues(t *testing.T) {
	t.Parallel()
	fixture := newSQLiteVersionPredicateFixture(t, field.Fields{field.Text("owner"), field.Number("unrelated")})
	const values = `{"owner":"allowed","unrelated":1e999}`
	var decoded store.Values
	if err := decoded.UnmarshalJSON([]byte(values)); err == nil {
		t.Fatal("count fixture unexpectedly decodes in Go")
	}
	// This is valid SQLite JSON. An exact owner predicate can count it without
	// decoding the unrelated number into store.Value's float64 representation.
	const snapshot = `{"ID":"parent","CreatedAt":"2026-01-02T12:00:00Z","UpdatedAt":"2026-01-02T12:00:00Z","Status":"draft","Revision":1,"Values":` + values + `}`
	if _, err := fixture.backend.db.ExecContext(t.Context(), `INSERT INTO ridu_versions
  (id, collection_id, document_id, revision, status, snapshot_json, created_at)
VALUES ('parent:1', ?, 'parent', 1, 'draft', ?, ?)`, string(fixture.collection.ID), snapshot, encodeTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	read, err := fixture.backend.BeginSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(t.Context()) }()
	for _, test := range []struct {
		owner string
		want  int
	}{{"allowed", 1}, {"denied", 0}} {
		request := fixture.request(query.Equal("owner", test.owner))
		predicate := sqliteVersionPredicate(request)
		if !predicate.exact || predicate.release != nil || strings.Contains(predicate.clause, sqliteDocumentMatcherFunction) {
			predicate.close()
			t.Fatalf("simple owner access left native SQL: %s", predicate.clause)
		}
		predicate.close()
		count, err := read.(store.VersionTransaction).CountVersions(t.Context(), request)
		if err != nil || count != test.want {
			t.Fatalf("%s count = %d, %v; want %d without decoding snapshot values", test.owner, count, err, test.want)
		}
	}
	request := fixture.request(query.And(query.Equal("owner", "denied"), query.Contains("owner", "allowed")))
	predicate := sqliteVersionPredicate(request)
	if !predicate.exact || predicate.release == nil || !strings.Contains(predicate.clause, sqliteDocumentMatcherFunction) {
		predicate.close()
		t.Fatalf("mixed access should combine native filtering with the matcher: %s", predicate.clause)
	}
	predicate.close()
	count, err := read.(store.VersionTransaction).CountVersions(t.Context(), request)
	if err != nil || count != 0 {
		t.Fatalf("mixed AND did not reject the snapshot before fallback decoding: count=%d err=%v", count, err)
	}
}
