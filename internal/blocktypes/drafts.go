package blocktypes

import "github.com/riducms/ridu/schema"

// DraftReadFields identifies authored fields that may be incomplete in a read,
// by the field IDs a definition traversal sees (see Catalog.Fields).
// Reused block families inherit this from every owning resource: a single named
// output type must describe both its published and unfinished occurrences.
func (catalog *Catalog) DraftReadFields(snapshot schema.Snapshot) map[schema.StableID]bool {
	fields := map[schema.StableID]bool{}
	variants := map[string]bool{}
	// A shared definition's fields are the same values at every placement, so
	// each field list is marked once.
	marked := map[*schema.Field]bool{}
	var mark func([]schema.Field)
	mark = func(children []schema.Field) {
		if len(children) == 0 || marked[&children[0]] {
			return
		}
		marked[&children[0]] = true
		for _, field := range children {
			fields[field.ID] = true
			if blocks, ok := catalog.Fields[field.ID]; ok {
				for _, name := range blocks.Variants {
					variants[name] = true
				}
			}
			mark(schema.EmbeddedDefinitionBlocks(field))
			if field.Nested != nil {
				mark(field.Nested.ResolvedFields())
			}
			if field.Blocks != nil {
				for _, block := range field.Blocks.Definitions() {
					mark(block.ResolvedFields())
				}
			}
			if field.Plugin != nil {
				for _, tree := range field.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						for _, block := range c.Definitions() {
							mark(block.ResolvedFields())
						}
					}
				}
			}
		}
	}
	for _, resource := range append(append([]schema.Collection{}, snapshot.Collections...), snapshot.Globals...) {
		if resource.Versions != nil && resource.Versions.Drafts {
			mark(resource.Fields)
			if resource.Auth != nil {
				for _, field := range resource.Fields {
					if field.Name == resource.Auth.IdentityField {
						delete(fields, field.ID)
					}
				}
			}
		}
	}
	// The catalog may have selected a non-draft occurrence as the representative
	// of a reused family. Include that representative's stable child identities.
	markedVariants := map[string]bool{}
	for {
		progressed := false
		for _, variant := range catalog.Variants {
			if variants[variant.Name] && !markedVariants[variant.Name] {
				markedVariants[variant.Name] = true
				mark(variant.Block.ResolvedFields())
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return fields
}
