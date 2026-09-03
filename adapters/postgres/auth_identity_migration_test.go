package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestPostgresAuthIdentityPlannerUpgradeIsTypedAndOrderedBeforeExactIndex(t *testing.T) {
	manifest := postgresAuthIdentityMigrationManifest()
	legacy, _ := atlasPlannerContractFor(atlasVersionV1)
	initial, err := buildArtifactWithPlannerContracts(context.Background(), "legacy-auth", nil, manifest, nil, false, legacy, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Planner.Version != atlasVersionV1 || !strings.Contains(artifactSQL(t, initial), "lower(") {
		t.Fatalf("legacy auth artifact = planner %q SQL %q", initial.Planner.Version, artifactSQL(t, initial))
	}

	upgrade, err := BuildArtifactWithPreviousPlanner(context.Background(), "canonical-auth", &manifest, manifest, nil, true, atlasVersionV1)
	if err != nil {
		t.Fatal(err)
	}
	if upgrade.Planner.Version != AtlasVersion || !hasRiskCode(upgrade.Risks, "RIDU_AUTH_IDENTITY_CANONICALIZATION") {
		t.Fatalf("canonical auth upgrade = planner %q risks %#v", upgrade.Planner.Version, upgrade.Risks)
	}
	steps := artifactTestSteps(t, upgrade)
	canonicalAt, firstIndexChangeAt := -1, -1
	for index, step := range steps {
		if step.Kind == ridumigration.StepCanonicalizeAuthIdentities {
			canonicalAt = index
		}
		if firstIndexChangeAt == -1 && step.Kind == ridumigration.StepSQL && strings.Contains(strings.ToUpper(step.SQL), "INDEX") {
			firstIndexChangeAt = index
		}
	}
	if canonicalAt == -1 || firstIndexChangeAt == -1 || canonicalAt >= firstIndexChangeAt {
		t.Fatalf("canonicalization/index order = canonical %d, index %d, steps %#v", canonicalAt, firstIndexChangeAt, steps)
	}
	if strings.Contains(artifactSQL(t, upgrade), "lower(") {
		t.Fatalf("current auth index retained provider-specific folding: %s", artifactSQL(t, upgrade))
	}
	if err := validatePostgresPlannedSQL(context.Background(), upgrade); err != nil {
		t.Fatalf("regenerate canonical auth upgrade: %v", err)
	}
	for _, phase := range upgrade.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepCanonicalizeAuthIdentities {
				continue
			}
			var payload ridumigration.CanonicalizeAuthIdentitiesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			expected := ridumigration.AuthIdentityResources(manifest.Snapshot())
			if !samePostgresAuthIdentityResources(payload.Resources, expected) {
				t.Fatalf("canonical auth resources = %#v, want %#v", payload.Resources, expected)
			}
		}
	}
}

func TestPostgresAuthIdentityPlannerUpgradeScopesCanonicalizationToRetainedLegacyFields(t *testing.T) {
	before := postgresAuthIdentityMigrationManifest()
	after := postgresAuthIdentityMigrationManifestWithAddedCollection()
	legacy, _ := atlasPlannerContractFor(atlasVersionV1)
	initial, err := buildArtifactWithPlannerContracts(context.Background(), "legacy-auth", nil, before, nil, false, legacy, legacy)
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := BuildArtifactWithPreviousPlanner(context.Background(), "add-auth", &before, after, nil, true, atlasVersionV1)
	if err != nil {
		t.Fatal(err)
	}
	var resources []ridumigration.AuthIdentityResource
	for _, phase := range upgrade.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepCanonicalizeAuthIdentities {
				continue
			}
			var payload ridumigration.CanonicalizeAuthIdentitiesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			resources = payload.Resources
		}
	}
	expected := ridumigration.RetainedAuthIdentityResources(before.Snapshot(), after.Snapshot())
	if !samePostgresAuthIdentityResources(resources, expected) || len(resources) != 1 || resources[0].CollectionID != "users" {
		t.Fatalf("mixed auth canonicalization scope = %#v, want retained %#v", resources, expected)
	}
	if len(ridumigration.AuthIdentityResources(after.Snapshot())) != 2 {
		t.Fatalf("mixed auth fixture does not contain its new resource: %#v", after.Snapshot().Collections)
	}
	if err := validatePostgresAuthIdentityPlannerHistory([]migrationartifact.File{
		{Name: "legacy-auth", Artifact: initial},
		{Name: "add-auth", Artifact: upgrade},
	}); err != nil {
		t.Fatalf("mixed auth planner history: %v", err)
	}
	if err := validatePostgresPlannedSQL(context.Background(), upgrade); err != nil {
		t.Fatalf("regenerate mixed auth upgrade: %v", err)
	}
	tampered := upgrade
	replaced := false
	for phaseIndex := range tampered.Phases {
		for stepIndex := range tampered.Phases[phaseIndex].Steps {
			step := &tampered.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepCanonicalizeAuthIdentities {
				continue
			}
			step.Payload, err = ridumigration.MarshalStepPayload(ridumigration.CanonicalizeAuthIdentitiesPayload{
				Resources: ridumigration.AuthIdentityResources(after.Snapshot()),
			})
			if err != nil {
				t.Fatal(err)
			}
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("mixed auth upgrade has no canonicalization step to tamper")
	}
	recomputeTestPhysicalDigests(t, &tampered)
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "retained identity fields") {
		t.Fatalf("artifact accepted new auth resource in canonicalization scope: %v", err)
	}
	if err := validatePostgresAuthIdentityPlannerHistory([]migrationartifact.File{
		{Name: "legacy-auth", Artifact: initial},
		{Name: "add-auth", Artifact: tampered},
	}); err == nil || !strings.Contains(err.Error(), "retained legacy fields") {
		t.Fatalf("history accepted new auth resource in canonicalization scope: %v", err)
	}
}

func TestPostgresAuthIdentityPlannerUpgradeRejectsUnsupportedHistory(t *testing.T) {
	manifest := postgresAuthIdentityMigrationManifest()
	for _, previous := range []string{"", "0.9.0"} {
		_, err := BuildArtifactWithPreviousPlanner(context.Background(), "canonical-auth", &manifest, manifest, nil, true, previous)
		if err == nil || !strings.Contains(err.Error(), "unsupported previous PostgreSQL planner version") {
			t.Fatalf("previous planner %q error = %v", previous, err)
		}
	}
}

func TestPostgresAuthIdentityPlannerUpgradeRequiresRetainedLegacyField(t *testing.T) {
	before := postgresAuthIdentityMigrationManifest()
	afterSnapshot := before.Snapshot()
	usernamePath, _ := query.NewPath("username")
	afterSnapshot.Collections[0].Fields = append(afterSnapshot.Collections[0].Fields, schema.Field{
		ID: "users-username", Name: "username", Path: usernamePath, Type: schema.FieldTypeText,
		Category: schema.FieldCategoryScalar, Required: true, Unique: true,
	})
	afterSnapshot.Collections[0].Auth.IdentityField = "username"
	after := schema.NewManifest(afterSnapshot)

	_, err := BuildArtifactWithPreviousPlanner(context.Background(), "replace-auth-identity", &before, after, nil, true, atlasVersionV1)
	if err == nil || !strings.Contains(err.Error(), "create the canonical auth identity artifact before that schema transition") {
		t.Fatalf("replace-all legacy identity fields = %v", err)
	}
}

func postgresAuthIdentityMigrationManifest() schema.Manifest {
	path, _ := query.NewPath("email")
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "auth identity migration"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: "users", Slug: "users", Capabilities: schema.Capabilities{Auth: true, Trash: true},
			Auth: &schema.AuthSettings{IdentityField: "email"},
			Fields: []schema.Field{{
				ID: "users-email", Name: "email", Path: path, Type: schema.FieldTypeEmail,
				Category: schema.FieldCategoryScalar, Required: true, Unique: true,
			}},
		}},
	})
}

func postgresAuthIdentityMigrationManifestWithAddedCollection() schema.Manifest {
	snapshot := postgresAuthIdentityMigrationManifest().Snapshot()
	path, _ := query.NewPath("username")
	snapshot.Collections = append(snapshot.Collections, schema.Collection{
		ID: "staff", Slug: "staff", Capabilities: schema.Capabilities{Auth: true, Trash: true},
		Auth: &schema.AuthSettings{IdentityField: "username"},
		Fields: []schema.Field{{
			ID: "staff-username", Name: "username", Path: path, Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Required: true, Unique: true,
		}},
	})
	return schema.NewManifest(snapshot)
}
