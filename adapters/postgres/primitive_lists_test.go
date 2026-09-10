package postgres

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

func TestPrimitiveListsPostgresMigrationAndIndexes(t *testing.T) {
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
	artifact, err := BuildArtifact(t.Context(), "add-lists", &before, after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !artifactContainsSQL(t, artifact, "jsonb") {
		t.Fatal("lists did not produce JSONB columns")
	}
	scalar := resolve(field.Fields{field.Text("value")})
	list := resolve(field.Fields{field.TextList("value")})
	for _, allow := range []bool{false, true} {
		if _, err := BuildArtifact(t.Context(), "scalar-to-list", &scalar, list, nil, allow); err == nil || !strings.Contains(err.Error(), "changes value shape") {
			t.Fatalf("unreviewed conversion allow=%v err=%v", allow, err)
		}
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "convert-list", Checksum: ridumigration.DataTransformChecksum([]byte("explicit-list-conversion"))}
	contract := currentAtlasPlannerContract()
	if _, err := buildArtifactWithPlannerContracts(t.Context(), "convert", &list, resolve(field.Fields{field.NumberList("value")}), nil, false, contract, contract, descriptor); err != nil {
		t.Fatalf("explicit same-storage list transform rejected: %v", err)
	}
	for _, unique := range []bool{false, true} {
		snapshot := after.Snapshot()
		snapshot.Collections[0].Fields[1].Index = !unique
		snapshot.Collections[0].Fields[1].Unique = unique
		if _, err := BuildArtifact(t.Context(), "index-list", nil, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("list index accepted: %v", err)
		}
	}
	snapshot := after.Snapshot()
	path, _ := query.ParsePath("points")
	snapshot.Collections[0].Indexes = []schema.CollectionIndex{{Fields: []query.Path{path}}}
	if _, err := BuildArtifact(t.Context(), "compound-list", nil, schema.NewManifest(snapshot), nil, false); err == nil || !strings.Contains(err.Error(), "primitive list") {
		t.Fatalf("compound list index accepted: %v", err)
	}
}

func TestPrimitiveListPostgresUnsupportedQueryMarkerIsCallerOnly(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Query bounds", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Array("sections", field.Fields{field.Array("links", field.Fields{field.TextList("labels"), field.Text("title")})}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"labels", "title"} {
		path, _ := query.ParsePath("sections.links." + name)
		node := query.In(path, query.String("value")).Node()
		for _, caller := range []bool{false, true} {
			request := store.Request{Collection: manifest.Snapshot().Collections[0]}
			if caller {
				request.Filter = &node
			} else {
				request.Access = &node
			}
			_, _, err := requestPredicate(request, false)
			var unsupported *primitivefield.UnsupportedQueryError
			if err == nil || errors.As(err, &unsupported) != (caller && name == "labels") {
				t.Fatalf("field %s caller=%v marked incorrectly: %v", name, caller, err)
			}
		}
	}
}
