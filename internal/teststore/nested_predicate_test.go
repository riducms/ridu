package teststore

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestNestedPredicatesMatchGroupsArraysAndBlocks(t *testing.T) {
	document := store.Document{ID: "post-1", Values: store.Values{
		"seo":      store.Object(store.Values{"title": store.String("Ridu docs")}),
		"optional": store.Null(),
		"links": store.List(
			store.Object(store.Values{"label": store.String("Docs")}),
			store.Object(store.Values{"label": store.String("Roadmap")}),
		),
		"layout": store.List(store.Object(store.Values{
			"blockType": store.String("hero"),
			"heading":   store.String("Build with Ridu"),
		})),
	}}
	for _, test := range []struct {
		path  string
		value string
	}{
		{path: "seo.title", value: "Ridu docs"},
		{path: "links.label", value: "Roadmap"},
		{path: "layout.hero.heading", value: "Build with Ridu"},
	} {
		path, _ := query.ParsePath(test.path)
		if !matches(document, query.Equal(path, query.String(test.value)).Node()) {
			t.Errorf("%s did not match", test.path)
		}
	}
	path, _ := query.ParsePath("links.label")
	if matches(document, query.NotEqual(path, query.String("Docs")).Node()) {
		t.Fatal("array not-equals matched while one row equals the excluded value")
	}
	nullPath, _ := query.ParsePath("optional")
	if matches(document, query.NotEqual(nullPath, query.Null()).Node()) {
		t.Fatal("explicit null matched not-equals null")
	}
}
