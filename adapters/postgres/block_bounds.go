package postgres

import "github.com/riducms/ridu/schema"

// JSONB has no physical change for row bounds. Do not let a manifest-only
// artifact silently adopt stricter constraints over existing block content.
func tightenedBlockBounds(before, after schema.Snapshot, mapping referenceShapeMapping, owners atlasIdentityMap) string {
	targets := make(map[string]schema.Field)
	var index func(schema.StableID, []schema.Field)
	index = func(owner schema.StableID, fields []schema.Field) {
		for _, field := range fields {
			targets[referenceShapeFieldKey(owner, field.ID)] = field
			index(owner, schema.ChildFields(field))
		}
	}
	for _, collection := range append(after.Collections, after.Globals...) {
		index(collection.ID, collection.Fields)
	}
	var inspect func(schema.StableID, []schema.Field) string
	inspect = func(owner schema.StableID, fields []schema.Field) string {
		for _, field := range fields {
			identity := mapping.field(owner, field)
			next, exists := targets[referenceShapeFieldKey(owners.collection(owner), identity.ID)]
			if exists && field.Blocks != nil && next.Blocks != nil {
				if next.Blocks.MinRows > field.Blocks.MinRows || next.Blocks.MaxRows > 0 && (field.Blocks.MaxRows == 0 || next.Blocks.MaxRows < field.Blocks.MaxRows) {
					return next.Path.String()
				}
			}
			if path := inspect(owner, schema.ChildFields(field)); path != "" {
				return path
			}
		}
		return ""
	}
	for _, collection := range append(before.Collections, before.Globals...) {
		if path := inspect(collection.ID, collection.Fields); path != "" {
			return path
		}
	}
	return ""
}
