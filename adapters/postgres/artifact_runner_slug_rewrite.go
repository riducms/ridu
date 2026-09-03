package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const (
	codeCollectionSlugRewriteMismatch      = "RIDU_COLLECTION_SLUG_REWRITE_MISMATCH"
	codeCollectionSlugRewriteOverlapUnsafe = "RIDU_COLLECTION_SLUG_REWRITE_OVERLAP_UNSAFE"
)

type collectionSlugRewritePair struct {
	Before schema.CollectionSlug
	After  schema.CollectionSlug
}

func validateCollectionSlugRewriteNonOverlap(pairs []collectionSlugRewritePair) error {
	sources := make(map[schema.CollectionSlug]struct{}, len(pairs))
	for _, pair := range pairs {
		if pair.Before != pair.After {
			sources[pair.Before] = struct{}{}
		}
	}
	for _, pair := range pairs {
		if pair.Before == pair.After {
			continue
		}
		if _, overlap := sources[pair.After]; overlap {
			return fmt.Errorf("%s: collection slug rewrite destination %q is also a source slug in the same artifact; chained and swapped rewrites cannot be applied sequentially without corrupting persisted references. Use non-overlapping temporary slugs across separate reviewed artifacts", codeCollectionSlugRewriteOverlapUnsafe, pair.After)
		}
	}
	return nil
}

func validatePlannedCollectionSlugRewriteNonOverlap(renames []Rename) error {
	pairs := make([]collectionSlugRewritePair, 0, len(renames))
	for _, rename := range renames {
		if rename.Kind != RenameCollection {
			continue
		}
		pairs = append(pairs, collectionSlugRewritePair{
			Before: rename.BeforeCollection.Slug,
			After:  rename.AfterCollection.Slug,
		})
	}
	return validateCollectionSlugRewriteNonOverlap(pairs)
}

func validatePostgresCollectionSlugRewriteHistory(files []migrationartifact.File) error {
	for _, file := range files {
		if err := validatePostgresCollectionSlugRewriteTopology(file.Artifact); err != nil {
			return fmt.Errorf("migration %s: %w", file.Name, err)
		}
	}
	return nil
}

// validatePostgresCollectionSlugRewriteTopology is deliberately independent
// of the artifact runner contract. A collection's stable ID proves
// continuity, but persisted polymorphic/plugin JSON stores its public slug. An
// assert-only or downgraded artifact that omits the exact rewrite would leave
// dormant values able to bind to a later collection reusing the old slug.
func validatePostgresCollectionSlugRewriteTopology(artifact ridumigration.Artifact) error {
	if artifact.Before == nil {
		return nil
	}
	intents, err := postgresArtifactRenameIntents(artifact)
	if err != nil {
		return fmt.Errorf("%s: %w", codeCollectionSlugRewriteMismatch, err)
	}
	pairs := make([]collectionSlugRewritePair, 0, len(intents))
	for _, intent := range intents {
		if intent.FieldBefore == "" && intent.FieldAfter == "" {
			pairs = append(pairs, collectionSlugRewritePair{Before: intent.CollectionBefore, After: intent.CollectionAfter})
		}
	}
	if err := validateCollectionSlugRewriteNonOverlap(pairs); err != nil {
		return err
	}
	beforeByID := make(map[schema.StableID]schema.Collection, len(artifact.Before.Collections))
	for _, collection := range artifact.Before.Collections {
		beforeByID[collection.ID] = collection
	}
	afterByID := make(map[schema.StableID]schema.Collection, len(artifact.After.Collections))
	for _, collection := range artifact.After.Collections {
		afterByID[collection.ID] = collection
	}
	expected := make(map[schema.StableID]struct{})
	for id, before := range beforeByID {
		if after, exists := afterByID[id]; exists && before.Slug != after.Slug {
			expected[id] = struct{}{}
		}
	}
	seen := make(map[schema.StableID]int)
	for _, intent := range intents {
		if intent.FieldBefore != "" || intent.FieldAfter != "" {
			continue
		}
		before, beforeFound := collectionBySlug(artifact.Before.Collections, intent.CollectionBefore)
		after, afterFound := collectionBySlug(artifact.After.Collections, intent.CollectionAfter)
		if !beforeFound || !afterFound {
			return fmt.Errorf("%s: collection content rewrite addresses absent collection %q -> %q", codeCollectionSlugRewriteMismatch, intent.CollectionBefore, intent.CollectionAfter)
		}
		if before.ID != after.ID {
			continue
		}
		if _, required := expected[before.ID]; !required || before.Slug == after.Slug || len(intent.Fields) != 0 {
			return fmt.Errorf("%s: same-identity collection rewrite %q -> %q is extra or does not exactly match a slug-only manifest transition", codeCollectionSlugRewriteMismatch, intent.CollectionBefore, intent.CollectionAfter)
		}
		seen[before.ID]++
		if seen[before.ID] != 1 {
			return fmt.Errorf("%s: same-identity collection %s has more than one content rewrite", codeCollectionSlugRewriteMismatch, before.ID)
		}
	}
	for id := range expected {
		if seen[id] != 1 {
			return fmt.Errorf("%s: same-identity collection slug change %q -> %q requires exactly one collection content rewrite so persisted polymorphic and plugin references cannot attach to a later slug reuse", codeCollectionSlugRewriteMismatch, beforeByID[id].Slug, afterByID[id].Slug)
		}
	}
	return nil
}

func postgresArtifactRenameIntents(artifact ridumigration.Artifact) ([]ridumigration.Rename, error) {
	var intents []ridumigration.Rename
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != ridumigration.StepRenameContent {
				continue
			}
			var payload ridumigration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode content rewrite %s: %w", step.ID, err)
			}
			intents = append(intents, payload.Rename)
		}
	}
	return intents, nil
}
