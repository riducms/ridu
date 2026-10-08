package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/querypath"
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
	artifact, err := BuildArtifact(t.Context(), "add-lists", &before, after, ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !artifactContainsSQL(t, artifact, "jsonb") {
		t.Fatal("lists did not produce JSONB columns")
	}
	scalar := resolve(field.Fields{field.Text("value")})
	list := resolve(field.Fields{field.TextList("value")})
	for _, allow := range []bool{false, true} {
		if _, err := BuildArtifact(t.Context(), "scalar-to-list", &scalar, list, ArtifactOptions{AllowDestructive: allow}); err == nil || !strings.Contains(err.Error(), "changes value shape") {
			t.Fatalf("unreviewed conversion allow=%v err=%v", allow, err)
		}
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "convert-list", Checksum: ridumigration.DataTransformChecksum([]byte("explicit-list-conversion"))}
	if _, err := planArtifact(t.Context(), "convert", &list, resolve(field.Fields{field.NumberList("value")}), ArtifactOptions{DataTransforms: []ridumigration.DataTransformDescriptor{descriptor}}); err != nil {
		t.Fatalf("explicit same-storage list transform rejected: %v", err)
	}
	for _, unique := range []bool{false, true} {
		snapshot := after.Snapshot()
		snapshot.Collections[0].Fields[1].Index = !unique
		snapshot.Collections[0].Fields[1].Unique = unique
		if _, err := BuildArtifact(t.Context(), "index-list", nil, schema.NewManifest(snapshot), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("list index accepted: %v", err)
		}
	}
	snapshot := after.Snapshot()
	path, _ := query.ParsePath("points")
	snapshot.Collections[0].Indexes = []schema.CollectionIndex{{Fields: []query.Path{path}}}
	if _, err := BuildArtifact(t.Context(), "compound-list", nil, schema.NewManifest(snapshot), ArtifactOptions{}); err == nil || !strings.Contains(err.Error(), "primitive list") {
		t.Fatalf("compound list index accepted: %v", err)
	}
}

func TestPostgresUnsupportedQueryPathMarkerIsCallerOnly(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Query bounds", Collections: []core.Collection{{Slug: "products", Fields: field.Fields{
		field.Text("title"),
		field.Array("sections", field.Fields{field.Array("links", field.Fields{field.TextList("labels"), field.Text("title")})}),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		supported bool
	}{{"sections.links.labels", true}, {"sections.links.title", true}, {"title.missing", false}} {
		path, _ := query.ParsePath(test.name)
		node := query.In(path, "value").Node()
		for _, caller := range []bool{false, true} {
			request := store.Request{Collection: manifest.Snapshot().Collections[0]}
			if caller {
				request.Filter = &node
			} else {
				request.Access = &node
			}
			_, _, err := requestPredicate(request, false)
			var unsupported *querypath.UnsupportedError
			if (err == nil) != test.supported || errors.As(err, &unsupported) != (caller && !test.supported) {
				t.Fatalf("path %s caller=%v marked incorrectly: %v", test.name, caller, err)
			}
		}
	}
}
