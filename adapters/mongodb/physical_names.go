package mongodb

import (
	"fmt"
	"sort"

	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	mongoMaxIndexesPerCollection = 64
	mongoMaxIndexKeyPatternBytes = 2048
)

type mongoPhysicalIndexPlanSet struct {
	collections []mongoCollectionIndexPlan
	system      []mongoSystemIndexPlan
}

type mongoPhysicalCollectionClaim struct {
	name        string
	identity    string
	description string
}

type mongoPhysicalIndexClaim struct {
	collection  string
	name        string
	identity    string
	description string
}

func mongoPhysicalIndexPlans(manifest schema.Manifest) (mongoPhysicalIndexPlanSet, error) {
	collections, err := mongoIndexPlans(manifest)
	if err != nil {
		return mongoPhysicalIndexPlanSet{}, err
	}
	system := mongoSystemIndexPlans(collections)
	if err := validateMongoPhysicalIndexPlans(collections, system); err != nil {
		return mongoPhysicalIndexPlanSet{}, err
	}
	return mongoPhysicalIndexPlanSet{collections: collections, system: system}, nil
}

func validateMongoPhysicalIndexPlans(collections []mongoCollectionIndexPlan, system []mongoSystemIndexPlan) error {
	claims := make([]mongoPhysicalCollectionClaim, 0, len(collections)+len(system))
	type indexPlan struct {
		physicalName string
		owner        string
		description  string
		definitions  []mongoIndexDefinition
	}
	indexPlans := make([]indexPlan, 0, len(collections)+len(system))
	for _, plan := range collections {
		description := fmt.Sprintf("content resource %q", plan.collection.ID)
		claims = append(claims, mongoPhysicalCollectionClaim{
			name: plan.physicalName, identity: "content:" + string(plan.collection.ID), description: description,
		})
		indexPlans = append(indexPlans, indexPlan{
			physicalName: plan.physicalName, owner: "content:" + string(plan.collection.ID),
			description: description, definitions: plan.definitions,
		})
	}
	for _, plan := range system {
		identity := fmt.Sprintf("system:%d", plan.kind)
		description := plan.description
		if plan.kind == mongoSystemVersionIndexes {
			identity = "versions:" + string(plan.collectionID)
		}
		claims = append(claims, mongoPhysicalCollectionClaim{
			name: plan.physicalName, identity: identity, description: description,
		})
		indexPlans = append(indexPlans, indexPlan{
			physicalName: plan.physicalName, owner: identity,
			description: description, definitions: plan.definitions,
		})
	}
	if err := validateMongoPhysicalCollectionClaims(claims); err != nil {
		return err
	}
	sort.Slice(indexPlans, func(left, right int) bool {
		if indexPlans[left].physicalName != indexPlans[right].physicalName {
			return indexPlans[left].physicalName < indexPlans[right].physicalName
		}
		return indexPlans[left].description < indexPlans[right].description
	})
	indexClaims := make([]mongoPhysicalIndexClaim, 0)
	for _, plan := range indexPlans {
		if err := validateMongoPhysicalIndexNames(plan.physicalName, plan.description, plan.definitions); err != nil {
			return err
		}
		for _, definition := range plan.definitions {
			identity := definition.identity
			if identity == "" {
				identity = plan.owner + ":index:" + definition.name
			}
			indexClaims = append(indexClaims, mongoPhysicalIndexClaim{
				collection: plan.physicalName, name: definition.name, identity: identity,
				description: fmt.Sprintf("index %q with identity %q for %s", definition.name, identity, plan.description),
			})
		}
	}
	return validateMongoPhysicalIndexClaims(indexClaims)
}

func validateMongoPhysicalCollectionClaims(claims []mongoPhysicalCollectionClaim) error {
	ordered := append([]mongoPhysicalCollectionClaim(nil), claims...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].name != ordered[right].name {
			return ordered[left].name < ordered[right].name
		}
		if ordered[left].identity != ordered[right].identity {
			return ordered[left].identity < ordered[right].identity
		}
		return ordered[left].description < ordered[right].description
	})
	seen := make(map[string]mongoPhysicalCollectionClaim, len(ordered))
	for _, claim := range ordered {
		if claim.name == "" || claim.identity == "" || claim.description == "" {
			return fmt.Errorf("MongoDB physical collection claim is incomplete")
		}
		if previous, collision := seen[claim.name]; collision {
			if previous.identity != claim.identity {
				return fmt.Errorf(
					"MongoDB physical collection name collision %q between %s and %s",
					claim.name, previous.description, claim.description,
				)
			}
			continue
		}
		seen[claim.name] = claim
	}
	return nil
}

func validateMongoPhysicalIndexNames(physicalName, description string, definitions []mongoIndexDefinition) error {
	if physicalName == "" || description == "" {
		return fmt.Errorf("MongoDB physical index plan is incomplete")
	}
	requiredIndexes := len(definitions) + 1 // MongoDB always owns the native _id_ index.
	if requiredIndexes > mongoMaxIndexesPerCollection {
		return fmt.Errorf(
			"MongoDB physical index plan for %s requires %d indexes including the native _id_ index; MongoDB supports at most %d",
			description, requiredIndexes, mongoMaxIndexesPerCollection,
		)
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.name == "" || definition.name == "_id_" {
			return fmt.Errorf("MongoDB adapter-owned index for %s has invalid name %q", description, definition.name)
		}
		encodedKeys, err := bson.Marshal(definition.keys)
		if err != nil {
			return fmt.Errorf("encode MongoDB key pattern for index %q on %s: %w", definition.name, description, err)
		}
		if len(encodedKeys) > mongoMaxIndexKeyPatternBytes {
			return fmt.Errorf(
				"MongoDB index %q for %s requires a %d-byte key pattern; MongoDB supports at most %d bytes",
				definition.name, description, len(encodedKeys), mongoMaxIndexKeyPatternBytes,
			)
		}
		if _, collision := seen[definition.name]; collision {
			return fmt.Errorf(
				"MongoDB physical index name collision %q in collection %q for %s",
				definition.name, physicalName, description,
			)
		}
		seen[definition.name] = struct{}{}
	}
	return nil
}

func validateMongoPhysicalIndexClaims(claims []mongoPhysicalIndexClaim) error {
	ordered := append([]mongoPhysicalIndexClaim(nil), claims...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].collection != ordered[right].collection {
			return ordered[left].collection < ordered[right].collection
		}
		if ordered[left].name != ordered[right].name {
			return ordered[left].name < ordered[right].name
		}
		if ordered[left].identity != ordered[right].identity {
			return ordered[left].identity < ordered[right].identity
		}
		return ordered[left].description < ordered[right].description
	})
	seen := make(map[string]mongoPhysicalIndexClaim, len(ordered))
	for _, claim := range ordered {
		if claim.collection == "" || claim.name == "" || claim.identity == "" || claim.description == "" {
			return fmt.Errorf("MongoDB physical index claim is incomplete")
		}
		key := claim.collection + "\x00" + claim.name
		if previous, collision := seen[key]; collision {
			if previous.identity != claim.identity {
				return fmt.Errorf(
					"MongoDB physical index name collision %q in collection %q between %s and %s",
					claim.name, claim.collection, previous.description, claim.description,
				)
			}
			continue
		}
		seen[key] = claim
	}
	return nil
}
