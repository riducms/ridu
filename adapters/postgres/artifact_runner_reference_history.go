package postgres

import (
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

// validatePostgresSemanticHistory is the shared semantic admission boundary
// for apply, verify, status, and plan. Keep these checks ordered and
// contract-independent so every surface reports the same unsafe history.
func validatePostgresSemanticHistory(files []migrationartifact.File) error {
	if err := validatePostgresPlannerHistory(files); err != nil {
		return err
	}
	if err := validatePostgresDataTransformIdentities(files, nil); err != nil {
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

// validatePostgresPlannerHistory admits only artifacts planned by this
// release's planner. Every committed artifact, applied or pending, is
// regenerated against that single contract.
func validatePostgresPlannerHistory(files []migrationartifact.File) error {
	for _, file := range files {
		planner := file.Artifact.Planner
		if planner.Name != atlasPlannerName || planner.Version != AtlasVersion {
			return fmt.Errorf("PostgreSQL migration %s uses unsupported planner %s %q; this Ridu release plans and applies only %s %q artifacts, so create a new migration history with ridu migrate create and apply it to a new database", file.Name, planner.Name, planner.Version, atlasPlannerName, AtlasVersion)
		}
	}
	return nil
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
