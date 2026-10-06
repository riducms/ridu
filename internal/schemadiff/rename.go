// Package schemadiff compares canonical manifests for migration intent that
// cannot be inferred safely from a database schema alone.
package schemadiff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/schema"
)

// RenameKind identifies the kind of schema address that may have changed.
type RenameKind string

const (
	RenameCollection RenameKind = "collection"
	RenameField      RenameKind = "field"
	// RenameBlockField renames a field of a block definition. The definition
	// is shared, so the rename applies at every placement of the block.
	RenameBlockField RenameKind = "block-field"
)

// FieldPair relates the same physical field before and after a collection rename.
type FieldPair struct {
	Before schema.Field
	After  schema.Field
}

// RenameCandidate is an unambiguous structural match that still requires
// explicit human confirmation before migration SQL may preserve its data.
type RenameCandidate struct {
	Kind RenameKind
	// BeforeCollection and AfterCollection hold a collection or field rename.
	// They are zero for a block field rename.
	BeforeCollection schema.Collection
	AfterCollection  schema.Collection
	// Block names the definition of a block field rename, whose fields have
	// definition-relative paths and IDs.
	Block       string
	BeforeField *schema.Field
	AfterField  *schema.Field
	// Fields contains a collection rename's own field pairs: its fields and
	// their groups' and arrays' fields. Block definitions are shared and keep
	// their fields under a collection rename.
	Fields []FieldPair
}

// RenameCandidates returns only one-to-one, shape-compatible collection and
// recursively nested field matches. Ambiguous matches are deliberately omitted.
func RenameCandidates(before, after schema.Manifest) []RenameCandidate {
	return RenameCandidatesWithCollectionMapping(before, after, nil)
}

// RenameCandidatesWithCollectionMapping normalizes references in the before
// manifest through simultaneously confirmed collection identities. It is used
// to replay immutable multi-rename artifacts where an owner and one of its
// relationship or upload targets move in the same transaction. Every mapped
// collection must still match its own unique structural rename candidate.
//
// A resource's fields are compared without entering the block definitions
// its containers select. Each definition both schemas place beneath a
// surviving resource is compared once, so its field renames are found once
// and the work follows definitions rather than placements.
func RenameCandidatesWithCollectionMapping(before, after schema.Manifest, collectionMapping map[schema.StableID]schema.StableID) []RenameCandidate {
	beforeSnapshot := before.Snapshot()
	afterSnapshot := after.Snapshot()
	beforeByID := collectionsByID(beforeSnapshot.Collections)
	afterByID := collectionsByID(afterSnapshot.Collections)

	removedByShape := make(map[string][]schema.Collection)
	addedByShape := make(map[string][]schema.Collection)
	for id, collection := range beforeByID {
		if _, exists := afterByID[id]; !exists {
			shape := collectionShape(collection, collectionMapping)
			removedByShape[shape] = append(removedByShape[shape], collection)
		}
	}
	for id, collection := range afterByID {
		if _, exists := beforeByID[id]; !exists {
			shape := collectionShape(collection, nil)
			addedByShape[shape] = append(addedByShape[shape], collection)
		}
	}

	var candidates []RenameCandidate
	renamedBefore := make(map[schema.StableID]bool)
	renamedAfter := make(map[schema.StableID]bool)
	survivors := make(map[schema.StableID]schema.StableID)
	for id, mapped := range collectionMapping {
		survivors[id] = mapped
	}
	for shape, removed := range removedByShape {
		added := addedByShape[shape]
		if len(removed) != 1 || len(added) != 1 || removed[0].Slug == added[0].Slug {
			continue
		}
		fields, complete := pairCollectionFields(removed[0], added[0], collectionMapping)
		if !complete {
			continue
		}
		candidates = append(candidates, RenameCandidate{
			Kind:             RenameCollection,
			BeforeCollection: removed[0],
			AfterCollection:  added[0],
			Fields:           fields,
		})
		renamedBefore[removed[0].ID] = true
		renamedAfter[added[0].ID] = true
		survivors[removed[0].ID] = added[0].ID
	}

	for id, beforeCollection := range beforeByID {
		afterCollection, exists := afterByID[id]
		if !exists || renamedBefore[id] || renamedAfter[id] {
			continue
		}
		candidates = append(candidates, fieldRenameCandidatesWithin(beforeCollection, afterCollection, "", beforeCollection.Fields, afterCollection.Fields, collectionMapping)...)
	}
	candidates = append(candidates, blockFieldRenameCandidates(beforeSnapshot, afterSnapshot, survivors, collectionMapping)...)

	sort.Slice(candidates, func(left, right int) bool {
		return candidateKey(candidates[left]) < candidateKey(candidates[right])
	})
	return candidates
}

// blockFieldRenameCandidates compares each block definition placed before and
// after the change beneath a surviving resource. Both of a definition's
// localization views declare the same names and shapes, so one is compared.
func blockFieldRenameCandidates(before, after schema.Snapshot, survivors, collectionMapping map[schema.StableID]schema.StableID) []RenameCandidate {
	previous, current := blockgraph.New(before), blockgraph.New(after)
	var candidates []RenameCandidate
	compared := make(map[string]bool)
	for _, key := range blockgraph.SortedKeys(blockgraph.Shared(previous, current, survivors, false)) {
		if compared[key.Slug] {
			continue
		}
		compared[key.Slug] = true
		beforeView, _ := previous.View(key)
		afterView, _ := current.View(key)
		candidates = append(candidates, fieldRenameCandidatesWithin(schema.Collection{}, schema.Collection{}, key.Slug, beforeView.ResolvedFields(), afterView.ResolvedFields(), collectionMapping)...)
	}
	return candidates
}

func collectionsByID(collections []schema.Collection) map[schema.StableID]schema.Collection {
	indexed := make(map[schema.StableID]schema.Collection, len(collections))
	for _, collection := range collections {
		indexed[collection.ID] = collection
	}
	return indexed
}

func candidateKey(candidate RenameCandidate) string {
	switch candidate.Kind {
	case RenameCollection:
		return "collection:" + string(candidate.BeforeCollection.Slug) + ":" + string(candidate.AfterCollection.Slug)
	case RenameBlockField:
		return "field:block:" + candidate.Block + ":" + candidate.BeforeField.Path.String() + ":" + candidate.AfterField.Path.String()
	}
	return "field:" + string(candidate.BeforeCollection.Slug) + ":" + candidate.BeforeField.Path.String() + ":" + candidate.AfterField.Path.String()
}

func collectionShape(collection schema.Collection, collectionMapping map[schema.StableID]schema.StableID) string {
	fieldShapes := make([]string, len(collection.Fields))
	for index, field := range collection.Fields {
		fieldShapes[index] = fieldShape(field, collection.ID, collectionMapping)
	}
	sort.Strings(fieldShapes)
	auth, _ := json.Marshal(collection.Auth)
	upload, _ := json.Marshal(collection.Upload)
	versions, _ := json.Marshal(collection.Versions)
	return fmt.Sprintf("auth=%s;upload=%s;versions=%s;fields=%s", auth, upload, versions, strings.Join(fieldShapes, "|"))
}

// fieldShape describes how a field's values are stored. A Blocks container is
// described by its selected definitions, which are shared and compared on
// their own, so a shape never expands the block graph.
func fieldShape(field schema.Field, self schema.StableID, collectionMapping map[schema.StableID]schema.StableID) string {
	var detail strings.Builder
	fmt.Fprintf(&detail, "%s|required=%t|unique=%t", field.Type, field.Required, field.Unique)
	if field.Default != nil {
		detail.WriteString("|default=" + *field.Default)
	}
	if field.List != nil {
		fmt.Fprintf(&detail, "|list-min=%d|list-max=%d", field.List.MinRows, field.List.MaxRows)
	}
	if field.Select != nil {
		fmt.Fprintf(&detail, "|many=%t", field.Select.HasMany)
		for _, value := range field.Select.DefaultValues {
			detail.WriteString("|select-default=" + value)
		}
		for _, option := range field.Select.Options {
			detail.WriteString("|option=" + option.Value)
		}
	}
	if field.Relationship != nil {
		fmt.Fprintf(&detail, "|many=%t|poly=%t", field.Relationship.HasMany, field.Relationship.Polymorphic)
		var targetIDs []string
		for _, target := range relationshipTargets(field.Relationship) {
			id := target.CollectionID
			if id == self && self != "" {
				targetIDs = append(targetIDs, "$self")
				continue
			}
			if mapped := collectionMapping[id]; mapped != "" {
				id = mapped
			}
			targetIDs = append(targetIDs, string(id))
		}
		sort.Strings(targetIDs)
		for _, id := range targetIDs {
			detail.WriteString("|target=" + id)
		}
	}
	if field.Upload != nil {
		id := field.Upload.CollectionID
		if id == self && self != "" {
			id = "$self"
		} else if mapped := collectionMapping[id]; mapped != "" {
			id = mapped
		}
		fmt.Fprintf(&detail, "|upload=%s|many=%t", id, field.Upload.HasMany)
	}
	if field.Nested != nil {
		children := make([]string, len(field.Nested.ResolvedFields()))
		for index, child := range field.Nested.ResolvedFields() {
			children[index] = fieldShape(child, self, collectionMapping)
		}
		sort.Strings(children)
		detail.WriteString("|nested={" + strings.Join(children, ",") + "}")
	}
	if field.Blocks != nil {
		slugs := append([]string(nil), field.Blocks.BlockReferences...)
		sort.Strings(slugs)
		detail.WriteString("|blocks=" + strings.Join(slugs, ","))
	}
	if field.Plugin != nil {
		detail.WriteString("|plugin=" + field.Plugin.Key + ":" + string(field.Plugin.Config))
		for _, tree := range field.Plugin.EmbeddedTrees {
			fmt.Fprintf(&detail, "|tree=%d:%q:%q:%q:%q", tree.Version, tree.Key, tree.Root, tree.Children, tree.Tag)
			for _, c := range tree.Cases {
				fmt.Fprintf(&detail, "|case=%q:%q:%q:%q", c.TagValue, c.Payload, c.Discriminator, c.Identity)
			}
		}
		for _, key := range field.Plugin.ReferenceKeys {
			detail.WriteString("|reference-key=" + key)
		}
	}
	for _, container := range schema.EmbeddedBlocks(field) {
		detail.WriteString("|embedded=" + container.Name + ":" + fieldShape(container, self, collectionMapping))
	}

	return detail.String()
}

func relationshipTargets(relationship *schema.RelationshipField) []schema.RelationshipTarget {
	if len(relationship.Targets) != 0 {
		targets := append([]schema.RelationshipTarget(nil), relationship.Targets...)
		sort.Slice(targets, func(left, right int) bool { return targets[left].CollectionID < targets[right].CollectionID })
		return targets
	}
	if relationship.CollectionID == "" {
		return nil
	}
	return []schema.RelationshipTarget{{CollectionID: relationship.CollectionID, CollectionSlug: relationship.CollectionSlug}}
}

func pairCollectionFields(before, after schema.Collection, collectionMapping map[schema.StableID]schema.StableID) ([]FieldPair, bool) {
	return pairFields(before.Fields, after.Fields, before.ID, after.ID, collectionMapping)
}

func pairFields(beforeFields, afterFields []schema.Field, beforeCollectionID, afterCollectionID schema.StableID, collectionMapping map[schema.StableID]schema.StableID) ([]FieldPair, bool) {
	pairs := make([]FieldPair, 0, len(beforeFields))
	usedAfter := make(map[int]bool)
	usedBefore := make(map[int]bool)

	for beforeIndex, beforeField := range beforeFields {
		for afterIndex, afterField := range afterFields {
			if usedAfter[afterIndex] || beforeField.Name != afterField.Name || fieldShape(beforeField, beforeCollectionID, collectionMapping) != fieldShape(afterField, afterCollectionID, nil) {
				continue
			}
			pairs = append(pairs, FieldPair{Before: beforeField, After: afterField})
			usedBefore[beforeIndex], usedAfter[afterIndex] = true, true
			break
		}
	}

	beforeByShape := make(map[string][]int)
	afterByShape := make(map[string][]int)
	for index, field := range beforeFields {
		if !usedBefore[index] {
			shape := fieldShape(field, beforeCollectionID, collectionMapping)
			beforeByShape[shape] = append(beforeByShape[shape], index)
		}
	}
	for index, field := range afterFields {
		if !usedAfter[index] {
			shape := fieldShape(field, afterCollectionID, nil)
			afterByShape[shape] = append(afterByShape[shape], index)
		}
	}
	for shape, beforeIndexes := range beforeByShape {
		afterIndexes := afterByShape[shape]
		if len(beforeIndexes) != 1 || len(afterIndexes) != 1 {
			continue
		}
		beforeIndex, afterIndex := beforeIndexes[0], afterIndexes[0]
		pairs = append(pairs, FieldPair{Before: beforeFields[beforeIndex], After: afterFields[afterIndex]})
		usedBefore[beforeIndex], usedAfter[afterIndex] = true, true
	}

	if len(usedBefore) != len(beforeFields) || len(usedAfter) != len(afterFields) {
		return nil, false
	}
	immediate := append([]FieldPair(nil), pairs...)
	for _, pair := range immediate {
		if pair.Before.Nested == nil || pair.After.Nested == nil {
			continue
		}
		children, complete := pairFields(pair.Before.Nested.ResolvedFields(), pair.After.Nested.ResolvedFields(), beforeCollectionID, afterCollectionID, collectionMapping)
		if !complete {
			return nil, false
		}
		pairs = append(pairs, children...)
	}
	sort.Slice(pairs, func(left, right int) bool { return pairs[left].Before.Name < pairs[right].Before.Name })
	return pairs, true
}

// fieldRenameCandidatesWithin compares one field list of a collection, or of
// the block definition named by block, and its groups and arrays.
func fieldRenameCandidatesWithin(before, after schema.Collection, block string, beforeFields, afterFields []schema.Field, collectionMapping map[schema.StableID]schema.StableID) []RenameCandidate {
	beforeByID := make(map[schema.StableID]schema.Field, len(beforeFields))
	afterByID := make(map[schema.StableID]schema.Field, len(afterFields))
	for _, field := range beforeFields {
		beforeByID[field.ID] = field
	}
	for _, field := range afterFields {
		afterByID[field.ID] = field
	}
	removedByShape := make(map[string][]schema.Field)
	addedByShape := make(map[string][]schema.Field)
	for id, field := range beforeByID {
		if _, exists := afterByID[id]; !exists {
			shape := fieldShape(field, before.ID, collectionMapping)
			removedByShape[shape] = append(removedByShape[shape], field)
		}
	}
	for id, field := range afterByID {
		if _, exists := beforeByID[id]; !exists {
			shape := fieldShape(field, after.ID, nil)
			addedByShape[shape] = append(addedByShape[shape], field)
		}
	}
	kind := RenameField
	if block != "" {
		kind = RenameBlockField
	}
	var candidates []RenameCandidate
	for shape, removed := range removedByShape {
		added := addedByShape[shape]
		if len(removed) != 1 || len(added) != 1 || removed[0].Name == added[0].Name {
			continue
		}
		beforeField, afterField := removed[0], added[0]
		candidates = append(candidates, RenameCandidate{
			Kind: kind, BeforeCollection: before, AfterCollection: after, Block: block,
			BeforeField: &beforeField, AfterField: &afterField,
		})
	}
	for id, beforeField := range beforeByID {
		afterField, exists := afterByID[id]
		if exists && beforeField.Nested != nil && afterField.Nested != nil {
			candidates = append(candidates, fieldRenameCandidatesWithin(before, after, block, beforeField.Nested.ResolvedFields(), afterField.Nested.ResolvedFields(), collectionMapping)...)
		}
	}
	return candidates
}
