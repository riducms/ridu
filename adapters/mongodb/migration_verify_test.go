package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// createMongoDBTestHistory publishes a small current-planner history: an
// initial collection, an added index, and an unindexed field addition.
func createMongoDBTestHistory(t *testing.T) (string, []migrationartifact.File) {
	t.Helper()
	directory := t.TempDir()
	indexed := mongoDBMigrationTestManifest(t, true, false)
	summary := indexed.Snapshot()
	summary.Collections[0].Fields = append(summary.Collections[0].Fields, mongoDBMigrationTextField(t, "posts-summary", "summary", false, false, false))
	for index, input := range []struct {
		name     string
		manifest schema.Manifest
	}{
		{name: "initial", manifest: mongoDBMigrationTestManifest(t, false, false)},
		{name: "add-title-index", manifest: indexed},
		{name: "add-summary", manifest: schema.NewManifest(summary)},
	} {
		if _, err := CreateArtifact(context.Background(), directory, input.name, input.manifest, time.Unix(int64(index+1), 0), ArtifactOptions{}); err != nil {
			t.Fatalf("create MongoDB test artifact %s: %v", input.name, err)
		}
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	return directory, files
}

func TestMongoDBArtifactReplayPrecompilesEveryBoundary(t *testing.T) {
	_, files := createMongoDBTestHistory(t)
	replay, err := prepareMongoDBArtifactReplay(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 3 || len(replay[0].steps) != 22 || len(replay[1].steps) != 2 || len(replay[2].steps) != 1 {
		t.Fatalf("MongoDB replay topology = %#v", replay)
	}
	for artifactIndex, artifact := range replay {
		assertions := 0
		for stepIndex, step := range artifact.steps {
			switch step.kind {
			case ridumigration.StepMongoDBCreateIndex:
				if step.index.collection == "" || step.index.name == "" || step.index.definition.name != step.index.name {
					t.Fatalf("artifact %d step %d has incomplete private index reconstruction: %#v", artifactIndex, stepIndex, step)
				}
			case ridumigration.StepMongoDBAssertSchema:
				assertions++
				if stepIndex != len(artifact.steps)-1 {
					t.Fatalf("artifact %d assertion is not final", artifactIndex)
				}
			default:
				t.Fatalf("artifact %d step %d has unsupported kind %q", artifactIndex, stepIndex, step.kind)
			}
		}
		if assertions != 1 {
			t.Fatalf("artifact %d has %d assertions, want one", artifactIndex, assertions)
		}
	}
	if replay[1].steps[0].index.collection != "z_c_a44f1b9751711635" || replay[1].steps[0].index.name != "z_i_d96543b87c70b105" {
		t.Fatalf("additive index identity = %s/%s", replay[1].steps[0].index.collection, replay[1].steps[0].index.name)
	}
}

func TestMongoDBArtifactVerificationFailsBeforeConnectionForInvalidHistory(t *testing.T) {
	const databaseURL = "mongodb://offline-secret:do-not-leak@127.0.0.1:1/ridu"
	config := Config{
		DatabaseURL: databaseURL, AllowInsecureTransport: true,
		ConnectTimeout: time.Millisecond, ServerSelectionTimeout: time.Millisecond,
	}
	if err := VerifyArtifacts(context.Background(), config, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "history is empty") || strings.Contains(err.Error(), "offline-secret") {
		t.Fatalf("empty-history verification error = %v", err)
	}

	_, files := createMongoDBTestHistory(t)
	tampered := files[0].Artifact
	tampered.Risks = append(tampered.Risks, ridumigration.Risk{
		Code: "RIDU_TAMPER", Level: ridumigration.RiskNotice, Message: "edited committed history",
	})
	encoded, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, files[0].Name), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = VerifyArtifacts(context.Background(), config, directory)
	if err == nil || !strings.Contains(err.Error(), "does not match planner") ||
		strings.Contains(err.Error(), "offline-secret") || strings.Contains(err.Error(), "do-not-leak") ||
		strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("tampered-history verification error = %v", err)
	}
}

func TestMongoDBArtifactVerificationHonorsCancellationBeforeConnection(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err := VerifyArtifacts(canceled, Config{DatabaseURL: "mongodb://offline-secret@127.0.0.1:1/ridu", AllowInsecureTransport: true}, t.TempDir())
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "offline-secret") {
		t.Fatalf("canceled MongoDB verification = %v", err)
	}
}

func TestMongoDBShadowDatabaseNamesAreRandomSafeAndBounded(t *testing.T) {
	seen := make(map[string]struct{}, 128)
	for range 128 {
		name, err := newMongoDBShadowDatabaseName()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, mongoDBShadowDatabasePrefix) || len(name) != len(mongoDBShadowDatabasePrefix)+32 ||
			len(name) > 63 || !databaseNamePattern.MatchString(name) {
			t.Fatalf("unsafe MongoDB shadow database name %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("duplicate MongoDB shadow database name %q", name)
		}
		seen[name] = struct{}{}
	}
}
