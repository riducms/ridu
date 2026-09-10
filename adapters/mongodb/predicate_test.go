package mongodb

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoRequestPredicateCombinesRequestAndAuthorizationRestrictions(t *testing.T) {
	title := mongoMustPath(t, "title")
	score := mongoMustPath(t, "score")
	filter := query.Equal(title, query.String("public")).Node()
	access := query.GreaterThan(score, query.Number(5)).Node()

	compiled, err := requestPredicate(store.Request{
		Collection:       mongoPredicateCollection(),
		ID:               "post-1",
		Filter:           &filter,
		Access:           &access,
		PublishedOnly:    true,
		Deletion:         store.DeletionTrash,
		ExpectedRevision: 7,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	children := mongoLogicalChildren(t, compiled, "$and")
	if len(children) != 6 {
		t.Fatalf("combined predicate children = %#v, want ID, deletion, publication, revision, filter, and access", children)
	}
	for _, path := range []string{
		mongoIDPath, mongoDeletedAtPath, mongoStatusPath, mongoRevisionPath,
		"values.title", "values.score",
	} {
		if !mongoContainsKey(compiled, path) {
			t.Errorf("combined predicate does not contain %q: %#v", path, compiled)
		}
	}
	if got := mongoValueForKey(compiled, mongoIDPath); got != "post-1" {
		t.Fatalf("ID predicate = %#v, want canonical string", got)
	}
	if !mongoContainsNestedOperator(compiled, mongoRevisionPath, "$type", "long") {
		t.Fatalf("expected revision lacks BSON long guard: %#v", compiled)
	}
	if _, err := bson.Marshal(compiled); err != nil {
		t.Fatalf("compiled predicate is not valid BSON: %v", err)
	}
}

func TestMongoRegexPredicatesQuoteUserInput(t *testing.T) {
	title := mongoMustPath(t, "title")
	injected := `{$where:"return true",$gt:""}`
	equality, err := compileMongoNode(mongoPredicateCollection(), query.Equal(title, query.String(injected)).Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	equalityChildren := mongoLogicalChildren(t, equality, "$and")
	if got := mongoNestedOperatorValue(equalityChildren[1], "values.title", "$eq"); got != injected {
		t.Fatalf("injection-shaped equality operand = %#v, want inert string %q", got, injected)
	}

	tests := []struct {
		name     string
		node     query.Node
		patterns []string
	}{
		{
			name:     "contains",
			node:     query.Contains(title, `.*$[draft](?i)`).Node(),
			patterns: []string{regexp.QuoteMeta(`.*$[draft](?i)`)},
		},
		{
			name:     "like",
			node:     query.Like(title, `hello .*$`).Node(),
			patterns: []string{regexp.QuoteMeta("hello"), regexp.QuoteMeta(`.*$`)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := compileMongoNode(mongoPredicateCollection(), test.node, "filter", mongoPredicateScope{})
			if err != nil {
				t.Fatal(err)
			}
			regexes := mongoRegexes(compiled)
			if len(regexes) != len(test.patterns) {
				t.Fatalf("regexes = %#v, want %v", regexes, test.patterns)
			}
			for index, pattern := range test.patterns {
				if regexes[index].Pattern != pattern || regexes[index].Options != "i" {
					t.Errorf("regex %d = %#v, want quoted %q with i option", index, regexes[index], pattern)
				}
			}
		})
	}
}

func TestMongoDeletionPredicateRequiresCanonicalMetadataType(t *testing.T) {
	collection := mongoPredicateCollection()
	tests := []struct {
		name string
		mode store.DeletionMode
		want bson.D
	}{
		{
			name: "active is explicit BSON null",
			want: bson.D{{
				Key: mongoDeletedAtPath,
				Value: bson.D{
					{Key: "$type", Value: "null"},
					{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
				},
			}},
		},
		{
			name: "trash is signed nanoseconds",
			mode: store.DeletionTrash,
			want: bson.D{{
				Key: mongoDeletedAtPath,
				Value: bson.D{
					{Key: "$type", Value: "long"},
					{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
				},
			}},
		},
		{
			name: "all still rejects malformed metadata",
			mode: store.DeletionAll,
			want: bson.D{{
				Key: "$or",
				Value: bson.A{
					bson.D{{
						Key: mongoDeletedAtPath,
						Value: bson.D{
							{Key: "$type", Value: "null"},
							{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
						},
					}},
					bson.D{{
						Key: mongoDeletedAtPath,
						Value: bson.D{
							{Key: "$type", Value: "long"},
							{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
						},
					}},
				},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := requestPredicate(store.Request{Collection: collection, Deletion: test.mode}, false)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(compiled, test.want) {
				t.Fatalf("deletion predicate = %#v, want %#v", compiled, test.want)
			}
		})
	}
}

func TestMongoNullAndMissingPredicatesMatchRiduTruthTable(t *testing.T) {
	optional := mongoMustPath(t, "optional")
	equalNull, err := compileMongoNode(mongoPredicateCollection(), query.Equal(optional, query.Null()).Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	wantNull := bson.D{{
		Key: "$or",
		Value: bson.A{
			bson.D{{Key: "values.optional", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{
				Key: "values.optional",
				Value: bson.D{
					{Key: "$type", Value: "null"},
					{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
				},
			}},
		},
	}}
	if !reflect.DeepEqual(equalNull, wantNull) {
		t.Fatalf("equal null = %#v, want explicit missing-or-null predicate %#v", equalNull, wantNull)
	}

	notEqualNull, err := compileMongoNode(mongoPredicateCollection(), query.NotEqual(optional, query.Null()).Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	wantNotNull := bson.D{{Key: "$nor", Value: bson.A{wantNull}}}
	if !reflect.DeepEqual(notEqualNull, wantNotNull) {
		t.Fatalf("not equal null = %#v, want exact negation %#v", notEqualNull, wantNotNull)
	}

	exists, err := query.Compare(optional, query.OperatorExists, query.Boolean(false))
	if err != nil {
		t.Fatal(err)
	}
	existsFalse, err := compileMongoNode(mongoPredicateCollection(), exists.Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(existsFalse, wantNull) {
		t.Fatalf("exists false = %#v, want same missing-or-null shape %#v", existsFalse, wantNull)
	}
}

func TestMongoRangesUseExactBSONTypeGuards(t *testing.T) {
	timestamp := "2026-08-30T10:11:12.123456789+01:00"
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		path        string
		value       query.Value
		wantPath    string
		wantType    string
		wantOperand any
	}{
		{name: "text", path: "title", value: query.String("Z"), wantPath: "values.title", wantType: "string", wantOperand: "Z"},
		{name: "number", path: "score", value: query.Number(42), wantPath: "values.score", wantType: "double", wantOperand: float64(42)},
		{name: "revision", path: "_revision", value: query.Number(2), wantPath: mongoRevisionPath, wantType: "long", wantOperand: float64(2)},
		{name: "metadata timestamp", path: "createdAt", value: query.String(timestamp), wantPath: mongoCreatedAtPath, wantType: "long", wantOperand: parsed.UnixNano()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := mongoMustPath(t, test.path)
			compiled, err := compileMongoNode(mongoPredicateCollection(), query.GreaterThan(path, test.value).Node(), "filter", mongoPredicateScope{})
			if err != nil {
				t.Fatal(err)
			}
			children := mongoLogicalChildren(t, compiled, "$and")
			if len(children) != 2 {
				t.Fatalf("range children = %#v", children)
			}
			if got := mongoNestedOperatorValue(children[0], test.wantPath, "$type"); got != test.wantType {
				t.Errorf("type guard = %#v, want %q", got, test.wantType)
			}
			if got := mongoNestedOperatorValue(children[1], test.wantPath, "$gt"); !reflect.DeepEqual(got, test.wantOperand) {
				t.Errorf("range operand = %#v, want %#v", got, test.wantOperand)
			}
		})
	}
}

func TestMongoNestedPredicatesGuardEveryObjectAncestor(t *testing.T) {
	path := mongoMustPath(t, "seo.details.summary")
	exists, err := query.Compare(path, query.OperatorExists, query.Boolean(true))
	if err != nil {
		t.Fatal(err)
	}
	in, err := query.Compare(path, query.OperatorIn, query.List(query.String("safe"), query.Null()))
	if err != nil {
		t.Fatal(err)
	}
	negated, err := query.Not(query.Equal(path, query.String("safe")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		node query.Node
	}{
		{name: "equal", node: query.Equal(path, query.String("safe")).Node()},
		{name: "equal null", node: query.Equal(path, query.Null()).Node()},
		{name: "not equal", node: query.NotEqual(path, query.String("safe")).Node()},
		{name: "contains", node: query.Contains(path, "safe").Node()},
		{name: "range", node: query.GreaterThan(path, query.String("a")).Node()},
		{name: "exists", node: exists.Node()},
		{name: "in including null", node: in.Node()},
		{name: "logical not", node: negated.Node()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := compileMongoNode(mongoPredicateCollection(), test.node, "filter", mongoPredicateScope{})
			if err != nil {
				t.Fatal(err)
			}
			for _, ancestor := range []string{"values.seo", "values.seo.details"} {
				if !mongoContainsNestedOperator(compiled, ancestor, "$type", "object") {
					t.Errorf("nested predicate lacks object guard for %q: %#v", ancestor, compiled)
				}
			}
			if _, err := bson.Marshal(compiled); err != nil {
				t.Fatalf("nested predicate is not valid BSON: %v", err)
			}
		})
	}
}

func TestMongoInvalidTimestampNotEqualMatchesNoDocuments(t *testing.T) {
	createdAt := mongoMustPath(t, "createdAt")
	for _, value := range []query.Value{query.Number(0), query.String("not-a-timestamp")} {
		compiled, err := compileMongoNode(
			mongoPredicateCollection(),
			query.NotEqual(createdAt, value).Node(),
			"access",
			mongoPredicateScope{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if want := mongoConstant(false); !reflect.DeepEqual(compiled, want) {
			t.Fatalf("invalid timestamp not-equal = %#v, want %#v", compiled, want)
		}
	}

	title := mongoMustPath(t, "title")
	compiled, err := compileMongoNode(
		mongoPredicateCollection(),
		query.NotEqual(title, query.Number(0)).Node(),
		"access",
		mongoPredicateScope{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := mongoConstant(true); !reflect.DeepEqual(compiled, want) {
		t.Fatalf("ordinary scalar type-mismatch not-equal = %#v, want %#v", compiled, want)
	}
}

func TestMongoRequestSortUsesNestedShapeAndStableID(t *testing.T) {
	title := mongoMustPath(t, "title")
	updatedAt := mongoMustPath(t, "updatedAt")
	descendingTitle, _ := query.NewSort(title, query.Descending)
	ascendingUpdatedAt, _ := query.NewSort(updatedAt, query.Ascending)
	compiled, err := requestSort(store.Request{
		Collection: mongoPredicateCollection(),
		Sort:       []query.Sort{descendingTitle, ascendingUpdatedAt},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := bson.D{
		{Key: "values.title", Value: int32(-1)},
		{Key: mongoUpdatedAtPath, Value: int32(1)},
		{Key: mongoIDPath, Value: int32(1)},
	}
	if !reflect.DeepEqual(compiled.order, want) || len(compiled.computed) != 0 || len(compiled.temporary) != 0 {
		t.Fatalf("sort = %#v, want %#v", compiled, want)
	}

	id := mongoMustPath(t, "id")
	descendingID, _ := query.NewSort(id, query.Descending)
	compiled, err = requestSort(store.Request{Collection: mongoPredicateCollection(), Sort: []query.Sort{descendingID}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (bson.D{{Key: mongoIDPath, Value: int32(-1)}}); !reflect.DeepEqual(compiled.order, want) {
		t.Fatalf("explicit ID sort = %#v, want no appended duplicate %#v", compiled, want)
	}
}

func TestMongoRequestSortRejectsDuplicateAndOversizedSpecifications(t *testing.T) {
	collection := mongoPredicateCollection()
	title := mongoMustPath(t, "title")
	ascending, _ := query.NewSort(title, query.Ascending)
	descending, _ := query.NewSort(title, query.Descending)
	if _, err := requestSort(store.Request{Collection: collection, Sort: []query.Sort{ascending, descending}}); err == nil || !strings.Contains(err.Error(), "duplicates path") {
		t.Fatalf("duplicate sort error = %v", err)
	}

	large := schema.Collection{ID: "large", Slug: "large"}
	sorts := make([]query.Sort, maxMongoSortTerms)
	for index := range sorts {
		name := "field" + strings.Repeat("x", index) + "a"
		path := mongoMustPath(t, name)
		large.Fields = append(large.Fields, schema.Field{ID: schema.StableID(name), Name: name, Type: schema.FieldTypeText})
		sorts[index], _ = query.NewSort(path, query.Ascending)
	}
	if _, err := requestSort(store.Request{Collection: large, Sort: sorts}); err == nil || !strings.Contains(err.Error(), "at most 32 terms") {
		t.Fatalf("oversized sort error = %v", err)
	}
	if compiled, err := requestSort(store.Request{Collection: large, Sort: sorts[:maxMongoSortTerms-1]}); err != nil || len(compiled.order) != maxMongoSortTerms {
		t.Fatalf("maximum stable sort = %#v, %v", compiled, err)
	}
}

func TestMongoProjectionIncludesMetadataAndRequestedValues(t *testing.T) {
	title := mongoMustPath(t, "title")
	headline := mongoMustPath(t, "seo.headline")
	owner := mongoMustPath(t, "owner")
	compiled, err := requestProjection(store.Request{
		Collection: mongoPredicateCollection(),
		Select:     []query.Path{title, headline, title},
		Populate:   []query.Population{{Path: owner}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := bson.D{
		{Key: mongoIDPath, Value: int32(1)},
		{Key: mongoCodecPath, Value: int32(1)},
		{Key: mongoIncarnationPath, Value: int32(1)},
		{Key: mongoCreatedAtPath, Value: int32(1)},
		{Key: mongoUpdatedAtPath, Value: int32(1)},
		{Key: mongoDeletedAtPath, Value: int32(1)},
		{Key: mongoStatusPath, Value: int32(1)},
		{Key: mongoRevisionPath, Value: int32(1)},
		{Key: mongoFencePath, Value: int32(1)},
		{Key: "values.title", Value: int32(1)},
		{Key: "values.seo.headline", Value: int32(1)},
		{Key: "values.owner", Value: int32(1)},
	}
	if !reflect.DeepEqual(compiled, want) {
		t.Fatalf("projection = %#v, want metadata plus selected/populated values %#v", compiled, want)
	}
	all, err := requestProjection(store.Request{Collection: mongoPredicateCollection()})
	if err != nil || len(all) != 0 {
		t.Fatalf("nil selection projection = %#v, %v; want unrestricted read", all, err)
	}
}

func TestMongoProjectionCoalescesAncestorAndDescendantPaths(t *testing.T) {
	seo := mongoMustPath(t, "seo")
	headline := mongoMustPath(t, "seo.headline")
	seoOwner := mongoMustPath(t, "seo.owner")
	for _, test := range []struct {
		name        string
		selectPaths []query.Path
		populate    []query.Population
	}{
		{name: "ancestor first", selectPaths: []query.Path{seo, headline}},
		{name: "descendant first", selectPaths: []query.Path{headline, seo}},
		{name: "selected ancestor subsumes population", selectPaths: []query.Path{seo}, populate: []query.Population{{Path: seoOwner}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := requestProjection(store.Request{
				Collection: mongoPredicateCollection(), Select: test.selectPaths, Populate: test.populate,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, element := range compiled {
				if strings.HasPrefix(element.Key, "values.seo.") {
					t.Fatalf("projection retains colliding descendant %q: %#v", element.Key, compiled)
				}
			}
			if got := mongoDirectValue(compiled, "values.seo"); got != int32(1) {
				t.Fatalf("coalesced group projection = %#v, want included ancestor", compiled)
			}
		})
	}
}

func TestMongoMalformedNodesFailClosed(t *testing.T) {
	title := mongoMustPath(t, "title")
	comparison := query.Equal(title, query.String("safe")).Node().Comparison
	tests := []struct {
		name string
		node query.Node
		want string
	}{
		{name: "missing comparison", node: query.Node{Kind: query.ExpressionComparison}, want: "missing its comparison"},
		{name: "comparison children", node: query.Node{Kind: query.ExpressionComparison, Comparison: comparison, Children: []query.Node{{}}}, want: "must not have children"},
		{name: "short and", node: query.Node{Kind: query.ExpressionAnd, Children: []query.Node{{}}}, want: "at least two children"},
		{name: "wide not", node: query.Node{Kind: query.ExpressionNot, Children: []query.Node{{}, {}}}, want: "exactly one child"},
		{name: "unknown kind", node: query.Node{Kind: "injected"}, want: "unsupported expression kind"},
		{name: "unknown operator", node: query.Node{Kind: query.ExpressionComparison, Comparison: &query.Comparison{Path: title, Operator: "injected", Value: query.String("safe")}}, want: "unsupported comparison operator"},
		{name: "in scalar", node: query.Node{Kind: query.ExpressionComparison, Comparison: &query.Comparison{Path: title, Operator: query.OperatorIn, Value: query.String("safe")}}, want: "requires a list operand"},
		{name: "empty path", node: query.Node{Kind: query.ExpressionComparison, Comparison: &query.Comparison{Operator: query.OperatorEqual, Value: query.String("safe")}}, want: "path is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := compileMongoNode(mongoPredicateCollection(), test.node, "filter", mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("compile error = %v, want containing %q", err, test.want)
			}
		})
	}
	if _, err := requestPredicate(store.Request{Collection: mongoPredicateCollection()}, true); err == nil || !strings.Contains(err.Error(), "document ID is required") {
		t.Fatalf("missing required ID error = %v", err)
	}
}

func TestMongoUnsupportedSchemaPathsFailClosed(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "localized", path: "localizedTitle", want: "localized"},
		{name: "opaque JSON", path: "metadata", want: "non-scalar"},
		{name: "unknown nested", path: "seo.missing", want: "not in collection"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := mongoMustPath(t, test.path)
			node := query.Equal(path, query.String("value")).Node()
			if _, err := compileMongoNode(mongoPredicateCollection(), node, "filter", mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("compile error = %v, want containing %q", err, test.want)
			}
		})
	}

	localized := mongoMustPath(t, "localizedTitle")
	access := query.Equal(localized, query.String("visible")).Node()
	if _, err := requestPredicate(store.Request{
		Collection: mongoPredicateCollection(), Access: &access, AllLocales: true,
		Locales: []schema.LocaleCode{"en", "fr"}, LocaleChain: []schema.LocaleCode{"en"},
	}, false); err != nil {
		t.Fatalf("localized all-locales access was rejected: %v", err)
	}
	nonLocalized := query.Equal(mongoMustPath(t, "title"), query.String("visible")).Node()
	if _, err := requestPredicate(store.Request{
		Collection: mongoPredicateCollection(), Access: &nonLocalized, AllLocales: true,
	}, false); err != nil {
		t.Fatalf("non-localized all-locales access was rejected: %v", err)
	}

	collection := mongoPredicateCollection()
	for _, root := range []string{"tags", "rows", "layout"} {
		compiled, err := requestProjection(store.Request{
			Collection: collection,
			Select:     []query.Path{mongoMustPath(t, root)},
		})
		if err != nil {
			t.Fatalf("whole-root %s projection: %v", root, err)
		}
		if got := mongoDirectValue(compiled, "values."+root); got != int32(1) {
			t.Fatalf("whole-root %s projection = %#v", root, compiled)
		}
	}
	for _, nested := range []string{"rows.label", "layout.hero.heading"} {
		if _, err := requestProjection(store.Request{
			Collection: collection,
			Select:     []query.Path{mongoMustPath(t, nested)},
		}); err == nil || !strings.Contains(err.Error(), "only whole-root repeated projections") {
			t.Fatalf("nested repeated projection %q error = %v", nested, err)
		}
	}
}

func TestMongoRepeatedPredicatesPreserveDocumentScopedBooleanSemantics(t *testing.T) {
	collection := mongoRepeatedCollection()
	rowsKind := mongoMustPath(t, "rows.kind")
	rowsLabel := mongoMustPath(t, "rows.label")

	splitRows, err := query.And(
		query.Equal(rowsKind, query.String("primary")),
		query.Equal(rowsLabel, query.String("visible")),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileMongoNode(collection, splitRows.Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	if matches := mongoElementMatches(compiled, "values.rows"); len(matches) != 2 {
		t.Fatalf("document-scoped array AND compiled to %d elemMatch clauses, want two independent row clauses: %#v", len(matches), compiled)
	}
	if _, err := bson.Marshal(compiled); err != nil {
		t.Fatalf("array predicate is not valid BSON: %v", err)
	}

	block := query.Equal(mongoMustPath(t, "layout.hero.heading"), query.String("Welcome"))
	compiled, err = compileMongoNode(collection, block.Node(), "access", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	matches := mongoElementMatches(compiled, "values.layout")
	if len(matches) != 1 {
		t.Fatalf("block comparison elemMatch clauses = %#v, want one", matches)
	}
	if !mongoContainsNestedOperator(matches[0], "blockType", "$eq", "hero") {
		t.Fatalf("block discriminator is not in comparison-local elemMatch %#v", matches[0])
	}
	if !mongoContainsNestedOperator(matches[0], "heading", "$eq", "Welcome") {
		t.Fatalf("block value is not in the same elemMatch %#v", matches[0])
	}
}

func TestMongoRepeatedSelectUsesWholeListTruthTable(t *testing.T) {
	collection := mongoRepeatedCollection()
	tags := mongoMustPath(t, "tags")

	contains, err := compileMongoNode(collection, query.Contains(tags, "alpha").Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	matches := mongoElementMatches(contains, "values.tags")
	if len(matches) != 1 || mongoDirectValue(matches[0], "$eq") != "alpha" {
		t.Fatalf("has-many select contains = %#v, want exact scalar membership", contains)
	}
	for _, expression := range []query.Expression{
		query.Equal(tags, query.String("alpha")),
		query.In(tags, query.String("alpha")),
	} {
		compiled, err := compileMongoNode(collection, expression.Node(), "filter", mongoPredicateScope{})
		if err != nil {
			t.Fatal(err)
		}
		if !mongoContainsConstant(compiled, false) {
			t.Fatalf("whole-list scalar comparison did not compile false: %#v", compiled)
		}
	}
}

func TestMongoRepeatedNegationAndDisjunctionKeepShapeGuardsOutside(t *testing.T) {
	collection := mongoRepeatedCollection()
	repeated := query.Equal(mongoMustPath(t, "rows.kind"), query.String("primary"))

	negated, err := query.Not(repeated)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileMongoNode(collection, negated.Node(), "filter", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	children := mongoLogicalChildren(t, compiled, "$and")
	if len(children) < 2 || !mongoContainsKey(children[0], "$jsonSchema") || !mongoContainsKey(children[0], "$setUnion") || !mongoContainsKey(children[len(children)-1], "$nor") {
		t.Fatalf("repeated NOT lacks an outer shape guard: %#v", compiled)
	}

	disjunction, err := query.Or(repeated, query.Equal(mongoMustPath(t, "title"), query.String("visible")))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err = compileMongoNode(collection, disjunction.Node(), "access", mongoPredicateScope{})
	if err != nil {
		t.Fatal(err)
	}
	children = mongoLogicalChildren(t, compiled, "$and")
	if len(children) < 2 || !mongoContainsKey(children[0], "$jsonSchema") || !mongoContainsKey(children[0], "$setUnion") || !mongoContainsKey(children[len(children)-1], "$or") {
		t.Fatalf("repeated OR lacks an outer shape guard: %#v", compiled)
	}
}

func TestMongoRepeatedRootShapeGuardCoversTheStrictStorageEnvelope(t *testing.T) {
	collection := mongoRepeatedCollection()
	for _, test := range []struct {
		name      string
		path      string
		fragments []string
	}{
		{
			name: "has-many select", path: "tags",
			fragments: []string{`"enum":["alpha","beta"]`, `"uniqueItems":true`},
		},
		{
			name: "array row", path: "rows.kind",
			fragments: []string{`"additionalProperties":false`, `\\x{3000}`, `"required":["kind","label"]`, `"details"`, `"bsonType":["object","null"]`, `"enum":["draft","published","",null]`, `"enum":["quiet","loud","",null]`},
		},
		{
			name: "block row", path: "layout.hero.heading",
			fragments: []string{`"oneOf"`, `"enum":["hero"]`, `"enum":["quote"]`, `"required":["blockType","heading"]`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := resolveMongoPredicatePath(collection, mongoMustPath(t, test.path), "access", mongoPredicateScope{})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := bson.MarshalExtJSON(mongoRepeatedShapePredicate(resolved), false, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(string(encoded), fragment) {
					t.Fatalf("shape guard for %q lacks %s: %s", test.path, fragment, encoded)
				}
			}
		})
	}

	notEqual, err := compileMongoNode(
		collection,
		query.NotEqual(mongoMustPath(t, "rows.kind"), query.String("blocked")).Node(),
		"access",
		mongoPredicateScope{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !mongoContainsKey(notEqual, "$jsonSchema") || !mongoContainsKey(notEqual, "required") {
		t.Fatalf("repeated not-equal lacks its full root guard: %#v", notEqual)
	}
}

func TestMongoDecoderFreePredicateGuardsTheCompleteAuthoredEnvelope(t *testing.T) {
	collection := mongoRepeatedCollection()
	titleFilter := query.Equal(mongoMustPath(t, "title"), query.String("visible")).Node()
	compiled, err := decoderFreeRequestPredicate(store.Request{
		Collection: collection,
		Filter:     &titleFilter,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.MarshalExtJSON(compiled, false, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, fragment := range []string{
		`"values"`, `"additionalProperties":false`, `"title"`, `"tags"`, `"rows"`, `"layout"`,
		`"uniqueItems":true`, `"oneOf"`, `"$setUnion"`, `"$objectToArray"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("decoder-free envelope lacks %s: %s", fragment, text)
		}
	}
	if !mongoContainsKey(compiled, "values.title") {
		t.Fatalf("decoder-free predicate lost the caller filter: %#v", compiled)
	}
	if strings.Count(text, `"riduRepeated":`) != 2 {
		t.Fatalf("decoder-free envelope row-key guards = %d, want array and blocks roots: %s", strings.Count(text, `"riduRepeated":`), text)
	}

	localized := mongoLocalizedScalarCollection()
	configured, err := mongoCollectionEnvelopePredicate(localized, []schema.LocaleCode{"en", "fr"})
	if err != nil {
		t.Fatal(err)
	}
	configuredJSON, err := bson.MarshalExtJSON(configured, false, false)
	if err != nil {
		t.Fatal(err)
	}
	configuredText := string(configuredJSON)
	if !strings.Contains(configuredText, `"en"`) || !strings.Contains(configuredText, `"fr"`) || strings.Contains(configuredText, `"patternProperties"`) {
		t.Fatalf("configured localized envelope is not an exact locale map: %s", configuredText)
	}
	sparse, err := mongoCollectionEnvelopePredicate(localized, nil)
	if err != nil {
		t.Fatal(err)
	}
	sparseJSON, err := bson.MarshalExtJSON(sparse, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sparseJSON), `"patternProperties"`) || !strings.Contains(string(sparseJSON), mongoLocaleCodePattern) {
		t.Fatalf("request-less localized envelope lacks canonical locale-key validation: %s", sparseJSON)
	}

	relationPath := mongoMustPath(t, "related")
	relations := mongoScalarCollection(false)
	relations.Fields = append(relations.Fields, schema.Field{
		ID: "posts-related", Name: "related", Path: relationPath,
		Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{
			Polymorphic: true, HasMany: true, Targets: []schema.RelationshipTarget{
				{CollectionID: "authors", CollectionSlug: "authors"},
				{CollectionID: "editors", CollectionSlug: "editors"},
			},
		},
	})
	relationGuard, err := mongoCollectionEnvelopePredicate(relations, nil)
	if err != nil {
		t.Fatal(err)
	}
	relationJSON, err := bson.MarshalExtJSON(relationGuard, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"relationTo"`, `"id"`, `"authors"`, `"editors"`, `"maxItems":512`, `"$strLenBytes"`} {
		if !strings.Contains(string(relationJSON), fragment) {
			t.Fatalf("relationship envelope lacks %s: %s", fragment, relationJSON)
		}
	}
}

func TestMongoRepeatedShapeGuardDedupUsesTheCompleteResolvedShape(t *testing.T) {
	collection := mongoRepeatedCollection()
	kind := query.Equal(mongoMustPath(t, "rows.kind"), query.String("primary"))
	note := query.Equal(mongoMustPath(t, "rows.details.note"), query.String("nested"))

	for _, expressions := range [][]query.Expression{{kind, note}, {note, kind}} {
		conjunction, err := query.And(expressions...)
		if err != nil {
			t.Fatal(err)
		}
		guards, err := mongoNodeRepeatedShapeGuards(collection, conjunction.Node(), "access", mongoPredicateScope{})
		if err != nil {
			t.Fatal(err)
		}
		if len(guards) != 2 {
			t.Fatalf("complete repeated guards = %d, want two distinct resolved shapes: %#v", len(guards), guards)
		}
		for _, guard := range guards {
			if !mongoContainsKey(guard, "$jsonSchema") || !mongoContainsKey(guard, "details") {
				t.Fatalf("complete repeated guard omits nested-group schema: %#v", guard)
			}
		}
	}
}

func TestMongoRepeatedPathsAreScopedForVersionsAndBoundedByRole(t *testing.T) {
	collection := mongoRepeatedCollection()
	path := mongoMustPath(t, "rows.label")
	compiled, err := compileMongoNode(
		collection,
		query.Equal(path, query.String("visible")).Node(),
		"version access",
		mongoPredicateScope{storagePrefix: mongoVersionSnapshotPath + "."},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.MarshalExtJSON(compiled, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "snapshot.values.rows") {
		t.Fatalf("version predicate was not scoped to its snapshot: %s", encoded)
	}

	sortTerm, err := query.NewSort(path, query.Ascending)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requestSort(store.Request{Collection: collection, Sort: []query.Sort{sortTerm}}); err == nil || !strings.Contains(err.Error(), "does not support repeated path") {
		t.Fatalf("repeated sort error = %v", err)
	}
	for _, role := range []string{"distinct", "index", "compound index", "list window"} {
		if _, err := resolveMongoPredicatePath(collection, path, role, mongoPredicateScope{}); err == nil || !strings.Contains(err.Error(), "does not support repeated path") {
			t.Fatalf("repeated %s error = %v", role, err)
		}
	}
	projection, err := requestProjection(store.Request{
		Collection: collection,
		Select:     []query.Path{mongoMustPath(t, "title")},
		Populate:   []query.Population{{Path: mongoMustPath(t, "rows")}},
	})
	if err != nil {
		t.Fatalf("repeated population projection: %v", err)
	}
	if !mongoContainsKey(projection, "values.rows") {
		t.Fatalf("repeated population projection omits its whole root: %#v", projection)
	}
}

func mongoPredicateCollection() schema.Collection {
	return schema.Collection{
		ID:       "posts",
		Slug:     "posts",
		Versions: &schema.VersionSettings{Drafts: true},
		Fields: []schema.Field{
			{ID: "title", Name: "title", Type: schema.FieldTypeText},
			{ID: "optional", Name: "optional", Type: schema.FieldTypeText},
			{ID: "score", Name: "score", Type: schema.FieldTypeNumber},
			{ID: "published", Name: "published", Type: schema.FieldTypeCheckbox},
			{ID: "localized-title", Name: "localizedTitle", Type: schema.FieldTypeText, Localized: true},
			{ID: "metadata", Name: "metadata", Type: schema.FieldTypeJSON},
			{ID: "owner", Name: "owner", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionID: "users", CollectionSlug: "users"}},
			{ID: "tags", Name: "tags", Type: schema.FieldTypeSelect, Select: &schema.SelectField{HasMany: true}},
			{ID: "seo", Name: "seo", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
				{ID: "seo-headline", Name: "headline", Type: schema.FieldTypeText},
				{ID: "seo-owner", Name: "owner", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionID: "users", CollectionSlug: "users"}},
				{ID: "seo-details", Name: "details", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{
					{ID: "seo-details-summary", Name: "summary", Type: schema.FieldTypeText},
				}}},
			}}},
			{ID: "rows", Name: "rows", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{
				{ID: "row-label", Name: "label", Type: schema.FieldTypeText},
			}}},
			{ID: "layout", Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{
				Slug: "hero", Fields: []schema.Field{{ID: "hero-heading", Name: "heading", Type: schema.FieldTypeText}},
			}}}},
		},
	}
}

func mongoMustPath(t *testing.T, value string) query.Path {
	t.Helper()
	path, err := query.ParsePath(value)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func mongoLogicalChildren(t *testing.T, document bson.D, operator string) bson.A {
	t.Helper()
	value := mongoDirectValue(document, operator)
	children, ok := value.(bson.A)
	if !ok {
		t.Fatalf("%s value = %#v, want bson.A in %#v", operator, value, document)
	}
	return children
}

func mongoDirectValue(document bson.D, key string) any {
	for _, element := range document {
		if element.Key == key {
			return element.Value
		}
	}
	return nil
}

func mongoContainsKey(value any, key string) bool {
	switch value := value.(type) {
	case bson.D:
		for _, element := range value {
			if element.Key == key || mongoContainsKey(element.Value, key) {
				return true
			}
		}
	case bson.A:
		for _, item := range value {
			if mongoContainsKey(item, key) {
				return true
			}
		}
	}
	return false
}

func mongoValueForKey(value any, key string) any {
	switch value := value.(type) {
	case bson.D:
		for _, element := range value {
			if element.Key == key {
				return element.Value
			}
			if found := mongoValueForKey(element.Value, key); found != nil {
				return found
			}
		}
	case bson.A:
		for _, item := range value {
			if found := mongoValueForKey(item, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func mongoNestedOperatorValue(value any, path, operator string) any {
	document, ok := value.(bson.D)
	if !ok {
		return nil
	}
	nested := mongoDirectValue(document, path)
	operators, ok := nested.(bson.D)
	if !ok {
		return nil
	}
	return mongoDirectValue(operators, operator)
}

func mongoContainsNestedOperator(value any, path, operator string, expected any) bool {
	switch value := value.(type) {
	case bson.D:
		if reflect.DeepEqual(mongoNestedOperatorValue(value, path, operator), expected) {
			return true
		}
		for _, element := range value {
			if mongoContainsNestedOperator(element.Value, path, operator, expected) {
				return true
			}
		}
	case bson.A:
		for _, item := range value {
			if mongoContainsNestedOperator(item, path, operator, expected) {
				return true
			}
		}
	}
	return false
}

func mongoElementMatches(value any, path string) []bson.D {
	var matches []bson.D
	switch value := value.(type) {
	case bson.D:
		for _, element := range value {
			if element.Key == path {
				operators, ok := element.Value.(bson.D)
				if ok {
					if match, ok := mongoDirectValue(operators, "$elemMatch").(bson.D); ok {
						matches = append(matches, match)
					}
				}
			}
			matches = append(matches, mongoElementMatches(element.Value, path)...)
		}
	case bson.A:
		for _, item := range value {
			matches = append(matches, mongoElementMatches(item, path)...)
		}
	}
	return matches
}

func mongoContainsConstant(value any, expected bool) bool {
	switch value := value.(type) {
	case bson.D:
		for _, element := range value {
			if element.Key == "$expr" && reflect.DeepEqual(element.Value, expected) {
				return true
			}
			if mongoContainsConstant(element.Value, expected) {
				return true
			}
		}
	case bson.A:
		for _, item := range value {
			if mongoContainsConstant(item, expected) {
				return true
			}
		}
	}
	return false
}

func mongoRegexes(value any) []bson.Regex {
	var result []bson.Regex
	switch value := value.(type) {
	case bson.Regex:
		result = append(result, value)
	case bson.D:
		for _, element := range value {
			result = append(result, mongoRegexes(element.Value)...)
		}
	case bson.A:
		for _, item := range value {
			result = append(result, mongoRegexes(item)...)
		}
	}
	return result
}
