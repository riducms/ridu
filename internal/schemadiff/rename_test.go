package schemadiff_test

import (
	"testing"

	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestCollectionRenameCandidatePairsDerivedFieldIdentities(t *testing.T) {
	before := manifest(collection("posts", "posts", textField("posts-title", "title")))
	after := manifest(collection("articles", "articles", textField("articles-title", "title")))

	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 1 || candidates[0].Kind != schemadiff.RenameCollection {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.BeforeCollection.Slug != "posts" || candidate.AfterCollection.Slug != "articles" {
		t.Fatalf("collection candidate = %#v", candidate)
	}
	if len(candidate.Fields) != 1 || candidate.Fields[0].Before.ID != "posts-title" || candidate.Fields[0].After.ID != "articles-title" {
		t.Fatalf("field pairs = %#v", candidate.Fields)
	}
}

func TestFieldRenameCandidateRequiresAUniqueShapeMatch(t *testing.T) {
	before := manifest(collection("posts", "posts", textField("posts-title", "title")))
	after := manifest(collection("posts", "posts", textField("posts-headline", "headline")))

	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 1 || candidates[0].Kind != schemadiff.RenameField {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].BeforeField.Name != "title" || candidates[0].AfterField.Name != "headline" {
		t.Fatalf("field candidate = %#v", candidates[0])
	}
}

func TestAmbiguousFieldShapesAreNotGuessed(t *testing.T) {
	before := manifest(collection("posts", "posts",
		textField("posts-first", "first"),
		textField("posts-second", "second"),
	))
	after := manifest(collection("posts", "posts",
		textField("posts-primary", "primary"),
		textField("posts-secondary", "secondary"),
	))

	if candidates := schemadiff.RenameCandidates(before, after); len(candidates) != 0 {
		t.Fatalf("ambiguous candidates = %#v", candidates)
	}
}

func TestNestedFieldRenameIsDetectedAtItsCanonicalPath(t *testing.T) {
	beforeGroupPath, _ := query.NewPath("seo")
	beforeTitlePath, _ := query.NewPath("seo", "title")
	afterGroupPath, _ := query.NewPath("seo")
	afterTitlePath, _ := query.NewPath("seo", "headline")
	before := manifest(collection("posts", "posts", schema.Field{
		ID: "posts-seo", Name: "seo", Path: beforeGroupPath, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "posts-seo-title", Name: "title", Path: beforeTitlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}},
		}},
	}))
	after := manifest(collection("posts", "posts", schema.Field{
		ID: "posts-seo", Name: "seo", Path: afterGroupPath, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{
			{ID: "posts-seo-headline", Name: "headline", Path: afterTitlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}},
		}},
	}))

	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 1 || candidates[0].BeforeField.Path.String() != "seo.title" || candidates[0].AfterField.Path.String() != "seo.headline" {
		t.Fatalf("nested candidates = %#v", candidates)
	}
}

func TestCollectionRenameCandidatesNormalizeSimultaneouslyRenamedTargets(t *testing.T) {
	relationship := func(id schema.StableID, name string, targetID schema.StableID, targetSlug schema.CollectionSlug) schema.Field {
		path, _ := query.NewPath(name)
		return schema.Field{
			ID: id, Name: name, Path: path, Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				Polymorphic: true,
				Targets:     []schema.RelationshipTarget{{CollectionID: targetID, CollectionSlug: targetSlug}},
			},
		}
	}
	before := manifest(
		collection("articles", "articles"),
		collection("z-pages", "z-pages", relationship("z-pages-related", "related", "articles", "articles")),
	)
	after := manifest(
		collection("posts", "posts"),
		collection("landing", "landing", relationship("landing-related", "related", "posts", "posts")),
	)

	plain := schemadiff.RenameCandidates(before, after)
	if len(plain) != 1 || plain[0].BeforeCollection.ID != "articles" {
		t.Fatalf("unmapped candidates = %#v, want only independent target rename", plain)
	}
	candidates := schemadiff.RenameCandidatesWithCollectionMapping(before, after, map[schema.StableID]schema.StableID{"articles": "posts", "z-pages": "landing"})
	if len(candidates) != 2 || candidates[0].BeforeCollection.ID != "articles" || candidates[1].BeforeCollection.ID != "z-pages" {
		t.Fatalf("mapped candidates = %#v", candidates)
	}
	if len(candidates[1].Fields) != 1 || candidates[1].Fields[0].Before.ID != "z-pages-related" || candidates[1].Fields[0].After.ID != "landing-related" {
		t.Fatalf("mapped owner field pairs = %#v", candidates[1].Fields)
	}
}

func manifest(collections ...schema.Collection) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Diff"},
		Collections: collections, Plugins: []schema.Plugin{},
	})
}

func collection(id schema.StableID, slug schema.CollectionSlug, fields ...schema.Field) schema.Collection {
	return schema.Collection{
		ID: id, Slug: slug, Labels: schema.CollectionLabels{Singular: string(slug), Plural: string(slug)},
		Fields: fields,
	}
}

func textField(id schema.StableID, name string) schema.Field {
	path, _ := query.NewPath(name)
	return schema.Field{ID: id, Name: name, Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}}
}
