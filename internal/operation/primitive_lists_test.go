package operation

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPrimitiveListValueSemantics(t *testing.T) {
	three, one := 3, 1
	zero, ten := 0.0, 10.0
	text := schema.Field{Name: "text", Type: schema.FieldTypeTextList, Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "Text"}, List: &schema.PrimitiveListField{}, Text: &schema.TextField{MaxLength: &three}}
	number := schema.Field{Name: "number", Type: schema.FieldTypeNumberList, Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "Numbers"}, List: &schema.PrimitiveListField{}, Number: &schema.NumberField{Min: &zero, Max: &ten}}
	tests := []struct {
		name       string
		field      schema.Field
		value      store.Value
		missing    bool
		code, item string
	}{
		{name: "omitted", field: text, missing: true}, {name: "null", field: text, value: store.Null()}, {name: "empty", field: text, value: store.List()},
		{name: "duplicates and empty preserved", field: text, value: store.List(store.String(""), store.String("é🙂界"), store.String("é🙂界"))},
		{name: "number zero", field: number, value: store.List(store.Number(0), store.Number(8.5), store.Number(8.5))},
		{name: "too long", field: text, value: store.List(store.String("ok"), store.String("é🙂界a")), code: "max_length", item: "item 2"},
		{name: "too small", field: number, value: store.List(store.Number(-1)), code: "min_value", item: "item 1"},
		{name: "too large", field: number, value: store.List(store.Number(11)), code: "max_value", item: "item 1"},
		{name: "number string", field: number, value: store.List(store.String("0")), code: "invalid_number", item: "item 1"},
		{name: "scalar", field: text, value: store.String("abc"), code: "invalid_type"},
		{name: "object item", field: text, value: store.List(store.Object(store.Values{})), code: "invalid_type", item: "item 1"},
		{name: "null item", field: text, value: store.List(store.String("ok"), store.Null()), code: "invalid_type", item: "item 2"},
		{name: "mixed", field: text, value: store.List(store.String("ok"), store.Number(1)), code: "invalid_type", item: "item 2"},
		{name: "NaN", field: number, value: store.List(store.Number(math.NaN())), code: "invalid_number", item: "item 1"},
		{name: "infinity", field: number, value: store.List(store.Number(math.Inf(1))), code: "invalid_number", item: "item 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := store.Values{}
			if !test.missing {
				values[test.field.Name] = test.value
			}
			got, issues := validate([]schema.Field{test.field}, values, true, nil)
			if test.code == "" {
				if len(issues) != 0 {
					t.Fatalf("issues=%#v", issues)
				}
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(values)
				if string(a) != string(b) {
					t.Fatalf("mutated input %s => %s", b, a)
				}
				return
			}
			if len(issues) != 1 || issues[0].Code != test.code || issues[0].Path != test.field.Name || !strings.Contains(issues[0].Message, test.item) {
				t.Fatalf("issues=%#v", issues)
			}
		})
	}
	text.Text.MinLength = &one
	_, issues := validate([]schema.Field{text}, store.Values{"text": store.List(store.String(""))}, true, nil)
	assertIssue(t, issues, "min_length", "text")
}

func TestPrimitiveListPresenceCountAndDefault(t *testing.T) {
	field := schema.Field{Name: "items", Type: schema.FieldTypeTextList, List: &schema.PrimitiveListField{MinRows: 1, MaxRows: 2}, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: "Items"}}
	for _, values := range []store.Values{{}, {"items": store.Null()}, {"items": store.List()}} {
		_, issues := validate([]schema.Field{field}, values, true, nil)
		assertIssue(t, issues, "min_rows", "items")
	}
	_, issues := validate([]schema.Field{field}, store.Values{"items": store.List(store.String("a"), store.String("a"), store.String("a"))}, true, nil)
	assertIssue(t, issues, "max_rows", "items")
	field.List = &schema.PrimitiveListField{}
	field.Required = true
	for _, values := range []store.Values{{}, {"items": store.Null()}, {"items": store.List()}} {
		_, issues := validate([]schema.Field{field}, values, true, nil)
		assertIssue(t, issues, "required", "items")
	}
	field.Required = false
	literal := `["","oak","oak"]`
	field.Default = &literal
	first, issues := validate([]schema.Field{field}, store.Values{}, true, nil)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	second, _ := validate([]schema.Field{field}, store.Values{}, true, nil)
	items, _ := first["items"].CopyList()
	items[1] = store.String("changed")
	if !reflect.DeepEqual(first, second) {
		t.Fatal("default values aliased")
	}
	explicit, issues := validate([]schema.Field{field}, store.Values{"items": store.Null()}, true, nil)
	if len(issues) != 0 || explicit["items"].Kind() != store.ValueNull {
		t.Fatal("null was defaulted")
	}
}

func TestPrimitiveListIssueLocaleUsesSchemaRatherThanUserKeys(t *testing.T) {
	list := schema.Field{ID: "@locale", Name: "items", Type: schema.FieldTypeTextList}
	rows := schema.Field{ID: "rows", Name: "rows", Type: schema.FieldTypeArray, Localized: true, Nested: &schema.NestedField{Fields: []schema.Field{list}}}
	collection := schema.Collection{ID: "products", Fields: []schema.Field{rows}}
	values := store.Values{"rows": store.Object(store.Values{"fr": store.List(store.Object(store.Values{"_key": store.String("@locale"), "items": store.List(store.Null())}))})}
	issues := []schema.Issue{{Code: "invalid_type", Path: "rows.fr.0.items", Message: "item 1 must be a string"}}
	correlatePrimitiveListIssues(collection, values, Context{Locale: "en", AllLocales: true}, issues)
	if issues[0].Locale != "fr" || issues[0].FieldID != "@locale" || issues[0].Target == "" {
		t.Fatalf("locale/field correlation=%#v", issues)
	}
}
