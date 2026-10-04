package graphql

import (
	"context"
	"reflect"
	"testing"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestValuesArgDistinguishesOmittedVariableFromExplicitAndDefaultNull(t *testing.T) {
	fields := []schema.Field{{Name: "title", Type: schema.FieldTypeText}, {Name: "caption", Type: schema.FieldTypeText}}
	for _, test := range []struct {
		name      string
		query     string
		variables map[string]interface{}
		wantNull  bool
		wantText  string
	}{
		{name: "omitted", query: `mutation($caption: String) { updatePost(data: {title: "Keep", caption: $caption}) { id } }`, variables: map[string]interface{}{}},
		{name: "explicit null", query: `mutation($caption: String) { updatePost(data: {title: "Keep", caption: $caption}) { id } }`, variables: map[string]interface{}{"caption": nil}, wantNull: true},
		{name: "default value", query: `mutation($caption: String = "Default caption") { updatePost(data: {title: "Keep", caption: $caption}) { id } }`, variables: map[string]interface{}{}, wantText: "Default caption"},
	} {
		t.Run(test.name, func(t *testing.T) {
			coerced := map[string]interface{}{"title": "Keep"}
			if test.wantText != "" {
				coerced["caption"] = test.wantText
			}
			params := inputParams(t, test.query, test.variables, map[string]interface{}{"data": coerced})
			values := valuesArg(params, "data", fields)
			if value, exists := values["caption"]; test.wantText != "" {
				if text, valid := value.StringValue(); !exists || !valid || text != test.wantText {
					t.Fatalf("caption default = %t/%#v, want %q; values=%#v", exists, value, test.wantText, values)
				}
			} else if exists != test.wantNull || exists && value.Kind() != store.ValueNull {
				t.Fatalf("caption presence/null = %t/%#v, want null=%t; values=%#v", exists, value, test.wantNull, values)
			}
			if value, _ := values["title"].StringValue(); value != "Keep" {
				t.Fatalf("coerced title was lost: %#v", values)
			}
		})
	}
}

func TestValuesArgRestoresNestedNullsWithoutUnknownOrOmittedFields(t *testing.T) {
	children := []schema.Field{{Name: "summary", Type: schema.FieldTypeText}, {Name: "optional", Type: schema.FieldTypeText}}
	rows := []schema.Field{{Name: "label", Type: schema.FieldTypeText}, {Name: "note", Type: schema.FieldTypeText}, {Name: "optional", Type: schema.FieldTypeText}}
	fields := []schema.Field{
		{Name: "caption", Type: schema.FieldTypeText},
		{Name: "untouched", Type: schema.FieldTypeText},
		{Name: "details", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: children}},
		{Name: "rows", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: rows}},
	}
	// The raw variable is a singleton row; graphql-go's typed argument has
	// already coerced it into a one-item list and dropped nullable nulls.
	raw := map[string]interface{}{
		"caption": nil,
		"details": map[string]interface{}{"summary": nil, "unknown": nil},
		"rows":    map[string]interface{}{"label": "One row", "note": nil, "unknown": nil},
		"unknown": nil,
	}
	coerced := map[string]interface{}{
		"details": map[string]interface{}{},
		"rows":    []interface{}{map[string]interface{}{"label": "One row"}},
	}
	params := inputParams(t, `mutation($data: PostUpdateInput!) { updatePost(data: $data) { id } }`, map[string]interface{}{"data": raw}, map[string]interface{}{"data": coerced})
	got := valuesArg(params, "data", fields)
	want := store.Values{
		"caption": store.Null(),
		"details": store.Object(store.Values{"summary": store.Null()}),
		"rows": store.List(store.Object(store.Values{
			"label": store.String("One row"), "note": store.Null(),
		})),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized input = %#v, want %#v", got, want)
	}
	if _, exists := got["untouched"]; exists {
		t.Fatalf("omitted top-level field was injected: %#v", got)
	}
}

func inputParams(t *testing.T, source string, variables, args map[string]interface{}) enginegraphql.ResolveParams {
	t.Helper()
	document, err := parser.Parse(parser.ParseParams{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	operation, ok := document.Definitions[0].(*ast.OperationDefinition)
	if !ok {
		t.Fatalf("operation type = %T", document.Definitions[0])
	}
	field, ok := operation.SelectionSet.Selections[0].(*ast.Field)
	if !ok {
		t.Fatalf("root selection type = %T", operation.SelectionSet.Selections[0])
	}
	return enginegraphql.ResolveParams{
		Context: context.WithValue(context.Background(), requestStateKey{}, requestState{rawVariables: variables}),
		Args:    args,
		Info:    enginegraphql.ResolveInfo{Operation: operation, FieldASTs: []*ast.Field{field}},
	}
}
