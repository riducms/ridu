package mongodb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoPhysicalIndexPlansRejectEveryCurrentNamespaceCollision(t *testing.T) {
	content := func(id, physicalName string) mongoCollectionIndexPlan {
		return mongoCollectionIndexPlan{
			collection: schema.Collection{ID: schema.StableID(id)}, physicalName: physicalName,
		}
	}
	versions := func(id, physicalName string) mongoSystemIndexPlan {
		return mongoSystemIndexPlan{
			kind: mongoSystemVersionIndexes, collectionID: schema.StableID(id),
			description: "version state for " + id, physicalName: physicalName,
		}
	}
	system := func(kind mongoSystemIndexKind, description, physicalName string) mongoSystemIndexPlan {
		return mongoSystemIndexPlan{kind: kind, description: description, physicalName: physicalName}
	}
	tests := []struct {
		name        string
		collections []mongoCollectionIndexPlan
		system      []mongoSystemIndexPlan
	}{
		{name: "content content", collections: []mongoCollectionIndexPlan{content("alpha", "collision"), content("beta", "collision")}},
		{name: "content version", collections: []mongoCollectionIndexPlan{content("alpha", "collision")}, system: []mongoSystemIndexPlan{versions("beta", "collision")}},
		{name: "content system", collections: []mongoCollectionIndexPlan{content("alpha", "collision")}, system: []mongoSystemIndexPlan{system(mongoSystemPreferenceIndexes, "preference state", "collision")}},
		{name: "version version", system: []mongoSystemIndexPlan{versions("alpha", "collision"), versions("beta", "collision")}},
		{name: "version system", system: []mongoSystemIndexPlan{
			versions("alpha", "collision"),
			system(mongoSystemPreferenceIndexes, "preference state", "collision"),
		}},
		{name: "system system", system: []mongoSystemIndexPlan{
			system(mongoSystemPreferenceIndexes, "preference state", "collision"),
			system(mongoSystemTaskIndexes, "task state", "collision"),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := validateMongoPhysicalIndexPlans(test.collections, test.system)
			if first == nil || !strings.Contains(first.Error(), `physical collection name collision "collision"`) {
				t.Fatalf("physical collision error = %v", first)
			}
			reversedCollections := append([]mongoCollectionIndexPlan(nil), test.collections...)
			for left, right := 0, len(reversedCollections)-1; left < right; left, right = left+1, right-1 {
				reversedCollections[left], reversedCollections[right] = reversedCollections[right], reversedCollections[left]
			}
			reversedSystem := append([]mongoSystemIndexPlan(nil), test.system...)
			for left, right := 0, len(reversedSystem)-1; left < right; left, right = left+1, right-1 {
				reversedSystem[left], reversedSystem[right] = reversedSystem[right], reversedSystem[left]
			}
			second := validateMongoPhysicalIndexPlans(reversedCollections, reversedSystem)
			if second == nil || second.Error() != first.Error() {
				t.Fatalf("reversed physical collision error = %v, want %v", second, first)
			}
		})
	}
}

func TestMongoPhysicalIndexPlanNamesAreScopedAndHistoricalClaimsCanRepeat(t *testing.T) {
	shared := mongoIndexDefinition{name: "z_i_shared", keys: bson.D{{Key: "values.shared", Value: int32(1)}}}
	plans := []mongoCollectionIndexPlan{
		{collection: schema.Collection{ID: "alpha"}, physicalName: "z_c_alpha", definitions: []mongoIndexDefinition{shared}},
		{collection: schema.Collection{ID: "beta"}, physicalName: "z_c_beta", definitions: []mongoIndexDefinition{shared}},
	}
	if err := validateMongoPhysicalIndexPlans(plans, nil); err != nil {
		t.Fatalf("same index name in separate collections rejected: %v", err)
	}
	if err := validateMongoPhysicalIndexNames("z_c_alpha", "content resource alpha", []mongoIndexDefinition{shared, shared}); err == nil || !strings.Contains(err.Error(), "physical index name collision") {
		t.Fatalf("same-collection index collision error = %v", err)
	}
	claim := mongoPhysicalCollectionClaim{name: "z_c_alpha", identity: "content:alpha", description: `content resource "alpha"`}
	if err := validateMongoPhysicalCollectionClaims([]mongoPhysicalCollectionClaim{claim, claim}); err != nil {
		t.Fatalf("same logical physical claim across historical plans rejected: %v", err)
	}
	indexClaim := mongoPhysicalIndexClaim{
		collection: "z_c_alpha", name: "z_i_shared", identity: "declared:index:alpha:field:alpha-title",
		description: `index "z_i_shared" for content resource "alpha"`,
	}
	if err := validateMongoPhysicalIndexClaims([]mongoPhysicalIndexClaim{indexClaim, indexClaim}); err != nil {
		t.Fatalf("same logical index claim across historical plans rejected: %v", err)
	}
	collidingIndexClaim := indexClaim
	collidingIndexClaim.identity = "declared:index:alpha:field:alpha-heading"
	collidingIndexClaim.description = `index "z_i_shared" for a different stable declaration`
	first := validateMongoPhysicalIndexClaims([]mongoPhysicalIndexClaim{indexClaim, collidingIndexClaim})
	second := validateMongoPhysicalIndexClaims([]mongoPhysicalIndexClaim{collidingIndexClaim, indexClaim})
	if first == nil || second == nil || first.Error() != second.Error() || !strings.Contains(first.Error(), "physical index name collision") {
		t.Fatalf("historical index collision errors = %v / %v", first, second)
	}
}

func TestMongoPhysicalIndexPlansEnforceExactServerBounds(t *testing.T) {
	definitions := make([]mongoIndexDefinition, mongoMaxIndexesPerCollection-1)
	for index := range definitions {
		definitions[index] = mongoIndexDefinition{
			name: fmt.Sprintf("z_i_%02d", index),
			keys: bson.D{{Key: fmt.Sprintf("values.field%d", index), Value: int32(1)}},
		}
	}
	if err := validateMongoPhysicalIndexNames("z_c_boundary", "boundary resource", definitions); err != nil {
		t.Fatalf("63 adapter-owned indexes plus native _id_ rejected: %v", err)
	}
	overflow := append(append([]mongoIndexDefinition(nil), definitions...), mongoIndexDefinition{
		name: "z_i_overflow", keys: bson.D{{Key: "values.overflow", Value: int32(1)}},
	})
	if err := validateMongoPhysicalIndexNames("z_c_overflow", "overflow resource", overflow); err == nil || !strings.Contains(err.Error(), "requires 65 indexes") {
		t.Fatalf("65-index physical plan error = %v", err)
	}

	definitionWithKeyBytes := func(size int) mongoIndexDefinition {
		t.Helper()
		// One int32 BSON element has 11 bytes of document, type, terminator,
		// and value overhead in addition to its key bytes.
		definition := mongoIndexDefinition{
			name: "z_i_key_boundary",
			keys: bson.D{{Key: strings.Repeat("a", size-11), Value: int32(1)}},
		}
		encoded, err := bson.Marshal(definition.keys)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) != size {
			t.Fatalf("encoded MongoDB key pattern = %d bytes, want %d", len(encoded), size)
		}
		return definition
	}
	if err := validateMongoPhysicalIndexNames("z_c_boundary", "boundary resource", []mongoIndexDefinition{
		definitionWithKeyBytes(mongoMaxIndexKeyPatternBytes),
	}); err != nil {
		t.Fatalf("exactly bounded MongoDB key pattern rejected: %v", err)
	}
	if err := validateMongoPhysicalIndexNames("z_c_overflow", "overflow resource", []mongoIndexDefinition{
		definitionWithKeyBytes(mongoMaxIndexKeyPatternBytes + 1),
	}); err == nil || !strings.Contains(err.Error(), "2049-byte key pattern") {
		t.Fatalf("oversized MongoDB key pattern error = %v", err)
	}
}

func TestMongoPhysicalIndexPlanUsesStableResourceIdentities(t *testing.T) {
	collection := mongoScalarCollection(false)
	collection.ID, collection.Slug = "stable-resource", "articles"
	collection.Capabilities.Versions = true
	collection.Versions = &schema.VersionSettings{MaxPerDocument: 3}
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	collection.Fields[0].Index = true
	before, err := mongoPhysicalIndexPlans(mongoIndexTestManifest(collection))
	if err != nil {
		t.Fatal(err)
	}
	renamed := collection
	renamed.Slug = "renamed-articles"
	after, err := mongoPhysicalIndexPlans(mongoIndexTestManifest(renamed))
	if err != nil {
		t.Fatal(err)
	}
	if len(before.collections) != 1 || len(after.collections) != 1 || before.collections[0].physicalName != after.collections[0].physicalName {
		t.Fatalf("content names across slug rename = %#v / %#v", before.collections, after.collections)
	}
	if len(before.collections[0].definitions) != 2 || len(after.collections[0].definitions) != 2 ||
		before.collections[0].definitions[0].name != after.collections[0].definitions[0].name ||
		before.collections[0].definitions[1].name != after.collections[0].definitions[1].name {
		t.Fatalf("index names across slug rename = %#v / %#v", before.collections[0].definitions, after.collections[0].definitions)
	}
	versionName := func(plans []mongoSystemIndexPlan) string {
		for _, plan := range plans {
			if plan.kind == mongoSystemVersionIndexes && plan.collectionID == collection.ID {
				return plan.physicalName
			}
		}
		return ""
	}
	if beforeName, afterName := versionName(before.system), versionName(after.system); beforeName == "" || beforeName != afterName {
		t.Fatalf("version names across slug rename = %q / %q", beforeName, afterName)
	}
}
