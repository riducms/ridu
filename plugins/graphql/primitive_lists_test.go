package graphql_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/store"
)

func TestGraphQLPrimitiveListsKeepStrictValueShapes(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "GraphQL lists", Plugins: []ridu.Plugin{graphqlplugin.New()}, Collections: []ridu.Collection{{Slug: "products", Fields: field.Fields{
		field.TextList("points").MinRows(1), field.NumberList("sizes").Min(0),
		field.Group("details", field.Fields{field.TextList("points")}),
		field.Array("variants", field.Fields{field.NumberList("sizes")}),
	}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	valid := graphQL(t, server.URL, `mutation($data: ProductCreateInput!){createProduct(data:$data){id points sizes}}`, map[string]interface{}{"data": map[string]interface{}{"points": []interface{}{"Oak", "Oak"}, "sizes": []interface{}{0, 10, 10}}})
	created := objectAt(t, valid, "data", "createProduct")
	if !reflect.DeepEqual(created["points"], []interface{}{"Oak", "Oak"}) || !reflect.DeepEqual(created["sizes"], []interface{}{float64(0), float64(10), float64(10)}) {
		t.Fatalf("round trip: %#v", valid)
	}
	for _, data := range []map[string]interface{}{
		{"points": "Oak"}, {"points": []interface{}{12}}, {"points": []interface{}{nil}},
		{"points": []interface{}{"Oak"}, "sizes": "12"}, {"points": []interface{}{"Oak"}, "sizes": []interface{}{"12"}},
		{"points": []interface{}{"Oak"}, "sizes": []interface{}{false}},
		{"points": []interface{}{"Oak"}, "details": map[string]interface{}{"points": "Oak"}},
		{"points": []interface{}{"Oak"}, "variants": []interface{}{map[string]interface{}{"sizes": 12}}},
	} {
		response := graphQL(t, server.URL, `mutation($data:ProductCreateInput!){createProduct(data:$data){id}}`, map[string]interface{}{"data": data})
		if response["errors"] == nil {
			t.Errorf("coerced invalid values %#v: %#v", data, response)
		}
	}
	for _, query := range []string{
		`mutation {createProduct(data:{points:"Oak"}){id}}`,
		`mutation($points:[String!]!){createProduct(data:{points:$points}){id}}`,
	} {
		if response := graphQL(t, server.URL, query, map[string]interface{}{"points": "Oak"}); response["errors"] == nil {
			t.Errorf("coerced singleton %s: %#v", query, response)
		}
	}
	for _, query := range []string{
		`mutation($points:[String!] = "Oak") {createProduct(data:{points:$points}){id}}`,
		`mutation($data:ProductCreateInput = {points:"Oak"}) {createProduct(data:$data){id}}`,
		`mutation {createProduct(data:{points:["Oak"],sizes:12}){id}}`,
	} {
		if response := graphQL(t, server.URL, query); response["errors"] == nil {
			t.Errorf("coerced literal or variable default %s: %#v", query, response)
		}
	}
	validDefaults := graphQL(t, server.URL, `mutation($points:[String!] = ["Oak","Oak"], $sizes:[Float!] = [0,10]) {createProduct(data:{points:$points,sizes:$sizes}){points sizes}}`)
	if validDefaults["errors"] != nil {
		t.Fatalf("explicit array defaults rejected: %#v", validDefaults)
	}
	updated := graphQL(t, server.URL, fmt.Sprintf(`mutation{updateProduct(id:%q,data:{sizes:[12,0]}){sizes}}`, created["id"]))
	if !reflect.DeepEqual(objectAt(t, updated, "data", "updateProduct")["sizes"], []interface{}{float64(12), float64(0)}) {
		t.Fatal(updated)
	}
	read, err := app.Local().Find(context.Background(), "products", created["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read.Values["sizes"], store.List(store.Number(12), store.Number(0))) {
		t.Fatal(read.Values)
	}
}

func TestGraphQLPrimitiveListQueriesExposeMembershipOnly(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "List queries", Plugins: []ridu.Plugin{graphqlplugin.New()}, Collections: []ridu.Collection{{Slug: "products", Fields: field.Fields{field.TextList("points"), field.NumberList("sizes")}}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range []store.Values{{"points": store.List(store.String("Oak"), store.String("Oak")), "sizes": store.List(store.Number(0), store.Number(10))}, {"points": store.List(store.String("Oak veneer")), "sizes": store.List(store.Number(12))}} {
		if _, err := app.Local().Create(context.Background(), "products", values, nil); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	for _, where := range []string{`points:{in:["Oak"]}`, `sizes:{in:[0]}`, `points:{not_in:["Oak"]}`} {
		result := graphQL(t, server.URL, `{Products(where:{`+where+`}){totalDocs}}`)
		if objectAt(t, result, "data", "Products")["totalDocs"] != float64(1) {
			t.Fatalf("%s: %#v", where, result)
		}
	}
	for _, where := range []string{`points:{equals:"Oak"}`, `points:{contains:"Oak"}`, `sizes:{greater_than:0}`} {
		result := graphQL(t, server.URL, `{Products(where:{`+where+`}){totalDocs}}`)
		if result["errors"] == nil {
			t.Fatal(result)
		}
	}
	sdl, err := graphqlplugin.GenerateSDL(app.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ProductPointsWhere", "ProductSizesWhere"} {
		start := strings.Index(sdl, "input "+name+" {")
		if start < 0 {
			t.Fatal(name)
		}
		section := sdl[start:]
		section = section[:strings.Index(section, "}")]
		for _, operator := range []string{"equals", "contains", "greater_than"} {
			if strings.Contains(section, operator) {
				t.Fatalf("%s advertises %s", name, operator)
			}
		}
	}
}
