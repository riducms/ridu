package mongodb

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/primitivefield"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPrimitiveListsMongoDBMigrationAndIndexes(t *testing.T) {
	resolve := func(fields field.Fields) schema.Manifest {
		t.Helper()
		manifest, err := core.Resolve(core.Config{Name: "Lists", Collections: []core.Collection{{Slug: "products", Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	before := resolve(field.Fields{field.Text("title")})
	after := resolve(field.Fields{field.Text("title"), field.TextList("points").Default("oak", "oak"), field.NumberList("sizes").Default(0, 0)})
	if _, err := buildMongoDBArtifact(t.Context(), "add-lists", &before, after, mongoDBPlannerVersion, currentMongoDBPlannerContract()); err != nil {
		t.Fatal(err)
	}
	scalar := resolve(field.Fields{field.Text("value")})
	list := resolve(field.Fields{field.TextList("value")})
	if _, err := buildMongoDBArtifact(t.Context(), "scalar-to-list", &scalar, list, mongoDBPlannerVersion, currentMongoDBPlannerContract()); err == nil || !strings.Contains(err.Error(), "changes value shape") {
		t.Fatalf("automatic conversion: %v", err)
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "convert-list", Checksum: ridumigration.DataTransformChecksum([]byte("explicit-list-conversion"))}
	if _, err := buildMongoDBArtifactWithOptions(t.Context(), "convert", &scalar, list, mongoDBPlannerVersion, currentMongoDBPlannerContract(), ArtifactOptions{AllowDestructive: true, DataTransforms: []ridumigration.DataTransformDescriptor{descriptor}}); err != nil {
		t.Fatalf("explicit transform rejected: %v", err)
	}
	for _, unique := range []bool{false, true} {
		snapshot := after.Snapshot()
		snapshot.Collections[0].Fields[1].Index = !unique
		snapshot.Collections[0].Fields[1].Unique = unique
		if err := validateCollectionEnvelope(snapshot.Collections[0]); err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("list index accepted: %v", err)
		}
	}
	snapshot := after.Snapshot()
	path, _ := query.ParsePath("points")
	snapshot.Collections[0].Indexes = []schema.CollectionIndex{{Fields: []query.Path{path}}}
	if err := validateCollectionEnvelope(snapshot.Collections[0]); err == nil || !strings.Contains(err.Error(), "primitive list") {
		t.Fatalf("compound list index accepted: %v", err)
	}
}

func TestPrimitiveListMongoDBUnsupportedQueryMarkerIsCallerOnly(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Query bounds", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Array("rows", field.Fields{field.TextList("points"), field.Array("nested", field.Fields{field.Text("title")})}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rows.points", "rows.nested.title"} {
		path, _ := query.ParsePath(name)
		node := query.In(path, query.String("value")).Node()
		for _, caller := range []bool{false, true} {
			request := store.Request{Collection: manifest.Snapshot().Collections[0]}
			if caller {
				request.Filter = &node
			} else {
				request.Access = &node
			}
			_, err := requestPredicate(request, false)
			var unsupported *primitivefield.UnsupportedQueryError
			if err == nil || errors.As(err, &unsupported) != (caller && name == "rows.points") {
				t.Fatalf("field %s caller=%v marked incorrectly: %v", name, caller, err)
			}
		}
	}
}

func TestPrimitiveListShapeGuardsSurviveNotAndOr(t *testing.T) {
	path, _ := query.ParsePath("points")
	title, _ := query.ParsePath("title")
	collection := schema.Collection{ID: "products", Slug: "products", Fields: []schema.Field{{ID: "points", Name: "points", Path: path, Type: schema.FieldTypeTextList, Category: schema.FieldCategoryScalar, List: &schema.PrimitiveListField{}, Text: &schema.TextField{}}, {ID: "title", Name: "title", Path: title, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}}}}
	negated, _ := query.Not(query.In(path, query.String("oak")))
	either, _ := query.Or(query.Equal(title, query.String("x")), query.In(path, query.String("oak")))
	for _, expression := range []query.Expression{negated, either} {
		node := expression.Node()
		guards, err := mongoNodeRepeatedShapeGuards(collection, node, "filter", mongoPredicateScope{})
		if err != nil {
			t.Fatal(err)
		}
		if len(guards) != 1 || len(guards[0]) != 1 || guards[0][0].Key != "$jsonSchema" {
			t.Fatalf("hoisted guards %#v", guards)
		}
		compiled, err := compileMongoNode(collection, node, "filter", mongoPredicateScope{})
		if err != nil {
			t.Fatal(err)
		}
		if len(compiled) != 1 || compiled[0].Key != "$and" {
			t.Fatalf("not/or lost outer shape guard: %#v", compiled)
		}
	}
}
