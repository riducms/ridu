package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlasschema "ariga.io/atlas/sql/schema"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAtlasArtifactPlansInitialSchemaAndBlocksFieldDeletion(t *testing.T) {
	before := atlasTestManifest(atlasTextField("posts-title", "title"), atlasTextField("posts-summary", "summary"))
	initial, err := BuildArtifact(context.Background(), "initial", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Planner.Version != AtlasVersion || !artifactContainsSQL(t, initial, "CREATE TABLE") {
		t.Fatalf("initial artifact = %#v", initial)
	}

	after := atlasTestManifest(atlasTextField("posts-title", "title"))
	_, err = BuildArtifact(context.Background(), "remove-summary", &before, after, nil, false)
	var safety *SafetyError
	if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_DROP_COLUMN") {
		t.Fatalf("destructive error = %#v, %v", safety, err)
	}
	allowed, err := BuildArtifact(context.Background(), "remove-summary", &before, after, nil, true)
	if err != nil || !artifactContainsSQL(t, allowed, "DROP COLUMN") || !hasRiskCode(allowed.Risks, "RIDU_DROP_COLUMN") {
		t.Fatalf("allowed artifact = %#v, %v", allowed, err)
	}
}

func TestUploadCollectionRenamesRemainPlannableWithSlugIndependentObjectOwnership(t *testing.T) {
	before := atlasUploadReferenceManifest()

	t.Run("explicit stable identity", func(t *testing.T) {
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections[0].Slug = "assets"
		after := schema.NewManifest(afterSnapshot)
		if _, err := BuildArtifact(context.Background(), "rename-upload-slug", &before, after, nil, false); err != nil {
			t.Fatalf("stable-ID upload slug rename: %v", err)
		}
	})

	t.Run("confirmed derived identity", func(t *testing.T) {
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections[0].ID = "assets"
		afterSnapshot.Collections[0].Slug = "assets"
		after := schema.NewManifest(afterSnapshot)
		rename := Rename{
			Kind: RenameCollection, BeforeCollection: before.Snapshot().Collections[0],
			AfterCollection: after.Snapshot().Collections[0],
		}
		artifact, err := BuildArtifact(context.Background(), "rename-upload-collection", &before, after, []Rename{rename}, false)
		if err != nil {
			t.Fatalf("confirmed upload collection rename: %v", err)
		}
		if !hasStepKind(t, artifact, ridumigration.StepRenameContent) {
			t.Fatalf("upload rename omitted committed semantic intent: %#v", artifactTestSteps(t, artifact))
		}
		if hasStepKind(t, artifact, ridumigration.StepRetireResources) || artifact.MinimumRunnerContract != ridumigration.RunnerContractVersion {
			t.Fatalf("confirmed rename was mistaken for resource removal: contract %d, steps %#v", artifact.MinimumRunnerContract, artifactTestSteps(t, artifact))
		}
	})
}

func TestUploadCollectionRemovalFailsClosedEvenWithDestructiveApproval(t *testing.T) {
	before := atlasUploadReferenceManifest()
	tests := map[string]schema.Manifest{}

	removed := before.Snapshot()
	removed.Collections = nil
	tests["remove collection"] = schema.NewManifest(removed)

	disabled := before.Snapshot()
	disabled.Collections[0].Capabilities.Upload = false
	disabled.Collections[0].Upload = nil
	tests["disable upload capability"] = schema.NewManifest(disabled)

	for name, after := range tests {
		t.Run(name, func(t *testing.T) {
			for _, allowDestructive := range []bool{false, true} {
				_, err := BuildArtifact(context.Background(), "remove-upload", &before, after, nil, allowDestructive)
				var safety *SafetyError
				if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE") ||
					!strings.Contains(err.Error(), "external objects") {
					t.Fatalf("allowDestructive=%t error = %#v, %v", allowDestructive, safety, err)
				}
			}
		})
	}
}

func TestCapabilityDisablesFailClosedBeforeDormantSharedStateCanReactivate(t *testing.T) {
	auth := &schema.AuthSettings{
		IdentityField: "email", SessionDurationSeconds: 3600, PasswordMinLength: 8, PasswordMaxBytes: 128,
		PasswordBcryptCost: 12, MaxLoginAttempts: 5, LockDurationSeconds: 60,
		APIKeys: true, PasswordReset: true, PasswordResetTokenDurationSeconds: 3600,
		VerifyEmail: true, VerificationTokenDurationSeconds: 3600,
	}
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	lock := &schema.DocumentLockSettings{DurationSeconds: 60}
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Capability disable"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "keepers", Slug: "keepers", Fields: []schema.Field{}},
			{ID: "stateful", Slug: "stateful", Capabilities: schema.Capabilities{Auth: true, Versions: true, Locking: true, Trash: true}, Auth: auth, Versions: versions, DocumentLock: lock, Fields: []schema.Field{}},
		},
		Globals: []schema.Global{{ID: "global-state", Slug: "state", Capabilities: schema.Capabilities{Global: true, Versions: true}, Versions: versions, Fields: []schema.Field{}}},
	})
	tests := map[string]struct {
		code   string
		mutate func(*schema.Snapshot)
	}{
		"authentication": {"RIDU_AUTH_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Auth = nil
			snapshot.Collections[1].Capabilities.Auth = false
		}},
		"API keys": {"RIDU_API_KEYS_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Auth.APIKeys = false
		}},
		"password reset": {"RIDU_PASSWORD_RESET_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Auth.PasswordReset = false
		}},
		"email verification": {"RIDU_VERIFY_EMAIL_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Auth.VerifyEmail = false
		}},
		"collection versions": {"RIDU_VERSIONS_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Versions = nil
			snapshot.Collections[1].Capabilities.Versions = false
		}},
		"drafts": {"RIDU_DRAFTS_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Versions.Drafts = false
		}},
		"global versions": {"RIDU_VERSIONS_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Globals[0].Versions = nil
			snapshot.Globals[0].Capabilities.Versions = false
		}},
		"document locking": {"RIDU_DOCUMENT_LOCK_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].DocumentLock = nil
			snapshot.Collections[1].Capabilities.Locking = false
		}},
		"trash": {"RIDU_TRASH_DISABLE_STATE_UNSAFE", func(snapshot *schema.Snapshot) {
			snapshot.Collections[1].Capabilities.Trash = false
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			afterSnapshot := before.Snapshot()
			test.mutate(&afterSnapshot)
			after := schema.NewManifest(afterSnapshot)
			_, err := BuildArtifact(context.Background(), "disable-"+strings.ReplaceAll(name, " ", "-"), &before, after, nil, true)
			var safety *SafetyError
			if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, test.code) {
				t.Fatalf("capability disable risk = %#v, %v", safety, err)
			}
		})
	}
}

func TestLaterReferenceTopologyAdditionsPlanAnOrderedIndexRebuild(t *testing.T) {
	path, _ := query.NewPath("owner")
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Reference topology"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "users", Slug: "users", Fields: []schema.Field{}},
			{ID: "posts", Slug: "posts", Fields: []schema.Field{{
				ID: "posts-owner", Name: "owner", Path: path, Type: schema.FieldTypeRelationship,
				Category: schema.FieldCategoryRelationship,
				Relationship: &schema.RelationshipField{
					CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteNullify,
				},
			}}},
		},
	})
	afterSnapshot := before.Snapshot()
	reviewerPath, _ := query.NewPath("reviewer")
	afterSnapshot.Collections[1].Fields = append(afterSnapshot.Collections[1].Fields, schema.Field{
		ID: "posts-reviewer", Name: "reviewer", Path: reviewerPath, Type: schema.FieldTypeRelationship,
		Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{
			CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteNullify,
		},
	})
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "add-reference", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	rebuild, assertion := -1, -1
	for index, step := range artifactTestSteps(t, artifact) {
		if step.Kind == ridumigration.StepBackfillReferences {
			rebuild = index
		}
		if step.Kind == ridumigration.StepAssertSchema {
			assertion = index
		}
	}
	if rebuild < 0 || assertion < 0 || rebuild >= assertion || !hasRiskLevel(artifact.Risks, "RIDU_REFERENCE_INDEX_REBUILD", ridumigration.RiskWarning) {
		t.Fatalf("reference topology rebuild = steps %#v risks %#v", artifactTestSteps(t, artifact), artifact.Risks)
	}
}

func TestReferenceIndexTopologyDetectsEveryStoredAddressChange(t *testing.T) {
	path, _ := query.NewPath("owner")
	base := schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name:         "Reference topology",
			Localization: &schema.LocalizationSettings{DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}}},
		},
		Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "users", Slug: "users", Fields: []schema.Field{}},
			{ID: "teams", Slug: "teams", Fields: []schema.Field{}},
			{ID: "posts", Slug: "posts", Fields: []schema.Field{{
				ID: "posts-owner", Name: "owner", Path: path, Type: schema.FieldTypeRelationship, Localized: true,
				Category: schema.FieldCategoryRelationship,
				Relationship: &schema.RelationshipField{
					CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteNullify,
				},
			}}},
		},
	}
	tests := map[string]func(*schema.Snapshot){
		"field removal": func(snapshot *schema.Snapshot) { snapshot.Collections[2].Fields = nil },
		"field retype": func(snapshot *schema.Snapshot) {
			snapshot.Collections[2].Fields[0].Type = schema.FieldTypeText
			snapshot.Collections[2].Fields[0].Category = schema.FieldCategoryScalar
			snapshot.Collections[2].Fields[0].Relationship = nil
		},
		"target change": func(snapshot *schema.Snapshot) {
			snapshot.Collections[2].Fields[0].Relationship.CollectionID = "teams"
			snapshot.Collections[2].Fields[0].Relationship.CollectionSlug = "teams"
		},
		"locale change": func(snapshot *schema.Snapshot) {
			snapshot.Application.Localization.Locales = append(snapshot.Application.Localization.Locales, schema.Locale{Code: "fr", Label: "French"})
		},
		"collection removal": func(snapshot *schema.Snapshot) { snapshot.Collections = snapshot.Collections[:2] },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			after := schema.NewManifest(base).Snapshot()
			mutate(&after)
			if !referenceIndexTopologyChanged(base, after) {
				t.Fatal("reference topology change was not detected")
			}
		})
	}
	after := schema.NewManifest(base).Snapshot()
	after.Application.Name = "Presentation-only rename"
	if referenceIndexTopologyChanged(base, after) {
		t.Fatal("application label change planned an unnecessary reference rebuild")
	}
}

func TestRemovingTheFinalResourceKeepsAndClearsReferenceStorage(t *testing.T) {
	path, _ := query.NewPath("owner")
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Final resource removal"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Fields: []schema.Field{{
				ID: "posts-owner", Name: "owner", Path: path, Type: schema.FieldTypeRelationship,
				Category: schema.FieldCategoryRelationship,
				Relationship: &schema.RelationshipField{
					CollectionID: "posts", CollectionSlug: "posts", OnDelete: schema.ReferenceDeleteNullify,
				},
			}},
		}},
	})
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections = []schema.Collection{}
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "remove-final-resource", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if artifactContainsSQL(t, artifact, `DROP TABLE "ridu_document_references"`) {
		t.Fatalf("final resource removal dropped permanent reference storage:\n%s", artifactSQL(t, artifact))
	}
	if !hasStepKind(t, artifact, ridumigration.StepBackfillReferences) {
		t.Fatalf("final resource removal did not clear derived references: %#v", artifactTestSteps(t, artifact))
	}
}

func TestRemovingCollectionsAndGlobalsPlansTypedStateRetirementBeforePhysicalDrop(t *testing.T) {
	versionSettings := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	authSettings := &schema.AuthSettings{IdentityField: "email", SessionDurationSeconds: 86400, APIKeys: true}
	lockSettings := &schema.DocumentLockSettings{DurationSeconds: 60}
	authCollection := func(id schema.StableID, slug schema.CollectionSlug) schema.Collection {
		email := atlasTextField(schema.StableID(string(id)+"-email"), "email")
		email.Required, email.Unique = true, true
		return schema.Collection{
			ID: id, Slug: slug, Fields: []schema.Field{email},
			Capabilities: schema.Capabilities{Auth: true, Versions: true, Locking: true},
			Auth:         authSettings, Versions: versionSettings, DocumentLock: lockSettings,
		}
	}
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Resource retirement"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			authCollection("keepers", "keepers"),
			authCollection("retired-users", "retired-users"),
		},
		Globals: []schema.Global{
			{ID: "global-keep", Slug: "keep", Capabilities: schema.Capabilities{Global: true, Versions: true}, Versions: versionSettings, Fields: []schema.Field{}},
			{ID: "global-retired", Slug: "retired", Capabilities: schema.Capabilities{Global: true, Versions: true}, Versions: versionSettings, Fields: []schema.Field{}},
		},
	})
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections = afterSnapshot.Collections[:1]
	afterSnapshot.Globals = afterSnapshot.Globals[:1]
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "retire-resources", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.MinimumRunnerContract != ridumigration.RunnerContractVersion {
		t.Fatalf("minimum runner contract = %d", artifact.MinimumRunnerContract)
	}
	if !hasRiskLevel(artifact.Risks, "RIDU_RETIRE_RESOURCE_STATE", ridumigration.RiskDestructive) {
		t.Fatalf("resource-retirement risk = %#v", artifact.Risks)
	}
	retirementFound := false
	for _, phase := range artifact.Phases {
		for stepIndex, step := range phase.Steps {
			if step.Kind != ridumigration.StepRetireResources {
				continue
			}
			retirementFound = true
			if phase.Mode != ridumigration.PhaseTransaction || stepIndex+1 >= len(phase.Steps) {
				t.Fatalf("resource retirement is not transactionally paired with a physical drop: %#v", phase)
			}
			var payload ridumigration.RetireResourcesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			want := []schema.StableID{"global-retired", "retired-users"}
			if !slices.Equal(payload.ResourceIDs, want) {
				t.Fatalf("retired resources = %#v, want %#v", payload.ResourceIDs, want)
			}
			next := phase.Steps[stepIndex+1]
			if next.Kind != ridumigration.StepSQL {
				t.Fatalf("step after resource retirement = %#v", next)
			}
			var physical ridumigration.SQLPayload
			if err := json.Unmarshal(next.Payload, &physical); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(physical.SQL, "DROP TABLE "+quote(collectionTable("retired-users"))) &&
				!strings.Contains(physical.SQL, "DROP TABLE "+quote(collectionTable("global-retired"))) {
				t.Fatalf("retirement is not immediately before a removed resource drop: %q", physical.SQL)
			}
		}
	}
	if !retirementFound {
		t.Fatalf("artifact omitted typed resource retirement: %#v", artifact.Phases)
	}
	artifact.PreviousArtifactDigest = strings.Repeat("0", 64)
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ridumigration.DecodeArtifact(encoded)
	if err != nil {
		t.Fatalf("decode resource-retirement artifact: %v", err)
	}
	wantDigest, _ := artifact.Digest()
	gotDigest, _ := decoded.Digest()
	if gotDigest != wantDigest {
		t.Fatalf("resource-retirement artifact digest changed through codec: %s != %s", gotDigest, wantDigest)
	}
}

func TestRemovedResourceIdentityIsKindScopedAndConfirmedRenamesArePreserved(t *testing.T) {
	before := schema.Snapshot{
		Collections: []schema.Collection{{ID: "old-collection"}, {ID: "cross-kind"}},
		Globals:     []schema.Global{{ID: "old-global"}, {ID: "global-cross-kind"}},
	}
	after := schema.Snapshot{
		Collections: []schema.Collection{{ID: "new-collection"}, {ID: "global-cross-kind"}},
		Globals:     []schema.Global{{ID: "cross-kind"}},
	}
	mapping := atlasIdentityMap{collections: map[schema.StableID]schema.StableID{"old-collection": "new-collection"}}
	want := []schema.StableID{"cross-kind", "global-cross-kind", "old-global"}
	if got := removedResourceIDs(before, after, mapping); !slices.Equal(got, want) {
		t.Fatalf("removed resource IDs = %#v, want %#v", got, want)
	}
}

func TestResourceRemovalFailsClosedWhenAnyReferenceBearingPhysicalRootSurvives(t *testing.T) {
	for _, shape := range []string{"polymorphic", "group", "array", "blocks"} {
		t.Run(shape, func(t *testing.T) {
			beforeRoot := retirementReferenceRoot(t, shape, true)
			afterRoot := retirementReferenceRoot(t, shape, false)
			before := retirementReferenceManifest(beforeRoot)
			afterSnapshot := before.Snapshot()
			afterSnapshot.Collections = afterSnapshot.Collections[1:]
			afterSnapshot.Collections[1].Fields = []schema.Field{afterRoot}
			after := schema.NewManifest(afterSnapshot)
			_, err := BuildArtifact(context.Background(), "unsafe-reference-root-"+shape, &before, after, nil, true)
			var safety *SafetyError
			if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_RESOURCE_REMOVAL_REFERENCE_ROOT_UNSAFE") {
				t.Fatalf("surviving %s root removal error = %#v, %v", shape, safety, err)
			}
		})
	}
}

func TestResourceRemovalDropsReferenceRootAndPurgesDependentVersionHistory(t *testing.T) {
	before := retirementReferenceManifest(retirementReferenceRoot(t, "group", true))
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections = afterSnapshot.Collections[1:]
	afterSnapshot.Collections[1].Fields = []schema.Field{}
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "retire-reference-root", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRiskLevel(artifact.Risks, "RIDU_RETIRE_DEPENDENT_VERSION_HISTORY", ridumigration.RiskDestructive) {
		t.Fatalf("dependent version-history risk = %#v", artifact.Risks)
	}
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
			found = slices.Equal(payload.PurgeVersionOwnerIDs, []schema.StableID{"entries"})
		}
	}
	if !found {
		t.Fatalf("resource retirement omitted dependent entries version purge: %#v", artifact.Phases)
	}
}

func TestStoredReferenceShapeDecreasesFailClosedBeforeDormantValuesCanReattach(t *testing.T) {
	direct := directRetirementReferenceRoot("retired-users")
	upload := uploadRetirementReferenceRoot("media")
	tests := map[string]struct {
		before schema.Field
		after  *schema.Field
	}{
		"direct field removal": {before: direct},
		"direct field retype": {
			before: direct,
			after:  pointerToField(atlasTextField(direct.ID, direct.Name)),
		},
		"polymorphic target-list decrease": {
			before: retirementReferenceRoot(t, "polymorphic", true),
			after:  pointerToField(retirementReferenceRoot(t, "polymorphic", false)),
		},
		"upload field removal": {before: upload},
		"nested group reference removal": {
			before: retirementReferenceRoot(t, "group", true),
			after:  pointerToField(referenceRootWithoutChildren(retirementReferenceRoot(t, "group", true))),
		},
		"nested array reference removal": {
			before: retirementReferenceRoot(t, "array", true),
			after:  pointerToField(referenceRootWithoutChildren(retirementReferenceRoot(t, "array", true))),
		},
		"nested block reference removal": {
			before: retirementReferenceRoot(t, "blocks", true),
			after:  pointerToField(referenceRootWithoutChildren(retirementReferenceRoot(t, "blocks", true))),
		},
		"nested reference localization change": {
			before: referenceRootWithLocalizedLeaf(retirementReferenceRoot(t, "group", true), true),
			after:  pointerToField(referenceRootWithLocalizedLeaf(retirementReferenceRoot(t, "group", true), false)),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := referenceShapeDecreaseManifest(test.before)
			afterSnapshot := before.Snapshot()
			afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = nil
			if test.after != nil {
				afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = []schema.Field{*test.after}
			}
			after := schema.NewManifest(afterSnapshot)
			for _, allowDestructive := range []bool{false, true} {
				_, err := BuildArtifact(context.Background(), "decrease-reference-shape", &before, after, nil, allowDestructive)
				var safety *SafetyError
				if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") ||
					!strings.Contains(err.Error(), "current values or version snapshots") {
					t.Fatalf("allowDestructive=%t reference-shape error = %#v, %v", allowDestructive, safety, err)
				}
			}
		})
	}
}

func TestRunnerRejectsForgedReferenceShapeDecreaseArtifacts(t *testing.T) {
	tests := map[string]struct {
		before schema.Field
		after  *schema.Field
	}{
		"direct removal": {before: directRetirementReferenceRoot("retired-users")},
		"direct retype": {
			before: directRetirementReferenceRoot("retired-users"),
			after:  pointerToField(atlasTextField("entries-content", "content")),
		},
		"polymorphic target decrease": {
			before: retirementReferenceRoot(t, "polymorphic", true),
			after:  pointerToField(retirementReferenceRoot(t, "polymorphic", false)),
		},
		"upload removal": {before: uploadRetirementReferenceRoot("media")},
		"nested group removal": {
			before: retirementReferenceRoot(t, "group", true),
			after:  pointerToField(referenceRootWithoutChildren(retirementReferenceRoot(t, "group", true))),
		},
		"nested array removal": {
			before: retirementReferenceRoot(t, "array", true),
			after:  pointerToField(referenceRootWithoutChildren(retirementReferenceRoot(t, "array", true))),
		},
		"nested block removal": {
			before: retirementReferenceRoot(t, "blocks", true),
			after:  pointerToField(referenceRootWithoutChildren(retirementReferenceRoot(t, "blocks", true))),
		},
		"nested localization change": {
			before: referenceRootWithLocalizedLeaf(retirementReferenceRoot(t, "group", true), true),
			after:  pointerToField(referenceRootWithLocalizedLeaf(retirementReferenceRoot(t, "group", true), false)),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := referenceShapeDecreaseManifest(test.before)
			afterSnapshot := before.Snapshot()
			afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = nil
			if test.after != nil {
				afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = []schema.Field{*test.after}
			}
			after := schema.NewManifest(afterSnapshot)
			artifact, err := ridumigration.NewArtifact("forged-reference-shape-decrease", atlasPlanner(), &before, after)
			if err != nil {
				t.Fatal(err)
			}
			steps := []ridumigration.Operation{}
			if test.after == nil && len(test.before.Path.Segments()) == 1 {
				steps = append(steps, ridumigration.Operation{
					Kind: ridumigration.StepSQL, Name: "drop old reference root",
					SQL: "ALTER TABLE " + quote(collectionTable("entries")) + " DROP COLUMN " + quote(fieldColumn(test.before.ID)),
				})
			}
			steps = append(steps, ridumigration.Operation{Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema"})
			artifact.Phases, err = phasesFromOperations(artifact.FromDigest, steps)
			if err != nil {
				t.Fatal(err)
			}
			if err := artifact.Validate(); err != nil {
				t.Fatalf("generic forged artifact validation: %v", err)
			}
			if err := validatePostgresResourceRetirementTopology(artifact); err == nil ||
				!strings.Contains(err.Error(), "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") {
				t.Fatalf("runner reference-shape preflight = %v", err)
			}
		})
	}
}

func TestAmbiguousNestedReferenceRenameMappingsFailClosed(t *testing.T) {
	beforeRoot := retirementReferenceRoot(t, "group", true)
	before := referenceShapeDecreaseManifest(beforeRoot)
	beforeReference := beforeRoot.Nested.ResolvedFields()[0]
	dormantPath, _ := query.ParsePath("content.dormant")
	dormant := schema.Field{
		ID: "entries-content-dormant", Name: "dormant", Path: dormantPath,
		Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{},
	}
	decoyPath, _ := query.ParsePath("content.decoy")
	decoy := beforeReference
	decoy.ID, decoy.Name, decoy.Path = "entries-content-decoy", "decoy", decoyPath
	afterRoot := beforeRoot
	afterRoot.Nested = &schema.NestedField{Fields: []schema.Field{dormant, decoy}}
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = []schema.Field{afterRoot}
	after := schema.NewManifest(afterSnapshot)
	ownerBefore := before.Snapshot().Collections[len(before.Snapshot().Collections)-1]
	ownerAfter := after.Snapshot().Collections[len(after.Snapshot().Collections)-1]
	renames := []Rename{
		{Kind: RenameField, BeforeCollection: ownerBefore, AfterCollection: ownerAfter, BeforeField: &beforeReference, AfterField: &dormant},
		{Kind: RenameField, BeforeCollection: ownerBefore, AfterCollection: ownerAfter, BeforeField: &beforeReference, AfterField: &decoy},
	}
	for _, allowDestructive := range []bool{false, true} {
		if _, err := BuildArtifact(context.Background(), "ambiguous-nested-reference-renames", &before, after, renames, allowDestructive); err == nil ||
			!strings.Contains(err.Error(), "RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS") {
			t.Fatalf("allowDestructive=%t ambiguous planner mapping = %v", allowDestructive, err)
		}
	}

	artifact, err := ridumigration.NewArtifact("forged-ambiguous-nested-reference-renames", atlasPlanner(), &before, after)
	if err != nil {
		t.Fatal(err)
	}
	steps := []ridumigration.Operation{
		{Kind: ridumigration.StepRenameContent, Name: "hide reference in dormant text", Rename: &ridumigration.Rename{
			CollectionBefore: "entries", CollectionAfter: "entries", FieldBefore: "content.reference", FieldAfter: "content.dormant",
		}},
		{Kind: ridumigration.StepRenameContent, Name: "map reference to decoy", Rename: &ridumigration.Rename{
			CollectionBefore: "entries", CollectionAfter: "entries", FieldBefore: "content.reference", FieldAfter: "content.decoy",
		}},
		{Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema"},
	}
	artifact.Phases, err = phasesFromOperations(artifact.FromDigest, steps)
	if err != nil {
		t.Fatal(err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("generic forged duplicate-mapping artifact validation: %v", err)
	}
	if err := validatePostgresResourceRetirementTopology(artifact); err == nil ||
		!strings.Contains(err.Error(), "RIDU_REFERENCE_RENAME_MAPPING_AMBIGUOUS") {
		t.Fatalf("runner ambiguous nested mapping preflight = %v", err)
	}
}

func TestReferenceLocaleRemovalFailsClosedBeforeLocalizedValuesCanReattach(t *testing.T) {
	root := referenceRootWithLocalizedLeaf(retirementReferenceRoot(t, "group", true), true)
	beforeSnapshot := referenceShapeDecreaseManifest(root).Snapshot()
	beforeSnapshot.Application.Localization.Locales = append(
		beforeSnapshot.Application.Localization.Locales,
		schema.Locale{Code: "fr", Label: "French"},
	)
	before := schema.NewManifest(beforeSnapshot)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Application.Localization.Locales = afterSnapshot.Application.Localization.Locales[:1]
	after := schema.NewManifest(afterSnapshot)
	for _, allowDestructive := range []bool{false, true} {
		_, err := BuildArtifact(context.Background(), "remove-reference-locale", &before, after, nil, allowDestructive)
		var safety *SafetyError
		if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") ||
			!strings.Contains(err.Error(), "remove application locale fr") {
			t.Fatalf("allowDestructive=%t locale-removal planner error = %#v, %v", allowDestructive, safety, err)
		}
	}

	artifact, err := ridumigration.NewArtifact("forged-reference-locale-removal", atlasPlanner(), &before, after)
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
		t.Fatalf("generic locale-removal artifact validation: %v", err)
	}
	if err := validatePostgresResourceRetirementTopology(artifact); err == nil ||
		!strings.Contains(err.Error(), "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE") {
		t.Fatalf("runner locale-removal preflight = %v", err)
	}
}

func TestStoredReferenceShapeTargetAdditionAndConfirmedRenameRemainPlannable(t *testing.T) {
	t.Run("polymorphic target addition", func(t *testing.T) {
		before := referenceShapeDecreaseManifest(retirementReferenceRoot(t, "polymorphic", false))
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = []schema.Field{retirementReferenceRoot(t, "polymorphic", true)}
		after := schema.NewManifest(afterSnapshot)
		if _, err := BuildArtifact(context.Background(), "add-reference-target", &before, after, nil, false); err != nil {
			t.Fatalf("additive target transition: %v", err)
		}
	})

	t.Run("confirmed direct field rename", func(t *testing.T) {
		beforeRoot := directRetirementReferenceRoot("retired-users")
		before := referenceShapeDecreaseManifest(beforeRoot)
		afterRoot := beforeRoot
		afterRoot.ID, afterRoot.Name = "entries-author", "author"
		afterRoot.Path, _ = query.NewPath("author")
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = []schema.Field{afterRoot}
		after := schema.NewManifest(afterSnapshot)
		ownerBefore := before.Snapshot().Collections[len(before.Snapshot().Collections)-1]
		ownerAfter := after.Snapshot().Collections[len(after.Snapshot().Collections)-1]
		rename := Rename{
			Kind: RenameField, BeforeCollection: ownerBefore, AfterCollection: ownerAfter,
			BeforeField: &beforeRoot, AfterField: &afterRoot,
		}
		artifact, err := BuildArtifact(context.Background(), "rename-reference-field", &before, after, []Rename{rename}, false)
		if err != nil {
			t.Fatalf("confirmed reference rename: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("confirmed reference rename runner preflight: %v", err)
		}
	})

	t.Run("confirmed nested root rename", func(t *testing.T) {
		beforeRoot := retirementReferenceRoot(t, "group", true)
		before := referenceShapeDecreaseManifest(beforeRoot)
		afterRoot := beforeRoot
		afterRoot.ID, afterRoot.Name = "entries-body", "body"
		afterRoot.Path, _ = query.NewPath("body")
		afterRoot.Nested = &schema.NestedField{Fields: append([]schema.Field(nil), beforeRoot.Nested.ResolvedFields()...)}
		afterRoot.Nested.ResolvedFields()[0].ID = "entries-body-reference"
		afterRoot.Nested.ResolvedFields()[0].Path, _ = query.ParsePath("body.reference")
		afterSnapshot := before.Snapshot()
		afterSnapshot.Collections[len(afterSnapshot.Collections)-1].Fields = []schema.Field{afterRoot}
		after := schema.NewManifest(afterSnapshot)
		ownerBefore := before.Snapshot().Collections[len(before.Snapshot().Collections)-1]
		ownerAfter := after.Snapshot().Collections[len(after.Snapshot().Collections)-1]
		rename := Rename{
			Kind: RenameField, BeforeCollection: ownerBefore, AfterCollection: ownerAfter,
			BeforeField: &beforeRoot, AfterField: &afterRoot,
		}
		artifact, err := BuildArtifact(context.Background(), "rename-reference-root", &before, after, []Rename{rename}, false)
		if err != nil {
			t.Fatalf("confirmed nested reference-root rename: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("confirmed nested reference-root rename runner preflight: %v", err)
		}
	})

	t.Run("confirmed collection rename rehydrates same-path derived field IDs", func(t *testing.T) {
		managerPath, _ := query.NewPath("manager")
		beforeManager := schema.Field{
			ID: "users-manager", Name: "manager", Path: managerPath,
			Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteNullify,
			},
		}
		afterManager := beforeManager
		afterManager.ID = "members-manager"
		afterManager.Relationship = &schema.RelationshipField{
			CollectionID: "members", CollectionSlug: "members", OnDelete: schema.ReferenceDeleteNullify,
		}
		beforeCollection := schema.Collection{ID: "users", Slug: "users", Fields: []schema.Field{beforeManager}}
		afterCollection := schema.Collection{ID: "members", Slug: "members", Fields: []schema.Field{afterManager}}
		before := schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "Same-path reference rename"},
			Plugins: []schema.Plugin{}, Collections: []schema.Collection{beforeCollection},
		})
		after := schema.NewManifest(schema.Snapshot{
			Version: schema.CurrentVersion, Application: schema.Application{Name: "Same-path reference rename"},
			Plugins: []schema.Plugin{}, Collections: []schema.Collection{afterCollection},
		})
		rename := Rename{
			Kind: RenameCollection, BeforeCollection: beforeCollection, AfterCollection: afterCollection,
			Fields: []FieldRename{{Before: beforeManager, After: afterManager}},
		}
		artifact, err := BuildArtifact(context.Background(), "rename-same-path-reference", &before, after, []Rename{rename}, false)
		if err != nil {
			t.Fatalf("confirmed same-path collection rename: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
			t.Fatalf("confirmed same-path collection rename runner preflight: %v", err)
		}
	})
}

func TestResourceRetirementArtifactsFailClosedOnPayloadAndDropTampering(t *testing.T) {
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Retirement tampering"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "keepers", Slug: "keepers", Fields: []schema.Field{}},
			{ID: "retired-a", Slug: "retired-a", Fields: []schema.Field{}},
			{ID: "retired-b", Slug: "retired-b", Fields: []schema.Field{}},
		},
	})
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections = afterSnapshot.Collections[:1]
	after := schema.NewManifest(afterSnapshot)
	original, err := BuildArtifact(context.Background(), "retirement-tampering", &before, after, nil, true)
	if err != nil {
		t.Fatal(err)
	}

	for name, ids := range map[string][]schema.StableID{
		"omitted removed ID": {"retired-a"},
		"extra survivor ID":  {"keepers", "retired-a", "retired-b"},
	} {
		t.Run(name, func(t *testing.T) {
			artifact := cloneRetirementArtifact(original)
			setRetirementResourceIDs(t, &artifact, ids)
			recomputeTestPhysicalDigests(t, &artifact)
			if err := artifact.Validate(); err == nil {
				t.Fatalf("tampered payload validation = %v", err)
			}
		})
	}

	t.Run("extra dependent version owner", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		for phaseIndex := range artifact.Phases {
			for stepIndex := range artifact.Phases[phaseIndex].Steps {
				step := &artifact.Phases[phaseIndex].Steps[stepIndex]
				if step.Kind != ridumigration.StepRetireResources {
					continue
				}
				var payload ridumigration.RetireResourcesPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				payload.PurgeVersionOwnerIDs = []schema.StableID{"keepers"}
				step.Payload, _ = ridumigration.MarshalStepPayload(payload)
			}
		}
		recomputeTestPhysicalDigests(t, &artifact)
		if err := artifact.Validate(); err != nil {
			t.Fatalf("generic dependent-owner validation: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err == nil || !strings.Contains(err.Error(), "version-owner purge") {
			t.Fatalf("dependent version-owner binding = %v", err)
		}
	})

	t.Run("missing physical drop", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		replaceResourceDropWithSQL(t, &artifact, "retired-b", "SELECT 1")
		recomputeTestPhysicalDigests(t, &artifact)
		if err := artifact.Validate(); err != nil {
			t.Fatalf("generic artifact validation should leave physical binding to the PostgreSQL runner: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err == nil || !strings.Contains(err.Error(), "physical table drop") {
			t.Fatalf("missing physical drop validation = %v", err)
		}
	})

	t.Run("drop split into later phase", func(t *testing.T) {
		artifact := cloneRetirementArtifact(original)
		replaceResourceDropWithSQL(t, &artifact, "retired-b", "SELECT 1")
		payload, _ := ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: "DROP TABLE " + quote(collectionTable("retired-b"))})
		var assertion ridumigration.Step
		for phaseIndex := range artifact.Phases {
			phase := &artifact.Phases[phaseIndex]
			for stepIndex, step := range phase.Steps {
				if step.Kind != ridumigration.StepAssertSchema {
					continue
				}
				assertion = step
				phase.Steps = append(phase.Steps[:stepIndex], phase.Steps[stepIndex+1:]...)
				break
			}
		}
		if assertion.Kind == "" {
			t.Fatal("schema assertion phase not found")
		}
		physical := ridumigration.Step{ID: "step-split-drop", Kind: ridumigration.StepSQL, ExecutorVersion: 1, Name: "split retired resource drop", Payload: payload}
		artifact.Phases = append(artifact.Phases, ridumigration.Phase{
			ID: "phase-split", Mode: ridumigration.PhaseTransaction, PhysicalContractVersion: ridumigration.PhysicalContractVersion,
			Steps: []ridumigration.Step{physical, assertion},
		})
		recomputeTestPhysicalDigests(t, &artifact)
		if err := artifact.Validate(); err != nil {
			t.Fatalf("generic split-phase artifact validation: %v", err)
		}
		if err := validatePostgresResourceRetirementTopology(artifact); err == nil || !strings.Contains(err.Error(), "same transaction phase") {
			t.Fatalf("split physical drop validation = %v", err)
		}
	})
}

func cloneRetirementArtifact(source ridumigration.Artifact) ridumigration.Artifact {
	clone := source
	clone.Risks = append([]ridumigration.Risk(nil), source.Risks...)
	clone.Phases = make([]ridumigration.Phase, len(source.Phases))
	for phaseIndex, phase := range source.Phases {
		clone.Phases[phaseIndex] = phase
		clone.Phases[phaseIndex].Steps = make([]ridumigration.Step, len(phase.Steps))
		for stepIndex, step := range phase.Steps {
			clone.Phases[phaseIndex].Steps[stepIndex] = step
			clone.Phases[phaseIndex].Steps[stepIndex].Payload = append(json.RawMessage(nil), step.Payload...)
		}
	}
	return clone
}

func setRetirementResourceIDs(t *testing.T, artifact *ridumigration.Artifact, ids []schema.StableID) {
	t.Helper()
	for phaseIndex := range artifact.Phases {
		for stepIndex := range artifact.Phases[phaseIndex].Steps {
			step := &artifact.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepRetireResources {
				continue
			}
			var payload ridumigration.RetireResourcesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload.ResourceIDs = append([]schema.StableID(nil), ids...)
			var err error
			step.Payload, err = ridumigration.MarshalStepPayload(payload)
			if err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("resource-retirement step not found")
}

func replaceResourceDropWithSQL(t *testing.T, artifact *ridumigration.Artifact, resourceID schema.StableID, replacement string) {
	t.Helper()
	want := "DROP TABLE " + quote(collectionTable(resourceID))
	for phaseIndex := range artifact.Phases {
		for stepIndex := range artifact.Phases[phaseIndex].Steps {
			step := &artifact.Phases[phaseIndex].Steps[stepIndex]
			if step.Kind != ridumigration.StepSQL {
				continue
			}
			var payload ridumigration.SQLPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(payload.SQL, want) {
				continue
			}
			step.Payload, _ = ridumigration.MarshalStepPayload(ridumigration.SQLPayload{SQL: replacement})
			return
		}
	}
	t.Fatalf("physical drop for %s not found", resourceID)
}

func retirementReferenceManifest(root schema.Field) schema.Manifest {
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Reference-root retirement"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "retired-users", Slug: "retired-users", Fields: []schema.Field{}},
			{ID: "keepers", Slug: "keepers", Fields: []schema.Field{}},
			{ID: "entries", Slug: "entries", Capabilities: schema.Capabilities{Versions: true}, Versions: versions, Fields: []schema.Field{root}},
		},
	})
}

func referenceShapeDecreaseManifest(root schema.Field) schema.Manifest {
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion,
		Application: schema.Application{
			Name: "Reference-shape decrease",
			Localization: &schema.LocalizationSettings{
				DefaultLocale: "en", Locales: []schema.Locale{{Code: "en", Label: "English"}},
			},
		},
		Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "retired-users", Slug: "retired-users", Fields: []schema.Field{}},
			{ID: "keepers", Slug: "keepers", Fields: []schema.Field{}},
			{ID: "media", Slug: "media", Capabilities: schema.Capabilities{Upload: true}, Upload: &schema.UploadSettings{}, Fields: []schema.Field{}},
			{ID: "entries", Slug: "entries", Capabilities: schema.Capabilities{Versions: true}, Versions: versions, Fields: []schema.Field{root}},
		},
	})
}

func directRetirementReferenceRoot(target schema.StableID) schema.Field {
	path, _ := query.NewPath("content")
	return schema.Field{
		ID: "entries-content", Name: "content", Path: path,
		Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{
			CollectionID: target, CollectionSlug: schema.CollectionSlug(target), OnDelete: schema.ReferenceDeleteNullify,
		},
	}
}

func uploadRetirementReferenceRoot(target schema.StableID) schema.Field {
	path, _ := query.NewPath("content")
	return schema.Field{
		ID: "entries-content", Name: "content", Path: path,
		Type: schema.FieldTypeUpload, Category: schema.FieldCategoryUpload,
		Upload: &schema.UploadField{
			CollectionID: target, CollectionSlug: schema.CollectionSlug(target), OnDelete: schema.ReferenceDeleteNullify,
		},
	}
}

func pointerToField(field schema.Field) *schema.Field { return &field }

func referenceRootWithoutChildren(root schema.Field) schema.Field {
	if root.Nested != nil {
		root.Nested = &schema.NestedField{}
	}
	if root.Blocks != nil {
		blocks := append([]schema.BlockType(nil), root.Blocks.ResolvedTypes()...)
		for index := range blocks {
			blocks[index].Fields = nil
		}
		root.Blocks = &schema.BlocksField{Types: blocks}
	}
	return root
}

func referenceRootWithLocalizedLeaf(root schema.Field, localized bool) schema.Field {
	if root.Nested != nil {
		root.Nested = &schema.NestedField{Fields: append([]schema.Field(nil), root.Nested.ResolvedFields()...)}
		root.Nested.ResolvedFields()[0].Localized = localized
	}
	return root
}

func retirementReferenceRoot(t *testing.T, shape string, includeRetired bool) schema.Field {
	t.Helper()
	targets := []schema.RelationshipTarget{{CollectionID: "keepers", CollectionSlug: "keepers"}}
	if includeRetired {
		targets = append(targets, schema.RelationshipTarget{CollectionID: "retired-users", CollectionSlug: "retired-users"})
	}
	rootPath, _ := query.NewPath("content")
	referencePath, _ := query.ParsePath("content.reference")
	reference := schema.Field{
		ID: "entries-content-reference", Name: "reference", Path: referencePath,
		Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
		Relationship: &schema.RelationshipField{Targets: targets, HasMany: true, Polymorphic: true, OnDelete: schema.ReferenceDeleteNullify},
	}
	if shape == "polymorphic" {
		reference.ID, reference.Name, reference.Path = "entries-content", "content", rootPath
		return reference
	}
	root := schema.Field{ID: "entries-content", Name: "content", Path: rootPath, Category: schema.FieldCategoryNested}
	switch shape {
	case "group", "array":
		root.Type = schema.FieldTypeGroup
		if shape == "array" {
			root.Type = schema.FieldTypeArray
		}
		root.Nested = &schema.NestedField{Fields: []schema.Field{reference}}
	case "blocks":
		root.Type = schema.FieldTypeBlocks
		root.Blocks = &schema.BlocksField{Types: []schema.BlockType{{Slug: "reference", Labels: schema.BlockLabels{Singular: "Reference"}, Fields: []schema.Field{reference}}}}
	default:
		t.Fatalf("unknown retirement reference shape %q", shape)
	}
	return root
}

func TestReferencePolicyOnlyChangeDoesNotRebuildDerivedEntries(t *testing.T) {
	path, _ := query.NewPath("owner")
	before := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Policy-only change"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{
			{ID: "users", Slug: "users", Fields: []schema.Field{}},
			{ID: "posts", Slug: "posts", Fields: []schema.Field{{
				ID: "posts-owner", Name: "owner", Path: path, Type: schema.FieldTypeRelationship,
				Category: schema.FieldCategoryRelationship,
				Relationship: &schema.RelationshipField{
					CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteNullify,
				},
			}}},
		},
	})
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[1].Fields[0].Relationship.OnDelete = schema.ReferenceDeleteRestrict
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "change-reference-policy", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if hasStepKind(t, artifact, ridumigration.StepBackfillReferences) || hasRiskCode(artifact.Risks, "RIDU_REFERENCE_INDEX_REBUILD") {
		t.Fatalf("policy-only change planned an unnecessary reference rebuild: steps %#v risks %#v", artifactTestSteps(t, artifact), artifact.Risks)
	}
}

func TestAtlasSchemaIncludesGlobalSingletonTables(t *testing.T) {
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	snapshot := manifest.Snapshot()
	globalTitle := atlasTextField("global-site-settings-site-name", "siteName")
	globalTitle.Index = true
	snapshot.Globals = []schema.Global{{
		ID: "global-site-settings", Slug: "site-settings",
		Labels:       schema.CollectionLabels{Singular: "Site settings", Plural: "Site settings"},
		Capabilities: schema.Capabilities{Global: true, Versions: true},
		Versions:     &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
		Fields:       []schema.Field{globalTitle},
	}}
	physical := atlasSchema(schema.NewManifest(snapshot), atlasIdentityMap{})
	table, exists := physical.Table(collectionTable("global-site-settings"))
	if !exists {
		t.Fatalf("global table %q is missing", collectionTable("global-site-settings"))
	}
	if len(table.Indexes) != 1 || table.Indexes[0].Unique {
		t.Fatalf("global field indexes = %#v, want one ordinary physical index", table.Indexes)
	}
	if _, exists := physical.Table("ridu_versions"); !exists {
		t.Fatal("version storage is missing for a versioned global")
	}
}

func TestAtlasSchemaIncludesDurableTasks(t *testing.T) {
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	table, exists := atlasSchema(manifest, atlasIdentityMap{}).Table("ridu_tasks")
	if !exists {
		t.Fatal("schema is missing ridu_tasks")
	}
	wantColumns := []string{
		"id", "task_slug", "queue", "concurrency_key", "input", "output", "state", "run_at", "attempts",
		"max_attempts", "retry_delay_ms", "max_retry_delay_ms", "backoff", "timeout_ms", "retention_ms",
		"lease_token", "lease_expires_at", "target_collection_id", "target_document_id",
		"requested_by_collection_id", "requested_by_document_id", "last_error_code", "last_error", "created_at",
		"updated_at", "completed_at", "retain_until",
	}
	if len(table.Columns) != len(wantColumns) {
		t.Fatalf("ridu_tasks columns = %d, want %d", len(table.Columns), len(wantColumns))
	}
	for index, name := range wantColumns {
		if table.Columns[index].Name != name {
			t.Fatalf("ridu_tasks column %d = %q, want %q", index, table.Columns[index].Name, name)
		}
	}
	for _, name := range []string{"concurrency_key", "output", "lease_token", "lease_expires_at", "target_collection_id", "target_document_id", "requested_by_collection_id", "requested_by_document_id", "last_error_code", "last_error", "completed_at", "retain_until"} {
		column, _ := table.Column(name)
		if column == nil || !column.Type.Null {
			t.Errorf("ridu_tasks nullable column %q = %#v", name, column)
		}
	}
	if table.PrimaryKey == nil || len(table.PrimaryKey.Parts) != 1 || table.PrimaryKey.Parts[0].C == nil || table.PrimaryKey.Parts[0].C.Name != "id" {
		t.Fatalf("ridu_tasks primary key = %#v", table.PrimaryKey)
	}
	checks := map[string]string{}
	for _, attribute := range table.Attrs {
		if check, ok := attribute.(*atlasschema.Check); ok {
			checks[check.Name] = check.Expr
		}
	}
	for _, name := range []string{
		"ridu_tasks_state_check", "ridu_tasks_backoff_check", "ridu_tasks_attempts_check", "ridu_tasks_max_attempts_check",
		"ridu_tasks_retry_check", "ridu_tasks_timeout_retention_check", "ridu_tasks_identifier_bounds_check",
		"ridu_tasks_payload_bounds_check", "ridu_tasks_error_bounds_check", "ridu_tasks_reference_bounds_check",
		"ridu_tasks_target_pair_check", "ridu_tasks_requester_pair_check", "ridu_tasks_lease_check",
		"ridu_tasks_terminal_check", "ridu_tasks_output_state_check",
	} {
		if checks[name] == "" {
			t.Errorf("ridu_tasks is missing check %q", name)
		}
	}
	if got, want := checks["ridu_tasks_retry_check"], fmt.Sprintf("retry_delay_ms BETWEEN 1 AND %d AND max_retry_delay_ms BETWEEN retry_delay_ms AND %d", store.MaxTaskRetryDelay.Milliseconds(), store.MaxTaskRetryDelay.Milliseconds()); got != want {
		t.Errorf("retry bounds check = %q, want %q", got, want)
	}
	if got, want := checks["ridu_tasks_timeout_retention_check"], fmt.Sprintf("timeout_ms BETWEEN 1 AND %d AND retention_ms BETWEEN %d AND %d", store.MaxTaskTimeout.Milliseconds(), store.MinTaskRetention.Milliseconds(), store.MaxTaskRetention.Milliseconds()); got != want {
		t.Errorf("timeout/retention bounds check = %q, want %q", got, want)
	}
	if got, want := checks["ridu_tasks_payload_bounds_check"], fmt.Sprintf("octet_length(input::text) BETWEEN 1 AND %d AND (output IS NULL OR octet_length(output::text) BETWEEN 1 AND %d)", store.MaxTaskPayloadBytes, store.MaxTaskPayloadBytes); got != want {
		t.Errorf("payload bounds check = %q, want %q", got, want)
	}
	wantIndexes := map[string]struct {
		columns   []string
		predicate string
	}{
		"ridu_tasks_due_idx":         {[]string{"state", "run_at", "created_at", "id"}, "(state = ANY (ARRAY['queued'::text, 'running'::text]))"},
		"ridu_tasks_lease_idx":       {[]string{"lease_expires_at"}, "(state = 'running'::text)"},
		"ridu_tasks_concurrency_idx": {[]string{"queue", "concurrency_key", "state", "lease_expires_at"}, "concurrency_key IS NOT NULL"},
		"ridu_tasks_target_idx":      {[]string{"task_slug", "target_collection_id", "target_document_id", "state", "run_at"}, "target_collection_id IS NOT NULL"},
		"ridu_tasks_requester_idx":   {[]string{"requested_by_collection_id", "requested_by_document_id"}, "requested_by_collection_id IS NOT NULL"},
		"ridu_tasks_retention_idx":   {[]string{"retain_until", "id"}, "retain_until IS NOT NULL"},
	}
	if len(table.Indexes) != len(wantIndexes) {
		t.Fatalf("ridu_tasks indexes = %#v", table.Indexes)
	}
	for _, index := range table.Indexes {
		want, exists := wantIndexes[index.Name]
		if !exists {
			t.Errorf("unexpected ridu_tasks index %q", index.Name)
			continue
		}
		var columns []string
		for _, part := range index.Parts {
			if part.C != nil {
				columns = append(columns, part.C.Name)
			}
		}
		if !slices.Equal(columns, want.columns) {
			t.Errorf("index %s columns = %v, want %v", index.Name, columns, want.columns)
		}
		predicate := ""
		for _, attribute := range index.Attrs {
			if value, ok := attribute.(*atlaspostgres.IndexPredicate); ok {
				predicate = value.P
			}
		}
		if predicate != want.predicate {
			t.Errorf("index %s predicate = %q, want %q", index.Name, predicate, want.predicate)
		}
	}
}

func TestScheduledPublishConcurrencyKeysRespectDurableBounds(t *testing.T) {
	if got := scheduledPublishConcurrencyKey("posts", "post-1"); got != "posts:post-1" {
		t.Fatalf("ordinary concurrency key = %q", got)
	}
	longID := strings.Repeat("document", 60)
	key := scheduledPublishConcurrencyKey("posts", longID)
	if len(key) > store.MaxTaskConcurrencyKeyBytes || !strings.HasPrefix(key, "sha256-") || key != scheduledPublishConcurrencyKey("posts", longID) {
		t.Fatalf("bounded concurrency key = %q", key)
	}
}

func TestAtlasSchemaUsesTypedColumnsAndIndexesPerLocale(t *testing.T) {
	localized := atlasTextField("posts-title", "title")
	localized.Required, localized.Unique, localized.Localized = true, true, true
	manifest := atlasTestManifest(localized)
	snapshot := manifest.Snapshot()
	snapshot.Application.Localization = &schema.LocalizationSettings{
		DefaultLocale: "en", Fallback: true,
		Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}},
	}
	physical := atlasSchema(schema.NewManifest(snapshot), atlasIdentityMap{})
	table, exists := physical.Table(collectionTable("posts"))
	if !exists {
		t.Fatal("localized collection table is missing")
	}
	if _, exists := table.Column(fieldColumn("posts-title")); exists {
		t.Fatal("localized field retained a lossy non-localized column")
	}
	for _, locale := range []schema.LocaleCode{"en", "fr"} {
		column, exists := table.Column(localizedFieldColumn("posts-title", locale))
		if !exists {
			t.Fatalf("localized %s column is missing", locale)
		}
		if !column.Type.Null {
			t.Fatalf("localized %s column is NOT NULL; per-locale writes must remain possible", locale)
		}
	}
	if len(table.Indexes) != 2 {
		t.Fatalf("localized unique indexes = %d, want one per locale", len(table.Indexes))
	}
}

func TestAtlasSchemaPlansOrdinaryCompoundLocalizedAndTrashIndexes(t *testing.T) {
	title := atlasTextField("posts-title", "title")
	title.Index, title.Localized = true, true
	rankPath, _ := query.ParsePath("seo.rank")
	rank := schema.Field{ID: "posts-seo-rank", Name: "rank", Path: rankPath, Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Index: true, Localized: true, Admin: schema.FieldAdmin{Label: "Rank"}, Number: &schema.NumberField{}}
	seoPath, _ := query.ParsePath("seo")
	seo := schema.Field{ID: "posts-seo", Name: "seo", Path: seoPath, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Admin: schema.FieldAdmin{Label: "SEO"}, Nested: &schema.NestedField{Fields: []schema.Field{rank}}}
	manifest := atlasTestManifest(title, seo)
	snapshot := manifest.Snapshot()
	snapshot.Application.Localization = &schema.LocalizationSettings{DefaultLocale: "en", Fallback: true, Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}
	snapshot.Collections[0].Capabilities.Trash = true
	titlePath, _ := query.ParsePath("title")
	snapshot.Collections[0].Indexes = []schema.CollectionIndex{{Fields: []query.Path{titlePath, rankPath}, Unique: true}}
	physical := atlasSchema(schema.NewManifest(snapshot), atlasIdentityMap{})
	table, _ := physical.Table(collectionTable("posts"))
	if len(table.Indexes) != 6 {
		t.Fatalf("planned indexes = %d, want two field indexes and one compound index per locale: %#v", len(table.Indexes), table.Indexes)
	}
	ordinary, unique, expression, partial := 0, 0, 0, 0
	for _, index := range table.Indexes {
		if index.Unique {
			unique++
		} else {
			ordinary++
		}
		for _, part := range index.Parts {
			if part.X != nil {
				expression++
			}
		}
		for _, attribute := range index.Attrs {
			if predicate, ok := attribute.(*atlaspostgres.IndexPredicate); ok && predicate.P == "deleted_at IS NULL" {
				partial++
			}
		}
	}
	if ordinary != 4 || unique != 2 || expression != 4 || partial != 2 {
		t.Fatalf("index shapes ordinary=%d unique=%d expressions=%d partial=%d", ordinary, unique, expression, partial)
	}
}

func TestUniqueAndIndexDoesNotPlanRedundantIndexes(t *testing.T) {
	title := atlasTextField("posts-title", "title")
	title.Unique, title.Index = true, true
	table, _ := atlasSchema(atlasTestManifest(title), atlasIdentityMap{}).Table(collectionTable("posts"))
	if len(table.Indexes) != 1 || !table.Indexes[0].Unique {
		t.Fatalf("unique+index planned %#v, want one unique index", table.Indexes)
	}
}

func TestIndexMigrationRisksCoverBuildAndIntegrityRemoval(t *testing.T) {
	title := atlasTextField("posts-title", "title")
	summary := atlasTextField("posts-summary", "summary")
	before := atlasTestManifest(title, summary)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields[0].Index = true
	titlePath, _ := query.ParsePath("title")
	summaryPath, _ := query.ParsePath("summary")
	afterSnapshot.Collections[0].Indexes = []schema.CollectionIndex{{Fields: []query.Path{titlePath, summaryPath}, Unique: true}}
	after := schema.NewManifest(afterSnapshot)
	added, err := BuildArtifact(context.Background(), "add-indexes", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"RIDU_BUILD_INDEX", "RIDU_BUILD_UNIQUE_INDEX"} {
		if !hasRiskLevel(added.Risks, code, ridumigration.RiskWarning) {
			t.Errorf("add risks = %#v, want warning %s", added.Risks, code)
		}
	}
	_, err = BuildArtifact(context.Background(), "drop-indexes", &after, before, nil, false)
	var safety *SafetyError
	if !errors.As(err, &safety) || !hasRiskLevel(safety.Risks, "RIDU_DROP_UNIQUE_INDEX", ridumigration.RiskDestructive) {
		t.Fatalf("drop unique safety = %#v %v", safety, err)
	}
	dropped, err := BuildArtifact(context.Background(), "drop-indexes", &after, before, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRiskLevel(dropped.Risks, "RIDU_DROP_INDEX", ridumigration.RiskWarning) || !hasRiskLevel(dropped.Risks, "RIDU_DROP_UNIQUE_INDEX", ridumigration.RiskDestructive) {
		t.Fatalf("drop risks = %#v", dropped.Risks)
	}
}

func TestVersionFourIndexesUseOnlyStructuredNoTransactionExecutors(t *testing.T) {
	title := atlasTextField("posts-title", "title")
	before := atlasTestManifest(title)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields[0].Index = true
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "structured-index", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == ridumigration.StepSQL {
				var payload ridumigration.SQLPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(strings.ToUpper(payload.SQL), "CREATE INDEX") || strings.Contains(strings.ToUpper(payload.SQL), "DROP INDEX") {
					t.Fatalf("index escaped into arbitrary transactional SQL: %s", payload.SQL)
				}
			}
			if step.Kind == ridumigration.StepConcurrentIndex {
				found = true
				if phase.Mode != ridumigration.PhaseNoTransaction {
					t.Fatalf("concurrent index phase mode = %s", phase.Mode)
				}
				var payload ridumigration.ConcurrentIndexPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil || payload.Action != ridumigration.ConcurrentIndexCreate || payload.Name == "" || payload.Table == "" || len(payload.Parts) == 0 {
					t.Fatalf("structured payload = %#v, %v", payload, err)
				}
			}
		}
	}
	if !found {
		t.Fatal("ordinary index was not promoted to a structured concurrent executor")
	}
}

func TestAtlasSchemaIncludesUploadReferenceIndexes(t *testing.T) {
	manifest := atlasUploadReferenceManifest()
	physical := atlasSchema(manifest, atlasIdentityMap{})
	versions, exists := physical.Table("ridu_versions")
	if !exists {
		t.Fatal("version storage is missing")
	}
	for _, name := range []string{"ridu_versions_upload_object_key_idx", "ridu_versions_upload_size_keys_idx"} {
		if index, exists := versions.Index(name); !exists || index == nil {
			t.Errorf("version storage is missing upload reference index %q", name)
		}
	}
	media, exists := physical.Table(collectionTable("media"))
	if !exists {
		t.Fatal("upload collection storage is missing")
	}
	var objectKeys, sizeKeys bool
	for _, index := range media.Indexes {
		objectKeys = objectKeys || strings.HasPrefix(index.Name, "z_i_")
		sizeKeys = sizeKeys || strings.HasPrefix(index.Name, "z_ui_")
	}
	if !objectKeys || !sizeKeys {
		t.Fatalf("upload collection indexes = %#v", media.Indexes)
	}
}

func TestFieldRenameWithSimultaneousIndexAdditionMatchesDefinitions(t *testing.T) {
	beforeTitle := atlasTextField("posts-title", "title")
	beforeTitle.Index = true
	beforeSummary := atlasTextField("posts-summary", "summary")
	before := atlasTestManifest(beforeTitle, beforeSummary)
	afterTitle := atlasTextField("posts-headline", "headline")
	afterTitle.Index = true
	afterSummary := atlasTextField("posts-summary", "summary")
	afterSummary.Index = true
	after := atlasTestManifest(afterTitle, afterSummary)
	rename := Rename{Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: after.Snapshot().Collections[0], BeforeField: &beforeTitle, AfterField: &afterTitle}
	artifact, err := BuildArtifact(context.Background(), "rename-and-index", &before, after, []Rename{rename}, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := artifactSQL(t, artifact)
	if strings.Count(joined, "ALTER INDEX") != 1 || strings.Count(joined, "CREATE INDEX") != 1 || strings.Contains(joined, "DROP INDEX") {
		t.Fatalf("rename+index SQL mismatched definitions:\n%s", joined)
	}
}

func TestAtlasSchemaIncludesDocumentLockLeases(t *testing.T) {
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	snapshot := manifest.Snapshot()
	snapshot.Collections[0].Capabilities.Locking = true
	snapshot.Collections[0].DocumentLock = &schema.DocumentLockSettings{DurationSeconds: 120}
	physical := atlasSchema(schema.NewManifest(snapshot), atlasIdentityMap{})
	table, exists := physical.Table("ridu_document_locks")
	if !exists {
		t.Fatal("document lock storage is missing for a lock-enabled collection")
	}
	for _, column := range []string{"collection_id", "document_id", "owner_collection_id", "owner_id", "owner_label", "created_at", "updated_at", "expires_at"} {
		if _, exists := table.Column(column); !exists {
			t.Fatalf("document lock storage is missing %q", column)
		}
	}
}

func TestAtlasSchemaIncludesLifecycleCleanupIndexes(t *testing.T) {
	manifest := atlasTestManifest(atlasTextField("posts-title", "title"))
	snapshot := manifest.Snapshot()
	snapshot.Collections[0].Capabilities.Auth = true
	snapshot.Collections[0].Capabilities.Versions = true
	snapshot.Collections[0].Capabilities.Locking = true
	snapshot.Collections[0].Auth = &schema.AuthSettings{IdentityField: "email"}
	snapshot.Collections[0].Versions = &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	snapshot.Collections[0].DocumentLock = &schema.DocumentLockSettings{DurationSeconds: 120}
	physical := atlasSchema(schema.NewManifest(snapshot), atlasIdentityMap{})
	for tableName, expected := range map[string][]string{
		"ridu_auth_sessions":  {"ridu_auth_sessions_expiry_idx"},
		"ridu_auth_api_keys":  {"ridu_auth_api_keys_expiry_idx"},
		"ridu_document_locks": {"ridu_document_locks_owner_idx"},
	} {
		table, exists := physical.Table(tableName)
		if !exists {
			t.Fatalf("lifecycle table %q is missing", tableName)
		}
		indexes := make(map[string]bool, len(table.Indexes))
		for _, index := range table.Indexes {
			indexes[index.Name] = true
		}
		for _, name := range expected {
			if !indexes[name] {
				t.Errorf("%s is missing index %q", tableName, name)
			}
		}
	}
}

func TestAtlasArtifactUsesExplicitRenameInsteadOfDropAndAdd(t *testing.T) {
	beforeField := atlasTextField("posts-title", "title")
	afterField := atlasTextField("posts-headline", "headline")
	before := atlasTestManifest(beforeField)
	after := atlasTestManifest(afterField)
	rename := Rename{
		Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: after.Snapshot().Collections[0],
		BeforeField: &beforeField, AfterField: &afterField,
	}
	artifact, err := BuildArtifact(context.Background(), "rename-title", &before, after, []Rename{rename}, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := artifactSQL(t, artifact)
	if !strings.Contains(joined, "RENAME COLUMN") || strings.Contains(joined, "DROP COLUMN") || strings.Contains(joined, "ADD COLUMN") {
		t.Fatalf("rename SQL = %s", joined)
	}
	foundIntent := false
	for _, step := range artifactTestSteps(t, artifact) {
		foundIntent = foundIntent || step.Rename != nil && step.Rename.FieldBefore == "title" && step.Rename.FieldAfter == "headline"
	}
	if !foundIntent {
		t.Fatalf("rename intent missing: %#v", artifactTestSteps(t, artifact))
	}
}

func TestAtlasArtifactRenamesLocalizedUniqueColumnsAndIndexesPerLocale(t *testing.T) {
	beforeField := atlasTextField("posts-title", "title")
	beforeField.Localized, beforeField.Unique = true, true
	afterField := atlasTextField("posts-headline", "headline")
	afterField.Localized, afterField.Unique = true, true
	localization := &schema.LocalizationSettings{
		DefaultLocale: "en", Fallback: true,
		Locales: []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French", FallbackLocales: []schema.LocaleCode{"en"}}},
	}
	beforeSnapshot := atlasTestManifest(beforeField).Snapshot()
	beforeSnapshot.Application.Localization = localization
	before := schema.NewManifest(beforeSnapshot)
	afterSnapshot := atlasTestManifest(afterField).Snapshot()
	afterSnapshot.Application.Localization = localization
	after := schema.NewManifest(afterSnapshot)
	rename := Rename{
		Kind: RenameField, BeforeCollection: before.Snapshot().Collections[0], AfterCollection: after.Snapshot().Collections[0],
		BeforeField: &beforeField, AfterField: &afterField,
	}

	artifact, err := BuildArtifact(context.Background(), "rename-localized-title", &before, after, []Rename{rename}, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := artifactSQL(t, artifact)
	locales := []schema.LocaleCode{"en", "fr"}
	if count := strings.Count(joined, "RENAME COLUMN"); count != len(locales) {
		t.Fatalf("localized column renames = %d, want %d:\n%s", count, len(locales), joined)
	}
	if count := strings.Count(joined, "ALTER INDEX"); count != len(locales) {
		t.Fatalf("localized index renames = %d, want %d:\n%s", count, len(locales), joined)
	}
	for _, locale := range locales {
		for _, fragment := range []string{
			localizedFieldColumn(beforeField.ID, locale), localizedFieldColumn(afterField.ID, locale),
			"z_u_" + identifierHash("posts:"+string(beforeField.ID)+":"+string(locale)),
			"z_u_" + identifierHash("posts:"+string(afterField.ID)+":"+string(locale)),
		} {
			if !strings.Contains(joined, fragment) {
				t.Errorf("localized %s rename SQL is missing %q:\n%s", locale, fragment, joined)
			}
		}
	}
	for _, fragment := range []string{"DROP COLUMN", "ADD COLUMN", "DROP INDEX", "CREATE UNIQUE INDEX"} {
		if strings.Contains(joined, fragment) {
			t.Errorf("localized rename planned %q instead of preserving storage:\n%s", fragment, joined)
		}
	}
	if artifact.Before == nil || artifact.Before.Version != schema.CurrentVersion || artifact.After.Version != schema.CurrentVersion {
		t.Fatalf("localized rename changed manifest lineage: before %#v after %d", artifact.Before, artifact.After.Version)
	}
}

func TestAtlasArtifactFailsClosedForIncompatibleTypeChange(t *testing.T) {
	before := atlasTestManifest(atlasTextField("posts-value", "value"))
	path, _ := query.NewPath("value")
	after := atlasTestManifest(schema.Field{ID: "posts-value", Name: "value", Path: path, Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: "value"}})
	_, err := BuildArtifact(context.Background(), "change-value-type", &before, after, nil, false)
	var safety *SafetyError
	if !errors.As(err, &safety) {
		t.Fatalf("expected incompatible type change to fail closed, got %v", err)
	}
}

func TestAtlasArtifactRecordsManifestOnlyTransitions(t *testing.T) {
	before := atlasTestManifest(atlasTextField("posts-value", "value"))
	afterSnapshot := before.Snapshot()
	afterSnapshot.Application.Name = "Renamed application"
	after := schema.NewManifest(afterSnapshot)
	artifact, err := BuildArtifact(context.Background(), "rename-application", &before, after, nil, false)
	steps := artifactTestSteps(t, artifact)
	if err != nil || artifact.MinimumRunnerContract != ridumigration.RunnerContractVersion || len(steps) != 1 || steps[0].Kind != ridumigration.StepAssertSchema {
		t.Fatalf("manifest-only artifact = %#v, %v", artifact, err)
	}
	if _, err := BuildArtifact(context.Background(), "no-op", &after, after, nil, false); err == nil {
		t.Fatal("expected an unchanged manifest to reject migration creation")
	}
}

func TestPluginMigrationsAreOrderedChecksummedAndReversible(t *testing.T) {
	base := atlasTestManifest(atlasTextField("posts-value", "value"))
	withPluginSnapshot := base.Snapshot()
	withPluginSnapshot.Plugins = []schema.Plugin{{
		Key: "search", Version: "1.0.0", GoPackage: "example.com/plugins/search", APIVersion: schema.CurrentPluginAPIVersion,
		Ridu: &schema.PluginCompatibility{Minimum: "0.0.0-dev"},
		DatabaseContributions: []schema.PluginDatabaseContribution{{
			Adapter: schema.PluginDatabaseAdapterPostgres, Tables: []string{"ridu_plugin_search_index"},
			Migrations: []schema.PluginMigration{{Version: 1, Name: "create-index", UpSQL: []string{"CREATE TABLE ridu_plugin_search_index (id text PRIMARY KEY)"}, DownSQL: []string{"DROP TABLE ridu_plugin_search_index"}}},
		}},
	}}
	withPlugin := schema.NewManifest(withPluginSnapshot)
	up, err := BuildArtifact(context.Background(), "add-search", &base, withPlugin, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	pluginStep := findPluginStep(t, up)
	if pluginStep == nil || pluginStep.Adapter != schema.PluginDatabaseAdapterPostgres || pluginStep.Direction != "up" || pluginStep.Checksum != ridumigration.PluginStepChecksum(schema.PluginDatabaseAdapterPostgres, "search", 1, "up", pluginStep.SQL) {
		t.Fatalf("plugin up step = %#v", pluginStep)
	}
	if _, err := BuildArtifact(context.Background(), "remove-search", &withPlugin, base, nil, false); err == nil {
		t.Fatal("plugin removal succeeded without destructive approval")
	}
	down, err := BuildArtifact(context.Background(), "remove-search", &withPlugin, base, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	pluginStep = findPluginStep(t, down)
	if pluginStep == nil || pluginStep.Direction != "down" || !hasRiskCode(down.Risks, "RIDU_PLUGIN_MIGRATION_DOWN") {
		t.Fatalf("plugin down artifact = %#v", down)
	}

	changedSnapshot := withPlugin.Snapshot()
	changedSnapshot.Plugins[0].DatabaseContributions[0].Migrations[0].UpSQL[0] += " CASCADE"
	changed := schema.NewManifest(changedSnapshot)
	if _, err := BuildArtifact(context.Background(), "mutate-search", &withPlugin, changed, nil, false); err == nil || !strings.Contains(err.Error(), "changed after publication") {
		t.Fatalf("changed plugin migration error = %v", err)
	}
}

func TestPostgresRejectsPluginPrivateSchemaWithoutPostgresContribution(t *testing.T) {
	manifest := atlasTestManifest(atlasTextField("posts-value", "value"))
	snapshot := manifest.Snapshot()
	snapshot.Plugins = []schema.Plugin{{
		Key: "search", Version: "1.0.0", GoPackage: "example.com/plugins/search", APIVersion: schema.CurrentPluginAPIVersion,
		Ridu: &schema.PluginCompatibility{Minimum: "0.0.0-dev"},
		DatabaseContributions: []schema.PluginDatabaseContribution{{
			Adapter:    schema.PluginDatabaseAdapterSQLite,
			Migrations: []schema.PluginMigration{{Version: 1, Name: "create-index", UpSQL: []string{"SELECT 1"}, DownSQL: []string{"SELECT 1"}}},
		}},
	}}
	if _, err := BuildArtifact(context.Background(), "sqlite-only-plugin", nil, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "does not support postgres") {
		t.Fatalf("PostgreSQL unsupported plugin error = %v", err)
	}
}

func findPluginStep(t *testing.T, artifact ridumigration.Artifact) *ridumigration.PluginStep {
	t.Helper()
	for _, step := range artifactTestSteps(t, artifact) {
		if step.Plugin != nil {
			return step.Plugin
		}
	}
	return nil
}

func atlasTestManifest(fields ...schema.Field) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Atlas test"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"}, Fields: fields,
		}},
	})
}

func atlasUploadReferenceManifest() schema.Manifest {
	objectKeyPath, _ := query.NewPath("objectKey")
	sizesPath, _ := query.NewPath("sizes")
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Upload reference indexes"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{
			ID: "media", Slug: "media", Labels: schema.CollectionLabels{Singular: "Medium", Plural: "Media"},
			Capabilities: schema.Capabilities{Upload: true, Versions: true}, Upload: &schema.UploadSettings{},
			Versions: &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
			Fields: []schema.Field{
				{ID: "upload-object-key", Name: "objectKey", Path: objectKeyPath, Type: schema.FieldTypeText, Category: schema.FieldCategoryUpload, Required: true, Index: true, Admin: schema.FieldAdmin{Label: "Object key"}, Text: &schema.TextField{}},
				{ID: "upload-sizes", Name: "sizes", Path: sizesPath, Type: schema.FieldTypeJSON, Category: schema.FieldCategoryUpload, Admin: schema.FieldAdmin{Label: "Sizes"}},
			},
		}},
	})
}

func atlasTextField(id schema.StableID, name string) schema.Field {
	path, _ := query.NewPath(name)
	return schema.Field{ID: id, Name: name, Path: path, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Admin: schema.FieldAdmin{Label: name}, Text: &schema.TextField{}}
}

func artifactSQL(t *testing.T, artifact ridumigration.Artifact) string {
	t.Helper()
	var statements []string
	for _, step := range artifactTestSteps(t, artifact) {
		if step.Kind == ridumigration.StepSQL {
			statements = append(statements, step.SQL)
		}
	}
	return strings.Join(statements, "; ")
}

func artifactContainsSQL(t *testing.T, artifact ridumigration.Artifact, fragment string) bool {
	t.Helper()
	return strings.Contains(strings.ToUpper(artifactSQL(t, artifact)), strings.ToUpper(fragment))
}

func hasRiskCode(risks []ridumigration.Risk, code string) bool {
	for _, risk := range risks {
		if risk.Code == code {
			return true
		}
	}
	return false
}

func hasRiskLevel(risks []ridumigration.Risk, code string, level ridumigration.RiskLevel) bool {
	for _, risk := range risks {
		if risk.Code == code && risk.Level == level {
			return true
		}
	}
	return false
}

func hasStepKind(t *testing.T, artifact ridumigration.Artifact, kind ridumigration.StepKind) bool {
	t.Helper()
	for _, step := range artifactTestSteps(t, artifact) {
		if step.Kind == kind {
			return true
		}
	}
	return false
}

func artifactTestSteps(t *testing.T, artifact ridumigration.Artifact) []ridumigration.Operation {
	t.Helper()
	var steps []ridumigration.Operation
	for _, phase := range artifact.Phases {
		for _, planned := range phase.Steps {
			step := ridumigration.Operation{Kind: planned.Kind, Name: planned.Name}
			switch planned.Kind {
			case ridumigration.StepSQL:
				var payload ridumigration.SQLPayload
				if err := json.Unmarshal(planned.Payload, &payload); err != nil {
					t.Fatalf("decode %s payload: %v", planned.Kind, err)
				}
				step.SQL = payload.SQL
			case ridumigration.StepRenameContent:
				var payload ridumigration.RenamePayload
				if err := json.Unmarshal(planned.Payload, &payload); err != nil {
					t.Fatalf("decode %s payload: %v", planned.Kind, err)
				}
				step.Rename = &payload.Rename
			case ridumigration.StepPluginSQL:
				var payload ridumigration.PluginPayload
				if err := json.Unmarshal(planned.Payload, &payload); err != nil {
					t.Fatalf("decode %s payload: %v", planned.Kind, err)
				}
				step.Plugin = &payload.Plugin
			case ridumigration.StepConcurrentIndex:
				var payload ridumigration.ConcurrentIndexPayload
				if err := json.Unmarshal(planned.Payload, &payload); err != nil {
					t.Fatalf("decode %s payload: %v", planned.Kind, err)
				}
				step.Kind = ridumigration.StepSQL
				if payload.Action == ridumigration.ConcurrentIndexDrop {
					step.SQL = "DROP INDEX CONCURRENTLY " + quote(payload.Name)
				} else {
					step.SQL = "CREATE "
					if payload.Unique {
						step.SQL += "UNIQUE "
					}
					step.SQL += "INDEX CONCURRENTLY " + quote(payload.Name) + " ON " + quote(payload.Table)
					if payload.Method != "" {
						step.SQL += " USING " + payload.Method
					}
					step.SQL += " (" + strings.Join(payload.Parts, ", ") + ")"
					if payload.Predicate != "" {
						step.SQL += " WHERE " + payload.Predicate
					}
				}
			}
			steps = append(steps, step)
		}
	}
	return steps
}
