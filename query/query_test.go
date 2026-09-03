package query_test

import (
	"encoding/json"
	"strings"
	"testing"

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

	expression, err := query.And(
		query.Equal(status, query.String("published")),
		query.NotEqual(title, query.String("Hidden")),
	)
	if err != nil {
		t.Fatalf("And: %v", err)
	}

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
