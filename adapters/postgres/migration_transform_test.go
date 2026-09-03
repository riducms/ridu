package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestPostgresDataTransformArtifactsAreDeterministicAndSupportDataOnlyHistory(t *testing.T) {
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "normalize-titles", Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v1")),
	}

	first, err := buildPostgresTransformTestArtifact(context.Background(), "normalize-titles", &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildPostgresTransformTestArtifact(context.Background(), "normalize-titles", &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical PostgreSQL data-only plans produced different artifacts")
	}
	first.PreviousArtifactDigest = strings.Repeat("a", 64)
	if _, err := first.Digest(); err != nil {
		t.Fatal(err)
	}
	if first.FromDigest != first.ToDigest {
		t.Fatalf("data-only manifest digest changed from %q to %q", first.FromDigest, first.ToDigest)
	}

	descriptors, err := postgresArtifactDataTransformDescriptors(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(descriptors, []ridumigration.DataTransformDescriptor{descriptor}) {
		t.Fatalf("artifact transforms = %#v", descriptors)
	}
	phase := first.Phases[len(first.Phases)-1]
	if phase.Mode != ridumigration.PhaseTransaction || len(phase.Steps) < 2 ||
		phase.Steps[len(phase.Steps)-2].Kind != ridumigration.StepDataTransform ||
		phase.Steps[len(phase.Steps)-1].Kind != ridumigration.StepAssertSchema {
		t.Fatalf("data-only transaction steps = %#v", phase.Steps)
	}
}

func TestPostgresDataTransformPlanningRejectsDuplicatesAndVersionedFieldChanges(t *testing.T) {
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "normalize-titles", Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v1")),
	}
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "duplicate", &manifest, manifest, descriptor, descriptor); err == nil || !strings.Contains(err.Error(), "bound more than once") {
		t.Fatalf("duplicate descriptor error = %v", err)
	}

	versionedSnapshot := manifest.Snapshot()
	versionedSnapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	versionedSnapshot.Collections[0].Capabilities.Versions = true
	before := schema.NewManifest(versionedSnapshot)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, atlasTextField("posts-summary", "summary"))
	after := schema.NewManifest(afterSnapshot)
	if _, err := buildPostgresTransformTestArtifact(context.Background(), "versioned-change", &before, after, descriptor); err == nil || !strings.Contains(err.Error(), "retained snapshots") {
		t.Fatalf("versioned transform error = %v", err)
	}
}

func TestPostgresStatusRejectsTransformNameChecksumReuseAcrossHistory(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	initial := ridumigration.DataTransformDescriptor{
		Name: "normalize-titles", Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v1")),
	}
	first, err := buildPostgresTransformTestArtifact(ctx, "initial-transform", nil, manifest, initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, first.Name, first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}

	changed := ridumigration.DataTransformDescriptor{
		Name: initial.Name, Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v2")),
	}
	second, err := buildPostgresTransformTestArtifact(ctx, "changed-transform", &manifest, manifest, changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, second.Name, second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}

	backend := &Store{}
	if _, err := backend.ArtifactStatus(ctx, directory); err == nil || !strings.Contains(err.Error(), "use a new transform name") {
		t.Fatalf("changed transform identity error = %v", err)
	}
}

func TestPostgresDataTransformRegistrationsFailBeforeDatabaseAccess(t *testing.T) {
	descriptor := ridumigration.DataTransformDescriptor{
		Name: "normalize-titles", Checksum: ridumigration.DataTransformChecksum([]byte("normalize-titles-v1")),
	}
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	artifact, err := buildPostgresTransformTestArtifact(context.Background(), "normalize-titles", &manifest, manifest, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	artifact.PreviousArtifactDigest = strings.Repeat("a", 64)
	digest, err := artifact.Digest()
	if err != nil {
		t.Fatal(err)
	}
	files := []migrationartifact.File{{Name: "00000000000000.000000000_normalize-titles.ridu.json", Digest: digest, Artifact: artifact}}
	request := ridumigration.ProjectRequest{
		Action: ridumigration.ProjectApply, DatabaseURL: "postgres://must-not-be-opened.invalid/ridu", Directory: "unused",
		AllowMaintenance: true, AllowInsecureDatabase: true,
	}
	callback := func(context.Context, ridumigration.DataTransaction) error { return nil }

	tests := []struct {
		name       string
		transforms []ridumigration.DataTransform
		contains   string
	}{
		{name: "missing", contains: "unregistered"},
		{name: "checksum changed", transforms: []ridumigration.DataTransform{{
			DataTransformDescriptor: ridumigration.DataTransformDescriptor{Name: descriptor.Name, Checksum: ridumigration.DataTransformChecksum([]byte("v2"))}, Up: callback, Down: callback,
		}}, contains: "checksum differs"},
		{name: "duplicate", transforms: []ridumigration.DataTransform{
			{DataTransformDescriptor: descriptor, Up: callback, Down: callback},
			{DataTransformDescriptor: descriptor, Up: callback, Down: callback},
		}, contains: "registered more than once"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, registryErr := newPostgresDataTransformRegistry(test.transforms)
			if registryErr != nil {
				if !strings.Contains(registryErr.Error(), test.contains) {
					t.Fatalf("registry error = %v", registryErr)
				}
				return
			}
			driver := &postgresProjectMigrationDriver{}
			err := driver.runProjectMigrationFiles(context.Background(), request, files, registry)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("preflight error = %v", err)
			}
		})
	}
}

func buildPostgresTransformTestArtifact(ctx context.Context, name string, before *schema.Manifest, after schema.Manifest, transforms ...ridumigration.DataTransformDescriptor) (ridumigration.Artifact, error) {
	contract := currentAtlasPlannerContract()
	return buildArtifactWithPlannerContracts(ctx, name, before, after, nil, false, contract, contract, transforms...)
}
