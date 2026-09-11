package queryaccess

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	fieldoperation "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// A variant name is a discriminator, not an optional object-property lookup.
// A permissive data-shape fallback can reinterpret the public hero path as the
// quote variant's private group, after admission correctly approves the path.
func variantIsolation(t *testing.T, factory Factory, options Options) {
	deny := field.Access{Read: func(fieldoperation.Context) (bool, error) { return false, nil }}
	_, app := factory(t, ridu.Config{Name: "Variant query confidentiality", Collections: []ridu.Collection{{
		Slug: "pages", Fields: field.Fields{
			field.JSON("metadata"), field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("secret"), field.JSON("metadata")}}, field.Block{Slug: "quote", Fields: field.Fields{field.Group("hero", field.Fields{field.Text("secret").Access(deny), field.JSON("metadata").Access(deny)})}}),
		},
	}}})
	_, err := app.Local().Create(t.Context(), "pages", store.Values{
		"metadata": store.Object(store.Values{"tag": store.String("visible")}),
		"layout": store.List(store.Object(store.Values{
			"blockType": store.String("quote"), "hero": store.Object(store.Values{
				"secret": store.String("hidden-token"), "metadata": store.Object(store.Values{"tag": store.String("hidden-json-token")}),
			}),
		})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Create(t.Context(), "pages", store.Values{
		"layout": store.List(store.Object(store.Values{
			"blockType": store.String("hero"), "secret": store.String("public-token"),
			"metadata": store.Object(store.Values{"tag": store.String("public-json-token")}),
		})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := app.Local().List(t.Context(), "pages", ridu.ListOptions{Where: query.Contains(path("layout.hero.secret"), "hidden-token")})
	if err != nil || page.Total != 0 || len(page.Documents) != 0 {
		t.Fatalf("public variant matched a private variant's shape: %#v, %v", page, err)
	}
	response := request(app.Handler(ridu.HandlerOptions{}), http.MethodGet, "/api/collections/pages/count?where="+url.QueryEscape(`{"layout.hero.secret":{"contains":"hidden-token"}}`), "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"totalDocs":0`) {
		t.Fatalf("public variant count inferred private data: %d %s", response.Code, response.Body.String())
	}
	_, err = app.Local().List(t.Context(), "pages", ridu.ListOptions{Where: query.Contains(path("layout.quote.hero.secret"), "hidden-token")})
	denied(t, err, "layout.quote.hero.secret")
	_, err = app.Local().List(t.Context(), "pages", ridu.ListOptions{Where: query.Equal(path("layout.quote.hero.metadata.tag"), query.String("hidden-json-token"))})
	denied(t, err, "layout.quote.hero.metadata.tag")
	if !options.OpaqueJSONDescendants {
		return
	}
	// Opaque JSON has no authored child permissions or variant discriminator;
	// its existing Local API descendant queries must remain usable.
	page, err = app.Local().List(t.Context(), "pages", ridu.ListOptions{Where: query.Equal(path("metadata.tag"), query.String("visible"))})
	if err != nil || page.Total != 1 || len(page.Documents) != 1 {
		t.Fatalf("opaque JSON descendant query: %#v, %v", page, err)
	}
	// Resolve the managed Block prefix using its schema discriminator before
	// looking inside opaque JSON. Falling back to the full raw path here would
	// make the quote variant's private hero.metadata look like public content.
	for token, total := range map[string]int{"hidden-json-token": 0, "public-json-token": 1} {
		page, err = app.Local().List(t.Context(), "pages", ridu.ListOptions{Where: query.Equal(path("layout.hero.metadata.tag"), query.String(token))})
		if err != nil || page.Total != total || len(page.Documents) != total {
			t.Fatalf("variant opaque JSON descendant %q: %#v, %v", token, page, err)
		}
	}
}
