package graphql_test

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
)

// graphql-go parses JSON literals without variable values, so a variable nested
// in one would be stored as null. The request must fail instead, while whole
// JSON variables, including their nulls, keep working.
func TestGraphQLRejectsVariablesInsideJSONLiterals(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "JSON literals", Plugins: []ridu.Plugin{graphqlplugin.New()}, Collections: []ridu.Collection{{
		Slug: "pages", Fields: field.Fields{
			field.Text("title"), field.JSON("metadata"),
			field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{field.Text("caption")}}),
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()

	rejected := graphQL(t, server.URL, `mutation($caption: String, $note: String) {
  createPage(data: {title: "Nested", layout: [{blockType: "card", caption: $caption}], metadata: {notes: [$note]}}) { id }
}`, map[string]interface{}{"caption": "Via variable", "note": "Also dropped"})
	errors, _ := rejected["errors"].([]interface{})
	var messages []string
	for _, item := range errors {
		message, _ := item.(map[string]interface{})["message"].(string)
		messages = append(messages, message)
	}
	joined := strings.Join(messages, "\n")
	if rejected["data"] != nil || len(errors) != 2 || !strings.Contains(joined, `"$caption"`) || !strings.Contains(joined, `"$note"`) || !strings.Contains(joined, "pass the whole JSON value as a variable") {
		t.Fatalf("nested JSON variables were not rejected: %#v", rejected)
	}
	if page, err := app.Local().List(context.Background(), "pages", ridu.ListOptions{}); err != nil {
		t.Fatal(err)
	} else if *page.Total != 0 {
		t.Fatalf("rejected mutation stored %d pages", *page.Total)
	}

	accepted := graphQL(t, server.URL, `mutation($layout: JSON, $metadata: JSON) {
  createPage(data: {title: "Whole", layout: $layout, metadata: $metadata}) {
    layout { ... on CardBlock { caption } }
    metadata
  }
}`, map[string]interface{}{
		"layout":   []interface{}{map[string]interface{}{"blockType": "card", "caption": "Via variable"}},
		"metadata": map[string]interface{}{"note": nil},
	})
	created := objectAt(t, accepted, "data", "createPage")
	layout, _ := created["layout"].([]interface{})
	if len(layout) != 1 || layout[0].(map[string]interface{})["caption"] != "Via variable" || !reflect.DeepEqual(created["metadata"], map[string]interface{}{"note": nil}) {
		t.Fatalf("whole JSON variables changed: %#v", accepted)
	}
}
