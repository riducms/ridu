package config

import (
	"fmt"
	"sort"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func bindInputBlocks(input Input) (Input, error) {
	registry, err := field.NewBlockRegistry(input.Blocks...)
	if err != nil {
		return input, err
	}
	input.Blocks = registry.Blocks()
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
	return input, nil
}
func (r *fieldResolver) resolveBlocks(blocks []field.Block, refs []string, path string, segments []string) *schema.BlocksField {
	if len(refs) == 0 {
		return &schema.BlocksField{Types: r.resolveBlockTypes(blocks, path, segments)}
	}
	for _, b := range blocks {
		r.resolver.resolveRegisteredBlock(b)
	}
	result := schema.ReferenceTypes(refs, r.resolver.blockTemplates, r.collectionID, segments)
	var validatePlacement func([]schema.Field)
	validatePlacement = func(fields []schema.Field) {
		for _, f := range fields {
			configPath := path + ".references." + f.Path.String()
			if prior, exists := r.seenIDs[f.ID]; exists {
				r.resolver.issue("duplicate_field_id", configPath, fmt.Sprintf("field ID %q is already used at %s", f.ID, prior))
			} else {
				r.seenIDs[f.ID] = configPath
			}
			if prior, exists := r.seenPaths[f.Path.String()]; exists {
				r.resolver.issue("duplicate_field_path", configPath, fmt.Sprintf("field path %q is already used at %s", f.Path.String(), prior))
			} else {
				r.seenPaths[f.Path.String()] = configPath
			}
			validatePlacement(schema.ChildFields(f))

		}
	}
	for _, b := range result.ResolvedTypes() {
		validatePlacement(b.ResolvedFields())
	}
	return result
}
func (r *resolver) registeredBlocks() []schema.BlockType {
	// Resolve unused declarations too, so invalid definitions do not depend on use.
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

func (r *resolver) resolveRegisteredBlock(b field.Block) {
	if _, exists := r.blockTemplates[b.Slug]; exists {
		return
	}
	f := fieldResolver{resolver: r, collectionID: schema.StableID("block-" + b.Slug), seenIDs: map[schema.StableID]string{}, seenPaths: map[string]string{}}
	resolved := f.resolveBlockTypes([]field.Block{b}, "blocks."+b.Slug, []string{"definition"})
	if len(resolved) == 1 {
		r.blockTemplates[b.Slug] = schema.BlockTemplate(resolved[0], f.collectionID, []string{"definition", b.Slug})
	}
}
