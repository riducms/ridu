package schema

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/riducms/ridu/query"
)

type blockScope struct {
	definitions          map[string]BlockType
	resource             StableID
	prefix               []string
	templateID           StableID
	suppressLocalization bool
}
type boundFields struct {
	once   sync.Once
	source []Field
	scope  blockScope
	fields []Field
}
type boundBlocks struct {
	once   sync.Once
	slugs  []string
	scope  blockScope
	blocks []BlockType
}

// ResolvedFields exposes placement-aware children. Registered metadata is shared;
// child placement views are materialized only when traversed. As with other
// snapshot fields, callers must not mutate a view concurrently with traversal.
func (b BlockType) ResolvedFields() []Field {
	if b.Fields == nil && b.bound != nil {
		return b.bound.get()
	}
	return b.Fields
}
func (n NestedField) ResolvedFields() []Field {
	if n.Fields == nil && n.bound != nil {
		return n.bound.get()
	}
	return n.Fields
}
func (b BlocksField) ResolvedTypes() []BlockType {
	if b.Types == nil && b.bound != nil {
		return b.bound.get()
	}
	return b.Types
}
func (c EmbeddedTreeCase) ResolvedTypes() []BlockType {
	if c.Types == nil && c.bound != nil {
		return c.bound.get()
	}
	return c.Types
}
func (b *boundFields) get() []Field {
	b.once.Do(func() {
		b.fields = make([]Field, len(b.source))
		for i, f := range b.source {
			b.fields[i] = b.scope.field(f)
		}
	})
	return b.fields
}
func (b *boundBlocks) get() []BlockType {
	b.once.Do(func() {
		b.blocks = make([]BlockType, 0, len(b.slugs))
		for _, slug := range b.slugs {
			template, ok := b.scope.definitions[slug]
			if !ok {
				continue
			}
			v := template
			v.Fields = nil
			v.bound = &boundFields{source: template.Fields, scope: blockScope{definitions: b.scope.definitions, resource: b.scope.resource, prefix: append(slices.Clone(b.scope.prefix), slug), templateID: StableID("block-" + slug), suppressLocalization: b.scope.suppressLocalization}}
			v.Labels.SingularTranslations = cloneStringMap(template.Labels.SingularTranslations)
			v.Labels.PluralTranslations = cloneStringMap(template.Labels.PluralTranslations)
			if template.Admin != nil {
				a := *template.Admin
				v.Admin = &a
			}
			b.blocks = append(b.blocks, v)
		}
	})
	return b.blocks
}

func (s blockScope) field(template Field) Field {
	// Copy only this node's mutable metadata, not its structural descendants.
	n, b, p := template.Nested, template.Blocks, template.Plugin
	template.Nested, template.Blocks, template.Plugin = nil, nil, nil
	f := cloneFields([]Field{template})[0]
	path := append(slices.Clone(s.prefix), template.Path.Segments()...)
	f.Path, _ = query.NewPath(path...)
	f.ID = PlacementFieldID(s.resource, path)
	rebasePresentation(&f, string(s.templateID), string(PlacementFieldID(s.resource, s.prefix)))
	f.Localized = f.Localized && !s.suppressLocalization
	children := s
	children.suppressLocalization = s.suppressLocalization || f.Localized
	if n != nil {
		metadata := *n
		metadata.Fields = nil
		metadata.bound = nil
		v := *cloneFields([]Field{{Nested: &metadata}})[0].Nested
		v.Fields = nil
		v.bound = &boundFields{source: n.Fields, scope: children}
		f.Nested = &v
	}
	if b != nil {
		v := *b
		v.Types = nil
		v.BlockReferences = slices.Clone(b.BlockReferences)
		if len(v.BlockReferences) > 0 {
			v.bound = &boundBlocks{slugs: v.BlockReferences, scope: blockScope{definitions: s.definitions, resource: s.resource, prefix: path, suppressLocalization: children.suppressLocalization}}
		} else {
			v.bound = nil
			v.Types = make([]BlockType, len(b.Types))
			for i, t := range b.Types {
				metadata := t
				metadata.Fields = nil
				v.Types[i] = cloneBlockTypes([]BlockType{metadata})[0]
				v.Types[i].Fields = nil
				v.Types[i].bound = &boundFields{source: t.Fields, scope: children}
			}
		}
		f.Blocks = &v
	}
	if p != nil {
		v := *p
		v.Config = slices.Clone(p.Config)
		v.ReferenceKeys = slices.Clone(p.ReferenceKeys)
		trees := slices.Clone(p.EmbeddedTrees)
		for ti := range trees {
			trees[ti].Cases = slices.Clone(trees[ti].Cases)
			for ci := range trees[ti].Cases {
				trees[ti].Cases[ci].Types = nil
			}
		}
		v.EmbeddedTrees = cloneEmbeddedTrees(trees)
		for ti := range v.EmbeddedTrees {
			t := &v.EmbeddedTrees[ti]
			for ci := range t.Cases {
				c := &t.Cases[ci]
				if len(c.BlockReferences) > 0 {
					c.Types = nil
					c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{definitions: s.definitions, resource: s.resource, prefix: append(append(slices.Clone(path), t.Key), c.TagValue), suppressLocalization: children.suppressLocalization}}
				} else {
					original := p.EmbeddedTrees[ti].Cases[ci].Types
					c.Types = make([]BlockType, len(original))
					for bi, block := range original {
						source := block.Fields
						block.Fields = nil
						c.Types[bi] = cloneBlockTypes([]BlockType{block})[0]
						c.Types[bi].Fields = nil
						c.Types[bi].bound = &boundFields{source: source, scope: children}
					}
				}
			}
		}
		f.Plugin = &v
	}
	return f
}

// PlacementFieldID keeps inline and referenced field persistence identities equal.
func PlacementFieldID(resource StableID, path []string) StableID {
	parts := []string{string(resource)}
	for _, part := range path {
		var out []rune
		for i, c := range []rune(part) {
			if c == '_' || c == '-' {
				if len(out) > 0 && out[len(out)-1] != '-' {
					out = append(out, '-')
				}
				continue
			}
			if unicode.IsUpper(c) {
				if i > 0 && len(out) > 0 && out[len(out)-1] != '-' {
					out = append(out, '-')
				}
				c = unicode.ToLower(c)
			}
			out = append(out, c)
		}
		parts = append(parts, string(out))
	}
	return StableID(strings.Join(parts, "-"))
}

// BindBlockReferences validates the registry and attaches lazy placement views.
// It does not evaluate access, defaults, hooks or other executable behavior.
func BindBlockReferences(snapshot *Snapshot) error {
	defs := make(map[string]BlockType, len(snapshot.Blocks))
	for i, b := range snapshot.Blocks {
		if !IsValidPluginKey(b.Slug) {
			return fmt.Errorf("blocks[%d].slug: invalid block slug", i)
		}
		if _, ok := defs[b.Slug]; ok {
			return fmt.Errorf("blocks[%d].slug: duplicate block slug %q", i, b.Slug)
		}
		defs[b.Slug] = b
	}
	state := map[string]uint8{}
	stack := []string{}
	var visit func(string) error
	var checkFields func([]Field, int) error
	checkList := func(inline []BlockType, refs []string) error {
		if len(inline) > 0 && len(refs) > 0 {
			return fmt.Errorf("use either inline blocks or blockReferences")
		}
		seen := map[string]bool{}
		for _, slug := range refs {
			if seen[slug] {
				return fmt.Errorf("duplicate block reference %q", slug)
			}
			seen[slug] = true
			if err := visit(slug); err != nil {
				return err
			}
		}
		return nil
	}
	checkFields = func(fields []Field, depth int) error {
		if depth > 48 {
			return fmt.Errorf("block schema nesting exceeds 48 levels")
		}
		for _, f := range fields {
			if f.Nested != nil {
				if err := checkFields(f.Nested.Fields, depth+1); err != nil {
					return err
				}
			}
			if f.Blocks != nil {
				if err := checkList(f.Blocks.Types, f.Blocks.BlockReferences); err != nil {
					return fmt.Errorf("%s: %w", f.Path.String(), err)
				}
				for _, b := range f.Blocks.Types {
					if err := checkFields(b.Fields, depth+1); err != nil {
						return err
					}
				}
			}
			if f.Plugin != nil {
				for _, t := range f.Plugin.EmbeddedTrees {
					for _, c := range t.Cases {
						if err := checkList(c.Types, c.BlockReferences); err != nil {
							return err
						}
						for _, b := range c.Types {
							if err := checkFields(b.Fields, depth+1); err != nil {
								return err
							}
						}
					}
				}
			}
		}
		return nil
	}
	visit = func(slug string) error {
		b, ok := defs[slug]
		if !ok {
			return fmt.Errorf("unknown block reference %q", slug)
		}
		if state[slug] == 2 {
			return nil
		}
		if state[slug] == 1 {
			return fmt.Errorf("block reference cycle: %s", strings.Join(append(slices.Clone(stack), slug), " -> "))
		}
		if len(stack) >= 48 {
			return fmt.Errorf("block reference nesting exceeds 48 levels")
		}
		state[slug] = 1
		stack = append(stack, slug)
		err := checkFields(b.Fields, 0)
		stack = stack[:len(stack)-1]
		state[slug] = 2
		return err
	}
	for _, b := range snapshot.Blocks {
		if err := visit(b.Slug); err != nil {
			return err
		}
	}
	if err := validateBlockGraphBudget(snapshot, defs); err != nil {
		return err
	}
	for _, resources := range [][]Collection{snapshot.Collections, snapshot.Globals} {
		for _, r := range resources {
			if err := checkFields(r.Fields, 0); err != nil {
				return err
			}
			attachBlockFields(r.Fields, defs, r.ID, false)
		}
	}
	return nil
}
func attachBlockFields(fields []Field, defs map[string]BlockType, resource StableID, localized bool) {
	for i := range fields {
		f := &fields[i]
		childLocalized := localized || f.Localized
		if f.Nested != nil {
			f.Nested.bound = nil
			attachBlockFields(f.Nested.Fields, defs, resource, childLocalized)
		}
		if f.Blocks != nil {
			b := f.Blocks
			b.bound = nil
			if len(b.BlockReferences) > 0 {
				b.Types = nil
				b.bound = &boundBlocks{slugs: b.BlockReferences, scope: blockScope{definitions: defs, resource: resource, prefix: f.Path.Segments(), suppressLocalization: childLocalized}}
			} else {
				for j := range b.Types {
					b.Types[j].bound = nil
					attachBlockFields(b.Types[j].Fields, defs, resource, childLocalized)
				}
			}
		}
		if f.Plugin != nil {
			for ti := range f.Plugin.EmbeddedTrees {
				t := &f.Plugin.EmbeddedTrees[ti]
				for ci := range t.Cases {
					c := &t.Cases[ci]
					c.bound = nil
					if len(c.BlockReferences) > 0 {
						c.Types = nil
						c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{definitions: defs, resource: resource, prefix: append(append(slices.Clone(f.Path.Segments()), t.Key), c.TagValue), suppressLocalization: childLocalized}}
					} else {
						for bi := range c.Types {
							c.Types[bi].bound = nil
							attachBlockFields(c.Types[bi].Fields, defs, resource, childLocalized)
						}
					}
				}
			}
		}
	}
}

// BlockTemplate detaches one resolved definition and makes field paths relative
// to its root. Only inline structure is copied; nested references stay compact.
func BlockTemplate(block BlockType, resource StableID, prefix []string) BlockType {
	block.Fields = cloneFields(block.ResolvedFields())
	block.bound = nil
	var normalize func([]Field)
	normalize = func(fields []Field) {
		for i := range fields {
			f := &fields[i]
			segments := f.Path.Segments()
			if len(segments) >= len(prefix) {
				segments = segments[len(prefix):]
			}
			rebasePresentation(f, string(PlacementFieldID(resource, prefix)), "block-"+block.Slug)
			f.Path, _ = query.NewPath(segments...)
			f.ID = PlacementFieldID(StableID("block-"+block.Slug), segments)
			if f.Nested != nil {
				normalize(f.Nested.Fields)
			}
			if f.Blocks != nil {
				f.Blocks.bound = nil
				for j := range f.Blocks.Types {
					normalize(f.Blocks.Types[j].Fields)
				}
			}
			if f.Plugin != nil {
				for ti := range f.Plugin.EmbeddedTrees {
					for ci := range f.Plugin.EmbeddedTrees[ti].Cases {
						c := &f.Plugin.EmbeddedTrees[ti].Cases[ci]
						c.bound = nil
						for bi := range c.Types {
							normalize(c.Types[bi].Fields)
						}
					}
				}
			}
		}
	}
	normalize(block.Fields)
	return block
}

// ReferenceTypes binds a registry field during config resolution. The definitions
// map is builder-owned until the final immutable manifest is installed.
func ReferenceTypes(slugs []string, definitions map[string]BlockType, resource StableID, path []string) *BlocksField {
	return &BlocksField{BlockReferences: slices.Clone(slugs), bound: &boundBlocks{slugs: slices.Clone(slugs), scope: blockScope{definitions: definitions, resource: resource, prefix: slices.Clone(path)}}}
}
func ReferenceCase(c EmbeddedTreeCase, definitions map[string]BlockType, resource StableID, path []string) EmbeddedTreeCase {
	c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{definitions: definitions, resource: resource, prefix: slices.Clone(path)}}
	return c
}

func cloneBlockTypes(blocks []BlockType) []BlockType {
	if blocks == nil {
		return nil
	}
	out := make([]BlockType, len(blocks))
	for i, b := range blocks {
		out[i] = b
		out[i].bound = nil
		out[i].Fields = cloneFields(b.Fields)
		out[i].Labels.SingularTranslations = cloneStringMap(b.Labels.SingularTranslations)
		out[i].Labels.PluralTranslations = cloneStringMap(b.Labels.PluralTranslations)
		if b.Admin != nil {
			a := *b.Admin
			out[i].Admin = &a
		}
	}
	return out
}

// Presentation groups share the identity of their first stored descendant.
func rebasePresentation(f *Field, from, to string) {
	rebase := func(id StableID) StableID { return StableID(to + strings.TrimPrefix(string(id), from)) }
	if f.Admin.Row != nil {
		f.Admin.Row.ID = rebase(f.Admin.Row.ID)
	}
	if f.Admin.TabGroup != nil {
		f.Admin.TabGroup.ID = rebase(f.Admin.TabGroup.ID)
	}
	if f.Admin.Collapsible != nil {
		f.Admin.Collapsible.ID = rebase(f.Admin.Collapsible.ID)
	}
}

// Count the logical placement graph without materializing any placement fields.
// Definition costs are memoized; only schema structure, never policy results, is reused.
func validateBlockGraphBudget(snapshot *Snapshot, definitions map[string]BlockType) error {
	type cost struct{ nodes, height int }
	cache := map[string]cost{}
	var fieldsCost func([]Field) (cost, error)
	var blockCost func(string) (cost, error)
	blockCost = func(slug string) (cost, error) {
		if c, ok := cache[slug]; ok {
			return c, nil
		}
		c, err := fieldsCost(definitions[slug].Fields)
		cache[slug] = c
		return c, err
	}
	fieldsCost = func(fields []Field) (cost, error) {
		total := cost{}
		for _, f := range fields {
			child := cost{}
			add := func(c cost, err error) error {
				if err != nil {
					return err
				}
				child.nodes += c.nodes
				child.height = max(child.height, c.height)
				return nil
			}
			if f.Nested != nil {
				if err := add(fieldsCost(f.Nested.Fields)); err != nil {
					return cost{}, err
				}
			}
			list := func(types []BlockType, refs []string) error {
				for _, b := range types {
					if err := add(fieldsCost(b.Fields)); err != nil {
						return err
					}
				}
				for _, slug := range refs {
					if err := add(blockCost(slug)); err != nil {
						return err
					}
				}
				return nil
			}
			if f.Blocks != nil {
				if err := list(f.Blocks.Types, f.Blocks.BlockReferences); err != nil {
					return cost{}, err
				}
			}
			if f.Plugin != nil {
				for _, tree := range f.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						if err := list(c.Types, c.BlockReferences); err != nil {
							return cost{}, err
						}
					}
				}
			}
			total.nodes += 1 + child.nodes
			total.height = max(total.height, 1+child.height)
			if total.nodes > 100000 || total.height > 48 {
				return cost{}, fmt.Errorf("%s: resolved block graph exceeds 100000 fields or 48 levels", f.Path.String())
			}
		}
		return total, nil
	}
	for _, b := range snapshot.Blocks {
		if _, err := blockCost(b.Slug); err != nil {
			return err
		}
	}
	nodes := 0
	for _, resources := range [][]Collection{snapshot.Collections, snapshot.Globals} {
		for _, r := range resources {
			c, err := fieldsCost(r.Fields)
			if err != nil {
				return err
			}
			nodes += c.nodes
			if nodes > 100000 {
				return fmt.Errorf("%s: resolved schema exceeds 100000 field placements", r.Slug)
			}
		}
	}
	return nil
}
