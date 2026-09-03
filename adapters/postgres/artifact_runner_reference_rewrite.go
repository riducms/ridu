package postgres

import (
	"fmt"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

type currentReferencePhysicalAliases struct {
	owners map[schema.StableID]schema.StableID
	roots  map[string]schema.StableID
}

func (aliases currentReferencePhysicalAliases) owner(id schema.StableID) schema.StableID {
	if mapped, exists := aliases.owners[id]; exists {
		return mapped
	}
	return id
}

func (aliases currentReferencePhysicalAliases) root(ownerID, rootID schema.StableID) schema.StableID {
	if mapped, exists := aliases.roots[statementFieldKey(ownerID, rootID)]; exists {
		return mapped
	}
	return rootID
}

// postgresCurrentReferencePhysicalAliases reconstructs only physical aliases
// already executed by Atlas before any content step. This lets an earlier
// target-slug rewrite inspect a later renamed owner's current JSON through its
// final table/column identity while the JSON still has its before-shape keys.
func postgresCurrentReferencePhysicalAliases(artifact ridumigration.Artifact) (currentReferencePhysicalAliases, error) {
	result := currentReferencePhysicalAliases{
		owners: make(map[schema.StableID]schema.StableID),
		roots:  make(map[string]schema.StableID),
	}
	if artifact.Before == nil {
		return result, nil
	}
	intents, err := postgresArtifactRenameIntents(artifact)
	if err != nil {
		return currentReferencePhysicalAliases{}, err
	}
	beforeBySlug := make(map[schema.CollectionSlug]schema.Collection, len(artifact.Before.Collections))
	beforeByID := make(map[schema.StableID]schema.Collection, len(artifact.Before.Collections))
	for _, collection := range artifact.Before.Collections {
		beforeBySlug[collection.Slug], beforeByID[collection.ID] = collection, collection
	}
	afterBySlug := make(map[schema.CollectionSlug]schema.Collection, len(artifact.After.Collections))
	afterByID := make(map[schema.StableID]schema.Collection, len(artifact.After.Collections))
	for _, collection := range artifact.After.Collections {
		afterBySlug[collection.Slug], afterByID[collection.ID] = collection, collection
	}

	for _, intent := range intents {
		if intent.FieldBefore != "" || intent.FieldAfter != "" {
			continue
		}
		before, beforeFound := beforeBySlug[intent.CollectionBefore]
		after, afterFound := afterBySlug[intent.CollectionAfter]
		if !beforeFound || !afterFound {
			return currentReferencePhysicalAliases{}, fmt.Errorf("current reference alias addresses absent collection %q -> %q", intent.CollectionBefore, intent.CollectionAfter)
		}
		if existing, duplicate := result.owners[before.ID]; duplicate && existing != after.ID {
			return currentReferencePhysicalAliases{}, fmt.Errorf("current reference owner %s maps to both %s and %s", before.ID, existing, after.ID)
		}
		result.owners[before.ID] = after.ID
	}

	addRoot := func(ownerID schema.StableID, before, after schema.Field) error {
		if len(before.Path.Segments()) != 1 || len(after.Path.Segments()) != 1 {
			return nil
		}
		key := statementFieldKey(ownerID, before.ID)
		if existing, duplicate := result.roots[key]; duplicate && existing != after.ID {
			return fmt.Errorf("current reference root %s.%s maps to both %s and %s", ownerID, before.ID, existing, after.ID)
		}
		result.roots[key] = after.ID
		return nil
	}

	// A confirmed collection rename changes derived field IDs even when a
	// root's public path does not change. Bind those equal paths explicitly.
	for beforeID, afterID := range result.owners {
		before, beforeFound := beforeByID[beforeID]
		after, afterFound := afterByID[afterID]
		if !beforeFound || !afterFound {
			continue
		}
		for _, beforeRoot := range before.Fields {
			afterRoot, exists := schemaFieldByPath(after.Fields, beforeRoot.Path.String())
			if !exists {
				continue
			}
			if err := addRoot(before.ID, beforeRoot, afterRoot); err != nil {
				return currentReferencePhysicalAliases{}, err
			}
		}
	}

	// Explicit root mappings cover standalone field renames and root path
	// changes bundled into a collection rename. Nested mappings intentionally
	// stay in JSON; their keys are moved by their later content step.
	for _, intent := range intents {
		beforeCollection, beforeFound := beforeBySlug[intent.CollectionBefore]
		afterCollection, afterFound := afterBySlug[intent.CollectionAfter]
		if !beforeFound || !afterFound {
			continue
		}
		pairs := append([]ridumigration.FieldRename(nil), intent.Fields...)
		if intent.FieldBefore != "" || intent.FieldAfter != "" {
			pairs = append(pairs, ridumigration.FieldRename{Before: intent.FieldBefore, After: intent.FieldAfter})
		}
		for _, pair := range pairs {
			beforeField, beforeExists := schemaFieldByPath(beforeCollection.Fields, pair.Before)
			afterField, afterExists := schemaFieldByPath(afterCollection.Fields, pair.After)
			if !beforeExists || !afterExists {
				return currentReferencePhysicalAliases{}, fmt.Errorf("current reference root alias addresses absent field %s.%s -> %s.%s", beforeCollection.ID, pair.Before, afterCollection.ID, pair.After)
			}
			if err := addRoot(beforeCollection.ID, beforeField, afterField); err != nil {
				return currentReferencePhysicalAliases{}, err
			}
		}
	}
	return result, nil
}
