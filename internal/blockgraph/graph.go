// Package blockgraph describes where a resolved schema places its block
// definitions. Migration planning compares each definition once and applies a
// change inside a definition at every placement of it; stored-value rewrites
// find every occurrence of a definition by walking stored values guided by the
// graph. Both therefore cost in proportion to definitions and stored data,
// never to the number of paths through the block graph.
package blockgraph

import (
	"sort"

	"github.com/riducms/ridu/schema"
)

// Key identifies one definition view: a definition and whether its placements
// lie beneath a localized ancestor. Such an ancestor stores the whole block once
// per locale, so the view clears the localization of the block's own fields.
// Every placement of a definition shares one of its two views.
type Key struct {
	Slug      string
	Localized bool
}

// Less orders keys by slug, the unlocalized view first.
func (key Key) Less(other Key) bool {
	if key.Slug != other.Slug {
		return key.Slug < other.Slug
	}
	return !key.Localized && other.Localized
}

// Graph records the definition views one schema snapshot places and the views
// each resource field and each view selects. Ordinary selections are Blocks
// containers, which store blocks as ordinary aggregate values; embedded
// selections are plugin embedded-tree cases, whose payloads the plugin owns.
type Graph struct {
	views     map[Key]schema.BlockType
	ordinary  map[Key][]Key
	embedded  map[Key][]Key
	resources []Resource
	byID      map[resourceKey]int
	closures  map[closureKey]map[Key]bool
}

// Resource is one collection or global and the views its fields select.
type Resource struct {
	Resource schema.Collection
	Global   bool
	Roots    []Root
}

// Root is one top-level field of a resource and the views its own subtree
// selects directly, without entering those views' fields.
type Root struct {
	Field    schema.Field
	Ordinary []Key
	Embedded []Key
}

type resourceKey struct {
	global bool
	id     schema.StableID
}

type closureKey struct {
	key      Key
	embedded bool
}

// New builds the graph of a snapshot. It visits every resource field and every
// placed definition view once.
func New(snapshot schema.Snapshot) *Graph {
	graph := &Graph{
		views:    make(map[Key]schema.BlockType),
		ordinary: make(map[Key][]Key),
		embedded: make(map[Key][]Key),
		byID:     make(map[resourceKey]int),
		closures: make(map[closureKey]map[Key]bool),
	}
	var pending []Key
	discover := func(selections []selection) {
		for _, selected := range selections {
			if _, known := graph.views[selected.key]; known {
				continue
			}
			graph.views[selected.key] = selected.view
			pending = append(pending, selected.key)
		}
	}
	for _, group := range []struct {
		global    bool
		resources []schema.Collection
	}{{false, snapshot.Collections}, {true, snapshot.Globals}} {
		for _, resource := range group.resources {
			node := Resource{Resource: resource, Global: group.global}
			for _, field := range resource.Fields {
				var selected []selection
				collectSelections(field, false, &selected)
				root := Root{Field: field}
				for _, item := range selected {
					if item.embedded {
						root.Embedded = appendKey(root.Embedded, item.key)
					} else {
						root.Ordinary = appendKey(root.Ordinary, item.key)
					}
				}
				node.Roots = append(node.Roots, root)
				discover(selected)
			}
			graph.byID[resourceKey{global: group.global, id: resource.ID}] = len(graph.resources)
			graph.resources = append(graph.resources, node)
		}
	}
	for len(pending) != 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		var selected []selection
		for _, field := range graph.views[key].ResolvedFields() {
			collectSelections(field, key.Localized, &selected)
		}
		for _, item := range selected {
			if item.embedded {
				graph.embedded[key] = appendKey(graph.embedded[key], item.key)
			} else {
				graph.ordinary[key] = appendKey(graph.ordinary[key], item.key)
			}
		}
		discover(selected)
	}
	return graph
}

type selection struct {
	key      Key
	view     schema.BlockType
	embedded bool
}

// collectSelections lists the views field and its own nested fields select.
// localized reports a localized ancestor; a localized field's descendants are
// stored once per locale, like those of a localized ancestor.
func collectSelections(field schema.Field, localized bool, selected *[]selection) {
	localized = localized || field.Localized
	if field.Nested != nil {
		for _, child := range field.Nested.ResolvedFields() {
			collectSelections(child, localized, selected)
		}
	}
	if field.Blocks != nil {
		for _, view := range field.Blocks.Definitions() {
			*selected = append(*selected, selection{key: Key{Slug: view.Slug, Localized: localized}, view: view})
		}
	}
	if field.Plugin != nil {
		for _, tree := range field.Plugin.EmbeddedTrees {
			for _, treeCase := range tree.Cases {
				for _, view := range treeCase.Definitions() {
					*selected = append(*selected, selection{key: Key{Slug: view.Slug, Localized: localized}, view: view, embedded: true})
				}
			}
		}
	}
}

// FieldSelections lists the views field and its own nested fields select
// directly, ordinary and embedded, without entering those views. localized
// reports a localized ancestor of field.
func FieldSelections(field schema.Field, localized bool) (ordinary, embedded []Key) {
	var selected []selection
	collectSelections(field, localized, &selected)
	for _, item := range selected {
		if item.embedded {
			embedded = appendKey(embedded, item.key)
		} else {
			ordinary = appendKey(ordinary, item.key)
		}
	}
	return ordinary, embedded
}

func appendKey(keys []Key, key Key) []Key {
	for _, existing := range keys {
		if existing == key {
			return keys
		}
	}
	return append(keys, key)
}

// Keys returns every placed view, ordinarily or embedded, in key order.
func (graph *Graph) Keys() []Key {
	keys := make([]Key, 0, len(graph.views))
	for key := range graph.views {
		keys = append(keys, key)
	}
	sortKeys(keys)
	return keys
}

func sortKeys(keys []Key) {
	sort.Slice(keys, func(left, right int) bool { return keys[left].Less(keys[right]) })
}

// View returns a placed definition view.
func (graph *Graph) View(key Key) (schema.BlockType, bool) {
	view, found := graph.views[key]
	return view, found
}

// Resources returns the snapshot's collections, then globals, in order.
func (graph *Graph) Resources() []Resource {
	return graph.resources
}

// Resource returns one collection or global by stable ID.
func (graph *Graph) Resource(id schema.StableID, global bool) (Resource, bool) {
	index, found := graph.byID[resourceKey{global: global, id: id}]
	if !found {
		return Resource{}, false
	}
	return graph.resources[index], true
}

// ResourceOf returns the graph's node of a resource of its snapshot.
func (graph *Graph) ResourceOf(resource schema.Collection) (Resource, bool) {
	for _, global := range []bool{false, true} {
		if node, found := graph.Resource(resource.ID, global); found && node.Resource.Slug == resource.Slug {
			return node, true
		}
	}
	return Resource{}, false
}

// Selects returns the views a view's own fields select directly.
func (graph *Graph) Selects(key Key, embedded bool) []Key {
	if embedded {
		return graph.embedded[key]
	}
	return graph.ordinary[key]
}

// Closure returns the views reachable from keys, including keys themselves:
// through ordinary containers only, or also through embedded cases. The result
// is shared and must not be changed.
func (graph *Graph) Closure(keys []Key, embedded bool) map[Key]bool {
	if len(keys) == 1 {
		return graph.closure(keys[0], embedded)
	}
	result := make(map[Key]bool)
	for _, key := range keys {
		for reached := range graph.closure(key, embedded) {
			result[reached] = true
		}
	}
	return result
}

func (graph *Graph) closure(key Key, embedded bool) map[Key]bool {
	memo := closureKey{key: key, embedded: embedded}
	if reached, done := graph.closures[memo]; done {
		return reached
	}
	reached := map[Key]bool{key: true}
	children := graph.ordinary[key]
	if embedded {
		children = append(append([]Key(nil), children...), graph.embedded[key]...)
	}
	for _, child := range children {
		for below := range graph.closure(child, embedded) {
			reached[below] = true
		}
	}
	graph.closures[memo] = reached
	return reached
}

// Placed returns the views a resource places, through ordinary containers only
// or also through embedded cases.
func (resource Resource) Placed(graph *Graph, embedded bool) map[Key]bool {
	var direct []Key
	for _, root := range resource.Roots {
		direct = append(direct, root.Ordinary...)
		if embedded {
			direct = append(direct, root.Embedded...)
		}
	}
	return graph.Closure(direct, embedded)
}

// EmbeddedPlacements returns the views a graph places inside embedded plugin
// payloads: those an embedded case selects, and every view beneath them.
func EmbeddedPlacements(graph *Graph) map[Key]bool {
	var direct []Key
	for _, resource := range graph.resources {
		for _, root := range resource.Roots {
			direct = append(direct, root.Embedded...)
		}
	}
	for key := range graph.views {
		direct = append(direct, graph.embedded[key]...)
	}
	return graph.Closure(direct, true)
}

// RootPlaces reports whether a root's subtree ordinarily places a definition
// whose slug is in slugs.
func (graph *Graph) RootPlaces(root Root, slugs map[string]bool) bool {
	for key := range graph.Closure(root.Ordinary, false) {
		if slugs[key.Slug] {
			return true
		}
	}
	return false
}

// Containing returns the slugs of the definitions that ordinarily place, at
// any depth, a definition whose slug is in slugs, together with slugs. A walk
// of stored values looking for blocks of slugs descends only into these.
func (graph *Graph) Containing(slugs map[string]bool) map[string]bool {
	result := make(map[string]bool, len(slugs))
	for slug := range slugs {
		result[slug] = true
	}
	for key := range graph.views {
		if result[key.Slug] {
			continue
		}
		for reached := range graph.closure(key, false) {
			if slugs[reached.Slug] {
				result[key.Slug] = true
				break
			}
		}
	}
	return result
}

// Depths returns, for each placed definition, the length of the longest chain
// of definitions placing it, ordinarily or embedded: 0 for a definition only
// resources place. A definition is deeper than every definition placing it,
// so ordering by depth visits a container's definition before its contents.
func (graph *Graph) Depths() map[string]int {
	parents := make(map[string][]string)
	for key := range graph.views {
		for _, embedded := range []bool{false, true} {
			for _, child := range graph.Selects(key, embedded) {
				parents[child.Slug] = append(parents[child.Slug], key.Slug)
			}
		}
	}
	depths := make(map[string]int, len(graph.views))
	var depth func(string) int
	depth = func(slug string) int {
		if value, done := depths[slug]; done {
			return value
		}
		value := 0
		for _, parent := range parents[slug] {
			if parentDepth := depth(parent) + 1; parentDepth > value {
				value = parentDepth
			}
		}
		depths[slug] = value
		return value
	}
	for key := range graph.views {
		depth(key.Slug)
	}
	return depths
}

// Survivors pairs each resource of before with the resource it remains in
// after: collections through mapping, which renames collection IDs, globals by
// ID. A resource without a survivor holds no stored values after the change.
func Survivors(before, after *Graph, mapping map[schema.StableID]schema.StableID) []Pair {
	var pairs []Pair
	for _, previous := range before.resources {
		id := previous.Resource.ID
		if mapped := mapping[id]; mapped != "" && !previous.Global {
			id = mapped
		}
		if next, found := after.Resource(id, previous.Global); found {
			pairs = append(pairs, Pair{Before: previous, After: next})
		}
	}
	return pairs
}

// Pair is one resource before and after a change.
type Pair struct {
	Before Resource
	After  Resource
}

// Shared returns the views both graphs place ordinarily, or also embedded,
// beneath a resource that survives the change: only there can blocks stored
// before the change remain stored after it. mapping renames collection IDs.
func Shared(before, after *Graph, mapping map[schema.StableID]schema.StableID, embedded bool) map[Key]bool {
	shared := make(map[Key]bool)
	for _, pair := range Survivors(before, after, mapping) {
		previous := pair.Before.Placed(before, embedded)
		for key := range pair.After.Placed(after, embedded) {
			if previous[key] {
				shared[key] = true
			}
		}
	}
	return shared
}

// SortedKeys returns a key set in key order.
func SortedKeys(keys map[Key]bool) []Key {
	result := make([]Key, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sortKeys(result)
	return result
}
