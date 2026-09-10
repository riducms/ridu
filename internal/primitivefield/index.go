package primitivefield

import (
	"fmt"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/schema"
)

func ValidateIndexes(collection schema.Collection) error {
	var walk func([]schema.Field) error
	walk = func(fields []schema.Field) error {
		for _, field := range fields {
			if IsList(field) && (field.Index || field.Unique) {
				return fmt.Errorf("primitive list %q does not support indexes or uniqueness", field.Path.String())
			}
			if err := walk(schema.ChildFields(field)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(collection.Fields); err != nil {
		return err
	}
	for _, index := range collection.Indexes {
		for _, path := range index.Fields {
			if field, ok := population.FieldAtPath(collection.Fields, path); ok && IsList(field) {
				return fmt.Errorf("primitive list %q cannot be part of a compound index", path.String())
			}
		}
	}
	return nil
}

func ValidateManifestIndexes(manifest schema.Manifest) error {
	snapshot := manifest.Snapshot()
	for _, resource := range append(snapshot.Collections, snapshot.Globals...) {
		if err := ValidateIndexes(resource); err != nil {
			return err
		}
	}
	return nil
}
