package mongodb

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoVersionsConfig(versions, drafts bool) ridu.Config {
	return ridu.Config{
		Name: "MongoDB enable versions",
		Collections: []ridu.Collection{{
			Slug: "posts", Trash: true, Versions: versions, VersionConfig: ridu.VersionConfig{Drafts: drafts},
			Fields: field.Fields{field.Text("title").Index().Unique()},
		}},
	}
}

func mongoVersionsManifest(t *testing.T, versions, drafts bool) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(mongoVersionsConfig(versions, drafts))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func mongoVersionsChoice(resource schema.StableID, existing ridumigration.ExistingDocuments) ArtifactOptions {
	return ArtifactOptions{ExistingDocuments: map[schema.StableID]ridumigration.ExistingDocuments{resource: existing}}
}

func mongoEnableVersionsSteps(t *testing.T, artifact ridumigration.Artifact) []ridumigration.EnableVersionsPayload {
	t.Helper()
	var payloads []ridumigration.EnableVersionsPayload
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepEnableVersions {
				continue
			}
			var payload ridumigration.EnableVersionsPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payloads = append(payloads, payload)
		}
	}
	return payloads
}

// The planner records the choice as the first step of the semantic
// transaction, reserves the converted documents' unique values before the
// reservation index is built, creates the version and published namespaces,
// and replans the artifact byte for byte.
func TestMongoDBArtifactEnablesVersionsWithRecordedChoice(t *testing.T) {
	ctx := t.Context()
	before, after := mongoVersionsManifest(t, false, false), mongoVersionsManifest(t, true, true)
	posts := mongoCollectionsBySlug(after.Snapshot().Collections)["posts"]
	if _, err := buildMongoDBArtifact(ctx, "enable-versions", &before, after, ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_REQUIRED") {
		t.Fatalf("migration without a choice = %v", err)
	}
	for _, existing := range []ridumigration.ExistingDocuments{ridumigration.ExistingPublished, ridumigration.ExistingDraft, ridumigration.ExistingRequireEmpty} {
		t.Run(string(existing), func(t *testing.T) {
			artifact, err := buildMongoDBArtifact(ctx, "enable-versions", &before, after, mongoVersionsChoice(posts.ID, existing))
			if err != nil {
				t.Fatal(err)
			}
			if steps := mongoEnableVersionsSteps(t, artifact); len(steps) != 1 || steps[0].ResourceID != posts.ID || steps[0].Existing != existing {
				t.Fatalf("recorded enable-versions steps = %#v", steps)
			}
			enabled, rebuilt, firstIndex := -1, -1, -1
			createdNamespaces := make(map[string]bool)
			for index, phase := range artifact.Phases {
				for _, step := range phase.Steps {
					switch step.Kind {
					case ridumigration.StepEnableVersions:
						if phase.Mode != ridumigration.PhaseTransaction || phase.Steps[0].ID != step.ID {
							t.Fatalf("enable-versions step is not first in a transaction phase: %#v", phase)
						}
						enabled = index
					case ridumigration.StepMongoDBRebuildHeadReservations:
						rebuilt = index
					case ridumigration.StepMongoDBCreateIndex:
						if firstIndex < 0 {
							firstIndex = index
						}
						var payload ridumigration.MongoDBCreateIndexPayload
						if err := json.Unmarshal(step.Payload, &payload); err != nil {
							t.Fatal(err)
						}
						createdNamespaces[payload.Collection] = true
					}
				}
			}
			if enabled < 0 || rebuilt <= enabled || firstIndex <= rebuilt {
				t.Fatalf("phase order: enable %d, reservation rebuild %d, first index %d", enabled, rebuilt, firstIndex)
			}
			for _, namespace := range []string{physicalCollectionName(posts.ID), physicalVersionCollectionName(posts.ID), physicalPublishedCollectionName(posts.ID)} {
				if !createdNamespaces[namespace] {
					t.Fatalf("migration creates no index in %s: %#v", namespace, createdNamespaces)
				}
			}
			found := false
			for _, risk := range artifact.Risks {
				found = found || risk.Code == "RIDU_VERSIONS_ENABLE"
			}
			if !found {
				t.Fatalf("risks = %#v", artifact.Risks)
			}
			artifact.PreviousArtifactDigest = strings.Repeat("a", 64)
			if err := validateMongoDBArtifactPlan(ctx, artifact, "enable-versions"); err != nil {
				t.Fatalf("replanning the recorded choice: %v", err)
			}
		})
	}
}

func TestMongoDBArtifactRefusesUnusableVersionsChoices(t *testing.T) {
	ctx := t.Context()
	before := mongoVersionsManifest(t, false, false)
	posts := mongoCollectionsBySlug(before.Snapshot().Collections)["posts"]
	withoutDrafts := mongoVersionsManifest(t, true, false)
	if _, err := buildMongoDBArtifact(ctx, "enable-versions", &before, withoutDrafts, mongoVersionsChoice(posts.ID, ridumigration.ExistingDraft)); err == nil || !strings.Contains(err.Error(), "does not enable drafts") {
		t.Fatalf("draft choice without drafts = %v", err)
	}
	if _, err := buildMongoDBArtifact(ctx, "initial", nil, withoutDrafts, mongoVersionsChoice(posts.ID, ridumigration.ExistingPublished)); err == nil || !strings.Contains(err.Error(), "no stored documents") {
		t.Fatalf("choice in an initial migration = %v", err)
	}
	if _, err := buildMongoDBArtifact(ctx, "initial", nil, withoutDrafts, ArtifactOptions{}); err != nil {
		t.Fatalf("initial versioned resource: %v", err)
	}
	// Only starting to keep versions is planned; stopping or changing how a
	// versioned resource keeps them still is not.
	withDrafts := mongoVersionsManifest(t, true, true)
	for name, transition := range map[string][2]schema.Manifest{
		"disable versions": {withoutDrafts, before},
		"enable drafts":    {withoutDrafts, withDrafts},
	} {
		if _, err := buildMongoDBArtifact(ctx, name, &transition[0], transition[1], ArtifactOptions{AllowDestructive: true}); err == nil || !strings.Contains(err.Error(), "changed outside its fields or indexes") {
			t.Fatalf("%s = %v", name, err)
		}
	}

	added, err := ridu.Resolve(ridu.Config{Name: "MongoDB enable versions", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Index().Unique(), field.Text("summary")},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildMongoDBArtifact(ctx, "add-summary", &before, added, mongoVersionsChoice(posts.ID, ridumigration.ExistingPublished)); err == nil || !strings.Contains(err.Error(), "does not start keeping versions") {
		t.Fatalf("choice for a resource that keeps no versions = %v", err)
	}

	renamed, err := ridu.Resolve(ridu.Config{Name: "MongoDB enable versions", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Versions: true,
		Fields: field.Fields{field.Text("heading").Index().Unique()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	options := mongoVersionsChoice(posts.ID, ridumigration.ExistingPublished)
	options.Renames = []ridumigration.Rename{{CollectionBefore: "posts", CollectionAfter: "posts", FieldBefore: "title", FieldAfter: "heading"}}
	if _, err := buildMongoDBArtifact(ctx, "rename-and-enable", &before, renamed, options); err == nil || !strings.Contains(err.Error(), "of its own") {
		t.Fatalf("enable versions with a rename = %v", err)
	}
	options = mongoVersionsChoice(posts.ID, ridumigration.ExistingPublished)
	options.AllowDestructive = true
	options.DataTransforms = []ridumigration.DataTransformDescriptor{{Name: "backfill", Checksum: ridumigration.DataTransformChecksum([]byte("backfill"))}}
	if _, err := buildMongoDBArtifact(ctx, "transform-and-enable", &before, withoutDrafts, options); err == nil || !strings.Contains(err.Error(), "of its own") {
		t.Fatalf("enable versions with a data transform = %v", err)
	}
}

// The runner compiles the step against the after manifest, treats it as
// maintenance, and never adopts it into a development database's history.
func TestMongoDBEnableVersionsStepIsRunnerOnlyMaintenance(t *testing.T) {
	ctx := t.Context()
	directory := t.TempDir()
	before, after := mongoVersionsManifest(t, false, false), mongoVersionsManifest(t, true, true)
	posts := mongoCollectionsBySlug(after.Snapshot().Collections)["posts"]
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), mongoVersionsChoice(posts.ID, ridumigration.ExistingDraft)); err != nil {
		t.Fatal(err)
	}
	files := mustReadMongoArtifacts(t, directory)
	replay, err := prepareMongoDBArtifactReplay(ctx, files)
	if err != nil {
		t.Fatal(err)
	}
	var compiled []mongoDBArtifactReplayStep
	for _, step := range replay[1].steps {
		if step.kind == ridumigration.StepEnableVersions {
			compiled = append(compiled, step)
		}
	}
	if len(compiled) != 1 || compiled[0].enableResource.ID != posts.ID || compiled[0].enableResource.Versions == nil || compiled[0].enableExisting != ridumigration.ExistingDraft {
		t.Fatalf("compiled enable-versions steps = %#v", compiled)
	}
	if !mongoDBReplayRequiresMaintenance(replay[1:]) || !mongoDBRunnerOnlyStep(ridumigration.StepEnableVersions, ridumigration.ExistingDraft) {
		t.Fatal("enabling versions on stored documents ran without maintenance or could be adopted")
	}
	// Schema sync enables versions on a resource that stores nothing, so a
	// database it synced can adopt a step that requires exactly that.
	if mongoDBRunnerOnlyStep(ridumigration.StepEnableVersions, ridumigration.ExistingRequireEmpty) {
		t.Fatal("a require-empty step blocks adopting what schema sync already did")
	}
	if blocking := mongoDBBlockingStep(files[1]); blocking != string(ridumigration.StepEnableVersions) {
		t.Fatalf("blocking step = %q", blocking)
	}
}

// seedMongoUnversionedPosts applies an unversioned history and stores two
// posts, one of them trashed, returning their IDs.
func seedMongoUnversionedPosts(t *testing.T, backend *Store, directory string) (string, string) {
	t.Helper()
	ctx := t.Context()
	if _, err := CreateArtifact(ctx, directory, "initial", mongoVersionsManifest(t, false, false), time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(mongoVersionsConfig(false, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	live, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Trashed")}, ridu.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(ctx, "posts", trashed.ID, ridu.MutationOptions{System: true}); err != nil {
		t.Fatal(err)
	}
	return live.ID, trashed.ID
}

// Enabling versions turns every stored document, trashed ones included, into
// a versioned document as the recorded choice says, keeps its unique values
// reserved across both heads, and replays in verify.
func TestMongoDBMigrationEnablesVersionsOnStoredDocuments(t *testing.T) {
	for _, existing := range []ridumigration.ExistingDocuments{ridumigration.ExistingPublished, ridumigration.ExistingDraft} {
		t.Run(string(existing), func(t *testing.T) {
			ctx := t.Context()
			backend := mongoIntegrationStore(t)
			directory := t.TempDir()
			liveID, trashedID := seedMongoUnversionedPosts(t, backend, directory)
			before, after := mongoVersionsManifest(t, false, false), mongoVersionsManifest(t, true, true)
			posts := mongoCollectionsBySlug(after.Snapshot().Collections)["posts"]

			reports, err := backend.ReviewDevelopmentVersions(ctx, before, after)
			if err != nil || len(reports) != 1 || reports[0].Resource.ID != posts.ID || reports[0].Documents != 2 {
				t.Fatalf("review = %#v, %v", reports, err)
			}
			if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_REQUIRED") {
				t.Fatalf("migration without a choice = %v", err)
			}
			if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), mongoVersionsChoice(posts.ID, existing)); err != nil {
				t.Fatal(err)
			}
			files := mustReadMongoArtifacts(t, directory)
			if steps := mongoEnableVersionsSteps(t, files[1].Artifact); len(steps) != 1 || steps[0].ResourceID != posts.ID || steps[0].Existing != existing {
				t.Fatalf("recorded enable-versions steps = %#v", steps)
			}
			if err := VerifyArtifacts(ctx, mongoDBMigrationVerifierConfig(t), directory); err != nil {
				t.Fatalf("verify replays the migration: %v", err)
			}
			if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "maintenance") {
				t.Fatalf("enabling versions without maintenance admission = %v", err)
			}
			if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
				t.Fatal(err)
			}
			if err := backend.Ready(ctx, after); err != nil {
				t.Fatal(err)
			}

			application, err := ridu.New(mongoVersionsConfig(true, true), backend)
			if err != nil {
				t.Fatal(err)
			}
			published, draft := false, true
			public, publicErr := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &published})
			working, err := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &draft, System: true})
			if err != nil {
				t.Fatal(err)
			}
			if title, _ := working.Values["title"].StringValue(); title != "Live" || working.Revision != 1 {
				t.Fatalf("working document = %#v", working)
			}
			versions, err := application.Local().Versions(ctx, "posts", liveID, ridu.FindOptions{System: true})
			if err != nil || len(versions) != 1 || versions[0].Revision != working.Revision || versions[0].Status != working.Status || !versions[0].CreatedAt.Equal(working.UpdatedAt) {
				t.Fatalf("versions = %#v, %v; working = %#v", versions, err, working)
			}
			if title, _ := versions[0].Snapshot.Values["title"].StringValue(); title != "Live" {
				t.Fatalf("version snapshot = %#v", versions[0].Snapshot)
			}
			switch existing {
			case ridumigration.ExistingPublished:
				if publicErr != nil {
					t.Fatalf("published read = %v", publicErr)
				}
				if title, _ := public.Values["title"].StringValue(); title != "Live" || working.Status != store.StatusPublished ||
					working.PublishedRevision != working.Revision || working.HasDraftChanges {
					t.Fatalf("published document = %#v, working = %#v", public, working)
				}
			case ridumigration.ExistingDraft:
				if !errors.Is(publicErr, store.ErrNotFound) {
					t.Fatalf("published read of a draft = %#v, %v", public, publicErr)
				}
				if working.Status != store.StatusDraft || working.PublishedRevision != 0 {
					t.Fatalf("draft document = %#v", working)
				}
			}
			trashed, err := application.Local().Find(ctx, "posts", trashedID, ridu.FindOptions{Draft: &draft, System: true, TrashOnly: true})
			if err != nil || trashed.DeletedAt == nil || trashed.Status != working.Status {
				t.Fatalf("trashed document = %#v, %v", trashed, err)
			}
			if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true}); err == nil {
				t.Fatal("the converted documents no longer hold their unique values")
			}
			if existing == ridumigration.ExistingPublished {
				// Once the working draft moves away, only the live head holds
				// the old value, through the reservation the migration rebuilt.
				if _, err := application.Local().Update(ctx, "posts", liveID, store.Values{"title": store.String("Working")}, ridu.MutationOptions{System: true, Draft: &draft}); err != nil {
					t.Fatal(err)
				}
				if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true}); err == nil {
					t.Fatal("the live head of a converted document no longer reserves its unique value")
				}
				if public, err := application.Local().Find(ctx, "posts", liveID, ridu.FindOptions{Draft: &published}); err != nil {
					t.Fatal(err)
				} else if title, _ := public.Values["title"].StringValue(); title != "Live" {
					t.Fatalf("published document after a draft save = %#v", public)
				}
			}
		})
	}
}

// A versioned global converts like a collection: its one stored document is
// published with a first version.
func TestMongoDBMigrationEnablesVersionsOnStoredGlobal(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	directory := t.TempDir()
	config := func(versions bool) ridu.Config {
		return ridu.Config{Name: "MongoDB enable global versions", Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
		}}, Globals: []ridu.Global{{
			Slug: "settings", Versions: versions, VersionConfig: ridu.VersionConfig{Drafts: versions},
			Fields: field.Fields{field.Text("siteName")},
		}}}
	}
	resolve := func(versions bool) schema.Manifest {
		t.Helper()
		manifest, err := ridu.Resolve(config(versions))
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before, after := resolve(false), resolve(true)
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	unversioned, err := ridu.New(config(false), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unversioned.Local().UpdateGlobal(ctx, "settings", store.Values{"siteName": store.String("Ridu")}, ridu.MutationOptions{System: true}); err != nil {
		t.Fatal(err)
	}
	settings := after.Snapshot().Globals[0]
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), mongoVersionsChoice(settings.ID, ridumigration.ExistingPublished)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifacts(ctx, mongoDBMigrationVerifierConfig(t), directory); err != nil {
		t.Fatalf("verify replays the migration: %v", err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatal(err)
	}
	versioned, err := ridu.New(config(true), backend)
	if err != nil {
		t.Fatal(err)
	}
	published := false
	live, err := versioned.Local().Global(ctx, "settings", ridu.FindOptions{Draft: &published})
	if name, _ := live.Values["siteName"].StringValue(); err != nil || name != "Ridu" || live.Status != store.StatusPublished {
		t.Fatalf("published global = %#v, %v", live, err)
	}
	versions, err := versioned.Local().GlobalVersions(ctx, "settings", ridu.FindOptions{System: true})
	if err != nil || len(versions) != 1 || versions[0].Status != store.StatusPublished {
		t.Fatalf("global versions = %#v, %v", versions, err)
	}
}

func TestMongoDBRequireEmptyStopsWhileDocumentsAreStored(t *testing.T) {
	ctx := t.Context()
	backend := mongoIntegrationStore(t)
	directory := t.TempDir()
	seedMongoUnversionedPosts(t, backend, directory)
	after := mongoVersionsManifest(t, true, false)
	posts := mongoCollectionsBySlug(after.Snapshot().Collections)["posts"]
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), mongoVersionsChoice(posts.ID, ridumigration.ExistingDraft)); err == nil || !strings.Contains(err.Error(), "does not enable drafts") {
		t.Fatalf("draft choice without drafts = %v", err)
	}
	if _, err := CreateArtifact(ctx, directory, "enable-versions", after, time.Unix(2, 0), mongoVersionsChoice(posts.ID, ridumigration.ExistingRequireEmpty)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifacts(ctx, mongoDBMigrationVerifierConfig(t), directory); err != nil {
		t.Fatalf("verify replays an empty history: %v", err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err == nil ||
		!strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_NOT_EMPTY") || !strings.Contains(err.Error(), "2 documents") {
		t.Fatalf("require-empty with stored documents = %v", err)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || statuses[1].Applied {
		t.Fatalf("statuses after a refused migration = %#v, %v", statuses, err)
	}
	working := backend.database.Collection(physicalCollectionName(posts.ID))
	if converted, err := working.CountDocuments(ctx, bson.D{{Key: mongoStatusPath, Value: bson.D{{Key: "$ne", Value: ""}}}}); err != nil || converted != 0 {
		t.Fatalf("refused migration converted %d documents, %v", converted, err)
	}
	for _, namespace := range []string{physicalVersionCollectionName(posts.ID), physicalPublishedCollectionName(posts.ID)} {
		if names, err := backend.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: namespace}}); err != nil || len(names) != 0 {
			t.Fatalf("refused migration created %s: %v, %v", namespace, names, err)
		}
	}
	if _, err := working.DeleteMany(ctx, bson.D{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatalf("require-empty on an empty collection = %v", err)
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatal(err)
	}
}

func TestMongoDBDevelopmentSyncEnablesVersionsOnlyWhenEmpty(t *testing.T) {
	ctx := t.Context()
	before, after := mongoVersionsManifest(t, false, false), mongoVersionsManifest(t, true, true)
	backend := mongoIntegrationStore(t)
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err != nil {
		t.Fatalf("enabling versions on an empty collection = %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(after) {
		t.Fatalf("recorded schema after enabling versions: exists=%t, %v", exists, err)
	}
	if err := backend.VerifyIndexes(ctx, after); err != nil {
		t.Fatal(err)
	}
	versioned, err := ridu.New(mongoVersionsConfig(true, true), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := versioned.Local().Create(ctx, "posts", store.Values{"title": store.String("Draft")}, ridu.MutationOptions{System: true}); err != nil {
		t.Fatalf("create in a collection that started keeping versions = %v", err)
	}

	backend = mongoIntegrationStore(t)
	if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(mongoVersionsConfig(false, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Live")}, ridu.MutationOptions{System: true}); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, after); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_DOCUMENTS") {
		t.Fatalf("enabling versions over a stored document = %v", err)
	}
	if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(before) {
		t.Fatalf("refused sync changed the recorded schema: exists=%t, %v", exists, err)
	}
	// The refusal comes before any index work, so the database still serves
	// the recorded schema exactly.
	if err := backend.VerifyIndexes(ctx, before); err != nil {
		t.Fatalf("refused sync changed indexes: %v", err)
	}
	reports, err := backend.ReviewDevelopmentVersions(ctx, before, after)
	if err != nil || len(reports) != 1 || reports[0].Resource.Slug != "posts" || reports[0].Documents != 1 {
		t.Fatalf("review = %#v, %v", reports, err)
	}
	if reports, err := backend.ReviewDevelopmentVersions(ctx, before, before); err != nil || len(reports) != 0 {
		t.Fatalf("review without enabling versions = %#v, %v", reports, err)
	}
}
