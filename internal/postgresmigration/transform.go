// Package postgresmigration owns private deterministic artifact assembly that
// is shared by the PostgreSQL planner and the portable CLI.
package postgresmigration

import (
	"fmt"
	"reflect"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// ValidateVersionedTransition refuses transform-backed field changes that
// would leave retained snapshots on the previous structure.
func ValidateVersionedTransition(before, after schema.Snapshot, transforms []migration.DataTransformDescriptor) error {
	if len(transforms) == 0 {
		return nil
	}
	if err := validateVersionedResources("collection", before.Collections, after.Collections); err != nil {
		return err
	}
	return validateVersionedResources("global", before.Globals, after.Globals)
}

func validateVersionedResources(kind string, before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		if previous.Versions == nil && !previous.Capabilities.Versions {
			continue
		}
		current, survives := afterByID[previous.ID]
		if survives && !reflect.DeepEqual(previous.Fields, current.Fields) {
			return fmt.Errorf("PostgreSQL data transforms cannot change fields on versioned %s %q until retained snapshots can be rewritten atomically", kind, previous.ID)
		}
	}
	return nil
}

// DataOnlyArtifact creates the ordinary assertion-only PostgreSQL phase that
// receives a compiled transform when the manifest itself is unchanged.
func DataOnlyArtifact(name string, planner migration.Planner, before, after schema.Manifest) (migration.Artifact, error) {
	artifact, err := migration.NewArtifact(name, planner, &before, after)
	if err != nil {
		return migration.Artifact{}, err
	}
	payload, err := migration.MarshalStepPayload(migration.AssertSchemaPayload{})
	if err != nil {
		return migration.Artifact{}, err
	}
	steps := []migration.Step{{
		ID: "step-0001", Kind: migration.StepAssertSchema, ExecutorVersion: 1,
		Name: "verify resulting PostgreSQL schema", Payload: payload,
	}}
	physicalBefore := migration.PhysicalDigestSeed(artifact.FromDigest)
	physicalAfter, err := migration.PhasePhysicalDigest(physicalBefore, migration.PhaseTransaction, steps)
	if err != nil {
		return migration.Artifact{}, err
	}
	artifact.Phases = []migration.Phase{{
		ID: "phase-001", Mode: migration.PhaseTransaction,
		PhysicalContractVersion: migration.PhysicalContractVersion,
		BeforePhysicalDigest:    physicalBefore, AfterPhysicalDigest: physicalAfter, Steps: steps,
	}}
	return artifact, nil
}

// BindDataTransforms inserts checksum-only callback identities immediately
// before the final PostgreSQL schema assertion without changing the artifact
// format or physical planner contract.
func BindDataTransforms(artifact migration.Artifact, transforms []migration.DataTransformDescriptor) (migration.Artifact, error) {
	if len(transforms) == 0 {
		return artifact, nil
	}
	seen := make(map[string]struct{}, len(transforms))
	for _, transform := range transforms {
		if err := transform.Validate(); err != nil {
			return migration.Artifact{}, err
		}
		if _, duplicate := seen[transform.Name]; duplicate {
			return migration.Artifact{}, fmt.Errorf("PostgreSQL data transform %q is bound more than once", transform.Name)
		}
		seen[transform.Name] = struct{}{}
	}
	if len(artifact.Phases) == 0 {
		return migration.Artifact{}, fmt.Errorf("PostgreSQL data transforms require a transaction phase")
	}
	phase := &artifact.Phases[len(artifact.Phases)-1]
	if phase.Mode != migration.PhaseTransaction || len(phase.Steps) == 0 || phase.Steps[len(phase.Steps)-1].Kind != migration.StepAssertSchema {
		return migration.Artifact{}, fmt.Errorf("PostgreSQL data transforms require a final transaction-phase schema assertion")
	}
	assertion := phase.Steps[len(phase.Steps)-1]
	steps := append([]migration.Step(nil), phase.Steps[:len(phase.Steps)-1]...)
	for _, transform := range transforms {
		payload, err := migration.MarshalStepPayload(migration.DataTransformPayload{Transform: transform})
		if err != nil {
			return migration.Artifact{}, err
		}
		steps = append(steps, migration.Step{
			Kind: migration.StepDataTransform, ExecutorVersion: 1,
			Name: "run data transform " + transform.Name, Payload: payload,
		})
	}
	phase.Steps = append(steps, assertion)
	stepNumber := 0
	for phaseIndex := range artifact.Phases {
		for stepIndex := range artifact.Phases[phaseIndex].Steps {
			stepNumber++
			artifact.Phases[phaseIndex].Steps[stepIndex].ID = fmt.Sprintf("step-%04d", stepNumber)
		}
	}
	physicalAfter, err := migration.PhasePhysicalDigest(phase.BeforePhysicalDigest, phase.Mode, phase.Steps)
	if err != nil {
		return migration.Artifact{}, err
	}
	phase.AfterPhysicalDigest = physicalAfter
	if err := artifact.Validate(); err != nil {
		return migration.Artifact{}, err
	}
	return artifact, nil
}
