package schema

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/riducms/ridu/query"
)

// EmbeddedTreeVersion is the declarative embedded-value metadata contract.
const EmbeddedTreeVersion uint32 = 1

// EmbeddedTree locates a structural node or node list through literal properties.
// Only Children is recursively traversed; case payloads use ordinary schemas.
type EmbeddedTree struct {
	Version  uint32             `json:"version"`
	Key      string             `json:"key"`
	Root     []string           `json:"root"`
	Children string             `json:"children"`
	Tag      string             `json:"tag"`
	Cases    []EmbeddedTreeCase `json:"cases"`
}

// EmbeddedTreeCase declares schema-owned payloads for one structural node tag.
// Like a Blocks field, it selects block definitions by slug; an
// empty selection reserves the envelope while admitting no payloads.
type EmbeddedTreeCase struct {
	BlockReferences []string `json:"blockReferences,omitempty"`
	bound           *boundBlocks
	TagValue        string `json:"tagValue"`
	Payload         string `json:"payload"`
	Discriminator   string `json:"discriminator"`
	Identity        string `json:"identity"`
}

// ChildFields enumerates immediate ordinary schema children, including those
// declared by plugins. It never examines document values or plugin settings.
// Like ResolvedFields, the result is a read-only view: when one declaration
// supplies every child it shares that declaration's slice instead of copying.
func ChildFields(field Field) []Field {
	var single []Field
	sources, total := 0, 0
	eachChildFieldSource(field, func(fields []Field) {
		if len(fields) != 0 {
			sources++
			total += len(fields)
			single = fields
		}
	})
	if sources <= 1 {
		// The full slice expression keeps a caller's append from writing into
		// the shared declaration.
		return single[:len(single):len(single)]
	}
	result := make([]Field, 0, total)
	eachChildFieldSource(field, func(fields []Field) { result = append(result, fields...) })
	return result
}

// DefinitionChildFields is ChildFields for processing that needs only field
// structure: it enters blocks through their shared definitions
// (see BlocksField.Definitions), so it never creates placement views and its
// results are the same values wherever a definition is placed. Paths and IDs
// beneath a block are definition-relative.
func DefinitionChildFields(field Field) []Field {
	var single []Field
	sources, total := 0, 0
	eachDefinitionChildSource(field, func(fields []Field) {
		if len(fields) != 0 {
			sources++
			total += len(fields)
			single = fields
		}
	})
	if sources <= 1 {
		return single[:len(single):len(single)]
	}
	result := make([]Field, 0, total)
	eachDefinitionChildSource(field, func(fields []Field) { result = append(result, fields...) })
	return result
}

// FieldListSet records field lists a definition traversal has entered. A shared
// definition passes the same slice wherever it is placed, so a traversal that
// enters each list once does work proportional to definitions.
type FieldListSet map[*Field]struct{}

// Add reports whether fields were not yet in the set, adding them. An empty
// list is never recorded and always reports true.
func (set FieldListSet) Add(fields []Field) bool {
	if len(fields) == 0 {
		return true
	}
	if _, seen := set[&fields[0]]; seen {
		return false
	}
	set[&fields[0]] = struct{}{}
	return true
}

// WalkDefinitionFields calls visit for each field reachable from the roots
// through DefinitionChildFields, in pre-order. Each shared definition's fields
// are visited once, however often the definition is placed, so the walk is
// proportional to definitions. It stops and returns false when visit does.
func WalkDefinitionFields(visit func(Field) bool, roots ...[]Field) bool {
	seen := FieldListSet{}
	var walk func([]Field) bool
	walk = func(fields []Field) bool {
		if !seen.Add(fields) {
			return true
		}
		for _, field := range fields {
			if !visit(field) {
				return false
			}
			complete := true
			eachDefinitionChildSource(field, func(children []Field) {
				complete = complete && walk(children)
			})
			if !complete {
				return false
			}
		}
		return true
	}
	for _, fields := range roots {
		if !walk(fields) {
			return false
		}
	}
	return true
}

func eachChildFieldSource(field Field, visit func([]Field)) {
	if field.Nested != nil {
		visit(field.Nested.ResolvedFields())
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.ResolvedTypes() {
			visit(block.ResolvedFields())
		}
	}
	if field.Plugin != nil {
		for _, tree := range field.Plugin.EmbeddedTrees {
			for _, treeCase := range tree.Cases {
				for _, block := range treeCase.ResolvedTypes() {
					visit(block.ResolvedFields())
				}
			}
		}
	}
}

// WalkPlacements calls visit for every field placement beneath a resource's
// fields, with the placement's absolute path segments, without creating
// placement views. shared reports a field of a block definition, whose own
// Path and ID are definition-relative; PlacementFieldID derives its placement
// ID from the segments. visit reports whether to visit the field's
// descendants. A complete walk is proportional to placements, so use it only
// for work that is inherently per placement, and prune it; segments is reused
// between calls.
func WalkPlacements(fields []Field, visit func(segments []string, field Field, shared bool) bool) {
	var segments []string
	// base is the placement of the definition whose fields' paths are relative
	// to it, or -1 for a resource's own fields, whose paths are absolute.
	var walk func(fields []Field, base int)
	walk = func(fields []Field, base int) {
		for _, field := range fields {
			if base < 0 {
				segments = field.Path.AppendSegments(segments[:0])
			} else {
				segments = field.Path.AppendSegments(segments[:base])
			}
			if !visit(segments, field, base >= 0) {
				continue
			}
			own := len(segments)
			if field.Nested != nil {
				walk(field.Nested.ResolvedFields(), base)
			}
			if field.Blocks != nil {
				for _, block := range field.Blocks.Definitions() {
					segments = append(segments[:own], block.Slug)
					walk(block.ResolvedFields(), own+1)
				}
			}
			if field.Plugin != nil {
				for _, tree := range field.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						for _, block := range c.Definitions() {
							segments = append(segments[:own], tree.Key, c.TagValue, block.Slug)
							walk(block.ResolvedFields(), own+3)
						}
					}
				}
			}
		}
	}
	walk(fields, -1)
}

// FieldTraits summarizes the fields of a list or beneath a field, so a value
// traversal can skip a subtree whose schema cannot need it.
type FieldTraits uint8

const (
	// FieldTraitLocalized marks a localized field.
	FieldTraitLocalized FieldTraits = 1 << iota
	// FieldTraitEmbedded marks a plugin field that declares embedded schemas.
	FieldTraitEmbedded
)

// ListTraits summarizes fields and all of their descendants. Each
// block definition is summarized once, however often it is placed.
func ListTraits(fields []Field) FieldTraits {
	var traits FieldTraits
	for _, field := range fields {
		if field.Localized {
			traits |= FieldTraitLocalized
		}
		if field.Plugin != nil && len(field.Plugin.EmbeddedTrees) != 0 {
			traits |= FieldTraitEmbedded
		}
		traits |= DescendantTraits(field)
	}
	return traits
}

// DescendantTraits summarizes the fields beneath field, as ListTraits does.
func DescendantTraits(field Field) FieldTraits {
	var traits FieldTraits
	blockTraits := func(block BlockType) {
		if block.shared == nil {
			traits |= ListTraits(block.ResolvedFields())
			return
		}
		block.shared.traitsOnce.Do(func() { block.shared.traits = ListTraits(block.Fields) })
		traits |= block.shared.traits
	}
	if field.Nested != nil {
		traits |= ListTraits(field.Nested.ResolvedFields())
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.Definitions() {
			blockTraits(block)
		}
	}
	if field.Plugin != nil {
		for _, tree := range field.Plugin.EmbeddedTrees {
			for _, c := range tree.Cases {
				for _, block := range c.Definitions() {
					blockTraits(block)
				}
			}
		}
	}
	return traits
}

// EachDefinitionChildList calls visit with each immediate child field list of
// field: a group or array's fields and each selectable block definition's
// fields, as DefinitionChildFields enters them. A shared definition passes the
// same slice at every placement, so callers can memoize by its first element.
func EachDefinitionChildList(field Field, visit func([]Field)) {
	eachDefinitionChildSource(field, visit)
}

func eachDefinitionChildSource(field Field, visit func([]Field)) {
	if field.Nested != nil {
		visit(field.Nested.ResolvedFields())
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.Definitions() {
			visit(block.ResolvedFields())
		}
	}
	if field.Plugin != nil {
		for _, tree := range field.Plugin.EmbeddedTrees {
			for _, treeCase := range tree.Cases {
				for _, block := range treeCase.Definitions() {
					visit(block.ResolvedFields())
				}
			}
		}
	}
}

// EmbeddedBlocks supplies virtual schema-only Blocks containers for each
// descriptor case. These do not represent stored fields or queryable columns.
// They allow schema consumers to reuse the ordinary variant catalog. Each
// container shares its case's selection: ResolvedTypes returns the case's
// placement views and Definitions its shared definition views.
func EmbeddedBlocks(field Field) []Field {
	if field.Plugin == nil {
		return nil
	}
	var result []Field
	for _, tree := range field.Plugin.EmbeddedTrees {
		for _, c := range tree.Cases {
			path, _ := query.ParsePath(field.Path.String() + "." + tree.Key + "." + c.TagValue)
			result = append(result, Field{ID: StableID(string(field.ID) + "_embedded_" + tree.Key + "_" + c.TagValue), Name: tree.Key + "_" + c.TagValue, Path: path, Type: FieldTypeBlocks, Category: FieldCategoryNested, Blocks: &BlocksField{BlockReferences: c.BlockReferences, bound: c.bound}})
		}
	}
	return result
}

// EmbeddedDefinitionBlocks is EmbeddedBlocks for definition traversal.
// Definitions and ResolvedTypes of the virtual containers are the case's.
func EmbeddedDefinitionBlocks(field Field) []Field { return EmbeddedBlocks(field) }

func cloneEmbeddedTrees(trees []EmbeddedTree) []EmbeddedTree {
	result := append([]EmbeddedTree(nil), trees...)
	for i := range result {
		result[i].Root = append([]string{}, trees[i].Root...)
		result[i].Cases = append([]EmbeddedTreeCase(nil), trees[i].Cases...)
		for j := range result[i].Cases {
			result[i].Cases[j].bound = nil
			result[i].Cases[j].BlockReferences = append([]string(nil), trees[i].Cases[j].BlockReferences...)
		}
	}
	return result
}

// ValidateEmbeddedMetadata validates descriptor structure before consumers can
// interpret it. Finite schema depth also rejects recursive/cyclic schema graphs.
// A block definition's fields are the same at every placement, so each block is
// checked at its first placement only; issue paths name that placement.
func ValidateEmbeddedMetadata(snapshot Snapshot) error {
	fieldTypes := map[string]PluginFieldType{}
	for _, p := range snapshot.Plugins {
		for _, fieldType := range p.FieldTypes {
			fieldTypes[fieldType.Key] = fieldType
		}
	}
	checkedBlocks := map[string]bool{}
	var walk func([]Field, string, int, int) error
	var walkChildren func(Field, string, int) error
	walk = func(fields []Field, path string, depth, offset int) error {
		if depth > 64 {
			return fmt.Errorf("embedded schema depth exceeds 64 at %s (recursive schemas are unsupported)", path)
		}
		for i, f := range fields {
			p := fmt.Sprintf("%s[%d]", path, offset+i)
			if f.Plugin != nil {
				if mapping, exists := fieldTypes[f.Plugin.Key]; exists {
					seen := map[string]bool{}
					for j, selector := range mapping.EmbeddedTypes {
						found := false
						for _, t := range f.Plugin.EmbeddedTrees {
							for _, c := range t.Cases {
								if selector == t.Key+"."+c.TagValue {
									found = true
								}
							}
						}
						if !found || seen[selector] {
							return fmt.Errorf("%s.plugin.embeddedTypes[%d]: selector %q must identify a distinct declared tree.case", p, j, selector)
						}
						seen[selector] = true
					}
				}
				if err := ValidateEmbeddedTrees(f.Plugin.EmbeddedTrees, p+".plugin.embeddedTrees"); err != nil {
					return err
				}
			}
			if err := walkChildren(f, p+".children", depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	// walkChildren visits ChildFields in order, keeping its child indexes while
	// skipping the fields of block definitions that were already checked.
	walkChildren = func(f Field, path string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("embedded schema depth exceeds 64 at %s (recursive schemas are unsupported)", path)
		}
		offset := 0
		walkBlocks := func(types []BlockType) error {
			for _, block := range types {
				if checkedBlocks[block.Slug] {
					offset += block.fieldCount()
					continue
				}
				fields := block.ResolvedFields()
				if err := walk(fields, path, depth, offset); err != nil {
					return err
				}
				checkedBlocks[block.Slug] = true
				offset += len(fields)
			}
			return nil
		}
		if f.Nested != nil {
			fields := f.Nested.ResolvedFields()
			if err := walk(fields, path, depth, offset); err != nil {
				return err
			}
			offset += len(fields)
		}
		if f.Blocks != nil {
			if err := walkBlocks(f.Blocks.Definitions()); err != nil {
				return err
			}
		}
		if f.Plugin != nil {
			for _, tree := range f.Plugin.EmbeddedTrees {
				for _, c := range tree.Cases {
					if err := walkBlocks(c.Definitions()); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for i, r := range snapshot.Collections {
		if err := walk(r.Fields, fmt.Sprintf("collections[%d].fields", i), 0, 0); err != nil {
			return err
		}
	}
	for i, r := range snapshot.Globals {
		if err := walk(r.Fields, fmt.Sprintf("globals[%d].fields", i), 0, 0); err != nil {
			return err
		}
	}
	return nil
}

// ValidateEmbeddedTrees checks the bounded descriptor language. Roots must be
// disjoint so the same structural node cannot be consumed by two descriptors.
func ValidateEmbeddedTrees(trees []EmbeddedTree, path string) error {
	keys := map[string]bool{}
	property := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`).MatchString
	for i, t := range trees {
		p := fmt.Sprintf("%s[%d]", path, i)
		if t.Version != EmbeddedTreeVersion {
			return fmt.Errorf("%s.version: unsupported embedded tree version %d", p, t.Version)
		}
		if !IsValidPluginKey(t.Key) || keys[t.Key] {
			return fmt.Errorf("%s.key: expected a distinct lowercase kebab-case key", p)
		}
		keys[t.Key] = true
		if !property(t.Children) || !property(t.Tag) || t.Children == t.Tag {
			return fmt.Errorf("%s: children and tag must be distinct literal property names", p)
		}
		if len(t.Root) > 32 {
			return fmt.Errorf("%s.root: exceeds 32 properties", p)
		}
		for j, s := range t.Root {
			if !property(s) {
				return fmt.Errorf("%s.root[%d]: expected a literal property name", p, j)
			}
		}
		for j := 0; j < i; j++ {
			a, b := trees[j].Root, t.Root
			n := len(a)
			if len(b) < n {
				n = len(b)
			}
			overlap := true
			for k := 0; k < n; k++ {
				if a[k] != b[k] {
					overlap = false
					break
				}
			}
			if overlap {
				return fmt.Errorf("%s.root: overlaps descriptor %d", p, j)
			}
		}
		if len(t.Cases) == 0 {
			return fmt.Errorf("%s.cases: at least one case is required", p)
		}
		tags := map[string]bool{}
		for j, c := range t.Cases {
			q := fmt.Sprintf("%s.cases[%d]", p, j)
			if !IsValidPluginKey(c.TagValue) || tags[c.TagValue] {
				return fmt.Errorf("%s.tagValue: expected a distinct lowercase kebab-case tag", q)
			}
			tags[c.TagValue] = true
			if !property(c.Payload) || !property(c.Discriminator) || !property(c.Identity) || c.Identity == c.Discriminator || c.Payload == t.Children || c.Payload == t.Tag {
				return fmt.Errorf("%s: payload, discriminator and identity must be valid nonconflicting literal properties", q)
			}
			// An empty allowlist reserves the envelope while admitting no payloads.
			variants := map[string]bool{}
			for k, b := range c.Definitions() {
				if !IsValidPluginKey(b.Slug) || variants[b.Slug] {
					return fmt.Errorf("%s.types[%d].key: expected distinct lowercase kebab-case variant", q, k)
				}
				variants[b.Slug] = true
				for _, f := range b.ResolvedFields() {
					if f.Name == c.Identity || f.Name == c.Discriminator {
						return fmt.Errorf("%s.types[%d].fields: %q conflicts with payload metadata", q, k, f.Name)
					}
				}
			}
		}
	}
	return nil
}

// EmbeddedTypeFields selects schema-only payload containers in generic argument order.
func EmbeddedTypeFields(field Field, selectors []string) []Field {
	var result []Field
	for _, selector := range selectors {
		for _, f := range EmbeddedDefinitionBlocks(field) {
			if strings.HasSuffix(f.Path.String(), "."+selector) {
				result = append(result, f)
				break
			}
		}
	}
	return result
}
