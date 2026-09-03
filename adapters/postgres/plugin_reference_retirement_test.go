package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestPluginReferenceKeysBlockTargetRetirementForEverySurvivingOwnerShape(t *testing.T) {
	tests := map[string]struct {
		collectionField *schema.Field
		globalField     *schema.Field
	}{
		"collection root":        {collectionField: pointerToField(pluginReferenceField(t, "entries-content", "content", "richtext", "relationTo"))},
		"nested collection root": {collectionField: pointerToField(pluginReferenceGroup(t))},
		"global root":            {globalField: pointerToField(pluginReferenceField(t, "site-content", "content", "richtext", "relationTo"))},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := pluginReferenceManifest(test.collectionField, test.globalField)
			afterSnapshot := before.Snapshot()
			afterSnapshot.Collections = afterSnapshot.Collections[1:]
			after := schema.NewManifest(afterSnapshot)
			for _, allowDestructive := range []bool{false, true} {
				_, err := BuildArtifact(context.Background(), "retire-plugin-target", &before, after, nil, allowDestructive)
				var safety *SafetyError
				if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_RESOURCE_REMOVAL_REFERENCE_ROOT_UNSAFE") {
					t.Fatalf("allowDestructive=%t plugin target retirement error = %#v, %v", allowDestructive, safety, err)
				}
			}
		})
	}
}

func TestPluginReferenceKeyNarrowingFailsClosedBeforeDormantValuesCanReattach(t *testing.T) {
	tests := map[string]struct {
		collection bool
		mutate     func(*schema.Field)
	}{
		"collection key removal": {collection: true, mutate: func(field *schema.Field) {
			field.Plugin.ReferenceKeys = nil
		}},
		"global key replacement": {mutate: func(field *schema.Field) {
			field.Plugin.ReferenceKeys = []string{"collection"}
		}},
		"plugin contract replacement": {collection: true, mutate: func(field *schema.Field) {
			field.Plugin.Key = "other-editor"
		}},
		"plugin field removal": {collection: true, mutate: func(field *schema.Field) {
			*field = schema.Field{}
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			collectionField := pointerToField(pluginReferenceField(t, "entries-content", "content", "richtext", "relationTo"))
			globalField := pointerToField(pluginReferenceField(t, "site-content", "content", "richtext", "relationTo"))
			before := pluginReferenceManifest(collectionField, globalField)
			afterSnapshot := before.Snapshot()
			if test.collection {
				test.mutate(&afterSnapshot.Collections[1].Fields[0])
				if afterSnapshot.Collections[1].Fields[0].ID == "" {
					afterSnapshot.Collections[1].Fields = nil
				}
			} else {
				test.mutate(&afterSnapshot.Globals[0].Fields[0])
			}
			after := schema.NewManifest(afterSnapshot)
			for _, allowDestructive := range []bool{false, true} {
				_, err := BuildArtifact(context.Background(), "narrow-plugin-reference", &before, after, nil, allowDestructive)
				var safety *SafetyError
				if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") ||
					!strings.Contains(err.Error(), "current values or version snapshots") {
					t.Fatalf("allowDestructive=%t plugin reference narrowing error = %#v, %v", allowDestructive, safety, err)
				}
			}

			artifact, err := ridumigration.NewArtifact("forged-plugin-reference-narrowing", atlasPlanner(), &before, after)
			if err != nil {
				t.Fatal(err)
			}
			artifact.Phases, err = phasesFromOperations(artifact.FromDigest, []ridumigration.Operation{{
				Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := artifact.Validate(); err != nil {
				t.Fatalf("generic forged plugin artifact validation: %v", err)
			}
			if err := validatePostgresResourceRetirementTopology(artifact); err == nil ||
				!strings.Contains(err.Error(), "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") {
				t.Fatalf("runner plugin reference-shape preflight = %v", err)
			}
		})
	}
}

func TestPluginReferenceTargetRetirementDropsRootsAndPurgesAllVersionedOwners(t *testing.T) {
	collectionRoot := pluginReferenceGroup(t)
	globalRoot := pluginReferenceField(t, "site-content", "content", "richtext", "relationTo")
	before := pluginReferenceManifest(&collectionRoot, &globalRoot)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections = afterSnapshot.Collections[1:]
	afterSnapshot.Collections[0].Fields = nil
	afterSnapshot.Globals[0].Fields = nil
	after := schema.NewManifest(afterSnapshot)

	artifact, err := BuildArtifact(context.Background(), "retire-plugin-reference-roots", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRiskLevel(artifact.Risks, "RIDU_RETIRE_DEPENDENT_VERSION_HISTORY", ridumigration.RiskDestructive) {
		t.Fatalf("plugin dependent version-history risk = %#v", artifact.Risks)
	}
	wantOwners := []schema.StableID{"entries", "site"}
	found := false
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepRetireResources {
				continue
			}
			var payload ridumigration.RetireResourcesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			found = slices.Equal(payload.ResourceIDs, []schema.StableID{"people"}) &&
				slices.Equal(payload.PurgeVersionOwnerIDs, wantOwners)
		}
	}
	if !found {
		t.Fatalf("plugin retirement payload omitted exact target/owners: %#v", artifact.Phases)
	}
	if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
		t.Fatalf("plugin retirement runner preflight: %v", err)
	}
}

func TestPluginReferenceTargetAdditionAndConfirmedCollectionRenameRemainPlannable(t *testing.T) {
	root := pluginReferenceField(t, "entries-content", "content", "richtext", "relationTo")
	before := pluginReferenceManifest(&root, nil)

	t.Run("target addition", func(t *testing.T) {
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections = append(afterSnapshot.Collections, schema.Collection{
			ID: "topics", Slug: "topics", Fields: []schema.Field{},
		})
		after := schema.NewManifest(afterSnapshot)
		if _, err := BuildArtifact(context.Background(), "add-plugin-reference-target", &before, after, nil, false); err != nil {
			t.Fatalf("additive plugin target transition: %v", err)
		}
	})

	t.Run("global retirement is not a collection target decrease", func(t *testing.T) {
		afterSnapshot := before.Snapshot()
		afterSnapshot.Globals = nil
		after := schema.NewManifest(afterSnapshot)
		artifact, err := BuildArtifact(context.Background(), "retire-unrelated-global", &before, after, nil, true)
		if err != nil {
			t.Fatalf("global retirement with surviving plugin reference: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("global retirement runner preflight: %v", err)
		}
	})

	t.Run("confirmed target rename", func(t *testing.T) {
		afterSnapshot := before.Snapshot()
		beforeTarget := before.Snapshot().Collections[0]
		afterSnapshot.Collections[0].ID = "members"
		afterSnapshot.Collections[0].Slug = "members"
		afterTarget := afterSnapshot.Collections[0]
		after := schema.NewManifest(afterSnapshot)
		rename := Rename{Kind: RenameCollection, BeforeCollection: beforeTarget, AfterCollection: afterTarget}
		artifact, err := BuildArtifact(context.Background(), "rename-plugin-reference-target", &before, after, []Rename{rename}, false)
		if err != nil {
			t.Fatalf("confirmed plugin target rename: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("confirmed plugin target rename runner preflight: %v", err)
		}
	})
}

func pluginReferenceManifest(collectionField, globalField *schema.Field) schema.Manifest {
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	collections := []schema.Collection{{ID: "people", Slug: "people", Fields: []schema.Field{}}}
	entry := schema.Collection{
		ID: "entries", Slug: "entries", Capabilities: schema.Capabilities{Versions: true},
		Versions: versions, Fields: []schema.Field{},
	}
	if collectionField != nil {
		entry.Fields = []schema.Field{*collectionField}
	}
	collections = append(collections, entry)
	global := schema.Global{
		ID: "site", Slug: "site", Capabilities: schema.Capabilities{Global: true, Versions: true},
		Versions: versions, Fields: []schema.Field{},
	}
	if globalField != nil {
		global.Fields = []schema.Field{*globalField}
	}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Plugin reference retirement"},
		Collections: collections, Globals: []schema.Global{global}, Plugins: []schema.Plugin{},
	})
}

func pluginReferenceField(t *testing.T, id schema.StableID, path, pluginKey string, referenceKeys ...string) schema.Field {
	t.Helper()
	parsed, err := query.ParsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return schema.Field{
		ID: id, Name: parsed.Segments()[len(parsed.Segments())-1], Path: parsed,
		Type: schema.FieldTypePlugin, Category: schema.FieldCategoryPlugin,
		Plugin: &schema.PluginField{Key: pluginKey, Config: json.RawMessage(`{"version":1}`), ReferenceKeys: referenceKeys},
	}
}

func pluginReferenceGroup(t *testing.T) schema.Field {
	t.Helper()
	rootPath, err := query.NewPath("content")
	if err != nil {
		t.Fatal(err)
	}
	child := pluginReferenceField(t, "entries-content-document", "content.document", "richtext", "relationTo", "collection")
	return schema.Field{
		ID: "entries-content", Name: "content", Path: rootPath,
		Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
		Nested: &schema.NestedField{Fields: []schema.Field{child}},
	}
}
