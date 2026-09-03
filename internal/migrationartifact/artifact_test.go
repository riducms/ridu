package migrationartifact_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestArtifactDecoderAcceptsOnlyCurrentVersion(t *testing.T) {
	artifact := testArtifact(t, "initial", nil, testManifest("posts"))
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	candidate := bytes.Replace(encoded, []byte(`"version":1`), []byte(`"version":99`), 1)
	if _, err := migration.DecodeArtifact(candidate); err == nil || !strings.Contains(err.Error(), "unsupported migration artifact version") {
		t.Fatalf("unsupported artifact version decode = %v", err)
	}
	var previousShape map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &previousShape); err != nil {
		t.Fatal(err)
	}
	delete(previousShape, "previousArtifactDigest")
	withoutPredecessor, err := json.Marshal(previousShape)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migration.DecodeArtifact(withoutPredecessor); err == nil || !strings.Contains(err.Error(), "previous artifact digest is required") {
		t.Fatalf("missing predecessor field decode = %v", err)
	}
}

func TestReadAllRejectsArtifactLikeFilesWithUnsupportedFormats(t *testing.T) {
	for _, name := range []string{
		"20000101000000.000000000_initial.zeno.json",
		"20000101000000_initial.sql",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, name), []byte("discarded"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.ReadAll(directory); err == nil || !strings.Contains(err.Error(), "unsupported migration artifact filename") {
				t.Fatalf("unsupported artifact error = %v", err)
			}
		})
	}
}

func TestArtifactsAreImmutableAndHistoryMustBeContinuous(t *testing.T) {
	directory := t.TempDir()
	firstManifest := testManifest("posts")
	first := testArtifact(t, "initial", nil, firstManifest)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if _, err := migrationartifact.Create(directory, "initial", first, now); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", first, now); err == nil {
		t.Fatal("expected immutable filename collision")
	}
	if temporary, err := filepath.Glob(filepath.Join(directory, ".ridu-artifact-*.tmp")); err != nil || len(temporary) != 0 {
		t.Fatalf("temporary artifacts after collision = %#v, %v", temporary, err)
	}

	secondSnapshot := firstManifest.Snapshot()
	secondSnapshot.Collections[0].Slug = "articles"
	secondManifest := schema.NewManifest(secondSnapshot)
	second := testArtifact(t, "rename", &firstManifest, secondManifest)
	if _, err := migrationartifact.Create(directory, "rename", second, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}

	encoded, err := os.ReadFile(files[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[1].Path, append(encoded[:len(encoded)-2], ' ', '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.ReadAll(directory); err == nil {
		t.Fatal("expected edited artifact to fail validation")
	}
}

func TestHistoryBindsEachArtifactToItsExactPredecessor(t *testing.T) {
	directory := t.TempDir()
	manifest := testManifest("posts")
	first, err := migrationartifact.Create(directory, "initial", testArtifact(t, "initial", nil, manifest), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := migrationartifact.Create(directory, "second", testArtifact(t, "second", &manifest, manifest), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	third, err := migrationartifact.Create(directory, "third", testArtifact(t, "third", &manifest, manifest), time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if second.Artifact.PreviousArtifactDigest != first.Digest || third.Artifact.PreviousArtifactDigest != second.Digest {
		t.Fatalf("artifact predecessor bindings = %q, %q; want %q, %q", second.Artifact.PreviousArtifactDigest, third.Artifact.PreviousArtifactDigest, first.Digest, second.Digest)
	}
	encodedSecond, err := json.Marshal(second.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	decodedSecond, err := migration.DecodeArtifact(encodedSecond)
	if err != nil {
		t.Fatal(err)
	}
	decodedDigest, err := decodedSecond.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if decodedDigest != second.Digest {
		t.Fatalf("published artifact digest changed through codec: %s != %s", decodedDigest, second.Digest)
	}

	secondPrefix := strings.SplitN(second.Name, "_", 2)[0]
	thirdPrefix := strings.SplitN(third.Name, "_", 2)[0]
	temporary := second.Path + ".swap"
	if err := os.Rename(second.Path, temporary); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(third.Path, filepath.Join(directory, secondPrefix+"_third"+migrationartifact.Extension)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, filepath.Join(directory, thirdPrefix+"_second"+migrationartifact.Extension)); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.ReadAll(directory); err == nil || !strings.Contains(err.Error(), "predecessor differs") {
		t.Fatalf("reordered same-manifest history error = %v", err)
	}
}

func TestCreatePublishesOnlyACompleteArtifactToConcurrentReaders(t *testing.T) {
	directory := t.TempDir()
	manifest := testManifest("posts")
	artifact := testArtifact(t, "initial", nil, manifest)
	artifact.Risks = []migration.Risk{{
		Code: "RIDU_TEST_LARGE_ARTIFACT", Level: migration.RiskNotice,
		Message: strings.Repeat("complete-before-publish ", 1<<19),
	}}

	stop := make(chan struct{})
	errors := make(chan error, 1)
	var started sync.WaitGroup
	var finished sync.WaitGroup
	started.Add(1)
	finished.Add(1)
	go func() {
		defer finished.Done()
		started.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			files, err := migrationartifact.ReadAll(directory)
			if err != nil {
				errors <- err
				return
			}
			if len(files) > 1 {
				errors <- fmt.Errorf("reader observed %d artifacts during one publication", len(files))
				return
			}
		}
	}()
	started.Wait()
	created, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
	close(stop)
	finished.Wait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errors:
		t.Fatalf("concurrent reader observed partial artifact: %v", err)
	default:
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 || files[0].Digest != created.Digest {
		t.Fatalf("published artifacts = %#v, %v", files, err)
	}
}

func TestCreateSerializesDistinctConcurrentInitialArtifacts(t *testing.T) {
	directory := t.TempDir()
	firstManifest := testManifest("posts")
	secondManifest := testManifest("articles")
	inputs := []struct {
		name     string
		artifact migration.Artifact
		now      time.Time
	}{
		{name: "initial-posts", artifact: testArtifact(t, "initial-posts", nil, firstManifest), now: time.Unix(1, 0)},
		{name: "initial-articles", artifact: testArtifact(t, "initial-articles", nil, secondManifest), now: time.Unix(2, 0)},
	}

	start := make(chan struct{})
	results := make(chan error, len(inputs))
	var workers sync.WaitGroup
	for _, input := range inputs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := migrationartifact.Create(directory, input.name, input.artifact, input.now)
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	var failureErrors []error
	for err := range results {
		if err == nil {
			successes++
		} else {
			failureErrors = append(failureErrors, err)
		}
	}
	if successes != 1 || len(failureErrors) != 1 {
		t.Fatalf("distinct concurrent initial publication = %d successes/%d failures, want 1/1", successes, len(failureErrors))
	}
	failure := failureErrors[0].Error()
	if !strings.Contains(failure, "directory is busy") && !strings.Contains(failure, "does not continue the latest committed manifest") {
		t.Fatalf("distinct concurrent initial publication failed for an unrelated reason: %v", failureErrors[0])
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("distinct concurrent initial history = %d files, %v", len(files), err)
	}
	if temporary, err := filepath.Glob(filepath.Join(directory, ".ridu-artifact-*.tmp")); err != nil || len(temporary) != 0 {
		t.Fatalf("temporary artifacts after distinct concurrent publication = %#v, %v", temporary, err)
	}
}

func TestRequireCurrentHistoryRejectsEmptyAndStaleExecutableManifest(t *testing.T) {
	directory := t.TempDir()
	initialManifest := testManifest("posts")
	if _, err := migrationartifact.RequireCurrentHistory(directory, initialManifest); err == nil || !strings.Contains(err.Error(), "history is empty") {
		t.Fatalf("empty history error = %v", err)
	}
	artifact := testArtifact(t, "initial", nil, initialManifest)
	if _, err := migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if files, err := migrationartifact.RequireCurrentHistory(directory, initialManifest); err != nil || len(files) != 1 {
		t.Fatalf("current history = %#v, %v", files, err)
	}
	if files, err := migrationartifact.RequireCurrentHistoryForPlanner(directory, initialManifest, "atlas"); err != nil || len(files) != 1 {
		t.Fatalf("current planner history = %#v, %v", files, err)
	}
	if _, err := migrationartifact.RequireCurrentHistoryForPlanner(directory, initialManifest, "mongodb"); err == nil || !strings.Contains(err.Error(), `uses planner "atlas" instead of selected database planner "mongodb"`) {
		t.Fatalf("wrong planner history error = %v", err)
	}
	if _, err := migrationartifact.RequireCurrentHistoryForPlanner(directory, initialManifest, " "); err == nil || !strings.Contains(err.Error(), "planner name is required") {
		t.Fatalf("empty planner history error = %v", err)
	}
	if _, err := migrationartifact.RequireCurrentHistory(directory, testManifest("articles")); err == nil || !strings.Contains(err.Error(), "does not match latest migration artifact") {
		t.Fatalf("stale history error = %v", err)
	}
}

func testArtifact(t *testing.T, name string, before *schema.Manifest, after schema.Manifest) migration.Artifact {
	t.Helper()
	artifact, err := migration.NewArtifact(name, migration.Planner{Name: "atlas", Version: "1.0.0"}, before, after)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := migration.MarshalStepPayload(migration.AssertSchemaPayload{})
	if err != nil {
		t.Fatal(err)
	}
	digest := migration.PhysicalDigestSeed(artifact.FromDigest)
	phase := migration.Phase{
		ID: "phase-001", Mode: migration.PhaseTransaction, PhysicalContractVersion: migration.PhysicalContractVersion,
		BeforePhysicalDigest: digest,
		Steps:                []migration.Step{{ID: "step-0001", Kind: migration.StepAssertSchema, ExecutorVersion: 1, Name: "verify schema", Payload: payload}},
	}
	phase.AfterPhysicalDigest, err = migration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases = []migration.Phase{phase}
	return artifact
}

func testManifest(slug schema.CollectionSlug) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Test"}, Plugins: []schema.Plugin{},
		Collections: []schema.Collection{{ID: schema.StableID(slug), Slug: slug, Fields: []schema.Field{}}},
	})
}
