package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLiteMaterializesEveryAcceptedIndexShape(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteIndexParityManifest(t)
	posts := sqliteCollectionBySlug(t, manifest, "posts")
	media := sqliteCollectionBySlug(t, manifest, "media")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	frWithFallback := []schema.LocaleCode{"fr", "en"}
	wanted := []string{
		documentFieldIndexName(posts.ID, "title", nil),
		documentFieldIndexName(posts.ID, "summary", nil),
		documentFieldIndexName(posts.ID, "source", nil),
		documentFieldIndexName(posts.ID, "contact", nil),
		documentFieldIndexName(posts.ID, "publishedAt", nil),
		documentFieldIndexName(posts.ID, "status", nil),
		documentFieldIndexName(posts.ID, "priority", nil),
		documentFieldIndexName(posts.ID, "score", nil),
		documentFieldIndexName(posts.ID, "externalKey", nil),
		documentFieldIndexName(posts.ID, "seo.slug", nil),
		documentFieldIndexName(posts.ID, "localizedTitle", []schema.LocaleCode{"en"}),
		documentFieldIndexName(posts.ID, "localizedTitle", []schema.LocaleCode{"fr"}),
		documentFieldIndexName(posts.ID, "localizedTitle", frWithFallback),
		documentFieldIndexName(posts.ID, "author", nil),
		documentFieldIndexName(posts.ID, "hero", nil),
		documentFieldIndexName(posts.ID, "featured", nil),
		documentFieldIndexName(media.ID, "objectKey", nil),
		documentCompoundIndexName(media.ID, []string{"filename", "filesize"}, nil),
		documentCompoundIndexName(posts.ID, []string{"tenant", "seo.slug"}, nil),
		documentCompoundIndexName(posts.ID, []string{"tenant", "localizedTitle"}, []schema.LocaleCode{"en"}),
		documentCompoundIndexName(posts.ID, []string{"tenant", "localizedTitle"}, []schema.LocaleCode{"fr"}),
		documentCompoundIndexName(posts.ID, []string{"tenant", "localizedTitle"}, frWithFallback),
	}
	for _, name := range wanted {
		var statement string
		if err := backend.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'ridu_documents' AND name = ?`, name).Scan(&statement); err != nil {
			t.Fatalf("physical index %q: %v", name, err)
		}
		if !strings.Contains(statement, "collection_id") || !strings.Contains(statement, "id COLLATE BINARY") {
			t.Fatalf("physical index %q is not collection-scoped and stably pageable: %s", name, statement)
		}
	}

	var count int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'ridu_documents' AND name LIKE ?`, documentIndexPrefix+"%").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(wanted) {
		t.Fatalf("manifest document indexes = %d, want %d", count, len(wanted))
	}
}

func TestSQLiteIndexedNestedLocalizedAndReferenceQueriesStayNative(t *testing.T) {
	ctx := context.Background()
	manifest := sqliteIndexParityManifest(t)
	posts := sqliteCollectionBySlug(t, manifest, "posts")
	users := sqliteCollectionBySlug(t, manifest, "users")
	media := sqliteCollectionBySlug(t, manifest, "media")
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		collection schema.Collection
		id         string
	}{
		{collection: users, id: "user-a"},
		{collection: users, id: "user-b"},
		{collection: media, id: "media-a"},
		{collection: media, id: "media-b"},
	} {
		values := store.Values{}
		if target.collection.ID == media.ID {
			values["objectKey"] = store.String("objects/" + target.id)
			values["filename"] = store.String(target.id + ".jpg")
			values["filesize"] = store.Number(100)
		}
		if _, err := write.Create(ctx, store.CreateRequest{Collection: target.collection, ID: target.id, Values: values}); err != nil {
			_ = write.Rollback(ctx)
			t.Fatal(err)
		}
	}
	fixtures := []struct {
		id, tenant, slug, author, hero string
		featured                       bool
		localized                      store.Values
	}{
		{id: "post-b", tenant: "acme", slug: "bravo", author: "user-b", hero: "media-b", localized: store.Values{"en": store.String("Fallback")}},
		{id: "post-a", tenant: "acme", slug: "alpha", author: "user-a", hero: "media-a", featured: true, localized: store.Values{"en": store.String("Hello"), "fr": store.String("Bonjour")}},
		{id: "post-c", tenant: "other", slug: "charlie", author: "user-a", hero: "media-b", localized: store.Values{"en": store.String("Other"), "fr": store.String("Autre")}},
	}
	for _, fixture := range fixtures {
		status := "published"
		priority := "low"
		score := 84.0
		publishedAt := "2026-08-30T12:00:00Z"
		if fixture.id == "post-a" {
			status = "draft"
			priority = "high"
			score = 42
			publishedAt = "2026-08-29T12:00:00Z"
		} else if fixture.id == "post-c" {
			score = 126
			publishedAt = "2026-08-31T12:00:00Z"
		}
		_, err := write.Create(ctx, store.CreateRequest{Collection: posts, ID: fixture.id, Values: store.Values{
			"title":          store.String(fixture.id),
			"summary":        store.String("summary-" + fixture.id),
			"source":         store.String("source-" + fixture.id),
			"contact":        store.String(fixture.id + "@example.com"),
			"publishedAt":    store.String(publishedAt),
			"status":         store.String(status),
			"priority":       store.String(priority),
			"score":          store.Number(score),
			"externalKey":    store.String("key-" + fixture.id),
			"tenant":         store.String(fixture.tenant),
			"seo":            store.Object(store.Values{"slug": store.String(fixture.slug)}),
			"localizedTitle": store.Object(fixture.localized),
			"author":         store.String(fixture.author),
			"hero":           store.String(fixture.hero),
			"featured":       store.Boolean(fixture.featured),
		}})
		if err != nil {
			_ = write.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	path := func(value string) query.Path {
		parsed, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	plannedIndexes := make(map[string]struct{})
	for _, indexed := range []struct {
		name       string
		collection schema.Collection
		field      string
		value      query.Value
		want       string
		index      string
		locales    []schema.LocaleCode
	}{
		{name: "text", collection: posts, field: "title", value: query.String("post-a"), want: "post-a", index: documentFieldIndexName(posts.ID, "title", nil)},
		{name: "textarea", collection: posts, field: "summary", value: query.String("summary-post-a"), want: "post-a", index: documentFieldIndexName(posts.ID, "summary", nil)},
		{name: "code", collection: posts, field: "source", value: query.String("source-post-a"), want: "post-a", index: documentFieldIndexName(posts.ID, "source", nil)},
		{name: "email", collection: posts, field: "contact", value: query.String("post-a@example.com"), want: "post-a", index: documentFieldIndexName(posts.ID, "contact", nil)},
		{name: "date", collection: posts, field: "publishedAt", value: query.String("2026-08-29T12:00:00Z"), want: "post-a", index: documentFieldIndexName(posts.ID, "publishedAt", nil)},
		{name: "single select", collection: posts, field: "status", value: query.String("draft"), want: "post-a", index: documentFieldIndexName(posts.ID, "status", nil)},
		{name: "radio", collection: posts, field: "priority", value: query.String("high"), want: "post-a", index: documentFieldIndexName(posts.ID, "priority", nil)},
		{name: "number", collection: posts, field: "score", value: query.Number(42), want: "post-a", index: documentFieldIndexName(posts.ID, "score", nil)},
		{name: "unique text", collection: posts, field: "externalKey", value: query.String("key-post-a"), want: "post-a", index: documentFieldIndexName(posts.ID, "externalKey", nil)},
		{name: "nested", collection: posts, field: "seo.slug", value: query.String("alpha"), want: "post-a", index: documentFieldIndexName(posts.ID, "seo.slug", nil)},
		{name: "localized exact English", collection: posts, field: "localizedTitle", value: query.String("Hello"), want: "post-a", index: documentFieldIndexName(posts.ID, "localizedTitle", []schema.LocaleCode{"en"}), locales: []schema.LocaleCode{"en"}},
		{name: "localized exact French", collection: posts, field: "localizedTitle", value: query.String("Bonjour"), want: "post-a", index: documentFieldIndexName(posts.ID, "localizedTitle", []schema.LocaleCode{"fr"}), locales: []schema.LocaleCode{"fr"}},
		{name: "localized fallback", collection: posts, field: "localizedTitle", value: query.String("Fallback"), want: "post-b", index: documentFieldIndexName(posts.ID, "localizedTitle", []schema.LocaleCode{"fr", "en"}), locales: []schema.LocaleCode{"fr", "en"}},
		{name: "relationship", collection: posts, field: "author", value: query.String("user-b"), want: "post-b", index: documentFieldIndexName(posts.ID, "author", nil)},
		{name: "upload", collection: posts, field: "hero", value: query.String("media-a"), want: "post-a", index: documentFieldIndexName(posts.ID, "hero", nil)},
		{name: "checkbox", collection: posts, field: "featured", value: query.Boolean(true), want: "post-a", index: documentFieldIndexName(posts.ID, "featured", nil)},
		{name: "upload object key", collection: media, field: "objectKey", value: query.String("objects/media-a"), want: "media-a", index: documentFieldIndexName(media.ID, "objectKey", nil)},
	} {
		t.Run(indexed.name, func(t *testing.T) {
			node := query.Equal(path(indexed.field), indexed.value).Node()
			request := store.Request{
				Collection: indexed.collection, Filter: &node, Page: 1, Limit: 10,
				Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: indexed.locales,
			}
			predicate := sqliteRequestPredicate(request)
			defer predicate.close()
			statement := `SELECT id FROM ridu_documents WHERE ` + predicate.clause + ` ORDER BY id COLLATE BINARY LIMIT ?`
			arguments := append(append([]any(nil), predicate.arguments...), 10)
			plan := sqliteExplainPlan(t, ctx, backend.db, statement, arguments...)
			if !strings.Contains(plan, "USING INDEX "+indexed.index) {
				t.Fatalf("indexed query did not use %s:\n%s\nquery: %s", indexed.index, plan, statement)
			}
			if strings.Contains(plan, "USE TEMP B-TREE") {
				t.Fatalf("indexed query used a temporary sort instead of %s:\n%s\nquery: %s", indexed.index, plan, statement)
			}
			plannedIndexes[indexed.index] = struct{}{}
			read, err := backend.BeginSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			page, err := read.List(ctx, request)
			if rollbackError := read.Rollback(ctx); err == nil && rollbackError != nil {
				err = rollbackError
			}
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != indexed.want {
				t.Fatalf("indexed query page = %#v", page)
			}
		})
	}
	filename := path("filename")
	filesize := path("filesize")
	filenameFilter := query.Equal(filename, query.String("media-a.jpg")).Node()
	mediaRequest := store.Request{
		Collection: media, Filter: &filenameFilter, Page: 1, Limit: 10,
		Sort: []query.Sort{{Path: filesize, Direction: query.Ascending}},
	}
	mediaPredicate := sqliteRequestPredicate(mediaRequest)
	defer mediaPredicate.close()
	mediaOrder, supported := sqliteSortClause(mediaRequest)
	if !supported || !mediaPredicate.exact {
		t.Fatalf("upload metadata query left native SQL: predicate=%#v order=%q", mediaPredicate, mediaOrder)
	}
	mediaArguments := append(append([]any(nil), mediaPredicate.arguments...), 10)
	mediaStatement := `SELECT id FROM ridu_documents WHERE ` + mediaPredicate.clause + ` ORDER BY ` + mediaOrder + ` LIMIT ?`
	mediaPlan := sqliteExplainPlan(t, ctx, backend.db, mediaStatement, mediaArguments...)
	mediaIndex := documentCompoundIndexName(media.ID, []string{"filename", "filesize"}, nil)
	if !strings.Contains(mediaPlan, "USING INDEX "+mediaIndex) || strings.Contains(mediaPlan, "USE TEMP B-TREE") {
		t.Fatalf("upload metadata query did not use %s:\n%s\nquery: %s", mediaIndex, mediaPlan, mediaStatement)
	}
	plannedIndexes[mediaIndex] = struct{}{}

	tenant := path("tenant")
	slug := path("seo.slug")
	tenantFilter := query.Equal(tenant, query.String("acme")).Node()
	request := store.Request{
		Collection: posts, Filter: &tenantFilter, Page: 1, Limit: 10,
		Sort: []query.Sort{{Path: slug, Direction: query.Ascending}},
	}
	predicate := sqliteRequestPredicate(request)
	defer predicate.close()
	order, supported := sqliteSortClause(request)
	if !supported || strings.Contains(order, sqliteDocumentMatcherFunction) {
		t.Fatalf("nested sort left native SQL: %q", order)
	}
	arguments := append(append([]any(nil), predicate.arguments...), 10)
	statement := `SELECT id FROM ridu_documents WHERE ` + predicate.clause + ` ORDER BY ` + order + ` LIMIT ?`
	plan := sqliteExplainPlan(t, ctx, backend.db, statement, arguments...)
	wantIndex := documentCompoundIndexName(posts.ID, []string{"tenant", "seo.slug"}, nil)
	if !strings.Contains(plan, "USING INDEX "+wantIndex) || strings.Contains(plan, "USE TEMP B-TREE") {
		t.Fatalf("compound indexed query plan does not preserve SQL pagination:\n%s\nquery: %s", plan, statement)
	}
	plannedIndexes[wantIndex] = struct{}{}

	for _, localizedCompound := range []struct {
		name    string
		locales []schema.LocaleCode
	}{
		{name: "English", locales: []schema.LocaleCode{"en"}},
		{name: "French", locales: []schema.LocaleCode{"fr"}},
		{name: "French fallback", locales: []schema.LocaleCode{"fr", "en"}},
	} {
		t.Run("localized compound "+localizedCompound.name, func(t *testing.T) {
			localizedPath := path("localizedTitle")
			request := store.Request{
				Collection: posts, Filter: &tenantFilter, Page: 1, Limit: 10,
				Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: localizedCompound.locales,
				Sort: []query.Sort{{Path: localizedPath, Direction: query.Ascending}},
			}
			predicate := sqliteRequestPredicate(request)
			defer predicate.close()
			order, supported := sqliteSortClause(request)
			if !supported || !predicate.exact {
				t.Fatalf("localized compound query left native SQL: predicate=%#v order=%q", predicate, order)
			}
			arguments := append(append([]any(nil), predicate.arguments...), 10)
			statement := `SELECT id FROM ridu_documents WHERE ` + predicate.clause + ` ORDER BY ` + order + ` LIMIT ?`
			plan := sqliteExplainPlan(t, ctx, backend.db, statement, arguments...)
			index := documentCompoundIndexName(posts.ID, []string{"tenant", "localizedTitle"}, localizedCompound.locales)
			if !strings.Contains(plan, "USING INDEX "+index) || strings.Contains(plan, "USE TEMP B-TREE") {
				t.Fatalf("localized compound query did not use %s:\n%s\nquery: %s", index, plan, statement)
			}
			plannedIndexes[index] = struct{}{}
		})
	}

	localizedTitle := path("localizedTitle")
	localizedRequest := store.Request{
		Collection: posts, Page: 1, Limit: 10,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"fr", "en"},
		Sort: []query.Sort{{Path: localizedTitle, Direction: query.Ascending}},
	}
	localizedPredicate := sqliteRequestPredicate(localizedRequest)
	defer localizedPredicate.close()
	localizedOrder, supported := sqliteSortClause(localizedRequest)
	if !supported || localizedPredicate.release != nil {
		t.Fatalf("localized filter/sort left native SQL: predicate=%#v order=%q", localizedPredicate, localizedOrder)
	}
	localizedArguments := append(append([]any(nil), localizedPredicate.arguments...), 10)
	localizedStatement := `SELECT id FROM ridu_documents WHERE ` + localizedPredicate.clause + ` ORDER BY ` + localizedOrder + ` LIMIT ?`
	localizedPlan := sqliteExplainPlan(t, ctx, backend.db, localizedStatement, localizedArguments...)
	localizedIndex := documentFieldIndexName(posts.ID, "localizedTitle", []schema.LocaleCode{"fr", "en"})
	if !strings.Contains(localizedPlan, "USING INDEX "+localizedIndex) || strings.Contains(localizedPlan, "USE TEMP B-TREE") {
		t.Fatalf("localized indexed query plan does not preserve SQL pagination:\n%s\nquery: %s", localizedPlan, localizedStatement)
	}
	plannedIndexes[localizedIndex] = struct{}{}

	accessExpression, err := query.Compare(localizedTitle, query.OperatorExists, query.Boolean(true))
	if err != nil {
		t.Fatal(err)
	}
	access := accessExpression.Node()
	allLocales := store.Request{
		Collection: posts, Access: &access, Page: 1, Limit: 10, AllLocales: true,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"en"},
	}
	accessPredicate := sqliteRequestPredicate(allLocales)
	if !accessPredicate.exact || accessPredicate.release != nil || strings.Contains(accessPredicate.clause, sqliteDocumentMatcherFunction) {
		accessPredicate.close()
		t.Fatalf("all-locales indexed access left the atomic native predicate: %#v", accessPredicate)
	}
	accessPredicate.close()
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := read.List(ctx, allLocales)
	if rollbackError := read.Rollback(ctx); err == nil && rollbackError != nil {
		err = rollbackError
	}
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Documents) != 2 {
		t.Fatalf("all-locales native access page = %#v", page)
	}
	desiredIndexes, err := sqliteDocumentIndexes(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name := range desiredIndexes {
		if _, covered := plannedIndexes[name]; !covered {
			t.Errorf("SQLite index %s has no query-plan assertion", name)
		}
	}
	if len(plannedIndexes) != len(desiredIndexes) {
		t.Fatalf("query-plan assertions cover %d indexes, manifest creates %d", len(plannedIndexes), len(desiredIndexes))
	}
}

func TestSQLiteResidualSortMaterializationIsBounded(t *testing.T) {
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite residual sort bound", Collections: []ridu.Collection{{
		Slug: "posts", Fields: []field.Definition{field.Array("rows", field.Fields(field.Text("label")))},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(ctx, `WITH RECURSIVE sequence(value) AS (
  SELECT 1 UNION ALL SELECT value + 1 FROM sequence WHERE value < ?
)
INSERT INTO ridu_documents (collection_id, id, created_at, updated_at, values_json)
SELECT ?, printf('post-%05d', value), value, value, '{"rows":[{"label":"value"}]}' FROM sequence`, maxSQLiteResidualSortDocuments+1, string(collection.ID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	repeated, _ := query.ParsePath("rows.label")
	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, listError := read.List(ctx, store.Request{
		Collection: collection, Page: 1, Limit: 10,
		Sort: []query.Sort{{Path: repeated, Direction: query.Ascending}},
	})
	if rollbackError := read.Rollback(ctx); listError == nil && rollbackError != nil {
		listError = rollbackError
	}
	if listError == nil || !strings.Contains(listError.Error(), "materialization limit") {
		t.Fatalf("residual sort overflow error = %v", listError)
	}
}

func sqliteIndexParityManifest(t *testing.T) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "SQLite index parity",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"},
			{Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}},
		}},
		Collections: []ridu.Collection{
			{Slug: "users", Fields: []field.Definition{field.Text("name")}},
			{
				Slug: "media", Upload: true, Fields: []field.Definition{field.Text("alt")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{"filename", "filesize"}}},
			},
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Index()),
					field.Textarea("summary", field.Index()),
					field.Code("source", field.Index()),
					field.Email("contact", field.Index()),
					field.Date("publishedAt", field.Index()),
					field.Select("status", field.OneOf("draft", "published"), field.Index()),
					field.Radio("priority", field.OneOf("low", "high"), field.Index()),
					field.Number("score", field.Index()),
					field.Text("externalKey", field.Unique()),
					field.Text("tenant"),
					field.Group("seo", field.Fields(field.Text("slug", field.Index()))),
					field.Text("localizedTitle", field.Localized(), field.Index()),
					field.Relationship("author", field.To("users"), field.Index()),
					field.Upload("hero", field.To("media"), field.Index()),
					field.Checkbox("featured", field.Index()),
				},
				Indexes: []ridu.CollectionIndex{
					{Fields: []string{"tenant", "seo.slug"}},
					{Fields: []string{"tenant", "localizedTitle"}, Unique: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func sqliteCollectionBySlug(t *testing.T, manifest schema.Manifest, slug schema.CollectionSlug) schema.Collection {
	t.Helper()
	for _, collection := range manifest.Snapshot().Collections {
		if collection.Slug == slug {
			return collection
		}
	}
	t.Fatalf("collection %q is missing", slug)
	return schema.Collection{}
}

func sqliteExplainPlan(t *testing.T, ctx context.Context, database *sql.DB, statement string, arguments ...any) string {
	t.Helper()
	rows, err := database.QueryContext(ctx, "EXPLAIN QUERY PLAN "+statement, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, "\n")
}
