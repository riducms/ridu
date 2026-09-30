package primitivefield

import (
	"fmt"

	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/schema"
)

func ValidateIndexes(collection schema.Collection) error {
	return validateIndexes(collection, map[string]bool{})
}

// checked holds registered block slugs already validated; a registered block
// has the same fields wherever it is referenced.
func validateIndexes(collection schema.Collection, checked map[string]bool) error {
	var walk func([]schema.Field) error
	walkBlocks := func(types []schema.BlockType, registered bool) error {
		for _, block := range types {
			if registered && checked[block.Slug] {
				continue
			}
			if err := walk(block.ResolvedFields()); err != nil {
				return err
			}
			if registered {
				checked[block.Slug] = true
			}
		}
		return nil
	}
	walk = func(fields []schema.Field) error {
		for _, field := range fields {
			if IsList(field) && (field.Index || field.Unique) {
				return fmt.Errorf("primitive list %q does not support indexes or uniqueness", field.Path.String())
			}
			if field.Nested != nil {
				if err := walk(field.Nested.ResolvedFields()); err != nil {
					return err
				}
			}
			if field.Blocks != nil {
				if err := walkBlocks(field.Blocks.ResolvedTypes(), len(field.Blocks.BlockReferences) > 0); err != nil {
					return err
				}
			}
			if field.Plugin != nil {
				for _, tree := range field.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						if err := walkBlocks(c.ResolvedTypes(), len(c.BlockReferences) > 0); err != nil {
							return err
						}
					}
				}
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
	checked := map[string]bool{}
	for _, resource := range append(snapshot.Collections, snapshot.Globals...) {
		if err := validateIndexes(resource, checked); err != nil {
			return err
		}
	}
	return nil
}
