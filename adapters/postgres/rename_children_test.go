package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
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
		Collections: []ridu.Collection{{Slug: "posts", Fields: fields}},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return config, manifest
}

// renameChildrenIntent confirms every rename the two schemas suggest, as
// ridu migrate create --accept-renames does.
func renameChildrenIntent(t *testing.T, before, after schema.Manifest) []Rename {
	t.Helper()
	var renames []Rename
	for _, candidate := range schemadiff.RenameCandidates(before, after) {
		if candidate.Kind != schemadiff.RenameField {
			t.Fatalf("unexpected rename candidate %#v", candidate)
		}
		renames = append(renames, Rename{
			Kind: RenameField, BeforeCollection: candidate.BeforeCollection, AfterCollection: candidate.AfterCollection,
			BeforeField: candidate.BeforeField, AfterField: candidate.AfterField,
		})
	}
	return renames
}

// Rename detection pairs fields by shape and ignores the names of their
// children, so a group renamed together with its children is offered as a
// rename. A field rename moves the stored value whole: the nested values
// would stay under the old child names, where the new config does not read
// them. Creating that migration is refused, at every depth a child can sit,
// and so is a rename that changes the field's own localization.
func TestPostgresFieldRenameRefusesRenamedChildren(t *testing.T) {
	ctx := context.Background()
	hero := func(name string) field.Block {
		return field.Block{Slug: "hero", Fields: field.Fields{field.Text(name), field.Number("height")}}
	}
	for name, candidate := range map[string]struct {
		before, after []field.Node
		child, change string
	}{
		"group": {
			[]field.Node{field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Text("handle"), field.Number("order")})},
			`"meta.`, "renames",
		},
		"array row": {
			[]field.Node{field.Array("rows", field.Fields{field.Text("label"), field.Number("weight")})},
			[]field.Node{field.Array("items", field.Fields{field.Text("name"), field.Number("weight")})},
			`"rows.label"`, "renames",
		},
		"block": {
			[]field.Node{field.Blocks("layout", hero("caption"))},
			[]field.Node{field.Blocks("sections", hero("credit"))},
			"caption", "renames",
		},
		"nested group": {
			[]field.Node{field.Group("meta", field.Fields{field.Group("seo", field.Fields{field.Text("slug")}), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Group("seo", field.Fields{field.Text("handle")}), field.Number("rank")})},
			`"meta.seo.slug"`, "renames",
		},
		"child localization": {
			[]field.Node{field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")})},
			[]field.Node{field.Group("info", field.Fields{field.Text("slug").Localized(), field.Number("rank")})},
			`"meta.slug"`, "changes the localization of",
		},
		// Without this rule the planner would record the rename and drop the
		// column whose values it was confirmed to keep.
		"own localization": {
			[]field.Node{field.Text("title")},
			[]field.Node{field.Text("headline").Localized()},
			`"title"`, "changes whether it is localized",
		},
	} {
		_, before := renameChildrenManifest(t, append(candidate.before, field.Number("views"))...)
		_, after := renameChildrenManifest(t, append(candidate.after, field.Number("views"))...)
		renames := renameChildrenIntent(t, before, after)
		if len(renames) != 1 {
			t.Errorf("%s: %d rename candidates, want the one this rule exists for", name, len(renames))
			continue
		}
		for entry, build := range map[string]func() error{
			"BuildArtifact": func() error {
				// Destructive approval does not turn the change into a rename.
				_, err := BuildArtifact(ctx, "rename", &before, after, renames, true)
				return err
			},
			"BuildArtifactWithPreviousPlanner": func() error {
				_, err := BuildArtifactWithPreviousPlanner(ctx, "rename", &before, after, renames, false, AtlasVersion)
				return err
			},
		} {
			err := build()
			if err == nil || !strings.Contains(err.Error(), "separate migration") || !strings.Contains(err.Error(), candidate.child) || !strings.Contains(err.Error(), candidate.change) {
				t.Errorf("%s through %s = %v", name, entry, err)
			}
		}
	}
}

// The rule applies when a migration is created. The planner that replays
// committed history does not apply it, so a migration that was created and
// applied before the rule existed still verifies against the planner.
func TestPostgresFieldRenameRuleDoesNotInvalidateCommittedHistory(t *testing.T) {
	ctx := context.Background()
	_, before := renameChildrenManifest(t, field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")}), field.Number("views"))
	_, after := renameChildrenManifest(t, field.Group("info", field.Fields{field.Text("handle"), field.Number("order")}), field.Number("views"))
	contract := currentAtlasPlannerContract()
	committed, err := buildArtifactWithPlannerContracts(ctx, "rename", &before, after, renameChildrenIntent(t, before, after), false, contract, contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePostgresPlannedSQL(ctx, committed); err != nil {
		t.Fatalf("a committed rename with renamed children no longer verifies: %v", err)
	}
}

// A group, array or blocks field renamed with its children unchanged is an
// ordinary rename: it migrates, and the nested values are read under the new
// name.
func TestPostgresFieldRenameKeepsUnchangedChildren(t *testing.T) {
	ctx := context.Background()
	backend := migrationArtifactTestBackend(t)
	fields := func(group, array, blocks string) []field.Node {
		return []field.Node{
			field.Group(group, field.Fields{field.Text("slug"), field.Number("rank")}),
			field.Array(array, field.Fields{field.Text("label")}),
			field.Blocks(blocks, field.Block{Slug: "hero", Fields: field.Fields{field.Text("caption")}}),
			field.Number("views"),
		}
	}
	beforeConfig, before := renameChildrenManifest(t, fields("meta", "rows", "layout")...)
	afterConfig, after := renameChildrenManifest(t, fields("info", "items", "sections")...)
	directory := t.TempDir()
	initial, err := BuildArtifact(ctx, "initial", nil, before, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{}); err != nil {
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

	renames := renameChildrenIntent(t, before, after)
	if len(renames) != 3 {
		t.Fatalf("rename candidates = %d, want the group, the array and the blocks field", len(renames))
	}
	rename, err := BuildArtifact(ctx, "rename", &before, after, renames, false)
	if err != nil {
		t.Fatalf("a rename with unchanged children was refused: %v", err)
	}
	if _, err := migrationartifact.Create(directory, "rename", rename, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	renamed, err := ridu.New(afterConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	found, err := renamed.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	info, _ := found.Values["info"].CopyObject()
	slug, _ := info["slug"].StringValue()
	rank, _ := info["rank"].NumberValue()
	items, _ := found.Values["items"].CopyList()
	sections, _ := found.Values["sections"].CopyList()
	if slug != "hello" || rank != 3 || len(items) != 1 || len(sections) != 1 {
		t.Fatalf("renamed document = %#v", found.Values)
	}
	item, _ := items[0].CopyObject()
	section, _ := sections[0].CopyObject()
	label, _ := item["label"].StringValue()
	caption, _ := section["caption"].StringValue()
	if label != "first" || caption != "Cover" {
		t.Fatalf("renamed array and blocks values = %#v, %#v", item, section)
	}
}
