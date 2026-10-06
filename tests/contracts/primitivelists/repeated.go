package primitivelists

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

// RepeatedConfig adds list placements beneath two arrays and inside a
// localized group to Config.
func RepeatedConfig() core.Config {
	config := Config()
	config.Collections[0].Fields = append(config.Collections[0].Fields,
		field.Array("sections", field.Fields{field.Array("links", field.Fields{field.TextList("labels")})}),
		field.Group("localizedDetails", field.Fields{field.TextList("points")}).Localized(),
	)
	return config
}

// ExerciseRepeatedQueries checks list membership through arrays, blocks and a
// localized group, directly and negated, on an application built from
// RepeatedConfig. Each row of a repeated level is considered separately and
// block rows bind to their block type.
func ExerciseRepeatedQueries(t *testing.T, app *core.App) {
	t.Helper()
	nested := func(label string) store.Value {
		link := store.Object(store.Values{"_key": store.String("link-" + label), "labels": store.List(store.String(label))})
		return store.List(store.Object(store.Values{"_key": store.String("section-" + label), "links": store.List(link)}))
	}
	values := Values()
	values["sections"] = nested("nested")
	values["localizedDetails"] = store.Object(store.Values{"points": store.List(store.String("Oak"))})
	first, err := app.Local().Create(t.Context(), "primitive-products", values, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	decoy := Values()
	decoy["variants"] = store.List(store.Object(store.Values{"_key": store.String("B"), "points": store.List(store.String("Pine")), "sizes": store.List(store.Number(99))}))
	decoy["content"] = store.List(store.Object(store.Values{"_key": store.String("B"), "blockType": store.String("note"), "points": store.List(store.String("Oak")), "sizes": store.List(store.Number(0))}))
	decoy["sections"] = nested("other")
	decoy["localizedDetails"] = store.Object(store.Values{"points": store.List(store.String("Pine"))})
	second, err := app.Local().Create(t.Context(), "primitive-products", decoy, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Handler(core.HandlerOptions{})
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
		{"localizedDetails.points", query.String("Oak"), first.ID},
	} {
		path, _ := query.ParsePath(test.path)
		for _, negate := range []bool{false, true} {
			expression := query.In(path, test.item)
			want := test.wantID
			if negate {
				expression = query.Not(expression)
				want = first.ID
				if test.wantID == first.ID {
					want = second.ID
				}
			}
			page, err := app.Local().List(t.Context(), "primitive-products", core.ListOptions{Where: expression})
			if err != nil || *page.Total != 1 || len(page.Documents) != 1 || page.Documents[0].ID != want {
				t.Fatalf("path %s negate=%v page=%#v: %v", test.path, negate, page, err)
			}
		}
		operand, _ := json.Marshal(test.item)
		where := fmt.Sprintf(`{%q:{"in":[%s]}}`, test.path, operand)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/collections/primitive-products/count?where="+url.QueryEscape(where), nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"totalDocs":1`) {
			t.Fatalf("REST count %s: %d %s", test.path, response.Code, response.Body.String())
		}
	}
}
