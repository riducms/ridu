package postgres

import (
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/schema"
)

// Planning compares JSONB content once per scope: each resource's own fields,
// and each block definition's own fields, never entering the definitions a
// scope's containers place. A change inside a definition applies at every
// placement of it, so a placement-dependent consequence (a dropped root, a
// retired owner) is decided by following the definitions each surviving
// resource root places through containers that keep them.

// definitionOwner keys a block definition's fields in a referenceShapeMapping,
// beside resource-owned fields. Stable IDs never contain a colon.
func definitionOwner(slug string) schema.StableID {
	return schema.StableID("block:" + slug)
}

// postgresScopeField is one field of a scope with its mapped identity, the
// scope's top-level field holding it, and its containers within the scope.
type postgresScopeField struct {
	field    schema.Field
	identity referenceShapeFieldIdentity
	root     schema.Field
	// chain records the containers above the field, as appendReferenceContainer does.
	chain []string
	// localized reports a localized container above the field within the scope.
	localized bool
}

// postgresScopeFields flattens a scope's own fields in pre-order. Before
// fields take the identities confirmed renames give them after the change.
func postgresScopeFields(fields []schema.Field, owner schema.StableID, mapping referenceShapeMapping, normalize bool) []postgresScopeField {
	var result []postgresScopeField
	var walk func([]schema.Field, *schema.Field, []string, bool)
	walk = func(fields []schema.Field, root *schema.Field, chain []string, localized bool) {
		for _, field := range fields {
			identity := referenceShapeFieldIdentity{ID: field.ID, Path: field.Path.String()}
			if normalize {
				identity = mapping.field(owner, field)
			}
			top := field
			if root != nil {
				top = *root
			}
			result = append(result, postgresScopeField{field: field, identity: identity, root: top, chain: chain, localized: localized})
			if field.Nested != nil {
				walk(field.Nested.ResolvedFields(), &top, appendReferenceContainer(chain, string(field.Type), field.Localized), localized || field.Localized)
			}
		}
	}
	walk(fields, nil, nil, false)
	return result
}

// postgresScopeEdge is one definition view a scope field places, keyed by the
// container's identity and its containers within the scope, so an edge kept
// after the change has the same key.
type postgresScopeEdge struct {
	key      string
	root     schema.Field
	view     blockgraph.Key
	embedded bool
	path     string
}

func (field postgresScopeField) edges(scopeLocalized bool) []postgresScopeEdge {
	localized := scopeLocalized || field.localized || field.field.Localized
	base := string(field.identity.ID) + "\x00" + field.identity.Path + "\x00" + strings.Join(field.chain, "/")
	var edges []postgresScopeEdge
	if field.field.Blocks != nil {
		for _, view := range field.field.Blocks.Definitions() {
			edges = append(edges, postgresScopeEdge{
				key:  base + "\x00" + appendReferenceContainer(nil, string(field.field.Type)+":"+view.Slug, field.field.Localized)[0],
				root: field.root, view: blockgraph.Key{Slug: view.Slug, Localized: localized}, path: field.identity.Path,
			})
		}
	}
	if field.field.Plugin != nil {
		for _, tree := range field.field.Plugin.EmbeddedTrees {
			for _, treeCase := range tree.Cases {
				for _, view := range treeCase.Definitions() {
					edges = append(edges, postgresScopeEdge{
						key:  base + "\x00" + appendReferenceContainer(nil, "embedded:"+tree.Key+"."+treeCase.TagValue+":"+view.Slug, field.field.Localized)[0],
						root: field.root, view: blockgraph.Key{Slug: view.Slug, Localized: localized}, embedded: true, path: field.identity.Path,
					})
				}
			}
		}
	}
	return edges
}

// postgresScopes compares two snapshots' block definitions for one analysis.
// Definition scopes are flattened once per view.
type postgresScopes struct {
	before, after *blockgraph.Graph
	mapping       referenceShapeMapping
	beforeFields  map[blockgraph.Key][]postgresScopeField
	afterFields   map[blockgraph.Key][]postgresScopeField
	kept          map[blockgraph.Key][]blockgraph.Key
	broken        map[blockgraph.Key][]postgresScopeEdge
}

func newPostgresScopes(before, after schema.Snapshot, mapping referenceShapeMapping) *postgresScopes {
	return &postgresScopes{
		before: blockgraph.New(before), after: blockgraph.New(after), mapping: mapping,
		beforeFields: make(map[blockgraph.Key][]postgresScopeField), afterFields: make(map[blockgraph.Key][]postgresScopeField),
		kept: make(map[blockgraph.Key][]blockgraph.Key), broken: make(map[blockgraph.Key][]postgresScopeEdge),
	}
}

// definition returns a definition view's own fields before (normalized
// through confirmed renames) or after the change, and whether it is placed.
func (scopes *postgresScopes) definition(key blockgraph.Key, before bool) ([]postgresScopeField, bool) {
	cache, graph := scopes.afterFields, scopes.after
	if before {
		cache, graph = scopes.beforeFields, scopes.before
	}
	if fields, done := cache[key]; done {
		return fields, true
	}
	view, placed := graph.View(key)
	if !placed {
		return nil, false
	}
	fields := postgresScopeFields(view.ResolvedFields(), definitionOwner(key.Slug), scopes.mapping, before)
	cache[key] = fields
	return fields, true
}

// follow splits the edges a scope places before the change into those kept
// after it and those it loses.
func follow(before, after []postgresScopeField, beforeLocalized, afterLocalized bool) (kept []postgresScopeEdge, broken []postgresScopeEdge) {
	keys := make(map[string]blockgraph.Key)
	for _, field := range after {
		for _, edge := range field.edges(afterLocalized) {
			keys[edge.key] = edge.view
		}
	}
	for _, field := range before {
		for _, edge := range field.edges(beforeLocalized) {
			if view, exists := keys[edge.key]; exists && view == edge.view {
				kept = append(kept, edge)
			} else {
				broken = append(broken, edge)
			}
		}
	}
	return kept, broken
}

// definitionEdges returns the definitions a definition view keeps placing
// after the change and the edges it loses. A view absent after the change
// keeps none.
func (scopes *postgresScopes) definitionEdges(key blockgraph.Key) ([]blockgraph.Key, []postgresScopeEdge) {
	if kept, done := scopes.kept[key]; done {
		return kept, scopes.broken[key]
	}
	before, _ := scopes.definition(key, true)
	after, placed := scopes.definition(key, false)
	var keptKeys []blockgraph.Key
	var broken []postgresScopeEdge
	if placed {
		keptEdges, lost := follow(before, after, key.Localized, key.Localized)
		for _, edge := range keptEdges {
			keptKeys = append(keptKeys, edge.view)
		}
		broken = lost
	} else {
		for _, field := range before {
			broken = append(broken, field.edges(key.Localized)...)
		}
	}
	scopes.kept[key], scopes.broken[key] = keptKeys, broken
	return keptKeys, broken
}

// survivingPlacements calls visit for each definition a surviving resource
// root places both before and after the change, through containers that keep
// it, and for each definition the root loses, at any depth. lost holds
// the definitions whose stored blocks the change leaves unreadable. Each
// definition is visited once per root.
func (scopes *postgresScopes) survivingPlacements(before, after []postgresScopeField, visit func(root schema.Field, key blockgraph.Key, lost bool)) {
	kept, broken := follow(before, after, false, false)
	type placement struct {
		root string
		key  blockgraph.Key
		lost bool
	}
	seen := make(map[placement]bool)
	var walk func(root schema.Field, key blockgraph.Key, lost bool)
	walk = func(root schema.Field, key blockgraph.Key, lost bool) {
		marker := placement{root: root.Name, key: key, lost: lost}
		if seen[marker] {
			return
		}
		seen[marker] = true
		visit(root, key, lost)
		if lost {
			for _, embedded := range []bool{false, true} {
				for _, child := range scopes.before.Selects(key, embedded) {
					walk(root, child, true)
				}
			}
			return
		}
		keptKeys, lostEdges := scopes.definitionEdges(key)
		for _, child := range keptKeys {
			walk(root, child, false)
		}
		for _, edge := range lostEdges {
			walk(root, edge.view, true)
		}
	}
	for _, edge := range kept {
		walk(edge.root, edge.view, false)
	}
	for _, edge := range broken {
		walk(edge.root, edge.view, true)
	}
}
