// Package population owns schema-driven relationship population traversal.
//
// Population paths are authored field paths, not runtime array indexes. One
// path therefore applies to every matching row in an array and every matching
// block instance. Keeping this traversal shared prevents adapters from
// disagreeing about nested or localized relationship shapes.
package population

import (
	"sort"

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
		switch field.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if field.Nested != nil {
				return fieldAtSegments(field.Nested.Fields, segments[1:])
			}
		case schema.FieldTypeBlocks:
			if field.Blocks == nil || len(segments) < 3 {
				return schema.Field{}, false
			}
			for _, block := range field.Blocks.Types {
				if block.Key == segments[1] {
					return fieldAtSegments(block.Fields, segments[2:])
				}
			}
		}
		return schema.Field{}, false
	}
	return schema.Field{}, false
}

// ReferenceFields returns every relationship and upload field in stable
// schema order, including fields beneath groups, arrays, and blocks.
func ReferenceFields(fields []schema.Field) []schema.Field {
	var result []schema.Field
	collectReferenceFields(fields, &result)
	return result
}

func collectReferenceFields(fields []schema.Field, result *[]schema.Field) {
	for _, field := range fields {
		if field.Relationship != nil || field.Upload != nil {
			*result = append(*result, field)
		}
		if field.Nested != nil {
			collectReferenceFields(field.Nested.Fields, result)
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.Types {
				collectReferenceFields(block.Fields, result)
			}
		}
	}
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
	segments := path.Segments()
	if len(segments) == 0 {
		return mapped, false
	}
	for _, field := range fields {
		if field.Name != segments[0] {
			continue
		}
		value, exists := mapped[field.Name]
		if !exists {
			return mapped, false
		}
		updated, found := mapField(field, value, segments, locales, transform)
		if found {
			mapped[field.Name] = updated
		}
		return mapped, found
	}
	return mapped, false
}

// VisitAtPath observes every visible runtime occurrence without changing the
// caller's values.
func VisitAtPath(fields []schema.Field, values store.Values, path query.Path, locales LocaleSelection, visit func(schema.Field, store.Value)) bool {
	_, matched := MapAtPath(fields, values, path, locales, func(field schema.Field, value store.Value) store.Value {
		visit(field, value)
		return value
	})
	return matched
}

func mapField(
	field schema.Field,
	value store.Value,
	segments []string,
	locales LocaleSelection,
	transform func(schema.Field, store.Value) store.Value,
) (store.Value, bool) {
	if len(segments) == 0 || segments[0] != field.Name {
		return value, false
	}
	if field.Localized {
		localized, valid := value.ObjectValue()
		if !valid {
			return value, false
		}
		field.Localized = false
		matched := false
		if locales.All {
			keys := sortedKeys(localized)
			for _, locale := range keys {
				updated, found := mapFieldValue(field, localized[locale], segments, locales, transform)
				if found {
					localized[locale] = updated
					matched = true
				}
			}
		} else {
			for index, locale := range locales.Chain {
				candidate, exists := localized[string(locale)]
				if !exists || candidate.Kind() == store.ValueNull {
					continue
				}
				if text, stringValue := candidate.StringValue(); stringValue && text == "" && index < len(locales.Chain)-1 {
					continue
				}
				updated, found := mapFieldValue(field, candidate, segments, locales, transform)
				if found {
					localized[string(locale)] = updated
					matched = true
				}
				break
			}
		}
		if matched {
			return store.Object(localized), true
		}
		return value, false
	}
	return mapFieldValue(field, value, segments, locales, transform)
}

func mapFieldValue(
	field schema.Field,
	value store.Value,
	segments []string,
	locales LocaleSelection,
	transform func(schema.Field, store.Value) store.Value,
) (store.Value, bool) {
	if len(segments) == 1 {
		return transform(field, value), true
	}
	switch field.Type {
	case schema.FieldTypeGroup:
		object, valid := value.ObjectValue()
		if !valid || field.Nested == nil {
			return value, false
		}
		updated, matched := mapChild(field.Nested.Fields, object, segments[1:], locales, transform)
		if matched {
			return store.Object(updated), true
		}
	case schema.FieldTypeArray:
		items, valid := value.Values()
		if !valid || field.Nested == nil {
			return value, false
		}
		matched := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			updated, found := mapChild(field.Nested.Fields, object, segments[1:], locales, transform)
			if found {
				items[index] = store.Object(updated)
				matched = true
			}
		}
		if matched {
			return store.List(items...), true
		}
	case schema.FieldTypeBlocks:
		items, valid := value.Values()
		if !valid || field.Blocks == nil || len(segments) < 3 {
			return value, false
		}
		blockKey := segments[1]
		var blockFields []schema.Field
		for _, block := range field.Blocks.Types {
			if block.Key == blockKey {
				blockFields = block.Fields
				break
			}
		}
		if blockFields == nil {
			return value, false
		}
		matched := false
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			actualType, _ := object["blockType"].StringValue()
			if actualType != blockKey {
				continue
			}
			updated, found := mapChild(blockFields, object, segments[2:], locales, transform)
			if found {
				items[index] = store.Object(updated)
				matched = true
			}
		}
		if matched {
			return store.List(items...), true
		}
	}
	return value, false
}

func mapChild(
	fields []schema.Field,
	values store.Values,
	segments []string,
	locales LocaleSelection,
	transform func(schema.Field, store.Value) store.Value,
) (store.Values, bool) {
	result := store.CloneValues(values)
	if len(segments) == 0 {
		return result, false
	}
	for _, child := range fields {
		if child.Name != segments[0] {
			continue
		}
		value, exists := result[child.Name]
		if !exists {
			return result, false
		}
		updated, matched := mapField(child, value, segments, locales, transform)
		if matched {
			result[child.Name] = updated
		}
		return result, matched
	}
	return result, false
}

// MapPopulatedDocuments recursively maps already-populated relationship and
// upload documents in response-shaped values. Single-locale responses have
// localized fields projected to their scalar/container value; all-locale
// responses retain locale maps.
func MapPopulatedDocuments(
	fields []schema.Field,
	values store.Values,
	allLocales bool,
	transform func(schema.StableID, schema.LocaleCode, store.Document) store.Document,
) store.Values {
	return mapPopulatedDocuments(fields, values, allLocales, "", transform)
}

func mapPopulatedDocuments(
	fields []schema.Field,
	values store.Values,
	allLocales bool,
	inheritedLocale schema.LocaleCode,
	transform func(schema.StableID, schema.LocaleCode, store.Document) store.Document,
) store.Values {
	result := store.CloneValues(values)
	for _, field := range fields {
		value, exists := result[field.Name]
		if !exists {
			continue
		}
		result[field.Name] = mapPopulatedField(field, value, allLocales, inheritedLocale, transform)
	}
	return result
}

func mapPopulatedField(field schema.Field, value store.Value, allLocales bool, inheritedLocale schema.LocaleCode, transform func(schema.StableID, schema.LocaleCode, store.Document) store.Document) store.Value {
	if field.Localized && allLocales {
		localized, valid := value.ObjectValue()
		if !valid {
			return value
		}
		field.Localized = false
		for locale, candidate := range localized {
			localized[locale] = mapPopulatedFieldValue(field, candidate, allLocales, schema.LocaleCode(locale), transform)
		}
		return store.Object(localized)
	}
	field.Localized = false
	return mapPopulatedFieldValue(field, value, allLocales, inheritedLocale, transform)
}

func mapPopulatedFieldValue(field schema.Field, value store.Value, allLocales bool, inheritedLocale schema.LocaleCode, transform func(schema.StableID, schema.LocaleCode, store.Document) store.Document) store.Value {
	switch field.Type {
	case schema.FieldTypeRelationship, schema.FieldTypeUpload, schema.FieldTypeJoin:
		return mapReferenceValue(field, value, inheritedLocale, transform)
	case schema.FieldTypeGroup:
		object, valid := value.ObjectValue()
		if valid && field.Nested != nil {
			return store.Object(mapPopulatedDocuments(field.Nested.Fields, object, allLocales, inheritedLocale, transform))
		}
	case schema.FieldTypeArray:
		items, valid := value.Values()
		if !valid || field.Nested == nil {
			return value
		}
		for index, item := range items {
			if object, valid := item.ObjectValue(); valid {
				items[index] = store.Object(mapPopulatedDocuments(field.Nested.Fields, object, allLocales, inheritedLocale, transform))
			}
		}
		return store.List(items...)
	case schema.FieldTypeBlocks:
		items, valid := value.Values()
		if !valid || field.Blocks == nil {
			return value
		}
		for index, item := range items {
			object, valid := item.ObjectValue()
			if !valid {
				continue
			}
			blockType, _ := object["blockType"].StringValue()
			for _, block := range field.Blocks.Types {
				if block.Key == blockType {
					items[index] = store.Object(mapPopulatedDocuments(block.Fields, object, allLocales, inheritedLocale, transform))
					break
				}
			}
		}
		return store.List(items...)
	}
	return value
}

func mapReferenceValue(field schema.Field, value store.Value, locale schema.LocaleCode, transform func(schema.StableID, schema.LocaleCode, store.Document) store.Document) store.Value {
	relationship := relationshipDetails(field)
	if field.Join != nil {
		relationship = &schema.RelationshipField{
			CollectionID: field.Join.CollectionID, CollectionSlug: field.Join.CollectionSlug,
			HasMany: true,
		}
	}
	if relationship == nil {
		return value
	}
	mapOne := func(reference store.Value) store.Value {
		if !relationship.Polymorphic {
			if document, populated := reference.DocumentValue(); populated {
				return store.Populated(transform(relationship.CollectionID, locale, document))
			}
			return reference
		}
		object, valid := reference.ObjectValue()
		if !valid {
			return reference
		}
		slug, _ := object["relationTo"].StringValue()
		if document, populated := object["id"].DocumentValue(); populated {
			for _, target := range relationship.Targets {
				if string(target.CollectionSlug) == slug {
					object["id"] = store.Populated(transform(target.CollectionID, locale, document))
					break
				}
			}
		}
		return store.Object(object)
	}
	if !relationship.HasMany {
		return mapOne(value)
	}
	items, valid := value.Values()
	if !valid {
		return value
	}
	for index, item := range items {
		items[index] = mapOne(item)
	}
	return store.List(items...)
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

func sortedKeys(values store.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
