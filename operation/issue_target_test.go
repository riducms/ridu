package operation_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/operation"
)

func TestIssueTargetFactoriesRemainDetached(t *testing.T) {
	base := operation.At().Row("section-a").Field("links")
	url := base.Row("link-b").Field("url")
	label := base.Row("link-c").Field("label")
	segments := url.Segments()
	segments[0].RowKey = "mutated"
	if len(base.Segments()) != 2 || url.Segments()[0].RowKey != "section-a" || label.Segments()[2].RowKey != "link-c" {
		t.Fatal("target refinement or inspection mutated another target")
	}
	if !reflect.DeepEqual(operation.At("seo.title").Segments(), operation.At("seo").Field("title").Segments()) {
		t.Fatal("simple descendant paths disagree")
	}
	if err := operation.At().Row("key.with[syntax]").Field("url").Err(); err != nil {
		t.Fatal("stable keys must remain literal identities:", err)
	}
}

func TestIssueTargetRejectsIndexesAndUnboundedSelectors(t *testing.T) {
	for _, target := range []operation.IssueTarget{
		operation.At("rows.0.title"), operation.At(""), operation.At().Row(""),
		operation.At().Block("row", ""), operation.At().Block("", "card"), operation.At().Locale(""),
		operation.At().Row(strings.Repeat("a", 4097)), operation.At(strings.Repeat("x.", 64) + "x"),
	} {
		if target.Err() == nil {
			t.Fatalf("malformed target accepted: %+v", target.Segments())
		}
		if target.Field("title").Err() == nil {
			t.Fatal("further refinement erased an earlier error")
		}
	}
}
