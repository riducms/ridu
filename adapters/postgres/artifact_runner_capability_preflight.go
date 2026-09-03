package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// validatePostgresCapabilityDecreaseHistory checks the complete immutable
// lineage. A dormant
// decrease in applied history is still unsafe because a later artifact can
// re-enable the capability and reactivate retained framework state.
func validatePostgresCapabilityDecreaseHistory(files []migrationartifact.File) error {
	for _, file := range files {
		if err := validatePostgresCapabilityDecreasePreflight(file.Artifact); err != nil {
			return fmt.Errorf("migration %s: %w", file.Name, err)
		}
	}
	return nil
}

// validatePostgresCapabilityDecreasePreflight applies the capability-state
// safety boundary directly to an artifact's embedded manifests.
func validatePostgresCapabilityDecreasePreflight(artifact ridumigration.Artifact) error {
	if artifact.Before == nil {
		return nil
	}
	renames, err := postgresCapabilityCollectionRenames(artifact)
	if err != nil {
		return fmt.Errorf("capability-decrease collection identity: %w", err)
	}

	before, after := *artifact.Before, artifact.After
	risks := unsafeUploadCollectionRemovals(before, after, renames)
	risks = append(risks, unsafeCapabilityDisables(before, after, renames)...)
	if len(risks) == 0 {
		return nil
	}
	return &SafetyError{Risks: normalizeRisks(risks)}
}

// postgresCapabilityCollectionRenames reconstructs only the collection
// identity continuity required for a before/after capability comparison. A
// mapping is admitted only when its source disappears, its target is new, and
// both sides are unique collection identities. The independent physical rename
// topology check runs before a mapping is admitted; deterministic planner
// binding remains an additional pending-artifact preflight.
func postgresCapabilityCollectionRenames(artifact ridumigration.Artifact) ([]Rename, error) {
	if artifact.Before == nil {
		return nil, nil
	}
	if err := validateCapabilityResourceIdentities(*artifact.Before, "before"); err != nil {
		return nil, err
	}
	if err := validateCapabilityResourceIdentities(artifact.After, "after"); err != nil {
		return nil, err
	}
	// Content intent is not itself proof that the physical relation kept its
	// identity. Reuse the independent topology validator before any stable-ID-
	// changing intent can normalize the capability comparison.
	if err := validatePostgresResourceRetirementTopology(artifact); err != nil {
		return nil, fmt.Errorf("confirm physical collection identity continuity: %w", err)
	}

	var intents []ridumigration.Rename
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			var payload ridumigration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode content rename %s: %w", step.ID, err)
			}
			intents = append(intents, payload.Rename)
		}
	}

	beforeTargets := make(map[schema.StableID]schema.StableID)
	afterSources := make(map[schema.StableID]schema.StableID)
	result := make([]Rename, 0, len(intents))
	for _, intent := range intents {
		if intent.FieldBefore != "" || intent.FieldAfter != "" {
			continue
		}
		before, beforeFound := uniqueCapabilityCollectionBySlug(artifact.Before.Collections, intent.CollectionBefore)
		after, afterFound := uniqueCapabilityCollectionBySlug(artifact.After.Collections, intent.CollectionAfter)
		if !beforeFound || !afterFound {
			return nil, fmt.Errorf("collection rename addresses absent collection %q -> %q", intent.CollectionBefore, intent.CollectionAfter)
		}
		if before.ID == after.ID {
			// A slug rewrite does not change the resource identity used for the
			// capability comparison. unsafeCapabilityDisables still compares the
			// same stable ID and catches a simultaneous decrease.
			continue
		}
		if postgresResourceIDExists(artifact.After, before.ID) {
			return nil, fmt.Errorf("collection rename source %s remains in the after manifest", before.ID)
		}
		if postgresResourceIDExists(*artifact.Before, after.ID) {
			return nil, fmt.Errorf("collection rename target %s already existed in the before manifest", after.ID)
		}
		if existing, duplicate := beforeTargets[before.ID]; duplicate {
			return nil, fmt.Errorf("collection rename source %s is mapped more than once (first target %s)", before.ID, existing)
		}
		if existing, duplicate := afterSources[after.ID]; duplicate {
			return nil, fmt.Errorf("collection rename target %s is mapped more than once (first source %s)", after.ID, existing)
		}
		beforeTargets[before.ID] = after.ID
		afterSources[after.ID] = before.ID
		result = append(result, Rename{
			Kind: RenameCollection, BeforeCollection: before, AfterCollection: after,
		})
	}
	return result, nil
}

func validateCapabilityResourceIdentities(snapshot schema.Snapshot, label string) error {
	ids := make(map[schema.StableID]string, len(snapshot.Collections)+len(snapshot.Globals))
	collectionSlugs := make(map[schema.CollectionSlug]struct{}, len(snapshot.Collections))
	globalSlugs := make(map[schema.CollectionSlug]struct{}, len(snapshot.Globals))
	validate := func(kind string, index int, resource schema.Collection, slugs map[schema.CollectionSlug]struct{}) error {
		if previous, duplicate := ids[resource.ID]; duplicate {
			return fmt.Errorf("%s resource stable ID %s is duplicated by %s[%d] and %s", label, resource.ID, kind, index, previous)
		}
		if _, duplicate := slugs[resource.Slug]; duplicate {
			return fmt.Errorf("%s %s slug %q is duplicated", label, kind, resource.Slug)
		}
		ids[resource.ID] = fmt.Sprintf("%s[%d]", kind, index)
		slugs[resource.Slug] = struct{}{}
		return nil
	}
	for index, collection := range snapshot.Collections {
		if err := validate("collections", index, collection, collectionSlugs); err != nil {
			return err
		}
	}
	for index, global := range snapshot.Globals {
		if err := validate("globals", index, global, globalSlugs); err != nil {
			return err
		}
	}
	return nil
}

func uniqueCapabilityCollectionBySlug(collections []schema.Collection, slug schema.CollectionSlug) (schema.Collection, bool) {
	var result schema.Collection
	found := false
	for _, collection := range collections {
		if collection.Slug != slug {
			continue
		}
		if found {
			return schema.Collection{}, false
		}
		result, found = collection, true
	}
	return result, found
}
