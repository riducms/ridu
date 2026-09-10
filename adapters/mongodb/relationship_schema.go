package mongodb

import (
	"fmt"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const maxMongoDocumentReferences = 512

func validateMongoRelationshipEnvelope(field schema.Field, path string) error {
	if field.Category != schema.FieldCategoryRelationship || field.Relationship == nil {
		return fmt.Errorf("MongoDB relationship field %q does not have a relationship contract", path)
	}
	relationship := field.Relationship
	switch relationship.OnDelete {
	case schema.ReferenceDeleteNullify, schema.ReferenceDeleteRestrict:
	default:
		return fmt.Errorf("MongoDB relationship field %q has unsupported delete action %q", path, relationship.OnDelete)
	}
	if field.Required && relationship.OnDelete == schema.ReferenceDeleteNullify {
		return fmt.Errorf("MongoDB required relationship field %q cannot use nullify on delete", path)
	}
	if relationship.Polymorphic {
		if relationship.CollectionID != "" || relationship.CollectionSlug != "" || len(relationship.Targets) == 0 {
			return fmt.Errorf("MongoDB polymorphic relationship field %q has invalid target metadata", path)
		}
		seenIDs := make(map[schema.StableID]struct{}, len(relationship.Targets))
		seenSlugs := make(map[schema.CollectionSlug]struct{}, len(relationship.Targets))
		for _, target := range relationship.Targets {
			if !schema.IsValidStableID(string(target.CollectionID)) || !schema.IsValidCollectionSlug(string(target.CollectionSlug)) {
				return fmt.Errorf("MongoDB polymorphic relationship field %q has an invalid target", path)
			}
			if _, duplicate := seenIDs[target.CollectionID]; duplicate {
				return fmt.Errorf("MongoDB polymorphic relationship field %q repeats target collection %q", path, target.CollectionID)
			}
			if _, duplicate := seenSlugs[target.CollectionSlug]; duplicate {
				return fmt.Errorf("MongoDB polymorphic relationship field %q repeats target slug %q", path, target.CollectionSlug)
			}
			seenIDs[target.CollectionID] = struct{}{}
			seenSlugs[target.CollectionSlug] = struct{}{}
		}
		return nil
	}
	if len(relationship.Targets) != 0 || !schema.IsValidStableID(string(relationship.CollectionID)) || !schema.IsValidCollectionSlug(string(relationship.CollectionSlug)) {
		return fmt.Errorf("MongoDB relationship field %q has invalid target metadata", path)
	}
	return nil
}

func validateMongoRelationshipValue(field schema.Field, value store.Value, path string) error {
	relationship := field.Relationship
	if relationship == nil {
		return fmt.Errorf("MongoDB relationship field %q does not have a relationship contract", path)
	}
	items := []store.Value{value}
	if relationship.HasMany {
		var valid bool
		items, valid = value.CopyList()
		if !valid {
			return fmt.Errorf("MongoDB relationship value %q must be a list", path)
		}
		if field.Required && len(items) == 0 {
			return fmt.Errorf("MongoDB document is missing required field %q", path)
		}
		if len(items) > maxMongoDocumentReferences {
			return fmt.Errorf("MongoDB relationship value %q exceeds %d references", path, maxMongoDocumentReferences)
		}
	}
	for index, item := range items {
		itemPath := path
		if relationship.HasMany {
			itemPath = fmt.Sprintf("%s.%d", path, index)
		}
		if !relationship.Polymorphic {
			id, valid := item.StringValue()
			if !valid {
				return fmt.Errorf("MongoDB relationship value %q must be a document ID string", itemPath)
			}
			if id == "" && !field.Required && !relationship.HasMany {
				continue
			}
			if err := store.ValidateDocumentID(id); err != nil {
				return fmt.Errorf("MongoDB relationship value %q: %w", itemPath, err)
			}
			continue
		}
		if item.Kind() != store.ValueObject || item.Len() != 2 {
			return fmt.Errorf("MongoDB polymorphic relationship value %q must contain only relationTo and id", itemPath)
		}
		relationTo, slugValid := item.Get("relationTo").StringValue()
		id, idValid := item.Get("id").StringValue()
		if !slugValid || !idValid || !mongoRelationshipTargetAllowsSlug(relationship, schema.CollectionSlug(relationTo)) {
			return fmt.Errorf("MongoDB polymorphic relationship value %q has an invalid target", itemPath)
		}
		if err := store.ValidateDocumentID(id); err != nil {
			return fmt.Errorf("MongoDB polymorphic relationship value %q: %w", itemPath, err)
		}
	}
	return nil
}

func mongoRelationshipTargetAllowsSlug(relationship *schema.RelationshipField, slug schema.CollectionSlug) bool {
	for _, target := range relationship.Targets {
		if target.CollectionSlug == slug {
			return true
		}
	}
	return false
}

func mongoReferenceDetails(field schema.Field) *schema.RelationshipField {
	if field.Relationship != nil {
		copy := *field.Relationship
		copy.Targets = append([]schema.RelationshipTarget(nil), field.Relationship.Targets...)
		return &copy
	}
	if field.Upload != nil {
		return &schema.RelationshipField{
			CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug,
			HasMany: field.Upload.HasMany, OnDelete: field.Upload.OnDelete,
		}
	}
	return nil
}
