package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func fieldKindConfig(t *testing.T, body field.Node) core.Config {
	t.Helper()
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return core.Config{Name: "Field kinds", Plugins: []core.Plugin{richtext.New()}, Storage: objects, StorageNamespace: "field-kinds", Collections: []core.Collection{
		{Slug: "users", Fields: field.Fields{field.Text("name")}},
		{Slug: "media", Upload: true, UploadConfig: core.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}, Fields: field.Fields{}},
		{Slug: "posts", Versions: true, Fields: field.Fields{body, field.Text("summary")}},
	}}
}

func fieldKindCases() []struct {
	name          string
	before, after field.Node
	value         store.Value
} {
	return []struct {
		name          string
		before, after field.Node
		value         store.Value
	}{
		{"blocks-to-richtext", field.Blocks("body", field.Block{Slug: "paragraph", Fields: field.Fields{field.Text("content")}}), richtext.Field("body"), store.List(store.Object(store.Values{"blockType": store.String("paragraph"), "content": store.String("Keep me until reviewed")}))},
		{"text-to-number", field.Text("body"), field.Number("body"), store.String("not a number")},
		{"relationship-to-upload", field.Relationship("body", "users"), field.Upload("body", "media"), store.String("target")},
		{"select-to-multiselect", field.Select("body", "one", "two"), field.MultiSelect("body", "one", "two"), store.String("one")},
		{"multiselect-to-select", field.MultiSelect("body", "one", "two"), field.Select("body", "one", "two"), store.List(store.String("one"), store.String("two"))},
	}
}

func fieldKindManifest(t *testing.T, config core.Config) schema.Manifest {
	t.Helper()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func fieldKindResource(t *testing.T, manifest schema.Manifest, slug string) schema.Collection {
	t.Helper()
	for _, resource := range manifest.Snapshot().Collections {
		if string(resource.Slug) == slug {
			return resource
		}
	}
	t.Fatalf("missing %s", slug)
	return schema.Collection{}
}

func fieldKindSeed(t *testing.T, backend store.Store, manifest schema.Manifest, value store.Value) string {
	t.Helper()
	ctx := context.Background()
	var locales []schema.LocaleCode
	if localization := manifest.Snapshot().Application.Localization; localization != nil {
		locales = localization.LocaleCodes()
	}
	transaction, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Create(ctx, store.CreateRequest{Collection: fieldKindResource(t, manifest, "users"), ID: "target", Values: store.Values{"name": store.String("Target")}, Locales: locales}); err != nil {
		t.Fatal(err)
	}
	resource := fieldKindResource(t, manifest, "posts")
	document, err := transaction.Create(ctx, store.CreateRequest{Collection: resource, ID: "owner", Values: store.Values{"body": value, "summary": store.String("Untouched")}, Locales: locales})
	if err != nil {
		t.Fatal(err)
	}
	versions := transaction.(store.VersionTransaction)
	if _, err := versions.SaveVersion(ctx, resource, document, 0); err != nil {
		t.Fatal(err)
	}
	document.Revision++
	if _, err := versions.SaveVersion(ctx, resource, document, 0); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return document.ID
}

func fieldKindDatabase(t *testing.T, adapter projectfile.DatabaseAdapter, definition projectfile.File, before schema.Manifest) (store.Store, string, string) {
	t.Helper()
	ctx := context.Background()
	switch adapter {
	case projectfile.DatabaseSQLite:
		path := devRenameSQLite(t, definition, before)
		return devRenameSQLiteStore(t, path), "", path
	case projectfile.DatabasePostgres:
		url, backend := devRenameDatabase(t)
		if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
			t.Fatal(err)
		}
		return backend, url, ""
	case projectfile.DatabaseMongoDB:
		url := devRenameMongoDB(t)
		backend := devRenameMongoDBStore(t, url)
		if err := backend.SyncDevelopmentSchema(ctx, before); err != nil {
			t.Fatal(err)
		}
		return backend, url, ""
	}
	t.Fatalf("unsupported adapter %s", adapter)
	return nil, "", ""
}

func fieldKindCommitBaseline(t *testing.T, definition projectfile.File, before schema.Manifest, target *developmentRenameTarget) {
	t.Helper()
	ctx := context.Background()
	directory := definition.Absolute(definition.Migrations)
	var err error
	switch definition.Database {
	case projectfile.DatabaseSQLite:
		_, err = sqlite.CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false)
	case projectfile.DatabaseMongoDB:
		_, err = mongodb.CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), mongodb.ArtifactOptions{})
	case projectfile.DatabasePostgres:
		artifact, buildErr := postgres.BuildArtifact(ctx, "initial", nil, before, nil, false)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err = migrationartifact.Create(directory, "initial", artifact, time.Unix(1, 0))
	}
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case target.sqlite != nil:
		_, err = target.sqlite.AdoptArtifacts(ctx, directory)
	default:
		_, err = target.adopt(ctx, directory)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentFieldKindsProtectManagedHistoryAcrossAdapters(t *testing.T) {
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		t.Run(string(adapter), func(t *testing.T) {
			ctx := context.Background()
			test := fieldKindCases()[0]
			before, after := fieldKindManifest(t, fieldKindConfig(t, test.before)), fieldKindManifest(t, fieldKindConfig(t, test.after))
			definition := devRenameProjectOn(t, adapter, before)
			backend, url, path := fieldKindDatabase(t, adapter, definition, before)
			fieldKindSeed(t, backend, before, test.value)
			renames, printed := devRenamePrompt("clear\ncancel\n", url)
			renames.databasePath, renames.stillHas = path, nil
			renames.readBaseline = nil
			renames.output = newCLIOutput(printed, printed, cliOutputOptions{})
			target, err := renames.openTarget(ctx, adapter)
			if err != nil {
				t.Fatal(err)
			}
			defer target.close()
			fieldKindCommitBaseline(t, definition, before, target)
			stops := 0
			renames.stopServer = func() bool { stops++; return true }
			err = renames.resolveFieldKinds(ctx, definition, after, true, nil)
			if err == nil || !strings.Contains(err.Error(), "clearing is unavailable") || !strings.Contains(err.Error(), "immutable migration history") {
				t.Fatalf("managed rejection = %v\n%s", err, printed.String())
			}
			if stops != 0 || !strings.Contains(printed.String(), "1 current documents and 2 version snapshots") || strings.Contains(printed.String(), "Choose clear") {
				t.Fatalf("unsafe managed prompt: stops=%d\n%s", stops, printed.String())
			}
			reports, err := target.reviewFieldKinds(ctx, before, after)
			if err != nil {
				t.Fatal(err)
			}
			if err := target.clearFieldKinds(ctx, before, after, reports); err == nil || !strings.Contains(err.Error(), "managed by ridu migrate") {
				t.Fatalf("direct managed clear = %v", err)
			}
			afterReports, err := target.reviewFieldKinds(ctx, before, after)
			if err != nil || afterReports[0].Documents != 1 || afterReports[0].Snapshots != 2 {
				t.Fatalf("managed clear changed values: %#v, %v", afterReports, err)
			}
		})
	}
}

func TestDevelopmentFieldKindsProtectSnapshotOnlyValuesAcrossAdapters(t *testing.T) {
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		t.Run(string(adapter), func(t *testing.T) {
			ctx := context.Background()
			test := fieldKindCases()[1]
			before, after := fieldKindManifest(t, fieldKindConfig(t, test.before)), fieldKindManifest(t, fieldKindConfig(t, test.after))
			definition := devRenameProjectOn(t, adapter, before)
			backend, url, path := fieldKindDatabase(t, adapter, definition, before)
			id := fieldKindSeed(t, backend, before, test.value)
			transaction, err := backend.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conformance.LockedUpdate(ctx, transaction, store.UpdateRequest{Request: store.Request{Collection: fieldKindResource(t, before, "posts"), ID: id}, Values: store.Values{"body": store.Null()}}); err != nil {
				t.Fatal(err)
			}
			if err := transaction.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			renames, printed := devRenamePrompt("cancel\n", url)
			renames.databasePath, renames.stillHas = path, nil
			renames.readBaseline = nil
			renames.output = newCLIOutput(printed, printed, cliOutputOptions{})
			if err := renames.resolveFieldKinds(ctx, definition, after, true, nil); err == nil || !strings.Contains(err.Error(), "cancelled") {
				t.Fatalf("snapshot-only guard = %v", err)
			}
			if !strings.Contains(printed.String(), "0 current documents and 2 version snapshots") {
				t.Fatalf("snapshot-only prompt = %s", printed.String())
			}
		})
	}
}

func TestDevelopmentFieldKindClearRechecksCountsAndSource(t *testing.T) {
	ctx := context.Background()
	test := fieldKindCases()[1]
	before, after := fieldKindManifest(t, fieldKindConfig(t, test.before)), fieldKindManifest(t, fieldKindConfig(t, test.after))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
	backend, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
	fieldKindSeed(t, backend, before, test.value)
	renames, _ := devRenameSQLitePrompt("clear\ny\n", path)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	if err := renames.resolveFieldKinds(ctx, definition, after, true, func() bool { return false }); err != errDevelopmentSourceChanged || stops != 0 {
		t.Fatalf("stale clear = %v, stops=%d", err, stops)
	}
	target, err := renames.openTarget(ctx, projectfile.DatabaseSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer target.close()
	reports, err := target.reviewFieldKinds(ctx, before, after)
	if err != nil {
		t.Fatal(err)
	}
	reports[0].Snapshots--
	if err := target.clearFieldKinds(ctx, before, after, reports); err == nil || !strings.Contains(err.Error(), "counts") {
		t.Fatalf("changed counts = %v", err)
	}
	actual, err := target.reviewFieldKinds(ctx, before, after)
	if err != nil || actual[0].Documents != 1 || actual[0].Snapshots != 2 {
		t.Fatalf("failed count check cleared values: %#v, %v", actual, err)
	}
}

func TestDevelopmentFieldKindsNoSyncDoesNotBypassSafety(t *testing.T) {
	ctx := t.Context()
	test := fieldKindCases()[0]
	before, after := fieldKindManifest(t, fieldKindConfig(t, test.before)), fieldKindManifest(t, fieldKindConfig(t, test.after))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
	backend, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
	fieldKindSeed(t, backend, before, test.value)
	renames, _ := devRenameSQLitePrompt("clear\ny\n", path)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	if err := renames.resolveFieldKinds(ctx, definition, after, false, nil); err == nil || !strings.Contains(err.Error(), "--no-sync") {
		t.Fatalf("no-sync accepted incompatible values: %v", err)
	}
	if stops != 0 {
		t.Fatal("no-sync rejection stopped the accepted server")
	}
	reports, err := backend.(*sqlite.Store).ReviewDevelopmentFieldKinds(ctx, before, after)
	if err != nil || reports[0].Documents != 1 || reports[0].Snapshots != 2 {
		t.Fatalf("no-sync changed retained content: %#v, %v", reports, err)
	}
}

func TestDevelopmentFieldKindsRecheckValuesWrittenWhileOldServerDrains(t *testing.T) {
	for name, running := range map[string]bool{"running": true, "already stopped": false} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			test := fieldKindCases()[0]
			before, after := fieldKindManifest(t, fieldKindConfig(t, test.before)), fieldKindManifest(t, fieldKindConfig(t, test.after))
			definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
			backend, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
			renames, _ := devRenameWithoutTerminal("", path)
			stops := 0
			resumes := 0
			renames.stopServer = func() bool {
				stops++
				// A write arrives between the first review and the post-drain recheck.
				fieldKindSeed(t, backend, before, test.value)
				return running
			}
			renames.resumeServer = func() error {
				if stops != 1 {
					t.Fatal("accepted server resumed before draining")
				}
				resumes++
				return nil
			}
			if err := renames.resolveFieldKinds(ctx, definition, after, true, nil); err == nil || !strings.Contains(err.Error(), "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM") {
				t.Fatalf("late old-kind write was accepted: %v", err)
			}
			expectedResumes := 0
			if running {
				expectedResumes = 1
			}
			if stops != 1 || resumes != expectedResumes {
				t.Fatalf("old server drain/resume count = %d/%d", stops, resumes)
			}
			recorded, exists, err := renames.baseline(ctx, definition)
			if err != nil || !exists || !recorded.Equal(before) {
				t.Fatalf("race changed accepted schema: exists=%t, %v", exists, err)
			}
			reports, err := backend.(*sqlite.Store).ReviewDevelopmentFieldKinds(ctx, before, after)
			if err != nil || reports[0].Documents != 1 || reports[0].Snapshots != 2 {
				t.Fatalf("race changed retained content: %#v, %v", reports, err)
			}
		})
	}
}

// Generated output describes the candidate; only the database still knows
// the stored blocks schema. Without a terminal the reload is held.
func TestDevelopmentFieldKindsUseRecordedSchemaAfterGeneration(t *testing.T) {
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		t.Run(string(adapter), func(t *testing.T) {
			ctx := t.Context()
			test := fieldKindCases()[0]
			before := fieldKindManifest(t, fieldKindConfig(t, test.before))
			after := fieldKindManifest(t, fieldKindConfig(t, test.after))
			definition := devRenameProjectOn(t, adapter, after)
			backend, url, path := fieldKindDatabase(t, adapter, definition, before)
			fieldKindSeed(t, backend, before, test.value)
			renames, printed := devRenameWithoutTerminal(url, path)
			stops := 0
			renames.stopServer = func() bool { stops++; return true }
			err := renames.resolveFieldKinds(ctx, definition, after, true, nil)
			if err == nil || !strings.Contains(err.Error(), "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM") || !strings.Contains(err.Error(), "ridu migrate create --transform") || !strings.Contains(err.Error(), "versioned collections such as posts") {
				t.Fatalf("field-kind guard = %v\n%s", err, printed.String())
			}
			if stops != 0 || !strings.Contains(printed.String(), "1 current documents and 2 version snapshots") {
				t.Fatalf("guard changed server or omitted stored values: stops=%d\n%s", stops, printed.String())
			}
			target, err := renames.openTarget(ctx, adapter)
			if err != nil {
				t.Fatal(err)
			}
			defer target.close()
			recorded, exists, err := renames.baseline(ctx, definition)
			if err != nil || !exists || !recorded.Equal(before) {
				t.Fatalf("rejection changed recorded database schema: exists=%t, %v", exists, err)
			}
			reports, err := target.reviewFieldKinds(ctx, before, after)
			if err != nil || reports[0].Documents != 1 || reports[0].Snapshots != 2 {
				t.Fatalf("rejection changed stored values: %#v, %v", reports, err)
			}
		})
	}
}

// Empty changed fields need no review on any adapter. Synchronization then
// changes even a PostgreSQL scalar column type, which it cannot cast, and the
// candidate reads the collection.
func TestDevelopmentFieldKindsAcceptEmptyValuesAcrossAdapters(t *testing.T) {
	localization := core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		for _, test := range []struct {
			name          string
			before, after field.Node
			value         store.Value
			localized     bool
		}{
			{"scalar", field.Text("body"), field.Number("body"), store.Null(), false},
			{"localized", field.Text("body").Localized(), field.Number("body").Localized(), store.Object(store.Values{"en": store.Null(), "fr": store.Null()}), true},
		} {
			t.Run(string(adapter)+"/"+test.name, func(t *testing.T) {
				ctx := t.Context()
				beforeConfig, afterConfig := fieldKindConfig(t, test.before), fieldKindConfig(t, test.after)
				if test.localized {
					beforeConfig.Localization, afterConfig.Localization = localization, localization
				}
				before, after := fieldKindManifest(t, beforeConfig), fieldKindManifest(t, afterConfig)
				definition := devRenameProjectOn(t, adapter, before)
				backend, databaseURL, databasePath := fieldKindDatabase(t, adapter, definition, before)
				fieldKindSeed(t, backend, before, test.value)
				renames, printed := devRenameWithoutTerminal(databaseURL, databasePath)
				stops := 0
				renames.stopServer = func() bool { stops++; return true }
				if err := renames.resolveFieldKinds(ctx, definition, after, true, nil); err != nil {
					t.Fatalf("empty field blocked reload: %v\n%s", err, printed.String())
				}
				if stops != 1 || strings.Contains(printed.String(), "contain stored values") {
					t.Fatalf("empty values offered destructive recovery: stops=%d\n%s", stops, printed.String())
				}
				if _, err := synchronizeDevelopmentSchema(ctx, adapter, databaseURL, databasePath, true, true, developmentPreparation{manifest: after}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
					t.Fatalf("empty field change did not synchronize: %v", err)
				}
				recorded, exists, err := renames.baseline(ctx, definition)
				if err != nil || !exists || !recorded.Equal(after) {
					t.Fatalf("synchronization did not record candidate: %t, %v", exists, err)
				}
				application, err := core.New(afterConfig, backend)
				if err != nil {
					t.Fatal(err)
				}
				if page, err := application.Local().List(ctx, "posts", core.ListOptions{}); err != nil || len(page.Documents) != 1 {
					t.Fatalf("collection unreadable after empty change: %#v, %v", page, err)
				}
			})
		}
	}
}

// A synchronization that fails after the empty-value drain changed nothing,
// so ridu dev restarts the accepted server once; a later save may not.
func TestDevelopmentFieldKindDrainResumesAfterRejectedReload(t *testing.T) {
	ctx := t.Context()
	before, after := fieldKindManifest(t, fieldKindConfig(t, field.Text("body"))), fieldKindManifest(t, fieldKindConfig(t, field.Number("body")))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
	_, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
	renames, printed := devRenameWithoutTerminal("", path)
	resumes := 0
	renames.stopServer = func() bool { return true }
	renames.resumeServer = func() error { resumes++; return nil }
	if err := renames.resolveFieldKinds(ctx, definition, after, true, nil); err != nil {
		t.Fatal(err)
	}
	rejection := renames.resumeAfterRejectedReload(errors.New("apply development schema failed"))
	if resumes != 1 || !strings.Contains(rejection.Error(), "stored content remain unchanged") || !strings.Contains(printed.String(), "Restarted the accepted development server") {
		t.Fatalf("drained server was not resumed: resumes=%d, %v\n%s", resumes, rejection, printed.String())
	}
	if renames.resumeAfterRejectedReload(errors.New("later failure")); resumes != 1 {
		t.Fatal("a resumed server was started again")
	}
}

// Kinds sharing one stored string representation need no review when every
// stored value fits; a select only admits its options, so text → select does.
func TestDevelopmentFieldKindsAcceptCompatibleStoredValues(t *testing.T) {
	ctx := t.Context()
	before := fieldKindManifest(t, fieldKindConfig(t, field.Text("body")))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
	backend, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
	fieldKindSeed(t, backend, before, store.String("Plain summary text"))
	selected := fieldKindManifest(t, fieldKindConfig(t, field.Select("body", "one", "two")))
	renames, printed := devRenameSQLitePrompt("cancel\n", path)
	renames.output = newCLIOutput(printed, printed, cliOutputOptions{})
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	if err := renames.resolveFieldKinds(ctx, definition, selected, true, nil); err == nil || !strings.Contains(err.Error(), "cancelled") || !strings.Contains(printed.String(), "changes from text to select") {
		t.Fatalf("text → select admitted arbitrary strings: %v\n%s", err, printed.String())
	}
	textarea := fieldKindManifest(t, fieldKindConfig(t, field.Textarea("body")))
	if err := renames.resolveFieldKinds(ctx, definition, textarea, true, nil); err != nil || stops != 0 {
		t.Fatalf("text → textarea needed review: stops=%d, %v", stops, err)
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", path, true, true, developmentPreparation{manifest: textarea}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatalf("text → textarea did not synchronize: %v", err)
	}
	unversioned := func(node field.Node) schema.Manifest {
		return fieldKindManifest(t, core.Config{Name: "Compatible", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{node}}}})
	}
	previous := unversioned(field.Text("body"))
	if _, err := postgres.BuildArtifact(ctx, "textarea", &previous, unversioned(field.Textarea("body")), nil, false); err != nil {
		t.Fatalf("PostgreSQL migration creation refused text → textarea: %v", err)
	}
}

// Clearing a rich-text value to repair one embedded block field would discard
// the whole value, so ridu dev offers no clear and keeps everything.
func TestDevelopmentFieldKindsRefuseEmbeddedPayloadClear(t *testing.T) {
	ctx := t.Context()
	config := func(level field.Node) core.Config {
		return fieldKindConfig(t, richtext.Field("body", richtext.Config{Blocks: []field.Block{{Slug: "callout", Fields: field.Fields{level}}}}))
	}
	before, after := fieldKindManifest(t, config(field.Text("level"))), fieldKindManifest(t, config(field.Number("level")))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
	backend, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
	body := store.Object(store.Values{"version": store.Number(1), "root": store.Object(store.Values{"type": store.String("root"), "children": store.List(
		store.Object(store.Values{"type": store.String("block"), "version": store.Number(1), "fields": store.Object(store.Values{"blockType": store.String("callout"), "_key": store.String("k1"), "level": store.String("high")})}),
	)})})
	fieldKindSeed(t, backend, before, body)
	renames, printed := devRenameSQLitePrompt("clear\ny\n", path)
	renames.output = newCLIOutput(printed, printed, cliOutputOptions{})
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	err := renames.resolveFieldKinds(ctx, definition, after, true, nil)
	if err == nil || !strings.Contains(err.Error(), "clearing is unavailable") || !strings.Contains(err.Error(), "restore the previous embedded field kind") {
		t.Fatalf("embedded change = %v\n%s", err, printed.String())
	}
	if stops != 0 || !strings.Contains(printed.String(), "posts.body embedded field callout.level changes from text to number; 1 current documents and 2 version snapshots") || strings.Contains(printed.String(), "Choose clear") {
		t.Fatalf("embedded change prompt: stops=%d\n%s", stops, printed.String())
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", path, true, true, developmentPreparation{manifest: after}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err == nil || !strings.Contains(err.Error(), "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM") {
		t.Fatalf("synchronization certified an embedded kind change over stored values: %v", err)
	}
}

// Clearing a field that stays required cannot succeed, so it is never offered
// and the accepted server keeps running. A clear that fails for any other
// reason restarts the drained server.
func TestDevelopmentFieldKindClearProtectsTheAcceptedServer(t *testing.T) {
	ctx := t.Context()
	required := func(node field.Node) schema.Manifest { return fieldKindManifest(t, fieldKindConfig(t, node)) }
	before, after := required(field.Text("body").Required()), required(field.Number("body").Required())
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, before)
	backend, _, path := fieldKindDatabase(t, projectfile.DatabaseSQLite, definition, before)
	fieldKindSeed(t, backend, before, store.String("not a number"))
	renames, _ := devRenameSQLitePrompt("clear\ny\n", path)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	if err := renames.resolveFieldKinds(ctx, definition, after, true, nil); err == nil || !strings.Contains(err.Error(), "stays required") || stops != 0 {
		t.Fatalf("required clear = %v, stops=%d", err, stops)
	}

	optional := required(field.Number("body"))
	renames, _ = devRenameSQLitePrompt("clear\ny\n", path)
	resumes := 0
	renames.stopServer = func() bool {
		// A write between review and clearing changes the confirmed counts.
		transaction, err := backend.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Create(ctx, store.CreateRequest{Collection: fieldKindResource(t, before, "posts"), ID: "late", Values: store.Values{"body": store.String("late"), "summary": store.String("Late")}}); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return true
	}
	renames.resumeServer = func() error { resumes++; return nil }
	if err := renames.resolveFieldKinds(ctx, definition, optional, true, nil); err == nil || !strings.Contains(err.Error(), "schema and content remain unchanged") || resumes != 1 {
		t.Fatalf("failed clear = %v, resumes=%d", err, resumes)
	}
	reports, err := backend.(*sqlite.Store).ReviewDevelopmentFieldKinds(ctx, before, optional)
	if err != nil || reports[0].Documents != 2 {
		t.Fatalf("failed clear changed values: %#v, %v", reports, err)
	}
}

// The same live-database proof covers all three issue reproductions on every
// official adapter. Cancellation and task-runner reloads preserve the accepted
// schema; clearing covers both snapshots and leaves unrelated values readable.
func TestDevelopmentFieldKindsReviewAndClearAcrossAdapters(t *testing.T) {
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		for _, test := range fieldKindCases() {
			t.Run(string(adapter)+"/"+test.name, func(t *testing.T) {
				ctx := context.Background()
				beforeConfig, afterConfig := fieldKindConfig(t, test.before), fieldKindConfig(t, test.after)
				before, after := fieldKindManifest(t, beforeConfig), fieldKindManifest(t, afterConfig)
				definition := devRenameProjectOn(t, adapter, before)
				databaseURL, databasePath := "", ""
				var backend store.Store
				switch adapter {
				case projectfile.DatabaseSQLite:
					databasePath = devRenameSQLite(t, definition, before)
					backend = devRenameSQLiteStore(t, databasePath)
				case projectfile.DatabasePostgres:
					url, postgresBackend := devRenameDatabase(t)
					databaseURL, backend = url, postgresBackend
					if err := postgresBackend.SyncDevelopmentSchema(ctx, before); err != nil {
						t.Fatal(err)
					}
				case projectfile.DatabaseMongoDB:
					databaseURL = devRenameMongoDB(t)
					mongoBackend := devRenameMongoDBStore(t, databaseURL)
					backend = mongoBackend
					if err := mongoBackend.SyncDevelopmentSchema(ctx, before); err != nil {
						t.Fatal(err)
					}
				}
				id := fieldKindSeed(t, backend, before, test.value)
				renames, printed := devRenamePrompt("cancel\n", databaseURL)
				renames.output = newCLIOutput(printed, printed, cliOutputOptions{})
				renames.databasePath, renames.stillHas = databasePath, nil
				renames.readBaseline = nil
				stops := 0
				renames.stopServer = func() bool { stops++; return true }
				if err := renames.resolveFieldKinds(ctx, definition, after, true, nil); err == nil || !strings.Contains(err.Error(), "cancelled") {
					t.Fatalf("cancel = %v\n%s", err, printed.String())
				}
				if stops != 0 || !strings.Contains(printed.String(), "1 current documents and 2 version snapshots") {
					t.Fatalf("cancel stopped server or omitted counts: %d\n%s", stops, printed.String())
				}
				clear, prompt := devRenamePrompt("clear\n\nmaybe\ny\n", databaseURL)
				clear.databasePath, clear.stillHas = databasePath, nil
				clear.readBaseline = nil
				clear.stopServer = func() bool { stops++; return true }
				if err := clear.resolveFieldKinds(ctx, definition, after, true, nil); err != nil {
					t.Fatalf("clear = %v\n%s", err, prompt.String())
				}
				if stops != 1 {
					t.Fatalf("clear stopped server %d times", stops)
				}
				if !strings.Contains(prompt.String(), "Answer y to permanently remove") || strings.Contains(prompt.String(), "move the existing data to the new name") {
					t.Fatalf("clear confirmation used rename semantics: %s", prompt.String())
				}
				if _, err := synchronizeDevelopmentSchema(ctx, adapter, databaseURL, databasePath, true, true, developmentPreparation{manifest: after}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
					t.Fatal(err)
				}
				application, err := core.New(afterConfig, backend)
				if err != nil {
					t.Fatal(err)
				}
				page, err := application.Local().List(ctx, "posts", core.ListOptions{})
				if err != nil || len(page.Documents) != 1 {
					t.Fatalf("collection unreadable after recovery: %#v, %v", page, err)
				}
				transaction, err := backend.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer transaction.Rollback(ctx)
				resource := fieldKindResource(t, after, "posts")
				versions, err := transaction.(store.VersionTransaction).ListVersions(ctx, store.VersionRequest{Collection: resource, DocumentID: id})
				if err != nil || len(versions) != 2 {
					t.Fatalf("version history = %#v, %v", versions, err)
				}
				for _, version := range versions {
					if value, exists := version.Snapshot.Values["body"]; exists && value.Kind() != store.ValueNull {
						t.Fatal("snapshot retains incompatible value")
					}
					if summary, _ := version.Snapshot.Values["summary"].StringValue(); summary != "Untouched" {
						t.Fatal("clear changed unrelated snapshot property")
					}
				}
			})
		}
	}
}

// Stored values say nothing about the schema that wrote them, so ridu dev
// refuses a PostgreSQL database with tables but no last-synchronized schema
// record instead of trusting a config its physical schema happens to match.
func TestDevelopmentFieldKindsRefuseUnrecordedDatabase(t *testing.T) {
	ctx := t.Context()
	before := fieldKindManifest(t, fieldKindConfig(t, field.Text("body")))
	definition := devRenameProjectOn(t, projectfile.DatabasePostgres, before)
	_, databaseURL, _ := fieldKindDatabase(t, projectfile.DatabasePostgres, definition, before)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `DROP TABLE ridu_postgres_schema`); err != nil {
		t.Fatal(err)
	}
	renames, printed := devRenameWithoutTerminal(databaseURL, "")
	if err := renames.resolveFieldKinds(ctx, definition, before, true, nil); err == nil || !strings.Contains(err.Error(), "RIDU_DEVELOPMENT_SCHEMA_UNKNOWN") {
		t.Fatalf("unrecorded database was accepted: %v\n%s", err, printed.String())
	}
	var recorded bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ridu_postgres_schema') IS NOT NULL`).Scan(&recorded); err != nil || recorded {
		t.Fatalf("refused reload wrote a schema record: %t, %v", recorded, err)
	}
}

// Offline migration creation cannot know that a field address has no values.
// Even destructive approval must never authorize a schema-only reinterpretation.
func TestFieldKindMigrationCreationRequiresRecoveryAcrossAdapters(t *testing.T) {
	additive := "supports only additive transitions"
	expected := map[projectfile.DatabaseAdapter]map[string]string{
		projectfile.DatabaseSQLite:  {"blocks-to-richtext": "changed its envelope", "text-to-number": additive, "relationship-to-upload": additive, "select-to-multiselect": additive, "multiselect-to-select": additive},
		projectfile.DatabaseMongoDB: {"blocks-to-richtext": "changed its envelope", "text-to-number": additive, "relationship-to-upload": additive, "select-to-multiselect": additive, "multiselect-to-select": additive},
		projectfile.DatabasePostgres: {
			"blocks-to-richtext": "changed its envelope", "relationship-to-upload": "cannot change the reference kind",
			"text-to-number": "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM", "select-to-multiselect": "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM", "multiselect-to-select": "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM",
		},
	}
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		for _, test := range fieldKindCases() {
			t.Run(string(adapter)+"/"+test.name, func(t *testing.T) {
				ctx := context.Background()
				before, after := fieldKindManifest(t, fieldKindConfig(t, test.before)), fieldKindManifest(t, fieldKindConfig(t, test.after))
				directory := t.TempDir()
				var err error
				switch adapter {
				case projectfile.DatabaseSQLite:
					_, err = sqlite.CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false)
					if err != nil {
						t.Fatal(err)
					}
					_, err = sqlite.CreateArtifact(ctx, directory, "change-kind", after, time.Unix(2, 0), true)
				case projectfile.DatabaseMongoDB:
					_, err = mongodb.CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), mongodb.ArtifactOptions{})
					if err != nil {
						t.Fatal(err)
					}
					_, err = mongodb.CreateArtifact(ctx, directory, "change-kind", after, time.Unix(2, 0), mongodb.ArtifactOptions{AllowDestructive: true})
				case projectfile.DatabasePostgres:
					_, err = postgres.BuildArtifact(ctx, "change-kind", &before, after, nil, true)
				}
				if err == nil || !strings.Contains(err.Error(), expected[adapter][test.name]) {
					t.Fatalf("schema-only incompatible field migration = %v", err)
				}
				files, readErr := migrationartifact.ReadAll(directory)
				if readErr != nil || len(files) > 1 {
					t.Fatalf("rejected creation published history: %v, %v", files, readErr)
				}
			})
		}
	}
}

// A registered transform admits a kind change of an unversioned collection.
// Versioned collections and PostgreSQL column casts it cannot run stay refused.
func TestFieldKindMigrationCreationAdmitsTransforms(t *testing.T) {
	ctx := t.Context()
	descriptor := migration.DataTransformDescriptor{Name: "convert-body", Checksum: migration.DataTransformChecksum([]byte("convert-body-v1"))}
	create := func(adapter projectfile.DatabaseAdapter, before, after schema.Manifest) error {
		directory := t.TempDir()
		switch adapter {
		case projectfile.DatabaseSQLite:
			if _, err := sqlite.CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), false); err != nil {
				t.Fatal(err)
			}
			_, err := sqlite.CreateArtifact(ctx, directory, "convert", after, time.Unix(2, 0), true, descriptor)
			return err
		case projectfile.DatabaseMongoDB:
			if _, err := mongodb.CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), mongodb.ArtifactOptions{}); err != nil {
				t.Fatal(err)
			}
			_, err := mongodb.CreateArtifact(ctx, directory, "convert", after, time.Unix(2, 0), mongodb.ArtifactOptions{AllowDestructive: true, DataTransforms: []migration.DataTransformDescriptor{descriptor}})
			return err
		default:
			artifact, err := postgres.BuildArtifact(ctx, "convert", &before, after, nil, true, descriptor)
			if err != nil {
				return err
			}
			steps := artifact.Phases[len(artifact.Phases)-1].Steps
			if len(steps) < 2 || steps[len(steps)-2].Kind != migration.StepDataTransform || steps[len(steps)-1].Kind != migration.StepAssertSchema {
				t.Fatalf("transform was not bound once before the final assertion: %#v", steps)
			}
			return nil
		}
	}
	paragraphs := field.Blocks("body", field.Block{Slug: "paragraph", Fields: field.Fields{field.Text("content")}})
	for _, adapter := range []projectfile.DatabaseAdapter{projectfile.DatabaseSQLite, projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		for _, versions := range []bool{false, true} {
			resolve := func(body field.Node) schema.Manifest {
				return fieldKindManifest(t, core.Config{Name: "Convert", Plugins: []core.Plugin{richtext.New()}, Collections: []core.Collection{{Slug: "posts", Versions: versions, Fields: field.Fields{body}}}})
			}
			err := create(adapter, resolve(paragraphs), resolve(richtext.Field("body")))
			if versions != (err != nil) || versions && !strings.Contains(err.Error(), "versioned") {
				t.Fatalf("%s versions=%t blocks → rich text with transform = %v", adapter, versions, err)
			}
		}
	}
	unversioned := func(body field.Node) schema.Manifest {
		return fieldKindManifest(t, core.Config{Name: "Convert", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{body}}}})
	}
	if err := create(projectfile.DatabasePostgres, unversioned(field.Text("body")), unversioned(field.Number("body"))); err == nil || !strings.Contains(err.Error(), "cannot convert posts.body from text to double precision in place") {
		t.Fatalf("PostgreSQL planned a column cast its runner cannot apply: %v", err)
	}
}
