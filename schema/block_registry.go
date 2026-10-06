package schema

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/riducms/ridu/query"
)

// BlockDefinitions holds the block definitions of one schema and the definition
// views shared by all of their placements. A builder may add definitions while
// it resolves configuration; views are created on first use.
type BlockDefinitions struct {
	templates map[string]BlockType
	mu        sync.Mutex
	shared    map[sharedDefinitionKey]*sharedDefinition
}

type sharedDefinitionKey struct {
	slug string
	// suppressLocalization records a localized ancestor, which stores one
	// projected child tree per locale and so clears every child's Localized.
	suppressLocalization bool
}

type sharedDefinition struct {
	once     sync.Once
	template BlockType
	view     BlockType

	traitsOnce sync.Once
	traits     FieldTraits
}

// NewBlockDefinitions shares templates, a map of definition-relative blocks by
// slug. The caller must not change a template once a placement or definition
// view may have read it.
func NewBlockDefinitions(templates map[string]BlockType) *BlockDefinitions {
	return &BlockDefinitions{templates: templates, shared: make(map[sharedDefinitionKey]*sharedDefinition)}
}

// definition returns the shared view of slug. A missing template is not
// remembered, so a builder can still register it later.
func (d *BlockDefinitions) definition(slug string, suppressLocalization bool) (BlockType, bool) {
	key := sharedDefinitionKey{slug: slug, suppressLocalization: suppressLocalization}
	d.mu.Lock()
	entry, exists := d.shared[key]
	if !exists {
		template, found := d.templates[slug]
		if !found {
			d.mu.Unlock()
			return BlockType{}, false
		}
		entry = &sharedDefinition{template: template}
		d.shared[key] = entry
	}
	d.mu.Unlock()
	entry.once.Do(func() {
		entry.view = entry.template
		entry.view.bound = nil
		entry.view.shared = entry
		entry.view.Fields = d.shareFields(entry.template.Fields, suppressLocalization)
	})
	return entry.view, true
}

// shareFields copies a template's structure once per definition and locale
// scope, binding nested containers to the shared views of their definitions.
// Field metadata is shared with the template and must be treated as read-only.
func (d *BlockDefinitions) shareFields(source []Field, suppressLocalization bool) []Field {
	fields := make([]Field, len(source))
	for i, f := range source {
		f.Localized = f.Localized && !suppressLocalization
		children := suppressLocalization || f.Localized
		if f.Nested != nil {
			nested := *f.Nested
			nested.bound = nil
			nested.Fields = d.shareFields(f.Nested.Fields, children)
			f.Nested = &nested
		}
		if f.Blocks != nil {
			blocks := *f.Blocks
			blocks.bound = &boundBlocks{slugs: blocks.BlockReferences, scope: blockScope{registry: d, suppressLocalization: children, shared: true}}
			f.Blocks = &blocks
		}
		if f.Plugin != nil {
			plugin := *f.Plugin
			plugin.EmbeddedTrees = slices.Clone(f.Plugin.EmbeddedTrees)
			for ti := range plugin.EmbeddedTrees {
				tree := &plugin.EmbeddedTrees[ti]
				tree.Cases = slices.Clone(tree.Cases)
				for ci := range tree.Cases {
					c := &tree.Cases[ci]
					c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{registry: d, suppressLocalization: children, shared: true}}
				}
			}
			f.Plugin = &plugin
		}
		fields[i] = f
	}
	return fields
}

type blockScope struct {
	registry             *BlockDefinitions
	resource             StableID
	prefix               []string
	templateID           StableID
	suppressLocalization bool
	// shared binds a container inside a definition view, whose types are the
	// shared definition views rather than placement views.
	shared bool
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

	definitionsOnce sync.Once
	definitions     []BlockType
}

// ResolvedFields exposes placement-aware children. Definition metadata is
// shared; child placement views are materialized only when traversed. As with
// other snapshot fields, callers must not mutate a view concurrently with
// traversal.
func (b BlockType) ResolvedFields() []Field {
	if b.Fields == nil && b.bound != nil {
		return b.bound.get()
	}
	return b.Fields
}

// fieldCount is len(ResolvedFields()) without materializing a placement view.
func (b BlockType) fieldCount() int {
	if b.Fields == nil && b.bound != nil {
		return len(b.bound.source)
	}
	return len(b.Fields)
}
func (n NestedField) ResolvedFields() []Field {
	if n.Fields == nil && n.bound != nil {
		return n.bound.get()
	}
	return n.Fields
}

// ResolvedTypes returns placement views: each selected block's fields carry
// this placement's resource path and stable IDs. A materialized view is kept for
// the life of the schema, so traversing every placement costs time and memory
// for every path through the block graph. Processing that needs only field
// structure uses Definitions instead.
func (b BlocksField) ResolvedTypes() []BlockType {
	if b.bound == nil {
		return nil
	}
	return b.bound.get()
}
func (c EmbeddedTreeCase) ResolvedTypes() []BlockType {
	if c.bound == nil {
		return nil
	}
	return c.bound.get()
}

// Definitions returns the block types a row may select without creating
// placement views: each is its shared definition view, the same values at
// every placement, with definition-relative paths and IDs and this placement's
// inherited locale scope, whose nested containers also yield definition views.
// Work keyed by these values is therefore proportional to block definitions.
// The result is shared and read-only.
func (b BlocksField) Definitions() []BlockType {
	if b.bound == nil {
		return nil
	}
	return b.bound.definitionViews()
}

// Definition returns the block type that slug selects, as Definitions does.
func (b BlocksField) Definition(slug string) (BlockType, bool) {
	return definitionBySlug(b.Definitions(), slug)
}

// Definitions returns the case's selectable block types, as BlocksField.Definitions does.
func (c EmbeddedTreeCase) Definitions() []BlockType {
	if c.bound == nil {
		return nil
	}
	return c.bound.definitionViews()
}

// Definition returns the case's block type that slug selects.
func (c EmbeddedTreeCase) Definition(slug string) (BlockType, bool) {
	return definitionBySlug(c.Definitions(), slug)
}

func definitionBySlug(types []BlockType, slug string) (BlockType, bool) {
	for _, block := range types {
		if block.Slug == slug {
			return block, true
		}
	}
	return BlockType{}, false
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
	if b.scope.shared {
		return b.definitionViews()
	}
	b.once.Do(func() {
		b.blocks = make([]BlockType, 0, len(b.slugs))
		for _, slug := range b.slugs {
			template, ok := b.scope.registry.templates[slug]
			if !ok {
				continue
			}
			v := template
			v.Fields = nil
			v.bound = &boundFields{source: template.Fields, scope: blockScope{registry: b.scope.registry, resource: b.scope.resource, prefix: append(slices.Clone(b.scope.prefix), slug), templateID: StableID("block-" + slug), suppressLocalization: b.scope.suppressLocalization}}
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

// definitionViews lists the shared views of the container's selection in
// order, once per container.
func (b *boundBlocks) definitionViews() []BlockType {
	b.definitionsOnce.Do(func() {
		views := make([]BlockType, 0, len(b.slugs))
		if b.scope.registry == nil {
			b.definitions = views
			return
		}
		for _, slug := range b.slugs {
			if view, found := b.scope.registry.definition(slug, b.scope.suppressLocalization); found {
				views = append(views, view)
			}
		}
		b.definitions = views
	})
	return b.definitions
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
		v.BlockReferences = slices.Clone(b.BlockReferences)
		v.bound = &boundBlocks{slugs: v.BlockReferences, scope: blockScope{registry: s.registry, resource: s.resource, prefix: path, suppressLocalization: children.suppressLocalization}}
		f.Blocks = &v
	}
	if p != nil {
		v := *p
		v.Config = slices.Clone(p.Config)
		v.ReferenceKeys = slices.Clone(p.ReferenceKeys)
		v.EmbeddedTrees = cloneEmbeddedTrees(p.EmbeddedTrees)
		for ti := range v.EmbeddedTrees {
			t := &v.EmbeddedTrees[ti]
			for ci := range t.Cases {
				c := &t.Cases[ci]
				c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{registry: s.registry, resource: s.resource, prefix: append(append(slices.Clone(path), t.Key), c.TagValue), suppressLocalization: children.suppressLocalization}}
			}
		}
		f.Plugin = &v
	}
	return f
}

// PlacementFieldID derives the stable ID of a field placed at canonical path
// segments beneath a resource, so every placement of a block definition has
// its own persistence identity.
func PlacementFieldID(resource StableID, path []string) StableID {
	// Build in a stack buffer so the retained ID is allocated at its exact size.
	var buffer [128]byte
	id := append(buffer[:0], resource...)
	for _, part := range path {
		id = appendIDSegment(append(id, '-'), part)
	}
	return StableID(id)
}

// appendIDSegment appends one path segment in kebab case: '_' and '-' collapse
// to one separator, and an upper-case letter after the first starts a word.
func appendIDSegment(id []byte, part string) []byte {
	start, dash := len(id), false
	for index, c := range part {
		if c == '_' || c == '-' {
			if len(id) > start && !dash {
				id = append(id, '-')
				dash = true
			}
			continue
		}
		if unicode.IsUpper(c) {
			if index > 0 && len(id) > start && !dash {
				id = append(id, '-')
			}
			c = unicode.ToLower(c)
		}
		id = utf8.AppendRune(id, c)
		dash = false
	}
	return id
}

// BindBlockReferences validates the block graph and attaches lazy placement
// views. Every container must select known definitions without repeats; the
// definitions must be acyclic, keep every placement path within the nesting
// and query path bounds (see pathExtent), and derive distinct stable IDs (see
// validatePlacementIDs). Each check visits every definition once, so
// validation cost follows the number of definitions rather than placements.
// It does not evaluate access, defaults, hooks or other executable behavior.
// Failures are a *ValidationError naming the responsible definitions.
func BindBlockReferences(snapshot *Snapshot) error {
	defs := make(map[string]BlockType, len(snapshot.Blocks))
	for i, b := range snapshot.Blocks {
		if !IsValidPluginKey(b.Slug) {
			return blockGraphError("invalid_block_slug", fmt.Sprintf("blocks[%d].slug", i), "invalid block slug")
		}
		if _, ok := defs[b.Slug]; ok {
			return blockGraphError("duplicate_block_slug", fmt.Sprintf("blocks[%d].slug", i), fmt.Sprintf("duplicate block slug %q", b.Slug))
		}
		defs[b.Slug] = b
	}
	graph := blockGraph{definitions: defs, extents: map[string]pathExtent{}}
	for _, b := range snapshot.Blocks {
		if _, err := graph.definition(b.Slug, "blocks."+b.Slug); err != nil {
			return err
		}
	}
	for _, group := range []struct {
		kind      string
		resources []Collection
	}{{"collections", snapshot.Collections}, {"globals", snapshot.Globals}} {
		for index, r := range group.resources {
			path := fmt.Sprintf("%s[%d].fields", group.kind, index)
			extent, err := graph.fields(r.Fields, path)
			if err != nil {
				return err
			}
			if err := extent.check(path, fmt.Sprintf("fields of %q", r.Slug)); err != nil {
				return err
			}
		}
	}
	if err := validatePlacementIDs(snapshot, defs); err != nil {
		return err
	}
	attachBlockReferences(snapshot)
	return nil
}

// maxFieldDepth bounds how deeply fields nest along any placement path,
// counting the fields of every block definition the path enters.
const maxFieldDepth = 48

func blockGraphError(code, path, message string) error {
	return NewValidationError([]Issue{{Code: code, Path: path, Message: message}})
}

// pathExtent measures the longest placement paths beneath a field list: their
// field nesting levels, canonical path segments and path bytes, each with the
// definitions its longest path enters, outermost first. A placement's
// canonical path must remain a valid query path.
type pathExtent [3]pathMeasure

type pathMeasure struct {
	value int
	chain []string
}

const (
	extentLevels = iota
	extentSegments
	extentBytes
)

func (extent pathExtent) check(path, subject string) error {
	for measure, limit := range [3]int{maxFieldDepth, query.MaxPathSegments, query.MaxPathBytes} {
		if extent[measure].value <= limit {
			continue
		}
		graph := "its own fields"
		if len(extent[measure].chain) > 0 {
			graph = "block graph " + strings.Join(extent[measure].chain, " -> ")
		}
		message := fmt.Sprintf("%s nest %d levels deep through %s; at most %d are supported", subject, extent[measure].value, graph, limit)
		if measure != extentLevels {
			unit := "segments"
			if measure == extentBytes {
				unit = "bytes"
			}
			message = fmt.Sprintf("placement paths of %s reach %d %s through %s; canonical paths support at most %d", subject, extent[measure].value, unit, graph, limit)
		}
		return blockGraphError("schema_depth_exceeded", path, message)
	}
	return nil
}

// widen keeps the longer of each measure of extent and child, where child is
// reached through levels further nesting levels, segments further path
// segments and bytes further path bytes.
func (extent *pathExtent) widen(child pathExtent, levels, segments, bytes int) {
	for measure, offset := range [3]int{levels, segments, bytes} {
		if value := child[measure].value + offset; value > extent[measure].value {
			extent[measure] = pathMeasure{value: value, chain: child[measure].chain}
		}
	}
}

// blockGraph checks references, cycles and path extents once per definition.
type blockGraph struct {
	definitions map[string]BlockType
	extents     map[string]pathExtent
	stack       []string
}

func (g *blockGraph) definition(slug, path string) (pathExtent, error) {
	if extent, done := g.extents[slug]; done {
		return extent, nil
	}
	block, known := g.definitions[slug]
	if !known {
		return pathExtent{}, blockGraphError("unknown_block_reference", path, fmt.Sprintf("unknown block reference %q", slug))
	}
	if slices.Contains(g.stack, slug) {
		return pathExtent{}, blockGraphError("block_reference_cycle", path, "block reference cycle: "+strings.Join(append(slices.Clone(g.stack), slug), " -> "))
	}
	g.stack = append(g.stack, slug)
	extent, err := g.fields(block.Fields, "blocks."+slug)
	g.stack = g.stack[:len(g.stack)-1]
	if err != nil {
		return pathExtent{}, err
	}
	for measure := range extent {
		extent[measure].chain = append([]string{slug}, extent[measure].chain...)
	}
	if err := extent.check("blocks."+slug, fmt.Sprintf("fields of block %q", slug)); err != nil {
		return pathExtent{}, err
	}
	g.extents[slug] = extent
	return extent, nil
}

func (g *blockGraph) fields(fields []Field, path string) (pathExtent, error) {
	var longest pathExtent
	for _, f := range fields {
		at := path + "." + f.Name
		var children pathExtent
		if f.Nested != nil {
			nested, err := g.fields(f.Nested.Fields, at)
			if err != nil {
				return pathExtent{}, err
			}
			children.widen(nested, 0, 0, 1)
		}
		// selection widens children by each selected definition, entered
		// through prefix segments of prefix bytes beneath the field.
		selection := func(slugs []string, at string, segments, bytes int) error {
			seen := make(map[string]bool, len(slugs))
			for _, slug := range slugs {
				if seen[slug] {
					return blockGraphError("duplicate_block_reference", at, fmt.Sprintf("duplicate block reference %q", slug))
				}
				seen[slug] = true
				definition, err := g.definition(slug, at)
				if err != nil {
					return err
				}
				children.widen(definition, 0, segments+1, bytes+len(slug)+2)
			}
			return nil
		}
		if f.Blocks != nil {
			if err := selection(f.Blocks.BlockReferences, at, 0, 0); err != nil {
				return pathExtent{}, err
			}
		}
		if f.Plugin != nil {
			for _, tree := range f.Plugin.EmbeddedTrees {
				for _, c := range tree.Cases {
					if err := selection(c.BlockReferences, at+"."+tree.Key+"."+c.TagValue, 2, len(tree.Key)+len(c.TagValue)+2); err != nil {
						return pathExtent{}, err
					}
				}
			}
		}
		longest.widen(children, 1, 1, len(f.Name))
	}
	return longest, nil
}

// attachBlockReferences attaches lazy placement views to an already validated
// snapshot.
func attachBlockReferences(snapshot *Snapshot) {
	defs := make(map[string]BlockType, len(snapshot.Blocks))
	for _, b := range snapshot.Blocks {
		defs[b.Slug] = b
	}
	registry := NewBlockDefinitions(defs)
	for _, resources := range [][]Collection{snapshot.Collections, snapshot.Globals} {
		for _, r := range resources {
			attachBlockFields(r.Fields, registry, r.ID, false)
		}
	}
}

func attachBlockFields(fields []Field, registry *BlockDefinitions, resource StableID, localized bool) {
	for i := range fields {
		f := &fields[i]
		childLocalized := localized || f.Localized
		if f.Nested != nil {
			f.Nested.bound = nil
			attachBlockFields(f.Nested.Fields, registry, resource, childLocalized)
		}
		if f.Blocks != nil {
			f.Blocks.bound = &boundBlocks{slugs: f.Blocks.BlockReferences, scope: blockScope{registry: registry, resource: resource, prefix: f.Path.Segments(), suppressLocalization: childLocalized}}
		}
		if f.Plugin != nil {
			for ti := range f.Plugin.EmbeddedTrees {
				t := &f.Plugin.EmbeddedTrees[ti]
				for ci := range t.Cases {
					c := &t.Cases[ci]
					c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{registry: registry, resource: resource, prefix: append(append(slices.Clone(f.Path.Segments()), t.Key), c.TagValue), suppressLocalization: childLocalized}}
				}
			}
		}
	}
}

// BlockTemplate detaches one resolved definition and makes field paths relative
// to its root. Nested containers stay compact selections of other definitions.
func BlockTemplate(block BlockType, resource StableID, prefix []string) BlockType {
	block.Fields = cloneFields(block.ResolvedFields())
	block.bound, block.shared = nil, nil
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
			}
			if f.Plugin != nil {
				for ti := range f.Plugin.EmbeddedTrees {
					for ci := range f.Plugin.EmbeddedTrees[ti].Cases {
						f.Plugin.EmbeddedTrees[ti].Cases[ci].bound = nil
					}
				}
			}
		}
	}
	normalize(block.Fields)
	return block
}

// FieldAtPath returns the placement view of the field at canonical path
// segments beneath a resource's fields: field names, with a block slug after a
// Blocks field and tree key, case tag and slug after an embedding plugin field.
// It creates placement views only along that path.
func FieldAtPath(fields []Field, segments []string) (Field, bool) {
	current := fields
	for index := 0; index < len(segments); {
		var found *Field
		for i := range current {
			if current[i].Name == segments[index] {
				found = &current[i]
				break
			}
		}
		if found == nil {
			return Field{}, false
		}
		index++
		if index == len(segments) {
			return *found, true
		}
		switch {
		case found.Blocks != nil:
			block, ok := definitionBySlug(found.Blocks.ResolvedTypes(), segments[index])
			if !ok {
				return Field{}, false
			}
			index++
			current = block.ResolvedFields()
		case found.Nested != nil:
			current = found.Nested.ResolvedFields()
		case found.Plugin != nil && index+2 < len(segments):
			current = nil
			for _, tree := range found.Plugin.EmbeddedTrees {
				for _, c := range tree.Cases {
					if tree.Key == segments[index] && c.TagValue == segments[index+1] {
						if block, ok := definitionBySlug(c.ResolvedTypes(), segments[index+2]); ok {
							current = block.ResolvedFields()
						}
					}
				}
			}
			if current == nil {
				return Field{}, false
			}
			index += 3
		default:
			return Field{}, false
		}
	}
	return Field{}, false
}

// ClearDescendantLocalization marks every descendant of a localized field
// unlocalized during config resolution, because the field stores one complete
// child tree per locale. Nested fields change in place; block placements
// inherit the cleared locale scope without being materialized.
func ClearDescendantLocalization(field *Field) {
	if field.Nested != nil {
		fields := field.Nested.ResolvedFields()
		for index := range fields {
			fields[index].Localized = false
			ClearDescendantLocalization(&fields[index])
		}
	}
	suppress := func(bound *boundBlocks) *boundBlocks {
		if bound == nil || bound.scope.shared {
			return bound
		}
		scope := bound.scope
		scope.suppressLocalization = true
		return &boundBlocks{slugs: bound.slugs, scope: scope}
	}
	if field.Blocks != nil {
		field.Blocks.bound = suppress(field.Blocks.bound)
	}
	if field.Plugin != nil {
		for i := range field.Plugin.EmbeddedTrees {
			cases := field.Plugin.EmbeddedTrees[i].Cases
			for j := range cases {
				cases[j].bound = suppress(cases[j].bound)
			}
		}
	}
}

// ReferenceTypes binds a container during config resolution. The definitions
// are builder-owned until the final immutable manifest is installed.
func ReferenceTypes(slugs []string, definitions *BlockDefinitions, resource StableID, path []string) *BlocksField {
	return &BlocksField{BlockReferences: slices.Clone(slugs), bound: &boundBlocks{slugs: slices.Clone(slugs), scope: blockScope{registry: definitions, resource: resource, prefix: slices.Clone(path)}}}
}

// ReferenceCase binds an embedded tree case during config resolution, as ReferenceTypes does.
func ReferenceCase(c EmbeddedTreeCase, definitions *BlockDefinitions, resource StableID, path []string) EmbeddedTreeCase {
	c.BlockReferences = slices.Clone(c.BlockReferences)
	c.bound = &boundBlocks{slugs: c.BlockReferences, scope: blockScope{registry: definitions, resource: resource, prefix: slices.Clone(path)}}
	return c
}

func cloneBlockTypes(blocks []BlockType) []BlockType {
	if blocks == nil {
		return nil
	}
	out := make([]BlockType, len(blocks))
	for i, b := range blocks {
		out[i] = b
		out[i].bound, out[i].shared = nil, nil
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
