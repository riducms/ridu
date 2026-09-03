package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// validatePendingArtifactInspection regenerates every pending plan before any
// schema or ledger mutation.
func validatePendingArtifactInspection(ctx context.Context, files []migrationartifact.File) error {
	for _, file := range files {
		if err := validatePostgresPlannedSQL(ctx, file.Artifact); err != nil {
			return fmt.Errorf("migration %s: %w", file.Name, err)
		}
	}
	return nil
}

// validatePostgresPlannedSQL regenerates the deterministic execution contract
// from the immutable manifests and confirmed rename intent. Artifact digests
// alone are not an authenticity boundary: an editor can recompute them after
// inserting SQL, changing structured executors, or hiding a destructive risk.
// Exact regeneration binds every phase, step, payload, physical digest,
// runner contract, and planner finding before schema or ledger mutation.
func validatePostgresPlannedSQL(ctx context.Context, artifact ridumigration.Artifact) error {
	renames, err := postgresRenamesFromArtifact(artifact)
	if err != nil {
		return migrationPlanMismatch("reconstruct confirmed rename intent: %v", err)
	}
	after := schema.NewManifest(artifact.After)
	var before *schema.Manifest
	if artifact.Before != nil {
		manifest := schema.NewManifest(*artifact.Before)
		before = &manifest
	}
	target := postgresArtifactTargetContract(artifact)
	source := postgresArtifactSourceContract(artifact)
	transforms, err := postgresArtifactDataTransformDescriptors(artifact)
	if err != nil {
		return migrationPlanMismatch("reconstruct compiled data transforms: %v", err)
	}
	expected, err := buildArtifactWithPlannerContracts(ctx, artifact.Name, before, after, renames, true, source, target, transforms...)
	if err != nil {
		return migrationPlanMismatch("regenerate deterministic Atlas plan: %v", err)
	}
	if artifact.Planner.Name != expected.Planner.Name {
		return migrationPlanMismatch("planner name does not match deterministic execution contract: got %q, want %q", artifact.Planner.Name, expected.Planner.Name)
	}
	if artifact.MinimumRunnerContract != expected.MinimumRunnerContract {
		return migrationPlanMismatch("minimum runner contract does not match deterministic execution contract: got %d, want %d", artifact.MinimumRunnerContract, expected.MinimumRunnerContract)
	}
	if !reflect.DeepEqual(artifact.Phases, expected.Phases) {
		return migrationPlanMismatch("execution phases do not exactly match deterministic Atlas plan: %s", describePhaseMismatch(artifact.Phases, expected.Phases))
	}
	if !reflect.DeepEqual(artifact.Risks, expected.Risks) {
		return migrationPlanMismatch("migration risks do not exactly match deterministic Atlas findings: got %#v, want %#v", artifact.Risks, expected.Risks)
	}
	return nil
}

func postgresArtifactTargetContract(artifact ridumigration.Artifact) atlasPlannerContract {
	return postgresPlannerTargetContract(artifact.Planner.Version)
}

func postgresPlannerTargetContract(version string) atlasPlannerContract {
	target, supported := atlasPlannerContractFor(version)
	if supported {
		return target
	}
	// Atlas provenance may differ while the complete generated contract is
	// identical. Unknown versions are therefore regenerated against the
	// current semantic contract and retain their recorded provenance.
	target = currentAtlasPlannerContract()
	target.version = version
	return target
}

func postgresArtifactSourceContract(artifact ridumigration.Artifact) atlasPlannerContract {
	if artifactHasStep(artifact, ridumigration.StepCanonicalizeAuthIdentities) {
		legacy, _ := atlasPlannerContractFor(atlasVersionV1)
		return legacy
	}
	return postgresArtifactTargetContract(artifact)
}

func artifactHasStep(artifact ridumigration.Artifact, kind ridumigration.StepKind) bool {
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == kind {
				return true
			}
		}
	}
	return false
}

// CodeMigrationPlanMismatch is the stable boundary code for an artifact whose
// contract cannot be reproduced from its embedded immutable inputs.
const CodeMigrationPlanMismatch = "RIDU_MIGRATION_PLAN_MISMATCH"

func migrationPlanMismatch(format string, arguments ...any) error {
	return fmt.Errorf(CodeMigrationPlanMismatch+": "+format, arguments...)
}

func artifactHasOrdinarySQL(artifact ridumigration.Artifact) bool {
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind == ridumigration.StepSQL {
				return true
			}
		}
	}
	return false
}

func describePhaseMismatch(actual, expected []ridumigration.Phase) string {
	if len(actual) != len(expected) {
		return fmt.Sprintf("got %d phases, want %d", len(actual), len(expected))
	}
	for phaseIndex := range expected {
		if reflect.DeepEqual(actual[phaseIndex], expected[phaseIndex]) {
			continue
		}
		if len(actual[phaseIndex].Steps) != len(expected[phaseIndex].Steps) {
			return fmt.Sprintf("phase %d (%s) has %d steps, want %d", phaseIndex+1, actual[phaseIndex].ID, len(actual[phaseIndex].Steps), len(expected[phaseIndex].Steps))
		}
		for stepIndex := range expected[phaseIndex].Steps {
			if !reflect.DeepEqual(actual[phaseIndex].Steps[stepIndex], expected[phaseIndex].Steps[stepIndex]) {
				return fmt.Sprintf("phase %d (%s) step %d (%s) differs from regenerated kind %s payload and position", phaseIndex+1, actual[phaseIndex].ID, stepIndex+1, actual[phaseIndex].Steps[stepIndex].ID, expected[phaseIndex].Steps[stepIndex].Kind)
			}
		}
		return fmt.Sprintf("phase %d (%s) mode, identity, or physical digest differs", phaseIndex+1, actual[phaseIndex].ID)
	}
	return "unknown phase mismatch"
}

func postgresRenamesFromArtifact(artifact ridumigration.Artifact) ([]Rename, error) {
	var intents []ridumigration.Rename
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			var payload ridumigration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("step %s: %w", step.ID, err)
			}
			intents = append(intents, payload.Rename)
		}
	}
	if len(intents) == 0 {
		return nil, nil
	}
	if artifact.Before == nil {
		return nil, fmt.Errorf("initial artifact contains content rename intent")
	}

	before := schema.NewManifest(*artifact.Before)
	after := schema.NewManifest(artifact.After)
	collectionMapping, err := postgresPlannerCollectionMapping(artifact, intents)
	if err != nil {
		return nil, err
	}
	candidates := schemadiff.RenameCandidatesWithCollectionMapping(before, after, collectionMapping)
	candidateRenames := make([]Rename, 0, len(candidates))
	for _, candidate := range candidates {
		candidateRenames = append(candidateRenames, postgresRenameFromCandidate(candidate))
	}
	stableSlugRenames, err := withStableCollectionSlugRenames(*artifact.Before, artifact.After, nil)
	if err != nil {
		return nil, err
	}
	candidateRenames = append(candidateRenames, stableSlugRenames...)
	used := make([]bool, len(candidateRenames))
	result := make([]Rename, 0, len(intents))
	for _, intent := range intents {
		matched := -1
		var rename Rename
		for index, converted := range candidateRenames {
			if used[index] {
				continue
			}
			if !sameRenameIntent(renameIntent(converted), intent) {
				continue
			}
			if matched != -1 {
				return nil, fmt.Errorf("rename %s -> %s is ambiguous in embedded manifests", intent.CollectionBefore, intent.CollectionAfter)
			}
			matched, rename = index, converted
		}
		if matched == -1 {
			return nil, fmt.Errorf("rename %s -> %s does not exactly match a deterministic manifest candidate", intent.CollectionBefore, intent.CollectionAfter)
		}
		used[matched] = true
		result = append(result, rename)
	}
	return result, nil
}

// postgresPlannerCollectionMapping extracts the complete one-to-one set of
// collection identities declared by an artifact before structural candidates
// are replayed. Shape comparison may then normalize a referring owner's old
// relationship/upload target through another collection rename in the same
// transaction. The mapping is not an admission by itself: every intent must
// still match a unique deterministic candidate and the independent PostgreSQL
// topology validator must prove its exact physical rename.
func postgresPlannerCollectionMapping(artifact ridumigration.Artifact, intents []ridumigration.Rename) (map[schema.StableID]schema.StableID, error) {
	if artifact.Before == nil {
		return nil, fmt.Errorf("initial artifact contains content rename intent")
	}
	mapping := make(map[schema.StableID]schema.StableID)
	targets := make(map[schema.StableID]schema.StableID)
	for _, intent := range intents {
		if intent.FieldBefore != "" || intent.FieldAfter != "" {
			continue
		}
		before, beforeFound := collectionBySlug(artifact.Before.Collections, intent.CollectionBefore)
		after, afterFound := collectionBySlug(artifact.After.Collections, intent.CollectionAfter)
		if !beforeFound || !afterFound {
			return nil, fmt.Errorf("rename %s -> %s addresses an absent collection", intent.CollectionBefore, intent.CollectionAfter)
		}
		if existing, duplicate := mapping[before.ID]; duplicate && existing != after.ID {
			return nil, fmt.Errorf("rename source %s maps to both %s and %s", before.ID, existing, after.ID)
		}
		if existing, duplicate := targets[after.ID]; duplicate && existing != before.ID {
			return nil, fmt.Errorf("rename target %s is mapped from both %s and %s", after.ID, existing, before.ID)
		}
		mapping[before.ID] = after.ID
		targets[after.ID] = before.ID
	}
	return mapping, nil
}

func postgresRenameFromCandidate(candidate schemadiff.RenameCandidate) Rename {
	rename := Rename{
		Kind:             RenameKind(candidate.Kind),
		BeforeCollection: candidate.BeforeCollection,
		AfterCollection:  candidate.AfterCollection,
		BeforeField:      candidate.BeforeField,
		AfterField:       candidate.AfterField,
	}
	for _, pair := range candidate.Fields {
		rename.Fields = append(rename.Fields, FieldRename{Before: pair.Before, After: pair.After})
	}
	return rename
}

func sameRenameIntent(left, right ridumigration.Rename) bool {
	if left.CollectionBefore != right.CollectionBefore || left.CollectionAfter != right.CollectionAfter ||
		left.FieldBefore != right.FieldBefore || left.FieldAfter != right.FieldAfter || len(left.Fields) != len(right.Fields) {
		return false
	}
	for index := range left.Fields {
		if left.Fields[index] != right.Fields[index] {
			return false
		}
	}
	return true
}
