package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ridumigration "github.com/riducms/ridu/migration"
)

func TestArtifactRunnerRejectsEmptyHistoryBeforeDatabaseAccess(t *testing.T) {
	directory := t.TempDir()
	var backend *Store
	if err := backend.applyArtifactFiles(context.Background(), nil, RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "history is empty") {
		t.Fatalf("empty apply error = %v", err)
	}
	if err := VerifyArtifacts(context.Background(), "not-a-database-url", directory); err == nil || !strings.Contains(err.Error(), "history is empty") {
		t.Fatalf("empty verify error = %v", err)
	}
	if _, err := backend.ArtifactStatus(context.Background(), directory); err == nil || !strings.Contains(err.Error(), "history is empty") {
		t.Fatalf("empty status error = %v", err)
	}
}

func TestTrafficSensitiveStepsAreMaintenanceClassified(t *testing.T) {
	for _, kind := range []ridumigration.StepKind{ridumigration.StepRenameContent, ridumigration.StepRetireResources, ridumigration.StepBackfillReferences} {
		t.Run(string(kind), func(t *testing.T) {
			artifact := ridumigration.Artifact{Version: ridumigration.ArtifactVersion, Phases: []ridumigration.Phase{{
				Mode: ridumigration.PhaseBatch, Steps: []ridumigration.Step{{Kind: kind}},
			}}}
			if !artifactRequiresMaintenance(artifact) {
				t.Fatalf("step %s was not classified as maintenance", kind)
			}
			err := &MaintenanceRequiredError{Artifacts: []string{"20000101000000.000000000_maintenance.ridu.json"}}
			if !errors.Is(err, ErrMaintenanceRequired) {
				t.Fatalf("maintenance error does not match stable sentinel: %v", err)
			} else if message := err.Error(); !strings.Contains(message, "every application process, writer, and worker") || !strings.Contains(message, "every retry") {
				t.Fatalf("maintenance instructions = %q", message)
			}
		})
	}
}

func TestRunnerOptionsAreBoundedUnlessUnboundedIsExplicit(t *testing.T) {
	defaults, err := normalizeRunnerOptions(RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]time.Duration{
		"advisory": defaults.AdvisoryLockWait, "lock": defaults.LockTimeout,
		"statement": defaults.StatementTimeout, "batch": defaults.BatchTimeout,
		"index": defaults.ConcurrentIndexTimeout, "idle": defaults.IdleInTransactionTimeout,
	} {
		if value <= 0 {
			t.Fatalf("%s default = %s", name, value)
		}
	}
	unbounded, err := normalizeRunnerOptions(RunnerOptions{AllowUnbounded: true})
	if err != nil || unbounded.AdvisoryLockWait != 0 || unbounded.StatementTimeout != 0 || unbounded.ConcurrentIndexTimeout != 0 {
		t.Fatalf("explicit unbounded options = %#v, %v", unbounded, err)
	}
	if _, err := normalizeRunnerOptions(RunnerOptions{LockTimeout: -time.Second}); err == nil {
		t.Fatal("negative timeout was admitted")
	}
}
