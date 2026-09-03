package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

func TestVerifyArtifactsWithOptionsRejectsStopBoundariesBeforeArtifactOrDatabaseAccess(t *testing.T) {
	for name, options := range map[string]RunnerOptions{
		"phase": {StopAfterPhase: "phase-001"},
		"step":  {StopAfterStep: "step-0001"},
	} {
		t.Run(name, func(t *testing.T) {
			err := VerifyArtifactsWithOptions(context.Background(), "not-a-database-url", t.TempDir(), options)
			if err == nil || !strings.Contains(err.Error(), "requires a complete shadow replay") {
				t.Fatalf("verify stop-boundary error = %v", err)
			}
		})
	}
}

func TestRequireCompleteShadowReplayRejectsPartialOrMismatchedStatus(t *testing.T) {
	files := []migrationartifact.File{
		{
			Name: "20000101000000.000000000_initial.ridu.json", Digest: strings.Repeat("a", 64),
			Artifact: ridumigration.Artifact{Version: ridumigration.ArtifactVersion, Phases: []ridumigration.Phase{{
				ID: "phase-001", Mode: ridumigration.PhaseTransaction,
				Steps: []ridumigration.Step{{ID: "step-0001", Kind: ridumigration.StepAssertSchema}},
			}}},
		},
		{
			Name: "20000101000001.000000000_followup.ridu.json", Digest: strings.Repeat("b", 64),
			Artifact: ridumigration.Artifact{Version: ridumigration.ArtifactVersion, Phases: []ridumigration.Phase{{
				ID: "phase-001", Mode: ridumigration.PhaseTransaction,
				Steps: []ridumigration.Step{{ID: "step-0001", Kind: ridumigration.StepAssertSchema}},
			}}},
		},
	}
	complete := []MigrationStatus{
		{Name: files[0].Name, Checksum: files[0].Digest, Version: ridumigration.ArtifactVersion, Applied: true, Phases: []MigrationPhaseStatus{{
			ID: "phase-001", Mode: ridumigration.PhaseTransaction, State: "complete",
			Steps: []MigrationStepStatus{{ID: "step-0001", Kind: ridumigration.StepAssertSchema, State: "complete"}},
		}}},
		{Name: files[1].Name, Checksum: files[1].Digest, Version: ridumigration.ArtifactVersion, Applied: true, Phases: []MigrationPhaseStatus{{
			ID: "phase-001", Mode: ridumigration.PhaseTransaction, State: "complete",
			Steps: []MigrationStepStatus{{ID: "step-0001", Kind: ridumigration.StepAssertSchema, State: "complete"}},
		}}},
	}
	if err := requireCompleteShadowReplay(files, complete); err != nil {
		t.Fatalf("complete shadow replay = %v", err)
	}

	cases := map[string][]MigrationStatus{
		"missing artifact": complete[:1],
		"pending artifact": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[1].Applied = false
			return candidate
		}(),
		"wrong digest": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Checksum = strings.Repeat("c", 64)
			return candidate
		}(),
		"partial phase": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Phases[0].State = "running"
			return candidate
		}(),
		"missing phase": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Phases = nil
			return candidate
		}(),
		"wrong phase mode": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Phases[0].Mode = ridumigration.PhaseBatch
			return candidate
		}(),
		"partial step": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Phases[0].Steps[0].State = "running"
			return candidate
		}(),
		"missing step": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Phases[0].Steps = nil
			return candidate
		}(),
		"wrong step kind": func() []MigrationStatus {
			candidate := cloneMigrationStatuses(complete)
			candidate[0].Phases[0].Steps[0].Kind = ridumigration.StepSQL
			return candidate
		}(),
	}
	for name, statuses := range cases {
		t.Run(name, func(t *testing.T) {
			if err := requireCompleteShadowReplay(files, statuses); err == nil {
				t.Fatal("partial shadow replay was accepted")
			}
		})
	}
}

func cloneMigrationStatuses(source []MigrationStatus) []MigrationStatus {
	result := append([]MigrationStatus(nil), source...)
	for statusIndex := range result {
		result[statusIndex].Phases = append([]MigrationPhaseStatus(nil), source[statusIndex].Phases...)
		for phaseIndex := range result[statusIndex].Phases {
			result[statusIndex].Phases[phaseIndex].Steps = append([]MigrationStepStatus(nil), source[statusIndex].Phases[phaseIndex].Steps...)
		}
	}
	return result
}
