package mongodb

import (
	"fmt"
	"strings"
	"testing"

	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestMongoPopulationEnvelopeRejectsInvalidPlansBeforeDocumentCommands(t *testing.T) {
	target := mongoScalarCollection(false)
	target.ID, target.Slug = "population-targets", "population-targets"
	relationshipPath := mongoMustPath(t, "target")
	owner := schema.Collection{
		ID: "population-owners", Slug: "population-owners",
		Fields: []schema.Field{{
			ID: "population-owners-target", Name: "target", Path: relationshipPath,
			Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				CollectionID: target.ID, CollectionSlug: target.Slug, OnDelete: schema.ReferenceDeleteNullify,
			},
		}},
	}
	collections := map[schema.StableID]schema.Collection{owner.ID: owner, target.ID: target}
	valid := store.Request{
		Collection: owner, Collections: collections,
		Populate: []query.Population{{Path: relationshipPath, Depth: 1}},
	}
	if err := validateRequestEnvelope(valid); err != nil {
		t.Fatalf("valid relationship population envelope = %v", err)
	}

	scalar := valid
	scalar.Populate = []query.Population{{Path: mongoMustPath(t, "missing")}}
	duplicate := valid
	duplicate.Populate = append(append([]query.Population(nil), valid.Populate...), valid.Populate...)
	deep := valid
	deep.Populate = []query.Population{{Path: relationshipPath, Depth: populationwalk.MaxDepth + 1}}
	missingTarget := valid
	missingTarget.Collections = map[schema.StableID]schema.Collection{owner.ID: owner}

	for _, test := range []struct {
		name    string
		request store.Request
		want    string
	}{
		{name: "non-relationship", request: scalar, want: "not a relationship"},
		{name: "duplicate", request: duplicate, want: "more than once"},
		{name: "depth", request: deep, want: "depth"},
		{name: "missing target", request: missingTarget, want: "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRequestEnvelope(test.request); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("population envelope error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoPopulationEnvelopeSeparatesExplicitAndGeneratedPathLimits(t *testing.T) {
	leaves := mongoScalarCollection(false)
	leaves.ID, leaves.Slug = "population-leaves", "population-leaves"
	hubPath := mongoMustPath(t, "hub")
	hub := schema.Collection{ID: "population-hubs", Slug: "population-hubs"}
	for index := 0; index < populationwalk.MaxExplicitPaths+1; index++ {
		name := fmt.Sprintf("reference%02d", index)
		path := mongoMustPath(t, name)
		hub.Fields = append(hub.Fields, schema.Field{
			ID: "population-hubs-" + schema.StableID(name), Name: name, Path: path,
			Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				CollectionID: leaves.ID, CollectionSlug: leaves.Slug, OnDelete: schema.ReferenceDeleteNullify,
			},
		})
	}
	owner := schema.Collection{
		ID: "population-sources", Slug: "population-sources",
		Fields: []schema.Field{{
			ID: "population-sources-hub", Name: "hub", Path: hubPath,
			Type: schema.FieldTypeRelationship, Category: schema.FieldCategoryRelationship,
			Relationship: &schema.RelationshipField{
				CollectionID: hub.ID, CollectionSlug: hub.Slug, OnDelete: schema.ReferenceDeleteNullify,
			},
		}},
	}
	collections := map[schema.StableID]schema.Collection{owner.ID: owner, hub.ID: hub, leaves.ID: leaves}
	if err := validateMongoPopulationEnvelope(store.Request{
		Collection: owner, Collections: collections,
		Populate: []query.Population{{Path: hubPath, Depth: 2}},
	}); err != nil {
		t.Fatalf("one explicit path with %d generated paths was rejected: %v", len(hub.Fields), err)
	}
	if err := validateMongoPopulationEnvelope(store.Request{
		Collection: hub, Collections: collections,
		Populate: populationwalk.DepthPopulations(hub, 1),
	}); err == nil || !strings.Contains(err.Error(), "explicit paths") {
		t.Fatalf("caller-authored population paths error = %v", err)
	}
	if err := validateMongoPopulationPlan(store.Request{
		Collection: hub, Collections: collections,
		Populate: populationwalk.DepthPopulations(hub, 1),
	}, false); err != nil {
		t.Fatalf("generated population paths were subjected to the explicit limit: %v", err)
	}
}
