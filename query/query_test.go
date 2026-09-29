package query_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
)

func TestPathIsValidatedAndImmutable(t *testing.T) {
	path, err := query.ParsePath("layout.featured-post.metaTitle")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	segments := path.Segments()
	segments[0] = "mutated"

	if got := path.String(); got != "layout.featured-post.metaTitle" {
		t.Fatalf("path = %q, want immutable layout.featured-post.metaTitle", got)
	}
	encoded, err := json.Marshal(path)
	if err != nil {
		t.Fatalf("Marshal path: %v", err)
	}
	if got, want := string(encoded), `"layout.featured-post.metaTitle"`; got != want {
		t.Fatalf("encoded path = %s, want %s", got, want)
	}
}

func TestCompleteScalarOperatorsValidateOperands(t *testing.T) {
	path, _ := query.NewPath("title")
	for _, operator := range []query.Operator{
		query.OperatorGreaterThan,
		query.OperatorGreaterThanEqual,
		query.OperatorLessThan,
		query.OperatorLessThanEqual,
	} {
		if _, err := query.Compare(path, operator, query.Number(10)); err != nil {
			t.Fatalf("Compare(%s): %v", operator, err)
		}
	}
	for _, operator := range []query.Operator{query.OperatorContains, query.OperatorLike} {
		if _, err := query.Compare(path, operator, query.String("ridu cms")); err != nil {
			t.Fatalf("Compare(%s): %v", operator, err)
		}
		if _, err := query.Compare(path, operator, query.Number(10)); err == nil {
			t.Fatalf("Compare(%s) accepted a number", operator)
		}
	}
}

func TestPathRejectsInvalidSegments(t *testing.T) {
	for _, value := range []string{"", "seo..title", "SEO.title", "seo.title slug"} {
		if _, err := query.ParsePath(value); err == nil {
			t.Errorf("ParsePath(%q) succeeded, want error", value)
		}
	}
}

func TestPathAdmitsOnlyCanonicalVersionMetadata(t *testing.T) {
	for _, value := range []string{"_status", "_revision"} {
		path, err := query.ParsePath(value)
		if err != nil {
			t.Fatalf("ParsePath(%s): %v", value, err)
		}
		if got := path.String(); got != value {
			t.Fatalf("metadata path = %q, want %q", got, value)
		}
	}
	for _, value := range []string{"_id", "_status.value", "group._status"} {
		if _, err := query.ParsePath(value); err == nil {
			t.Errorf("ParsePath(%q) succeeded, want reserved metadata rejection", value)
		}
	}
}

func TestPathRejectsUnboundedSegmentsAndBytes(t *testing.T) {
	segments := make([]string, query.MaxPathSegments+1)
	for index := range segments {
		segments[index] = "field"
	}
	if _, err := query.NewPath(segments...); err == nil {
		t.Fatal("path exceeding segment limit succeeded")
	}
	if _, err := query.NewPath("field" + strings.Repeat("a", query.MaxPathBytes)); err == nil {
		t.Fatal("path exceeding byte limit succeeded")
	}
}

func TestPathJSONRoundTrip(t *testing.T) {
	var path query.Path
	if err := json.Unmarshal([]byte(`"seo.title"`), &path); err != nil {
		t.Fatalf("Unmarshal path: %v", err)
	}
	if got := path.String(); got != "seo.title" {
		t.Fatalf("decoded path = %q, want seo.title", got)
	}
	if err := json.Unmarshal([]byte(`"SEO.title"`), &path); err == nil {
		t.Fatal("Unmarshal invalid path succeeded, want validation error")
	}
}

func TestExpressionSnapshotsAreDetached(t *testing.T) {
	status, err := query.ParsePath("status")
	if err != nil {
		t.Fatal(err)
	}
	title, err := query.ParsePath("title")
	if err != nil {
		t.Fatal(err)
	}

	expression := query.And(
		query.Equal(status, query.String("published")),
		query.NotEqual(title, query.String("Hidden")),
	)

	snapshot := expression.Node()
	snapshot.Children[0].Comparison.Operator = query.OperatorNotEqual
	snapshot.Children = nil

	fresh := expression.Node()
	if len(fresh.Children) != 2 {
		t.Fatalf("fresh child count = %d, want 2", len(fresh.Children))
	}
	if got := fresh.Children[0].Comparison.Operator; got != query.OperatorEqual {
		t.Fatalf("fresh operator = %q, want equal", got)
	}
}

func TestOperatorValueCompatibility(t *testing.T) {
	path, err := query.ParsePath("status")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := query.Compare(path, query.OperatorIn, query.String("draft")); err == nil {
		t.Fatal("Compare(in, string) succeeded, want list compatibility error")
	}
	if _, err := query.Compare(path, query.OperatorEqual, query.List(query.String("draft"))); err == nil {
		t.Fatal("Compare(equal, list) succeeded, want scalar compatibility error")
	}
}

func TestExistsRequiresBooleanOperand(t *testing.T) {
	path, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := query.Compare(path, query.OperatorExists, query.Boolean(true)); err != nil {
		t.Fatalf("boolean exists comparison: %v", err)
	}
	if _, err := query.Compare(path, query.OperatorExists, query.String("yes")); err == nil {
		t.Fatal("string exists comparison succeeded")
	}
}

func TestFieldAndLogicalHelpersPanicOnlyOnProgrammerErrors(t *testing.T) {
	if got := query.Field("seo", "title").String(); got != "seo.title" {
		t.Fatalf("Field path = %q", got)
	}
	status := query.Equal(query.Field("status"), query.String("published"))
	if got := query.And(status).Kind(); got != query.ExpressionComparison {
		t.Fatalf("And with one condition = %q, want the condition itself", got)
	}
	if got := query.Or(status).Kind(); got != query.ExpressionComparison {
		t.Fatalf("Or with one condition = %q, want the condition itself", got)
	}
	if got := query.Not(status).Kind(); got != query.ExpressionNot {
		t.Fatalf("Not kind = %q", got)
	}
	for name, call := range map[string]func(){
		"dotted field":  func() { query.Field("seo.title") },
		"empty field":   func() { query.Field() },
		"empty And":     func() { query.And() },
		"empty Or":      func() { query.Or() },
		"nil condition": func() { query.And(status, nil) },
		"nil Not":       func() { query.Not(nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			call()
		}()
	}
}

type statusName string

type rank uint16

func TestHelpersAcceptFieldNamesAndPlainValues(t *testing.T) {
	cases := []struct {
		name       string
		expression query.Expression
		want       string
	}{
		{"string", query.Equal("status", "published"), `status equal "published"`},
		{"nested name", query.NotEqual("seo.title", "Draft"), `seo.title not_equal "Draft"`},
		{"path", query.Equal(query.Field("seo", "title"), query.Null()), `seo.title equal null`},
		{"named string", query.Equal("status", statusName("draft")), `status equal "draft"`},
		{"boolean", query.Equal("featured", true), `featured equal true`},
		{"integer", query.GreaterThan("position", 3), `position greater_than 3`},
		{"named unsigned", query.LessThanEqual("rank", rank(7)), `rank less_than_equal 7`},
		{"float", query.GreaterThanEqual("score", 2.5), `score greater_than_equal 2.5`},
		{"in", query.In("status", "draft", "published"), `status in ["draft","published"]`},
		{"in values", query.In("status", []query.Value{query.String("draft")}...), `status in ["draft"]`},
		{"contains", query.Contains("title", "ridu"), `title contains "ridu"`},
		{"like", query.Like("title", "go cms"), `title like "go cms"`},
		{"exists", query.Exists("image", false), `image exists false`},
	}
	for _, test := range cases {
		comparison := test.expression.Node().Comparison
		value, err := json.Marshal(comparison.Value)
		if err != nil {
			t.Fatalf("%s: marshal value: %v", test.name, err)
		}
		if got := comparison.Path.String() + " " + string(comparison.Operator) + " " + string(value); got != test.want {
			t.Errorf("%s = %s, want %s", test.name, got, test.want)
		}
	}
}

func TestHelpersPanicOnValuesANumberCannotHold(t *testing.T) {
	for name, call := range map[string]func(){
		"malformed name":   func() { query.Equal("SEO.title", "x") },
		"empty name":       func() { query.Equal("", "x") },
		"large integer":    func() { query.Equal("id", int64(1)<<60) },
		"large unsigned":   func() { query.Equal("id", uint64(1)<<60) },
		"not a number":     func() { query.GreaterThan("score", math.NaN()) },
		"infinite":         func() { query.LessThan("score", math.Inf(1)) },
		"list for equal":   func() { query.Equal("status", query.List(query.String("x"))) },
		"boolean in range": func() { query.GreaterThan("score", query.Boolean(true)) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			call()
		}()
	}
	if got := query.Equal("id", int64(1)<<53).Node().Comparison.Value; got.Kind() != query.ValueNumber {
		t.Fatalf("2^53 = %v, want a number", got)
	}
}

func TestSortHelpers(t *testing.T) {
	for _, test := range []struct {
		sort      query.Sort
		path      string
		direction query.Direction
	}{
		{query.Asc("title"), "title", query.Ascending},
		{query.Desc("seo.title"), "seo.title", query.Descending},
		{query.Desc(query.Field("createdAt")), "createdAt", query.Descending},
	} {
		if test.sort.Path.String() != test.path || test.sort.Direction != test.direction {
			t.Errorf("sort = %s %s, want %s %s", test.sort.Path, test.sort.Direction, test.path, test.direction)
		}
	}
}

// embedded satisfies Expression without being the package's own type.
type embedded struct{ query.Expression }

func TestLogicalHelpersMergeNestedConditions(t *testing.T) {
	a, b, c := query.Equal("a", 1), query.Equal("b", 2), query.Equal("c", 3)
	shape := func(expression query.Expression) string {
		var describe func(query.Node) string
		describe = func(node query.Node) string {
			if node.Comparison != nil {
				return node.Comparison.Path.String()
			}
			parts := make([]string, len(node.Children))
			for index, child := range node.Children {
				parts[index] = describe(child)
			}
			return string(node.Kind) + "(" + strings.Join(parts, " ") + ")"
		}
		return describe(expression.Node())
	}
	for _, test := range []struct {
		expression query.Expression
		want       string
	}{
		{query.And(query.And(a, b), c), "and(a b c)"},
		{query.Or(a, query.Or(b, c)), "or(a b c)"},
		{query.And(query.Or(a, b), c), "and(or(a b) c)"},
		{query.Or(query.And(a, b), c), "or(and(a b) c)"},
		{query.And(query.Not(query.And(a, b)), c), "and(not(and(a b)) c)"},
		{query.And(embedded{query.And(a, b)}, c), "and(a b c)"},
		{query.Not(embedded{a}), "not(a)"},
	} {
		if got := shape(test.expression); got != test.want {
			t.Errorf("shape = %s, want %s", got, test.want)
		}
	}
}

func TestBuildingAFilterIncrementallyHasLinearAllocationCount(t *testing.T) {
	build := func(count int) float64 {
		return testing.AllocsPerRun(20, func() {
			filter := query.Equal("a", "x")
			for range count {
				filter = query.And(filter, query.Equal("b", "y"))
			}
		})
	}
	small, large := build(10), build(40)
	// Sharing immutable nodes keeps allocation counts near 4 times for 4 times
	// the conditions. This does not measure the bytes copied while flattening.
	if large > small*6 {
		t.Fatalf("allocations grew from %.0f to %.0f for 4 times the conditions", small, large)
	}
}

func TestDateTimeMatchesTheAdminFormat(t *testing.T) {
	moment := time.Date(2026, 9, 29, 15, 5, 0, 0, time.FixedZone("BST", 3600))
	if got, _ := query.DateTime(moment).StringValue(); got != "2026-09-29T14:05:00.000Z" {
		t.Fatalf("DateTime = %q", got)
	}
	if got := query.GreaterThan("publishedAt", query.DateTime(moment)).Kind(); got != query.ExpressionComparison {
		t.Fatalf("comparison kind = %q", got)
	}
}
