package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

// validatePostgresSemanticHistory is the shared semantic admission boundary
// for apply, verify, status, and plan. Keep these checks ordered and
// contract-independent so every surface reports the same unsafe history.
func validatePostgresSemanticHistory(files []migrationartifact.File) error {
	if err := validatePostgresDataTransformIdentities(files, nil); err != nil {
		return err
	}
	if err := validatePostgresAuthIdentityPlannerHistory(files); err != nil {
		return err
	}
	if err := validatePostgresCollectionSlugRewriteHistory(files); err != nil {
		return err
	}
	if err := validatePostgresCapabilityDecreaseHistory(files); err != nil {
		return err
	}
	return validatePostgresReferenceSafetyHistory(files)
}

func validatePostgresAuthIdentityPlannerHistory(files []migrationartifact.File) error {
	previousVersion := ""
	for index, file := range files {
		version := file.Artifact.Planner.Version
		canonicalResources, hasCanonicalization, err := postgresHistoryCanonicalAuthResources(file.Artifact)
		if err != nil {
			return fmt.Errorf("migration %s: %w", file.Name, err)
		}
		if index == 0 {
			if hasCanonicalization {
				return fmt.Errorf("migration %s: initial PostgreSQL artifact cannot canonicalize legacy auth identities", file.Name)
			}
			previousVersion = version
			continue
		}
		requiresCanonicalization := previousVersion == atlasVersionV1 && version == AtlasVersion && file.Artifact.Before != nil &&
			len(ridumigration.RetainedAuthIdentityResources(*file.Artifact.Before, file.Artifact.After)) != 0
		if hasCanonicalization != requiresCanonicalization {
			return fmt.Errorf("migration %s: PostgreSQL planner transition %q -> %q has inconsistent auth identity canonicalization", file.Name, previousVersion, version)
		}
		if hasCanonicalization {
			expected := ridumigration.RetainedAuthIdentityResources(*file.Artifact.Before, file.Artifact.After)
			if !samePostgresAuthIdentityResources(canonicalResources, expected) {
				return fmt.Errorf("migration %s: PostgreSQL auth identity canonicalization scope does not match retained legacy fields", file.Name)
			}
		}
		if previousVersion == AtlasVersion && version == atlasVersionV1 {
			return fmt.Errorf("migration %s: PostgreSQL planner contract cannot downgrade from %q to %q", file.Name, previousVersion, version)
		}
		previousVersion = version
	}
	return nil
}

func postgresHistoryCanonicalAuthResources(artifact ridumigration.Artifact) ([]ridumigration.AuthIdentityResource, bool, error) {
	var resources []ridumigration.AuthIdentityResource
	found := false
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepCanonicalizeAuthIdentities {
				continue
			}
			if found {
				return nil, false, fmt.Errorf("contains more than one auth identity canonicalization step")
			}
			var payload ridumigration.CanonicalizeAuthIdentitiesPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, false, fmt.Errorf("has malformed auth identity canonicalization payload")
			}
			resources = payload.Resources
			found = true
		}
	}
	return resources, found, nil
}

// validatePostgresReferenceSafetyHistory checks every immutable transition.
// A reference-shape
// decrease is unsafe at the transition where values become dormant; checking
// only a later restore cannot distinguish those values from new data.
func validatePostgresReferenceSafetyHistory(files []migrationartifact.File) error {
	for _, file := range files {
		if err := validatePostgresReferenceSafety(file.Artifact); err != nil {
			return fmt.Errorf("migration %s: %w", file.Name, err)
		}
	}
	return nil
}

func validatePostgresReferenceSafety(artifact ridumigration.Artifact) error {
	if artifact.Before == nil {
		return nil
	}
	return validatePostgresResourceRetirementTopology(artifact)
}
