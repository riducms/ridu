package postgres_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListsPostgresLifecycle(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run isolated PostgreSQL primitive-list lifecycle")
	}
	config := primitivelists.Config()
	config.Collections[0].Access.Publish = config.Collections[0].Access.Update
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := openPostgresConformanceStore(t, databaseURL, manifest)
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	primitivelists.Exercise(t, app)
}

func TestPrimitiveListsPostgresRepeatedQueryBoundary(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run isolated PostgreSQL primitive-list repeated queries")
	}
	config := primitivelists.Config()
	config.Collections[0].Fields = append(config.Collections[0].Fields, field.Array("sections", field.Fields{field.Array("links", field.Fields{field.TextList("labels")})}))
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := openPostgresConformanceStore(t, databaseURL, manifest)
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.Local().Create(t.Context(), "primitive-products", primitivelists.Values(), nil)
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
	path, _ := query.ParsePath("sections.links.labels")
	_, err = app.Local().List(t.Context(), "primitive-products", core.ListOptions{Where: query.In(path, query.String("nested"))})
	var failure *core.OperationError
	if !errors.As(err, &failure) || failure.Code != "bad_query" || failure.Status != 400 || len(failure.Issues) != 1 || failure.Issues[0].Code != "unsupported_path" || failure.Issues[0].Path != path.String() || !strings.Contains(err.Error(), "unsupported repeated fields") {
		t.Fatalf("nested repeated query must reject explicitly: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/collections/primitive-products?where="+url.QueryEscape(`{"sections.links.labels":{"in":["nested"]}}`), nil)
	app.Handler(core.HandlerOptions{}).ServeHTTP(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"bad_request"`) || !strings.Contains(response.Body.String(), `"path":"sections.links.labels"`) || !strings.Contains(response.Body.String(), `"code":"unsupported_path"`) {
		t.Fatalf("REST repeated query %d: %s", response.Code, response.Body.String())
	}
}

func TestPrimitiveListsPostgresDefaultColumns(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run isolated PostgreSQL primitive-list default columns")
	}
	config := core.Config{Name: "List defaults", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{field.TextList("points").Default("oak", "oak"), field.NumberList("sizes").Default(0, 0)}}}}
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend := openPostgresConformanceStore(t, databaseURL, manifest)
	app, err := core.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.Local().Create(t.Context(), "products", store.Values{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	points, _ := created.Values["points"].CopyList()
	sizes, _ := created.Values["sizes"].CopyList()
	if len(points) != 2 || len(sizes) != 2 {
		t.Fatalf("defaults %#v", created.Values)
	}
}
