package sqlite

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestPrimitiveListsSQLiteMigrationAndIndexes(t *testing.T) {
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
	if _, err := planArtifact(t.Context(), "add-lists", &before, after, false); err != nil {
		t.Fatal(err)
	}
	scalar := resolve(field.Fields{field.Text("value")})
	list := resolve(field.Fields{field.TextList("value")})
	if _, err := planArtifact(t.Context(), "scalar-to-list", &scalar, list, true); err == nil || !strings.Contains(err.Error(), "changes value shape") {
		t.Fatalf("automatic conversion: %v", err)
	}
	descriptor := ridumigration.DataTransformDescriptor{Name: "convert-list", Checksum: ridumigration.DataTransformChecksum([]byte("explicit-list-conversion"))}
	if _, err := planArtifact(t.Context(), "convert", &scalar, list, true, descriptor); err != nil {
		t.Fatalf("explicit transform rejected: %v", err)
	}
	for _, unique := range []bool{false, true} {
		snapshot := after.Snapshot()
		snapshot.Collections[0].Fields[1].Index = !unique
		snapshot.Collections[0].Fields[1].Unique = unique
		if _, err := sqliteDocumentIndexes(schema.NewManifest(snapshot)); err == nil || !strings.Contains(err.Error(), "primitive list") {
			t.Fatalf("list index accepted: %v", err)
		}
	}
	snapshot := after.Snapshot()
	path, _ := query.ParsePath("points")
	snapshot.Collections[0].Indexes = []schema.CollectionIndex{{Fields: []query.Path{path}}}
	if _, err := sqliteDocumentIndexes(schema.NewManifest(snapshot)); err == nil || !strings.Contains(err.Error(), "primitive list") {
		t.Fatalf("compound list index accepted: %v", err)
	}
}
