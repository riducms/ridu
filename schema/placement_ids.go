package schema

import (
	"fmt"
	"slices"
	"strings"
)

// validatePlacementIDs rejects a schema in which two field placements of one
// resource derive the same stable ID, or two block definitions' fields the same
// definition-relative ID.
//
// A placement's ID joins its resource ID and the kebab-cased segments of its
// canonical path with "-". Segments contain "-" themselves, so two different
// paths can derive one ID. Such paths agree up to some scope (a field list, a
// container's selected slugs, a plugin field's tree keys or a tree's case
// tags) and then continue through two siblings whose kebab forms begin with
// the same word. Whether they collide depends only on what lies below those
// siblings, which is the same at every placement of the enclosing definition.
// So each scope is checked once: in each resource, in each definition, and
// across the registry for definition-relative IDs. Two siblings are compared
// by walking both subtrees word by word in step, which follows only structure
// that matches, so the work is proportional to definitions, not placements.
func validatePlacementIDs(snapshot *Snapshot, defs map[string]BlockType) error {
	placements := newIDChecker(defs, false)
	resources := []struct {
		kind  string
		items []Collection
	}{{"collections", snapshot.Collections}, {"globals", snapshot.Globals}}
	for _, group := range resources {
		for index := range group.items {
			resource := &group.items[index]
			path := fmt.Sprintf("%s[%d].fields", group.kind, index)
			if collision, found := placements.scopes(fieldNodes(resource.Fields), nil); found {
				return blockGraphError("duplicate_field_id", path, fmt.Sprintf("fields %q and %q of %q both derive stable ID %q; rename one of them", collision.first, collision.second, resource.Slug, PlacementFieldID(resource.ID, collision.segments)))
			}
		}
	}
	for index := range snapshot.Blocks {
		block := &snapshot.Blocks[index]
		if collision, found := placements.scopes(fieldNodes(block.Fields), nil); found {
			return blockGraphError("duplicate_field_id", "blocks."+block.Slug+".fields", fmt.Sprintf("fields %q and %q of block %q derive the same stable ID wherever the block is placed (%q within the definition); rename one of them", collision.first, collision.second, block.Slug, PlacementFieldID(StableID("block-"+block.Slug), collision.segments)))
		}
	}
	// Definition-relative IDs share the "block-" namespace: they collide only
	// within a definition's own fields, which the registry scope compares.
	definitions := newIDChecker(defs, true)
	slugs := make([]idNode, len(snapshot.Blocks))
	for index, block := range snapshot.Blocks {
		slugs[index] = slugNode(block.Slug)
	}
	if collision, found := definitions.scope(slugs, nil); found {
		return blockGraphError("duplicate_field_id", "blocks", fmt.Sprintf("fields %q and %q of different blocks both derive definition ID %q; rename a field or block", collision.first, collision.second, PlacementFieldID("block", collision.segments)))
	}
	return nil
}

// idNode is one canonical path segment: a field, a selected block slug, an
// embedded tree key or an embedded case tag. Only a field has its own ID.
type idNode struct {
	key     idKey
	words   []string
	segment string
	field   bool
}

// idKey identifies a segment's subtree. A slug's subtree is its definition's
// fields wherever it is selected.
type idKey struct {
	field *Field
	slug  string
	tree  *EmbeddedTree
	cases *EmbeddedTreeCase
}

func newIDNode(key idKey, segment string, field bool) idNode {
	return idNode{key: key, words: strings.Split(string(appendIDSegment(nil, segment)), "-"), segment: segment, field: field}
}

func fieldNodes(fields []Field) []idNode {
	nodes := make([]idNode, len(fields))
	for index := range fields {
		nodes[index] = newIDNode(idKey{field: &fields[index]}, fields[index].Name, true)
	}
	return nodes
}

func slugNode(slug string) idNode { return newIDNode(idKey{slug: slug}, slug, false) }

type idState struct {
	a, b   idKey
	ia, ib int
}

type idCollision struct {
	first, second string
	segments      []string
}

type idChecker struct {
	definitions map[string]BlockType
	// own stops at selected definitions: definition-relative IDs cover only a
	// definition's own fields.
	own      bool
	children map[idKey][]idNode
	byWord   map[idKey]map[string][]idNode
	visited  map[idState]bool
}

func newIDChecker(definitions map[string]BlockType, own bool) *idChecker {
	return &idChecker{definitions: definitions, own: own, children: map[idKey][]idNode{}, byWord: map[idKey]map[string][]idNode{}, visited: map[idState]bool{}}
}

// scopes checks nodes as one scope and then each scope nested beneath them,
// without entering selected definitions, which are checked on their own.
func (c *idChecker) scopes(nodes []idNode, prefix []string) (idCollision, bool) {
	if collision, found := c.scope(nodes, prefix); found {
		return collision, true
	}
	for _, node := range nodes {
		if node.key.slug != "" {
			continue
		}
		if collision, found := c.scopes(c.childNodes(node), append(slices.Clone(prefix), node.segment)); found {
			return collision, true
		}
	}
	return idCollision{}, false
}

// scope compares the siblings that can begin the same ID.
func (c *idChecker) scope(nodes []idNode, prefix []string) (idCollision, bool) {
	groups := map[string][]idNode{}
	for _, node := range nodes {
		groups[node.words[0]] = append(groups[node.words[0]], node)
	}
	for _, node := range nodes {
		group := groups[node.words[0]]
		if len(group) < 2 || group[0].key != node.key {
			continue
		}
		for i := range group {
			for j := i + 1; j < len(group); j++ {
				first, second, found := c.overlap(group[i], group[j], 0, 0)
				if !found {
					continue
				}
				first = append(append(slices.Clone(prefix), group[i].segment), first...)
				second = append(append(slices.Clone(prefix), group[j].segment), second...)
				return idCollision{first: strings.Join(first, "."), second: strings.Join(second, "."), segments: first}, true
			}
		}
	}
	return idCollision{}, false
}

// overlap reports whether some field placement at or below a and one at or
// below b derive the same ID, given that the first ia words of a and ib words
// of b agree. It returns the colliding paths below a and b.
func (c *idChecker) overlap(a, b idNode, ia, ib int) ([]string, []string, bool) {
	for ia < len(a.words) && ib < len(b.words) {
		if a.words[ia] != b.words[ib] {
			return nil, nil, false
		}
		ia++
		ib++
	}
	state := idState{a: a.key, b: b.key, ia: ia, ib: ib}
	if c.visited[state] {
		return nil, nil, false
	}
	c.visited[state] = true
	switch aEnd, bEnd := ia == len(a.words), ib == len(b.words); {
	case aEnd && bEnd:
		if a.field && b.field {
			return []string{}, []string{}, true
		}
		for _, x := range c.childNodes(a) {
			for _, y := range c.wordChildren(b)[x.words[0]] {
				if px, py, found := c.overlap(x, y, 0, 0); found {
					return append([]string{x.segment}, px...), append([]string{y.segment}, py...), true
				}
			}
		}
	case aEnd:
		for _, x := range c.wordChildren(a)[b.words[ib]] {
			if px, py, found := c.overlap(x, b, 0, ib); found {
				return append([]string{x.segment}, px...), py, true
			}
		}
	default:
		for _, y := range c.wordChildren(b)[a.words[ia]] {
			if px, py, found := c.overlap(a, y, ia, 0); found {
				return px, append([]string{y.segment}, py...), true
			}
		}
	}
	return nil, nil, false
}

func (c *idChecker) childNodes(node idNode) []idNode {
	if cached, ok := c.children[node.key]; ok {
		return cached
	}
	var children []idNode
	selection := func(slugs []string) {
		for _, slug := range slugs {
			children = append(children, slugNode(slug))
		}
	}
	switch key := node.key; {
	case key.field != nil:
		if key.field.Nested != nil {
			children = append(children, fieldNodes(key.field.Nested.Fields)...)
		}
		if key.field.Blocks != nil && !c.own {
			selection(key.field.Blocks.BlockReferences)
		}
		if key.field.Plugin != nil && !c.own {
			for index := range key.field.Plugin.EmbeddedTrees {
				tree := &key.field.Plugin.EmbeddedTrees[index]
				children = append(children, newIDNode(idKey{tree: tree}, tree.Key, false))
			}
		}
	case key.tree != nil:
		for index := range key.tree.Cases {
			c := &key.tree.Cases[index]
			children = append(children, newIDNode(idKey{cases: c}, c.TagValue, false))
		}
	case key.cases != nil:
		selection(key.cases.BlockReferences)
	default:
		children = fieldNodes(c.definitions[key.slug].Fields)
	}
	c.children[node.key] = children
	return children
}

func (c *idChecker) wordChildren(node idNode) map[string][]idNode {
	if cached, ok := c.byWord[node.key]; ok {
		return cached
	}
	index := map[string][]idNode{}
	for _, child := range c.childNodes(node) {
		index[child.words[0]] = append(index[child.words[0]], child)
	}
	c.byWord[node.key] = index
	return index
}
