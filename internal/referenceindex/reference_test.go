package referenceindex_test

import (
	"testing"

	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestCollectAndNullifyTargetAcrossNestedLocalizedReferenceShapes(t *testing.T) {
	collection := schema.Collection{ID: "posts", Fields: []schema.Field{
		{
			ID: "posts-author", Name: "author", Type: schema.FieldTypeRelationship,
			Relationship: &schema.RelationshipField{CollectionID: "users", OnDelete: schema.ReferenceDeleteNullify},
		},
		{
			ID: "posts-rows", Name: "rows", Type: schema.FieldTypeArray,
			Nested: &schema.NestedField{Fields: []schema.Field{{
				ID: "posts-rows-tags", Name: "tags", Type: schema.FieldTypeRelationship,
				Relationship: &schema.RelationshipField{CollectionID: "tags", HasMany: true, OnDelete: schema.ReferenceDeleteNullify},
			}}},
		},
		{
			ID: "posts-gallery", Name: "gallery", Type: schema.FieldTypeBlocks, Localized: true,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{Key: "image", Fields: []schema.Field{{
				ID: "posts-gallery-asset", Name: "asset", Type: schema.FieldTypeUpload,
				Upload: &schema.UploadField{CollectionID: "media", OnDelete: schema.ReferenceDeleteNullify},
			}}}}},
		},
	}}
	document := store.Document{ID: "post-1", Values: store.Values{
		"author": store.String("user-1"),
		"rows": store.List(store.Object(store.Values{
			"_key": store.String("row-1"),
			"tags": store.List(store.String("tag-1"), store.String("tag-1"), store.String("tag-2")),
		})),
		"gallery": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{"blockType": store.String("image"), "asset": store.String("media-1")})),
			"fr": store.List(store.Object(store.Values{"blockType": store.String("image"), "asset": store.String("media-2")})),
		}),
	}}

	entries := referenceindex.Collect(collection, document)
	if len(entries) != 6 {
		t.Fatalf("entries = %#v", entries)
	}
	var duplicateOccurrences []int
	var mediaLocales []schema.LocaleCode
	for _, entry := range entries {
		if entry.FieldID == "posts-rows-tags" && entry.Target.DocumentID == "tag-1" {
			duplicateOccurrences = append(duplicateOccurrences, entry.Occurrence)
		}
		if entry.FieldID == "posts-gallery-asset" {
			mediaLocales = append(mediaLocales, entry.Locale)
		}
	}
	if len(duplicateOccurrences) != 2 || duplicateOccurrences[0] != 0 || duplicateOccurrences[1] != 1 {
		t.Fatalf("duplicate occurrences = %#v", duplicateOccurrences)
	}
	if len(mediaLocales) != 2 || mediaLocales[0] != "en" || mediaLocales[1] != "fr" {
		t.Fatalf("media locales = %#v", mediaLocales)
	}

	values, changed := referenceindex.NullifyTarget(collection, document.Values, store.DocumentReference{CollectionID: "tags", DocumentID: "tag-1"})
	if !changed {
		t.Fatal("nested has-many target was not removed")
	}
	rows, _ := values["rows"].Values()
	row, _ := rows[0].ObjectValue()
	key, _ := row["_key"].StringValue()
	tags, _ := row["tags"].Values()
	remaining, _ := tags[0].StringValue()
	if key != "row-1" || len(tags) != 1 || remaining != "tag-2" {
		t.Fatalf("reconciled row = %#v", row)
	}

	values, changed = referenceindex.NullifyTarget(collection, values, store.DocumentReference{CollectionID: "media", DocumentID: "media-1"})
	if !changed {
		t.Fatal("localized block upload was not nullified")
	}
	localized, _ := values["gallery"].ObjectValue()
	enBlocks, _ := localized["en"].Values()
	enBlock, _ := enBlocks[0].ObjectValue()
	frBlocks, _ := localized["fr"].Values()
	frBlock, _ := frBlocks[0].ObjectValue()
	if enBlock["asset"].Kind() != store.ValueNull {
		t.Fatalf("English asset = %#v", enBlock["asset"])
	}
	if id, _ := frBlock["asset"].StringValue(); id != "media-2" {
		t.Fatalf("French asset = %#v", frBlock["asset"])
	}
}

func TestNullifyTargetLeavesRestrictReferencesUntouched(t *testing.T) {
	collection := schema.Collection{ID: "posts", Fields: []schema.Field{{
		ID: "posts-owner", Name: "owner", Type: schema.FieldTypeRelationship, Required: true,
		Relationship: &schema.RelationshipField{CollectionID: "users", OnDelete: schema.ReferenceDeleteRestrict},
	}}}
	values := store.Values{"owner": store.String("user-1")}
	updated, changed := referenceindex.NullifyTarget(collection, values, store.DocumentReference{CollectionID: "users", DocumentID: "user-1"})
	if changed {
		t.Fatal("restrict reference was mutated")
	}
	if id, _ := updated["owner"].StringValue(); id != "user-1" {
		t.Fatalf("owner = %#v", updated["owner"])
	}
}

func TestNullifyTargetRemovesOnlyMatchingPolymorphicMembers(t *testing.T) {
	collection := schema.Collection{ID: "posts", Fields: []schema.Field{{
		ID: "posts-subjects", Name: "subjects", Type: schema.FieldTypeRelationship,
		Relationship: &schema.RelationshipField{
			Polymorphic: true, HasMany: true, OnDelete: schema.ReferenceDeleteNullify,
			Targets: []schema.RelationshipTarget{
				{CollectionID: "people", CollectionSlug: "people"},
				{CollectionID: "teams", CollectionSlug: "teams"},
			},
		},
	}}}
	reference := func(collection, id string) store.Value {
		return store.Object(store.Values{"relationTo": store.String(collection), "id": store.String(id)})
	}
	document := store.Document{ID: "post-1", Values: store.Values{"subjects": store.List(
		reference("teams", "shared"), reference("people", "shared"),
		reference("people", "shared"), reference("teams", "other"),
	)}}
	entries := referenceindex.Collect(collection, document)
	if len(entries) != 4 || entries[0].Target.CollectionID != "teams" || entries[1].Target.CollectionID != "people" {
		t.Fatalf("polymorphic entries = %#v", entries)
	}
	updated, changed := referenceindex.NullifyTarget(collection, document.Values, store.DocumentReference{CollectionID: "people", DocumentID: "shared"})
	if !changed {
		t.Fatal("matching polymorphic members were not removed")
	}
	subjects, _ := updated["subjects"].Values()
	if len(subjects) != 2 {
		t.Fatalf("remaining polymorphic members = %#v", subjects)
	}
	for index, wantID := range []string{"shared", "other"} {
		object, _ := subjects[index].ObjectValue()
		if relationTo, id := stringValue(object["relationTo"]), stringValue(object["id"]); relationTo != "teams" || id != wantID {
			t.Errorf("remaining member %d = %#v", index, object)
		}
	}
}

func TestRemoveResourceTargetsAcrossNestedLocalizedAndPolymorphicShapes(t *testing.T) {
	collection := schema.Collection{ID: "posts", Fields: []schema.Field{
		{
			ID: "posts-owner", Name: "owner", Type: schema.FieldTypeRelationship,
			Relationship: &schema.RelationshipField{CollectionID: "users", OnDelete: schema.ReferenceDeleteRestrict},
		},
		{
			ID: "posts-sections", Name: "sections", Type: schema.FieldTypeArray,
			Nested: &schema.NestedField{Fields: []schema.Field{{
				ID: "posts-sections-subjects", Name: "subjects", Type: schema.FieldTypeRelationship,
				Relationship: &schema.RelationshipField{
					Polymorphic: true, HasMany: true, OnDelete: schema.ReferenceDeleteRestrict,
					Targets: []schema.RelationshipTarget{
						{CollectionID: "people", CollectionSlug: "people"},
						{CollectionID: "teams", CollectionSlug: "teams"},
					},
				},
			}}},
		},
		{
			ID: "posts-gallery", Name: "gallery", Type: schema.FieldTypeBlocks, Localized: true,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{Key: "image", Fields: []schema.Field{{
				ID: "posts-gallery-assets", Name: "assets", Type: schema.FieldTypeUpload,
				Upload: &schema.UploadField{CollectionID: "media", HasMany: true, OnDelete: schema.ReferenceDeleteRestrict},
			}}}}},
		},
	}}
	reference := func(collection, id string) store.Value {
		return store.Object(store.Values{"relationTo": store.String(collection), "id": store.String(id)})
	}
	values := store.Values{
		"owner": store.String("user-1"),
		"sections": store.List(store.Object(store.Values{
			"_key": store.String("section-1"),
			"subjects": store.List(
				reference("people", "person-1"),
				reference("teams", "team-1"),
			),
		})),
		"gallery": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"blockType": store.String("image"),
				"assets":    store.List(store.String("media-1"), store.String("media-2")),
			})),
		}),
	}

	if !referenceindex.TargetsAnyResource(collection, []schema.StableID{"users", "people", "media"}) {
		t.Fatal("reference topology did not report retired targets")
	}
	if referenceindex.TargetsAnyResource(collection, []schema.StableID{"unrelated"}) {
		t.Fatal("reference topology reported an unrelated target")
	}
	updated, changed := referenceindex.RemoveResourceTargets(collection, values, []schema.StableID{"users", "people", "media"})
	if !changed {
		t.Fatal("retired resource values were not removed")
	}
	if updated["owner"].Kind() != store.ValueNull {
		t.Fatalf("restrict-policy owner was retained: %#v", updated["owner"])
	}
	sections, _ := updated["sections"].Values()
	section, _ := sections[0].ObjectValue()
	subjects, _ := section["subjects"].Values()
	if len(subjects) != 1 {
		t.Fatalf("polymorphic subjects = %#v", subjects)
	}
	subject, _ := subjects[0].ObjectValue()
	if stringValue(subject["relationTo"]) != "teams" || stringValue(subject["id"]) != "team-1" {
		t.Fatalf("remaining polymorphic subject = %#v", subject)
	}
	localized, _ := updated["gallery"].ObjectValue()
	blocks, _ := localized["en"].Values()
	block, _ := blocks[0].ObjectValue()
	assets, _ := block["assets"].Values()
	if len(assets) != 0 {
		t.Fatalf("retired upload values = %#v", assets)
	}
}

func stringValue(value store.Value) string {
	text, _ := value.StringValue()
	return text
}
