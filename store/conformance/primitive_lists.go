package conformance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func runPrimitiveLists(t *testing.T, factory Factory) {
	snapshot := conformanceManifest().Snapshot()
	text := schema.Field{ID: "list-text", Name: "texts", Path: mustPath("texts"), Type: schema.FieldTypeTextList, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, List: &schema.PrimitiveListField{}, Admin: schema.FieldAdmin{Label: "Texts"}}
	number := schema.Field{ID: "list-number", Name: "numbers", Path: mustPath("numbers"), Type: schema.FieldTypeNumberList, Category: schema.FieldCategoryScalar, Number: &schema.NumberField{}, List: &schema.PrimitiveListField{}, Admin: schema.FieldAdmin{Label: "Numbers"}}
	localized := text
	localized.ID = "list-localized"
	localized.Name = "localizedList"
	localized.Path = mustPath("localizedList")
	localized.Localized = true
	child := text
	child.ID = "list-group-child"
	child.Path = mustPath("details.texts")
	group := schema.Field{ID: "list-group", Name: "details", Path: mustPath("details"), Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Nested: &schema.NestedField{Fields: []schema.Field{child}}, Admin: schema.FieldAdmin{Label: "Details"}}
	for index := range snapshot.Collections {
		if snapshot.Collections[index].Slug == "records" {
			snapshot.Collections[index].Fields = append(snapshot.Collections[index].Fields, text, number, localized, group)
		}
	}
	fixture := openFixture(t, factory, schema.NewManifest(snapshot))
	first := recordValues("public", 1)
	first["texts"] = store.List(store.String(""), store.String("oak"), store.String("oak"), store.String("$literal"))
	first["numbers"] = store.List(store.Number(0), store.Number(8.5), store.Number(8.5), store.Number(-1))
	first["localizedList"] = store.Object(store.Values{"en": store.List(store.String("English")), "fr": store.List()})
	first["details"] = store.Object(store.Values{"texts": store.List(store.String("nested"), store.String("nested"))})
	transaction := begin(t, fixture.backend)
	original, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: id(1), Values: first}))
	if err != nil {
		rollback(t, transaction)
		t.Fatal(err)
	}
	versions := transaction.(store.VersionTransaction)
	if _, err := versions.SaveVersion(t.Context(), fixture.records, original, 10); err != nil {
		rollback(t, transaction)
		t.Fatal(err)
	}
	for index, value := range []store.Value{store.List(), store.Null(), {}} {
		values := recordValues("public", float64(index+2))
		if index < 2 {
			values["texts"] = value
		}
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: id(index + 2), Values: values})); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
	}
	commit(t, transaction)
	read := begin(t, fixture.backend)
	found, err := read.Find(t.Context(), fixture.request(store.Request{Collection: fixture.records, ID: id(1), AllLocales: true}))
	rollback(t, read)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"texts", "numbers", "localizedList", "details"} {
		primitiveListValueEqual(t, found.Values[name], first[name])
	}
	textPath := mustPath("texts")
	cases := []struct {
		name       string
		expression query.Expression
		chain      []schema.LocaleCode
		ids        []string
	}{
		{"membership", query.In(textPath, query.String("oak")), nil, []string{id(1)}},
		{"duplicate membership", query.In(textPath, query.String("oak"), query.String("oak")), nil, []string{id(1)}},
		{"empty string", query.In(textPath, query.String("")), nil, []string{id(1)}},
		{"literal query string", query.In(textPath, query.String("$literal")), nil, []string{id(1)}},
		{"number zero", query.In(mustPath("numbers"), query.Number(0)), nil, []string{id(1)}},
		{"number exact", query.In(mustPath("numbers"), query.Number(8.5)), nil, []string{id(1)}},
		{"negative membership", listNot(query.In(textPath, query.String("oak"))), nil, []string{id(2), id(3), id(4)}},
		{"empty operands", query.In(textPath), nil, nil},
		{"exists includes empty", listExists(textPath, true), nil, []string{id(1), id(2)}},
		{"absent", listExists(textPath, false), nil, []string{id(3), id(4)}},
		{"null equality", query.Equal(textPath, query.Null()), nil, []string{id(3), id(4)}},
		{"null inequality", query.NotEqual(textPath, query.Null()), nil, []string{id(1), id(2)}},
		{"group membership", query.In(mustPath("details.texts"), query.String("nested")), nil, []string{id(1)}},
		{"locale exact", query.In(mustPath("localizedList"), query.String("English")), []schema.LocaleCode{"en"}, []string{id(1)}},
		{"empty locale blocks fallback", query.In(mustPath("localizedList"), query.String("English")), []schema.LocaleCode{"fr", "en"}, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			node := test.expression.Node()
			request := fixture.request(store.Request{Collection: fixture.records, Filter: &node, Page: 1, Limit: 10, LocaleChain: test.chain})
			read := begin(t, fixture.backend)
			page, err := read.List(t.Context(), request)
			rollback(t, read)
			if err != nil {
				t.Fatal(err)
			}
			assertDocumentIDs(t, page.Documents, test.ids...)
			if page.Total != len(test.ids) {
				t.Fatalf("total %d, want %d", page.Total, len(test.ids))
			}
			request.Filter = nil
			request.Access = &node
			read = begin(t, fixture.backend)
			page, err = read.List(t.Context(), request)
			rollback(t, read)
			if err != nil {
				t.Fatal(err)
			}
			assertDocumentIDs(t, page.Documents, test.ids...)
		})
	}
	for _, expression := range []query.Expression{query.Equal(textPath, query.String("oak")), query.NotEqual(textPath, query.String("oak")), query.Contains(textPath, "oak"), query.Like(textPath, "oak"), query.GreaterThan(mustPath("numbers"), query.Number(0)), query.In(textPath, query.Number(1)), query.In(textPath, query.Null())} {
		node := expression.Node()
		read := begin(t, fixture.backend)
		_, err := read.List(t.Context(), fixture.request(store.Request{Collection: fixture.records, Filter: &node}))
		rollback(t, read)
		if err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("unsupported list query error = %v", err)
		}
	}
	order, _ := query.NewSort(textPath, query.Ascending)
	read = begin(t, fixture.backend)
	_, err = read.List(t.Context(), fixture.request(store.Request{Collection: fixture.records, Sort: []query.Sort{order}}))
	rollback(t, read)
	if err == nil {
		t.Fatal("list sort accepted")
	}
	transaction = begin(t, fixture.backend)
	updated, err := transaction.Update(t.Context(), fixture.updateRequest(store.UpdateRequest{Request: store.Request{Collection: fixture.records, ID: id(1)}, Values: store.Values{"texts": store.List(store.String("replacement")), "numbers": store.List()}}))
	if err != nil {
		rollback(t, transaction)
		t.Fatal(err)
	}
	primitiveListValueEqual(t, updated.Values["texts"], store.List(store.String("replacement")))
	primitiveListValueEqual(t, updated.Values["numbers"], store.List())
	commit(t, transaction)
	read = begin(t, fixture.backend)
	history, err := read.(store.VersionTransaction).ListVersions(t.Context(), fixture.versionRequest(store.VersionRequest{Collection: fixture.records, DocumentID: id(1), AllLocales: true}))
	rollback(t, read)
	if err != nil || len(history) != 1 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	primitiveListValueEqual(t, history[0].Snapshot.Values["texts"], first["texts"])
}

func primitiveListValueEqual(t *testing.T, actual, want store.Value) {
	t.Helper()
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(want)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("value %s, want %s", a, b)
	}
}

func listNot(expression query.Expression) query.Expression {
	result, _ := query.Not(expression)
	return result
}
func listExists(path query.Path, value bool) query.Expression {
	result, _ := query.Compare(path, query.OperatorExists, query.Boolean(value))
	return result
}
