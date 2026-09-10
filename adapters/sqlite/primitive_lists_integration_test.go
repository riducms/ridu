package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListsSQLiteLifecycle(t *testing.T) {
	config := primitivelists.Config()
	config.Collections[0].Access.Publish = config.Collections[0].Access.Update
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "lists.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.Exercise(t, app)
}

func TestPrimitiveListsSQLiteRepeatedQueryBoundary(t *testing.T) {
	config := primitivelists.Config()
	config.Collections[0].Fields = append(config.Collections[0].Fields, field.Array("sections", field.Fields{field.Array("links", field.Fields{field.TextList("labels")})}))
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "list-queries.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	values := primitivelists.Values()
	values["sections"] = store.List(store.Object(store.Values{"_key": store.String("section-A"), "links": store.List(store.Object(store.Values{"_key": store.String("link-A"), "labels": store.List(store.String("nested"), store.String("nested"))}))}))
	first, err := app.Local().Create(t.Context(), "primitive-products", values, nil)
	if err != nil {
		t.Fatal(err)
	}
	decoy := primitivelists.Values()
	decoy["variants"] = store.List(store.Object(store.Values{"_key": store.String("B"), "points": store.List(store.String("Pine")), "sizes": store.List(store.Number(99))}))
	decoy["content"] = store.List(store.Object(store.Values{"_key": store.String("B"), "blockType": store.String("note"), "points": store.List(store.String("Oak")), "sizes": store.List(store.Number(0))}))
	second, err := app.Local().Create(t.Context(), "primitive-products", decoy, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		item   query.Value
		wantID string
	}{
		{"variants.points", query.String("Oak"), first.ID},
		{"variants.points", query.String("Pine"), second.ID},
		{"variants.sizes", query.Number(0), first.ID},
		{"content.card.points", query.String("Oak"), first.ID},
		{"content.card.sizes", query.Number(8.5), first.ID},
		{"content.note.points", query.String("Oak"), second.ID},
		{"sections.links.labels", query.String("nested"), first.ID},
	} {
		path, _ := query.ParsePath(test.path)
		for _, negate := range []bool{false, true} {
			expression := query.In(path, test.item)
			want := test.wantID
			if negate {
				expression, _ = query.Not(expression)
				want = first.ID
				if test.wantID == first.ID {
					want = second.ID
				}
			}
			page, err := app.Local().List(t.Context(), "primitive-products", core.ListOptions{Where: expression})
			if err != nil || page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != want {
				t.Fatalf("path %s negate=%v page=%#v: %v", test.path, negate, page, err)
			}
		}
	}
}
