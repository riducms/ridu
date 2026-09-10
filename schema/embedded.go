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

type EmbeddedTreeCase struct {
	BlockReferences []string `json:"blockReferences,omitempty"`
	bound           *boundBlocks
	TagValue        string      `json:"tagValue"`
	Payload         string      `json:"payload"`
	Discriminator   string      `json:"discriminator"`
	Identity        string      `json:"identity"`
	Types           []BlockType `json:"types,omitempty"`
}

// ChildFields enumerates immediate ordinary schema children, including those
// declared by plugins. It never examines document values or plugin settings.
func ChildFields(field Field) []Field {
	var result []Field
	if field.Nested != nil {
		result = append(result, field.Nested.ResolvedFields()...)
	}
	if field.Blocks != nil {
		for _, block := range field.Blocks.ResolvedTypes() {
			result = append(result, block.ResolvedFields()...)
		}
	}
	if field.Plugin != nil {
		for _, tree := range field.Plugin.EmbeddedTrees {
			for _, c := range tree.Cases {
				for _, block := range c.ResolvedTypes() {
					result = append(result, block.ResolvedFields()...)
				}
			}
		}
	}
	return result
}

// EmbeddedBlocks supplies virtual schema-only Blocks containers for each
// descriptor case. These do not represent stored fields or queryable columns.
// They allow schema consumers to reuse the ordinary variant catalog.
func EmbeddedBlocks(field Field) []Field {
	if field.Plugin == nil {
		return nil
	}
	var result []Field
	for _, tree := range field.Plugin.EmbeddedTrees {
		for _, c := range tree.Cases {
			path, _ := query.ParsePath(field.Path.String() + "." + tree.Key + "." + c.TagValue)
			result = append(result, Field{ID: StableID(string(field.ID) + "_embedded_" + tree.Key + "_" + c.TagValue), Name: tree.Key + "_" + c.TagValue, Path: path, Type: FieldTypeBlocks, Category: FieldCategoryNested, Blocks: &BlocksField{Types: c.ResolvedTypes()}})
		}
	}
	return result
}

func cloneEmbeddedTrees(trees []EmbeddedTree) []EmbeddedTree {
	result := append([]EmbeddedTree(nil), trees...)
	for i := range result {
		result[i].Root = append([]string{}, trees[i].Root...)
		result[i].Cases = append([]EmbeddedTreeCase(nil), trees[i].Cases...)
		for j := range result[i].Cases {
			result[i].Cases[j].bound = nil
			result[i].Cases[j].BlockReferences = append([]string(nil), trees[i].Cases[j].BlockReferences...)
			result[i].Cases[j].Types = append([]BlockType(nil), trees[i].Cases[j].Types...)
			for k := range result[i].Cases[j].Types {
				b := &result[i].Cases[j].Types[k]
				b.bound = nil
				b.Fields = cloneFields(b.Fields)
				b.Labels.SingularTranslations = cloneStringMap(b.Labels.SingularTranslations)
				b.Labels.PluralTranslations = cloneStringMap(b.Labels.PluralTranslations)
				if b.Admin != nil {
					a := *b.Admin
					b.Admin = &a
				}
			}
		}
	}
	return result
}

// ValidateEmbeddedMetadata validates descriptor structure before consumers can
// interpret it. Finite schema depth also rejects recursive/cyclic schema graphs.
func ValidateEmbeddedMetadata(snapshot Snapshot) error {
	fieldTypes := map[string]PluginFieldType{}
	for _, p := range snapshot.Plugins {
		for _, fieldType := range p.FieldTypes {
			fieldTypes[fieldType.Key] = fieldType
		}
	}
	count := 0
	var walk func([]Field, string, int) error
	walk = func(fields []Field, path string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("embedded schema depth exceeds 64 at %s (recursive schemas are unsupported)", path)
		}
		for i, f := range fields {
			count++
			if count > 10000 {
				return fmt.Errorf("schema work budget exceeded at %s", path)
			}
			p := fmt.Sprintf("%s[%d]", path, i)
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
			if err := walk(ChildFields(f), p+".children", depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for i, r := range snapshot.Collections {
		if err := walk(r.Fields, fmt.Sprintf("collections[%d].fields", i), 0); err != nil {
			return err
		}
	}
	for i, r := range snapshot.Globals {
		if err := walk(r.Fields, fmt.Sprintf("globals[%d].fields", i), 0); err != nil {
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
			for k, b := range c.ResolvedTypes() {
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
		for _, f := range EmbeddedBlocks(field) {
			if strings.HasSuffix(f.Path.String(), "."+selector) {
				result = append(result, f)
				break
			}
		}
	}
	return result
}
