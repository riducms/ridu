package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// A population selection shapes the populated documents only. Their
// collection's field read rules still see every field, so a rule that reads a
// sibling the caller didn't select can't be bypassed, and each path keeps its
// own selection when several populate the same collection.
func TestPopulationSelectionDoesNotChangeWhatTargetReadRulesObserve(t *testing.T) {
	ctx := context.Background()
	app, err := ridu.New(ridu.Config{Name: "Population rules", Collections: []ridu.Collection{
		{Slug: "authors", Fields: field.Fields{
			// This rule fails open: a missing sibling reads as public.
			field.Text("name").Access(field.Access{Read: func(ctx operation.Context) (bool, error) {
				private, _ := ctx.Siblings.Get("private").BooleanValue()
				return !private, nil
			}}),
			field.Checkbox("private"),
			field.Text("bio"),
		}},
		{Slug: "posts", Fields: field.Fields{
			field.Relationship("author", "authors"),
			field.Relationship("editor", "authors"),
			field.Group("meta", field.Fields{field.Relationship("reviewer", "authors")}),
			field.Blocks("sections", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("source", "authors")}}),
		}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	author := func(name string, private bool) string {
		t.Helper()
		created, err := app.Local().Create(ctx, "authors", store.Values{"name": store.String(name), "private": store.Boolean(private), "bio": store.String("Bio")}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	hidden, shown := author("Hidden", true), author("Shown", false)
	post, err := app.Local().Create(ctx, "posts", store.Values{
		"author":   store.String(hidden),
		"editor":   store.String(shown),
		"meta":     store.Object(store.Values{"reviewer": store.String(hidden)}),
		"sections": store.List(store.Object(store.Values{"blockType": store.String("quote"), "source": store.String(hidden)})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path := func(raw string) query.Path {
		parsed, err := query.ParsePath(raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	name, private := []query.Path{path("name")}, []query.Path{path("private")}
	populations := []query.Population{
		{Path: path("author"), Select: name},
		{Path: path("editor"), Select: name},
		{Path: path("meta.reviewer"), Select: name},
		{Path: path("sections.quote.source"), Select: private},
	}
	found, err := app.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{Populate: populations})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.Local().List(ctx, "posts", ridu.ListOptions{Populate: populations})
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []store.Document{found, listed.Documents[0]} {
		meta, _ := document.Values["meta"].CopyObject()
		sections, _ := document.Values["sections"].CopyList()
		quote, _ := sections[0].CopyObject()
		for _, test := range []struct {
			path  string
			value store.Value
			want  map[string]any
		}{
			// The rule saw private, hid the name, and private itself wasn't selected.
			{"author", document.Values["author"], map[string]any{}},
			{"editor", document.Values["editor"], map[string]any{"name": "Shown"}},
			{"meta.reviewer", meta["reviewer"], map[string]any{}},
			{"sections.quote.source", quote["source"], map[string]any{"private": true}},
		} {
			populated, ok := test.value.CopyDocument()
			if !ok {
				t.Fatalf("%s was not populated: %#v", test.path, test.value)
			}
			names := make([]string, 0, len(populated.Values))
			for name := range populated.Values {
				names = append(names, name)
			}
			slices.Sort(names)
			if len(populated.Values) != len(test.want) {
				t.Fatalf("%s returned %v, want %v", test.path, names, test.want)
			}
			for name, want := range test.want {
				var got any
				if text, ok := populated.Values[name].StringValue(); ok {
					got = text
				} else if boolean, ok := populated.Values[name].BooleanValue(); ok {
					got = boolean
				}
				if got != want {
					t.Fatalf("%s.%s = %#v, want %#v", test.path, name, populated.Values[name], want)
				}
			}
		}
	}
}

// Read rules make Ridu read whole documents, but a selection still decides what
// must be recoverable: a retired block in a field the caller didn't select
// doesn't fail the read, at the root or in a populated document.
func TestSelectedReadsWithReadRulesSkipUnselectedBlockRecovery(t *testing.T) {
	ctx := context.Background()
	backend := teststore.New()
	readable := field.Access{Read: func(operation.Context) (bool, error) { return true, nil }}
	config := ridu.Config{Name: "Retired blocks", Collections: []ridu.Collection{
		{Slug: "pages", Fields: field.Fields{
			field.Text("name").Access(readable),
			field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}, field.Block{Slug: "retired", Fields: field.Fields{field.Text("note")}}),
		}},
		{Slug: "links", Fields: field.Fields{field.Relationship("page", "pages")}},
	}}
	original, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	page, err := original.Local().Create(ctx, "pages", store.Values{"name": store.String("Retired"), "layout": store.List(store.Object(store.Values{"blockType": store.String("retired"), "note": store.String("Old")}))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	link, err := original.Local().Create(ctx, "links", store.Values{"page": store.String(page.ID)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	config.Collections[0].Fields[1] = field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}})
	current, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	name, _ := query.NewPath("name")
	pagePath, _ := query.NewPath("page")
	root, err := current.Local().Find(ctx, "pages", page.ID, ridu.FindOptions{Select: []query.Path{name}})
	if text, _ := root.Values["name"].StringValue(); err != nil || text != "Retired" {
		t.Fatalf("selected root read = %#v, %v", root.Values, err)
	}
	populated, err := current.Local().Find(ctx, "links", link.ID, ridu.FindOptions{Populate: []query.Population{{Path: pagePath, Select: []query.Path{name}}}})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := populated.Values["page"].CopyDocument()
	if text, _ := target.Values["name"].StringValue(); text != "Retired" || len(target.Values) != 1 {
		t.Fatalf("selected populated page = %#v", target.Values)
	}
	// Reading the retired field itself still requires recovery.
	_, err = current.Local().Find(ctx, "links", link.ID, ridu.FindOptions{Populate: []query.Population{{Path: pagePath}}})
	var recovery *ridu.OperationError
	if !errors.As(err, &recovery) || recovery.Code != "block_recovery_required" {
		t.Fatalf("whole populated page = %v, want block recovery", err)
	}
}
