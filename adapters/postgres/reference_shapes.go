package postgres

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// postgresReferenceLeaf is one persisted reference of a scope: a relationship,
// an upload, or a declared plugin reference property. Its container chain and
// localization are relative to the scope.
type postgresReferenceLeaf struct {
	key           string
	path          string
	root          schema.Field
	rootID        schema.StableID
	containers    string
	leafLocalized bool
	localized     bool
	kind          string
	pluginKey     string
	targets       []schema.StableID
	hasMany       bool
	polymorphic   bool
}

func referenceLeaves(fields []postgresScopeField, collectionMapping atlasIdentityMap, owner schema.StableID, pluginTargets []schema.StableID, normalize bool) []postgresReferenceLeaf {
	var leaves []postgresReferenceLeaf
	for _, item := range fields {
		field := item.field
		rootID := item.root.ID
		if normalize {
			rootID = collectionMapping.field(owner, rootID)
		}
		base := postgresReferenceLeaf{
			key: string(item.identity.ID) + "\x00" + item.identity.Path + "\x00", path: item.identity.Path,
			root: item.root, rootID: rootID, containers: strings.Join(item.chain, "/"),
			leafLocalized: field.Localized, localized: item.localized || field.Localized,
		}
		if field.Relationship != nil {
			leaf := base
			leaf.kind = "relationship"
			leaf.hasMany, leaf.polymorphic = field.Relationship.HasMany, field.Relationship.Polymorphic
			leaf.targets = relationshipReferenceTargets(field.Relationship, collectionMapping, normalize)
			leaves = append(leaves, leaf)
		}
		if field.Upload != nil {
			leaf := base
			leaf.kind = "upload"
			leaf.hasMany = field.Upload.HasMany
			target := field.Upload.CollectionID
			if normalize {
				target = collectionMapping.collection(target)
			}
			leaf.targets = []schema.StableID{target}
			leaves = append(leaves, leaf)
		}
		if field.Plugin != nil {
			for _, key := range field.Plugin.ReferenceKeys {
				leaf := base
				leaf.key += key
				leaf.kind, leaf.pluginKey = "plugin", field.Plugin.Key
				leaf.targets = append([]schema.StableID(nil), pluginTargets...)
				leaves = append(leaves, leaf)
			}
		}
	}
	return leaves
}

// referenceLeafDecreased reports whether a reference loses semantics or
// targets. withRoot also compares the physical root column of a resource's
// own reference.
func referenceLeafDecreased(before, after postgresReferenceLeaf, exists, withRoot bool) (bool, string) {
	if !exists {
		return true, "remove or retype the field"
	}
	if before.kind != after.kind {
		return true, "change the reference kind"
	}
	if before.pluginKey != after.pluginKey {
		return true, "change the plugin reference contract"
	}
	if withRoot && (before.rootID != after.rootID || before.root.Localized != after.root.Localized) ||
		before.leafLocalized != after.leafLocalized || before.containers != after.containers {
		return true, "change the physical root or nested container shape"
	}
	if before.hasMany != after.hasMany || before.polymorphic != after.polymorphic {
		return true, "change relationship cardinality or polymorphic shape"
	}
	afterTargets := make(map[schema.StableID]bool, len(after.targets))
	for _, target := range after.targets {
		afterTargets[target] = true
	}
	for _, target := range before.targets {
		if !afterTargets[target] {
			return true, "remove or replace target " + string(target)
		}
	}
	return false, ""
}

// persistedReferenceShape is the placement of a decreased reference that its
// retirement contract inspects.
type persistedReferenceShape struct {
	OwnerID        schema.StableID
	OwnerVersioned bool
	RootID         schema.StableID
	RootLocalized  bool
	Targets        []schema.StableID
}

// unsafeReferenceShapeDecreases rejects the transition at which reference
// semantics disappear or narrow, rather than waiting for a later target
// removal. Otherwise a two-artifact sequence can hide values from the
// immediate retirement comparison while leaving current storage or version
// snapshots ready to reattach after the old shape and identity are restored.
//
// The one generic safe case is deliberately narrow: a target referenced by
// the old leaf is retired in this artifact, the complete physical root column
// is dropped, and every versioned owner is included in the same retirement's
// full-history purge. That is the contract already enforced by
// retire_resources; ordinary --allow-destructive is not a substitute.
//
// A resource's own references are compared directly. A block definition's
// references are compared once; each surviving resource root then applies the
// definition's decreases, and the loss of every definition it no longer
// places, to its own placements, where the retirement contract is decided.
func unsafeReferenceShapeDecreases(
	before, after schema.Snapshot,
	mapping atlasIdentityMap,
	fieldMapping referenceShapeMapping,
	physicalSteps []ridumigration.Operation,
	removed, purgeVersionOwners []schema.StableID,
) []ridumigration.Risk {
	removedSet := make(map[schema.StableID]bool, len(removed))
	for _, id := range removed {
		removedSet[id] = true
	}
	purgedOwners := make(map[schema.StableID]bool, len(purgeVersionOwners))
	for _, id := range purgeVersionOwners {
		purgedOwners[id] = true
	}
	var locales []schema.LocaleCode
	if before.Application.Localization != nil {
		locales = before.Application.Localization.LocaleCodes()
	}
	removedLocales := removedReferenceLocales(before, after)
	beforePluginTargets := pluginReferenceTargets(before.Collections, mapping, true)
	afterPluginTargets := pluginReferenceTargets(after.Collections, atlasIdentityMap{}, false)
	scopes := newPostgresScopes(before, after, fieldMapping)

	type definitionDecrease struct {
		leaf      postgresReferenceLeaf
		decreased bool
		reason    string
	}
	definitions := make(map[blockgraph.Key][]definitionDecrease)
	definition := func(key blockgraph.Key) []definitionDecrease {
		if decreases, done := definitions[key]; done {
			return decreases
		}
		beforeFields, _ := scopes.definition(key, true)
		afterFields, placed := scopes.definition(key, false)
		afterByKey := make(map[string]postgresReferenceLeaf)
		if placed {
			for _, leaf := range referenceLeaves(afterFields, atlasIdentityMap{}, definitionOwner(key.Slug), afterPluginTargets, false) {
				afterByKey[leaf.key] = leaf
			}
		}
		var decreases []definitionDecrease
		for _, leaf := range referenceLeaves(beforeFields, mapping, definitionOwner(key.Slug), beforePluginTargets, true) {
			next, exists := afterByKey[leaf.key]
			decreased, reason := referenceLeafDecreased(leaf, next, exists, false)
			decreases = append(decreases, definitionDecrease{leaf: leaf, decreased: decreased, reason: reason})
		}
		definitions[key] = decreases
		return decreases
	}

	risks := make(map[string]ridumigration.Risk)
	report := func(owner schema.StableID, versioned bool, rootID schema.StableID, root schema.Field, leaf postgresReferenceLeaf, usesLocalization, decreased bool, reason, address string) {
		if !decreased && usesLocalization && len(removedLocales) != 0 {
			decreased, reason = true, "remove application locale "+strings.Join(removedLocales, ", ")
		}
		if !decreased {
			return
		}
		shape := persistedReferenceShape{OwnerID: owner, OwnerVersioned: versioned, RootID: rootID, RootLocalized: root.Localized, Targets: leaf.targets}
		if referenceShapeDecreaseIsRetired(shape, removedSet, purgedOwners, physicalSteps, locales) {
			return
		}
		message := fmt.Sprintf("cannot %s for stored reference %s in a generic schema artifact; current values or version snapshots can become reachable again after the old reference shape and target identity are restored. Keep the reference shape, or retire a referenced target and drop the complete physical root in the same resource-retirement artifact so versioned owner history is purged; otherwise use a separately reviewed application-owned data migration", reason, address)
		risks[message] = ridumigration.Risk{Code: "RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE", Level: ridumigration.RiskDestructive, Message: message}
	}
	inspect := func(previous, next schema.Collection, nextOwnerID schema.StableID) {
		beforeFields := postgresScopeFields(previous.Fields, previous.ID, fieldMapping, true)
		afterFields := postgresScopeFields(next.Fields, nextOwnerID, referenceShapeMapping{}, false)
		afterByKey := make(map[string]postgresReferenceLeaf)
		for _, leaf := range referenceLeaves(afterFields, atlasIdentityMap{}, nextOwnerID, afterPluginTargets, false) {
			afterByKey[leaf.key] = leaf
		}
		versioned := previous.Versions != nil
		for _, leaf := range referenceLeaves(beforeFields, mapping, previous.ID, beforePluginTargets, true) {
			next, exists := afterByKey[leaf.key]
			decreased, reason := referenceLeafDecreased(leaf, next, exists, true)
			report(nextOwnerID, versioned, leaf.rootID, leaf.root, leaf, leaf.localized, decreased, reason, string(nextOwnerID)+"."+leaf.path)
		}
		scopes.survivingPlacements(beforeFields, afterFields, func(root schema.Field, key blockgraph.Key, lost bool) {
			rootID := mapping.field(previous.ID, root.ID)
			for _, item := range definition(key) {
				decreased, reason := item.decreased, item.reason
				if lost {
					decreased, reason = true, "remove or retype the field"
				}
				address := string(nextOwnerID) + ".**." + key.Slug + "." + item.leaf.path
				report(nextOwnerID, versioned, rootID, root, item.leaf, key.Localized || item.leaf.localized, decreased, reason, address)
			}
		})
	}
	afterCollections := make(map[schema.StableID]schema.Collection, len(after.Collections))
	for _, resource := range after.Collections {
		afterCollections[resource.ID] = resource
	}
	afterGlobals := make(map[schema.StableID]schema.Global, len(after.Globals))
	for _, resource := range after.Globals {
		afterGlobals[resource.ID] = resource
	}
	for _, previous := range before.Collections {
		nextID := mapping.collection(previous.ID)
		if next, exists := afterCollections[nextID]; exists {
			inspect(previous, next, nextID)
		}
	}
	for _, previous := range before.Globals {
		if next, exists := afterGlobals[previous.ID]; exists {
			inspect(previous, next, previous.ID)
		}
	}
	messages := make([]string, 0, len(risks))
	for message := range risks {
		messages = append(messages, message)
	}
	sort.Strings(messages)
	result := make([]ridumigration.Risk, len(messages))
	for index, message := range messages {
		result[index] = risks[message]
	}
	return result
}

// rootReferencesRemovedResource reports whether a top-level field's stored
// values can hold a reference to a removed resource: through its own
// references or those of any block definition it places, at any depth.
type removedReferenceRoots struct {
	graph         *blockgraph.Graph
	removed       map[schema.StableID]bool
	pluginTargets map[schema.StableID]bool
	definitions   map[blockgraph.Key]bool
}

func newRemovedReferenceRoots(before schema.Snapshot, removed, pluginTargets map[schema.StableID]bool) *removedReferenceRoots {
	return &removedReferenceRoots{graph: blockgraph.New(before), removed: removed, pluginTargets: pluginTargets, definitions: make(map[blockgraph.Key]bool)}
}

func (roots *removedReferenceRoots) root(field schema.Field) bool {
	if roots.ownReferencesRemoved([]schema.Field{field}) {
		return true
	}
	ordinary, embedded := blockgraph.FieldSelections(field, false)
	for key := range roots.graph.Closure(append(ordinary, embedded...), true) {
		references, done := roots.definitions[key]
		if !done {
			view, _ := roots.graph.View(key)
			references = roots.ownReferencesRemoved(view.ResolvedFields())
			roots.definitions[key] = references
		}
		if references {
			return true
		}
	}
	return false
}

// ownReferencesRemoved inspects fields and their groups and arrays only.
func (roots *removedReferenceRoots) ownReferencesRemoved(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Relationship != nil {
			if !field.Relationship.Polymorphic && roots.removed[field.Relationship.CollectionID] {
				return true
			}
			for _, target := range field.Relationship.Targets {
				if roots.removed[target.CollectionID] {
					return true
				}
			}
		}
		if field.Upload != nil && roots.removed[field.Upload.CollectionID] {
			return true
		}
		// A plugin reference key declares that matching JSON properties contain
		// collection slugs, but intentionally does not narrow the possible target
		// collections. Treat every collection in the source snapshot as a potential
		// target. If any collection is retired, the complete plugin root must leave
		// physical storage with it; otherwise current or historical nested values can
		// become live again after the slug/stable identity is reused.
		if field.Plugin != nil && len(field.Plugin.ReferenceKeys) != 0 {
			for target := range roots.pluginTargets {
				if roots.removed[target] {
					return true
				}
			}
		}
		if field.Nested != nil && roots.ownReferencesRemoved(field.Nested.ResolvedFields()) {
			return true
		}
	}
	return false
}

type referenceIndexTopology struct {
	Locales     []schema.LocaleCode
	Resources   []referenceIndexResource
	Definitions []referenceIndexDefinition
}

type referenceIndexResource struct {
	ID     schema.StableID
	Fields []referenceIndexField
}

// referenceIndexDefinition is the reference topology of one placed block
// definition view, shared by all of its placements.
type referenceIndexDefinition struct {
	Slug      string
	Localized bool
	Fields    []referenceIndexField
}

type referenceIndexField struct {
	ID           schema.StableID
	Name         string
	Type         schema.FieldType
	Localized    bool
	Relationship *referenceIndexRelationship
	Upload       *referenceIndexUpload
	Nested       []referenceIndexField
	// Blocks and EmbeddedBlocks name the selected definitions whose stored
	// blocks can hold references.
	Blocks         []string
	Embedded       []schema.EmbeddedTree
	EmbeddedBlocks []string
}

type referenceIndexRelationship struct {
	CollectionID   schema.StableID
	CollectionSlug schema.CollectionSlug
	Targets        []schema.RelationshipTarget
	HasMany        bool
	Polymorphic    bool
}

type referenceIndexUpload struct {
	CollectionID   schema.StableID
	CollectionSlug schema.CollectionSlug
	HasMany        bool
}

func referenceIndexTopologyChanged(before, after schema.Snapshot) bool {
	return !reflect.DeepEqual(referenceTopology(before), referenceTopology(after))
}

// referenceTopology describes the derived reference index of a schema: the
// references each resource's own fields and each placed block definition
// hold. A placement's references are its definition's, so two schemas index
// the same references exactly when their topologies are equal.
func referenceTopology(snapshot schema.Snapshot) referenceIndexTopology {
	topology := referenceIndexTopology{}
	if snapshot.Application.Localization != nil {
		topology.Locales = append([]schema.LocaleCode(nil), snapshot.Application.Localization.LocaleCodes()...)
	}
	graph := blockgraph.New(snapshot)
	own := make(map[blockgraph.Key]bool)
	for _, key := range graph.Keys() {
		view, _ := graph.View(key)
		own[key] = hasOwnReferences(view.ResolvedFields())
	}
	holds := func(key blockgraph.Key) bool {
		for reached := range graph.Closure([]blockgraph.Key{key}, true) {
			if own[reached] {
				return true
			}
		}
		return false
	}
	builder := referenceTopologyBuilder{holds: holds}
	for _, resource := range graph.Resources() {
		if fields := builder.fields(resource.Resource.Fields, false); len(fields) != 0 {
			topology.Resources = append(topology.Resources, referenceIndexResource{ID: resource.Resource.ID, Fields: fields})
		}
	}
	for _, key := range graph.Keys() {
		view, _ := graph.View(key)
		if fields := builder.fields(view.ResolvedFields(), key.Localized); len(fields) != 0 {
			topology.Definitions = append(topology.Definitions, referenceIndexDefinition{Slug: key.Slug, Localized: key.Localized, Fields: fields})
		}
	}
	return topology
}

func hasOwnReferences(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Relationship != nil || field.Upload != nil || field.Nested != nil && hasOwnReferences(field.Nested.ResolvedFields()) {
			return true
		}
	}
	return false
}

type referenceTopologyBuilder struct {
	holds func(blockgraph.Key) bool
}

func (builder referenceTopologyBuilder) fields(fields []schema.Field, localized bool) []referenceIndexField {
	var result []referenceIndexField
	for _, field := range fields {
		childLocalized := localized || field.Localized
		candidate := referenceIndexField{ID: field.ID, Name: field.Name, Type: field.Type, Localized: field.Localized}
		if field.Relationship != nil {
			candidate.Relationship = &referenceIndexRelationship{
				CollectionID: field.Relationship.CollectionID, CollectionSlug: field.Relationship.CollectionSlug,
				Targets: append([]schema.RelationshipTarget(nil), field.Relationship.Targets...),
				HasMany: field.Relationship.HasMany, Polymorphic: field.Relationship.Polymorphic,
			}
		}
		if field.Upload != nil {
			candidate.Upload = &referenceIndexUpload{
				CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug,
				HasMany: field.Upload.HasMany,
			}
		}
		if field.Nested != nil {
			candidate.Nested = builder.fields(field.Nested.ResolvedFields(), childLocalized)
		}
		if field.Blocks != nil {
			for _, view := range field.Blocks.Definitions() {
				if builder.holds(blockgraph.Key{Slug: view.Slug, Localized: childLocalized}) {
					candidate.Blocks = append(candidate.Blocks, view.Slug)
				}
			}
		}
		if field.Plugin != nil && len(field.Plugin.EmbeddedTrees) != 0 {
			for _, tree := range field.Plugin.EmbeddedTrees {
				for _, treeCase := range tree.Cases {
					for _, view := range treeCase.Definitions() {
						if builder.holds(blockgraph.Key{Slug: view.Slug, Localized: childLocalized}) {
							candidate.EmbeddedBlocks = append(candidate.EmbeddedBlocks, tree.Key+"."+treeCase.TagValue+"."+view.Slug)
						}
					}
				}
			}
			if len(candidate.EmbeddedBlocks) != 0 {
				candidate.Embedded = make([]schema.EmbeddedTree, len(field.Plugin.EmbeddedTrees))
				for ti, source := range field.Plugin.EmbeddedTrees {
					tree := source
					tree.Cases = make([]schema.EmbeddedTreeCase, len(source.Cases))
					for ci, sourceCase := range source.Cases {
						// The topology records the selected variants by slug.
						tree.Cases[ci] = schema.EmbeddedTreeCase{TagValue: sourceCase.TagValue, Payload: sourceCase.Payload, Discriminator: sourceCase.Discriminator, Identity: sourceCase.Identity, BlockReferences: sourceCase.BlockReferences}
					}
					candidate.Embedded[ti] = tree
				}
			}
		}
		if candidate.Relationship != nil || candidate.Upload != nil || len(candidate.Nested) != 0 || len(candidate.Blocks) != 0 || len(candidate.EmbeddedBlocks) != 0 {
			result = append(result, candidate)
		}
	}
	return result
}
