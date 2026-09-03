package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresCollectionRenamePreflightRejectsValueOnlyCTEDataTampering(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	directory := t.TempDir()

	beforeCollection := renameBindingManifestCollection("articles", "articles")
	afterCollection := renameBindingManifestCollection("posts", "posts")
	before := renameBindingManifest(beforeCollection)
	after := renameBindingManifest(afterCollection)
	initial, err := BuildArtifact(ctx, "initial-rename-guard", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.pool.Exec(ctx, "INSERT INTO "+quote(collectionTable(beforeCollection.ID))+" (id) VALUES ('document-1')"); err != nil {
		t.Fatal(err)
	}

	rename, err := BuildArtifact(ctx, "rename-guarded-collection", &before, after, []Rename{{
		Kind: RenameCollection, BeforeCollection: beforeCollection, AfterCollection: afterCollection,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	insertSQLBeforeCollectionRename(t, &rename, beforeCollection.Slug, afterCollection.Slug,
		"WITH changed AS (UPDATE "+quote(collectionTable(afterCollection.ID))+" SET updated_at = updated_at + interval '1 hour' RETURNING 1) SELECT count(*) FROM changed",
	)
	recomputeTestPhysicalDigests(t, &rename)
	if err := rename.Validate(); err != nil {
		t.Fatalf("generic CTE tamper fixture: %v", err)
	}
	if err := validatePostgresResourceRetirementTopology(rename); err != nil {
		t.Fatalf("CTE fixture should reach exact planner binding: %v", err)
	}
	if _, err := migrationartifact.Create(directory, rename.Name, rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	err = backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true})
	if err == nil || !strings.Contains(err.Error(), "execution phases do not exactly match deterministic Atlas plan") {
		t.Fatalf("CTE data tamper apply = %v", err)
	}

	var sourceRows int
	if err := backend.pool.QueryRow(ctx, "SELECT count(*) FROM "+quote(collectionTable(beforeCollection.ID))).Scan(&sourceRows); err != nil || sourceRows != 1 {
		t.Fatalf("rolled-back source rows = %d, %v", sourceRows, err)
	}
	var targetExists bool
	if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, collectionTable(afterCollection.ID)).Scan(&targetExists); err != nil || targetExists {
		t.Fatalf("rolled-back target table exists = %t, %v", targetExists, err)
	}
}

func TestPostgresCollectionAndNestedReferenceRenameRewritesCurrentAndOldOwnerVersions(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	directory := t.TempDir()
	beforeCollection, afterCollection, fieldPairs := nestedReferenceIdentityRenameCollections(t)
	before := renameBindingManifest(beforeCollection)
	after := renameBindingManifest(afterCollection)
	initial, err := BuildArtifact(ctx, "initial-nested-reference-rename", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	plugin := store.Object(store.Values{
		"node": store.Object(store.Values{
			"relationTo": store.String("articles"), "id": store.String("owner"),
			"migrationMarker": store.String("nested-plugin-rename"),
		}),
	})
	document, err := transaction.Create(ctx, store.CreateRequest{
		Collection: beforeCollection, ID: "owner", Status: store.StatusPublished,
		Values: store.Values{"content": store.Object(store.Values{
			"document": plugin,
			"related": store.Object(store.Values{
				"relationTo": store.String("articles"), "id": store.String("owner"),
				"migrationMarker": store.String("nested-polymorphic-rename"),
			}),
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, beforeCollection, document, 10); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	rename, err := BuildArtifact(ctx, "rename-nested-reference-owner", &before, after, []Rename{{
		Kind: RenameCollection, BeforeCollection: beforeCollection, AfterCollection: afterCollection, Fields: fieldPairs,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, rename.Name, rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}

	var current, versions []byte
	if err := backend.pool.QueryRow(ctx, "SELECT "+quote(fieldColumn("posts-body"))+" FROM "+quote(collectionTable("posts"))+" WHERE id = 'owner'").Scan(&current); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT jsonb_agg(snapshot ORDER BY revision) FROM ridu_versions WHERE collection_id = 'posts' AND document_id = 'owner'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	for label, encoded := range map[string][]byte{"current": current, "versions": versions} {
		assertMarkedCollectionSlugReference(t, label, encoded, "nested-plugin-rename", "posts")
		assertMarkedCollectionSlugReference(t, label, encoded, "nested-polymorphic-rename", "posts")
	}
}

func TestPostgresEarlierTargetRenameRewritesLaterRenamedOwnerCurrentAndVersions(t *testing.T) {
	backend := migrationArtifactTestBackend(t)
	ctx := context.Background()
	directory := t.TempDir()
	beforeTarget, afterTarget, beforeOwner, afterOwner, ownerFields := crossOwnerReferenceRenameCollections(t)
	before := renameBindingManifest(beforeTarget, beforeOwner)
	after := renameBindingManifest(afterTarget, afterOwner)
	initial, err := BuildArtifact(ctx, "initial-cross-owner-reference-rename", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: beforeTarget, ID: "article-1", Values: store.Values{}}); err != nil {
		t.Fatal(err)
	}
	plugin := store.Object(store.Values{"node": store.Object(store.Values{
		"relationTo": store.String("articles"), "id": store.String("article-1"),
		"migrationMarker": store.String("cross-owner-plugin"),
	})})
	opaque := store.Object(store.Values{"node": store.Object(store.Values{
		"relationTo": store.String("articles"), "id": store.String("article-1"),
		"migrationMarker": store.String("cross-owner-opaque"),
	})})
	ownerDocument, err := transaction.Create(ctx, store.CreateRequest{
		Collection: beforeOwner, ID: "page-1", Status: store.StatusPublished,
		Values: store.Values{"content": store.Object(store.Values{
			"document": plugin,
			"related": store.Object(store.Values{
				"relationTo": store.String("articles"), "id": store.String("article-1"),
				"migrationMarker": store.String("cross-owner-polymorphic"),
			}),
			"opaque": opaque,
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.(store.VersionTransaction).SaveVersion(ctx, beforeOwner, ownerDocument, 10); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	rename, err := BuildArtifact(ctx, "rename-target-before-referring-owner", &before, after, []Rename{
		{Kind: RenameCollection, BeforeCollection: beforeTarget, AfterCollection: afterTarget},
		{Kind: RenameCollection, BeforeCollection: beforeOwner, AfterCollection: afterOwner, Fields: ownerFields},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, rename.Name, rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}

	var current, versions []byte
	if err := backend.pool.QueryRow(ctx, "SELECT "+quote(fieldColumn("landing-body"))+" FROM "+quote(collectionTable("landing"))+" WHERE id = 'page-1'").Scan(&current); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT jsonb_agg(snapshot ORDER BY revision) FROM ridu_versions WHERE collection_id = 'landing' AND document_id = 'page-1'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	for label, encoded := range map[string][]byte{"current": current, "versions": versions} {
		assertMarkedCollectionSlugReference(t, label, encoded, "cross-owner-plugin", "posts")
		assertMarkedCollectionSlugReference(t, label, encoded, "cross-owner-polymorphic", "posts")
		assertMarkedCollectionSlugReference(t, label, encoded, "cross-owner-opaque", "articles")
	}
	var applied int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil || applied != 2 {
		t.Fatalf("cross-owner migration ledger rows = %d, %v", applied, err)
	}
}

func crossOwnerReferenceRenameCollections(t *testing.T) (
	schema.Collection,
	schema.Collection,
	schema.Collection,
	schema.Collection,
	[]FieldRename,
) {
	t.Helper()
	path := func(value string) query.Path {
		parsed, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	beforeTarget := schema.Collection{ID: "articles", Slug: "articles", Fields: []schema.Field{}}
	afterTarget := schema.Collection{ID: "posts", Slug: "posts", Fields: []schema.Field{}}
	beforePlugin := schema.Field{
		ID: "z-pages-content-document", Name: "document", Path: path("content.document"),
		Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "richtext", Config: json.RawMessage(`{"version":1}`), ReferenceKeys: []string{"relationTo"}},
	}
	beforeRelationship := schema.Field{
		ID: "z-pages-content-related", Name: "related", Path: path("content.related"),
		Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{
			Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: beforeTarget.ID, CollectionSlug: beforeTarget.Slug}},
			OnDelete: schema.ReferenceDeleteNullify,
		},
	}
	beforeOpaque := schema.Field{
		ID: "z-pages-content-opaque", Name: "opaque", Path: path("content.opaque"),
		Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "opaque", Config: json.RawMessage(`{"version":1}`)},
	}
	beforeRoot := schema.Field{
		ID: "z-pages-content", Name: "content", Path: path("content"),
		Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{beforePlugin, beforeRelationship, beforeOpaque}},
	}
	afterPlugin := beforePlugin
	afterPlugin.ID, afterPlugin.Name, afterPlugin.Path = "landing-body-editor", "editor", path("body.editor")
	afterRelationship := beforeRelationship
	afterRelationship.ID, afterRelationship.Name, afterRelationship.Path = "landing-body-links", "links", path("body.links")
	afterRelationship.Relationship = &schema.RelationshipField{
		Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: afterTarget.ID, CollectionSlug: afterTarget.Slug}},
		OnDelete: schema.ReferenceDeleteNullify,
	}
	afterOpaque := beforeOpaque
	afterOpaque.ID, afterOpaque.Path = "landing-body-opaque", path("body.opaque")
	afterRoot := schema.Field{
		ID: "landing-body", Name: "body", Path: path("body"),
		Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{afterPlugin, afterRelationship, afterOpaque}},
	}
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	beforeOwner := schema.Collection{
		ID: "z-pages", Slug: "z-pages", Capabilities: schema.Capabilities{Versions: true}, Versions: versions,
		Fields: []schema.Field{beforeRoot},
	}
	afterOwner := schema.Collection{
		ID: "landing", Slug: "landing", Capabilities: schema.Capabilities{Versions: true}, Versions: versions,
		Fields: []schema.Field{afterRoot},
	}
	return beforeTarget, afterTarget, beforeOwner, afterOwner, []FieldRename{
		{Before: beforeRoot, After: afterRoot},
		{Before: beforePlugin, After: afterPlugin},
		{Before: beforeOpaque, After: afterOpaque},
		{Before: beforeRelationship, After: afterRelationship},
	}
}

func nestedReferenceIdentityRenameCollections(t *testing.T) (schema.Collection, schema.Collection, []FieldRename) {
	t.Helper()
	path := func(value string) query.Path {
		parsed, err := query.ParsePath(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	beforePlugin := schema.Field{
		ID: "articles-content-document", Name: "document", Path: path("content.document"),
		Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "richtext", Config: json.RawMessage(`{"version":1}`), ReferenceKeys: []string{"relationTo"}},
	}
	beforeRelationship := schema.Field{
		ID: "articles-content-related", Name: "related", Path: path("content.related"),
		Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{
			Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: "articles", CollectionSlug: "articles"}},
			OnDelete: schema.ReferenceDeleteNullify,
		},
	}
	beforeRoot := schema.Field{
		ID: "articles-content", Name: "content", Path: path("content"),
		Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{beforePlugin, beforeRelationship}},
	}
	afterPlugin := beforePlugin
	afterPlugin.ID, afterPlugin.Name, afterPlugin.Path = "posts-body-editor", "editor", path("body.editor")
	afterRelationship := beforeRelationship
	afterRelationship.ID, afterRelationship.Name, afterRelationship.Path = "posts-body-links", "links", path("body.links")
	afterRelationship.Relationship = &schema.RelationshipField{
		Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: "posts", CollectionSlug: "posts"}},
		OnDelete: schema.ReferenceDeleteNullify,
	}
	afterRoot := schema.Field{
		ID: "posts-body", Name: "body", Path: path("body"),
		Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{afterPlugin, afterRelationship}},
	}
	before := schema.Collection{
		ID: "articles", Slug: "articles", Capabilities: schema.Capabilities{Versions: true}, Versions: versions,
		Fields: []schema.Field{beforeRoot},
	}
	after := schema.Collection{
		ID: "posts", Slug: "posts", Capabilities: schema.Capabilities{Versions: true}, Versions: versions,
		Fields: []schema.Field{afterRoot},
	}
	return before, after, []FieldRename{
		{Before: beforeRoot, After: afterRoot},
		{Before: beforePlugin, After: afterPlugin},
		{Before: beforeRelationship, After: afterRelationship},
	}
}

func renameBindingManifestCollection(id, slug string) schema.Collection {
	return schema.Collection{ID: schema.StableID(id), Slug: schema.CollectionSlug(slug), Fields: []schema.Field{}}
}
