package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/enableversions"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func postgresVersionsConfig(versions, drafts bool) ridu.Config {
	return ridu.Config{
		Name: "PostgreSQL enable versions",
		Collections: []ridu.Collection{
			{Slug: "categories", Fields: field.Fields{field.Text("name")}},
			{
				Slug: "posts", Trash: true, Versions: versions, VersionConfig: ridu.VersionConfig{Drafts: drafts},
				Fields: field.Fields{field.Text("title").Index().Unique(), field.Relationship("category", "categories")},
			},
		},
	}
}

func postgresVersionsManifest(t *testing.T, versions, drafts bool) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(postgresVersionsConfig(versions, drafts))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func postgresVersionsCollection(t *testing.T, manifest schema.Manifest, slug schema.CollectionSlug) schema.Collection {
	t.Helper()
	collection, found := collectionBySlug(manifest.Snapshot().Collections, slug)
	if !found {
		t.Fatalf("collection %q is absent", slug)
	}
	return collection
}

func postgresVersionsChoice(resource schema.Collection, existing ridumigration.ExistingDocuments) map[schema.StableID]ridumigration.ExistingDocuments {
	return map[schema.StableID]ridumigration.ExistingDocuments{resource.ID: existing}
}

// A migration that enables versions records what the stored documents
// become. A require-empty check runs before any DDL, in the transaction that
// adds the versioned columns; a conversion runs after every physical change.
// Every step needs maintenance, but only a conversion rewrites documents, so
// only it blocks baseline adoption.
func TestPostgresArtifactRecordsWhatExistingDocumentsBecome(t *testing.T) {
	ctx := context.Background()
	before, after := postgresVersionsManifest(t, false, false), postgresVersionsManifest(t, true, true)
	posts := postgresVersionsCollection(t, after, "posts")
	if _, err := BuildArtifact(ctx, "enable-versions", &before, after, ArtifactOptions{AllowDestructive: true}); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_REQUIRED") {
		t.Fatalf("migration without a choice = %v", err)
	}
	for _, existing := range []ridumigration.ExistingDocuments{ridumigration.ExistingPublished, ridumigration.ExistingDraft, ridumigration.ExistingRequireEmpty} {
		t.Run(string(existing), func(t *testing.T) {
			artifact, err := BuildArtifact(ctx, "enable-versions", &before, after, ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, existing)})
			if err != nil {
				t.Fatal(err)
			}
			recorded, err := enableversions.Recorded(artifact)
			if err != nil || len(recorded) != 1 || recorded[posts.ID] != existing {
				t.Fatalf("recorded choices = %#v, %v", recorded, err)
			}
			risk := false
			for _, candidate := range artifact.Risks {
				risk = risk || candidate.Code == "RIDU_VERSIONS_ENABLE" && candidate.Level == ridumigration.RiskWarning
			}
			if !risk {
				t.Fatalf("risks = %#v", artifact.Risks)
			}
			type located struct {
				phase int
				step  ridumigration.Step
			}
			var steps []located
			for index, phase := range artifact.Phases {
				for _, step := range phase.Steps {
					steps = append(steps, located{phase: index, step: step})
				}
			}
			enable := -1
			for index, candidate := range steps {
				if candidate.step.Kind == ridumigration.StepEnableVersions {
					enable = index
				}
			}
			if enable < 0 || artifact.Phases[steps[enable].phase].Mode != ridumigration.PhaseTransaction {
				t.Fatalf("enable-versions step position %d in %#v", enable, artifact.Phases)
			}
			if existing == ridumigration.ExistingRequireEmpty {
				addsStatus := false
				for _, step := range artifact.Phases[0].Steps {
					addsStatus = addsStatus || step.Kind == ridumigration.StepSQL && strings.Contains(string(step.Payload), `ADD COLUMN \"_status\"`)
				}
				if enable != 0 || !addsStatus {
					t.Fatalf("require-empty does not run first in the transaction that adds the versioned columns: %#v", artifact.Phases[0])
				}
			} else {
				for index, candidate := range steps {
					physical := candidate.step.Kind == ridumigration.StepSQL || candidate.step.Kind == ridumigration.StepConcurrentIndex
					if physical && index > enable || candidate.step.Kind == ridumigration.StepAssertSchema && index < enable {
						t.Fatalf("step %s (%s) runs on the wrong side of the conversion", candidate.step.ID, candidate.step.Kind)
					}
				}
			}
			rewrites := existing != ridumigration.ExistingRequireEmpty
			if !artifactRequiresMaintenance(artifact) {
				t.Fatal("enabling versions ran without maintenance admission")
			}
			if blocking := postgresBlockingStep(migrationartifact.File{Artifact: artifact}); (blocking != "") != rewrites {
				t.Fatalf("adoption blocking step = %q", blocking)
			}
			if err := validatePostgresPlannedSQL(ctx, artifact); err != nil {
				t.Fatalf("regenerating from the recorded choice: %v", err)
			}
			if err := validatePostgresCapabilityDecreasePreflight(artifact); err != nil {
				t.Fatalf("capability preflight: %v", err)
			}

			// A choice edited after review no longer matches its plan.
			edited := ridumigration.ExistingPublished
			if existing == ridumigration.ExistingPublished {
				edited = ridumigration.ExistingDraft
			}
			tampered := artifact
			tampered.Phases = append([]ridumigration.Phase(nil), artifact.Phases...)
			phase := &tampered.Phases[steps[enable].phase]
			phase.Steps = append([]ridumigration.Step(nil), phase.Steps...)
			for index := range phase.Steps {
				if phase.Steps[index].Kind == ridumigration.StepEnableVersions {
					phase.Steps[index].Payload, err = json.Marshal(ridumigration.EnableVersionsPayload{ResourceID: posts.ID, Existing: edited})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := validatePostgresPlannedSQL(ctx, tampered); err == nil || !strings.Contains(err.Error(), CodeMigrationPlanMismatch) {
				t.Fatalf("edited choice = %v", err)
			}
		})
	}

	withoutDrafts := postgresVersionsManifest(t, true, false)
	if _, err := BuildArtifact(ctx, "enable-versions", &before, withoutDrafts, ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, ridumigration.ExistingDraft)}); err == nil || !strings.Contains(err.Error(), "does not enable drafts") {
		t.Fatalf("draft choice without drafts = %v", err)
	}
	categories := postgresVersionsCollection(t, after, "categories")
	choices := map[schema.StableID]ridumigration.ExistingDocuments{posts.ID: ridumigration.ExistingPublished, categories.ID: ridumigration.ExistingPublished}
	if _, err := BuildArtifact(ctx, "enable-versions", &before, after, ArtifactOptions{ExistingDocuments: choices}); err == nil || !strings.Contains(err.Error(), "does not start keeping versions") {
		t.Fatalf("choice for a resource that keeps no versions = %v", err)
	}
	if _, err := BuildArtifact(ctx, "initial", nil, after, ArtifactOptions{ExistingDocuments: postgresVersionsChoice(posts, ridumigration.ExistingPublished)}); err == nil || !strings.Contains(err.Error(), "does not start keeping versions") {
		t.Fatalf("choice in an initial migration = %v", err)
	}
	if _, err := BuildArtifact(ctx, "initial", nil, after, ArtifactOptions{}); err != nil {
		t.Fatalf("initial versioned resource: %v", err)
	}

	// The runner refuses an artifact whose manifests enable versions without
	// recording a choice, even if its digests were recomputed.
	forged, err := ridumigration.NewArtifact("forged-enable-versions", atlasPlanner(), &before, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePostgresCapabilityDecreasePreflight(forged); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_REQUIRED") {
		t.Fatalf("runner preflight accepted an unrecorded version enable: %v", err)
	}
}

// Enabling versions rewrites stored content, so it cannot share a migration
// with renames, including the rewrite of a changed collection slug, or with
// data transforms.
func TestPostgresVersionsEnableTakesAMigrationOfItsOwn(t *testing.T) {
	ctx := context.Background()
	before := atlasTestManifest(atlasTextField("posts-title", "title"))
	versioned := func(snapshot schema.Snapshot) schema.Snapshot {
		snapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 2}
		snapshot.Collections[0].Capabilities.Versions = true
		return snapshot
	}
	choice := map[schema.StableID]ridumigration.ExistingDocuments{"posts": ridumigration.ExistingPublished}

	slugChange := versioned(before.Snapshot())
	slugChange.Collections[0].Slug = "articles"
	if _, err := BuildArtifact(ctx, "enable-versions", &before, schema.NewManifest(slugChange), ArtifactOptions{ExistingDocuments: choice}); err == nil || !strings.Contains(err.Error(), "migration of its own") {
		t.Fatalf("versions with a collection slug change = %v", err)
	}

	renamed := versioned(atlasTestManifest(atlasTextField("posts-heading", "heading")).Snapshot())
	beforeField, afterField := before.Snapshot().Collections[0].Fields[0], renamed.Collections[0].Fields[0]
	rename := Rename{
		Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: renamed.Collections[0],
		BeforeField: &beforeField, AfterField: &afterField,
	}
	if _, err := BuildArtifact(ctx, "enable-versions", &before, schema.NewManifest(renamed), ArtifactOptions{Renames: []Rename{rename}, ExistingDocuments: choice}); err == nil || !strings.Contains(err.Error(), "migration of its own") {
		t.Fatalf("versions with a field rename = %v", err)
	}

	transform := ridumigration.DataTransformDescriptor{Name: "backfill-titles", Checksum: ridumigration.DataTransformChecksum([]byte("backfill-v1"))}
	if _, err := BuildArtifact(ctx, "enable-versions", &before, schema.NewManifest(versioned(before.Snapshot())), ArtifactOptions{
		DataTransforms: []ridumigration.DataTransformDescriptor{transform}, ExistingDocuments: choice,
	}); err == nil || !strings.Contains(err.Error(), "migration of its own") {
		t.Fatalf("versions with a data transform = %v", err)
	}
	if _, err := BuildArtifact(ctx, "enable-versions", &before, schema.NewManifest(versioned(before.Snapshot())), ArtifactOptions{ExistingDocuments: choice}); err != nil {
		t.Fatalf("versions in a migration of their own: %v", err)
	}
}
