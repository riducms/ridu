package operation

import (
	"errors"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestSortPathsMustReachOneStoredScalarThroughUnlocalizedGroups(t *testing.T) {
	text := func(name string) schema.Field { return schema.Field{Name: name, Type: schema.FieldTypeText} }
	group := func(name string, localized bool, children ...schema.Field) schema.Field {
		return schema.Field{Name: name, Type: schema.FieldTypeGroup, Localized: localized, Nested: &schema.NestedField{Fields: children}}
	}
	fields := []schema.Field{
		text("title"),
		{Name: "caption", Type: schema.FieldTypeText, Localized: true},
		{Name: "author", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionSlug: "people"}},
		{Name: "authors", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionSlug: "people", HasMany: true}},
		{Name: "subject", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionSlug: "posts"}}}},
		{Name: "tags", Type: schema.FieldTypeSelect, Select: &schema.SelectField{HasMany: true}},
		{Name: "sizes", Type: schema.FieldTypeNumberList},
		{Name: "metadata", Type: schema.FieldTypeJSON},
		{Name: "body", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "test.body"}},
		{Name: "location", Type: schema.FieldTypePoint},
		{Name: "rows", Type: schema.FieldTypeArray, Nested: &schema.NestedField{Fields: []schema.Field{text("label")}}},
		group("seo", false, text("title"), group("details", false, text("summary"))),
		group("translated", true, text("title")),
		group("outer", false, group("inner", true, text("title"))),
	}
	versioned := schema.Collection{Fields: fields, Versions: &schema.VersionSettings{}}
	for _, name := range []string{"id", "createdAt", "_status", "_revision", "title", "caption", "author", "seo.title", "seo.details.summary"} {
		if err := validateSortPath(versioned, query.Field(splitPath(name)...)); err != nil {
			t.Errorf("sort %s: %v", name, err)
		}
	}
	unversioned := schema.Collection{Fields: fields}
	for _, name := range []string{"authors", "subject", "tags", "sizes", "metadata", "metadata.key", "body", "location", "rows", "rows.label", "seo", "seo.details", "translated", "translated.title", "outer.inner.title", "_status", "_revision"} {
		err := validateSortPath(unversioned, query.Field(splitPath(name)...))
		var failure *Error
		if !errors.As(err, &failure) || failure.Status != 400 || failure.Code != "bad_query" || len(failure.Issues) != 1 || failure.Issues[0].Code != "unsupported_path" || failure.Issues[0].Path != name {
			t.Errorf("sort %s error = %#v, want 400 unsupported_path", name, err)
		}
	}
}

func splitPath(name string) []string {
	path, err := query.ParsePath(name)
	if err != nil {
		panic(err)
	}
	return path.Segments()
}
