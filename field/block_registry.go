package field

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/riducms/ridu/schema"
)

// BlockRegistry owns the block definitions of one configuration, one per slug.
// Blocks registered centrally and blocks declared inline in a Blocks field or
// an embedded tree case are the same kind of definition: binding interns every
// inline declaration by slug, so each definition is bound once and shared by
// all of its placements. Bound containers select their definitions by slug and
// do not grant a placement mutation rights over a shared definition.
type BlockRegistry struct {
	// sources are central declarations by slug, bound on first use.
	sources map[string]Block
	blocks  map[string]Block
	// declared records where each definition was first declared.
	declared map[string]string
	names    map[string]string
	order    []string
	// stack holds central definitions being bound, for cycle diagnostics.
	stack []string
	// inline memoizes bound inline declaration lists by the identity of the
	// authored list. Copies of one container share that list, so a list
	// placed many times through shared definitions is interned once.
	inline map[*Block][]Block
}

// NewBlockRegistry snapshots and binds a finite registry of centrally declared
// blocks. Forward references are supported; cycles and duplicate declarations
// are configuration errors. Inline declarations within these definitions are
// interned like any other.
func NewBlockRegistry(blocks ...Block) (*BlockRegistry, error) {
	r := &BlockRegistry{sources: map[string]Block{}, blocks: map[string]Block{}, declared: map[string]string{}, names: map[string]string{}, inline: map[*Block][]Block{}}
	for i, b := range cloneBlocks(blocks) {
		p := fmt.Sprintf("blocks[%d]", i)
		if !schema.IsValidPluginKey(b.Slug) {
			return nil, blockRegistryError("invalid_block_slug", p+".slug", "block slug must be lowercase kebab-case")
		}
		if _, exists := r.sources[b.Slug]; exists {
			return nil, blockRegistryError("duplicate_block_slug", p+".slug", "block slug is already registered")
		}
		b.TypeName = blockTypeName(b)
		if err := r.claimName(b, p); err != nil {
			return nil, err
		}
		r.sources[b.Slug] = b
		r.declared[b.Slug] = p
		r.order = append(r.order, b.Slug)
	}
	for _, slug := range slices.Clone(r.order) {
		if _, err := r.definition(slug, "blocks."+slug); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// blockTypeName is the definition's generated type family name: its explicit
// TypeName, or one derived from its slug.
func blockTypeName(b Block) string {
	if b.TypeName != "" {
		return b.TypeName
	}
	var name strings.Builder
	for _, part := range strings.Split(b.Slug, "-") {
		if part != "" {
			name.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return name.String()
}

func (r *BlockRegistry) claimName(b Block, path string) error {
	if !schema.IsValidBlockTypeName(b.TypeName) {
		return blockRegistryError("invalid_block_type_name", path+".typeName", "block type name must be an exported ASCII identifier")
	}
	if prior, exists := r.names[b.TypeName]; exists && prior != b.Slug {
		return blockRegistryError("block_type_name_conflict", path+".typeName", fmt.Sprintf("type name %q is already used by block %q declared at %s", b.TypeName, prior, r.declared[prior]))
	}
	r.names[b.TypeName] = b.Slug
	return nil
}

// Blocks returns detached definitions: central registrations in order, then
// interned inline declarations in the order they were first bound.
func (r *BlockRegistry) Blocks() []Block {
	out := make([]Block, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, r.blocks[slug])
	}
	return cloneBlocks(out)
}

// Bind returns a detached field graph whose containers select shared
// definitions. Policy closures remain application-owned and execute normally
// per occurrence.
func (r *BlockRegistry) Bind(fields Fields) (Fields, error) { return r.BindAt(fields, "fields") }

// BindAt is Bind with the resource's authored path in diagnostics. Inline
// declarations are interned into the registry: a slug declared before must
// have an identical declaration, or binding fails naming both.
func (r *BlockRegistry) BindAt(fields Fields, path string) (Fields, error) {
	return r.bindFields(fields, path, 0)
}

// definition binds a central definition on first use. References select only
// central definitions, so they never depend on configuration order.
func (r *BlockRegistry) definition(slug, path string) (Block, error) {
	source, central := r.sources[slug]
	if !central {
		if _, inline := r.blocks[slug]; inline {
			return Block{}, blockRegistryError("unknown_block_reference", path, fmt.Sprintf("block %q is declared inline at %s; register it in Config.Blocks to reference it", slug, r.declared[slug]))
		}
		return Block{}, blockRegistryError("unknown_block_reference", path, fmt.Sprintf("block %q is not registered", slug))
	}
	if b, bound := r.blocks[slug]; bound {
		return b, nil
	}
	if slices.Contains(r.stack, slug) {
		return Block{}, blockRegistryError("block_reference_cycle", path, "block reference cycle: "+strings.Join(append(slices.Clone(r.stack), slug), " -> "))
	}
	if len(r.stack) >= 48 {
		return Block{}, blockRegistryError("schema_depth_exceeded", path, "block reference nesting exceeds 48 levels")
	}
	r.stack = append(r.stack, slug)
	fields, err := r.bindFields(source.Fields, "blocks."+slug+".fields", 0)
	r.stack = r.stack[:len(r.stack)-1]
	if err != nil {
		return Block{}, err
	}
	source.Fields = fields
	r.blocks[slug] = source
	return source, nil
}

func (r *BlockRegistry) bindFields(fields Fields, path string, depth int) (Fields, error) {
	if depth > 48 {
		return nil, blockRegistryError("schema_depth_exceeded", path, "field nesting exceeds 48 levels")
	}
	out := fields.Snapshot()
	for i, n := range out {
		d := Snapshot(n)
		p := fmt.Sprintf("%s[%d]", path, i)
		var err error
		switch {
		case len(d.blockReferences) > 0:
			if len(d.blocks) > 0 && !d.referencesBound {
				return nil, blockRegistryError("mixed_block_definitions", p, "use either inline blocks or references")
			}
			if d.blocks, err = r.references(d.blockReferences, p+".references"); err != nil {
				return nil, err
			}
			d.referencesBound = true
		case len(d.blocks) > 0:
			if d.blocks, err = r.declare(d.blocks, p+".blocks", depth); err != nil {
				return nil, err
			}
			d.blockReferences, d.referencesBound = blockSlugs(d.blocks), true
		}
		if len(d.fields) > 0 {
			if d.fields, err = r.bindFields(d.fields, p+".fields", depth+1); err != nil {
				return nil, err
			}
		}
		if len(d.pluginTrees) > 0 {
			trees := cloneEmbeddedTrees(d.pluginTrees)
			for ti := range trees {
				for ci := range trees[ti].Cases {
					c := &trees[ti].Cases[ci]
					authored := d.pluginTrees[ti].Cases[ci].Types
					cp := fmt.Sprintf("%s.embeddedTrees[%d].cases[%d]", p, ti, ci)
					switch {
					case len(c.BlockReferences) > 0:
						if len(c.Types) > 0 && !c.referencesBound {
							return nil, blockRegistryError("mixed_block_definitions", cp, "use either inline blocks or references")
						}
						if c.Types, err = r.references(c.BlockReferences, cp+".blockReferences"); err != nil {
							return nil, err
						}
						c.referencesBound = true
					case len(c.Types) > 0:
						if c.Types, err = r.declare(authored, cp+".types", depth); err != nil {
							return nil, err
						}
						c.BlockReferences, c.referencesBound = blockSlugs(c.Types), true
					}
				}
			}
			d.pluginTrees = trees
		}
		out[i] = d
	}
	return out, nil
}

func blockSlugs(blocks []Block) []string {
	slugs := make([]string, len(blocks))
	for i, b := range blocks {
		slugs[i] = b.Slug
	}
	return slugs
}

func (r *BlockRegistry) references(slugs []string, path string) ([]Block, error) {
	blocks := make([]Block, len(slugs))
	seen := map[string]bool{}
	for i, slug := range slugs {
		p := fmt.Sprintf("%s[%d]", path, i)
		if seen[slug] {
			return nil, blockRegistryError("duplicate_block_reference", p, "block reference is repeated")
		}
		seen[slug] = true
		b, err := r.definition(slug, p)
		if err != nil {
			return nil, err
		}
		blocks[i] = b
	}
	return blocks, nil
}

// declare interns one container's inline declarations. A declaration is the
// same definition as an earlier one with its slug only when both bind to
// identical values: the same configuration, labels, admin settings and type
// name, and the same executable callbacks. Callbacks are values Go cannot
// compare, so they are identical only when shared from one declaration.
func (r *BlockRegistry) declare(authored []Block, path string, depth int) ([]Block, error) {
	if bound, seen := r.inline[&authored[0]]; seen && len(bound) == len(authored) {
		return bound, nil
	}
	bound := make([]Block, len(authored))
	seen := make(map[string]bool, len(authored))
	for i, b := range authored {
		p := fmt.Sprintf("%s[%d]", path, i)
		if !schema.IsValidPluginKey(b.Slug) || seen[b.Slug] {
			return nil, blockRegistryError("invalid_block_slug", p+".slug", "block slug must be unique lowercase kebab-case")
		}
		seen[b.Slug] = true
		b = b.Snapshot()
		b.TypeName = blockTypeName(b)
		fields, err := r.bindFields(b.Fields, p+".fields", depth+1)
		if err != nil {
			return nil, err
		}
		b.Fields = fields
		prior, exists := r.blocks[b.Slug]
		if !exists {
			if _, central := r.sources[b.Slug]; central {
				if prior, err = r.definition(b.Slug, p); err != nil {
					return nil, err
				}
				exists = true
			}
		}
		if exists {
			if !reflect.DeepEqual(prior, b) {
				return nil, blockRegistryError("block_definition_conflict", p, fmt.Sprintf("block %q differs from its declaration at %s; a slug names one definition, so reuse that declaration (callbacks are identical only when shared from one value), register it in Config.Blocks, or choose another slug", b.Slug, r.declared[b.Slug]))
			}
			bound[i] = prior
			continue
		}
		if err := r.claimName(b, p); err != nil {
			return nil, err
		}
		r.blocks[b.Slug] = b
		r.declared[b.Slug] = p
		r.order = append(r.order, b.Slug)
		bound[i] = b
	}
	r.inline[&authored[0]] = bound
	return bound, nil
}

func blockRegistryError(code, path, message string) error {
	return schema.NewValidationError([]schema.Issue{{Code: code, Path: path, Message: message}})
}

// References selects definitions registered in Config.Blocks, in picker order.
// Inline declarations and references are mutually exclusive and validated
// during resolution.
func (f BlocksField) References(slugs ...string) BlocksField {
	if f.definition.referencesBound {
		f.definition.blocks = nil
	}
	f.definition.blockReferences = slices.Clone(slugs)
	f.definition.referencesBound = false
	return f
}

// BlockReferences returns the slugs a container selects: its declared
// references or, once bound, its interned inline declarations. It never
// expands definitions.
func (d View) BlockReferences() []string { return slices.Clone(d.blockReferences) }
