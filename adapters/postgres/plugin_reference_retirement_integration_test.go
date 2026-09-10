package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresPluginReferenceRetirementPreventsCurrentAndVersionResurrectionAfterIdentityReuse(t *testing.T) {
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	backend := collectionRetirementBackend(t, ctx, baseURL)
	directory := t.TempDir()

	beforeConfig := pluginReferenceLiveConfig(true, true)
	before, err := ridu.Resolve(beforeConfig)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := BuildArtifact(ctx, "initial-plugin-reference-state", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial-plugin-reference-state", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeApp, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	targetID := "reused-person-id"
	ownerID := "surviving-entry-id"
	if _, err := beforeApp.Local().Import(ctx, "people", store.Values{"name": store.String("Old person")}, ridu.ImportOptions{
		ID: targetID, Status: store.StatusPublished,
	}, nil); err != nil {
		t.Fatal(err)
	}
	legacy := livePluginReferenceDocument(targetID, "legacy-plugin-reference")
	entry, err := beforeApp.Local().Import(ctx, "entries", store.Values{
		"title": store.String("Surviving entry"), "content": legacy,
	}, ridu.ImportOptions{ID: ownerID, Status: store.StatusPublished}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = beforeApp.Local().PublishChanges(ctx, "entries", entry.ID, store.Values{"content": legacy}, entry.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	global, err := beforeApp.Local().UpdateGlobal(ctx, "site", store.Values{
		"title": store.String("Site"), "content": legacy,
	}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	global, err = beforeApp.Local().PublishGlobalChanges(ctx, "site", store.Values{"content": legacy}, global.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if versions, err := beforeApp.Local().Versions(ctx, "entries", entry.ID, nil); err != nil || len(versions) < 2 ||
		!pluginReferenceVersionsContain(versions, "legacy-plugin-reference") {
		t.Fatalf("seeded collection plugin versions = %#v, %v", versions, err)
	}
	if versions, err := beforeApp.Local().GlobalVersions(ctx, "site", nil); err != nil || len(versions) < 2 ||
		!pluginReferenceVersionsContain(versions, "legacy-plugin-reference") {
		t.Fatalf("seeded global plugin versions = %#v, %v", versions, err)
	}

	afterConfig := pluginReferenceLiveConfig(false, false)
	after, err := ridu.Resolve(afterConfig)
	if err != nil {
		t.Fatal(err)
	}
	retirement, err := BuildArtifact(ctx, "retire-plugin-reference-state", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "retire-plugin-reference-state", retirement, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); !errors.Is(err, ErrMaintenanceRequired) {
		t.Fatalf("plugin reference retirement without maintenance admission = %v", err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}

	readdedConfig := pluginReferenceLiveConfig(true, true)
	readded, err := ridu.Resolve(readdedConfig)
	if err != nil {
		t.Fatal(err)
	}
	readdition, err := BuildArtifact(ctx, "readd-plugin-reference-state", &after, readded, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "readd-plugin-reference-state", readdition, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	readdedApp, err := ridu.New(readdedConfig, backend)
	if err != nil {
		t.Fatal(err)
	}

	currentEntry, err := readdedApp.Local().Find(ctx, "entries", ownerID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := currentEntry.Values["content"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("retired current collection plugin value reattached: %#v", value)
	}
	currentGlobal, err := readdedApp.Local().Global(ctx, "site", nil)
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := currentGlobal.Values["content"]; exists && value.Kind() != store.ValueNull {
		t.Fatalf("retired current global plugin value reattached: %#v", value)
	}
	if versions, err := readdedApp.Local().Versions(ctx, "entries", ownerID, nil); err != nil || len(versions) != 0 {
		t.Fatalf("retired collection plugin versions reattached = %#v, %v", versions, err)
	}
	if versions, err := readdedApp.Local().GlobalVersions(ctx, "site", nil); err != nil || len(versions) != 0 {
		t.Fatalf("retired global plugin versions reattached = %#v, %v", versions, err)
	}

	if _, err := readdedApp.Local().Import(ctx, "people", store.Values{"name": store.String("New person")}, ridu.ImportOptions{
		ID: targetID, Status: store.StatusPublished,
	}, nil); err != nil {
		t.Fatal(err)
	}
	fresh := livePluginReferenceDocument(targetID, "fresh-plugin-reference")
	currentEntry, err = readdedApp.Local().Find(ctx, "entries", ownerID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readdedApp.Local().PublishChanges(ctx, "entries", ownerID, store.Values{"content": fresh}, currentEntry.Revision, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readdedApp.Local().PublishGlobalChanges(ctx, "site", store.Values{"content": fresh}, currentGlobal.Revision, nil); err != nil {
		t.Fatal(err)
	}
	entryVersions, err := readdedApp.Local().Versions(ctx, "entries", ownerID, nil)
	if err != nil || len(entryVersions) != 1 || !pluginReferenceVersionsContain(entryVersions, "fresh-plugin-reference") ||
		pluginReferenceVersionsContain(entryVersions, "legacy-plugin-reference") {
		t.Fatalf("reincarnated collection plugin versions = %#v, %v", entryVersions, err)
	}
	globalVersions, err := readdedApp.Local().GlobalVersions(ctx, "site", nil)
	if err != nil || len(globalVersions) != 1 || !pluginReferenceVersionsContain(globalVersions, "fresh-plugin-reference") ||
		pluginReferenceVersionsContain(globalVersions, "legacy-plugin-reference") {
		t.Fatalf("reincarnated global plugin versions = %#v, %v", globalVersions, err)
	}
	var oldSnapshots int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_versions
WHERE collection_id = ANY($1::text[]) AND snapshot::text LIKE '%legacy-plugin-reference%'`, []string{
		string(pluginReferenceResourceID(readded.Snapshot(), "entries")),
		string(pluginReferenceGlobalID(readded.Snapshot(), "site")),
	}).Scan(&oldSnapshots); err != nil || oldSnapshots != 0 {
		t.Fatalf("legacy plugin snapshots after stable/document identity reuse = %d, %v", oldSnapshots, err)
	}
}

func TestPostgresStableSlugRewriteCoversPluginGlobalPolymorphicAndVersionsBeforeReuse(t *testing.T) {
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backend := collectionRetirementBackend(t, ctx, baseURL)
	directory := t.TempDir()

	before := pluginSlugRewriteManifest("people", false)
	initial, err := BuildArtifact(ctx, "initial-plugin-slug-state", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial-plugin-slug-state", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	beforeSnapshot := before.Snapshot()
	target := beforeSnapshot.Collections[0]
	owner := beforeSnapshot.Collections[1]
	global := beforeSnapshot.Globals[0]
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: target, ID: "shared-target", Values: store.Values{}}); err != nil {
		t.Fatal(err)
	}
	pluginValue := historicalPluginReferenceDocument("shared-target", "slug-rewrite")
	opaqueValue := historicalPluginReferenceDocument("shared-target", "opaque-sibling")
	structuredValue := store.Object(store.Values{
		"reference": pluginValue,
		"opaque":    opaqueValue,
	})
	ownerDocument, err := transaction.Create(ctx, store.CreateRequest{
		Collection: owner, ID: "owner", Status: store.StatusPublished,
		Values: store.Values{
			"content":    pluginValue,
			"structured": structuredValue,
			"related": store.Object(store.Values{
				"relationTo": store.String("people"), "id": store.String("shared-target"),
				"migrationMarker": store.String("polymorphic-slug-rewrite"),
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	globalDocument, err := transaction.Create(ctx, store.CreateRequest{
		Collection: global, ID: "site", Status: store.StatusPublished,
		Values: store.Values{"content": pluginValue, "structured": structuredValue},
	})
	if err != nil {
		t.Fatal(err)
	}
	versions := transaction.(store.VersionTransaction)
	if _, err := versions.SaveVersion(ctx, owner, ownerDocument, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(ctx, global, globalDocument, 10); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	after := pluginSlugRewriteManifest("members", false)
	rename, err := BuildArtifact(ctx, "rewrite-stable-plugin-slug", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStepKind(t, rename, ridumigration.StepRenameContent) || artifactHasTableRename(rename, "people-target", "people-target") {
		t.Fatalf("same-ID slug plan = %#v", artifactTestSteps(t, rename))
	}
	if _, err := migrationartifact.Create(directory, "rewrite-stable-plugin-slug", rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}

	reused := pluginSlugRewriteManifest("members", true)
	reuseArtifact, err := BuildArtifact(ctx, "reuse-old-plugin-slug", &after, reused, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "reuse-old-plugin-slug", reuseArtifact, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}

	var currentPlugin, currentStructured, currentPolymorphic, currentGlobal, currentGlobalStructured, versionSnapshots []byte
	if err := backend.pool.QueryRow(ctx, "SELECT "+quote(fieldColumn("entries-content"))+", "+quote(fieldColumn("entries-structured"))+", "+quote(fieldColumn("entries-related"))+" FROM "+quote(collectionTable("entries"))+" WHERE id = 'owner'").
		Scan(&currentPlugin, &currentStructured, &currentPolymorphic); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, "SELECT "+quote(fieldColumn("site-content"))+", "+quote(fieldColumn("site-structured"))+" FROM "+quote(collectionTable("site"))+" WHERE id = 'site'").
		Scan(&currentGlobal, &currentGlobalStructured); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT jsonb_agg(snapshot ORDER BY collection_id) FROM ridu_versions
WHERE collection_id = ANY($1::text[])`, []string{"entries", "site"}).Scan(&versionSnapshots); err != nil {
		t.Fatal(err)
	}
	for label, encoded := range map[string][]byte{
		"collection plugin current": currentPlugin,
		"polymorphic current":       currentPolymorphic,
		"global plugin current":     currentGlobal,
	} {
		assertOnlyCollectionSlugReferences(t, label, encoded, "members")
	}
	for label, encoded := range map[string][]byte{
		"collection nested current":  currentStructured,
		"global nested current":      currentGlobalStructured,
		"collection/global versions": versionSnapshots,
	} {
		assertMarkedCollectionSlugReference(t, label, encoded, "slug-rewrite", "members")
		assertMarkedCollectionSlugReference(t, label, encoded, "opaque-sibling", "people")
	}
	assertMarkedCollectionSlugReference(t, "collection/global versions", versionSnapshots, "polymorphic-slug-rewrite", "members")
}

func TestPostgresOverlappingSlugRewriteFailsBeforeCurrentVersionOrLedgerMutation(t *testing.T) {
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backend := collectionRetirementBackend(t, ctx, baseURL)
	directory := t.TempDir()

	before := overlappingSlugLiveManifest("people", "members")
	initial, err := BuildArtifact(ctx, "initial-overlapping-slug-state", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, initial.Name, initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	snapshot := before.Snapshot()
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	for index, id := range []string{"person", "member"} {
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: snapshot.Collections[index], ID: id, Values: store.Values{}}); err != nil {
			t.Fatal(err)
		}
	}
	value := overlappingSlugReferenceValue()
	entry, err := transaction.Create(ctx, store.CreateRequest{
		Collection: snapshot.Collections[2], ID: "owner", Status: store.StatusPublished,
		Values: store.Values{"content": value},
	})
	if err != nil {
		t.Fatal(err)
	}
	global, err := transaction.Create(ctx, store.CreateRequest{
		Collection: snapshot.Globals[0], ID: "site", Status: store.StatusPublished,
		Values: store.Values{"content": value},
	})
	if err != nil {
		t.Fatal(err)
	}
	versions := transaction.(store.VersionTransaction)
	if _, err := versions.SaveVersion(ctx, snapshot.Collections[2], entry, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := versions.SaveVersion(ctx, snapshot.Globals[0], global, 10); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	after := overlappingSlugLiveManifest("members", "people")
	forged := overlappingSlugRewriteArtifact(t, before, after)
	forgedFile, err := migrationartifact.Create(directory, forged.Name, forged, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err == nil || !strings.Contains(err.Error(), codeCollectionSlugRewriteOverlapUnsafe) {
		t.Fatalf("overlapping live rewrite = %v", err)
	}

	var currentCollection, currentGlobal, versionSnapshots []byte
	if err := backend.pool.QueryRow(ctx, "SELECT "+quote(fieldColumn("entries-content"))+" FROM "+quote(collectionTable("entries"))+" WHERE id = 'owner'").Scan(&currentCollection); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, "SELECT "+quote(fieldColumn("site-content"))+" FROM "+quote(collectionTable("site"))+" WHERE id = 'site'").Scan(&currentGlobal); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT jsonb_agg(snapshot ORDER BY collection_id) FROM ridu_versions
WHERE collection_id = ANY($1::text[])`, []string{"entries", "site"}).Scan(&versionSnapshots); err != nil {
		t.Fatal(err)
	}
	for label, encoded := range map[string][]byte{
		"collection current": currentCollection,
		"global current":     currentGlobal,
		"version snapshots":  versionSnapshots,
	} {
		assertMarkedCollectionSlugReference(t, label, encoded, "people-reference", "people")
		assertMarkedCollectionSlugReference(t, label, encoded, "members-reference", "members")
	}
	var applied, partial int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_migration_steps WHERE artifact_name = $1`, forgedFile.Name).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if applied != 1 || partial != 0 {
		t.Fatalf("rejected overlap mutated ledgers: applied=%d partial=%d", applied, partial)
	}
}

func overlappingSlugRewriteArtifact(t *testing.T, before, after schema.Manifest) ridumigration.Artifact {
	t.Helper()
	artifact, err := ridumigration.NewArtifact("overlapping-slug-rewrites", atlasPlanner(), &before, after)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases, err = phasesFromOperations(artifact.FromDigest, []ridumigration.Operation{
		{Kind: ridumigration.StepRenameContent, Name: "first slug rewrite", Rename: &ridumigration.Rename{
			CollectionBefore: before.Snapshot().Collections[0].Slug,
			CollectionAfter:  after.Snapshot().Collections[0].Slug,
		}},
		{Kind: ridumigration.StepRenameContent, Name: "second slug rewrite", Rename: &ridumigration.Rename{
			CollectionBefore: before.Snapshot().Collections[1].Slug,
			CollectionAfter:  after.Snapshot().Collections[1].Slug,
		}},
		{Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("overlapping rewrite fixture: %v", err)
	}
	return artifact
}

func pluginReferenceLiveConfig(includeTarget, includeReferenceRoots bool) ridu.Config {
	collections := make([]ridu.Collection, 0, 2)
	if includeTarget {
		collections = append(collections, ridu.Collection{
			Slug: "people", Fields: field.Fields{field.Text("name").Required()},
		})
	}
	entryFields := field.Fields{field.Text("title").Required()}
	globalFields := field.Fields{field.Text("title").Required()}
	if includeReferenceRoots {
		config := richtext.Config{
			Features: []richtext.Feature{richtext.FeatureRelationships}, RelationshipCollections: []string{"people"},
		}
		entryFields = append(entryFields, richtext.Field("content", config))
		globalFields = append(globalFields, richtext.Field("content", config))
	}
	collections = append(collections, ridu.Collection{
		Slug: "entries", Fields: entryFields, Versions: true,
		VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
	})
	return ridu.Config{
		Name: "Plugin reference retirement", Plugins: []ridu.Plugin{richtext.New()}, Collections: collections,
		Globals: []ridu.Global{{
			Slug: "site", Fields: globalFields, Versions: true,
			VersionConfig: ridu.VersionConfig{Drafts: true, MaxPerDocument: 10},
		}},
	}
}

func pluginSlugRewriteManifest(targetSlug schema.CollectionSlug, reuseOldSlug bool) schema.Manifest {
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	content := pluginReferenceFieldForManifest("entries-content", "content")
	globalContent := pluginReferenceFieldForManifest("site-content", "content")
	structured := pluginSlugRewriteStructuredField("entries")
	globalStructured := pluginSlugRewriteStructuredField("site")
	relatedPath, _ := query.NewPath("related")
	related := schema.Field{
		ID: "entries-related", Name: "related", Path: relatedPath,
		Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{
			Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionID: "people-target", CollectionSlug: targetSlug}},
			OnDelete: schema.ReferenceDeleteNullify,
		},
	}
	collections := []schema.Collection{
		{ID: "people-target", Slug: targetSlug, Fields: []schema.Field{}},
		{ID: "entries", Slug: "entries", Capabilities: schema.Capabilities{Versions: true}, Versions: versions, Fields: []schema.Field{content, structured, related}},
	}
	if reuseOldSlug {
		collections = append(collections, schema.Collection{ID: "people-reused", Slug: "people", Fields: []schema.Field{}})
	}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Stable plugin slug rewrite"},
		Collections: collections,
		Globals: []schema.Global{{
			ID: "site", Slug: "site", Capabilities: schema.Capabilities{Global: true, Versions: true},
			Versions: versions, Fields: []schema.Field{globalContent, globalStructured},
		}},
		Plugins: []schema.Plugin{},
	})
}

func overlappingSlugLiveManifest(first, second schema.CollectionSlug) schema.Manifest {
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Overlapping slug rewrite"},
		Collections: []schema.Collection{
			{ID: "people-target", Slug: first, Fields: []schema.Field{}},
			{ID: "members-target", Slug: second, Fields: []schema.Field{}},
			{
				ID: "entries", Slug: "entries", Capabilities: schema.Capabilities{Versions: true}, Versions: versions,
				Fields: []schema.Field{pluginReferenceFieldForManifest("entries-content", "content")},
			},
		},
		Globals: []schema.Global{{
			ID: "site", Slug: "site", Capabilities: schema.Capabilities{Global: true, Versions: true}, Versions: versions,
			Fields: []schema.Field{pluginReferenceFieldForManifest("site-content", "content")},
		}},
		Plugins: []schema.Plugin{},
	})
}

func overlappingSlugReferenceValue() store.Value {
	return store.Object(store.Values{"nodes": store.List(
		store.Object(store.Values{
			"relationTo": store.String("people"), "id": store.String("person"),
			"migrationMarker": store.String("people-reference"),
		}),
		store.Object(store.Values{
			"relationTo": store.String("members"), "id": store.String("member"),
			"migrationMarker": store.String("members-reference"),
		}),
	)})
}

func pluginSlugRewriteStructuredField(owner string) schema.Field {
	rootPath, _ := query.NewPath("structured")
	referencePath, _ := query.ParsePath("structured.reference")
	opaquePath, _ := query.ParsePath("structured.opaque")
	reference := schema.Field{
		ID: schema.StableID(owner + "-structured-reference"), Name: "reference", Path: referencePath,
		Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "richtext", Config: json.RawMessage(`{"version":1}`), ReferenceKeys: []string{"relationTo"}},
	}
	opaque := schema.Field{
		ID: schema.StableID(owner + "-structured-opaque"), Name: "opaque", Path: opaquePath,
		Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "opaque", Config: json.RawMessage(`{"version":1}`)},
	}
	return schema.Field{
		ID: schema.StableID(owner + "-structured"), Name: "structured", Path: rootPath,
		Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{reference, opaque}},
	}
}

func pluginReferenceFieldForManifest(id schema.StableID, name string) schema.Field {
	path, _ := query.NewPath(name)
	return schema.Field{
		ID: id, Name: name, Path: path, Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: "richtext", Config: json.RawMessage(`{"version":1}`), ReferenceKeys: []string{"relationTo"}},
	}
}

func assertOnlyCollectionSlugReferences(t *testing.T, label string, encoded []byte, want string) {
	t.Helper()
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("%s JSON: %v", label, err)
	}
	count := 0
	var inspect func(any)
	inspect = func(current any) {
		switch current := current.(type) {
		case []any:
			for _, child := range current {
				inspect(child)
			}
		case map[string]any:
			if slug, ok := current["relationTo"].(string); ok {
				count++
				if slug != want {
					t.Fatalf("%s relationTo = %q, want %q after old-slug reuse", label, slug, want)
				}
			}
			for _, child := range current {
				inspect(child)
			}
		}
	}
	inspect(value)
	if count == 0 {
		t.Fatalf("%s contains no collection slug reference: %s", label, encoded)
	}
}

func assertMarkedCollectionSlugReference(t *testing.T, label string, encoded []byte, marker, want string) {
	t.Helper()
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("%s JSON: %v", label, err)
	}
	found := 0
	var inspect func(any)
	inspect = func(current any) {
		switch current := current.(type) {
		case []any:
			for _, child := range current {
				inspect(child)
			}
		case map[string]any:
			if current["migrationMarker"] == marker {
				found++
				if slug, _ := current["relationTo"].(string); slug != want {
					t.Fatalf("%s marker %q relationTo = %q, want %q", label, marker, slug, want)
				}
			}
			for _, child := range current {
				inspect(child)
			}
		}
	}
	inspect(value)
	if found == 0 {
		t.Fatalf("%s contains no marker %q: %s", label, marker, encoded)
	}
}

func livePluginReferenceDocument(targetID, marker string) store.Value {
	// Current engine writes require the portable envelope. Keep the version
	// marker in ordinary text; arbitrary properties are reserved for the raw
	// historical migration fixtures below.
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root": store.Object(store.Values{
			"type": store.String("root"), "children": store.List(store.Object(store.Values{
				"type": store.String("paragraph"), "children": store.List(
					store.Object(store.Values{"type": store.String("text"), "text": store.String(marker)}),
					store.Object(store.Values{"type": store.String("relationship"), "relationTo": store.String("people"), "id": store.String(targetID)}),
				),
			})),
		}),
	})
}

func historicalPluginReferenceDocument(targetID, marker string) store.Value {
	reference := store.Object(store.Values{
		"type": store.String("relationship"), "relationTo": store.String("people"),
		"id": store.String(targetID), "migrationMarker": store.String(marker),
	})
	paragraph := store.Object(store.Values{
		"type": store.String("paragraph"), "children": store.List(reference),
	})
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root": store.Object(store.Values{
			"type": store.String("root"), "children": store.List(paragraph),
		}),
	})
}

func pluginReferenceVersionsContain(versions []store.Version, marker string) bool {
	for _, version := range versions {
		encoded, _ := json.Marshal(version.Snapshot.Values)
		if strings.Contains(string(encoded), marker) {
			return true
		}
	}
	return false
}

func pluginReferenceResourceID(snapshot schema.Snapshot, slug schema.CollectionSlug) schema.StableID {
	for _, collection := range snapshot.Collections {
		if collection.Slug == slug {
			return collection.ID
		}
	}
	return ""
}

func pluginReferenceGlobalID(snapshot schema.Snapshot, slug schema.CollectionSlug) schema.StableID {
	for _, global := range snapshot.Globals {
		if global.Slug == slug {
			return global.ID
		}
	}
	return ""
}
