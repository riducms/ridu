package mongodb

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestMongoListWindowPredicateUsesOneDirectUniqueTextRange(t *testing.T) {
	collection := mongoWindowCollection(t)
	path, indexName, predicate, err := mongoListWindowPredicate(store.Request{
		Collection: collection,
		IndexWindow: &store.IndexWindow{
			Path: mongoWindowPath(t, "slug"), LowerBound: "a", UpperBound: "c",
		},
		Limit: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "values.slug" {
		t.Fatalf("MongoDB list window storage path = %q, want values.slug", path)
	}
	wantIndexName := mongoDeclaredIndexName(collection.ID, "field:window-posts-slug", true)
	if indexName != wantIndexName {
		t.Fatalf("MongoDB list window index = %q, want %q", indexName, wantIndexName)
	}
	encoded := predicate.String()
	for _, fragment := range []string{"meta.deletedAt", "$gte", "$lt", "values.slug"} {
		if !strings.Contains(encoded, fragment) {
			t.Fatalf("MongoDB list window predicate %s does not contain %q", encoded, fragment)
		}
	}
}

func TestMongoListWindowPredicateRejectsAnythingOutsideTheBoundedContract(t *testing.T) {
	collection := mongoWindowCollection(t)
	base := store.Request{
		Collection: collection,
		IndexWindow: &store.IndexWindow{
			Path: mongoWindowPath(t, "slug"), LowerBound: "a", UpperBound: "c",
		},
		Limit: 10,
	}

	filter := query.Equal(mongoWindowPath(t, "title"), query.String("hidden")).Node()
	withFilter := base
	withFilter.Filter = &filter
	if _, _, _, err := mongoListWindowPredicate(withFilter); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("MongoDB list window filter error = %v", err)
	}

	withoutUniqueIndex := base
	withoutUniqueIndex.Collection.Fields = append([]schema.Field(nil), collection.Fields...)
	withoutUniqueIndex.Collection.Fields[0].Unique = false
	if _, _, _, err := mongoListWindowPredicate(withoutUniqueIndex); err == nil || !strings.Contains(err.Error(), "unique, indexed") {
		t.Fatalf("MongoDB list window unindexed path error = %v", err)
	}
}

func mongoWindowCollection(t *testing.T) schema.Collection {
	t.Helper()
	return schema.Collection{
		ID: "window-posts", Slug: "window-posts",
		Labels: schema.CollectionLabels{Singular: "Window post", Plural: "Window posts"},
		Fields: []schema.Field{
			{
				ID: "window-posts-slug", Name: "slug", Path: mongoWindowPath(t, "slug"),
				Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar,
				Index: true, Unique: true, Required: true, Text: &schema.TextField{},
			},
			{
				ID: "window-posts-title", Name: "title", Path: mongoWindowPath(t, "title"),
				Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
			},
		},
	}
}

func mongoWindowPath(t *testing.T, segments ...string) query.Path {
	t.Helper()
	path, err := query.NewPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
