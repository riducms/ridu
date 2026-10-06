package operation

import (
	"slices"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// bindingTrie routes the canonical path segments of a document walk to the
// bindings located there. A block definition's fields have one node, shared by
// every container that selects the definition, so the trie follows block
// definitions, not placements, and one walk of a document finds every
// binding's locations.
type bindingTrie struct {
	children map[string]*bindingTrie
	// bindings are the indexes of the bindings located at this node.
	bindings []int
}

func (node *bindingTrie) child(key string, child *bindingTrie) {
	if child == nil {
		return
	}
	if node.children == nil {
		node.children = map[string]*bindingTrie{}
	}
	node.children[key] = child
}

// bindingTrieBuilder derives a collection's trie from its schema, entering
// each block definition once.
type bindingTrieBuilder struct {
	// own finds a resource's own bindings by canonical path; definitions
	// finds a block definition's bindings by slug and definition-relative path.
	own         map[string][]int
	definitions map[string]map[string][]int
	// shared memoizes each definition's node; nil when nothing is bound
	// within the definition.
	shared map[string]*bindingTrie
	built  map[string]bool
	// lasts memoizes each definition's last placements; see last.
	lasts map[string]map[int][]int
}

// newBindingTrie builds the trie of a collection's fields, and the
// placementOrder key of each binding's last placement in them, or nil when it
// has none. A collection narrowed to a block definition's fields (see
// Collection.narrowed) roots both at that definition.
func newBindingTrie(collection Collection) (*bindingTrie, [][]int) {
	builder := bindingTrieBuilder{own: map[string][]int{}, definitions: map[string]map[string][]int{}, shared: map[string]*bindingTrie{}, built: map[string]bool{}, lasts: map[string]map[int][]int{}}
	for index, binding := range collection.Bindings {
		path := binding.Field.Path.String()
		if binding.Block == "" {
			builder.own[path] = append(builder.own[path], index)
			continue
		}
		if builder.definitions[binding.Block] == nil {
			builder.definitions[binding.Block] = map[string][]int{}
		}
		builder.definitions[binding.Block][path] = append(builder.definitions[binding.Block][path], index)
	}
	definition, prefix := "", collection.placement.path()
	if collection.placement.shared {
		definition, prefix = collection.placement.canonical[len(collection.placement.canonical)-1], ""
	}
	lasts := make([][]int, len(collection.Bindings))
	for index, key := range builder.last(collection.Schema.Fields, prefix, definition) {
		lasts[index] = key
	}
	if root := builder.fields(collection.Schema.Fields, prefix, definition); root != nil {
		return root, lasts
	}
	return &bindingTrie{}, lasts
}

// last maps each binding placed among fields, whose paths extend prefix
// within definition, to the placementOrder key, relative to fields, of its
// last placement in schema order. Each block definition is examined once.
func (builder *bindingTrieBuilder) last(fields []schema.Field, prefix, definition string) map[int][]int {
	result := map[int][]int{}
	// Later fields, and a field's descendants after the field itself, come
	// later in a depth-first walk, so later assignments win.
	place := func(entries map[int][]int, key ...int) {
		for binding, rest := range entries {
			result[binding] = append(slices.Clone(key), rest...)
		}
	}
	for index := range fields {
		field := &fields[index]
		path := joinFieldPath(prefix, field.Name)
		bound := builder.own[path]
		if definition != "" {
			bound = builder.definitions[definition][path]
		}
		for _, binding := range bound {
			result[binding] = []int{index}
		}
		if field.Nested != nil && (field.Type == schema.FieldTypeGroup || field.Type == schema.FieldTypeArray) {
			place(builder.last(field.Nested.ResolvedFields(), path, definition), index)
		}
		if field.Type == schema.FieldTypeBlocks && field.Blocks != nil {
			for selected, block := range field.Blocks.Definitions() {
				place(builder.definitionLast(block), index, selected)
			}
		}
		if embedded.HasFields(*field) {
			for treeIndex, tree := range field.Plugin.EmbeddedTrees {
				for caseIndex, c := range tree.Cases {
					for selected, block := range c.Definitions() {
						place(builder.definitionLast(block), index, treeIndex, caseIndex, selected)
					}
				}
			}
		}
	}
	return result
}

// definitionLast is last for a block definition's fields, once per definition.
func (builder *bindingTrieBuilder) definitionLast(block schema.BlockType) map[int][]int {
	if result, known := builder.lasts[block.Slug]; known {
		return result
	}
	result := builder.last(block.ResolvedFields(), "", block.Slug)
	builder.lasts[block.Slug] = result
	return result
}

// fields builds the node of a field list whose paths extend prefix, within
// definition, or within the resource when definition is empty. It returns
// nil when nothing in the list is bound.
func (builder *bindingTrieBuilder) fields(fields []schema.Field, prefix, definition string) *bindingTrie {
	var node *bindingTrie
	for index := range fields {
		child := builder.field(&fields[index], joinFieldPath(prefix, fields[index].Name), definition)
		if child == nil {
			continue
		}
		if node == nil {
			node = &bindingTrie{}
		}
		node.child(fields[index].Name, child)
	}
	return node
}

func (builder *bindingTrieBuilder) field(field *schema.Field, path, definition string) *bindingTrie {
	node := &bindingTrie{bindings: builder.own[path]}
	if definition != "" {
		node.bindings = builder.definitions[definition][path]
	}
	switch field.Type {
	case schema.FieldTypeGroup, schema.FieldTypeArray:
		if field.Nested != nil {
			if nested := builder.fields(field.Nested.ResolvedFields(), path, definition); nested != nil {
				node.children = nested.children
			}
		}
	case schema.FieldTypeBlocks:
		if field.Blocks != nil {
			for _, block := range field.Blocks.Definitions() {
				node.child(block.Slug, builder.definition(block))
			}
		}
	}
	if embedded.HasFields(*field) {
		for _, tree := range field.Plugin.EmbeddedTrees {
			treeNode := &bindingTrie{}
			for _, c := range tree.Cases {
				caseNode := &bindingTrie{}
				for _, block := range c.Definitions() {
					caseNode.child(block.Slug, builder.definition(block))
				}
				if caseNode.children != nil {
					treeNode.child(c.TagValue, caseNode)
				}
			}
			if treeNode.children != nil {
				node.child(tree.Key, treeNode)
			}
		}
	}
	if len(node.bindings) == 0 && node.children == nil {
		return nil
	}
	return node
}

// definition returns the shared node of a block definition's fields. Block
// definitions form an acyclic graph, so each is built once.
func (builder *bindingTrieBuilder) definition(block schema.BlockType) *bindingTrie {
	if builder.built[block.Slug] {
		return builder.shared[block.Slug]
	}
	builder.built[block.Slug] = true
	node := builder.fields(block.ResolvedFields(), "", block.Slug)
	builder.shared[block.Slug] = node
	return node
}

// bindingRoots lists, for each binding, the root members of the trie whose
// values can hold its locations.
func bindingRoots(root *bindingTrie, bindings int) [][]string {
	result := make([][]string, bindings)
	reached := map[*bindingTrie][]int{}
	var reach func(*bindingTrie) []int
	reach = func(node *bindingTrie) []int {
		if indexes, known := reached[node]; known {
			return indexes
		}
		indexes := slices.Clone(node.bindings)
		for _, child := range node.children {
			indexes = append(indexes, reach(child)...)
		}
		slices.Sort(indexes)
		indexes = slices.Compact(indexes)
		reached[node] = indexes
		return indexes
	}
	names := make([]string, 0, len(root.children))
	for name := range root.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, index := range reach(root.children[name]) {
			result[index] = append(result[index], name)
		}
	}
	return result
}

// bindingLocations finds, for each binding that want selects, its locations
// in root, in document order. The walk follows the document only along the
// trie, so it visits a block definition's fields only where rows select it.
func bindingLocations(collection Collection, root store.Value, allLocales, includeMissing bool, want func(int) bool) [][]fieldLocation {
	plan := collection.runtimePlan()
	result := make([][]fieldLocation, len(collection.Bindings))
	if plan.bindingTrie == nil || len(plan.bindingTrie.children) == 0 {
		return result
	}
	walker := bindingWalker{allLocales: allLocales, includeMissing: includeMissing, want: want, result: result}
	walker.object(collection.Schema.Fields, root, plan.bindingTrie, rootPosition(collection))
	return result
}

// placementLocations finds the locations of the placement named by its
// canonical path, walking only that path.
func placementLocations(collection Collection, root store.Value, allLocales, includeMissing bool, canonical string) []fieldLocation {
	relative := canonical
	if prefix := collection.placement.path(); prefix != "" {
		relative = strings.TrimPrefix(canonical, prefix+".")
	}
	return collectFieldLocations(collection.Schema.Fields, root, strings.Split(relative, "."), rootPosition(collection), allLocales, includeMissing)
}

type bindingWalker struct {
	allLocales, includeMissing bool
	want                       func(int) bool
	result                     [][]fieldLocation
}

func (walker bindingWalker) add(node *bindingTrie, location fieldLocation) {
	for _, index := range node.bindings {
		if walker.want == nil || walker.want(index) {
			walker.result[index] = append(walker.result[index], location)
		}
	}
}

func (walker bindingWalker) object(fields []schema.Field, values store.Value, node *bindingTrie, at walkPosition) {
	for index := range fields {
		field := &fields[index]
		child := node.children[field.Name]
		if child == nil {
			continue
		}
		value, exists := values.Lookup(field.Name)
		member := at.member(field)
		if walker.allLocales && field.Localized {
			if !exists || value.Kind() != store.ValueObject {
				continue
			}
			locales := make([]string, 0, value.Len())
			for code := range value.Entries() {
				locales = append(locales, code)
			}
			sort.Strings(locales)
			for _, code := range locales {
				localized, translation := schema.LocaleCode(code), value.Get(code)
				translated := member.translation(localized)
				if len(child.bindings) != 0 {
					walker.add(child, translated.location(fields, translation, fieldLocationSiblings(fields, values, walker.allLocales, localized), at.runtime))
				}
				if len(child.children) != 0 {
					walker.descend(field, translation, child, translated)
				}
			}
			continue
		}
		if !exists && !walker.includeMissing {
			continue
		}
		if len(child.bindings) != 0 {
			walker.add(child, member.location(fields, value, fieldLocationSiblings(fields, values, walker.allLocales, at.locale), at.runtime))
		}
		if exists && len(child.children) != 0 {
			walker.descend(field, value, child, member)
		}
	}
}

// descend mirrors collectFieldDescendantLocations for every path in node.
func (walker bindingWalker) descend(field *schema.Field, value store.Value, node *bindingTrie, at walkPosition) {
	switch field.Type {
	case schema.FieldTypePlugin:
		bases := placementBases{}
		// Operation preflight rejects malformed envelopes.
		_ = embedded.Visit(*field, value, at.runtime, nil, func(occurrence embedded.ReadOccurrence) error {
			tree := node.children[occurrence.Tree.Key]
			if tree == nil {
				return nil
			}
			tag := tree.children[occurrence.Case.TagValue]
			if tag == nil {
				return nil
			}
			if variant := tag.children[occurrence.Type.Slug]; variant != nil {
				walker.object(occurrence.Fields, occurrence.Payload, variant, at.payload(occurrence, bases))
			}
			return nil
		})
	case schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject && field.Nested != nil {
			walker.object(field.Nested.ResolvedFields(), value, node, at)
		}
	case schema.FieldTypeArray:
		if field.Nested == nil {
			return
		}
		for row := range identifiedRowsInOrder(value, nil) {
			walker.object(field.Nested.ResolvedFields(), row.value, node, at.row(row.index, row.identity))
		}
	case schema.FieldTypeBlocks:
		if field.Blocks == nil {
			return
		}
		bases := placementBases{}
		for row := range identifiedRowsInOrder(value, field.Blocks) {
			variant := node.children[row.kind]
			if variant == nil {
				continue
			}
			block, _ := field.Blocks.Definition(row.kind)
			walker.object(block.ResolvedFields(), row.value, variant, at.row(row.index, row.identity).enterVia(bases, placementKey{2: row.kind}))
		}
	}
}

// bindingPriors indexes each selected binding's locations in root by identity,
// including missing values, so a hook can find a location's prior value. A
// zero root has no locations.
func bindingPriors(collection Collection, root store.Value, allLocales bool, want func(int) bool) []map[string]fieldLocation {
	result := make([]map[string]fieldLocation, len(collection.Bindings))
	if root.IsZero() {
		return result
	}
	for index, locations := range bindingLocations(collection, root, allLocales, true, want) {
		if len(locations) != 0 {
			result[index] = indexFieldLocations(locations)
		}
	}
	return result
}

// anyBinding reports whether want selects one of the collection's bindings.
func anyBinding(collection Collection, want func(int) bool) bool {
	for index := range collection.Bindings {
		if want(index) {
			return true
		}
	}
	return false
}

// lazyBindingLocations walks a root for every selected binding on first use.
type lazyBindingLocations struct {
	collection                 Collection
	root                       *lazyObject
	allLocales, includeMissing bool
	want                       func(int) bool
	locations                  [][]fieldLocation
}

func (lazy *lazyBindingLocations) get(index int) []fieldLocation {
	if lazy.locations == nil {
		lazy.locations = bindingLocations(lazy.collection, lazy.root.value(), lazy.allLocales, lazy.includeMissing, lazy.want)
	}
	return lazy.locations[index]
}

// placementGroup is one binding's locations at one placement, named by its
// canonical path.
type placementGroup struct {
	binding   int
	canonical string
	// indexes selects the group's locations from the binding's locations, in
	// their order; nil selects all of them.
	indexes []int
}

// locations returns the group's locations among each binding's locations.
func (group placementGroup) locations(locations [][]fieldLocation) []fieldLocation {
	all := locations[group.binding]
	if group.indexes == nil {
		return all
	}
	result := make([]fieldLocation, len(group.indexes))
	for position, index := range group.indexes {
		result[position] = all[index]
	}
	return result
}

// placementOrders orders placements as a depth-first walk of the schema
// visits them, deriving each placement's key once.
type placementOrders struct {
	collection Collection
	plan       *collectionPlan
	keys       map[string][]int
}

func newPlacementOrders(collection Collection) *placementOrders {
	return &placementOrders{collection: collection, plan: collection.runtimePlan()}
}

func (orders *placementOrders) key(group placementGroup) []int {
	// A resource's own binding has one placement, keyed once by the plan.
	if orders.collection.Bindings[group.binding].Block == "" {
		if key := orders.plan.bindingLast[group.binding]; key != nil {
			return key
		}
	}
	if key, known := orders.keys[group.canonical]; known {
		return key
	}
	if orders.keys == nil {
		orders.keys = map[string][]int{}
	}
	relative := group.canonical
	if prefix := orders.collection.placement.path(); prefix != "" {
		relative = strings.TrimPrefix(relative, prefix+".")
	}
	key := placementOrder(orders.collection.Schema.Fields, strings.Split(relative, "."))
	orders.keys[group.canonical] = key
	return key
}

func (orders *placementOrders) compare(left, right placementGroup) int {
	if left.canonical == right.canonical {
		return 0
	}
	return compareOrder(orders.key(left), orders.key(right))
}

// dispatchOrder groups each binding's locations by placement and orders the
// groups as their callbacks run: placements in schema order, depth first.
// Each group keeps its locations in the order given. A block definition's
// binding is located at many placements, so callbacks of different fields
// run in the order of their placements in the schema, not of the definitions
// that declare them.
func dispatchOrder(orders *placementOrders, locations [][]fieldLocation) []placementGroup {
	var groups []placementGroup
	for binding, list := range locations {
		if len(list) == 0 {
			continue
		}
		single := true
		for _, location := range list[1:] {
			if !location.samePlacement(list[0]) {
				single = false
				break
			}
		}
		if single {
			canonical := orders.plan.bindingPaths[binding]
			if list[0].shared() || canonical == "" {
				canonical = list[0].canonical()
			}
			groups = append(groups, placementGroup{binding: binding, canonical: canonical})
			continue
		}
		type placement struct {
			field *schema.Field
			base  string
		}
		placed := map[placement]int{}
		for index, location := range list {
			key := placement{field: location.field, base: location.base}
			group, known := placed[key]
			if !known {
				group = len(groups)
				placed[key] = group
				groups = append(groups, placementGroup{binding: binding, canonical: location.canonical()})
			}
			groups[group].indexes = append(groups[group].indexes, index)
		}
	}
	if !slices.IsSortedFunc(groups, orders.compare) {
		slices.SortStableFunc(groups, orders.compare)
	}
	return groups
}

// eachDispatched visits every location of the groups in order.
func eachDispatched(groups []placementGroup, locations [][]fieldLocation, visit func(binding int, location fieldLocation) error) error {
	for _, group := range groups {
		all := locations[group.binding]
		if group.indexes == nil {
			for _, location := range all {
				if err := visit(group.binding, location); err != nil {
					return err
				}
			}
			continue
		}
		for _, index := range group.indexes {
			if err := visit(group.binding, all[index]); err != nil {
				return err
			}
		}
	}
	return nil
}
