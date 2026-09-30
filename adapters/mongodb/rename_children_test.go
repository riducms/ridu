package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func renameChildrenManifest(t *testing.T, fields ...field.Node) (ridu.Config, schema.Manifest) {
	t.Helper()
	config := ridu.Config{
		Name: "Rename children",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: fields}},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return config, manifest
}

// renameChildrenIntent confirms every rename the two schemas suggest, as
// ridu migrate create --accept-renames does.
func renameChildrenIntent(t *testing.T, before, after schema.Manifest) []ridumigration.Rename {
	t.Helper()
	var renames []ridumigration.Rename
	for _, candidate := range schemadiff.RenameCandidates(before, after) {
		if candidate.Kind != schemadiff.RenameField {
			t.Fatalf("unexpected rename candidate %#v", candidate)
		}
		renames = append(renames, ridumigration.Rename{
			CollectionBefore: candidate.BeforeCollection.Slug, CollectionAfter: candidate.AfterCollection.Slug,
			FieldBefore: candidate.BeforeField.Path.String(), FieldAfter: candidate.AfterField.Path.String(),
		})
	}
	return renames
}

func renameChildrenHistory(t *testing.T, before schema.Manifest) string {
	t.Helper()
	directory := t.TempDir()
	if _, err := CreateArtifact(context.Background(), directory, "initial", before, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	return directory
}

// A group, array or blocks field can be renamed: its children keep their
// names and follow it, so they are not removals. When the same migration also
// renames or relocalizes a child, or relocalizes the field itself, the stored
// values would move with their old inner shape, and the planner says to make
// that change separately instead of reporting a removed field.
func TestMongoDBFieldRenameCarriesUnchangedChildrenAndRefusesChangedOnes(t *testing.T) {
	ctx := context.Background()
	hero := func(name string) field.Block {
		return field.Block{Slug: "hero", Fields: field.Fields{field.Text(name), field.Number("height")}}
	}
	for name, candidate := range map[string]struct {
		before, after []field.Node
		// refusal is empty when the rename must plan.
		refusal string
	}{
		"group": {
			[]field.Node{field.Group("meta", field.Fields{field.Text("slug").Index(), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Text("slug").Index(), field.Number("rank")})},
			"",
		},
		"array": {
			[]field.Node{field.Array("rows", field.Fields{field.Text("label"), field.Number("weight")})},
			[]field.Node{field.Array("items", field.Fields{field.Text("label"), field.Number("weight")})},
			"",
		},
		"blocks": {
			[]field.Node{field.Blocks("layout", hero("caption"))},
			[]field.Node{field.Blocks("sections", hero("caption"))},
			"",
		},
		"nested group": {
			[]field.Node{field.Group("meta", field.Fields{field.Group("seo", field.Fields{field.Text("slug")}), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Group("seo", field.Fields{field.Text("slug")}), field.Number("rank")})},
			"",
		},
		"group with renamed children": {
			[]field.Node{field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Text("handle"), field.Number("order")})},
			"renames its child",
		},
		"array with a renamed row field": {
			[]field.Node{field.Array("rows", field.Fields{field.Text("label"), field.Number("weight")})},
			[]field.Node{field.Array("items", field.Fields{field.Text("name"), field.Number("weight")})},
			`renames its child "rows.label"`,
		},
		"blocks with a renamed block field": {
			[]field.Node{field.Blocks("layout", hero("caption"))},
			[]field.Node{field.Blocks("sections", hero("credit"))},
			"renames its child",
		},
		"group with a relocalized child": {
			[]field.Node{field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Text("slug").Localized(), field.Number("rank")})},
			`changes the localization of its child "meta.slug"`,
		},
		"relocalized field": {
			[]field.Node{field.Text("title")},
			[]field.Node{field.Text("headline").Localized()},
			"changes whether it is localized",
		},
	} {
		_, before := renameChildrenManifest(t, append(candidate.before, field.Number("views"))...)
		_, after := renameChildrenManifest(t, append(candidate.after, field.Number("views"))...)
		renames := renameChildrenIntent(t, before, after)
		if len(renames) != 1 {
			t.Errorf("%s: %d rename candidates, want one", name, len(renames))
			continue
		}
		directory := renameChildrenHistory(t, before)
		_, err := CreateArtifactWithOptions(ctx, directory, "rename", after, time.Unix(2, 0), ArtifactOptions{Renames: renames})
		switch {
		case candidate.refusal == "" && err != nil:
			t.Errorf("%s was refused: %v", name, err)
		case candidate.refusal != "" && (err == nil || !strings.Contains(err.Error(), candidate.refusal) || !strings.Contains(err.Error(), "separate migration")):
			t.Errorf("%s = %v", name, err)
		}
		if candidate.refusal != "" {
			continue
		}
		// The written history replays against the planner.
		files, err := migrationartifact.ReadAll(directory)
		if err != nil || len(files) != 2 {
			t.Fatalf("%s history = %d files, %v", name, len(files), err)
		}
		if err := validateMongoDBArtifactHistory(ctx, files); err != nil {
			t.Errorf("%s history does not replay: %v", name, err)
		}
	}
}

// Committed history replays against this planner and must keep producing the
// same artifact. Before the children followed a renamed field, the only way
// to rename a container was to bind a data transform, which made its children
// read as a schema change the transform answers for. That reading, and the
// risk it records, stays for any migration that binds a transform.
func TestMongoDBContainerRenameWithATransformKeepsItsRecordedRisk(t *testing.T) {
	ctx := context.Background()
	fields := func(group string) ridu.Config {
		return ridu.Config{Name: "Rename children", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{
			field.Group(group, field.Fields{field.Text("slug"), field.Number("rank")}), field.Number("views"),
		}}}}
	}
	before, err := ridu.Resolve(fields("meta"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := ridu.Resolve(fields("info"))
	if err != nil {
		t.Fatal(err)
	}
	directory := renameChildrenHistory(t, before)
	transform := ridumigration.DataTransformDescriptor{Name: "move-meta", Checksum: ridumigration.DataTransformChecksum([]byte("move-meta-v1"))}
	options := ArtifactOptions{Renames: renameChildrenIntent(t, before, after), DataTransforms: []ridumigration.DataTransformDescriptor{transform}}
	if _, err := CreateArtifactWithOptions(ctx, directory, "rename", after, time.Unix(2, 0), options); err == nil || !strings.Contains(err.Error(), "safety resolution") {
		t.Fatalf("a container rename with a transform, without destructive approval = %v", err)
	}
	options.AllowDestructive = true
	if _, err := CreateArtifactWithOptions(ctx, directory, "rename", after, time.Unix(2, 0), options); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("history = %d files, %v", len(files), err)
	}
	recorded := false
	for _, risk := range files[1].Artifact.Risks {
		recorded = recorded || risk.Code == mongoRiskTransformedSchema
	}
	if !recorded {
		t.Fatalf("risks = %#v", files[1].Artifact.Risks)
	}
	if err := validateMongoDBArtifactHistory(ctx, files); err != nil {
		t.Fatalf("history does not replay: %v", err)
	}
}

// Against a replica set: a group, an array and a blocks field renamed with
// their children unchanged migrate. Nested values are read under the new
// names in the current document and in retained versions, and an index on a
// child follows the rename.
func TestMongoDBFieldRenameMovesContainersWithTheirChildren(t *testing.T) {
	backend := mongoIntegrationStore(t)
	ctx := t.Context()
	fields := func(group, array, blocks string) []field.Node {
		return []field.Node{
			field.Group(group, field.Fields{field.Text("slug").Index(), field.Number("rank")}),
			field.Array(array, field.Fields{field.Text("label")}),
			field.Blocks(blocks, field.Block{Slug: "hero", Fields: field.Fields{field.Text("caption")}}),
			field.Number("views"),
		}
	}
	beforeConfig, before := renameChildrenManifest(t, fields("meta", "rows", "layout")...)
	afterConfig, after := renameChildrenManifest(t, fields("info", "items", "sections")...)
	directory := renameChildrenHistory(t, before)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, before); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"meta":   store.Object(store.Values{"slug": store.String("hello"), "rank": store.Number(3)}),
		"rows":   store.List(store.Object(store.Values{"label": store.String("first")})),
		"layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "caption": store.String("Cover")})),
		"views":  store.Number(1),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"views": store.Number(2)}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	renames := renameChildrenIntent(t, before, after)
	if len(renames) != 3 {
		t.Fatalf("rename candidates = %d, want the group, the array and the blocks field", len(renames))
	}
	if _, err := CreateArtifactWithOptions(ctx, directory, "rename", after, time.Unix(2, 0), ArtifactOptions{Renames: renames}); err != nil {
		t.Fatalf("a rename with unchanged children was refused: %v", err)
	}
	// ridu migrate verify replays the history in a shadow database.
	if err := VerifyArtifactsWithOptions(ctx, mongoDBMigrationVerifierConfig(t), directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatalf("verify the rename history: %v", err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	// Readiness verifies the exact index plan, which now names info.slug
	// instead of meta.slug.
	if err := backend.VerifyIndexes(ctx, before); err == nil {
		t.Fatal("the index on the renamed group's child did not follow the rename")
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatalf("the migrated database is not ready for the renamed schema: %v", err)
	}

	renamed, err := ridu.New(afterConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	requireRenamed := func(subject string, values store.Values) {
		t.Helper()
		info, _ := values["info"].CopyObject()
		slug, _ := info["slug"].StringValue()
		rank, _ := info["rank"].NumberValue()
		items, _ := values["items"].CopyList()
		sections, _ := values["sections"].CopyList()
		if slug != "hello" || rank != 3 || len(items) != 1 || len(sections) != 1 {
			t.Fatalf("%s = %#v", subject, values)
		}
		item, _ := items[0].CopyObject()
		section, _ := sections[0].CopyObject()
		label, _ := item["label"].StringValue()
		caption, _ := section["caption"].StringValue()
		if label != "first" || caption != "Cover" {
			t.Fatalf("%s array and blocks values = %#v, %#v", subject, item, section)
		}
		for _, old := range []string{"meta", "rows", "layout"} {
			if _, kept := values[old]; kept {
				t.Fatalf("%s still has %s: %#v", subject, old, values)
			}
		}
	}
	found, err := renamed.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	requireRenamed("the current document", found.Values)
	versions, err := renamed.Local().Versions(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil || len(versions) != 2 {
		t.Fatalf("retained versions = %d, %v", len(versions), err)
	}
	for _, version := range versions {
		requireRenamed("a retained version", version.Snapshot.Values)
	}
	slug, err := query.NewPath("info", "slug")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := renamed.Local().List(ctx, "posts", ridu.ListOptions{Where: query.Equal(slug, "hello")})
	if err != nil || len(listed.Documents) != 1 {
		t.Fatalf("query by the renamed group's indexed child = %#v, %v", listed, err)
	}
}

// A collection index that names a child of the renamed field follows it: the
// config has to change the index path in the same save, and that is still
// the same index.
func TestMongoDBFieldRenameCarriesACollectionIndexOnAChild(t *testing.T) {
	ctx := context.Background()
	config := func(group string) ridu.Config {
		return ridu.Config{Name: "Rename children", Collections: []ridu.Collection{{
			Slug:    "posts",
			Fields:  field.Fields{field.Group(group, field.Fields{field.Text("slug"), field.Number("rank")}), field.Number("views")},
			Indexes: []ridu.CollectionIndex{{Fields: []string{group + ".slug", "views"}}},
		}}}
	}
	before, err := ridu.Resolve(config("meta"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := ridu.Resolve(config("info"))
	if err != nil {
		t.Fatal(err)
	}
	directory := renameChildrenHistory(t, before)
	renames := renameChildrenIntent(t, before, after)
	if len(renames) != 1 {
		t.Fatalf("rename candidates = %d", len(renames))
	}
	if _, err := CreateArtifactWithOptions(ctx, directory, "rename", after, time.Unix(2, 0), ArtifactOptions{Renames: renames}); err != nil {
		t.Fatalf("a rename whose child is in a collection index was refused: %v", err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("history = %d files, %v", len(files), err)
	}
	if err := validateMongoDBArtifactHistory(ctx, files); err != nil {
		t.Fatalf("history does not replay: %v", err)
	}
}
