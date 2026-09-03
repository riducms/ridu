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
)

func TestMongoDBArtifactReplayPrecompilesEveryFrozenBoundary(t *testing.T) {
	files, err := migrationartifact.ReadAll(filepath.Join("testdata", "historical-v1"))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := prepareMongoDBArtifactReplay(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != 3 || len(replay[0].steps) != 22 || len(replay[1].steps) != 2 || len(replay[2].steps) != 1 {
		t.Fatalf("frozen MongoDB replay topology = %#v", replay)
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
		t.Fatalf("frozen additive index identity = %s/%s", replay[1].steps[0].index.collection, replay[1].steps[0].index.name)
	}
}

func TestMongoDBArtifactVerificationFailsBeforeConnectionForInvalidHistory(t *testing.T) {
	const databaseURL = "mongodb://offline-secret:do-not-leak@127.0.0.1:1/ridu"
	config := Config{
		DatabaseURL: databaseURL, AllowInsecureTransport: true,
		ConnectTimeout: time.Millisecond, ServerSelectionTimeout: time.Millisecond,
	}
	if err := verifyMongoDBArtifacts(context.Background(), config, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "history is empty") || strings.Contains(err.Error(), "offline-secret") {
		t.Fatalf("empty-history verification error = %v", err)
	}

	files, err := migrationartifact.ReadAll(filepath.Join("testdata", "historical-v1"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := files[0].Artifact
	tampered.Risks = append(tampered.Risks, ridumigration.Risk{
		Code: "RIDU_TAMPER", Level: ridumigration.RiskNotice, Message: "edited frozen history",
	})
	encoded, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, files[0].Name), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = verifyMongoDBArtifacts(context.Background(), config, directory)
	if err == nil || !strings.Contains(err.Error(), "does not match planner") ||
		strings.Contains(err.Error(), "offline-secret") || strings.Contains(err.Error(), "do-not-leak") ||
		strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("tampered-history verification error = %v", err)
	}
}

func TestMongoDBArtifactVerificationHonorsCancellationBeforeConnection(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err := verifyMongoDBArtifacts(canceled, Config{DatabaseURL: "mongodb://offline-secret@127.0.0.1:1/ridu", AllowInsecureTransport: true}, t.TempDir())
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
