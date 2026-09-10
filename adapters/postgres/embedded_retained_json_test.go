package postgres

import (
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	outline "github.com/riducms/ridu/tests/contracts/embedded_plugin"
)

func postgresEmbeddedRetainedJSONFixture(t *testing.T, nested bool) schema.Manifest {
	t.Helper()
	var content field.Node = outline.Field("content", field.Block{Slug: "card", Fields: field.Fields{field.Text("secret")}})
	if nested {
		content = field.Group("wrapper", field.Fields{content})
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Retained embedded JSON", Plugins: []ridu.Plugin{outline.Plugin{}},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections:  []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Group("settings", field.Fields{field.Text("caption"), content})}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestPostgresEmbeddedRetainedJSONRequiresTransform(t *testing.T) {
	for _, name := range []string{"enable-localization", "disable-localization", "require-content", "remove-content", "remove-wrapper", "replace-root-with-json", "localize-wrapper", "array-wrapper"} {
		t.Run(name, func(t *testing.T) {
			before := postgresEmbeddedRetainedJSONFixture(t, strings.HasSuffix(name, "wrapper"))
			snapshot := before.Snapshot()
			root := &snapshot.Collections[0].Fields[0]
			content := &root.Nested.ResolvedFields()[1]
			switch name {
			case "enable-localization":
				content.Localized = true
			case "disable-localization":
				content.Localized = true
				before = schema.NewManifest(snapshot)
				content.Localized = false
			case "require-content":
				content.Required = true
			case "localize-wrapper":
				content.Localized = true
			case "array-wrapper":
				content.Type = schema.FieldTypeArray
			case "remove-content", "remove-wrapper":
				root.Nested.Fields = root.Nested.ResolvedFields()[:1]
			case "replace-root-with-json":
				root.Type, root.Category, root.Nested = schema.FieldTypeJSON, schema.FieldCategoryScalar, nil
			}
			after := schema.NewManifest(snapshot)
			for _, destructive := range []bool{false, true} {
				if _, err := BuildArtifact(t.Context(), "retained-json-change", &before, after, nil, destructive); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
					t.Errorf("planner admitted a surviving JSON value (destructive=%v): %v", destructive, err)
				}
			}

			// This is a valid, correctly digested artifact of exactly the unsafe
			// assert_schema-only shape. Runner admission must independently reject it.
			manifestOnly, err := ridumigration.NewArtifact("retained-json-change", atlasPlannerForContract(currentAtlasPlannerContract()), &before, after)
			if err != nil {
				t.Fatal(err)
			}
			manifestOnly.Phases, err = phasesFromOperations(manifestOnly.FromDigest, []ridumigration.Operation{{Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := manifestOnly.Validate(); err != nil {
				t.Fatalf("invalid runner fixture: %v", err)
			}
			file := migrationartifact.File{Name: manifestOnly.Name, Artifact: manifestOnly}
			if _, err := preflightPendingArtifacts(t.Context(), []migrationartifact.File{file}, RunnerOptions{AllowMaintenance: true}); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
				t.Errorf("runner admitted an assert_schema-only artifact: %v", err)
			}

			descriptor := ridumigration.DataTransformDescriptor{Name: "repair-retained-json", Checksum: ridumigration.DataTransformChecksum([]byte("repair-retained-json"))}
			artifact, err := buildPostgresTransformTestArtifact(t.Context(), "retained-json-change", &before, after, descriptor)
			if err != nil {
				t.Fatalf("explicit compiled transform: %v", err)
			}
			if err := validatePostgresPlannedSQL(t.Context(), artifact); err != nil {
				t.Fatalf("runner rejected a bound transform: %v", err)
			}
		})
	}
}

func TestPostgresEmbeddedRemovalDefersOnlyToPhysicalRootRemoval(t *testing.T) {
	for _, name := range []string{"column", "collection", "global-column", "global"} {
		t.Run(name, func(t *testing.T) {
			before := postgresEmbeddedRetainedJSONFixture(t, true)
			snapshot := before.Snapshot()
			if strings.HasPrefix(name, "global") {
				snapshot.Globals, snapshot.Collections = snapshot.Collections, nil
				snapshot.Globals[0].Capabilities.Global = true
				before = schema.NewManifest(snapshot)
			}
			switch name {
			case "column":
				snapshot.Collections[0].Fields = nil
			case "collection":
				snapshot.Collections = nil
			case "global-column":
				snapshot.Globals[0].Fields = nil
			case "global":
				snapshot.Globals = nil
			}
			artifact, err := BuildArtifact(t.Context(), "remove-physical-root", &before, schema.NewManifest(snapshot), nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(artifactSQL(t, artifact), "DROP ") {
				t.Fatal("removal did not produce a physical drop")
			}
			if err := validatePostgresPlannedSQL(t.Context(), artifact); err != nil {
				t.Fatalf("runner rejected physical removal: %v", err)
			}
		})
	}
}

func TestPostgresEmbeddedRetainedJSONFollowsConfirmedRootRename(t *testing.T) {
	before := postgresEmbeddedRetainedJSONFixture(t, false)
	snapshot := before.Snapshot()
	beforeRoot := snapshot.Collections[0].Fields[0]
	afterRoot := beforeRoot
	afterRoot.ID, afterRoot.Name = "renamed-settings", "preferences"
	afterRoot.Path, _ = query.ParsePath("preferences")
	afterRoot.Nested = &schema.NestedField{Fields: append([]schema.Field(nil), beforeRoot.Nested.ResolvedFields()...)}
	afterRoot.Nested.Fields = afterRoot.Nested.ResolvedFields()[:1]
	snapshot.Collections[0].Fields[0] = afterRoot
	after := schema.NewManifest(snapshot)
	renames := []Rename{{Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: snapshot.Collections[0], BeforeField: &beforeRoot, AfterField: &afterRoot}}
	if _, err := BuildArtifact(t.Context(), "rename-retained-root", &before, after, renames, true); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("renamed JSON root was mistaken for removed storage: %v", err)
	}
}

func TestPostgresEmbeddedOwnContractAllowsConfirmedFieldRename(t *testing.T) {
	before := postgresEmbeddedRetainedJSONFixture(t, false)
	snapshot := before.Snapshot()
	content := snapshot.Collections[0].Fields[0].Nested.ResolvedFields()[1]
	content.Name = "content"
	content.Path, _ = query.ParsePath("content")
	snapshot.Collections[0].Fields = []schema.Field{content}
	before = schema.NewManifest(snapshot)
	beforeField := snapshot.Collections[0].Fields[0]
	afterField := beforeField
	afterField.ID, afterField.Name = "renamed-content", "body"
	afterField.Path, _ = query.ParsePath("body")
	snapshot.Collections[0].Fields[0] = afterField
	after := schema.NewManifest(snapshot)
	renames := []Rename{{Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: snapshot.Collections[0], BeforeField: &beforeField, AfterField: &afterField}}
	artifact, err := BuildArtifact(t.Context(), "rename-embedded-field", &before, after, renames, false)
	if err != nil {
		t.Fatalf("confirmed field rename: %v", err)
	}
	if !hasStepKind(t, artifact, ridumigration.StepRenameContent) {
		t.Fatal("confirmed rename omitted the ordinary content rewrite")
	}
	if err := validatePostgresPlannedSQL(t.Context(), artifact); err != nil {
		t.Fatalf("runner rejected confirmed rename: %v", err)
	}
}

func TestPostgresEmbeddedPayloadRenameStillRequiresTransform(t *testing.T) {
	resolve := func(slug schema.CollectionSlug, child string) schema.Manifest {
		t.Helper()
		manifest, err := ridu.Resolve(ridu.Config{
			Name: "Embedded payload rename", Plugins: []ridu.Plugin{outline.Plugin{}},
			Collections: []ridu.Collection{{Slug: slug, Fields: field.Fields{
				outline.Field("content", field.Block{Slug: "card", Fields: field.Fields{field.Text(child)}}),
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before, after := resolve("posts", "secret"), resolve("articles", "heading")
	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 1 {
		t.Fatalf("expected one deterministic collection rename candidate, got %#v", candidates)
	}
	// A collection rename also maps descendant identities. That mapping cannot
	// authorize a payload-key rewrite the ordinary content rewriter cannot do.
	renames := []Rename{postgresRenameFromCandidate(candidates[0])}
	if _, err := BuildArtifact(t.Context(), "rename-embedded-payload", &before, after, renames, false); err == nil || !strings.Contains(err.Error(), "compiled data transform") {
		t.Fatalf("planner admitted an embedded payload rename: %v", err)
	}
}
