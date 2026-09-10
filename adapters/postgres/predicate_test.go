package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPredicateCompilerUsesNativeSystemTimestampColumns(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts"}
	createdAt, _ := query.NewPath("createdAt")
	compiler := predicateCompiler{collection: collection}
	compiled, err := compiler.compile(query.GreaterThanEqual(createdAt, query.String("2026-01-02T13:00:00+01:00")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if compiled != `"created_at" >= $1` {
		t.Fatalf("createdAt predicate = %s", compiled)
	}
	if len(compiler.arguments) != 1 {
		t.Fatalf("createdAt arguments = %#v", compiler.arguments)
	}
	timestamp, ok := compiler.arguments[0].(time.Time)
	if !ok || !timestamp.Equal(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("createdAt argument = %#v", compiler.arguments[0])
	}

	updatedAt, _ := query.NewPath("updatedAt")
	sortTerm, _ := query.NewSort(updatedAt, query.Descending)
	order, err := sortClause(store.Request{Collection: collection, Sort: []query.Sort{sortTerm}})
	if err != nil {
		t.Fatal(err)
	}
	if order != `"updated_at" DESC, "id" ASC` {
		t.Fatalf("updatedAt order = %s", order)
	}

	compiler = predicateCompiler{collection: collection, snapshot: true}
	compiled, err = compiler.compile(query.Equal(updatedAt, query.String("2026-01-02T12:00:00Z")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, `("snapshot" ->> 'UpdatedAt')::timestamptz = $1`) {
		t.Fatalf("snapshot updatedAt predicate = %s", compiled)
	}

	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.NotEqual(createdAt, query.Number(0)).Node())
	if err != nil {
		t.Fatal(err)
	}
	if compiled != "FALSE" || len(compiler.arguments) != 0 {
		t.Fatalf("invalid timestamp inequality = %s, arguments %#v; want fail-closed FALSE", compiled, compiler.arguments)
	}
}

func TestPredicateCompilerSupportsNestedGroupsAndRepeatedRows(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{
		{ID: "seo", Name: "seo", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "seo-title", Name: "title", Type: schema.FieldTypeText},
		}}},
		{ID: "links", Name: "links", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "links-label", Name: "label", Type: schema.FieldTypeText},
		}}},
	}}

	seoPath, _ := query.ParsePath("seo.title")
	compiler := predicateCompiler{collection: collection}
	compiled, err := compiler.compile(query.Like(seoPath, "Ridu").Node())
	if err != nil {
		t.Fatalf("compile nested group: %v", err)
	}
	if !strings.Contains(compiled, "#>> '{title}'") || !strings.Contains(compiled, "ILIKE $1") {
		t.Fatalf("nested group predicate = %s", compiled)
	}

	linksPath, _ := query.ParsePath("links.label")
	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.Equal(linksPath, query.String("Docs")).Node())
	if err != nil {
		t.Fatalf("compile nested array: %v", err)
	}
	if !strings.Contains(compiled, "jsonb_array_elements") || !strings.Contains(compiled, "#>> '{label}' = $1") {
		t.Fatalf("nested array predicate = %s", compiled)
	}

	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.NotEqual(linksPath, query.String("Private")).Node())
	if err != nil {
		t.Fatalf("compile nested array inequality: %v", err)
	}
	if !strings.HasPrefix(compiled, "NOT EXISTS") {
		t.Fatalf("nested array inequality = %s", compiled)
	}
}

func TestPredicateCompilerSupportsNestedOpaqueJSONPaths(t *testing.T) {
	collection := schema.Collection{ID: "media", Slug: "media", Fields: []schema.Field{
		{ID: "upload-sizes", Name: "sizes", Type: schema.FieldTypeJSON},
	}}
	path, err := query.ParsePath("sizes.card.objectKey")
	if err != nil {
		t.Fatal(err)
	}
	compiler := predicateCompiler{collection: collection}
	compiled, err := compiler.compile(query.Equal(path, query.String("ridu/object.png")).Node())
	if err != nil {
		t.Fatalf("compile nested JSON path: %v", err)
	}
	if !strings.Contains(compiled, "#> '{card,objectKey}'") || !strings.Contains(compiled, "jsonb_typeof") {
		t.Fatalf("nested JSON predicate = %s", compiled)
	}
}

func TestPredicateCompilerTreatsMultiSelectContainsAsExactMembership(t *testing.T) {
	roles, err := query.ParsePath("roles")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "users", Slug: "users", Fields: []schema.Field{{
		ID: "users-roles", Name: "roles", Type: schema.FieldTypeSelect,
		Select: &schema.SelectField{HasMany: true, Options: []schema.SelectOption{{Value: "admin"}, {Value: "editor"}}},
	}}}
	compiler := predicateCompiler{collection: collection}
	compiled, err := compiler.compile(query.Contains(roles, "admin").Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "@> jsonb_build_array($1::text)") {
		t.Fatalf("multi-select contains predicate = %s", compiled)
	}
	if columnType(collection.Fields[0]) != "jsonb" || !isJSONStoredField(collection.Fields[0]) {
		t.Fatalf("multi-select storage = %q/json=%v", columnType(collection.Fields[0]), isJSONStoredField(collection.Fields[0]))
	}
}

func TestPredicateCompilerSupportsVersionStatusMetadata(t *testing.T) {
	status, err := query.ParsePath("_status")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "lessons", Slug: "lessons", Versions: &schema.VersionSettings{Drafts: true}}
	compiler := predicateCompiler{collection: collection}
	compiled, err := compiler.compile(query.Equal(status, query.String("published")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, `"_status" = $1`) {
		t.Fatalf("status predicate = %s", compiled)
	}
	compiler = predicateCompiler{collection: collection, snapshot: true}
	compiled, err = compiler.compile(query.Equal(status, query.String("draft")).Node())
	if err != nil || !strings.Contains(compiled, `"snapshot" ->> 'Status' = $1`) {
		t.Fatalf("snapshot status predicate = %s, %v", compiled, err)
	}
}

func TestPredicateCompilerUsesStableByteOrderingForText(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{
		{ID: "title", Name: "title", Type: schema.FieldTypeText},
		{ID: "metadata", Name: "metadata", Type: schema.FieldTypeJSON},
	}}
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "scalar", path: "title"},
		{name: "JSON", path: "metadata"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, err := query.ParsePath(test.path)
			if err != nil {
				t.Fatal(err)
			}
			compiler := predicateCompiler{collection: collection}
			compiled, err := compiler.compile(query.GreaterThan(path, query.String("Z")).Node())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(compiled, `COLLATE "C"`) {
				t.Fatalf("ordered text predicate = %s", compiled)
			}
		})
	}
}

func TestFieldsForReadNeverSelectsPresentationColumns(t *testing.T) {
	collection := schema.Collection{ID: "categories", Slug: "categories", Fields: []schema.Field{
		{ID: "categories-name", Name: "name", Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar},
		{ID: "categories-posts", Name: "posts", Type: schema.FieldTypeJoin, Category: schema.FieldCategoryPresentation, Join: &schema.JoinField{}},
	}}
	fields := fieldsForRead(store.Request{Collection: collection})
	if len(fields) != 1 || fields[0].Name != "name" {
		t.Fatalf("read fields = %#v", fields)
	}
	if fields := fieldsForRead(store.Request{Collection: collection, Select: []query.Path{}}); len(fields) != 0 {
		t.Fatalf("metadata-only read fields = %#v, want none", fields)
	}
	document := store.Document{ID: "category-1", Values: store.Values{"name": store.String("Guides")}}
	if projected := projectDocument(document, nil); len(projected.Values) != 1 {
		t.Fatalf("omitted projection values = %#v, want all", projected.Values)
	}
	if projected := projectDocument(document, []query.Path{}); len(projected.Values) != 0 {
		t.Fatalf("metadata-only projection values = %#v, want none", projected.Values)
	}
}

func TestListWindowQueryIsOneUniqueIndexRangeWithoutCountOrOffset(t *testing.T) {
	queuePath, err := query.ParsePath("reconcileQueueKey")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "release-actions", Slug: "release-actions", Fields: []schema.Field{{
		ID: "release-actions-reconcile-queue", Name: "reconcileQueueKey", Path: queuePath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
		Unique: true, Index: true,
	}}}
	statement, arguments, fields, err := listWindowQuery(store.Request{
		Collection: collection,
		Limit:      50,
		IndexWindow: &store.IndexWindow{
			Path: queuePath, LowerBound: "0:", UpperBound: "1:",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(statement)
	if strings.Contains(upper, "COUNT(") || strings.Contains(upper, " OFFSET ") {
		t.Fatalf("window query performs an unbounded count or offset: %s", statement)
	}
	column := quote(fieldColumn(collection.Fields[0].ID))
	if !strings.Contains(statement, column+" >= $1") || !strings.Contains(statement, column+" < $2") ||
		!strings.Contains(statement, "ORDER BY "+column+" ASC LIMIT $3") || strings.Contains(statement, `ORDER BY "id"`) {
		t.Fatalf("window query is not the exact unique-index range: %s", statement)
	}
	if len(arguments) != 3 || arguments[0] != "0:" || arguments[1] != "1:" || arguments[2] != 51 {
		t.Fatalf("window arguments = %#v, want lower, upper, and one overflow sentinel", arguments)
	}
	if len(fields) != 1 || fields[0].ID != collection.Fields[0].ID {
		t.Fatalf("window fields = %#v", fields)
	}
}

func TestVersionPredicateCompilerReadsSnapshotValues(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{
		{ID: "owner-id", Name: "owner", Type: schema.FieldTypeText},
		{ID: "links", Name: "links", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "links-label", Name: "label", Type: schema.FieldTypeText},
		}}},
	}}
	ownerPath, _ := query.ParsePath("owner")
	compiler := predicateCompiler{collection: collection, next: 2, arguments: []any{"posts", "post-1"}, snapshot: true}
	compiled, err := compiler.compile(query.Equal(ownerPath, query.String("owner-b")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, `"snapshot" #>> '{Values,owner}' = $3`) {
		t.Fatalf("snapshot owner predicate = %s", compiled)
	}

	linksPath, _ := query.ParsePath("links.label")
	compiler = predicateCompiler{collection: collection, snapshot: true}
	compiled, err = compiler.compile(query.Equal(linksPath, query.String("Docs")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, `"snapshot" #> '{Values,links}'`) || !strings.Contains(compiled, `jsonb_typeof`) {
		t.Fatalf("snapshot nested-array predicate = %s", compiled)
	}

	collection.Fields[1].Localized = true
	compiler = predicateCompiler{collection: collection, snapshot: true, localeChain: []schema.LocaleCode{"fr"}}
	compiled, err = compiler.compile(query.Equal(linksPath, query.String("Documentation")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, `"snapshot" #> '{Values,links,fr}'`) || !strings.Contains(compiled, `jsonb_typeof`) {
		t.Fatalf("localized snapshot nested-array predicate = %s", compiled)
	}
}

func TestPredicateCompilerInsertsLocaleAtIntermediateContainerBoundaries(t *testing.T) {
	collection := localizedIntermediatePredicateCollection()
	tests := []struct {
		name    string
		path    string
		want    []string
		wantNot []string
	}{
		{
			name: "group",
			path: "content.details.name",
			want: []string{"#> '{details,fr}'", "#>> '{name}'"},
			wantNot: []string{
				"#>> '{details,name}'",
				"#>> '{details,fr,name}'",
			},
		},
		{
			name: "array",
			path: "content.rows.label",
			want: []string{"#> '{rows,fr}'", "jsonb_array_elements", "jsonb_typeof"},
			wantNot: []string{
				"#> '{rows}'",
			},
		},
		{
			name: "blocks",
			path: "content.layout.hero.heading",
			want: []string{"#> '{layout,fr}'", "->> 'blockType'"},
			wantNot: []string{
				"#> '{layout}'",
			},
		},
		{
			name: "localized group in repeated row",
			path: "content.entries.metadata.code",
			want: []string{"#> '{entries}'", "ridu_item.value #> '{metadata,fr}'", "#>> '{code}'"},
			wantNot: []string{
				"#>> '{metadata,fr,code}'",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, err := query.ParsePath(test.path)
			if err != nil {
				t.Fatal(err)
			}
			compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr"}}
			compiled, err := compiler.compile(query.Equal(path, query.String("visible")).Node())
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.want {
				if !strings.Contains(compiled, fragment) {
					t.Fatalf("predicate %s is missing %q", compiled, fragment)
				}
			}
			for _, fragment := range test.wantNot {
				if strings.Contains(compiled, fragment) {
					t.Fatalf("predicate %s unexpectedly contains %q", compiled, fragment)
				}
			}
		})
	}
}

func TestAccessPredicateCompilerChecksIntermediateLocalizedContainersForEveryLocale(t *testing.T) {
	collection := localizedIntermediatePredicateCollection()
	path, err := query.ParsePath("content.rows.label")
	if err != nil {
		t.Fatal(err)
	}
	compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"en"}}
	compiled, err := compileAccessPredicate(&compiler, query.Equal(path, query.String("visible")).Node(), true, []schema.LocaleCode{"en", "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"#> '{rows,en}'", "#> '{rows,fr}'", ") AND ("} {
		if !strings.Contains(compiled, fragment) {
			t.Fatalf("all-locales predicate %s is missing %q", compiled, fragment)
		}
	}
	if strings.Contains(compiled, "#> '{rows}'") {
		t.Fatalf("all-locales predicate expands the locale object as an array: %s", compiled)
	}
}

func TestVersionPredicateCompilerInsertsLocaleAtIntermediateContainers(t *testing.T) {
	collection := localizedIntermediatePredicateCollection()
	tests := []struct {
		path string
		want []string
	}{
		{path: "content.details.name", want: []string{"#> '{Values,content,details,fr}'", "#>> '{name}'"}},
		{path: "content.rows.label", want: []string{"#> '{Values,content,rows,fr}'", "jsonb_typeof"}},
		{path: "content.layout.hero.heading", want: []string{"#> '{Values,content,layout,fr}'", "->> 'blockType'"}},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			path, err := query.ParsePath(test.path)
			if err != nil {
				t.Fatal(err)
			}
			compiler := predicateCompiler{collection: collection, snapshot: true, localeChain: []schema.LocaleCode{"fr"}}
			compiled, err := compiler.compile(query.Equal(path, query.String("visible")).Node())
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.want {
				if !strings.Contains(compiled, fragment) {
					t.Fatalf("snapshot predicate %s is missing %q", compiled, fragment)
				}
			}
		})
	}
}

func TestPredicateCompilerSelectsLocalizedContainersBeforeDescendants(t *testing.T) {
	collection := localizedIntermediatePredicateCollection()
	tests := []struct {
		path string
		want []string
		not  []string
	}{
		{
			path: "content.details.name",
			want: []string{"#> '{details,fr}'", "#> '{details,en}'", ") #>> '{name}'"},
			not:  []string{"'{details,fr,name}'", "'{details,en,name}'"},
		},
		{
			path: "content.sections.items.label",
			want: []string{"#> '{sections,fr}'", "#> '{sections,en}'", ") #> '{items}'"},
			not:  []string{"'{sections,fr,items}'", "'{sections,en,items}'"},
		},
		{
			path: "content.sections.panels.hero.heading",
			want: []string{"#> '{sections,fr}'", "#> '{sections,en}'", ") #> '{panels}'", "->> 'blockType'"},
			not:  []string{"'{sections,fr,panels}'", "'{sections,en,panels}'"},
		},
		{
			path: "content.entries.metadata.code",
			want: []string{"ridu_item.value #> '{metadata,fr}'", "ridu_item.value #> '{metadata,en}'", ") #>> '{code}'"},
			not:  []string{"'{metadata,fr,code}'", "'{metadata,en,code}'"},
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			path, err := query.ParsePath(test.path)
			if err != nil {
				t.Fatal(err)
			}
			compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr", "en"}}
			compiled, err := compiler.compile(query.Equal(path, query.String("visible")).Node())
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.want {
				if !strings.Contains(compiled, fragment) {
					t.Fatalf("predicate %s is missing %q", compiled, fragment)
				}
			}
			for _, fragment := range test.not {
				if strings.Contains(compiled, fragment) {
					t.Fatalf("predicate %s falls back below the localized container via %q", compiled, fragment)
				}
			}
		})
	}

	rootRows, err := query.ParsePath("localizedRoot.rows.label")
	if err != nil {
		t.Fatal(err)
	}
	compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr", "en"}}
	compiled, err := compiler.compile(query.Equal(rootRows, query.String("visible")).Node())
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := "COALESCE(NULLIF(" + quote(localizedFieldColumn("localized-root", "fr")) + ", 'null'::jsonb), NULLIF(" + quote(localizedFieldColumn("localized-root", "en")) + ", 'null'::jsonb)) #> '{rows}'"
	if !strings.Contains(compiled, wantRoot) {
		t.Fatalf("root-localized container predicate %s is missing %q", compiled, wantRoot)
	}

	compiler = predicateCompiler{collection: collection, snapshot: true, localeChain: []schema.LocaleCode{"fr", "en"}}
	compiled, err = compiler.compile(query.Equal(rootRows, query.String("visible")).Node())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"#> '{Values,localizedRoot,fr}'", "#> '{Values,localizedRoot,en}'", ") #> '{rows}'"} {
		if !strings.Contains(compiled, fragment) {
			t.Fatalf("root-localized snapshot predicate %s is missing %q", compiled, fragment)
		}
	}
}

func TestPredicateCompilerKeepsOptionalValueComparisonsTwoValued(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{
		{ID: "subtitle", Name: "subtitle", Type: schema.FieldTypeText},
		{ID: "score", Name: "score", Type: schema.FieldTypeNumber},
		{ID: "metadata", Name: "metadata", Type: schema.FieldTypeJSON},
		{ID: "plugin-data", Name: "pluginData", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "test"}},
		{
			ID: "rows", Name: "rows", Type: schema.FieldTypeArray,
			Nested: &schema.NestedField{Fields: []schema.Field{
				{ID: "rows-label", Name: "label", Type: schema.FieldTypeText},
				{ID: "rows-payload", Name: "payload", Type: schema.FieldTypeJSON},
			}},
		},
		{
			ID: "layout", Name: "layout", Type: schema.FieldTypeBlocks,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{{ID: "layout-hero-heading", Name: "heading", Type: schema.FieldTypeText}}}}},
		},
	}}
	path, err := query.ParsePath("subtitle")
	if err != nil {
		t.Fatal(err)
	}
	compiler := predicateCompiler{collection: collection}
	compiled, err := compiler.compile(query.Like(path, "").Node())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compiled, "TRUE") || !strings.Contains(compiled, "IS NOT NULL") {
		t.Fatalf("empty like predicate = %s", compiled)
	}

	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.NotEqual(path, query.String("hidden")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "IS NULL OR") || !strings.Contains(compiled, "<> $1") {
		t.Fatalf("not-equal predicate = %s", compiled)
	}

	not, err := query.Not(query.Equal(path, query.String("hidden")))
	if err != nil {
		t.Fatal(err)
	}
	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(not.Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "NOT (COALESCE(") || !strings.Contains(compiled, "FALSE") {
		t.Fatalf("not predicate = %s", compiled)
	}

	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.In(path, query.Null()).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "IS NULL") || strings.Contains(compiled, "IN (") || len(compiler.arguments) != 0 {
		t.Fatalf("scalar null-only IN predicate = %s arguments %#v", compiled, compiler.arguments)
	}
	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.In(path, query.Null(), query.String("visible")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "IS NULL OR") || !strings.Contains(compiled, "IN ($1)") || len(compiler.arguments) != 1 {
		t.Fatalf("scalar mixed IN predicate = %s arguments %#v", compiled, compiler.arguments)
	}

	score, err := query.ParsePath("score")
	if err != nil {
		t.Fatal(err)
	}
	for _, expression := range []query.Expression{query.Like(score, ""), query.Like(score, "9")} {
		compiler = predicateCompiler{collection: collection}
		compiled, err = compiler.compile(expression.Node())
		if err != nil {
			t.Fatal(err)
		}
		if compiled != "FALSE" {
			t.Fatalf("non-string like predicate = %s", compiled)
		}
	}
	for _, test := range []struct {
		expression query.Expression
		want       string
	}{
		{expression: query.Equal(score, query.String("9")), want: "FALSE"},
		{expression: query.NotEqual(score, query.String("9")), want: "TRUE"},
		{expression: query.GreaterThan(score, query.String("9")), want: "FALSE"},
	} {
		compiler = predicateCompiler{collection: collection}
		compiled, err = compiler.compile(test.expression.Node())
		if err != nil {
			t.Fatal(err)
		}
		if compiled != test.want {
			t.Fatalf("type-mismatched scalar predicate = %s, want %s", compiled, test.want)
		}
	}

	for _, name := range []string{"metadata", "pluginData", "rows.payload"} {
		t.Run("json string "+name, func(t *testing.T) {
			jsonPath, err := query.ParsePath(name)
			if err != nil {
				t.Fatal(err)
			}
			like := query.Like(jsonPath, "visible")
			compiler := predicateCompiler{collection: collection}
			compiled, err := compiler.compile(like.Node())
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range []string{"jsonb_typeof", "= 'string'", "#>> '{}'", "ILIKE"} {
				if !strings.Contains(compiled, fragment) {
					t.Fatalf("JSON/plugin string predicate %s is missing %q", compiled, fragment)
				}
			}
			notLike, err := query.Not(like)
			if err != nil {
				t.Fatal(err)
			}
			compiler = predicateCompiler{collection: collection}
			compiled, err = compiler.compile(notLike.Node())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(compiled, "NOT (COALESCE(") || !strings.Contains(compiled, "jsonb_typeof") {
				t.Fatalf("outer NOT JSON/plugin predicate = %s", compiled)
			}
		})
	}
	metadata, err := query.ParsePath("metadata")
	if err != nil {
		t.Fatal(err)
	}
	compiler = predicateCompiler{collection: collection, snapshot: true}
	compiled, err = compiler.compile(query.Like(metadata, "visible").Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, `"snapshot" #> '{Values,metadata}'`) || !strings.Contains(compiled, "jsonb_typeof") {
		t.Fatalf("snapshot JSON string predicate = %s", compiled)
	}
	jsonComparisons := []struct {
		name       string
		expression query.Expression
		want       []string
	}{
		{
			name: "string equality", expression: query.Equal(metadata, query.String("9")),
			want: []string{"jsonb_typeof", "= 'string'", "CASE WHEN", "= $1"},
		},
		{
			name: "number equality", expression: query.Equal(metadata, query.Number(9)),
			want: []string{"jsonb_typeof", "= 'number'", "::double precision", "= $1"},
		},
		{
			name: "boolean equality", expression: query.Equal(metadata, query.Boolean(true)),
			want: []string{"jsonb_typeof", "= 'boolean'", "::boolean", "= $1"},
		},
		{
			name: "null equality", expression: query.Equal(metadata, query.Null()),
			want: []string{"IS NULL OR", "= 'null'::jsonb"},
		},
		{
			name: "mixed membership", expression: query.In(metadata, query.Null(), query.String("visible"), query.Number(9)),
			want: []string{"IS NULL OR", "= 'null'::jsonb", "= 'string'", "= 'number'"},
		},
		{
			name: "ordered number", expression: query.GreaterThan(metadata, query.Number(5)),
			want: []string{"= 'number'", "::double precision", "> $1", "COALESCE"},
		},
	}
	for _, test := range jsonComparisons {
		t.Run("JSON "+test.name, func(t *testing.T) {
			compiler := predicateCompiler{collection: collection}
			compiled, err := compiler.compile(test.expression.Node())
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.want {
				if !strings.Contains(compiled, fragment) {
					t.Fatalf("JSON %s predicate %s is missing %q", test.name, compiled, fragment)
				}
			}
		})
	}
	pluginData, err := query.ParsePath("pluginData")
	if err != nil {
		t.Fatal(err)
	}
	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.Equal(pluginData, query.Number(9)).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "jsonb_typeof") || !strings.Contains(compiled, "= 'number'") {
		t.Fatalf("plugin number equality predicate = %s", compiled)
	}
	rowPayload, err := query.ParsePath("rows.payload")
	if err != nil {
		t.Fatal(err)
	}
	compiler = predicateCompiler{collection: collection, snapshot: true}
	compiled, err = compiler.compile(query.In(rowPayload, query.Null(), query.Number(9)).Node())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"#> '{Values,rows}'", "ridu_item.value #> '{payload}' IS NOT NULL", "= 'null'::jsonb", "= 'number'"} {
		if !strings.Contains(compiled, fragment) {
			t.Fatalf("repeated snapshot JSON membership %s is missing %q", compiled, fragment)
		}
	}

	rows, err := query.ParsePath("rows.label")
	if err != nil {
		t.Fatal(err)
	}
	nullOnly := query.In(rows, query.Null())
	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(nullOnly.Node())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"NOT EXISTS", "ridu_item.value #> '{label}' IS NOT NULL", "ridu_item.value #>> '{label}' IS NULL"} {
		if !strings.Contains(compiled, fragment) {
			t.Fatalf("repeated null-only IN predicate %s is missing %q", compiled, fragment)
		}
	}

	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.In(rows, query.Null(), query.String("visible")).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "IS NULL OR") || !strings.Contains(compiled, "IN ($1)") {
		t.Fatalf("repeated mixed IN predicate = %s", compiled)
	}

	notNullOnly, err := query.Not(nullOnly)
	if err != nil {
		t.Fatal(err)
	}
	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(notNullOnly.Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "NOT (COALESCE(") || !strings.Contains(compiled, "NOT EXISTS") {
		t.Fatalf("outer NOT repeated IN predicate = %s", compiled)
	}

	compiler = predicateCompiler{collection: collection}
	compiled, err = compiler.compile(query.NotEqual(rows, query.Null()).Node())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled, "NOT (NOT EXISTS") {
		t.Fatalf("repeated not-equal null predicate = %s", compiled)
	}

	heading, err := query.ParsePath("layout.hero.heading")
	if err != nil {
		t.Fatal(err)
	}
	compiler = predicateCompiler{collection: collection, snapshot: true}
	compiled, err = compiler.compile(query.In(heading, query.Null()).Node())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"#> '{Values,layout}'", "->> 'blockType'", "#> '{heading}' IS NOT NULL"} {
		if !strings.Contains(compiled, fragment) {
			t.Fatalf("snapshot block null IN predicate %s is missing %q", compiled, fragment)
		}
	}
}

func TestLocalizedScalarCompilerPreservesTheFinalEmptyCandidate(t *testing.T) {
	collection := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{
		{ID: "title", Name: "title", Type: schema.FieldTypeText, Localized: true},
		{
			ID: "editor", Name: "editor", Type: schema.FieldTypeRelationship, Localized: true,
			Relationship: &schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"},
		},
		{
			ID: "asset", Name: "asset", Type: schema.FieldTypeUpload, Localized: true,
			Upload: &schema.UploadField{CollectionID: "media", CollectionSlug: "media"},
		},
		{
			ID: "content", Name: "content", Type: schema.FieldTypeGroup,
			Nested: &schema.NestedField{Fields: []schema.Field{{ID: "content-note", Name: "note", Type: schema.FieldTypeText, Localized: true}}},
		},
	}}
	for _, name := range []string{"title", "editor", "asset", "content.note"} {
		t.Run(name, func(t *testing.T) {
			path, err := query.ParsePath(name)
			if err != nil {
				t.Fatal(err)
			}
			comparison := query.Equal(path, query.Null()).Node()

			compiler := predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr"}}
			exact, err := compiler.compile(comparison)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(exact, "NULLIF(") {
				t.Fatalf("exact-locale predicate collapses empty into null: %s", exact)
			}

			compiler = predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr", "en"}}
			fallback, err := compiler.compile(comparison)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(fallback, "NULLIF(") || strings.Count(fallback, "NULLIF(") != 1 {
				t.Fatalf("fallback predicate must skip empty only before the final locale: %s", fallback)
			}

			compiler = predicateCompiler{collection: collection, snapshot: true, localeChain: []schema.LocaleCode{"fr"}}
			snapshot, err := compiler.compile(comparison)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(snapshot, "NULLIF(") {
				t.Fatalf("exact snapshot predicate collapses empty into null: %s", snapshot)
			}

			compiler = predicateCompiler{collection: collection, localeChain: []schema.LocaleCode{"fr"}}
			allLocales, err := compileAccessPredicate(&compiler, comparison, true, []schema.LocaleCode{"en", "fr"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(allLocales, "NULLIF(") {
				t.Fatalf("all-locales predicate collapses an exact empty value into null: %s", allLocales)
			}
		})
	}
}

func localizedIntermediatePredicateCollection() schema.Collection {
	return schema.Collection{ID: "pages", Slug: "pages", Fields: []schema.Field{{
		ID: "content", Name: "content", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
			{
				ID: "content-details", Name: "details", Type: schema.FieldTypeGroup, Localized: true,
				Nested: &schema.NestedField{Fields: []schema.Field{{ID: "content-details-name", Name: "name", Type: schema.FieldTypeText}}},
			},
			{
				ID: "content-rows", Name: "rows", Type: schema.FieldTypeArray, Localized: true,
				Nested: &schema.NestedField{Fields: []schema.Field{{ID: "content-rows-label", Name: "label", Type: schema.FieldTypeText}}},
			},
			{
				ID: "content-layout", Name: "layout", Type: schema.FieldTypeBlocks, Localized: true,
				Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{{ID: "content-layout-hero-heading", Name: "heading", Type: schema.FieldTypeText}}}}},
			},
			{
				ID: "content-entries", Name: "entries", Type: schema.FieldTypeArray,
				Nested: &schema.NestedField{Fields: []schema.Field{{
					ID: "content-entries-metadata", Name: "metadata", Type: schema.FieldTypeGroup, Localized: true,
					Nested: &schema.NestedField{Fields: []schema.Field{{ID: "content-entries-metadata-code", Name: "code", Type: schema.FieldTypeText}}},
				}}},
			},
			{
				ID: "content-sections", Name: "sections", Type: schema.FieldTypeGroup, Localized: true,
				Nested: &schema.NestedField{Fields: []schema.Field{
					{
						ID: "content-sections-items", Name: "items", Type: schema.FieldTypeArray,
						Nested: &schema.NestedField{Fields: []schema.Field{{ID: "content-sections-items-label", Name: "label", Type: schema.FieldTypeText}}},
					},
					{
						ID: "content-sections-panels", Name: "panels", Type: schema.FieldTypeBlocks,
						Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{{ID: "content-sections-panels-hero-heading", Name: "heading", Type: schema.FieldTypeText}}}}},
					},
				}},
			},
		}},
	}, {
		ID: "localized-root", Name: "localizedRoot", Type: schema.FieldTypeGroup, Localized: true,
		Nested: &schema.NestedField{Fields: []schema.Field{{
			ID: "localized-root-rows", Name: "rows", Type: schema.FieldTypeArray,
			Nested: &schema.NestedField{Fields: []schema.Field{{ID: "localized-root-rows-label", Name: "label", Type: schema.FieldTypeText}}},
		}}},
	}}}
}
