package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func TestListWindowUsesManifestJSONIndexAndBoundsMaterialization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{Name: "SQLite bounded window", Collections: []ridu.Collection{{
		Slug:   "actions",
		Fields: field.Fields{field.Text("reconcileQueueKey").Required().Unique().Index()},
	}}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	collection := manifest.Snapshot().Collections[0]
	path := collection.Fields[0].Path
	backend, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}

	if err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		statement, err := connection.PrepareContext(ctx, `INSERT INTO ridu_documents (
  collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json
) VALUES (?, ?, ?, ?, NULL, '', 0, ?)`)
		if err != nil {
			return err
		}
		defer statement.Close()
		now := encodeTime(time.Now().UTC())
		insert := func(id, key string) error {
			encoded, err := json.Marshal(store.Values{"reconcileQueueKey": store.String(key)})
			if err != nil {
				return err
			}
			_, err = statement.ExecContext(ctx, string(collection.ID), id, now, now, string(encoded))
			return err
		}
		for index := 0; index < 2_000; index++ {
			if err := insert(fmt.Sprintf("lower-%04d", index), fmt.Sprintf("0:%04d", index)); err != nil {
				return err
			}
		}
		for _, item := range []struct{ id, key string }{
			{id: "inside-a", key: "1:a"},
			{id: "inside-b", key: "1:b"},
			{id: "inside-c", key: "1:c"},
		} {
			if err := insert(item.id, item.key); err != nil {
				return err
			}
		}
		for index := 0; index < 2_000; index++ {
			if err := insert(fmt.Sprintf("upper-%04d", index), fmt.Sprintf("2:%04d", index)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	request := store.Request{
		Collection: collection,
		Limit:      2,
		IndexWindow: &store.IndexWindow{
			Path: path, LowerBound: "1:", UpperBound: "2:",
		},
	}
	query, arguments, err := listWindowQuery(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToUpper(query), "COUNT(") || strings.Contains(strings.ToUpper(query), " OFFSET ") {
		t.Fatalf("window query is not count-free and offset-free: %s", query)
	}
	rows, err := backend.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	joinedPlan := strings.Join(plan, "\n")
	wantIndex := documentFieldIndexName(collection.ID, "reconcileQueueKey", nil)
	if !strings.Contains(joinedPlan, "USING INDEX "+wantIndex) {
		t.Fatalf("window query plan does not use manifest index %q:\n%s\nquery: %s", wantIndex, joinedPlan, query)
	}
	if strings.Contains(joinedPlan, "USE TEMP B-TREE") {
		t.Fatalf("window query plan sorts outside the manifest index:\n%s", joinedPlan)
	}

	transaction, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	window, err := transaction.(*documentTransaction).ListWindow(ctx, request)
	if rollbackErr := transaction.Rollback(ctx); err == nil && rollbackErr != nil {
		err = rollbackErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !window.HasMore || len(window.Documents) != 2 || window.Documents[0].ID != "inside-a" || window.Documents[1].ID != "inside-b" {
		t.Fatalf("bounded window = %#v", window)
	}
}

func TestAuthIdentityUsesCanonicalExactUniqueIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{
		Name: "SQLite auth identity index", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true,
			Fields: field.Fields{field.Email("email").Required().Unique()},
		}},
	}
	manifest, err := ridu.Resolve(config)
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

	create := func(id, identity string) error {
		transaction, err := backend.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = transaction.Create(ctx, store.CreateRequest{
			Collection: collection, ID: id,
			Values: store.Values{"email": store.String(identity)},
		})
		if err != nil {
			_ = transaction.Rollback(ctx)
			return err
		}
		return transaction.Commit(ctx)
	}
	if err := create("mixed", "Mixed@Example.Test"); err != nil {
		t.Fatal(err)
	}
	if err := create("duplicate", "mixed@example.test"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("mixed-case duplicate = %v, want conflict", err)
	}

	identityField := collection.Fields[0]
	identityKey, _ := json.Marshal(store.CanonicalAuthIdentity("MIXED@example.test"))
	rows, err := backend.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+indexedAuthIdentityQuery,
		string(collection.ID), "field:"+string(identityField.ID), string(identityKey),
	)
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	rows.Close()
	joinedPlan := strings.Join(plan, "\n")
	if !strings.Contains(joinedPlan, "sqlite_autoindex_ridu_unique_values") {
		t.Fatalf("auth identity lookup does not use the unique-value primary index:\n%s", joinedPlan)
	}
	document, err := loadDocumentByIdentity(ctx, backend.db, collection, "MIXED@example.test")
	if err != nil || document.ID != "mixed" {
		t.Fatalf("canonical indexed identity = %#v, %v", document, err)
	}
}

func TestSQLiteCompoundUniqueIndexUsesExactLocalizedTuples(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{
		Name: "SQLite localized compound uniqueness",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug:    "posts",
			Fields:  field.Fields{field.Text("tenant").Index(), field.Text("localizedCode").Localized().Index()},
			Indexes: []ridu.CollectionIndex{{Fields: []string{"tenant", "localizedCode"}, Unique: true}},
		}},
	}
	manifest, err := ridu.Resolve(config)
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

	create := func(id, tenant string, localized store.Values) error {
		transaction, err := backend.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = transaction.Create(ctx, store.CreateRequest{
			Collection: collection, ID: id,
			Values: store.Values{
				"tenant": store.String(tenant), "localizedCode": store.Object(localized),
			},
		})
		if err != nil {
			_ = transaction.Rollback(ctx)
			return err
		}
		return transaction.Commit(ctx)
	}
	if err := create("first", "acme", store.Values{
		"en": store.String("SAME"), "fr": store.String("OTHER"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := create("duplicate-en", "acme", store.Values{
		"en": store.String("SAME"), "fr": store.String("DIFFERENT"),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("same exact-locale tuple error = %v, want conflict", err)
	}
	if err := create("french", "locale", store.Values{"fr": store.String("LOCAL")}); err != nil {
		t.Fatal(err)
	}
	if err := create("english", "locale", store.Values{"en": store.String("LOCAL")}); err != nil {
		t.Fatalf("different exact locales conflicted: %v", err)
	}
}

func TestSQLiteUniqueNumbersTreatSignedZeroAsEqual(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{Name: "SQLite signed-zero uniqueness", Collections: []ridu.Collection{{
		Slug:    "numbers",
		Fields:  field.Fields{field.Number("single").Unique(), field.Text("tenant").Index(), field.Number("tuple").Index()},
		Indexes: []ridu.CollectionIndex{{Fields: []string{"tenant", "tuple"}, Unique: true}},
	}}}
	manifest, err := ridu.Resolve(config)
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

	create := func(id string, single float64, tenant string, tuple float64) error {
		transaction, err := backend.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = transaction.Create(ctx, store.CreateRequest{
			Collection: collection, ID: id,
			Values: store.Values{
				"single": store.Number(single), "tenant": store.String(tenant), "tuple": store.Number(tuple),
			},
		})
		if err != nil {
			_ = transaction.Rollback(ctx)
			return err
		}
		return transaction.Commit(ctx)
	}
	negativeZero := math.Copysign(0, -1)
	if err := create("field-negative", negativeZero, "field-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := create("field-positive", 0, "field-b", 2); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("signed-zero field uniqueness error = %v, want conflict", err)
	}
	if err := create("tuple-negative", 1, "tuple", negativeZero); err != nil {
		t.Fatal(err)
	}
	if err := create("tuple-positive", 2, "tuple", 0); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("signed-zero compound uniqueness error = %v, want conflict", err)
	}
}

func TestSQLiteNativeNumberPredicatesCompareInFloat64Space(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{Name: "SQLite large numeric predicates", Collections: []ridu.Collection{{
		Slug: "measurements", Fields: field.Fields{field.Number("score").Index()},
	}}}
	manifest, err := ridu.Resolve(config)
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
	var indexSQL string
	if err := backend.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type = 'index' AND name = ?`, documentFieldIndexName(collection.ID, "score", nil)).Scan(&indexSQL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexSQL, "CAST(") {
		t.Fatalf("numeric expression index does not use float64 comparison space: %s", indexSQL)
	}
	const large = float64(9223372036854774784)
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{
		Collection: collection, ID: "large", Values: store.Values{"score": store.Number(large)},
	}); err != nil {
		_ = write.Rollback(ctx)
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	score, _ := query.NewPath("score")
	for _, test := range []struct {
		name       string
		expression query.Expression
	}{
		{name: "equal", expression: query.Equal(score, query.Number(large))},
		{name: "less than or equal", expression: query.LessThanEqual(score, query.Number(large))},
		{name: "greater than or equal", expression: query.GreaterThanEqual(score, query.Number(large))},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := test.expression.Node()
			compiled, supported := compileSQLiteNode(collection, node)
			if !supported || !compiled.exact || !strings.Contains(compiled.clause, "CAST(") {
				t.Fatalf("compiled large-number predicate = %#v, supported=%t", compiled, supported)
			}
			read, err := backend.BeginSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			page, err := read.List(ctx, store.Request{
				Collection: collection, Filter: &node, Page: 1, Limit: 10,
			})
			if rollbackError := read.Rollback(ctx); err == nil && rollbackError != nil {
				err = rollbackError
			}
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != "large" {
				t.Fatalf("large-number page = %#v", page)
			}
		})
	}
}

func TestComplexAccessRemainsAnAtomicSQLitePredicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{
		Name: "SQLite complex access",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Localized(), field.Array("rows", field.Fields{field.Text("label")}), field.Text("note")},
		}},
	}
	manifest, err := ridu.Resolve(config)
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

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	for _, document := range []struct {
		id    string
		label string
	}{
		{id: "allowed", label: "team"},
		{id: "denied", label: "other"},
	} {
		if _, err := transaction.Create(ctx, store.CreateRequest{
			Collection: collection, ID: document.id, CreatedAt: timestamp, UpdatedAt: timestamp,
			Values: store.Values{
				"title": store.Object(store.Values{
					"en": store.String("Allowed"), "fr": store.String("Permis"),
				}),
				"rows": store.List(store.Object(store.Values{"label": store.String(document.label)})),
			},
		}); err != nil {
			_ = transaction.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	rowsLabel, _ := query.ParsePath("rows.label")
	title, _ := query.NewPath("title")
	note, _ := query.NewPath("note")
	accessExpression, err := query.And(
		query.Equal(rowsLabel, query.String("team")),
		query.Contains(title, "PER"),
		query.Equal(note, query.Null()),
	)
	if err != nil {
		t.Fatal(err)
	}
	access := accessExpression.Node()
	createdAt, _ := query.NewPath("createdAt")
	filter := query.GreaterThanEqual(createdAt, query.String("2026-01-02T13:00:00+01:00")).Node()
	request := store.Request{
		Collection: collection, Filter: &filter, Access: &access, Page: 1, Limit: 10,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"fr"},
	}
	predicate := sqliteRequestPredicate(request)
	if !predicate.exact || predicate.release == nil || !strings.Contains(predicate.clause, sqliteDocumentMatcherFunction+"(") {
		predicate.close()
		t.Fatalf("complex access escaped the SQL predicate: %#v", predicate)
	}
	predicate.close()

	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := read.List(ctx, request)
	if rollbackErr := read.Rollback(ctx); err == nil && rollbackErr != nil {
		err = rollbackErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != "allowed" {
		t.Fatalf("localized repeated/null access page = %#v", page)
	}

	request.AllLocales = true
	read, err = backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err = read.List(ctx, request)
	if rollbackErr := read.Rollback(ctx); err == nil && rollbackErr != nil {
		err = rollbackErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Documents) != 0 {
		t.Fatalf("all-locales access did not require every locale: %#v", page)
	}
}

func TestSQLiteResidualListMaximumPageDoesNotOverflow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite residual pagination", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Array("rows", field.Fields{field.Text("label")})},
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
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := write.Create(ctx, store.CreateRequest{
			Collection: collection, ID: id,
			Values: store.Values{"rows": store.List(store.Object(store.Values{"label": store.String(id)}))},
		}); err != nil {
			_ = write.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	repeated, err := query.ParsePath("rows.label")
	if err != nil {
		t.Fatal(err)
	}
	request := store.Request{
		Collection: collection, Page: math.MaxInt, Limit: 2,
		Sort: []query.Sort{{Path: repeated, Direction: query.Ascending}},
	}
	if _, native := sqliteSortClause(request); native {
		t.Fatal("repeated sort unexpectedly used the native SQLite path")
	}
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
	if len(page.Documents) != 0 || page.Total != 2 || page.Page != math.MaxInt || page.Limit != 2 {
		t.Fatalf("residual maximum page = %#v", page)
	}
}

func TestNativePredicateNullTruthTableMatchesFunctionalOracle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	config := ridu.Config{Name: "SQLite null truth table", Collections: []ridu.Collection{{
		Slug: "items", Fields: field.Fields{field.Text("value")},
	}}}
	manifest, err := ridu.Resolve(config)
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

	fixtures := []struct {
		id     string
		values store.Values
	}{
		{id: "missing", values: store.Values{}},
		{id: "null", values: store.Values{"value": store.Null()}},
		{id: "wrong-type", values: store.Values{"value": store.Number(42)}},
		{id: "equal", values: store.Values{"value": store.String("equal")}},
		{id: "unequal", values: store.Values{"value": store.String("unequal")}},
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	documents := make([]store.Document, 0, len(fixtures))
	for _, fixture := range fixtures {
		document, err := write.Create(ctx, store.CreateRequest{
			Collection: collection, ID: fixture.id, Values: fixture.values,
		})
		if err != nil {
			_ = write.Rollback(ctx)
			t.Fatal(err)
		}
		documents = append(documents, document)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	valuePath, _ := query.NewPath("value")
	notEqual := query.NotEqual(valuePath, query.String("equal"))
	notEqualExpression, err := query.Not(query.Equal(valuePath, query.String("equal")))
	if err != nil {
		t.Fatal(err)
	}
	notOrderedExpression, err := query.Not(query.GreaterThan(valuePath, query.String("m")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		node query.Node
	}{
		{name: "not-equal", node: notEqual.Node()},
		{name: "top-level-not-equal", node: notEqualExpression.Node()},
		{name: "top-level-not-ordered", node: notOrderedExpression.Node()},
	}

	read, err := backend.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Rollback(ctx) }()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := store.Request{Collection: collection, Filter: &test.node, Page: 1, Limit: 100}
			predicate := sqliteRequestPredicate(request)
			if !predicate.exact || predicate.release != nil {
				predicate.close()
				t.Fatalf("predicate unexpectedly left the native SQL path: %#v", predicate)
			}
			predicate.close()
			page, err := read.List(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			var got, want []string
			for _, document := range page.Documents {
				got = append(got, document.ID)
			}
			for _, document := range documents {
				if documentMatchesRequest(document, request) {
					want = append(want, document.ID)
				}
			}
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("SQL IDs %v != matcher IDs %v", got, want)
			}
		})
	}
}
