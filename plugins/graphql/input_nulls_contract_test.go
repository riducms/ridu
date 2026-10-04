package graphql_test

import (
	"net/http/httptest"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/store"
)

func TestGraphQLNullableVariablesClearOnlySubmittedFields(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "GraphQL nullable inputs", Plugins: []ridu.Plugin{graphqlplugin.New()}, Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{
			field.Text("caption"),
			field.Text("untouched"),
			field.Group("details", field.Fields{field.Text("summary"), field.Text("optional")}),
			field.Array("rows", field.Fields{field.Text("label"), field.Text("note"), field.Text("optional")}),
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := application.Local()
	created, err := local.Create(t.Context(), "posts", store.Values{
		"caption":   store.String("Original caption"),
		"untouched": store.String("Keep untouched"),
		"details": store.Object(store.Values{
			"summary": store.String("Original summary"), "optional": store.String("Keep optional"),
		}),
		"rows": store.List(store.Object(store.Values{
			"label": store.String("Original row"), "note": store.String("Original note"),
		})),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	mutation := `mutation($id: ID!, $data: PostUpdateInput!) { updatePost(id: $id, data: $data) { id } }`
	cleared := graphQL(t, server.URL, mutation, map[string]interface{}{
		"id": created.ID,
		"data": map[string]interface{}{
			"caption": nil,
			"details": map[string]interface{}{"summary": nil},
			// A singleton object is valid GraphQL input for an array of rows.
			"rows": map[string]interface{}{"label": "One row", "note": nil},
		},
	})
	if cleared["errors"] != nil {
		t.Fatalf("nullable/singleton mutation failed: %#v", cleared)
	}
	read := func() store.Values {
		t.Helper()
		document, findError := local.Find(t.Context(), "posts", created.ID, ridu.FindOptions{})
		if findError != nil {
			t.Fatal(findError)
		}
		return document.Values
	}
	values := read()
	untouched, _ := values["untouched"].StringValue()
	if values["caption"].Kind() != store.ValueNull || untouched != "Keep untouched" {
		t.Fatalf("top-level clear/omission = %#v", values)
	}
	if child, present := values["details"].Lookup("summary"); !present || child.Kind() != store.ValueNull {
		t.Fatalf("nested group clear = %#v", values["details"])
	}
	row, present := values["rows"].ListItem(0)
	if !present || values["rows"].Len() != 1 {
		t.Fatalf("singleton row coercion = %#v", values["rows"])
	}
	if child, present := row.Lookup("note"); !present || child.Kind() != store.ValueNull {
		t.Fatalf("nested array child clear = %#v", row)
	}

	// A missing nullable variable is not an authored clear. Restore the field,
	// then omit the variable entirely from the next GraphQL operation.
	if _, err := local.Update(t.Context(), "posts", created.ID, store.Values{"caption": store.String("Retained caption")}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
	omitted := graphQL(t, server.URL, `mutation($id: ID!, $caption: String) { updatePost(id: $id, data: {caption: $caption}) { id } }`, map[string]interface{}{"id": created.ID})
	retained, _ := read()["caption"].StringValue()
	if omitted["errors"] != nil || retained != "Retained caption" {
		t.Fatalf("omitted variable changed the field: response=%#v values=%#v", omitted, read())
	}

	clearedContainers := graphQL(t, server.URL, mutation, map[string]interface{}{"id": created.ID, "data": map[string]interface{}{"details": nil, "rows": nil}})
	if clearedContainers["errors"] != nil {
		t.Fatalf("nullable group/array clear failed: %#v", clearedContainers)
	}
	values = read()
	if values["details"].Kind() != store.ValueNull || values["rows"].Kind() != store.ValueNull {
		t.Fatalf("container nulls were omitted: %#v", values)
	}
	unknown := graphQL(t, server.URL, mutation, map[string]interface{}{"id": created.ID, "data": map[string]interface{}{"unknown": nil}})
	retained, _ = read()["caption"].StringValue()
	if unknown["errors"] == nil || retained != "Retained caption" {
		t.Fatalf("unknown input key was admitted or changed content: response=%#v values=%#v", unknown, read())
	}
}
