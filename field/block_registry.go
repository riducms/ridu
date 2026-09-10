package field

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riducms/ridu/schema"
)

// BlockRegistry owns immutable, centrally declared block definitions. Bind makes
// references traversable without granting a referencing resource mutation rights.
type BlockRegistry struct {
	blocks map[string]Block
	order  []string
}

// NewBlockRegistry snapshots and resolves a finite registry. Forward references
// are supported; cycles and duplicate declarations are configuration errors.
func NewBlockRegistry(blocks ...Block) (BlockRegistry, error) {
	r := BlockRegistry{blocks: make(map[string]Block), order: make([]string, 0, len(blocks))}
	source := make(map[string]Block, len(blocks))
	names := map[string]string{}
	for i, b := range cloneBlocks(blocks) {
		p := fmt.Sprintf("blocks[%d].slug", i)
		if !schema.IsValidPluginKey(b.Slug) {
			return r, blockRegistryError("invalid_block_slug", p, "block slug must be lowercase kebab-case")
		}
		if _, exists := source[b.Slug]; exists {
			return r, blockRegistryError("duplicate_block_slug", p, "block slug is already registered")
		}
		if b.TypeName == "" {
			for _, part := range strings.Split(b.Slug, "-") {
				b.TypeName += strings.ToUpper(part[:1]) + part[1:]
			}
		}
		if !schema.IsValidBlockTypeName(b.TypeName) {
			return r, blockRegistryError("invalid_block_type_name", fmt.Sprintf("blocks[%d].typeName", i), "block type name must be an exported ASCII identifier")
		}
		if prior, exists := names[b.TypeName]; exists {
			return r, blockRegistryError("block_type_name_conflict", fmt.Sprintf("blocks[%d].typeName", i), fmt.Sprintf("type name %q is already used by block %q", b.TypeName, prior))
		}
		names[b.TypeName] = b.Slug
		source[b.Slug] = b
		r.order = append(r.order, b.Slug)
	}
	stack := []string{}
	var resolve func(string, string) (Block, error)
	resolve = func(slug, path string) (Block, error) {
		if b, ok := r.blocks[slug]; ok {
			return b, nil
		}
		b, ok := source[slug]
		if !ok {
			return Block{}, blockRegistryError("unknown_block_reference", path, fmt.Sprintf("block %q is not registered", slug))
		}
		if slices.Contains(stack, slug) {
			return Block{}, blockRegistryError("block_reference_cycle", path, "block reference cycle: "+strings.Join(append(slices.Clone(stack), slug), " -> "))
		}
		if len(stack) >= 48 {
			return Block{}, blockRegistryError("schema_depth_exceeded", path, "block reference nesting exceeds 48 levels")
		}
		stack = append(stack, slug)
		fields, err := bindBlockFields(b.Fields, "blocks."+slug+".fields", resolve, 0)
		stack = stack[:len(stack)-1]
		if err != nil {
			return Block{}, err
		}
		b.Fields = fields
		r.blocks[slug] = b
		return b, nil
	}
	for _, slug := range r.order {
		if _, err := resolve(slug, "blocks."+slug); err != nil {
			return BlockRegistry{}, err
		}
	}
	for _, slug := range r.order {
		if err := checkBlockTraversalBudget(r.blocks[slug].Fields, "blocks."+slug); err != nil {
			return BlockRegistry{}, err
		}
	}
	return r, nil
}

// Blocks returns detached declarations in registration order.
func (r BlockRegistry) Blocks() []Block {
	out := make([]Block, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, r.blocks[slug])
	}
	return cloneBlocks(out)
}

// Bind returns a detached field graph with read-only referenced branches.
// Policy closures remain application-owned and execute normally per occurrence.
func (r BlockRegistry) Bind(fields Fields) (Fields, error) { return r.BindAt(fields, "fields") }

// BindAt includes the resource's authored path in reference diagnostics.
func (r BlockRegistry) BindAt(fields Fields, path string) (Fields, error) {
	bound, err := bindBlockFields(fields, path, func(slug, path string) (Block, error) {
		b, ok := r.blocks[slug]
		if !ok {
			return Block{}, blockRegistryError("unknown_block_reference", path, fmt.Sprintf("block %q is not registered", slug))
		}
		return b, nil
	}, 0)
	if err != nil {
		return nil, err
	}
	if err := checkBlockTraversalBudget(bound, path); err != nil {
		return nil, err
	}
	return bound, nil
}

func bindBlockFields(fields Fields, path string, resolve func(string, string) (Block, error), depth int) (Fields, error) {
	if depth > 48 {
		return nil, blockRegistryError("schema_depth_exceeded", path, "field nesting exceeds 48 levels")
	}
	out := fields.Snapshot()
	for i, n := range out {
		d := Snapshot(n)
		p := fmt.Sprintf("%s[%d]", path, i)
		var err error
		if len(d.blockReferences) > 0 {
			if len(d.blocks) > 0 && !d.referencesBound {
				return nil, blockRegistryError("mixed_block_definitions", p, "use either inline blocks or references")
			}
			d.blocks, err = bindBlockList(d.blockReferences, p+".references", resolve)
			if err != nil {
				return nil, err
			}
			d.referencesBound = true
		} else {
			d.blocks = cloneBlocks(d.blocks)
			for j := range d.blocks {
				d.blocks[j].Fields, err = bindBlockFields(d.blocks[j].Fields, fmt.Sprintf("%s.blocks[%d].fields", p, j), resolve, depth+1)
				if err != nil {
					return nil, err
				}
			}
		}
		if len(d.fields) > 0 {
			d.fields, err = bindBlockFields(d.fields, p+".fields", resolve, depth+1)
			if err != nil {
				return nil, err
			}
		}
		d.pluginTrees = cloneEmbeddedTrees(d.pluginTrees)
		for ti := range d.pluginTrees {
			for ci := range d.pluginTrees[ti].Cases {
				c := &d.pluginTrees[ti].Cases[ci]
				cp := fmt.Sprintf("%s.embeddedTrees[%d].cases[%d]", p, ti, ci)
				if len(c.BlockReferences) > 0 {
					if len(c.Types) > 0 && !c.referencesBound {
						return nil, blockRegistryError("mixed_block_definitions", cp, "use either inline blocks or references")
					}
					c.Types, err = bindBlockList(c.BlockReferences, cp+".blockReferences", resolve)
					if err != nil {
						return nil, err
					}
					c.referencesBound = true
				} else {
					for bi := range c.Types {
						c.Types[bi].Fields, err = bindBlockFields(c.Types[bi].Fields, fmt.Sprintf("%s.types[%d].fields", cp, bi), resolve, depth+1)
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}
		out[i] = d
	}
	return out, nil
}
func bindBlockList(slugs []string, path string, resolve func(string, string) (Block, error)) ([]Block, error) {
	blocks := make([]Block, len(slugs))
	seen := map[string]bool{}
	for i, slug := range slugs {
		p := fmt.Sprintf("%s[%d]", path, i)
		if seen[slug] {
			return nil, blockRegistryError("duplicate_block_reference", p, "block reference is repeated")
		}
		seen[slug] = true
		b, err := resolve(slug, p)
		if err != nil {
			return nil, err
		}
		blocks[i] = b
	}
	return blocks, nil
}
func blockRegistryError(code, path, message string) error {
	return schema.NewValidationError([]schema.Issue{{Code: code, Path: path, Message: message}})
}

// References selects registered definitions in picker order. Inline declarations
// and references are mutually exclusive and validated during resolution.
func (f BlocksField) References(slugs ...string) BlocksField {
	if f.definition.referencesBound {
		f.definition.blocks = nil
	}
	f.definition.blockReferences = slices.Clone(slugs)
	f.definition.referencesBound = false
	return f
}

// BlockReferences returns the declared registry slugs, never expanded definitions.
func (d View) BlockReferences() []string { return slices.Clone(d.blockReferences) }

// Bound the logical placement graph too: a small DAG can have exponentially many paths.
func checkBlockTraversalBudget(fields Fields, path string) error {
	work := 0
	var walk func(Fields, int) error
	walk = func(fs Fields, depth int) error {
		if depth > 48 {
			return blockRegistryError("schema_depth_exceeded", path, "resolved field nesting exceeds 48 levels")
		}
		for _, node := range fs {
			work++
			if work > 100000 {
				return blockRegistryError("schema_work_exceeded", path, "resolved field graph exceeds 100000 placements")
			}
			for _, branch := range Snapshot(node).Branches() {
				if err := walk(branch.Fields, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(fields, 0)
}
