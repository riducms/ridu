package blocktypes

import "github.com/riducms/ridu/schema"

// DraftReadFields identifies authored fields that may be incomplete in a read.
// Reused block families inherit this from every owning resource: a single named
// output type must describe both its published and unfinished occurrences.
func (catalog *Catalog) DraftReadFields(snapshot schema.Snapshot) map[schema.StableID]bool {
	fields := map[schema.StableID]bool{}
	variants := map[string]bool{}
	var mark func([]schema.Field)
	mark = func(children []schema.Field) {
		for _, field := range children {
			fields[field.ID] = true
			if blocks, ok := catalog.Fields[field.ID]; ok {
				for _, name := range blocks.Variants {
					variants[name] = true
				}
			}
			mark(schema.EmbeddedBlocks(field))
			mark(schema.ChildFields(field))
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
	marked := map[string]bool{}
	for {
		progressed := false
		for _, variant := range catalog.Variants {
			if variants[variant.Name] && !marked[variant.Name] {
				marked[variant.Name] = true
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
