package operation

import (
	"fmt"

	"github.com/riducms/ridu/internal/localization"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (engine *Engine) preparePopulations(collection Collection, operationContext Context, selection localization.Selection, request *store.Request) *Error {
	if len(request.Populate) > populationwalk.MaxExplicitPaths {
		return &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population must not request more than %d fields", populationwalk.MaxExplicitPaths)}
	}
	seenPaths := make(map[string]struct{}, len(request.Populate))
	seenTargets := make(map[schema.StableID]struct{})
	for index := range request.Populate {
		candidate := &request.Populate[index]
		canonical := candidate.Path.String()
		if _, duplicate := seenPaths[canonical]; duplicate {
			return &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population field %q is requested more than once", canonical)}
		}
		seenPaths[canonical] = struct{}{}
		field, found := populationwalk.FieldAtPath(collection.Schema.Fields, candidate.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			return &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population field %q is not a relationship or upload", canonical)}
		}
		if candidate.Depth <= 0 {
			candidate.Depth = 1
		}
		if candidate.Depth > populationwalk.MaxDepth {
			return &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population depth must not exceed %d", populationwalk.MaxDepth)}
		}
		preparedSelection, populationSelectionError := preparePopulationSelection(engine.schemas, relationship, candidate.Select)
		if populationSelectionError != nil {
			return populationSelectionError
		}
		candidate.Select = preparedSelection
		targets, expanded, graphError := engine.populationTargetGraph(relationship, candidate.Depth)
		if graphError != nil {
			return graphError
		}
		if len(seenTargets)+expanded > populationwalk.MaxExpandedPaths {
			return &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population expands more than %d relationship fields", populationwalk.MaxExpandedPaths)}
		}
		for _, target := range targets {
			seenTargets[target.CollectionID] = struct{}{}
		}
	}

	for targetID := range seenTargets {
		targetCollection, available := engine.collections[string(targetID)]
		if !available {
			continue
		}
		populationContext := operationContext
		populationContext.Operation, populationContext.Collection = Read, targetCollection.Schema
		populationContext.ID, populationContext.Data = "", store.Values{}
		populationContext.Value, populationContext.SiblingData = store.Value{}, nil
		populationContext.Document, populationContext.Original, populationContext.FieldPath = nil, nil, ""
		populationContext.Locale, populationContext.AllLocales = selection.Locale, selection.All
		decision, err := authorize(targetCollection, populationContext)
		if err != nil {
			return &Error{Code: "access_failed", Status: 500, Message: "population access rule failed", Cause: err}
		}
		switch decision.Kind {
		case Where:
			request.PopulationAccess[targetID] = decision.Access
		case Deny:
			request.PopulationAccess[targetID] = denyAllAccessPredicate()
		}
	}
	return nil
}

func preparePopulationSelection(collections map[schema.StableID]schema.Collection, relationship *schema.RelationshipField, selected []query.Path) ([]query.Path, *Error) {
	if selected == nil {
		return nil, nil
	}
	targets := relationshipSchemaTargets(relationship)
	// Keep a non-nil empty selection distinct from an omitted selection. Stores
	// use that distinction to return metadata without authored values.
	authored := make([]query.Path, 0, len(selected))
	for _, path := range selected {
		segments := path.Segments()
		if len(segments) != 1 {
			return nil, &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("populated target selection %q must be a top-level field", path.String())}
		}
		if metadata, metadataAvailable := populationMetadataAvailable(collections, targets, segments[0]); metadata {
			if !metadataAvailable {
				return nil, &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("populated target selection field %q is not defined", path.String())}
			}
			// Framework metadata is projected independently from authored Values.
			// The resolver reserves these names, so stores never receive them as
			// authored projection paths.
			continue
		}
		available := false
		for _, target := range targets {
			collection, exists := collections[target.CollectionID]
			if !exists {
				continue
			}
			for _, field := range collection.Fields {
				if field.Name == segments[0] && field.Category != schema.FieldCategoryPresentation {
					available = true
					break
				}
			}
			if available {
				break
			}
		}
		if available {
			authored = append(authored, path)
			continue
		}
		return nil, &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("populated target selection field %q is not defined", path.String())}
	}
	return authored, nil
}

func populationMetadataAvailable(collections map[schema.StableID]schema.Collection, targets []schema.RelationshipTarget, name string) (metadata, available bool) {
	switch name {
	case "id", "createdAt", "updatedAt":
		return true, true
	case "deletedAt", "_status", "_revision":
		for _, target := range targets {
			collection, exists := collections[target.CollectionID]
			if !exists {
				continue
			}
			if name == "deletedAt" && collection.Capabilities.Trash || (name == "_status" || name == "_revision") && collection.Versions != nil {
				return true, true
			}
		}
		return true, false
	default:
		return false, false
	}
}

func (engine *Engine) populationTargetGraph(relationship *schema.RelationshipField, depth int) ([]schema.RelationshipTarget, int, *Error) {
	queue := relationshipSchemaTargets(relationship)
	result := make([]schema.RelationshipTarget, 0, len(queue))
	seen := make(map[schema.StableID]bool)
	expanded := 0
	for level := 0; level < depth && len(queue) != 0; level++ {
		next := []schema.RelationshipTarget{}
		for _, target := range queue {
			if seen[target.CollectionID] {
				continue
			}
			seen[target.CollectionID] = true
			result = append(result, target)
			collection, exists := engine.schemas[target.CollectionID]
			if !exists || level+1 >= depth {
				continue
			}
			references := populationwalk.ReferenceFields(collection.Fields)
			expanded += len(references)
			if expanded > populationwalk.MaxExpandedPaths {
				return nil, expanded, &Error{Code: "bad_query", Status: 400, Message: fmt.Sprintf("population expands more than %d relationship fields", populationwalk.MaxExpandedPaths)}
			}
			for _, field := range references {
				nested := populationwalk.RelationshipDetails(field)
				next = append(next, relationshipSchemaTargets(nested)...)
			}
		}
		queue = next
	}
	return result, expanded, nil
}
