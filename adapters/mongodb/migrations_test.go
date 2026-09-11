package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoDBArtifactPlansInitialIndexesDeterministically(t *testing.T) {
	manifest := mongoDBMigrationTestManifest(t, true, false)
	first, err := buildMongoDBArtifact(context.Background(), "initial", nil, manifest, "", currentMongoDBPlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildMongoDBArtifact(context.Background(), "initial", nil, manifest, "", currentMongoDBPlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical MongoDB inputs produced different artifacts")
	}
	if first.Version != 1 || first.MinimumRunnerContract != 1 || first.Planner.Name != mongoDBPlannerName || first.Planner.Version != mongoDBPlannerVersionV2 {
		t.Fatalf("artifact contract = version %d runner %d planner %#v", first.Version, first.MinimumRunnerContract, first.Planner)
	}

	plans, err := mongoPhysicalIndexPlans(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := mongoDBFlattenedIndexPlans(plans)
	if len(first.Phases) != len(want)+1 {
		t.Fatalf("artifact phases = %d, want %d creates plus assertion", len(first.Phases), len(want))
	}
	for index, planned := range want {
		phase := first.Phases[index]
		if phase.ID != fmt.Sprintf("phase-%03d", index+1) || phase.Mode != ridumigration.PhaseNoTransaction || phase.PhysicalContractVersion != 1 || len(phase.Steps) != 1 {
			t.Fatalf("create phase %d = %#v", index, phase)
		}
		step := phase.Steps[0]
		if step.ID != fmt.Sprintf("step-%04d", index+1) || step.Kind != ridumigration.StepMongoDBCreateIndex || step.ExecutorVersion != 1 {
			t.Fatalf("create step %d = %#v", index, step)
		}
		var closed map[string]json.RawMessage
		if err := json.Unmarshal(step.Payload, &closed); err != nil {
			t.Fatal(err)
		}
		if len(closed) != 2 || closed["collection"] == nil || closed["index"] == nil {
			t.Fatalf("create payload keys = %#v, want collection/index only", closed)
		}
		var payload ridumigration.MongoDBCreateIndexPayload
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Collection != planned.collection || payload.Index != planned.name || payload.Index == "_id_" {
			t.Fatalf("create payload = %#v, want %s/%s", payload, planned.collection, planned.name)
		}
		if fingerprint, err := mongoIndexFingerprint([]mongoIndexDefinition{planned.definition}); err != nil || fingerprint == "" {
			t.Fatalf("private definition for %s/%s is not reconstructable: %q, %v", planned.collection, planned.name, fingerprint, err)
		}
	}
	assertion := first.Phases[len(first.Phases)-1]
	if assertion.Mode != ridumigration.PhaseNoTransaction || len(assertion.Steps) != 1 || assertion.Steps[0].Kind != ridumigration.StepMongoDBAssertSchema || string(assertion.Steps[0].Payload) != "{}" {
		t.Fatalf("final MongoDB assertion = %#v", assertion)
	}
	if len(first.Risks) == 0 || !mongoDBMigrationHasRisk(first, "RIDU_MONGODB_INDEX_BUILD_NON_TRANSACTIONAL") {
		t.Fatalf("initial risks = %#v", first.Risks)
	}
}

func mongoDBMigrationHasRisk(artifact ridumigration.Artifact, code string) bool {
	for _, risk := range artifact.Risks {
		if risk.Code == code {
			return true
		}
	}
	return false
}

func TestMongoDBArtifactPlansAdditiveAndManifestOnlyTransitions(t *testing.T) {
	before := mongoDBMigrationTestManifest(t, false, false)
	after := mongoDBMigrationTestManifest(t, true, false)
	additive, err := buildMongoDBArtifact(context.Background(), "add-title-index", &before, after, mongoDBPlannerVersion, currentMongoDBPlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	if len(additive.Phases) != 2 || additive.Phases[0].Steps[0].Kind != ridumigration.StepMongoDBCreateIndex || additive.Phases[1].Steps[0].Kind != ridumigration.StepMongoDBAssertSchema {
		t.Fatalf("additive phases = %#v", additive.Phases)
	}
	if len(additive.Risks) != 1 || additive.Risks[0].Code != "RIDU_MONGODB_INDEX_BUILD_NON_TRANSACTIONAL" {
		t.Fatalf("additive risks = %#v", additive.Risks)
	}

	unique := mongoDBMigrationTestManifest(t, false, true)
	uniqueArtifact, err := buildMongoDBArtifact(context.Background(), "add-title-unique", &before, unique, mongoDBPlannerVersion, currentMongoDBPlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	if len(uniqueArtifact.Risks) != 2 || !mongoDBMigrationHasRisk(uniqueArtifact, "RIDU_MONGODB_INDEX_BUILD_NON_TRANSACTIONAL") || !mongoDBMigrationHasRisk(uniqueArtifact, "RIDU_BUILD_UNIQUE_INDEX") {
		t.Fatalf("unique-index risks = %#v", uniqueArtifact.Risks)
	}

	manifestOnlySnapshot := before.Snapshot()
	manifestOnlySnapshot.Application.Name = "Renamed application"
	manifestOnlySnapshot.Application.AllowIDOnCreate = true
	manifestOnlySnapshot.Application.Endpoints = []schema.Endpoint{{Method: "GET", Path: "/health", Summary: "Health"}}
	manifestOnlySnapshot.Collections[0].Fields = append(manifestOnlySnapshot.Collections[0].Fields, mongoDBMigrationTextField(t, "posts-summary", "summary", false, false, false))
	manifestOnly := schema.NewManifest(manifestOnlySnapshot)
	metadataArtifact, err := buildMongoDBArtifact(context.Background(), "metadata", &before, manifestOnly, mongoDBPlannerVersion, currentMongoDBPlannerContract())
	if err != nil {
		t.Fatal(err)
	}
	if len(metadataArtifact.Phases) != 1 || metadataArtifact.Phases[0].Steps[0].Kind != ridumigration.StepMongoDBAssertSchema || len(metadataArtifact.Risks) != 0 {
		t.Fatalf("manifest-only artifact = %#v", metadataArtifact)
	}
	if _, err := buildMongoDBArtifact(context.Background(), "no-op", &manifestOnly, manifestOnly, mongoDBPlannerVersion, currentMongoDBPlannerContract()); err == nil || !strings.Contains(err.Error(), "schema is current") {
		t.Fatalf("unchanged manifest error = %v", err)
	}
}

func TestMongoDBArtifactRejectsNonAdditiveTransitions(t *testing.T) {
	before := mongoDBMigrationTestManifest(t, true, false)
	tests := map[string]func(schema.Snapshot) schema.Snapshot{
		"resource removal": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections = nil
			return snapshot
		},
		"resource rename": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections[0].Slug = "articles"
			return snapshot
		},
		"capability change": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections[0].Capabilities.Trash = true
			return snapshot
		},
		"field removal": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections[0].Fields = nil
			return snapshot
		},
		"field rename": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections[0].Fields[0].Name = "heading"
			snapshot.Collections[0].Fields[0].Path = mongoDBMigrationPath(t, "heading")
			return snapshot
		},
		"field type change": func(snapshot schema.Snapshot) schema.Snapshot {
			field := &snapshot.Collections[0].Fields[0]
			field.Type, field.Text, field.Number = schema.FieldTypeNumber, nil, &schema.NumberField{}
			return snapshot
		},
		"index removal": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections[0].Fields[0].Index = false
			return snapshot
		},
		"required field addition": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Collections[0].Fields = append(snapshot.Collections[0].Fields, mongoDBMigrationTextField(t, "posts-required", "required", true, false, false))
			return snapshot
		},
		"content localization change": func(snapshot schema.Snapshot) schema.Snapshot {
			snapshot.Application.Localization = &schema.LocalizationSettings{
				Locales: []schema.Locale{{Code: "en", Label: "English"}}, DefaultLocale: "en",
			}
			return snapshot
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			after := schema.NewManifest(mutate(before.Snapshot()))
			if _, err := buildMongoDBArtifact(context.Background(), "unsupported", &before, after, mongoDBPlannerVersion, currentMongoDBPlannerContract()); err == nil {
				t.Fatal("unsupported MongoDB transition was planned")
			}
		})
	}
}

func TestMongoDBArtifactRejectsInvalidPlannerLineage(t *testing.T) {
	manifest := mongoDBMigrationTestManifest(t, false, false)
	if _, err := buildMongoDBArtifact(context.Background(), "bad-initial", nil, manifest, mongoDBPlannerVersion, currentMongoDBPlannerContract()); err == nil {
		t.Fatal("initial artifact accepted previous planner lineage")
	}
	if _, err := buildMongoDBArtifact(context.Background(), "missing-lineage", &manifest, mongoDBMigrationRenamedApplication(manifest), "", currentMongoDBPlannerContract()); err == nil {
		t.Fatal("non-initial artifact accepted missing planner lineage")
	}
	if _, err := buildMongoDBArtifact(context.Background(), "changed-lineage", &manifest, mongoDBMigrationRenamedApplication(manifest), "0.9.0", currentMongoDBPlannerContract()); err == nil {
		t.Fatal("non-initial artifact accepted unsupported predecessor planner")
	}
}

func TestMongoDBArtifactUnionRejectsHistoricalPhysicalCollisionsDeterministically(t *testing.T) {
	definition := mongoIndexDefinition{name: "z_i_shared", identity: "field:shared", keys: bson.D{{Key: "values.shared", Value: int32(1)}}}
	before := mongoPhysicalIndexPlanSet{collections: []mongoCollectionIndexPlan{{
		collection: schema.Collection{ID: "alpha"}, physicalName: "z_c_collision", definitions: []mongoIndexDefinition{definition},
	}}}
	after := mongoPhysicalIndexPlanSet{collections: []mongoCollectionIndexPlan{{
		collection: schema.Collection{ID: "beta"}, physicalName: "z_c_collision", definitions: []mongoIndexDefinition{definition},
	}}}
	first := validateMongoDBMigrationPlanUnion(before, after)
	second := validateMongoDBMigrationPlanUnion(after, before)
	if first == nil || second == nil || first.Error() != second.Error() || !strings.Contains(first.Error(), "physical collection name collision") {
		t.Fatalf("union collision errors = %v / %v", first, second)
	}

	invalidDefinition := mongoIndexDefinition{name: "z_i_bad", keys: bson.D{{Key: "bad", Value: make(chan int)}}}
	invalid := mongoPhysicalIndexPlanSet{collections: []mongoCollectionIndexPlan{{
		collection: schema.Collection{ID: "invalid"}, physicalName: "z_c_invalid", definitions: []mongoIndexDefinition{invalidDefinition},
	}}}
	if _, _, err := mongoDBAddedIndexPlans(invalid, invalid); err == nil || !strings.Contains(err.Error(), "fingerprint previous MongoDB index") {
		t.Fatalf("unencodable definition error = %v", err)
	}
}

func TestMongoDBArtifactExactReplanRejectsSelfConsistentTampering(t *testing.T) {
	manifest := mongoDBMigrationTestManifest(t, true, false)
	artifact, err := buildMongoDBArtifact(context.Background(), "initial", nil, manifest, "", currentMongoDBPlannerContract())
	if err != nil {
		t.Fatal(err)
	}

	stepTampered := mongoDBMigrationCloneArtifact(t, artifact)
	stepTampered.Phases[0].Steps[0].Name = "create an unreviewed physical index"
	mongoDBMigrationRecomputePhysicalDigests(t, &stepTampered)
	if err := stepTampered.Validate(); err != nil {
		t.Fatalf("self-consistent step tamper is not a valid adversarial fixture: %v", err)
	}
	if err := validateMongoDBArtifactPlan(context.Background(), stepTampered, "tampered-step", ""); err == nil || !strings.Contains(err.Error(), "does not match planner") {
		t.Fatalf("step tamper exact-replan error = %v", err)
	}

	riskTampered := mongoDBMigrationCloneArtifact(t, artifact)
	riskTampered.Risks = append(riskTampered.Risks, ridumigration.Risk{
		Code: "RIDU_UNREVIEWED", Level: ridumigration.RiskNotice, Message: "self-consistent but planner-unowned risk",
	})
	if err := riskTampered.Validate(); err != nil {
		t.Fatalf("self-consistent risk tamper is not a valid adversarial fixture: %v", err)
	}
	if err := validateMongoDBArtifactPlan(context.Background(), riskTampered, "tampered-risk", ""); err == nil || !strings.Contains(err.Error(), "does not match planner") {
		t.Fatalf("risk tamper exact-replan error = %v", err)
	}

	wrongPlanner := mongoDBMigrationCloneArtifact(t, artifact)
	wrongPlanner.Planner.Name = "atlas"
	if err := validateMongoDBArtifactPlan(context.Background(), wrongPlanner, "wrong-planner", ""); err == nil || !strings.Contains(err.Error(), `uses planner "atlas"`) {
		t.Fatalf("wrong planner error = %v", err)
	}
	unsupported := mongoDBMigrationCloneArtifact(t, artifact)
	unsupported.Planner.Version = "9.0.0"
	if err := validateMongoDBArtifactPlan(context.Background(), unsupported, "unsupported", ""); err == nil || !strings.Contains(err.Error(), "unsupported planner version") {
		t.Fatalf("unsupported planner error = %v", err)
	}
}

func TestMongoDBCreateArtifactIsOfflineAtomicAndLineageBound(t *testing.T) {
	directory := t.TempDir()
	secret := "mongodb://offline-secret:do-not-leak@127.0.0.1:1/ridu"
	t.Setenv("RIDU_MONGODB_URL", secret)
	t.Setenv("DATABASE_URL", secret)
	initialManifest := mongoDBMigrationTestManifest(t, false, false)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	initial, err := CreateArtifact(context.Background(), directory, "initial", initialManifest, now)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Name == "" || initial.Path == "" || initial.Checksum == "" || initial.Version != 1 {
		t.Fatalf("created initial artifact = %#v", initial)
	}
	encoded, err := os.ReadFile(initial.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "offline-secret") || strings.Contains(string(encoded), secret) {
		t.Fatal("MongoDB migration artifact contains an environment credential")
	}

	after := mongoDBMigrationRenamedApplication(initialManifest)
	second, err := CreateArtifact(context.Background(), directory, "metadata", after, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("published MongoDB history = %d files, %v", len(files), err)
	}
	if files[0].Digest != initial.Checksum || files[1].Digest != second.Checksum || files[1].Artifact.PreviousArtifactDigest != files[0].Digest {
		t.Fatalf("published MongoDB lineage = %#v", files)
	}
	if _, err := CreateArtifact(context.Background(), directory, "no-op", after, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "schema is current") || strings.Contains(err.Error(), "offline-secret") {
		t.Fatalf("no-op creation error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CreateArtifact(cancelled, directory, "cancelled", mongoDBMigrationRenamedApplication(after), now.Add(3*time.Second)); err != context.Canceled {
		t.Fatalf("cancelled creation error = %v", err)
	}
	files, err = migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("failed creation changed history = %d files, %v", len(files), err)
	}
}

func TestMongoDBCreateArtifactRejectsEditedHistoryBeforePublishing(t *testing.T) {
	directory := t.TempDir()
	secret := "mongodb://history-secret:do-not-leak@127.0.0.1:1/ridu"
	t.Setenv("RIDU_MONGODB_URL", secret)
	manifest := mongoDBMigrationTestManifest(t, false, false)
	created, err := CreateArtifact(context.Background(), directory, "initial", manifest, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("initial history = %#v, %v", files, err)
	}
	tampered := files[0].Artifact
	tampered.Risks = append(tampered.Risks, ridumigration.Risk{Code: "RIDU_TAMPER", Level: ridumigration.RiskNotice, Message: "edited after publication"})
	encoded, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created.Path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	after := mongoDBMigrationRenamedApplication(manifest)
	_, err = CreateArtifact(context.Background(), directory, "metadata", after, time.Unix(2, 0))
	if err == nil || !strings.Contains(err.Error(), "does not match planner") || strings.Contains(err.Error(), "history-secret") {
		t.Fatalf("edited history error = %v", err)
	}
	files, readErr := migrationartifact.ReadAll(directory)
	if readErr != nil || len(files) != 1 {
		t.Fatalf("edited history failure published another artifact: %d files, %v", len(files), readErr)
	}
}

func TestMongoDBArtifactPublicationRejectsAHeadInsertedAfterExactValidation(t *testing.T) {
	directory := t.TempDir()
	manifest := mongoDBMigrationTestManifest(t, false, false)
	if _, err := CreateArtifact(context.Background(), directory, "initial", manifest, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	validatedHistory, err := migrationartifact.ReadAll(directory)
	if err != nil || len(validatedHistory) != 1 {
		t.Fatalf("validated history = %d files, %v", len(validatedHistory), err)
	}
	after := mongoDBMigrationRenamedApplication(manifest)
	planned, err := buildMongoDBArtifact(
		context.Background(), "metadata", &manifest, after, mongoDBPlannerVersion, currentMongoDBPlannerContract(),
	)
	if err != nil {
		t.Fatal(err)
	}

	intervening, err := ridumigration.NewArtifact(
		"intervening", ridumigration.Planner{Name: mongoDBPlannerName, Version: mongoDBPlannerVersion}, &manifest, manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	intervening.Phases, err = mongoDBArtifactPhases(intervening.FromDigest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "intervening", intervening, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}

	if _, err := publishMongoDBArtifact(directory, "metadata", planned, validatedHistory, time.Unix(3, 0)); err == nil || !strings.Contains(err.Error(), "does not continue the latest committed artifact") {
		t.Fatalf("stale exact-history publication error = %v", err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("stale exact-history publication changed history = %d files, %v", len(files), err)
	}
}

func TestMongoDBCreateArtifactConcurrentPublicationHasOneWinner(t *testing.T) {
	directory := t.TempDir()
	firstManifest := mongoDBMigrationTestManifest(t, false, false)
	secondManifest := mongoDBMigrationRenamedApplication(firstManifest)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	inputs := []struct {
		name     string
		manifest schema.Manifest
		now      time.Time
	}{
		{name: "initial-posts", manifest: firstManifest, now: now},
		{name: "initial-renamed", manifest: secondManifest, now: now.Add(time.Second)},
	}
	start := make(chan struct{})
	errors := make(chan error, len(inputs))
	var workers sync.WaitGroup
	for _, input := range inputs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := CreateArtifact(context.Background(), directory, input.name, input.manifest, input.now)
			errors <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	successes, failures := 0, 0
	for err := range errors {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent publication = %d successes/%d failures, want 1/1", successes, failures)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("concurrent publication history = %d files, %v", len(files), err)
	}
	temporary, err := filepath.Glob(filepath.Join(directory, ".ridu-artifact-*.tmp"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary MongoDB artifacts = %#v, %v", temporary, err)
	}
}

func mongoDBMigrationTestManifest(t *testing.T, indexed, unique bool) schema.Manifest {
	t.Helper()
	return schema.NewManifest(schema.Snapshot{
		Version:     schema.CurrentVersion,
		Application: schema.Application{Name: "MongoDB migrations"},
		Collections: []schema.Collection{{
			ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
			Fields: []schema.Field{mongoDBMigrationTextField(t, "posts-title", "title", false, indexed, unique)},
		}},
		Plugins: []schema.Plugin{},
	})
}

func mongoDBMigrationTextField(t *testing.T, id schema.StableID, name string, required, indexed, unique bool) schema.Field {
	t.Helper()
	return schema.Field{
		ID: id, Name: name, Path: mongoDBMigrationPath(t, name), Type: schema.FieldTypeText,
		Category: schema.FieldCategoryScalar, Required: required, Index: indexed, Unique: unique, Text: &schema.TextField{},
	}
}

func mongoDBMigrationPath(t *testing.T, segments ...string) query.Path {
	t.Helper()
	path, err := query.NewPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func mongoDBMigrationRenamedApplication(manifest schema.Manifest) schema.Manifest {
	snapshot := manifest.Snapshot()
	snapshot.Application.Name += " renamed"
	return schema.NewManifest(snapshot)
}

func mongoDBMigrationCloneArtifact(t *testing.T, artifact ridumigration.Artifact) ridumigration.Artifact {
	t.Helper()
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := ridumigration.DecodeArtifact(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return clone
}

func mongoDBMigrationRecomputePhysicalDigests(t *testing.T, artifact *ridumigration.Artifact) {
	t.Helper()
	physical := ridumigration.PhysicalDigestSeed(artifact.FromDigest)
	for index := range artifact.Phases {
		artifact.Phases[index].BeforePhysicalDigest = physical
		after, err := ridumigration.PhasePhysicalDigest(physical, artifact.Phases[index].Mode, artifact.Phases[index].Steps)
		if err != nil {
			t.Fatal(err)
		}
		artifact.Phases[index].AfterPhysicalDigest = after
		physical = after
	}
}
