package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// sqliteRenameConfig declares one field at every depth a rename can reach:
// the root, a localized root field, a group, an array row and a block.
func sqliteRenameConfig(title, intro, slug, label, caption string) ridu.Config {
	return ridu.Config{
		Name: "SQLite renames",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: field.Fields{
			field.Text(title).Unique().Index(),
			field.Textarea(intro).Localized(),
			field.Group("seo", field.Fields{field.Text(slug), field.Number("rank")}),
			field.Array("rows", field.Fields{field.Text(label), field.Number("weight")}),
			field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text(caption), field.Number("height")}}),
		}}},
	}
}

func sqliteRenameManifest(t *testing.T, config ridu.Config) schema.Manifest {
	t.Helper()
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// sqliteRenameIntentFor accepts every rename the two schemas imply, encoded
// as ridu migrate create encodes it.
func sqliteRenameIntentFor(t *testing.T, before, after schema.Manifest, want int) []migration.Rename {
	t.Helper()
	var renames []migration.Rename
	for _, candidate := range schemadiff.RenameCandidates(before, after) {
		if candidate.Kind == schemadiff.RenameCollection {
			t.Fatalf("unexpected rename candidate %#v", candidate)
		}
		renames = append(renames, migration.Rename{
			CollectionBefore: candidate.BeforeCollection.Slug, CollectionAfter: candidate.AfterCollection.Slug, Block: candidate.Block,
			FieldBefore: candidate.BeforeField.Path.String(), FieldAfter: candidate.AfterField.Path.String(),
		})
	}
	if len(renames) != want {
		t.Fatalf("rename candidates = %#v, want %d", renames, want)
	}
	return renames
}

func sqliteStoredValues(t *testing.T, backend *Store, query string, arguments ...any) map[string]any {
	t.Helper()
	var encoded string
	if err := backend.db.QueryRowContext(context.Background(), query, arguments...).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// A reviewed rename moves stored content at every depth, in current documents
// and retained versions, keeps uniqueness enforced under the new name, and
// moves everything back on rollback.
func TestSQLiteFieldRenameMovesContentAndRollsBack(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	beforeConfig := sqliteRenameConfig("title", "intro", "slug", "label", "caption")
	afterConfig := sqliteRenameConfig("headline", "lede", "handle", "name", "credit")
	before, after := sqliteRenameManifest(t, beforeConfig), sqliteRenameManifest(t, afterConfig)
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	values := store.Values{
		"title": store.String("Hello"),
		"intro": store.String("English intro"),
		"seo":   store.Object(store.Values{"slug": store.String("hello"), "rank": store.Number(3)}),
		"rows": store.List(
			store.Object(store.Values{"label": store.String("first"), "weight": store.Number(1)}),
			store.Object(store.Values{"label": store.String("second"), "weight": store.Number(2)}),
		),
		"layout": store.List(store.Object(store.Values{
			"blockType": store.String("hero"), "caption": store.String("Cover"), "height": store.Number(40),
		})),
	}
	post, err := application.Local().Create(ctx, "posts", values, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"intro": store.String("Intro en français")}, ridu.MutationOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	collectionID := string(before.Snapshot().Collections[0].ID)
	// A key no schema names must survive the rewrite untouched.
	if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_documents SET values_json = json_set(values_json, '$.legacy', 'kept') WHERE collection_id = ? AND id = ?`, collectionID, post.ID); err != nil {
		t.Fatal(err)
	}

	renames := sqliteRenameIntentFor(t, before, after, 5)
	created, err := CreateArtifactWithRenames(ctx, directory, "rename", after, time.Unix(2, 0), renames)
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := sqliteArtifactRenames(files[1].Artifact)
	if err != nil || len(recorded) != 5 || created.Name != files[1].Name {
		t.Fatalf("recorded renames = %#v, %v", recorded, err)
	}
	for _, risk := range files[1].Artifact.Risks {
		if risk.Level == migration.RiskDestructive {
			t.Fatalf("a rename was planned as destructive: %#v", risk)
		}
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}

	current := sqliteStoredValues(t, backend, `SELECT values_json FROM ridu_documents WHERE collection_id = ? AND id = ?`, collectionID, post.ID)
	assertSQLiteRenamed(t, "current document", current)
	if current["legacy"] != "kept" {
		t.Fatalf("the rename dropped an unrelated key: %#v", current)
	}
	var versions int
	rows, err := backend.db.QueryContext(ctx, `SELECT snapshot_json FROM ridu_versions WHERE collection_id = ? AND document_id = ? ORDER BY revision`, collectionID, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		var snapshot struct{ Values map[string]any }
		if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
			t.Fatal(err)
		}
		assertSQLiteRenamed(t, "version snapshot", snapshot.Values)
		versions++
	}
	if err := rows.Close(); err != nil || versions != 2 {
		t.Fatalf("retained versions = %d, %v", versions, err)
	}

	renamed, err := ridu.New(afterConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	found, err := renamed.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	headline, _ := found.Values["headline"].StringValue()
	lede, _ := found.Values["lede"].StringValue()
	if headline != "Hello" || lede != "Intro en français" {
		t.Fatalf("renamed document = %#v", found.Values)
	}
	// Uniqueness and the index now follow the new name.
	headlinePath, _ := query.NewPath("headline")
	listed, err := renamed.Local().List(ctx, "posts", ridu.ListOptions{Where: query.Equal(headlinePath, "Hello")})
	if err != nil || len(listed.Documents) != 1 {
		t.Fatalf("query by the renamed field = %#v, %v", listed, err)
	}
	if _, err := renamed.Local().Create(ctx, "posts", store.Values{"headline": store.String("Hello")}, ridu.MutationOptions{}); err == nil {
		t.Fatal("the renamed field no longer enforces uniqueness")
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, after)
	if err != nil || len(statuses) != 2 || !statuses[1].Applied {
		t.Fatalf("status after rename = %#v, %v", statuses, err)
	}

	if err := backend.DownArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	restored := sqliteStoredValues(t, backend, `SELECT values_json FROM ridu_documents WHERE collection_id = ? AND id = ?`, collectionID, post.ID)
	if restored["title"] != "Hello" || restored["headline"] != nil || restored["legacy"] != "kept" {
		t.Fatalf("rolled-back document = %#v", restored)
	}
	if intro, _ := restored["intro"].(map[string]any); intro["en"] != "English intro" || intro["fr"] != "Intro en français" {
		t.Fatalf("rolled-back localized value = %#v", restored["intro"])
	}
	if seo, _ := restored["seo"].(map[string]any); seo["slug"] != "hello" || seo["handle"] != nil {
		t.Fatalf("rolled-back group = %#v", restored["seo"])
	}
	original, err := application.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := original.Values["title"].StringValue(); title != "Hello" {
		t.Fatalf("rolled-back document through the earlier config = %#v", original.Values)
	}
	// The migration applies again after a rollback.
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	assertSQLiteRenamed(t, "reapplied document", sqliteStoredValues(t, backend, `SELECT values_json FROM ridu_documents WHERE collection_id = ? AND id = ?`, collectionID, post.ID))
}

func assertSQLiteRenamed(t *testing.T, subject string, values map[string]any) {
	t.Helper()
	if values["headline"] != "Hello" || values["title"] != nil {
		t.Fatalf("%s root field = %#v", subject, values)
	}
	lede, _ := values["lede"].(map[string]any)
	if lede["en"] != "English intro" || values["intro"] != nil {
		t.Fatalf("%s localized field = %#v", subject, values)
	}
	seo, _ := values["seo"].(map[string]any)
	if seo["handle"] != "hello" || seo["slug"] != nil || seo["rank"] != float64(3) {
		t.Fatalf("%s group = %#v", subject, seo)
	}
	rows, _ := values["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("%s rows = %#v", subject, values["rows"])
	}
	for index, want := range []string{"first", "second"} {
		row, _ := rows[index].(map[string]any)
		if row["name"] != want || row["label"] != nil || row["weight"] != float64(index+1) {
			t.Fatalf("%s row %d = %#v", subject, index, row)
		}
	}
	layout, _ := values["layout"].([]any)
	if len(layout) != 1 {
		t.Fatalf("%s layout = %#v", subject, values["layout"])
	}
	hero, _ := layout[0].(map[string]any)
	if hero["credit"] != "Cover" || hero["caption"] != nil || hero["blockType"] != "hero" || hero["height"] != float64(40) {
		t.Fatalf("%s block = %#v", subject, hero)
	}
}

// Rename intent is checked against what the two schemas imply. Anything else
// is refused before a file is written, and a migration whose recorded renames
// were edited no longer matches the planner.
func TestSQLiteFieldRenameRefusesUnreviewableIntent(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	config := func(fields ...field.Node) ridu.Config {
		return ridu.Config{Name: "SQLite renames", Collections: []ridu.Collection{{Slug: "posts", Fields: fields}}}
	}
	before := sqliteRenameManifest(t, config(field.Text("title"), field.Number("views")))
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	title := migration.Rename{CollectionBefore: "posts", CollectionAfter: "posts", FieldBefore: "title", FieldAfter: "headline"}
	for name, candidate := range map[string]struct {
		after   schema.Manifest
		renames []migration.Rename
		want    string
	}{
		"not a candidate": {
			sqliteRenameManifest(t, config(field.Text("headline"), field.Number("views"))),
			[]migration.Rename{{CollectionBefore: "posts", CollectionAfter: "posts", FieldBefore: "views", FieldAfter: "headline"}},
			"not an unambiguous field rename",
		},
		"repeated": {
			sqliteRenameManifest(t, config(field.Text("headline"), field.Number("views"))),
			[]migration.Rename{title, title},
			"renamed more than once",
		},
		"changed shape": {
			sqliteRenameManifest(t, config(field.Number("headline"), field.Number("views"))),
			[]migration.Rename{title},
			"not an unambiguous field rename",
		},
		"another field removed": {
			sqliteRenameManifest(t, config(field.Text("headline"))),
			[]migration.Rename{title},
			"was removed",
		},
		"collection": {
			sqliteRenameManifest(t, ridu.Config{Name: "SQLite renames", Collections: []ridu.Collection{{
				Slug: "articles", Fields: field.Fields{field.Text("title"), field.Number("views")},
			}}}),
			[]migration.Rename{{CollectionBefore: "posts", CollectionAfter: "articles"}},
			"cannot rename collection",
		},
	} {
		if _, err := CreateArtifactWithRenames(ctx, directory, "rename", candidate.after, time.Unix(2, 0), candidate.renames); err == nil || !strings.Contains(err.Error(), candidate.want) {
			t.Errorf("%s = %v, want %q", name, err, candidate.want)
		}
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("refused renames wrote files: %d, %v", len(files), err)
	}
	if _, err := CreateArtifactWithRenames(ctx, t.TempDir(), "rename", before, time.Unix(2, 0), []migration.Rename{title}); err == nil || !strings.Contains(err.Error(), "initial SQLite migration") {
		t.Fatalf("rename without history = %v", err)
	}
}

// A document that already stores a value under the new name would lose one of
// the two values, so the migration stops and changes nothing. The conflict
// sits in the last collection's retained version, after every earlier
// document and snapshot has been rewritten, so only the transaction keeps the
// database unchanged.
func TestSQLiteFieldRenameStopsBeforeOverwritingAValue(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	config := func(name string) ridu.Config {
		return ridu.Config{Name: "SQLite renames", Collections: []ridu.Collection{
			{Slug: "alpha", Versions: true, Fields: field.Fields{field.Text(name)}},
			{Slug: "beta", Versions: true, Fields: field.Fields{field.Text(name)}},
		}}
	}
	before, after := sqliteRenameManifest(t, config("title")), sqliteRenameManifest(t, config("headline"))
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config("title"), backend)
	if err != nil {
		t.Fatal(err)
	}
	documents := map[string]string{}
	for _, collection := range []string{"alpha", "beta"} {
		created, err := application.Local().Create(ctx, collection, store.Values{"title": store.String("Moves")}, ridu.MutationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		documents[collection] = created.ID
	}
	if _, err := backend.db.ExecContext(ctx, `UPDATE ridu_versions SET snapshot_json = json_set(snapshot_json, '$.Values.headline', 'Already here') WHERE collection_id = 'beta'`); err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		t.Helper()
		var encoded []string
		for _, query := range []string{
			`SELECT collection_id || ':' || values_json FROM ridu_documents ORDER BY collection_id`,
			`SELECT collection_id || ':' || snapshot_json FROM ridu_versions ORDER BY collection_id`,
		} {
			rows, err := backend.db.QueryContext(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var row string
				if err := rows.Scan(&row); err != nil {
					t.Fatal(err)
				}
				encoded = append(encoded, row)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return strings.Join(encoded, "\n")
	}
	untouched := stored()
	if !strings.Contains(untouched, `alpha:{"title":"Moves"}`) {
		t.Fatalf("stored content before the rename:\n%s", untouched)
	}

	renames := sqliteRenameIntentFor(t, before, after, 2)
	if _, err := CreateArtifactWithRenames(ctx, directory, "rename", after, time.Unix(2, 0), renames); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err == nil || !strings.Contains(err.Error(), "already has a value") || !strings.Contains(err.Error(), documents["beta"]) {
		t.Fatalf("rename onto a stored value = %v", err)
	}
	if after := stored(); after != untouched {
		t.Fatalf("a failed rename changed stored content:\n%s\nwas:\n%s", after, untouched)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, after)
	if err != nil || len(statuses) != 2 || statuses[1].Applied {
		t.Fatalf("status after a failed rename = %#v, %v", statuses, err)
	}

	// The development rename is one transaction too.
	development := newSQLiteMigrationStore(t)
	if err := development.Migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	for id, values := range map[string]string{"clear": `{"title":"Moves"}`, "occupied": `{"title":"Stays","headline":"Already here"}`} {
		collection := map[string]string{"clear": "alpha", "occupied": "beta"}[id]
		if _, err := development.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, status, revision, values_json)
VALUES (?, ?, 1, 1, '', 0, ?)`, collection, id, values); err != nil {
			t.Fatal(err)
		}
	}
	if err := development.RenameDevelopmentFields(ctx, before, after, renames); err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("development rename onto a stored value = %v", err)
	}
	unchanged := sqliteStoredValues(t, development, `SELECT values_json FROM ridu_documents WHERE id = 'clear'`)
	if unchanged["title"] != "Moves" {
		t.Fatalf("a failed development rename changed a document: %#v", unchanged)
	}
}

// A rename carries a field's content to a new name and nothing else. When the
// same save also changes how the field or its children are stored, the moved
// values would sit where the new config does not read them, so the planner
// refuses rather than report the data preserved.
func TestSQLiteFieldRenameRefusesAFieldThatAlsoChangesShape(t *testing.T) {
	ctx := context.Background()
	config := func(fields ...field.Node) ridu.Config {
		return ridu.Config{
			Name: "SQLite renames",
			Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
				{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
			}},
			Collections: []ridu.Collection{{Slug: "posts", Fields: fields}},
		}
	}
	for name, candidate := range map[string]struct {
		before, after ridu.Config
		// detected says whether the two schemas suggest a rename at all.
		detected bool
	}{
		"becomes localized": {
			config(field.Text("title"), field.Number("views")),
			config(field.Text("headline").Localized(), field.Number("views")),
			true,
		},
		"stops being localized": {
			config(field.Text("title").Localized(), field.Number("views")),
			config(field.Text("headline"), field.Number("views")),
			true,
		},
		"children renamed too": {
			config(field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")})),
			config(field.Group("info", field.Fields{field.Text("handle"), field.Number("order")})),
			true,
		},
		"child removed": {
			config(field.Group("meta", field.Fields{field.Text("slug"), field.Number("rank")}), field.Text("title")),
			config(field.Group("info", field.Fields{field.Text("slug")}), field.Text("title")),
			false,
		},
	} {
		before, after := sqliteRenameManifest(t, candidate.before), sqliteRenameManifest(t, candidate.after)
		directory := t.TempDir()
		if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
			t.Fatal(err)
		}
		var renames []migration.Rename
		for _, detected := range schemadiff.RenameCandidates(before, after) {
			renames = append(renames, migration.Rename{
				CollectionBefore: detected.BeforeCollection.Slug, CollectionAfter: detected.AfterCollection.Slug,
				FieldBefore: detected.BeforeField.Path.String(), FieldAfter: detected.AfterField.Path.String(),
			})
		}
		if detected := len(renames) != 0; detected != candidate.detected {
			t.Errorf("%s: rename detected = %t", name, detected)
			continue
		}
		if len(renames) == 0 {
			// Nothing to confirm: the change is a removal, which is refused.
			if _, err := CreateArtifact(ctx, directory, "rename", after, time.Unix(2, 0), false); err == nil {
				t.Errorf("%s: the change was planned without a rename", name)
			}
			continue
		}
		if _, err := CreateArtifactWithRenames(ctx, directory, "rename", after, time.Unix(2, 0), renames); err == nil || !strings.Contains(err.Error(), "more than its name changes") {
			t.Errorf("%s = %v", name, err)
		}
	}
}

// Field IDs are derived from the collection slug and the field path, so two
// collections can hold fields with the same ID. A rename in one of them must
// not excuse removing the other.
func TestSQLiteFieldRenameIsScopedToItsCollection(t *testing.T) {
	ctx := context.Background()
	config := func(nested string, removed bool) ridu.Config {
		flat := field.Fields{field.Number("views")}
		if !removed {
			flat = append(flat, field.Text("title"))
		}
		return ridu.Config{Name: "SQLite renames", Collections: []ridu.Collection{
			{Slug: "posts", Fields: field.Fields{field.Group("meta", field.Fields{field.Text(nested), field.Number("rank")})}},
			{Slug: "posts-meta", Fields: flat},
		}}
	}
	before := sqliteRenameManifest(t, config("title", false))
	snapshot := before.Snapshot()
	nestedID := snapshot.Collections[0].Fields[0].Nested.ResolvedFields()[0].ID
	if flatID := snapshot.Collections[1].Fields[1].ID; nestedID != flatID {
		t.Skipf("field IDs %q and %q no longer collide", nestedID, flatID)
	}
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	rename := []migration.Rename{{CollectionBefore: "posts", CollectionAfter: "posts", FieldBefore: "meta.title", FieldAfter: "meta.headline"}}
	if _, err := CreateArtifactWithRenames(ctx, directory, "rename", sqliteRenameManifest(t, config("headline", true)), time.Unix(2, 0), rename); err == nil || !strings.Contains(err.Error(), "was removed") {
		t.Fatalf("a removal riding on another collection's rename = %v", err)
	}
	if _, err := CreateArtifactWithRenames(ctx, directory, "rename", sqliteRenameManifest(t, config("headline", false)), time.Unix(2, 0), rename); err != nil {
		t.Fatalf("the rename on its own = %v", err)
	}
}

// A rich text field with its own blocks keeps those blocks' stored payloads
// when it is renamed: the value moves whole and the block fields, whose IDs
// follow the field's name, still count as unchanged.
func TestSQLiteFieldRenameCarriesARichTextFieldWithBlocks(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	config := func(name string) ridu.Config {
		callout := field.Block{Slug: "callout", Fields: field.Fields{field.Text("tone"), field.Number("weight")}}
		return ridu.Config{Name: "SQLite renames", Plugins: []ridu.Plugin{richtext.New()}, Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{richtext.Field(name, richtext.Config{Blocks: []field.Block{callout}}), field.Number("views")},
		}}}
	}
	before, after := sqliteRenameManifest(t, config("body")), sqliteRenameManifest(t, config("article"))
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	const document = `{"root":{"type":"root","children":[{"type":"block","fields":{"blockType":"callout","_key":"k1","tone":"warm","weight":2}}]}}`
	if _, err := backend.db.ExecContext(ctx, `INSERT INTO ridu_documents
  (collection_id, id, created_at, updated_at, status, revision, values_json)
VALUES ('posts', 'post-1', 1, 1, '', 0, json_object('body', json(?), 'views', 1))`, document); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifactWithRenames(ctx, directory, "rename", after, time.Unix(2, 0), sqliteRenameIntentFor(t, before, after, 1)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	var moved, left string
	if err := backend.db.QueryRowContext(ctx, `SELECT json_extract(values_json, '$.article'), coalesce(json_extract(values_json, '$.body'), '') FROM ridu_documents WHERE id = 'post-1'`).Scan(&moved, &left); err != nil {
		t.Fatal(err)
	}
	if left != "" || !strings.Contains(moved, `"tone":"warm"`) || !strings.Contains(moved, `"weight":2`) {
		t.Fatalf("renamed rich text = %s, old name = %q", moved, left)
	}
}

// ridu dev renames content in the database it synchronizes with the executor
// migrations use, brings that database to the renamed schema in the same
// step, and leaves a database with migration history to ridu migrate.
func TestSQLiteDevelopmentRenameMovesContentOnlyWithoutMigrationHistory(t *testing.T) {
	ctx := context.Background()
	config := func(name string) ridu.Config {
		return ridu.Config{Name: "SQLite renames", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text(name).Unique()}}}}
	}
	before, after := sqliteRenameManifest(t, config("title")), sqliteRenameManifest(t, config("headline"))
	renames := sqliteRenameIntentFor(t, before, after, 1)

	backend := newSQLiteMigrationStore(t)
	if err := backend.Migrate(ctx, before); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config("title"), backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.RenameDevelopmentFields(ctx, before, after, renames); err != nil {
		t.Fatal(err)
	}
	// The database now records the renamed schema as the one it has, so
	// nothing takes the applied rename for one still waiting.
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatalf("the database after a development rename: %v", err)
	}
	if err := backend.Ready(ctx, before); err == nil {
		t.Fatal("the database still reports the schema from before the rename")
	}
	renamed, err := ridu.New(config("headline"), backend)
	if err != nil {
		t.Fatal(err)
	}
	found, err := renamed.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if headline, _ := found.Values["headline"].StringValue(); headline != "Hello" {
		t.Fatalf("renamed development document = %#v", found.Values)
	}
	if _, err := renamed.Local().Create(ctx, "posts", store.Values{"headline": store.String("Hello")}, ridu.MutationOptions{}); err == nil {
		t.Fatal("the renamed field no longer enforces uniqueness")
	}

	managed := newSQLiteMigrationStore(t)
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	if err := managed.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := managed.RenameDevelopmentFields(ctx, before, after, renames); err == nil || !strings.Contains(err.Error(), "managed by ridu migrate") {
		t.Fatalf("development rename on a managed database = %v", err)
	}
}

// A collection index or a join that names the renamed field follows it: the
// config has to change both in the same save, and that is still one rename.
func TestSQLiteFieldRenameCarriesIndexesAndJoinsThatNameTheField(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	config := func(author string) ridu.Config {
		return ridu.Config{Name: "SQLite renames", Collections: []ridu.Collection{
			{Slug: "users", Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", author)}},
			{
				Slug:    "posts",
				Fields:  field.Fields{field.Relationship(author, "users").OnDelete(field.ReferenceDeleteRestrict), field.Number("views")},
				Indexes: []ridu.CollectionIndex{{Fields: []string{author, "views"}, Unique: true}},
			},
		}}
	}
	before, after := sqliteRenameManifest(t, config("author")), sqliteRenameManifest(t, config("writer"))
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	backend := newSQLiteMigrationStore(t)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config("author"), backend)
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Create(ctx, "users", store.Values{"name": store.String("Ada")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{"author": store.String(user.ID), "views": store.Number(1)}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := CreateArtifactWithRenames(ctx, directory, "rename", after, time.Unix(2, 0), sqliteRenameIntentFor(t, before, after, 1)); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	renamed, err := ridu.New(config("writer"), backend)
	if err != nil {
		t.Fatal(err)
	}
	// The compound unique index and the reference index follow the new name.
	if _, err := renamed.Local().Create(ctx, "posts", store.Values{"writer": store.String(user.ID), "views": store.Number(1)}, ridu.MutationOptions{}); err == nil {
		t.Fatal("the compound unique index no longer covers the renamed field")
	}
	if _, err := renamed.Local().Delete(ctx, "users", user.ID, ridu.MutationOptions{}); err == nil {
		t.Fatal("the renamed relationship no longer protects its target")
	}
	joined, err := renamed.Local().ListJoin(ctx, "users", user.ID, "posts", ridu.ListOptions{})
	if err != nil || len(joined.Documents) != 1 {
		t.Fatalf("join over the renamed field = %#v, %v", joined, err)
	}
	if err := backend.DownArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{"author": store.String(user.ID), "views": store.Number(1)}, ridu.MutationOptions{}); err == nil {
		t.Fatal("the rolled-back compound unique index no longer covers the field")
	}

	// An index that changes in any other way is still refused.
	reshaped := config("writer")
	reshaped.Collections[1].Indexes[0].Unique = false
	unrenamed := t.TempDir()
	if _, err := CreateArtifact(ctx, unrenamed, "initial", before, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifactWithRenames(ctx, unrenamed, "reshaped", sqliteRenameManifest(t, reshaped), time.Unix(2, 0), sqliteRenameIntentFor(t, before, after, 1)); err == nil || !strings.Contains(err.Error(), "existing index") {
		t.Fatalf("rename with a reshaped index = %v", err)
	}
}
