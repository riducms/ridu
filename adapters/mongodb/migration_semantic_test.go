package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestRewriteMongoScheduledPublishTaskCollectionIDPreservesOpaqueState(t *testing.T) {
	task := store.Task{
		Slug: mongoScheduledPublishTaskSlug, Queue: "scheduled-publish", ConcurrencyKey: "authors:post-1",
		Input:       json.RawMessage(`{"collectionID":"authors","documentID":"post-1","expectedRevision":3,"requestedByCollectionID":"authors","requestedByUserID":"user-1","future":{"keep":true}}`),
		Target:      &store.DocumentReference{CollectionID: "authors", DocumentID: "post-1"},
		RequestedBy: &store.DocumentReference{CollectionID: "authors", DocumentID: "user-1"},
	}
	updated, changed, err := rewriteMongoScheduledPublishTaskCollectionID(task, "authors", "members")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || updated.Target.CollectionID != "members" || updated.RequestedBy.CollectionID != "members" {
		t.Fatalf("scheduled-publish identity rewrite = %#v", updated)
	}
	if want := mongoScheduledPublishConcurrencyKey("members", "post-1"); updated.ConcurrencyKey != want {
		t.Fatalf("scheduled-publish concurrency key = %q, want %q", updated.ConcurrencyKey, want)
	}
	var input map[string]any
	if err := json.Unmarshal(updated.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input["collectionID"] != "members" || input["requestedByCollectionID"] != "members" || input["expectedRevision"] != float64(3) {
		t.Fatalf("scheduled-publish input rewrite = %#v", input)
	}
	future, ok := input["future"].(map[string]any)
	if !ok || future["keep"] != true {
		t.Fatalf("scheduled-publish unknown input was not preserved: %#v", input)
	}
	if string(task.Input) == string(updated.Input) || task.Target.CollectionID != "authors" || task.RequestedBy.CollectionID != "authors" {
		t.Fatalf("scheduled-publish rewrite mutated its input task: %#v", task)
	}

	generic := cloneMongoTask(task)
	generic.Slug = "application-owned"
	generic.Input = json.RawMessage(`{"collectionID":"authors","opaque":true}`)
	generic.ConcurrencyKey = "authors:opaque"
	if mongoScheduledPublishTaskMentionsCollection(generic, "authors") {
		t.Fatal("generic opaque task was selected for built-in input/concurrency rewriting")
	}
	if generic.ConcurrencyKey != "authors:opaque" || string(generic.Input) != `{"collectionID":"authors","opaque":true}` {
		t.Fatalf("generic opaque task changed = %#v", generic)
	}
}

func TestRewriteMongoScheduledPublishTaskCollectionIDFailsClosedOnDivergence(t *testing.T) {
	task := store.Task{
		Slug: mongoScheduledPublishTaskSlug, Queue: "scheduled-publish", ConcurrencyKey: "authors:post-1",
		Input:  json.RawMessage(`{"collectionID":"different","documentID":"post-1","expectedRevision":3}`),
		Target: &store.DocumentReference{CollectionID: "authors", DocumentID: "post-1"},
	}
	if _, _, err := rewriteMongoScheduledPublishTaskCollectionID(task, "authors", "members"); err == nil || !strings.Contains(err.Error(), "target reference") {
		t.Fatalf("divergent scheduled-publish rewrite error = %v", err)
	}
}

func TestRenameMongoStoreFieldBySchemaHandlesLocalizedRepeatedAndRecursiveContainers(t *testing.T) {
	path := func(segments ...string) query.Path {
		value, err := query.NewPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	text := func(name string, pathValue query.Path) schema.Field {
		return schema.Field{Name: name, Path: pathValue, Type: schema.FieldTypeText}
	}
	before := []schema.Field{
		{
			Name: "meta", Path: path("meta"), Type: schema.FieldTypeGroup, Localized: true,
			Nested: &schema.NestedField{Fields: []schema.Field{text("oldTitle", path("meta", "oldTitle"))}},
		},
		{
			Name: "rows", Path: path("rows"), Type: schema.FieldTypeArray, Localized: true,
			Nested: &schema.NestedField{Fields: []schema.Field{text("oldLabel", path("rows", "oldLabel"))}},
		},
		{
			Name: "layout", Path: path("layout"), Type: schema.FieldTypeBlocks, Localized: true,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{Key: "feature", Fields: []schema.Field{
				text("oldCaption", path("layout", "feature", "oldCaption")),
			}}}},
		},
	}
	after := []schema.Field{
		{
			Name: "content", Path: path("content"), Type: schema.FieldTypeGroup, Localized: true,
			Nested: &schema.NestedField{Fields: []schema.Field{text("heading", path("content", "heading"))}},
		},
		{
			Name: "rows", Path: path("rows"), Type: schema.FieldTypeArray, Localized: true,
			Nested: &schema.NestedField{Fields: []schema.Field{text("label", path("rows", "label"))}},
		},
		{
			Name: "layout", Path: path("layout"), Type: schema.FieldTypeBlocks, Localized: true,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{Key: "feature", Fields: []schema.Field{
				text("caption", path("layout", "feature", "caption")),
			}}}},
		},
	}
	schemas := mongoDBMigrationFieldSchemas{before: before, after: after}
	values := store.Values{
		"meta": store.Object(store.Values{
			"en": store.Object(store.Values{"oldTitle": store.String("English")}),
			"fr": store.Object(store.Values{"oldTitle": store.String("French")}),
		}),
		"rows": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{"_key": store.String("row-1"), "oldLabel": store.String("Row")})),
			"fr": store.List(),
		}),
		"layout": store.Object(store.Values{
			"en": store.List(store.Object(store.Values{
				"_key": store.String("block-1"), "blockType": store.String("feature"), "oldCaption": store.String("Block"),
			})),
			"fr": store.List(),
		}),
	}
	pairs := []ridumigration.FieldRename{
		{Before: "meta", After: "content"},
		{Before: "meta.oldTitle", After: "content.heading"},
		{Before: "rows.oldLabel", After: "rows.label"},
		{Before: "layout.feature.oldCaption", After: "layout.feature.caption"},
	}
	for _, pair := range pairs {
		changed, err := renameMongoStoreFieldBySchema(values, schemas, pair)
		if err != nil {
			t.Fatalf("rename %s -> %s: %v", pair.Before, pair.After, err)
		}
		if !changed {
			t.Fatalf("rename %s -> %s did not change data", pair.Before, pair.After)
		}
	}
	content, _ := values["content"].ObjectValue()
	englishContent, _ := content["en"].ObjectValue()
	rows, _ := values["rows"].ObjectValue()
	englishRows, _ := rows["en"].Values()
	row, _ := englishRows[0].ObjectValue()
	layout, _ := values["layout"].ObjectValue()
	englishLayout, _ := layout["en"].Values()
	block, _ := englishLayout[0].ObjectValue()
	if _, stale := values["meta"]; stale {
		t.Fatalf("recursive container source remains: %#v", values)
	}
	if heading, _ := englishContent["heading"].StringValue(); heading != "English" {
		t.Fatalf("localized group rename = %#v", englishContent)
	}
	if label, _ := row["label"].StringValue(); label != "Row" {
		t.Fatalf("localized array rename = %#v", row)
	}
	if caption, _ := block["caption"].StringValue(); caption != "Block" {
		t.Fatalf("localized block rename = %#v", block)
	}
	for _, pair := range pairs {
		if changed, err := renameMongoStoreFieldBySchema(values, schemas, pair); err != nil || changed {
			t.Fatalf("idempotent rename %s -> %s = %t, %v", pair.Before, pair.After, changed, err)
		}
	}

	collision := store.Values{"meta": store.Object(store.Values{"en": store.Object(store.Values{
		"oldTitle": store.String("source"), "heading": store.String("target"),
	})})}
	original := store.CloneValues(collision)
	if _, err := renameMongoStoreFieldBySchema(collision, schemas, ridumigration.FieldRename{Before: "meta.oldTitle", After: "content.heading"}); err == nil || !strings.Contains(err.Error(), "already contains") {
		t.Fatalf("localized field collision error = %v", err)
	}
	collisionJSON, _ := json.Marshal(collision)
	originalJSON, _ := json.Marshal(original)
	if string(collisionJSON) != string(originalJSON) {
		t.Fatalf("collision mutated values: %s, want %s", collisionJSON, originalJSON)
	}
}

func TestRenameMongoStoreFieldsBySchemaOrdersNestedMappings(t *testing.T) {
	path := func(segments ...string) query.Path {
		value, err := query.NewPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := []schema.Field{
		{
			Name: "a", Path: path("a"), Type: schema.FieldTypeGroup,
			Nested: &schema.NestedField{Fields: []schema.Field{
				{
					Name: "b", Path: path("a", "b"), Type: schema.FieldTypeGroup,
					Nested: &schema.NestedField{Fields: []schema.Field{
						{Name: "c", Path: path("a", "b", "c"), Type: schema.FieldTypeText},
					}},
				},
			}},
		},
	}
	after := []schema.Field{
		{
			Name: "x", Path: path("x"), Type: schema.FieldTypeGroup,
			Nested: &schema.NestedField{Fields: []schema.Field{
				{
					Name: "y", Path: path("x", "y"), Type: schema.FieldTypeGroup,
					Nested: &schema.NestedField{Fields: []schema.Field{
						{Name: "z", Path: path("x", "y", "z"), Type: schema.FieldTypeText},
					}},
				},
			}},
		},
	}
	schemas := mongoDBMigrationFieldSchemas{before: before, after: after}
	pairs := []ridumigration.FieldRename{
		{Before: "a", After: "x"},
		{Before: "a.b", After: "x.y"},
		{Before: "a.b.c", After: "x.y.z"},
	}
	permutations := [][]int{
		{0, 1, 2}, {0, 2, 1}, {1, 0, 2},
		{1, 2, 0}, {2, 0, 1}, {2, 1, 0},
	}
	for _, permutation := range permutations {
		permuted := []ridumigration.FieldRename{pairs[permutation[0]], pairs[permutation[1]], pairs[permutation[2]]}
		originalIntent := append([]ridumigration.FieldRename(nil), permuted...)
		for _, storedShape := range []string{"current", "version-only snapshot"} {
			t.Run(fmt.Sprintf("%v/%s", permutation, storedShape), func(t *testing.T) {
				values := store.Values{"a": store.Object(store.Values{"b": store.Object(store.Values{"c": store.String("preserved")})})}
				changed, err := renameMongoStoreFieldsBySchema(values, schemas, permuted)
				if err != nil || !changed {
					t.Fatalf("ordered nested rename = %t, %v", changed, err)
				}
				x, _ := values["x"].ObjectValue()
				y, _ := x["y"].ObjectValue()
				if value, _ := y["z"].StringValue(); value != "preserved" {
					t.Fatalf("nested rename result = %#v", values)
				}
				if _, stale := values["a"]; stale {
					t.Fatalf("nested rename retained its source = %#v", values)
				}
				if changed, err := renameMongoStoreFieldsBySchema(values, schemas, permuted); err != nil || changed {
					t.Fatalf("idempotent nested rename = %t, %v", changed, err)
				}
			})
		}
		if !reflect.DeepEqual(permuted, originalIntent) {
			t.Fatalf("rename ordering mutated artifact intent: %#v, want %#v", permuted, originalIntent)
		}
	}

	collision := store.Values{
		"a": store.Object(store.Values{"b": store.Object(store.Values{"c": store.String("source")})}),
		"x": store.Object(store.Values{"sentinel": store.String("target")}),
	}
	original := store.CloneValues(collision)
	if _, err := renameMongoStoreFieldsBySchema(collision, schemas, pairs); err == nil || !strings.Contains(err.Error(), "already contains") {
		t.Fatalf("nested container collision error = %v", err)
	}
	collisionJSON, _ := json.Marshal(collision)
	originalJSON, _ := json.Marshal(original)
	if string(collisionJSON) != string(originalJSON) {
		t.Fatalf("nested container collision mutated values: %s, want %s", collisionJSON, originalJSON)
	}
}

func TestRewriteMongoPolymorphicRelationshipSlugsFollowsImmutableFieldShapes(t *testing.T) {
	targets := []schema.RelationshipTarget{
		{CollectionID: "members", CollectionSlug: "members"},
		{CollectionID: "teams", CollectionSlug: "teams"},
	}
	polymorphic := func(name string, hasMany, localized bool) schema.Field {
		return schema.Field{
			Name: name, Type: schema.FieldTypeRelationship, Localized: localized,
			Relationship: &schema.RelationshipField{Polymorphic: true, HasMany: hasMany, Targets: targets},
		}
	}
	fields := []schema.Field{
		{
			Name: "metadata", Type: schema.FieldTypeGroup,
			Nested: &schema.NestedField{Fields: []schema.Field{
				{Name: "relationTo", Type: schema.FieldTypeText},
				{
					Name: "nested", Type: schema.FieldTypeGroup,
					Nested: &schema.NestedField{Fields: []schema.Field{{Name: "relationTo", Type: schema.FieldTypeText}}},
				},
			}},
		},
		{Name: "opaque", Type: schema.FieldTypeJSON},
		{
			Name: "richtext", Type: schema.FieldTypePlugin,
			Plugin: &schema.PluginField{Key: "richtext", ReferenceKeys: []string{"relationTo"}},
		},
		{Name: "opaquePlugin", Type: schema.FieldTypePlugin, Plugin: &schema.PluginField{Key: "opaque"}},
		polymorphic("subject", false, false),
		polymorphic("localizedSubjects", true, true),
		{
			Name: "content", Type: schema.FieldTypeGroup,
			Nested: &schema.NestedField{Fields: []schema.Field{{
				Name: "rows", Type: schema.FieldTypeArray,
				Nested: &schema.NestedField{Fields: []schema.Field{
					polymorphic("reviewer", false, false),
					{Name: "relationTo", Type: schema.FieldTypeText},
				}},
			}}},
		},
		{
			Name: "layout", Type: schema.FieldTypeBlocks,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{{
				Key: "feature", Fields: []schema.Field{
					polymorphic("subject", false, false),
					{Name: "relationTo", Type: schema.FieldTypeText},
				},
			}}},
		},
	}
	reference := func(collection, id string) store.Value {
		return store.Object(store.Values{"relationTo": store.String(collection), "id": store.String(id)})
	}
	values := store.Values{
		"metadata": store.Object(store.Values{
			"relationTo": store.String("authors"),
			"nested":     store.Object(store.Values{"relationTo": store.String("authors")}),
		}),
		"opaque": store.Object(store.Values{"relationTo": store.String("authors")}),
		"richtext": store.Object(store.Values{
			"relationTo": store.String("authors"),
			"children": store.List(store.Object(store.Values{
				"relationTo": store.String("authors"), "collection": store.String("authors"),
			})),
		}),
		"opaquePlugin": store.Object(store.Values{"relationTo": store.String("authors")}),
		"subject":      reference("authors", "author-1"),
		"localizedSubjects": store.Object(store.Values{
			"en": store.List(reference("authors", "author-1"), reference("teams", "team-1")),
			"fr": store.Null(),
		}),
		"content": store.Object(store.Values{"rows": store.List(store.Object(store.Values{
			"_key":       store.String("row-1"),
			"reviewer":   reference("authors", "author-2"),
			"relationTo": store.String("authors"),
		}))}),
		"layout": store.List(
			store.Object(store.Values{
				"_key": store.String("feature-1"), "blockType": store.String("feature"),
				"subject": reference("authors", "author-3"), "relationTo": store.String("authors"),
			}),
			store.Object(store.Values{
				"_key": store.String("unknown-1"), "blockType": store.String("unknown"),
				"subject": reference("authors", "author-4"), "relationTo": store.String("authors"),
			}),
		),
	}
	want := store.Values{
		"metadata": store.Object(store.Values{
			"relationTo": store.String("authors"),
			"nested":     store.Object(store.Values{"relationTo": store.String("authors")}),
		}),
		"opaque": store.Object(store.Values{"relationTo": store.String("authors")}),
		"richtext": store.Object(store.Values{
			"relationTo": store.String("members"),
			"children": store.List(store.Object(store.Values{
				"relationTo": store.String("members"), "collection": store.String("authors"),
			})),
		}),
		"opaquePlugin": store.Object(store.Values{"relationTo": store.String("authors")}),
		"subject":      reference("members", "author-1"),
		"localizedSubjects": store.Object(store.Values{
			"en": store.List(reference("members", "author-1"), reference("teams", "team-1")),
			"fr": store.Null(),
		}),
		"content": store.Object(store.Values{"rows": store.List(store.Object(store.Values{
			"_key":       store.String("row-1"),
			"reviewer":   reference("members", "author-2"),
			"relationTo": store.String("authors"),
		}))}),
		"layout": store.List(
			store.Object(store.Values{
				"_key": store.String("feature-1"), "blockType": store.String("feature"),
				"subject": reference("members", "author-3"), "relationTo": store.String("authors"),
			}),
			store.Object(store.Values{
				"_key": store.String("unknown-1"), "blockType": store.String("unknown"),
				"subject": reference("authors", "author-4"), "relationTo": store.String("authors"),
			}),
		),
	}

	if !rewriteMongoCollectionReferences(fields, values, "authors", "members") {
		t.Fatal("schema-declared polymorphic references were not rewritten")
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("schema-driven relationship rewrite = %#v, want %#v", values, want)
	}
	if rewriteMongoCollectionReferences(fields, values, "authors", "members") {
		t.Fatal("idempotent relationship rewrite reported another change")
	}
}

func TestMongoDBMigrationReferenceFieldSetsUseBeforeAndAfterOwnerSchemas(t *testing.T) {
	relationship := func(name string) schema.Field {
		path, err := query.ParsePath(name)
		if err != nil {
			t.Fatal(err)
		}
		return schema.Field{
			ID: schema.StableID("entries-" + name), Name: name, Path: path, Type: schema.FieldTypeRelationship,
			Relationship: &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{
				{CollectionID: "authors", CollectionSlug: "authors"},
			}},
		}
	}
	beforeSnapshot := mongoDBMigrationTestManifest(t, false, false).Snapshot()
	beforeSnapshot.Collections = append(beforeSnapshot.Collections, schema.Collection{
		ID: "entries", Slug: "entries", Fields: []schema.Field{relationship("oldSubject")},
	})
	before := schema.NewManifest(beforeSnapshot)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[1] = schema.Collection{
		ID: "records", Slug: "records", Fields: []schema.Field{relationship("newSubject")},
	}
	afterSnapshot.Collections[1].Fields[0].Relationship.Targets[0] = schema.RelationshipTarget{CollectionID: "members", CollectionSlug: "members"}
	after := schema.NewManifest(afterSnapshot)
	plan := mongoDBArtifactReplayPlan{
		before: &before, after: after,
		collectionMapping: map[schema.StableID]schema.StableID{"entries": "records", "authors": "members"},
	}
	fieldSchemas := mongoDBMigrationOwnerFieldSchemas(plan, "records")
	if len(fieldSchemas.before) != 1 || fieldSchemas.before[0].Name != "oldSubject" || len(fieldSchemas.after) != 1 || fieldSchemas.after[0].Name != "newSubject" {
		t.Fatalf("immutable owner field schemas = %#v", fieldSchemas)
	}
	values := store.Values{"oldSubject": store.Object(store.Values{
		"relationTo": store.String("authors"), "id": store.String("author-1"),
	})}
	changed := false
	for _, fields := range fieldSchemas.all() {
		changed = rewriteMongoCollectionReferences(fields, values, "authors", "members") || changed
	}
	if !changed {
		t.Fatal("before-schema relationship hidden by simultaneous owner rename was not rewritten")
	}
	oldSubject, _ := values["oldSubject"].ObjectValue()
	if relationTo, _ := oldSubject["relationTo"].StringValue(); relationTo != "members" {
		t.Fatalf("before-schema relationship slug = %q", relationTo)
	}
}

func TestMongoDBRetirementRejectsSurvivingDeclaredPluginReferences(t *testing.T) {
	before := mongoDBMigrationTestManifest(t, false, false).Snapshot()
	before.Collections[0].Fields = append(before.Collections[0].Fields, schema.Field{
		ID: "posts-content", Name: "content", Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{{
			ID: "posts-content-richtext", Name: "richtext", Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
			Plugin: &schema.PluginField{Key: "richtext", ReferenceKeys: []string{"relationTo"}},
		}}},
	})
	before.Collections = append(before.Collections, schema.Collection{ID: "authors", Slug: "authors"})
	after := before
	after.Collections = append([]schema.Collection(nil), before.Collections[:1]...)

	err := validateMongoDBRetirement(before, after, nil, []schema.StableID{"authors"})
	if err == nil || !strings.Contains(err.Error(), "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") || !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("declared plugin retirement admission error = %v", err)
	}

	opaqueBefore := before
	opaqueBefore.Collections = append([]schema.Collection(nil), before.Collections...)
	opaqueBefore.Collections[0].Fields = append([]schema.Field(nil), before.Collections[0].Fields...)
	opaqueBefore.Collections[0].Fields[1].Nested = &schema.NestedField{Fields: []schema.Field{{
		ID: "posts-content-opaque", Name: "opaque", Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "opaque"},
	}}}
	opaqueAfter := opaqueBefore
	opaqueAfter.Collections = append([]schema.Collection(nil), opaqueBefore.Collections[:1]...)
	if err := validateMongoDBRetirement(opaqueBefore, opaqueAfter, nil, []schema.StableID{"authors"}); err != nil {
		t.Fatalf("opaque plugin blocked unrelated retirement: %v", err)
	}

	additiveBefore := mongoDBMigrationTestManifest(t, false, false).Snapshot()
	additiveBefore.Collections = append(additiveBefore.Collections, schema.Collection{ID: "authors", Slug: "authors"})
	additiveAfter := additiveBefore
	additiveAfter.Collections = append([]schema.Collection(nil), additiveBefore.Collections[:1]...)
	additiveAfter.Collections[0].Fields = append(additiveAfter.Collections[0].Fields, before.Collections[0].Fields[1])
	if err := validateMongoDBRetirement(additiveBefore, additiveAfter, nil, []schema.StableID{"authors"}); err != nil {
		t.Fatalf("new empty plugin root blocked unrelated retirement: %v", err)
	}

	globalBefore := before
	globalBefore.Globals = []schema.Collection{{ID: "legacy-settings", Slug: "legacy-settings", Capabilities: schema.Capabilities{Global: true}}}
	globalAfter := globalBefore
	globalAfter.Globals = nil
	if err := validateMongoDBRetirement(globalBefore, globalAfter, nil, []schema.StableID{"legacy-settings"}); err != nil {
		t.Fatalf("collection-slug plugin metadata blocked unrelated global retirement: %v", err)
	}
}

func TestMongoDBRenamePlanRejectsOverlappingSlugRewrites(t *testing.T) {
	tests := map[string][]ridumigration.Rename{
		"chain": {
			{CollectionBefore: "people", CollectionAfter: "members"},
			{CollectionBefore: "members", CollectionAfter: "editors"},
		},
		"swap": {
			{CollectionBefore: "people", CollectionAfter: "members"},
			{CollectionBefore: "members", CollectionAfter: "people"},
		},
	}
	manifest := mongoDBMigrationTestManifest(t, false, false)
	for name, intents := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := compileMongoDBRenamePlan(manifest, manifest, intents)
			if err == nil || !strings.Contains(err.Error(), "RIDU_COLLECTION_SLUG_REWRITE_OVERLAP_UNSAFE") {
				t.Fatalf("overlapping MongoDB slug rewrite error = %v", err)
			}
		})
	}
}

func TestMongoDBV2PlansAndReplaysConfirmedFieldRenameDeterministically(t *testing.T) {
	directory := t.TempDir()
	before := mongoDBMigrationTestManifest(t, true, false)
	if _, err := CreateArtifact(context.Background(), directory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields[0] = mongoDBMigrationTextField(t, "posts-heading", "heading", false, true, false)
	after := schema.NewManifest(afterSnapshot)
	intent := ridumigration.Rename{
		CollectionBefore: "posts", CollectionAfter: "posts",
		FieldBefore: "title", FieldAfter: "heading",
	}
	first, err := CreateArtifactWithOptions(context.Background(), directory, "rename-title", after, time.Unix(2, 0), ArtifactOptions{Renames: []ridumigration.Rename{intent}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	if files[1].Artifact.Planner.Version != mongoDBPlannerVersionV2 {
		t.Fatalf("semantic planner = %q", files[1].Artifact.Planner.Version)
	}
	kinds := mongoDBMigrationStepKinds(files[1].Artifact)
	wantKinds := []ridumigration.StepKind{
		ridumigration.StepMongoDBDropIndex,
		ridumigration.StepRenameContent,
		ridumigration.StepMongoDBCreateIndex,
		ridumigration.StepMongoDBAssertSchema,
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("field rename steps = %v, want %v", kinds, wantKinds)
	}
	if files[1].Artifact.Phases[0].Mode != ridumigration.PhaseNoTransaction || files[1].Artifact.Phases[1].Mode != ridumigration.PhaseTransaction || files[1].Artifact.Phases[2].Mode != ridumigration.PhaseNoTransaction {
		t.Fatalf("field rename phase modes = %#v", files[1].Artifact.Phases)
	}
	replay, err := prepareMongoDBArtifactReplay(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if replay[1].steps[0].dropIndex.resourceID != "posts" || !reflect.DeepEqual(replay[1].steps[1].rename, intent) || replay[1].steps[2].index.resourceID != "posts" {
		t.Fatalf("field rename replay = %#v", replay[1])
	}

	secondDirectory := t.TempDir()
	if _, err := CreateArtifact(context.Background(), secondDirectory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	second, err := CreateArtifactWithOptions(context.Background(), secondDirectory, "rename-title", after, time.Unix(2, 0), ArtifactOptions{Renames: []ridumigration.Rename{intent}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != second.Name || first.Checksum != second.Checksum {
		t.Fatalf("deterministic v1 -> v2 artifact = %s/%s and %s/%s", first.Name, first.Checksum, second.Name, second.Checksum)
	}
}

func TestMongoDBV2PlansTypedCollectionRenameAndBindsPhysicalIdentity(t *testing.T) {
	directory := t.TempDir()
	before := mongoDBMigrationTestManifest(t, false, false)
	if _, err := CreateArtifact(context.Background(), directory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].ID = "articles"
	afterSnapshot.Collections[0].Slug = "articles"
	after := schema.NewManifest(afterSnapshot)
	intent := ridumigration.Rename{CollectionBefore: "posts", CollectionAfter: "articles"}
	if _, err := CreateArtifactWithOptions(context.Background(), directory, "rename-posts", after, time.Unix(2, 0), ArtifactOptions{Renames: []ridumigration.Rename{intent}}); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	kinds := mongoDBMigrationStepKinds(files[1].Artifact)
	if len(kinds) < 3 || kinds[0] != ridumigration.StepMongoDBRenameResource || kinds[1] != ridumigration.StepRenameContent || kinds[len(kinds)-1] != ridumigration.StepMongoDBAssertSchema {
		t.Fatalf("collection rename steps = %v", kinds)
	}
	replay, err := prepareMongoDBArtifactReplay(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	payload := replay[1].steps[0].resourceRename
	if payload.BeforeID != "posts" || payload.AfterID != "articles" {
		t.Fatalf("typed resource rename = %#v", payload)
	}
	if replay[1].steps[0].resourceRenameContentRequired || replay[1].steps[0].resourceRenameVersionRequired {
		t.Fatalf("empty unindexed rename unexpectedly requires source namespaces: %#v", replay[1].steps[0])
	}

	tampered := files[1].Artifact
	tamperedPayload, err := ridumigration.MarshalStepPayload(ridumigration.MongoDBRenameResourcePayload{BeforeID: "posts", AfterID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	for phaseIndex := range tampered.Phases {
		for stepIndex := range tampered.Phases[phaseIndex].Steps {
			if tampered.Phases[phaseIndex].Steps[stepIndex].Kind == ridumigration.StepMongoDBRenameResource {
				tampered.Phases[phaseIndex].Steps[stepIndex].Payload = tamperedPayload
			}
		}
	}
	mongoDBMigrationRecomputePhysicalDigests(t, &tampered)
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "do not exactly match") {
		t.Fatalf("tampered physical rename binding = %v", err)
	}
}

func TestMongoDBRenameNamespaceAdmissionUsesImmutablePhysicalContract(t *testing.T) {
	tests := []struct {
		name           string
		beforeExists   bool
		afterExists    bool
		sourceRequired bool
		wantPerform    bool
		wantError      bool
	}{
		{name: "empty unindexed source", sourceRequired: false},
		{name: "required source missing", sourceRequired: true, wantError: true},
		{name: "resumed target", afterExists: true, sourceRequired: true},
		{name: "perform", beforeExists: true, sourceRequired: true, wantPerform: true},
		{name: "ambiguous both present", beforeExists: true, afterExists: true, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			perform, err := mongoDBRenameNamespaceAction(test.beforeExists, test.afterExists, test.sourceRequired)
			if perform != test.wantPerform || (err != nil) != test.wantError {
				t.Fatalf("action = %t, %v", perform, err)
			}
		})
	}

	indexed := mongoDBMigrationTestManifest(t, true, false)
	plans, err := mongoPhysicalIndexPlans(indexed)
	if err != nil {
		t.Fatal(err)
	}
	content, versions := mongoDBRenameSourceRequirements(plans, "posts")
	if !content || versions {
		t.Fatalf("indexed source requirements = content %t, versions %t", content, versions)
	}
}

func TestMongoDBSemanticPhaseSchedulesOneReferenceRebuildForMultipleRenames(t *testing.T) {
	phase := mongoDBArtifactReplayPhase{steps: []mongoDBArtifactReplayStep{
		{kind: ridumigration.StepRenameContent},
		{kind: ridumigration.StepRenameContent},
		{kind: ridumigration.StepRetireResources},
	}}
	if !mongoDBMigrationPhaseNeedsReferenceRebuild(phase) {
		t.Fatal("multiple rename intents did not schedule their one phase-level reference rebuild")
	}
	phase.steps = []mongoDBArtifactReplayStep{{kind: ridumigration.StepDataTransform}, {kind: ridumigration.StepRetireResources}}
	if mongoDBMigrationPhaseNeedsReferenceRebuild(phase) {
		t.Fatal("rename-free semantic phase scheduled a reference rebuild")
	}
}

func TestMongoDBV2DataOnlyTransformAndNoOpAdmission(t *testing.T) {
	directory := t.TempDir()
	manifest := mongoDBMigrationTestManifest(t, false, false)
	if _, err := CreateArtifact(context.Background(), directory, "initial", manifest, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "normalize-titles", Checksum: strings.Repeat("a", 64)}
	if _, err := CreateArtifactWithOptions(context.Background(), directory, "normalize", manifest, time.Unix(2, 0), ArtifactOptions{DataTransforms: []ridumigration.DataTransformDescriptor{descriptor}}); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact := files[1].Artifact
	if artifact.FromDigest != artifact.ToDigest || !reflect.DeepEqual(mongoDBMigrationStepKinds(artifact), []ridumigration.StepKind{ridumigration.StepDataTransform, ridumigration.StepMongoDBAssertSchema}) {
		t.Fatalf("data-only transform artifact = %#v", artifact)
	}
	if _, err := prepareMongoDBArtifactReplay(context.Background(), files); err != nil {
		t.Fatal(err)
	}

	noOpDirectory := t.TempDir()
	if _, err := CreateArtifact(context.Background(), noOpDirectory, "initial", manifest, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifactWithOptions(context.Background(), noOpDirectory, "noop", manifest, time.Unix(2, 0), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "schema is current") {
		t.Fatalf("unbound no-op error = %v", err)
	}
}

func TestMongoDBV2TransformRequiresChecksumRegistrationAndDestructiveReview(t *testing.T) {
	directory := t.TempDir()
	before := mongoDBMigrationTestManifest(t, false, false)
	if _, err := CreateArtifact(context.Background(), directory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	afterSnapshot := before.Snapshot()
	field := &afterSnapshot.Collections[0].Fields[0]
	field.Required = true
	after := schema.NewManifest(afterSnapshot)
	descriptor := ridumigration.DataTransformDescriptor{Name: "backfill-title", Checksum: strings.Repeat("b", 64)}
	options := ArtifactOptions{DataTransforms: []ridumigration.DataTransformDescriptor{descriptor}}
	if _, err := CreateArtifactWithOptions(context.Background(), directory, "backfill", after, time.Unix(2, 0), options); err == nil {
		t.Fatal("transformed schema change bypassed destructive review")
	} else {
		var safety *SafetyError
		if !errors.As(err, &safety) || len(safety.Risks) == 0 {
			t.Fatalf("transformed schema safety error = %v", err)
		}
	}
	options.AllowDestructive = true
	if _, err := CreateArtifactWithOptions(context.Background(), directory, "backfill", after, time.Unix(2, 0), options); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireMongoDBDataTransformRegistry(files, nil); err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("missing compiled transform registration = %v", err)
	}
	wrong := ridumigration.DataTransform{
		DataTransformDescriptor: ridumigration.DataTransformDescriptor{Name: descriptor.Name, Checksum: strings.Repeat("c", 64)},
		Up:                      func(context.Context, ridumigration.DataTransaction) error { return nil }, Down: func(context.Context, ridumigration.DataTransaction) error { return nil },
	}
	registry, err := newMongoDBDataTransformRegistry([]ridumigration.DataTransform{wrong})
	if err != nil {
		t.Fatal(err)
	}
	if err := requireMongoDBDataTransformRegistry(files, registry); err == nil || !strings.Contains(err.Error(), "checksum differs") {
		t.Fatalf("changed compiled transform checksum = %v", err)
	}
}

func TestMongoDBPlanAndStatusRejectTransformNameChecksumReuseAcrossHistory(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := mongoDBMigrationTestManifest(t, false, false)
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	initial := ridumigration.DataTransformDescriptor{Name: "normalize-titles", Checksum: strings.Repeat("a", 64)}
	if _, err := CreateArtifactWithOptions(ctx, directory, "initial-transform", manifest, time.Unix(2, 0), ArtifactOptions{
		DataTransforms: []ridumigration.DataTransformDescriptor{initial},
	}); err != nil {
		t.Fatal(err)
	}

	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	changed := ridumigration.DataTransformDescriptor{Name: initial.Name, Checksum: strings.Repeat("b", 64)}
	second, err := buildMongoDBArtifactWithOptions(
		ctx, "changed-transform", &manifest, manifest, files[len(files)-1].Artifact.Planner.Version,
		currentMongoDBPlannerContract(), ArtifactOptions{DataTransforms: []ridumigration.DataTransformDescriptor{changed}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, second.Name, second, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}

	backend := &Store{}
	for name, inspect := range map[string]func() error{
		"plan": func() error {
			_, err := backend.ArtifactPlan(ctx, directory)
			return err
		},
		"status": func() error {
			_, err := backend.ArtifactStatus(ctx, directory)
			return err
		},
		"manifest-bound inspection": func() error {
			_, err := InspectArtifacts(ctx, Config{}, directory, manifest)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := inspect(); err == nil || !strings.Contains(err.Error(), "use a new transform name") {
				t.Fatalf("%s changed transform identity error = %v", name, err)
			}
		})
	}
}

func TestMongoDBV2RetirementDropsNamespacesBeforeCreatingTargetIndexes(t *testing.T) {
	directory := t.TempDir()
	before := mongoDBMigrationTestManifest(t, false, false)
	if _, err := CreateArtifact(context.Background(), directory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections = []schema.Collection{{
		ID: "pages", Slug: "pages", Labels: schema.CollectionLabels{Singular: "Page", Plural: "Pages"},
		Fields: []schema.Field{mongoDBMigrationTextField(t, "pages-title", "title", false, true, false)},
	}}
	after := schema.NewManifest(afterSnapshot)
	if _, err := CreateArtifactWithOptions(context.Background(), directory, "replace-posts", after, time.Unix(2, 0), ArtifactOptions{AllowDestructive: true}); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	kinds := mongoDBMigrationStepKinds(files[1].Artifact)
	retireIndex, dropIndex, firstCreateIndex := -1, -1, -1
	for index, kind := range kinds {
		switch kind {
		case ridumigration.StepRetireResources:
			retireIndex = index
		case ridumigration.StepMongoDBDropResources:
			dropIndex = index
		case ridumigration.StepMongoDBCreateIndex:
			if firstCreateIndex < 0 {
				firstCreateIndex = index
			}
		}
	}
	if retireIndex < 0 || dropIndex <= retireIndex || firstCreateIndex <= dropIndex || kinds[len(kinds)-1] != ridumigration.StepMongoDBAssertSchema {
		t.Fatalf("retirement/create topology = %v", kinds)
	}
	replay, err := prepareMongoDBArtifactReplay(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay[1].steps[retireIndex].resourceIDs, []schema.StableID{"posts"}) || !reflect.DeepEqual(replay[1].steps[dropIndex].resourceIDs, []schema.StableID{"posts"}) {
		t.Fatalf("retirement replay = %#v", replay[1])
	}
}

func mongoDBMigrationStepKinds(artifact ridumigration.Artifact) []ridumigration.StepKind {
	var result []ridumigration.StepKind
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			result = append(result, step.Kind)
		}
	}
	return result
}
