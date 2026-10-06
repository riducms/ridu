package primitivefield

import (
	"fmt"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/schema"
)

// ValidateEvolution prevents storage casts from masquerading as value-shape
// migrations. Applications must use an explicit, supported migration procedure.
// Each resource's own fields are compared, and each block definition's fields
// once, wherever the definition is placed.
func ValidateEvolution(before, after schema.Snapshot) error {
	check := func(previous, next []schema.Field, prefix string) error {
		old := map[schema.StableID]schema.Field{}
		var index func([]schema.Field)
		index = func(fields []schema.Field) {
			for _, field := range fields {
				old[field.ID] = field
				if field.Nested != nil {
					index(field.Nested.ResolvedFields())
				}
			}
		}
		index(previous)
		var walk func([]schema.Field) error
		walk = func(fields []schema.Field) error {
			for _, field := range fields {
				if previous, ok := old[field.ID]; ok && previous.Type != field.Type && (IsList(previous) || IsList(field)) {
					return fmt.Errorf("field %q changes value shape from %q to %q; add a new field and migrate existing values explicitly, or register a supported compiled data transform; automatic list conversion is not available", prefix+field.Path.String(), previous.Type, field.Type)
				}
				if field.Nested != nil {
					if err := walk(field.Nested.ResolvedFields()); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return walk(next)
	}
	previous, current := blockgraph.New(before), blockgraph.New(after)
	for _, pair := range blockgraph.Survivors(previous, current, nil) {
		if err := check(pair.Before.Resource.Fields, pair.After.Resource.Fields, ""); err != nil {
			return err
		}
	}
	for _, key := range blockgraph.SortedKeys(blockgraph.Shared(previous, current, nil, true)) {
		beforeView, _ := previous.View(key)
		afterView, _ := current.View(key)
		if err := check(beforeView.ResolvedFields(), afterView.ResolvedFields(), key.Slug+"."); err != nil {
			return err
		}
	}
	return nil
}
