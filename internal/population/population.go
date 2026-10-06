// Package population owns schema-driven relationship population traversal.
//
// Population paths are authored field paths, not runtime array indexes. One
// path therefore applies to every matching row in an array and every matching
// block instance. Keeping this traversal shared prevents adapters from
// disagreeing about nested or localized relationship shapes.
package population

import (
	"sort"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	// MaxDepth bounds recursive target expansion. It intentionally matches the
	// public REST and GraphQL contract.
	MaxDepth = 5
	// MaxExplicitPaths bounds independently requested population branches. A
	// depth limit alone does not bound a deliberately wide schema or request.
	MaxExplicitPaths = 64
	// MaxExpandedPaths bounds the schema-driven relationship branches visited
	// while recursively populating targets.
	MaxExpandedPaths = 256
)

// LocaleSelection identifies the canonical localized values visible to one
// store request. All maps every configured locale; otherwise Chain is the
// requested locale followed by its effective fallbacks.
type LocaleSelection struct {
	All   bool
	Chain []schema.LocaleCode
}

// FieldAtPath resolves a canonical schema path through groups, arrays, and
// block discriminators. Repeated runtime rows do not appear in schema paths.
// It describes the field's configuration: a registered block's field is its
// shared definition, whose path and ID are definition-relative.
func FieldAtPath(fields []schema.Field, path query.Path) (schema.Field, bool) {
	return fieldAtSegments(fields, path.Segments())
}

func fieldAtSegments(fields []schema.Field, segments []string) (schema.Field, bool) {
	if len(segments) == 0 {
		return schema.Field{}, false
	}
	for _, field := range fields {
		if field.Name != segments[0] {
			continue
		}
		if len(segments) == 1 {
			return field, true
		}
		if embedded.HasFields(field) && len(segments) >= 5 {
			for _, tree := range field.Plugin.EmbeddedTrees {
				if tree.Key != segments[1] {
					continue
				}
				for _, c := range tree.Cases {
					if c.TagValue != segments[2] {
						continue
					}
					if variant, found := c.Definition(segments[3]); found {
						return fieldAtSegments(variant.ResolvedFields(), segments[4:])
					}
				}
			}
		}
		switch field.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if field.Nested != nil {
				return fieldAtSegments(field.Nested.ResolvedFields(), segments[1:])
			}
		case schema.FieldTypeBlocks:
			if field.Blocks == nil || len(segments) < 3 {
				return schema.Field{}, false
			}
			if block, found := field.Blocks.Definition(segments[1]); found {
				return fieldAtSegments(block.ResolvedFields(), segments[2:])
			}
		}
		return schema.Field{}, false
	}
	return schema.Field{}, false
}

// ReferenceFields returns every relationship and upload field placement in
// stable schema order, including fields beneath groups, arrays, and blocks.
// Each field's Path is its canonical placement path; a registered block's
// field otherwise describes its shared definition. The walk skips field lists
// without references, so it follows the reference placements rather than
// every placement of the schema.
func ReferenceFields(fields []schema.Field) []schema.Field {
	counts := referenceCounts{}
	var result []schema.Field
	schema.WalkPlacements(fields, func(segments []string, field schema.Field, shared bool) bool {
		if counts.field(field) == 0 {
			return false
		}
		if field.Relationship != nil || field.Upload != nil {
			if shared {
				field.Path, _ = query.NewPath(segments...)
			}
			result = append(result, field)
		}
		return true
	})
	return result
}

// ReferenceFieldCount is len(ReferenceFields(fields)), counted once per block
// definition rather than once per placement.
func ReferenceFieldCount(fields []schema.Field) int {
	return referenceCounts{}.list(fields)
}

// ReferenceTargets lists the collections that some reference field can target,
// in first-seen schema order, visiting each block definition once.
func ReferenceTargets(fields []schema.Field) []schema.RelationshipTarget {
	var result []schema.RelationshipTarget
	seen := map[schema.StableID]bool{}
	schema.WalkDefinitionFields(func(field schema.Field) bool {
		relationship := RelationshipDetails(field)
		if relationship == nil {
			return true
		}
		targets := relationship.Targets
		if !relationship.Polymorphic {
			targets = []schema.RelationshipTarget{{CollectionID: relationship.CollectionID, CollectionSlug: relationship.CollectionSlug}}
		}
		for _, target := range targets {
			if !seen[target.CollectionID] {
				seen[target.CollectionID] = true
				result = append(result, target)
			}
		}
		return true
	}, fields)
	return result
}

// referenceCounts memoizes reference placements per field list. A shared
// definition is one slice at every placement, so each is counted once.
type referenceCounts map[*schema.Field]int

func (counts referenceCounts) list(fields []schema.Field) int {
	if len(fields) == 0 {
		return 0
	}
	if count, known := counts[&fields[0]]; known {
		return count
	}
	total := 0
	for _, field := range fields {
		total += counts.field(field)
	}
	counts[&fields[0]] = total
	return total
}

func (counts referenceCounts) field(field schema.Field) int {
	total := 0
	if field.Relationship != nil || field.Upload != nil {
		total++
	}
	schema.EachDefinitionChildList(field, func(children []schema.Field) {
		total += counts.list(children)
	})
	return total
}

// DepthPopulations expands every relationship/upload field in a target
// collection to the remaining depth.
func DepthPopulations(collection schema.Collection, depth int) []query.Population {
	if depth <= 0 {
		return nil
	}
	fields := ReferenceFields(collection.Fields)
	result := make([]query.Population, len(fields))
	for index, field := range fields {
		result[index] = query.Population{Path: field.Path, Depth: depth}
	}
	return result
}

// MapAtPath applies transform to every visible runtime occurrence of the
// terminal schema field. The returned values are always detached. matched is
// false when the schema path or runtime shape did not contain an occurrence.
func MapAtPath(
	fields []schema.Field,
	values store.Values,
	path query.Path,
	locales LocaleSelection,
	transform func(schema.Field, store.Value) store.Value,
) (mapped store.Values, matched bool) {
	mapped = store.CloneValues(values)
	walker := pathWalker{locales: locales, transform: transform}
	name, updated, matched := walker.child(fields, func(name string) (store.Value, bool) {
		value, exists := mapped[name]
		return value, exists
	}, path.Segments())
	if matched {
		mapped[name] = updated
	}
	return mapped, matched
}

// VisitAtPath observes every visible runtime occurrence without changing the
// caller's values or materializing mutable copies of the traversed containers.
func VisitAtPath(fields []schema.Field, values store.Values, path query.Path, locales LocaleSelection, visit func(schema.Field, store.Value)) bool {
	walker := pathWalker{locales: locales, visit: visit}
	_, _, matched := walker.child(fields, func(name string) (store.Value, bool) {
		value, exists := values[name]
		return value, exists
	}, path.Segments())
	return matched
}

// Both operations use the same schema, locale and embedded admission rules.
// Only a mapping callback requires rebuilding ancestors of a matched field.
// A mapping callback may return an equal value; matching is deliberately not
// inferred from equality or from the identities of its immutable children.
type pathWalker struct {
	locales   LocaleSelection
	transform func(schema.Field, store.Value) store.Value
	visit     func(schema.Field, store.Value)
}

func (w pathWalker) field(field schema.Field, value store.Value, segments []string) (store.Value, bool) {
	if len(segments) == 0 || segments[0] != field.Name {
		return value, false
	}
	if !field.Localized {
		return w.fieldValue(field, value, segments)
	}
	if value.Kind() != store.ValueObject {
		return value, false
	}
	field.Localized = false
	var localized store.Values
	matched := false
	apply := func(locale string, candidate store.Value) {
		updated, found := w.fieldValue(field, candidate, segments)
		matched = matched || found
		if found && w.transform != nil {
			if localized == nil {
				localized, _ = value.CopyObject()
			}
			localized[locale] = updated
		}
	}
	if w.locales.All {
		for _, locale := range sortedKeys(value) {
			apply(locale, value.Get(locale))
		}
	} else {
		for index, locale := range w.locales.Chain {
			candidate, exists := value.Lookup(string(locale))
			if !exists || candidate.Kind() == store.ValueNull {
				continue
			}
			if text, stringValue := candidate.StringValue(); stringValue && text == "" && index < len(w.locales.Chain)-1 {
				continue
			}
			apply(string(locale), candidate)
			break
		}
	}
	if localized != nil {
		return store.Object(localized), matched
	}
	return value, matched
}

func (w pathWalker) fieldValue(field schema.Field, value store.Value, segments []string) (store.Value, bool) {
	if len(segments) == 1 {
		if w.transform != nil {
			return w.transform(field, value), true
		}
		w.visit(field, value)
		return value, true
	}
	if embedded.HasFields(field) && len(segments) >= 5 {
		matched := false
		apply := func(o embedded.ReadOccurrence) (store.Value, bool, error) {
			if o.Tree.Key != segments[1] || o.Case.TagValue != segments[2] || o.Type.Slug != segments[3] {
				return o.Payload, false, nil
			}
			payload, found := w.object(o.Fields, o.Payload, segments[4:])
			matched = matched || found
			return payload, found && w.transform != nil, nil
		}
		var updated store.Value
		var err error
		if w.transform == nil {
			updated = value
			err = embedded.Visit(field, value, field.Name, embedded.NewBudget(), func(o embedded.ReadOccurrence) error {
				_, _, err := apply(o)
				return err
			})
		} else {
			updated, err = embedded.TransformValue(field, value, field.Name, embedded.NewBudget(), apply)
		}
		if err != nil {
			return value, false
		}
		return updated, matched
	}

	switch field.Type {
	case schema.FieldTypeGroup:
		if field.Nested != nil {
			return w.object(field.Nested.ResolvedFields(), value, segments[1:])
		}
	case schema.FieldTypeArray:
		if field.Nested != nil {
			return w.list(field.Nested.ResolvedFields(), value, segments[1:], "", false)
		}
	case schema.FieldTypeBlocks:
		if field.Blocks == nil || len(segments) < 3 {
			return value, false
		}
		if block, found := field.Blocks.Definition(segments[1]); found {
			return w.list(block.ResolvedFields(), value, segments[2:], block.Slug, true)
		}
	}
	return value, false
}

func (w pathWalker) object(fields []schema.Field, value store.Value, segments []string) (store.Value, bool) {
	if value.Kind() != store.ValueObject {
		return value, false
	}
	name, updated, matched := w.child(fields, value.Lookup, segments)
	if matched && w.transform != nil {
		object, _ := value.CopyObject()
		object[name] = updated
		return store.Object(object), true
	}
	return value, matched
}

func (w pathWalker) list(fields []schema.Field, value store.Value, segments []string, blockKey string, blocks bool) (store.Value, bool) {
	matched := false
	updated, _ := mapList(value, func(item store.Value) (store.Value, bool) {
		if blocks {
			actual, _ := item.Get("blockType").StringValue()
			if actual != blockKey {
				return item, false
			}
		}
		updated, found := w.object(fields, item, segments)
		matched = matched || found
		return updated, found && w.transform != nil
	})
	return updated, matched
}

func (w pathWalker) child(fields []schema.Field, lookup func(string) (store.Value, bool), segments []string) (string, store.Value, bool) {
	if len(segments) == 0 {
		return "", store.Value{}, false
	}
	for _, child := range fields {
		if child.Name != segments[0] {
			continue
		}
		value, exists := lookup(child.Name)
		if !exists {
			return child.Name, value, false
		}
		updated, matched := w.field(child, value, segments)
		return child.Name, updated, matched
	}
	return "", store.Value{}, false
}

// MapPopulatedDocuments recursively maps already-populated relationship and
// upload documents in response-shaped values. Single-locale responses have
// localized fields projected to their scalar/container value; all-locale
// responses retain locale maps. Callback documents are detached. When a
// callback runs, the result is a new root map; immutable branches without
// populated documents are reused. Without populated documents the input root
// map itself is returned, so callers that need a detached map must copy it.
func MapPopulatedDocuments(
	fields []schema.Field,
	values store.Values,
	allLocales bool,
	transform func(schema.StableID, schema.LocaleCode, store.Document) store.Document,
) store.Values {
	walker := populatedWalker{allLocales: allLocales, transform: transform}
	var result store.Values
	for _, field := range fields {
		if value, exists := values[field.Name]; exists {
			if updated, changed := walker.field(field, value, ""); changed {
				if result == nil {
					result = store.CloneValues(values)
				}
				result[field.Name] = updated
			}
		}
	}
	if result == nil {
		return values
	}
	return result
}

type populatedWalker struct {
	allLocales bool
	transform  func(schema.StableID, schema.LocaleCode, store.Document) store.Document
}

func (w populatedWalker) object(fields []schema.Field, value store.Value, inheritedLocale schema.LocaleCode) (store.Value, bool) {
	if value.Kind() != store.ValueObject {
		return value, false
	}
	var object store.Values
	for _, field := range fields {
		candidate, exists := value.Lookup(field.Name)
		if object != nil {
			candidate, exists = object[field.Name]
		}
		if !exists {
			continue
		}
		if updated, changed := w.field(field, candidate, inheritedLocale); changed {
			if object == nil {
				object, _ = value.CopyObject()
			}
			object[field.Name] = updated
		}
	}
	if object != nil {
		return store.Object(object), true
	}
	return value, false
}

func (w populatedWalker) field(field schema.Field, value store.Value, inheritedLocale schema.LocaleCode) (store.Value, bool) {
	if field.Localized && w.allLocales {
		if value.Kind() != store.ValueObject {
			return value, false
		}
		field.Localized = false
		var localized store.Values
		for locale, candidate := range value.Entries() {
			if updated, changed := w.fieldValue(field, candidate, schema.LocaleCode(locale)); changed {
				if localized == nil {
					localized, _ = value.CopyObject()
				}
				localized[locale] = updated
			}
		}
		if localized != nil {
			return store.Object(localized), true
		}
		return value, false
	}
	field.Localized = false
	return w.fieldValue(field, value, inheritedLocale)
}

func (w populatedWalker) fieldValue(field schema.Field, value store.Value, inheritedLocale schema.LocaleCode) (store.Value, bool) {
	if embedded.HasFields(field) {
		changed := false
		updated, err := embedded.TransformValue(field, value, field.Name, embedded.NewBudget(), func(o embedded.ReadOccurrence) (store.Value, bool, error) {
			payload, found := w.object(o.Fields, o.Payload, inheritedLocale)
			changed = changed || found
			return payload, found, nil
		})
		if err != nil {
			return value, false
		}
		return updated, changed
	}

	switch field.Type {
	case schema.FieldTypeRelationship, schema.FieldTypeUpload, schema.FieldTypeJoin:
		return w.reference(field, value, inheritedLocale)
	case schema.FieldTypeGroup:
		if field.Nested != nil {
			return w.object(field.Nested.ResolvedFields(), value, inheritedLocale)
		}
	case schema.FieldTypeArray, schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList || (field.Type == schema.FieldTypeArray && field.Nested == nil) || (field.Type == schema.FieldTypeBlocks && field.Blocks == nil) {
			return value, false
		}
		return mapList(value, func(item store.Value) (store.Value, bool) {
			if field.Type == schema.FieldTypeArray {
				return w.object(field.Nested.ResolvedFields(), item, inheritedLocale)
			}
			blockType, _ := item.Get("blockType").StringValue()
			if block, found := field.Blocks.Definition(blockType); found {
				return w.object(block.ResolvedFields(), item, inheritedLocale)
			}
			return item, false
		})
	}
	return value, false
}

func (w populatedWalker) reference(field schema.Field, value store.Value, locale schema.LocaleCode) (store.Value, bool) {
	// This traversal only reads the schema. RelationshipDetails still returns a
	// defensive copy at its caller-facing boundary.
	relationship := field.Relationship
	var synthesized schema.RelationshipField
	if relationship == nil && field.Upload != nil {
		synthesized = schema.RelationshipField{CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug, HasMany: field.Upload.HasMany}
		relationship = &synthesized
	}
	if field.Join != nil {
		synthesized = schema.RelationshipField{CollectionID: field.Join.CollectionID, CollectionSlug: field.Join.CollectionSlug, HasMany: true}
		relationship = &synthesized
	}
	if relationship == nil {
		return value, false
	}
	mapOne := func(reference store.Value) (store.Value, bool) {
		if !relationship.Polymorphic {
			if document, populated := reference.CopyDocument(); populated {
				return store.Populated(w.transform(relationship.CollectionID, locale, document)), true
			}
			return reference, false
		}
		slug, _ := reference.Get("relationTo").StringValue()
		if reference.Get("id").Kind() == store.ValueDocument {
			for _, target := range relationship.Targets {
				if string(target.CollectionSlug) == slug {
					document, _ := reference.Get("id").CopyDocument()
					updated := store.Populated(w.transform(target.CollectionID, locale, document))
					object, _ := reference.CopyObject()
					object["id"] = updated
					return store.Object(object), true
				}
			}
		}
		return reference, false
	}
	if !relationship.HasMany {
		return mapOne(value)
	}
	return mapList(value, mapOne)
}

// mapList creates one mutable working slice only when a child changes. It
// batches dense edits before constructing the immutable result once.
func mapList(value store.Value, transform func(store.Value) (store.Value, bool)) (store.Value, bool) {
	if value.Kind() != store.ValueList {
		return value, false
	}
	var items []store.Value
	index := 0
	for item := range value.Elements() {
		if updated, changed := transform(item); changed {
			if items == nil {
				items, _ = value.CopyList()
			}
			items[index] = updated
		}
		index++
	}
	if items != nil {
		return store.List(items...), true
	}
	return value, false
}

// RelationshipDetails presents uploads through the same finite target shape
// as relationships.
func RelationshipDetails(field schema.Field) *schema.RelationshipField {
	return relationshipDetails(field)
}

func relationshipDetails(field schema.Field) *schema.RelationshipField {
	if field.Relationship != nil {
		copy := *field.Relationship
		copy.Targets = append([]schema.RelationshipTarget(nil), field.Relationship.Targets...)
		return &copy
	}
	if field.Upload != nil {
		return &schema.RelationshipField{
			CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug,
			HasMany: field.Upload.HasMany,
		}
	}
	return nil
}

func sortedKeys(value store.Value) []string {
	keys := make([]string, 0, value.Len())
	for key := range value.Entries() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
