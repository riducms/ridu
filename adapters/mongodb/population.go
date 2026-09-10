package mongodb

import (
	"context"
	"fmt"
	"sort"

	"github.com/riducms/ridu/internal/localization"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const maxMongoPopulationLookupIDs = 256

func validateMongoPopulationEnvelope(request store.Request) error {
	return validateMongoPopulationPlan(request, true)
}

func validateMongoPopulationPlan(request store.Request, enforceExplicitLimit bool) error {
	if len(request.Populate) == 0 {
		return nil
	}
	if enforceExplicitLimit && len(request.Populate) > populationwalk.MaxExplicitPaths {
		return fmt.Errorf("MongoDB population supports at most %d explicit paths", populationwalk.MaxExplicitPaths)
	}
	seenPaths := make(map[string]struct{}, len(request.Populate))
	expanded := 0
	for _, population := range request.Populate {
		canonical := population.Path.String()
		if _, duplicate := seenPaths[canonical]; duplicate {
			return fmt.Errorf("MongoDB population field %q is requested more than once", canonical)
		}
		seenPaths[canonical] = struct{}{}
		depth := population.Depth
		if depth <= 0 {
			depth = 1
		}
		if depth > populationwalk.MaxDepth {
			return fmt.Errorf("MongoDB population depth must not exceed %d", populationwalk.MaxDepth)
		}
		field, found := populationwalk.FieldAtPath(request.Collection.Fields, population.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			return fmt.Errorf("MongoDB population field %q is not a relationship", canonical)
		}

		queue := mongoRelationshipTargets(relationship)
		seenTargets := make(map[schema.StableID]struct{})
		for level := 0; level < depth && len(queue) != 0; level++ {
			next := make([]schema.RelationshipTarget, 0)
			for _, targetReference := range queue {
				if _, duplicate := seenTargets[targetReference.CollectionID]; duplicate {
					continue
				}
				seenTargets[targetReference.CollectionID] = struct{}{}
				target, exists := request.Collections[targetReference.CollectionID]
				if !exists || target.ID != targetReference.CollectionID || target.Slug != targetReference.CollectionSlug {
					return fmt.Errorf("MongoDB relationship target %q is unavailable", targetReference.CollectionID)
				}
				if err := validateCollectionEnvelope(target); err != nil {
					return fmt.Errorf("MongoDB relationship target %q: %w", target.ID, err)
				}
				if level+1 >= depth {
					continue
				}
				references := populationwalk.ReferenceFields(target.Fields)
				expanded += len(references)
				if expanded > populationwalk.MaxExpandedPaths {
					return fmt.Errorf("MongoDB population expands more than %d relationship fields", populationwalk.MaxExpandedPaths)
				}
				for _, nestedField := range references {
					next = append(next, mongoRelationshipTargets(populationwalk.RelationshipDetails(nestedField))...)
				}
			}
			queue = next
		}
	}
	return nil
}

func (transaction *documentTransaction) populate(ctx context.Context, documents []store.Document, request store.Request) ([]store.Document, error) {
	return transaction.populatePlan(ctx, documents, request, true, true)
}

func (transaction *documentTransaction) populatePlan(
	ctx context.Context,
	documents []store.Document,
	request store.Request,
	enforceExplicitLimit bool,
	consumeOutputBudget bool,
) ([]store.Document, error) {
	if len(request.Populate) == 0 {
		return documents, nil
	}
	if err := validateMongoPopulationPlan(request, enforceExplicitLimit); err != nil {
		return nil, err
	}
	populationBudget := request.PopulationBudget
	if populationBudget == nil {
		populationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
		request.PopulationBudget = populationBudget
	}
	seenPaths := make(map[string]struct{}, len(request.Populate))
	for _, candidate := range request.Populate {
		population := candidate
		canonical := population.Path.String()
		if _, duplicate := seenPaths[canonical]; duplicate {
			return nil, fmt.Errorf("MongoDB population field %q is requested more than once", canonical)
		}
		seenPaths[canonical] = struct{}{}
		if population.Depth <= 0 {
			population.Depth = 1
		}
		if population.Depth > populationwalk.MaxDepth {
			return nil, fmt.Errorf("MongoDB population depth must not exceed %d", populationwalk.MaxDepth)
		}
		field, found := populationwalk.FieldAtPath(request.Collection.Fields, population.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			return nil, fmt.Errorf("MongoDB population field %q is not a relationship", canonical)
		}

		populated := make(map[schema.StableID]map[string]store.Document)
		for _, relationshipTarget := range mongoRelationshipTargets(relationship) {
			target, exists := request.Collections[relationshipTarget.CollectionID]
			if !exists || target.ID != relationshipTarget.CollectionID || target.Slug != relationshipTarget.CollectionSlug {
				return nil, fmt.Errorf("MongoDB relationship target %q is unavailable", relationshipTarget.CollectionID)
			}
			if err := validateCollectionEnvelope(target); err != nil {
				return nil, fmt.Errorf("MongoDB relationship target %q: %w", target.ID, err)
			}
			if err := transaction.store.requireVerifiedIndexes(target); err != nil {
				return nil, fmt.Errorf("MongoDB relationship target %q: %w", target.ID, err)
			}
			ids := collectMongoRelationshipIDs(
				documents,
				request.Collection.Fields,
				population.Path,
				relationship,
				relationshipTarget.CollectionSlug,
				populationwalk.LocaleSelection{All: request.AllLocales, Chain: request.LocaleChain},
			)
			byID := make(map[string]store.Document, len(ids))
			for start := 0; start < len(ids); start += maxMongoPopulationLookupIDs {
				end := min(start+maxMongoPopulationLookupIDs, len(ids))
				batch := ids[start:end]
				targetRequest := store.Request{
					Collection: target, Collections: request.Collections,
					Access: request.PopulationAccess[target.ID], PublishedOnly: request.PublishedOnly,
					Deletion: store.DeletionActive, Locales: request.Locales,
					LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
				}
				predicate, err := requestPredicate(targetRequest, false)
				if err != nil {
					return nil, err
				}
				predicate = mongoAnd([]bson.D{
					predicate,
					mongoTypeGuard(mongoIDPath, "string"),
					{{Key: mongoIDPath, Value: bson.D{{Key: "$in", Value: batch}}}},
				})
				cursor, err := transaction.collection(target).Find(ctx, predicate, options.Find().SetSort(bson.D{{Key: mongoIDPath, Value: int32(1)}}))
				if err != nil {
					return nil, translateMongoError(ctx, err)
				}
				var targetDocuments []store.Document
				for cursor.Next(ctx) {
					document, decodeErr := decodeCollectionDocumentForLocales(cursor.Current, target, request.Locales)
					if decodeErr != nil {
						_ = cursor.Close(ctx)
						return nil, decodeErr
					}
					targetDocuments = append(targetDocuments, document)
				}
				if cursorErr := cursor.Err(); cursorErr != nil {
					_ = cursor.Close(ctx)
					return nil, translateMongoError(ctx, cursorErr)
				}
				if err := cursor.Close(ctx); err != nil {
					return nil, translateMongoError(ctx, err)
				}
				if population.Depth > 1 {
					targetDocuments, err = transaction.populatePlan(ctx, targetDocuments, store.Request{
						Collection: target, Collections: request.Collections,
						Populate:         populationwalk.DepthPopulations(target, population.Depth-1),
						PopulationAccess: request.PopulationAccess, PopulationBudget: populationBudget,
						PublishedOnly: request.PublishedOnly, Locales: request.Locales,
						LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
					}, false, false)
					if err != nil {
						return nil, err
					}
				}
				for _, document := range targetDocuments {
					selection := localization.Selection{
						Configured: request.Locales,
						Chain:      request.LocaleChain,
						All:        request.AllLocales,
					}
					if len(selection.Chain) != 0 {
						selection.Locale = selection.Chain[0]
					}
					document = localization.ProjectDocument(document, target.Fields, selection)
					byID[document.ID] = projectDocument(document, population.Select)
				}
			}
			populated[relationshipTarget.CollectionID] = byID
		}

		var populationError error
		for index := range documents {
			mapped, _ := populationwalk.MapAtPath(
				request.Collection.Fields,
				documents[index].Values,
				population.Path,
				populationwalk.LocaleSelection{All: request.AllLocales, Chain: request.LocaleChain},
				func(_ schema.Field, candidate store.Value) store.Value {
					if populationError != nil {
						return candidate
					}
					return populateMongoRelationshipValue(candidate, relationship, populated, func(document store.Document) bool {
						if consumeOutputBudget {
							populationError = populationBudget.ConsumeDocument(document)
						}
						return populationError == nil
					})
				},
			)
			if populationError != nil {
				return nil, populationError
			}
			documents[index].Values = mapped
		}
	}
	return documents, nil
}

// requireVerifiedPopulationIndexes validates every target collection that can
// be queried by the requested depth expansion. Find and List call this before
// issuing the base document command so a population plan cannot perform part
// of a read before discovering unverified target infrastructure.
func (backend *Store) requireVerifiedPopulationIndexes(request store.Request) error {
	for _, population := range request.Populate {
		depth := population.Depth
		if depth <= 0 {
			depth = 1
		}
		field, found := populationwalk.FieldAtPath(request.Collection.Fields, population.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			return fmt.Errorf("MongoDB population field %q is not a relationship", population.Path.String())
		}
		queue := mongoRelationshipTargets(relationship)
		seenTargets := make(map[schema.StableID]struct{})
		for level := 0; level < depth && len(queue) != 0; level++ {
			next := make([]schema.RelationshipTarget, 0)
			for _, targetReference := range queue {
				if _, seen := seenTargets[targetReference.CollectionID]; seen {
					continue
				}
				seenTargets[targetReference.CollectionID] = struct{}{}
				target, exists := request.Collections[targetReference.CollectionID]
				if !exists || target.ID != targetReference.CollectionID || target.Slug != targetReference.CollectionSlug {
					return fmt.Errorf("MongoDB relationship target %q is unavailable", targetReference.CollectionID)
				}
				if err := backend.requireVerifiedIndexes(target); err != nil {
					return fmt.Errorf("MongoDB relationship target %q: %w", target.ID, err)
				}
				if level+1 >= depth {
					continue
				}
				for _, nestedField := range populationwalk.ReferenceFields(target.Fields) {
					next = append(next, mongoRelationshipTargets(populationwalk.RelationshipDetails(nestedField))...)
				}
			}
			queue = next
		}
	}
	return nil
}

func mongoRelationshipTargets(relationship *schema.RelationshipField) []schema.RelationshipTarget {
	if relationship.Polymorphic {
		return append([]schema.RelationshipTarget(nil), relationship.Targets...)
	}
	return []schema.RelationshipTarget{{CollectionID: relationship.CollectionID, CollectionSlug: relationship.CollectionSlug}}
}

func collectMongoRelationshipIDs(
	documents []store.Document,
	fields []schema.Field,
	path query.Path,
	relationship *schema.RelationshipField,
	slug schema.CollectionSlug,
	locales populationwalk.LocaleSelection,
) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, document := range documents {
		populationwalk.VisitAtPath(fields, document.Values, path, locales, func(_ schema.Field, value store.Value) {
			items := []store.Value{value}
			if relationship.HasMany {
				items, _ = value.CopyList()
			}
			for _, reference := range items {
				id := ""
				if relationship.Polymorphic {
					object, valid := reference.CopyObject()
					if !valid {
						continue
					}
					relationTo, _ := object["relationTo"].StringValue()
					if relationTo != string(slug) {
						continue
					}
					id, _ = object["id"].StringValue()
				} else {
					id, _ = reference.StringValue()
				}
				if id == "" {
					continue
				}
				if _, duplicate := seen[id]; !duplicate {
					seen[id] = struct{}{}
					ids = append(ids, id)
				}
			}
		})
	}
	sort.Strings(ids)
	return ids
}

func populateMongoRelationshipValue(value store.Value, relationship *schema.RelationshipField, populated map[schema.StableID]map[string]store.Document, consume func(store.Document) bool) store.Value {
	populateOne := func(reference store.Value) store.Value {
		if !relationship.Polymorphic {
			id, valid := reference.StringValue()
			if !valid {
				return reference
			}
			if document, found := populated[relationship.CollectionID][id]; found && consume(document) {
				return store.Populated(document)
			}
			return reference
		}
		object, valid := reference.CopyObject()
		if !valid {
			return reference
		}
		slug, _ := object["relationTo"].StringValue()
		id, _ := object["id"].StringValue()
		for _, target := range relationship.Targets {
			if string(target.CollectionSlug) != slug {
				continue
			}
			if document, found := populated[target.CollectionID][id]; found && consume(document) {
				object["id"] = store.Populated(document)
			}
			break
		}
		return store.Object(object)
	}
	if !relationship.HasMany {
		return populateOne(value)
	}
	items, valid := value.CopyList()
	if !valid {
		return value
	}
	for index, item := range items {
		items[index] = populateOne(item)
	}
	return store.List(items...)
}
