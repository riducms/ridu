package content

import (
	"errors"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestAffordableProductsCombineCallerAndAccessFilters(t *testing.T) {
	app := newQueryApp(t, Products)
	for _, item := range []struct {
		title   string
		price   float64
		inStock bool
		visible bool
	}{
		{"Affordable", 40, true, true},
		{"Expensive", 70, true, true},
		{"Sold out", 30, false, true},
		{"Hidden", 20, true, false},
	} {
		_, err := app.Local().Create(t.Context(), "products", store.Values{
			"title": store.String(item.title), "price": store.Number(item.price),
			"inStock": store.Boolean(item.inStock), "visible": store.Boolean(item.visible),
		}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}

	page, err := FindAffordableProducts(t.Context(), app.Local(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Documents) != 1 {
		t.Fatalf("affordable products = %#v", page)
	}
	if title, _ := page.Documents[0].Values["title"].StringValue(); title != "Affordable" {
		t.Fatalf("unexpected product %q", title)
	}

	// Removing the caller's budget and stock filter does not remove read access.
	page, err = app.Local().List(t.Context(), "products", ridu.ListOptions{})
	if err != nil || page.Total != 3 {
		t.Fatalf("public products = %#v, error = %v", page, err)
	}
}

func TestPathSpellingIsCheckedBeforeSchemaMembership(t *testing.T) {
	group, err := query.NewPath("seo", "title")
	if err != nil || group.String() != "seo.title" {
		t.Fatalf("group path = %s, error = %v", group, err)
	}
	if _, err := query.NewPath("seo.title"); err == nil {
		t.Fatal("NewPath accepted a dotted string as one segment")
	}
	app := newQueryApp(t, Products)
	unknown, err := query.NewPath("unknown")
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().List(t.Context(), "products", ridu.ListOptions{
		Where: query.Equal(unknown, query.String("anything")),
	})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || failure.Code != "bad_query" {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestRepeatedPathsMatchRowsIndependentlyAndKeepBlockTypes(t *testing.T) {
	app := newQueryApp(t, ridu.Collection{
		Slug: "catalog",
		Fields: field.Fields{
			field.Array("variants", field.Fields{
				field.Text("sku"), field.Number("price"),
			}),
			field.Blocks("layout",
				field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}},
				field.Block{Slug: "quote", Fields: field.Fields{field.Text("heading")}},
			),
		},
	})
	_, err := app.Local().Create(t.Context(), "catalog", store.Values{
		"variants": store.List(
			store.Object(store.Values{"sku": store.String("red"), "price": store.Number(30)}),
			store.Object(store.Values{"sku": store.String("blue"), "price": store.Number(5)}),
		),
		"layout": store.List(store.Object(store.Values{
			"blockType": store.String("quote"), "heading": store.String("Sale"),
		})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	path := func(value string) query.Path {
		parsed, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	independentRows, err := query.And(
		query.Equal(path("variants.sku"), query.String("red")),
		query.LessThanEqual(path("variants.price"), query.Number(10)),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		where query.Expression
		want  int
	}{
		{"different rows satisfy And", independentRows, 1},
		{"one equal row excludes NotEqual", query.NotEqual(path("variants.sku"), query.String("red")), 0},
		{"hero path ignores quote", query.Equal(path("layout.hero.heading"), query.String("Sale")), 0},
		{"quote path finds quote", query.Equal(path("layout.quote.heading"), query.String("Sale")), 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := app.Local().List(t.Context(), "catalog", ridu.ListOptions{Where: test.where})
			if err != nil || page.Total != test.want {
				t.Fatalf("total = %d, want %d, error = %v", page.Total, test.want, err)
			}
		})
	}
}

func newQueryApp(t *testing.T, collections ...ridu.Collection) *ridu.App {
	t.Helper()
	config := ridu.Config{Name: "Query examples", Collections: collections}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	return app
}
