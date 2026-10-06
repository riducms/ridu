package config

import (
	"fmt"
	"sort"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

// bindInputBlocks binds every container to a definition of the configuration's
// block registry, interning inline declarations, so resolution sees one
// representation: containers that select definitions by slug.
func bindInputBlocks(input Input) (Input, error) {
	registry, err := field.NewBlockRegistry(input.Blocks...)
	if err != nil {
		return input, err
	}
	input.Collections = append([]Collection(nil), input.Collections...)
	input.Globals = append([]Global(nil), input.Globals...)
	for i := range input.Collections {
		input.Collections[i].Fields, err = registry.BindAt(input.Collections[i].Fields, fmt.Sprintf("collections[%d].fields", i))
		if err != nil {
			return input, err
		}
	}
	for i := range input.Globals {
		input.Globals[i].Fields, err = registry.BindAt(input.Globals[i].Fields, fmt.Sprintf("globals[%d].fields", i))
		if err != nil {
			return input, err
		}
	}
	// Binding interned the inline declarations, so the registry now holds every definition.
	input.Blocks = registry.Blocks()
	return input, nil
}

// resolveBlocks resolves a bound container's definitions once each and binds
// the container to them at this placement.
func (r *fieldResolver) resolveBlocks(blocks []field.Block, refs []string, path string, segments []string) *schema.BlocksField {
	if len(refs) == 0 {
		r.resolver.issue("missing_block_types", path+".blocks", "blocks field requires at least one block type")
		return &schema.BlocksField{}
	}
	for _, b := range blocks {
		r.resolver.resolveRegisteredBlock(b)
	}
	return schema.ReferenceTypes(refs, r.resolver.blockDefinitions, r.collectionID, segments)
}

// registeredBlocks resolves every definition, used or not, so invalid
// definitions do not depend on use, and returns them in slug order.
func (r *resolver) registeredBlocks() []schema.BlockType {
	for _, b := range r.input.Blocks {
		r.resolveRegisteredBlock(b)
	}
	slugs := make([]string, 0, len(r.blockTemplates))
	for slug := range r.blockTemplates {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	if len(slugs) == 0 {
		return nil
	}
	out := make([]schema.BlockType, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, r.blockTemplates[slug])
	}
	return out
}

// resolveRegisteredBlock resolves one definition on first use. Diagnostics name
// the definition, "blocks.<slug>", wherever it was declared.
func (r *resolver) resolveRegisteredBlock(b field.Block) {
	if _, exists := r.blockTemplates[b.Slug]; exists {
		return
	}
	path := "blocks." + b.Slug
	r.validateDefinitionIndexes(b.Fields, path+".fields", true)
	f := fieldResolver{resolver: r, collectionID: "block", seenIDs: map[schema.StableID]string{}, seenPaths: map[string]string{}}
	r.blockTemplates[b.Slug] = schema.BlockTemplate(f.resolveBlockDefinition(b, path), f.collectionID, []string{b.Slug})
}
