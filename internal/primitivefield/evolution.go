package primitivefield

import (
	"fmt"

	"github.com/riducms/ridu/schema"
)

// ValidateEvolution prevents storage casts from masquerading as value-shape
// migrations. Applications must use an explicit, supported migration procedure.
func ValidateEvolution(before, after schema.Snapshot) error {
	old := map[schema.StableID]schema.Field{}
	var walk func([]schema.Field, func(schema.Field) error) error
	walk = func(fields []schema.Field, visit func(schema.Field) error) error {
		for _, field := range fields {
			if err := visit(field); err != nil {
				return err
			}
			if err := walk(schema.ChildFields(field), visit); err != nil {
				return err
			}
		}
		return nil
	}
	index := func(fields []schema.Field) {
		_ = walk(fields, func(field schema.Field) error { old[field.ID] = field; return nil })
	}
	for _, resource := range before.Collections {
		index(resource.Fields)
	}
	for _, resource := range before.Globals {
		index(resource.Fields)
	}
	check := func(field schema.Field) error {
		if previous, ok := old[field.ID]; ok && previous.Type != field.Type && (IsList(previous) || IsList(field)) {
			return fmt.Errorf("field %q changes value shape from %q to %q; add a new field and migrate existing values explicitly, or register a supported compiled data transform; automatic list conversion is not available", field.Path.String(), previous.Type, field.Type)
		}
		return nil
	}
	for _, resource := range after.Collections {
		if err := walk(resource.Fields, check); err != nil {
			return err
		}
	}
	for _, resource := range after.Globals {
		if err := walk(resource.Fields, check); err != nil {
			return err
		}
	}
	return nil
}
