package richtextblocks

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
	"testing"
)

// RunReferences runs the same persistence/lifecycle assertions with centrally
// registered definitions, including references inside embedded payloads.
func RunReferences(t *testing.T, factory Factory) {
	Run(t, func(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
		return factory(t, referenceConfiguration(t, config))
	})
}
func referenceConfiguration(t *testing.T, config ridu.Config) ridu.Config {
	t.Helper()
	config.Collections = append([]ridu.Collection(nil), config.Collections...)
	config.Globals = append([]ridu.Global(nil), config.Globals...)
	definitions := map[string]field.Block{}
	var convert func(field.Fields) field.Fields
	var register func([]field.Block) []string
	register = func(blocks []field.Block) []string {
		slugs := make([]string, 0, len(blocks))
		for _, b := range blocks {
			slugs = append(slugs, b.Slug)
			if _, exists := definitions[b.Slug]; exists {
				continue
			}
			b.Fields = convert(b.Fields)
			definitions[b.Slug] = b
			config.Blocks = append(config.Blocks, b)
		}
		return slugs
	}
	convert = func(fields field.Fields) field.Fields {
		result, err := fields.Edit(func(d *field.ChildrenDraft) error {
			for i, node := range fields {
				view := field.Snapshot(node)
				if view.Kind() == field.KindBlocks {
					slugs := register(view.Blocks())
					b, err := field.AsBlocks(node)
					if err != nil {
						return err
					}
					b, err = b.EditBlocks(func(defs *[]field.Block) error { *defs = nil; return nil })
					if err != nil {
						return err
					}
					if err := d.ReplaceAt(i, b.References(slugs...)); err != nil {
						return err
					}
				} else if view.Kind() == field.KindPlugin {
					trees := view.EmbeddedTrees()
					for ti := range trees {
						for ci := range trees[ti].Cases {
							c := &trees[ti].Cases[ci]
							c.BlockReferences = register(c.Types)
							c.Types = nil
						}
					}
					plugin, err := field.AsPlugin(node)
					if err != nil {
						return err
					}
					if err := d.ReplaceAt(i, plugin.EmbeddedTrees(trees...)); err != nil {
						return err
					}
				} else {
					for _, branch := range view.Branches() {
						children := convert(branch.Fields)
						if err := d.EditBranchAt(i, branch.Selector, func(c *field.ChildrenDraft) error {
							for j, child := range children {
								if err := c.ReplaceAt(j, child); err != nil {
									return err
								}
							}
							return nil
						}); err != nil {
							return err
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for i := range config.Collections {
		config.Collections[i].Fields = convert(config.Collections[i].Fields)
	}
	for i := range config.Globals {
		config.Globals[i].Fields = convert(config.Globals[i].Fields)
	}
	return config
}
