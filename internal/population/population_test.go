package population_test

import (
	"reflect"
	"testing"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestMapAtPathTraversesLocalizedGroupsArraysAndBlocks(t *testing.T) {
	person := schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"}
	fields := []schema.Field{{
		Name: "sections", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{{
			Name: "content", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{
				Slug: "quote", Fields: []schema.Field{{
					Name: "credit", Path: mustPath(t, "sections", "content", "quote", "credit"), Type: schema.FieldTypeGroup,
					Nested: &schema.NestedField{Fields: []schema.Field{{
						Name: "author", Path: mustPath(t, "sections", "content", "quote", "credit", "author"), Type: schema.FieldTypeRelationship,
						Localized: true, Relationship: &person,
					}}},
				}},
			}}},
		}}},
	}}
	values := store.Values{"sections": store.List(
		store.Object(store.Values{"content": store.List(
			store.Object(store.Values{"blockType": store.String("quote"), "credit": store.Object(store.Values{
				"author": store.Object(store.Values{"en": store.String("person-en"), "fr": store.String("person-fr")}),
			})}),
			store.Object(store.Values{"blockType": store.String("other"), "credit": store.Object(store.Values{"author": store.String("untouched")})}),
		)}),
	)}
	path := mustPath(t, "sections", "content", "quote", "credit", "author")
	var visited []string
	mapped, matched := population.MapAtPath(fields, values, path, population.LocaleSelection{Chain: []schema.LocaleCode{"fr", "en"}}, func(_ schema.Field, value store.Value) store.Value {
		id, _ := value.StringValue()
		visited = append(visited, id)
		return store.String("populated:" + id)
	})
	if !matched || !reflect.DeepEqual(visited, []string{"person-fr"}) {
		t.Fatalf("matched=%v visited=%v", matched, visited)
	}
	sections, _ := mapped["sections"].CopyList()
	section, _ := sections[0].CopyObject()
	blocks, _ := section["content"].CopyList()
	quote, _ := blocks[0].CopyObject()
	credit, _ := quote["credit"].CopyObject()
	localized, _ := credit["author"].CopyObject()
	if got, _ := localized["fr"].StringValue(); got != "populated:person-fr" {
		t.Fatalf("French mapped value = %q", got)
	}
	if got, _ := localized["en"].StringValue(); got != "person-en" {
		t.Fatalf("English value changed = %q", got)
	}
}

func TestMapAtPathAllLocalesVisitsStableLocaleOrder(t *testing.T) {
	path := mustPath(t, "editor")
	fields := []schema.Field{{
		Name: "editor", Path: path, Type: schema.FieldTypeRelationship, Localized: true,
		Relationship: &schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"},
	}}
	values := store.Values{"editor": store.Object(store.Values{"fr": store.String("fr-id"), "en": store.String("en-id")})}
	var visited []string
	population.VisitAtPath(fields, values, path, population.LocaleSelection{All: true}, func(_ schema.Field, value store.Value) {
		id, _ := value.StringValue()
		visited = append(visited, id)
	})
	if !reflect.DeepEqual(visited, []string{"en-id", "fr-id"}) {
		t.Fatalf("visited locales = %v", visited)
	}
}

func TestReferenceFieldsAndFieldAtPathIncludeBlockDiscriminator(t *testing.T) {
	reference := schema.Field{
		Name: "asset", Path: mustPath(t, "layout", "hero", "asset"), Type: schema.FieldTypeUpload,
		Upload: &schema.UploadField{CollectionID: "media", CollectionSlug: "media"},
	}
	fields := []schema.Field{{Name: "layout", Type: schema.FieldTypeBlocks, Blocks: &schema.BlocksField{Types: []schema.BlockType{{Slug: "hero", Fields: []schema.Field{reference}}}}}}
	found, ok := population.FieldAtPath(fields, reference.Path)
	if !ok || found.ID != reference.ID || found.Path.String() != "layout.hero.asset" {
		t.Fatalf("field = %#v, found=%v", found, ok)
	}
	references := population.ReferenceFields(fields)
	if len(references) != 1 || references[0].Path.String() != "layout.hero.asset" {
		t.Fatalf("references = %#v", references)
	}
}

func TestMapPopulatedDocumentsTraversesNestedLocalizedResponseShape(t *testing.T) {
	path := mustPath(t, "meta", "reviewer")
	fields := []schema.Field{{
		Name: "meta", Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{{
			Name: "reviewer", Path: path, Type: schema.FieldTypeRelationship, Localized: true,
			Relationship: &schema.RelationshipField{CollectionID: "people", CollectionSlug: "people"},
		}}},
	}}
	values := store.Values{"meta": store.Object(store.Values{"reviewer": store.Object(store.Values{
		"en": store.Populated(store.Document{ID: "one", Values: store.Values{"secret": store.String("remove")}}),
		"fr": store.Populated(store.Document{ID: "two", Values: store.Values{"secret": store.String("remove")}}),
	})})}
	seenLocales := map[schema.LocaleCode]bool{}
	mapped := population.MapPopulatedDocuments(fields, values, true, func(_ schema.StableID, locale schema.LocaleCode, document store.Document) store.Document {
		seenLocales[locale] = true
		delete(document.Values, "secret")
		return document
	})
	meta, _ := mapped["meta"].CopyObject()
	localized, _ := meta["reviewer"].CopyObject()
	for _, locale := range []string{"en", "fr"} {
		document, populated := localized[locale].CopyDocument()
		if !populated {
			t.Fatalf("%s reviewer was not populated", locale)
		}
		if _, leaked := document.Values["secret"]; leaked {
			t.Fatalf("%s reviewer leaked secret", locale)
		}
	}
	if !seenLocales["en"] || !seenLocales["fr"] {
		t.Fatalf("callback locales = %#v", seenLocales)
	}
}

func TestMapPopulatedDocumentsTraversesInverseJoinTargets(t *testing.T) {
	fields := []schema.Field{{
		Name: "posts", Path: mustPath(t, "posts"), Type: schema.FieldTypeJoin,
		Join: &schema.JoinField{CollectionID: "posts", CollectionSlug: "posts"},
	}}
	if population.RelationshipDetails(fields[0]) != nil {
		t.Fatal("presentation-only join became an explicit population input")
	}
	values := store.Values{"posts": store.List(
		store.Populated(store.Document{ID: "one", Values: store.Values{"secret": store.String("remove")}}),
		store.Populated(store.Document{ID: "two", Values: store.Values{"secret": store.String("remove")}}),
	)}
	var visited []string
	mapped := population.MapPopulatedDocuments(fields, values, false, func(targetID schema.StableID, _ schema.LocaleCode, document store.Document) store.Document {
		if targetID != "posts" {
			t.Fatalf("target ID = %q", targetID)
		}
		visited = append(visited, document.ID)
		delete(document.Values, "secret")
		return document
	})
	joined, valid := mapped["posts"].CopyList()
	if !valid || !reflect.DeepEqual(visited, []string{"one", "two"}) {
		t.Fatalf("visited=%v joined=%#v", visited, mapped["posts"])
	}
	for _, value := range joined {
		document, populated := value.CopyDocument()
		if !populated {
			t.Fatalf("join target = %#v", value)
		}
		if _, leaked := document.Values["secret"]; leaked {
			t.Fatalf("join target leaked secret: %#v", document)
		}
	}
}

func mustPath(t *testing.T, segments ...string) query.Path {
	t.Helper()
	path, err := query.NewPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
