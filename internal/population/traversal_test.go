package population_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestVisitAndMapAtPathShareLocaleSelection(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   store.Value
		locales population.LocaleSelection
		want    []store.Value
	}{
		{
			name: "all includes null and empty in sorted order",
			value: store.Object(store.Values{
				"fr": store.String("French"), "en": store.Null(), "de": store.String(""),
			}),
			locales: population.LocaleSelection{All: true},
			want:    []store.Value{store.String(""), store.Null(), store.String("French")},
		},
		{
			name: "chain skips absent null and nonfinal empty",
			value: store.Object(store.Values{
				"fr": store.Null(), "en": store.String(""), "de": store.String("German"),
			}),
			locales: population.LocaleSelection{Chain: []schema.LocaleCode{"es", "fr", "en", "de"}},
			want:    []store.Value{store.String("German")},
		},
		{
			name:    "final empty is visible",
			value:   store.Object(store.Values{"en": store.String("")}),
			locales: population.LocaleSelection{Chain: []schema.LocaleCode{"fr", "en"}},
			want:    []store.Value{store.String("")},
		},
		{
			name:    "nonobject locale map is not visible",
			value:   store.String("malformed"),
			locales: population.LocaleSelection{All: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := []schema.Field{{Name: "title", Type: schema.FieldTypeText, Localized: true}}
			values := store.Values{"title": test.value}
			path := mustPath(t, "title")
			var observed, transformed []store.Value
			visitor := func(field schema.Field, value store.Value) {
				if field.Localized {
					t.Fatal("callback field still localized")
				}
				observed = append(observed, value)
			}
			visited := population.VisitAtPath(fields, values, path, test.locales, visitor)
			mapped, matched := population.MapAtPath(fields, values, path, test.locales, func(field schema.Field, value store.Value) store.Value {
				if field.Localized {
					t.Fatal("callback field still localized")
				}
				transformed = append(transformed, value)
				return store.String("updated")
			})
			if visited != (len(test.want) != 0) || matched != visited || !reflect.DeepEqual(observed, test.want) || !reflect.DeepEqual(transformed, observed) {
				t.Fatalf("visited=%v matched=%v observed=%#v transformed=%#v want=%#v", visited, matched, observed, transformed, test.want)
			}
			mapped["title"] = store.Null()
			if !reflect.DeepEqual(values["title"], test.value) {
				t.Fatal("mapping or editing returned root changed the input")
			}
		})
	}
}

func TestVisitAtPathDoesNotFallbackAfterSelectingMalformedContainer(t *testing.T) {
	fields := []schema.Field{{Name: "meta", Type: schema.FieldTypeGroup, Localized: true, Nested: &schema.NestedField{Fields: []schema.Field{{Name: "author", Type: schema.FieldTypeText}}}}}
	values := store.Values{"meta": store.Object(store.Values{
		"fr": store.String("malformed"), "en": store.Object(store.Values{"author": store.String("English")}),
	})}
	path := mustPath(t, "meta", "author")
	locales := population.LocaleSelection{Chain: []schema.LocaleCode{"fr", "en"}}
	if population.VisitAtPath(fields, values, path, locales, func(schema.Field, store.Value) { t.Fatal("visited fallback after selecting French") }) {
		t.Fatal("malformed selected container matched")
	}
	if _, matched := population.MapAtPath(fields, values, path, locales, func(schema.Field, store.Value) store.Value {
		t.Fatal("mapped fallback after selecting French")
		return store.Null()
	}); matched {
		t.Fatal("malformed selected container matched")
	}
}

func TestVisitAtPathRetainsSnapshotsAndSkipsUnmatchedRows(t *testing.T) {
	fields := []schema.Field{{Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{
		Slug: "card", Fields: []schema.Field{{Name: "payload", Type: schema.FieldTypeGroup}},
	}}}}}
	payload := func(title string) store.Value { return store.Object(store.Values{"title": store.String(title)}) }
	block := func(kind, title string) store.Value {
		return store.Object(store.Values{"blockType": store.String(kind), "payload": payload(title)})
	}
	values := store.Values{"layout": store.List(store.Null(), block("card", "one"), block("other", "ignored"), block("card", "two"))}
	path := mustPath(t, "layout", "card", "payload")
	var retained []store.Value
	if !population.VisitAtPath(fields, values, path, population.LocaleSelection{}, func(_ schema.Field, value store.Value) {
		retained = append(retained, value)
		copy, _ := value.CopyObject()
		copy["title"] = store.String("edited visitor copy")
	}) {
		t.Fatal("path did not match")
	}
	mapped, matched := population.MapAtPath(fields, values, path, population.LocaleSelection{}, func(_ schema.Field, value store.Value) store.Value {
		copy, _ := value.CopyObject()
		copy["title"] = store.String("updated")
		return store.Object(copy)
	})
	if !matched || !reflect.DeepEqual(retained, []store.Value{payload("one"), payload("two")}) {
		t.Fatalf("matched=%v retained=%#v", matched, retained)
	}
	original, _ := values["layout"].ListItem(1)
	updated, _ := mapped["layout"].ListItem(1)
	if title, _ := original.Get("payload").Get("title").StringValue(); title != "one" {
		t.Fatalf("input title changed to %q", title)
	}
	if title, _ := updated.Get("payload").Get("title").StringValue(); title != "updated" {
		t.Fatalf("mapped title = %q", title)
	}
	ignored, _ := mapped["layout"].ListItem(2)
	if !reflect.DeepEqual(ignored, block("other", "ignored")) {
		t.Fatal("unmatched block changed")
	}
}

func TestVisitAndMapEmbeddedPathPreserveStreamingErrors(t *testing.T) {
	field, node := embeddedPopulationFixture()
	path := mustPath(t, "content", "cards", "widget", "card", "author")
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "later malformed node"}[invalid], func(t *testing.T) {
			second := node("two", "second")
			if invalid {
				second = store.Number(3)
			}
			input := store.Object(store.Values{"outline": store.List(node("one", "first"), second)})
			values := store.Values{"content": input}
			var observed, transformed []string
			visited := population.VisitAtPath([]schema.Field{field}, values, path, population.LocaleSelection{}, func(_ schema.Field, value store.Value) {
				text, _ := value.StringValue()
				observed = append(observed, text)
			})
			mapped, matched := population.MapAtPath([]schema.Field{field}, values, path, population.LocaleSelection{}, func(_ schema.Field, value store.Value) store.Value {
				text, _ := value.StringValue()
				transformed = append(transformed, text)
				return store.String("updated")
			})
			want := []string{"first", "second"}
			if invalid {
				want = want[:1]
				if !reflect.DeepEqual(mapped["content"], input) {
					t.Fatal("partially mapped embedded value escaped after later error")
				}
			}
			if visited != !invalid || matched != visited || !reflect.DeepEqual(observed, want) || !reflect.DeepEqual(transformed, want) {
				t.Fatalf("visited=%v matched=%v observed=%v transformed=%v", visited, matched, observed, transformed)
			}
			if !reflect.DeepEqual(values["content"], input) {
				t.Fatal("embedded input mutated")
			}
		})
	}
}

func TestMapPopulatedDocumentsKeepsDetachedCallbackAndUnpopulatedBranches(t *testing.T) {
	field := schema.Field{Name: "links", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{
		HasMany: true, Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: "people-id", CollectionSlug: "people"}},
	}}
	reference := func(slug string, id store.Value) store.Value {
		return store.Object(store.Values{"relationTo": store.String(slug), "id": id, "extra": store.String("kept")})
	}
	document := store.Populated(store.Document{ID: "one", Values: store.Values{"secret": store.String("private")}})
	unknown := reference("unknown", document)
	unpopulated := reference("people", store.String("two"))
	values := store.Values{"links": store.List(reference("people", document), unknown, unpopulated, store.Null())}
	var retained store.Document
	calls := 0
	mapped := population.MapPopulatedDocuments([]schema.Field{field}, values, false, func(target schema.StableID, locale schema.LocaleCode, doc store.Document) store.Document {
		calls++
		if target != "people-id" || locale != "" || doc.ID != "one" {
			t.Fatalf("target=%q locale=%q document=%#v", target, locale, doc)
		}
		retained = doc
		delete(doc.Values, "secret")
		doc.Values["public"] = store.String("shown")
		return doc
	})
	retained.Values["public"] = store.String("later callback edit")
	if calls != 1 {
		t.Fatalf("callbacks=%d", calls)
	}
	first, _ := mapped["links"].ListItem(0)
	updated, _ := first.Get("id").CopyDocument()
	if text, _ := updated.Values["public"].StringValue(); text != "shown" {
		t.Fatalf("retained callback mutated result: %q", text)
	}
	original, _ := document.CopyDocument()
	if _, exists := original.Values["secret"]; !exists {
		t.Fatal("callback mutated original populated document")
	}
	for index, expected := range []store.Value{unknown, unpopulated, store.Null()} {
		actual, _ := mapped["links"].ListItem(index + 1)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("unpopulated or unknown branch %d changed", index+1)
		}
	}
	mapped["links"] = store.Null()
	if values["links"].Kind() != store.ValueList {
		t.Fatal("result root was not detached")
	}
}

func embeddedPopulationFixture() (schema.Field, func(string, string) store.Value) {
	field := schema.Field{Name: "content", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{EmbeddedTrees: []schema.EmbeddedTree{{
		Version: 1, Key: "cards", Root: []string{"outline"}, Children: "items", Tag: "kind",
		Cases: []schema.EmbeddedTreeCase{{TagValue: "widget", Payload: "content", Discriminator: "schema", Identity: "uid", Types: []schema.BlockType{{
			Slug: "card", Fields: []schema.Field{{Name: "author", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"}}},
		}}}},
	}}}}
	return field, func(key, author string) store.Value {
		return store.Object(store.Values{"kind": store.String("widget"), "content": store.Object(store.Values{
			"schema": store.String("card"), "uid": store.String(key), "author": store.String(author),
		})})
	}
}
