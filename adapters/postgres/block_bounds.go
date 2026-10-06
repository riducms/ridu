package postgres

import (
	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/schema"
)

// JSONB has no physical change for row bounds. Do not let a manifest-only
// artifact silently adopt stricter constraints over existing block content.
// Containers are compared in each resource's own fields and once in each block
// definition that stays placed, ordinarily or in an embedded tree.
func tightenedBlockBounds(before, after schema.Snapshot, mapping referenceShapeMapping, owners atlasIdentityMap) string {
	tightened := func(owner schema.StableID, previous, next []schema.Field, label string) string {
		targets := make(map[schema.StableID]schema.Field)
		for _, field := range postgresScopeFields(next, "", referenceShapeMapping{}, false) {
			targets[field.identity.ID] = field.field
		}
		for _, field := range postgresScopeFields(previous, owner, mapping, true) {
			current, exists := targets[field.identity.ID]
			if exists && field.field.Blocks != nil && current.Blocks != nil {
				if current.Blocks.MinRows > field.field.Blocks.MinRows || current.Blocks.MaxRows > 0 && (field.field.Blocks.MaxRows == 0 || current.Blocks.MaxRows < field.field.Blocks.MaxRows) {
					return label + current.Path.String()
				}
			}
		}
		return ""
	}
	previous, current := blockgraph.New(before), blockgraph.New(after)
	for _, pair := range blockgraph.Survivors(previous, current, owners.collections) {
		if path := tightened(pair.Before.Resource.ID, pair.Before.Resource.Fields, pair.After.Resource.Fields, ""); path != "" {
			return path
		}
	}
	for _, key := range blockgraph.SortedKeys(blockgraph.Shared(previous, current, owners.collections, true)) {
		beforeView, _ := previous.View(key)
		afterView, _ := current.View(key)
		if path := tightened(definitionOwner(key.Slug), beforeView.ResolvedFields(), afterView.ResolvedFields(), "block "+key.Slug+"."); path != "" {
			return path
		}
	}
	return ""
}
