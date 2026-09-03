package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestConfirmRenameCandidatesDefaultsToPreservingData(t *testing.T) {
	candidate := schemadiff.RenameCandidate{
		Kind:             schemadiff.RenameCollection,
		BeforeCollection: schema.Collection{ID: "posts", Slug: "posts"},
		AfterCollection:  schema.Collection{ID: "articles", Slug: "articles"},
	}
	var output bytes.Buffer
	accepted, err := confirmRenameCandidates([]schemadiff.RenameCandidate{candidate}, strings.NewReader("\n"), &output, false)
	if err != nil || len(accepted) != 1 {
		t.Fatalf("confirmation = %#v, %v", accepted, err)
	}
	if !strings.Contains(output.String(), `collection rename "posts" -> "articles"`) {
		t.Fatalf("prompt = %q", output.String())
	}
}

func TestBuildPostgresArtifactWithDataTransformsRejectsVersionedFieldChanges(t *testing.T) {
	beforeSnapshot := renameTestManifest("posts", "title").Snapshot()
	beforeSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	beforeSnapshot.Collections[0].Capabilities.Versions = true
	before := schema.NewManifest(beforeSnapshot)
	afterSnapshot := before.Snapshot()
	summaryPath, _ := query.NewPath("summary")
	afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, schema.Field{
		ID: "posts-summary", Name: "summary", Path: summaryPath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
	})
	after := schema.NewManifest(afterSnapshot)
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "backfill-summary", Checksum: ridumigration.DataTransformChecksum([]byte("backfill-summary-v1")),
	}
	_, err := buildPostgresArtifactWithDataTransforms(
		context.Background(), descriptor.Name, &before, after, nil, false, postgres.AtlasVersion,
		[]ridumigration.DataTransformDescriptor{descriptor},
	)
	if err == nil || !strings.Contains(err.Error(), "retained snapshots") {
		t.Fatalf("versioned CLI transform planning error = %v", err)
	}
}

func TestMigrationRenameBaseDoesNotTrustDisposableGeneratedSchema(t *testing.T) {
	root := t.TempDir()
	before := renameTestManifest("posts", "title")
	if err := writeRenameTestManifest(filepath.Join(root, "generated", "ridu.schema.json"), before); err != nil {
		t.Fatal(err)
	}

	base, exists, err := migrationRenameBase(filepath.Join(root, "migrations"))
	if err != nil || exists {
		t.Fatalf("base exists=%t err=%v snapshot=%#v", exists, err, base.Snapshot())
	}
}

func TestMigrationRenameBaseUsesImmutableArtifactState(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "migrations")
	committed := renameTestManifest("pages", "title")
	artifact, err := ridumigration.NewArtifact("initial", ridumigration.Planner{Name: "atlas", Version: postgres.AtlasVersion}, nil, committed)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := ridumigration.MarshalStepPayload(ridumigration.AssertSchemaPayload{})
	if err != nil {
		t.Fatal(err)
	}
	physical := ridumigration.PhysicalDigestSeed(artifact.FromDigest)
	phase := ridumigration.Phase{
		ID: "phase-001", Mode: ridumigration.PhaseTransaction, PhysicalContractVersion: ridumigration.PhysicalContractVersion,
		BeforePhysicalDigest: physical,
		Steps:                []ridumigration.Step{{ID: "step-0001", Kind: ridumigration.StepAssertSchema, ExecutorVersion: 1, Name: "assert", Payload: payload}},
	}
	phase.AfterPhysicalDigest, err = ridumigration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases = []ridumigration.Phase{phase}
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}

	base, exists, err := migrationRenameBase(directory)
	if err != nil || !exists || !base.Equal(committed) {
		t.Fatalf("base exists=%t err=%v snapshot=%#v", exists, err, base.Snapshot())
	}
	plannerVersion, err := migrationHeadPlannerVersion(directory)
	if err != nil || plannerVersion != postgres.AtlasVersion {
		t.Fatalf("head planner version = %q, %v", plannerVersion, err)
	}
}

func writeRenameTestManifest(path string, manifest schema.Manifest) error {
	encoded, err := manifest.Bytes()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

func TestConfirmRenameCandidatesSupportsExplicitRejectionAndNonInteractiveAcceptance(t *testing.T) {
	candidate := schemadiff.RenameCandidate{
		Kind:             schemadiff.RenameField,
		BeforeCollection: schema.Collection{ID: "posts", Slug: "posts"},
		AfterCollection:  schema.Collection{ID: "posts", Slug: "posts"},
		BeforeField:      &schema.Field{Name: "title"},
		AfterField:       &schema.Field{Name: "headline"},
	}
	accepted, err := confirmRenameCandidates([]schemadiff.RenameCandidate{candidate}, strings.NewReader("no\n"), &bytes.Buffer{}, false)
	if err != nil || len(accepted) != 0 {
		t.Fatalf("rejection = %#v, %v", accepted, err)
	}
	accepted, err = confirmRenameCandidates([]schemadiff.RenameCandidate{candidate}, nil, &bytes.Buffer{}, true)
	if err != nil || len(accepted) != 1 {
		t.Fatalf("non-interactive acceptance = %#v, %v", accepted, err)
	}
}

func TestMongoDBRenamesFreezeOnlyConfirmedSchemaAddresses(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	headlinePath, _ := query.NewPath("headline")
	collection := schemadiff.RenameCandidate{
		Kind:             schemadiff.RenameCollection,
		BeforeCollection: schema.Collection{ID: "posts", Slug: "posts"},
		AfterCollection:  schema.Collection{ID: "articles", Slug: "articles"},
		Fields: []schemadiff.FieldPair{{
			Before: schema.Field{ID: "posts-title", Name: "title", Path: titlePath},
			After:  schema.Field{ID: "articles-headline", Name: "headline", Path: headlinePath},
		}},
	}
	field := schemadiff.RenameCandidate{
		Kind:             schemadiff.RenameField,
		BeforeCollection: schema.Collection{ID: "pages", Slug: "pages"},
		AfterCollection:  schema.Collection{ID: "pages", Slug: "pages"},
		BeforeField:      &schema.Field{ID: "pages-title", Name: "title", Path: titlePath},
		AfterField:       &schema.Field{ID: "pages-headline", Name: "headline", Path: headlinePath},
	}
	renamed := mongoDBRenames([]schemadiff.RenameCandidate{collection, field})
	if len(renamed) != 2 || renamed[0].CollectionBefore != "posts" || renamed[0].CollectionAfter != "articles" ||
		len(renamed[0].Fields) != 1 || renamed[0].Fields[0] != (ridumigration.FieldRename{Before: "title", After: "headline"}) ||
		renamed[1].CollectionBefore != "pages" || renamed[1].CollectionAfter != "pages" ||
		renamed[1].FieldBefore != "title" || renamed[1].FieldAfter != "headline" || len(renamed[1].Fields) != 0 {
		t.Fatalf("MongoDB rename intents = %#v", renamed)
	}
}

func TestMongoDBHistoryRequiresProjectDriverOnlyForCompiledTransforms(t *testing.T) {
	files := []migrationartifact.File{{Artifact: ridumigration.Artifact{Phases: []ridumigration.Phase{{Steps: []ridumigration.Step{{Kind: ridumigration.StepRenameContent}}}}}}}
	if mongoDBHistoryRequiresProjectDriver(files) {
		t.Fatal("typed adapter-owned rename unexpectedly required a compiled project driver")
	}
	files[0].Artifact.Phases[0].Steps = append(files[0].Artifact.Phases[0].Steps, ridumigration.Step{Kind: ridumigration.StepDataTransform})
	if !mongoDBHistoryRequiresProjectDriver(files) {
		t.Fatal("compiled MongoDB data transform did not require the project driver")
	}
}

func renameTestManifest(slug schema.CollectionSlug, fieldName string) schema.Manifest {
	path, _ := query.NewPath(fieldName)
	id := schema.StableID(slug)
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Rename"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: id, Slug: slug, Fields: []schema.Field{{
				ID: schema.StableID(string(id) + "-" + fieldName), Name: fieldName, Path: path,
				Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
			}},
		}},
	})
}
