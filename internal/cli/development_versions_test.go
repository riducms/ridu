package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/enableversions"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func devVersionsConfig(versions, drafts bool, fields ...field.Node) core.Config {
	if len(fields) == 0 {
		fields = []field.Node{field.Text("title")}
	}
	return core.Config{Name: "Versions", Collections: []core.Collection{{
		Slug: "posts", Versions: versions, VersionConfig: core.VersionConfig{Drafts: drafts}, Fields: fields,
	}}}
}

// devVersionsSQLite is a SQLite project whose committed history and
// development database both have the unversioned posts collection.
func devVersionsSQLite(t *testing.T) (projectfile.File, string, *sqlite.Store, schema.Manifest) {
	t.Helper()
	previous := devRenameManifest(t, devVersionsConfig(false, false))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	if _, err := sqlite.CreateArtifact(context.Background(), definition.Absolute(definition.Migrations), "initial", previous, time.Now(), sqlite.ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	databasePath := devRenameSQLite(t, definition, previous)
	return definition, databasePath, devRenameSQLiteStore(t, databasePath), previous
}

func devVersionsCreatePost(t *testing.T, backend *sqlite.Store) store.Document {
	t.Helper()
	application, err := core.New(devVersionsConfig(false, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Hello")}, core.MutationOptions{System: true})
	if err != nil {
		t.Fatal(err)
	}
	return post
}

// While the collection stores nothing, enabling versions needs no decision:
// nothing is asked, no migration is written and schema sync enables them.
func TestDevelopmentVersionsOnAnEmptyCollectionNeedNoDecision(t *testing.T) {
	ctx := context.Background()
	definition, databasePath, _, _ := devVersionsSQLite(t)
	current := devRenameManifest(t, devVersionsConfig(true, true))
	renames, prompt := devRenameSQLitePrompt("", databasePath)
	if err := renames.resolveVersions(ctx, definition, current, nil); err != nil {
		t.Fatalf("enabling versions on an empty collection = %v\n%s", err, prompt.String())
	}
	if prompt.Len() != 0 {
		t.Fatalf("an empty collection was asked about: %s", prompt.String())
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial" {
		t.Fatalf("migrations = %v", names)
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatal(err)
	}
}

// Over stored documents ridu dev asks what they become, records the answer in
// a migration for other databases, and converts the development documents
// directly, so the database stays under ridu dev.
func TestDevelopmentVersionsAskAndMigrateStoredSQLiteDocuments(t *testing.T) {
	for _, existing := range []migration.ExistingDocuments{migration.ExistingPublished, migration.ExistingDraft} {
		t.Run(string(existing), func(t *testing.T) {
			ctx := context.Background()
			definition, databasePath, backend, _ := devVersionsSQLite(t)
			post := devVersionsCreatePost(t, backend)
			versioned := devVersionsConfig(true, true)
			current := devRenameManifest(t, versioned)
			// An empty line is not an answer, and an unknown one is asked again.
			renames, prompt := devRenameSQLitePrompt("\nmaybe\n"+string(existing)+"\n\n", databasePath)
			stops := 0
			renames.stopServer = func() bool {
				stops++
				return true
			}
			if err := renames.resolveVersions(ctx, definition, current, nil); err != nil {
				t.Fatalf("enabling versions over a stored post = %v\n%s", err, prompt.String())
			}
			if !strings.Contains(prompt.String(), `Collection "posts" starts keeping versions and stores 1 document here`) ||
				!strings.Contains(prompt.String(), "Enter published, draft or cancel.") {
				t.Fatalf("prompt = %s", prompt.String())
			}
			if stops != 1 {
				t.Fatalf("the running server was stopped %d times", stops)
			}
			if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,enable-versions-posts" {
				t.Fatalf("migrations = %v", names)
			}
			if managed, err := backend.HasMigrationHistory(ctx); err != nil || managed {
				t.Fatalf("enabling versions handed the development database to ridu migrate: %t, %v", managed, err)
			}
			if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
				t.Fatal(err)
			}
			application, err := core.New(versioned, backend)
			if err != nil {
				t.Fatal(err)
			}
			published := false
			found, err := application.Local().Find(ctx, "posts", post.ID, core.FindOptions{Draft: &published})
			switch existing {
			case migration.ExistingPublished:
				if title, _ := found.Values["title"].StringValue(); err != nil || title != "Hello" {
					t.Fatalf("published post = %#v, %v", found, err)
				}
			case migration.ExistingDraft:
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("published read of a draft post = %#v, %v", found, err)
				}
			}

			// The migration carries the same choice to a deployed database.
			deployed := devRenameSQLiteStore(t, filepath.Join(t.TempDir(), "deployed.sqlite"))
			if err := deployed.ApplyArtifacts(ctx, definition.Absolute(definition.Migrations)); err != nil {
				t.Fatal(err)
			}
			if err := deployed.Ready(ctx, current); err != nil {
				t.Fatalf("the written migrations do not reach the new config: %v", err)
			}
		})
	}
}

// Without a terminal, or after cancel, the accepted schema and the stored
// documents stay as they were and nothing is written.
func TestDevelopmentVersionsWaitWithoutADecision(t *testing.T) {
	ctx := context.Background()
	current := devRenameManifest(t, devVersionsConfig(true, true))
	for name, session := range map[string]func(string) (*developmentRenames, *bytes.Buffer){
		"no terminal": func(databasePath string) (*developmentRenames, *bytes.Buffer) {
			return devRenameWithoutTerminal("", databasePath)
		},
		"cancel": func(databasePath string) (*developmentRenames, *bytes.Buffer) {
			return devRenameSQLitePrompt("cancel\n", databasePath)
		},
	} {
		t.Run(name, func(t *testing.T) {
			definition, databasePath, backend, previous := devVersionsSQLite(t)
			devVersionsCreatePost(t, backend)
			renames, prompt := session(databasePath)
			err := renames.resolveVersions(ctx, definition, current, nil)
			var held developmentRenameHeldError
			switch name {
			case "no terminal":
				if !errors.As(err, &held) || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_DOCUMENTS") {
					t.Fatalf("enabling versions without a terminal = %v", err)
				}
			case "cancel":
				if err == nil || !strings.Contains(err.Error(), "cancelled") {
					t.Fatalf("cancelled choice = %v\n%s", err, prompt.String())
				}
			}
			if names := migrationNames(t, definition); strings.Join(names, ",") != "initial" {
				t.Fatalf("migrations = %v", names)
			}
			if recorded, exists, err := backend.DevelopmentManifest(ctx); err != nil || !exists || !recorded.Equal(previous) {
				t.Fatalf("the development schema changed: exists=%t, %v", exists, err)
			}
			if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_EXISTING_DOCUMENTS") {
				t.Fatalf("schema sync over the undecided post = %v", err)
			}
		})
	}
}

// A rename and converting stored documents cannot share one migration, so
// they are settled in separate saves. Without stored documents there is
// nothing to decide, and the rename prompt still gets its turn.
func TestDevelopmentVersionsRefuseASaveWithARename(t *testing.T) {
	ctx := context.Background()
	definition, databasePath, backend, _ := devVersionsSQLite(t)
	current := devRenameManifest(t, devVersionsConfig(true, true, field.Text("headline")))
	renames, prompt := devRenameSQLitePrompt("", databasePath)
	if err := renames.resolveVersions(ctx, definition, current, nil); err != nil || prompt.Len() != 0 {
		t.Fatalf("versions on an empty collection beside a possible rename = %v\n%s", err, prompt.String())
	}
	devVersionsCreatePost(t, backend)
	renames, _ = devRenameSQLitePrompt("published\n\n", databasePath)
	if err := renames.resolveVersions(ctx, definition, current, nil); err == nil || !strings.Contains(err.Error(), "separate saves") {
		t.Fatalf("versions over a stored post and a rename in one save = %v", err)
	}
}

// Schema sync enabled versions on an empty collection without a migration.
// The migration ridu dev later writes for the changes no migration covers
// records require-empty for it, which every database can run.
func TestDevelopmentVersionsRecordWhatSchemaSyncAlreadyEnabled(t *testing.T) {
	ctx := context.Background()
	config := func(pages, posts bool) core.Config {
		return core.Config{Name: "Versions", Collections: []core.Collection{
			{Slug: "pages", Versions: pages, Fields: field.Fields{field.Text("title")}},
			{Slug: "posts", Versions: posts, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title")}},
		}}
	}
	committed := devRenameManifest(t, config(false, false))
	synced := devRenameManifest(t, config(true, false))
	current := devRenameManifest(t, config(true, true))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, committed)
	directory := definition.Absolute(definition.Migrations)
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", committed, time.Now(), sqlite.ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	databasePath := devRenameSQLite(t, definition, committed)
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: synced}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatal(err)
	}
	backend := devRenameSQLiteStore(t, databasePath)
	application, err := core.New(config(true, false), backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, core.MutationOptions{System: true}); err != nil {
		t.Fatal(err)
	}

	renames, prompt := devRenameSQLitePrompt("draft\n\n", databasePath)
	if err := renames.resolveVersions(ctx, definition, current, nil); err != nil {
		t.Fatalf("enabling versions over a stored post = %v\n%s", err, prompt.String())
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[1].Artifact.Name != "changes-before-enable-versions-posts" {
		t.Fatalf("migrations = %v", migrationNames(t, definition))
	}
	pages, posts := current.Snapshot().Collections[0], current.Snapshot().Collections[1]
	earlier, err := enableversions.Recorded(files[1].Artifact)
	if err != nil || len(earlier) != 1 || earlier[pages.ID] != migration.ExistingRequireEmpty {
		t.Fatalf("choices recorded for what schema sync enabled = %#v, %v", earlier, err)
	}
	chosen, err := enableversions.Recorded(files[2].Artifact)
	if err != nil || len(chosen) != 1 || chosen[posts.ID] != migration.ExistingDraft {
		t.Fatalf("choices recorded for the stored post = %#v, %v", chosen, err)
	}
	deployed := devRenameSQLiteStore(t, filepath.Join(t.TempDir(), "deployed.sqlite"))
	if err := deployed.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := deployed.Ready(ctx, current); err != nil {
		t.Fatalf("the written migrations do not reach the new config: %v", err)
	}
}

func TestChooseExistingDocumentsFromTheFlagOrAPrompt(t *testing.T) {
	previous := devRenameManifest(t, devVersionsConfig(false, false))
	withDrafts := devRenameManifest(t, devVersionsConfig(true, true))
	withoutDrafts := devRenameManifest(t, devVersionsConfig(true, false))
	postsID := withDrafts.Snapshot().Collections[0].ID

	choices, err := chooseExistingDocuments(previous, true, withDrafts, "draft", nil, io.Discard)
	if err != nil || len(choices) != 1 || choices[postsID] != migration.ExistingDraft {
		t.Fatalf("flag choice = %#v, %v", choices, err)
	}
	if _, err := chooseExistingDocuments(previous, true, withoutDrafts, "draft", nil, io.Discard); err == nil || !strings.Contains(err.Error(), "does not enable drafts") {
		t.Fatalf("draft flag without drafts = %v", err)
	}
	if _, err := chooseExistingDocuments(previous, true, withDrafts, "keep", nil, io.Discard); err == nil || !strings.Contains(err.Error(), "--versions-existing") {
		t.Fatalf("unknown flag value = %v", err)
	}
	if _, err := chooseExistingDocuments(previous, true, previous, "published", nil, io.Discard); err == nil || !strings.Contains(err.Error(), "no collection or global starts keeping versions") {
		t.Fatalf("flag without enabled versions = %v", err)
	}
	if choices, err := chooseExistingDocuments(previous, true, withDrafts, "", nil, io.Discard); err != nil || choices != nil {
		t.Fatalf("no flag and no input = %#v, %v", choices, err)
	}

	var output bytes.Buffer
	choices, err = chooseExistingDocuments(previous, true, withoutDrafts, "", strings.NewReader("draft\n\n"), &output)
	if err != nil || choices[postsID] != migration.ExistingPublished {
		t.Fatalf("prompted choice = %#v, %v\n%s", choices, err, output.String())
	}
	if !strings.Contains(output.String(), "does not enable drafts") || strings.Contains(output.String(), "  draft ") {
		t.Fatalf("prompt without drafts = %s", output.String())
	}
	if _, err := chooseExistingDocuments(previous, true, withDrafts, "", strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "--versions-existing") {
		t.Fatalf("prompt without an answer = %v", err)
	}
}

// On PostgreSQL and MongoDB the answer goes through the migration runner, as
// ridu migrate up --allow-maintenance would after the application stopped.
func TestDevelopmentVersionsMigrateStoredDocumentsThroughTheRunner(t *testing.T) {
	versioned := devVersionsConfig(true, true)
	for _, database := range []projectfile.DatabaseAdapter{projectfile.DatabasePostgres, projectfile.DatabaseMongoDB} {
		t.Run(string(database), func(t *testing.T) {
			ctx := context.Background()
			previous := devRenameManifest(t, devVersionsConfig(false, false))
			current := devRenameManifest(t, versioned)
			definition := devRenameProjectOn(t, database, previous)
			directory := definition.Absolute(definition.Migrations)
			var databaseURL string
			var backend store.Store
			var statuses func() ([]bool, error)
			switch database {
			case projectfile.DatabasePostgres:
				url, postgresBackend := devRenameDatabase(t)
				databaseURL, backend = url, postgresBackend
				initial, err := postgres.BuildArtifact(ctx, "initial", nil, previous, postgres.ArtifactOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := migrationartifact.Create(directory, "initial", initial, time.Now()); err != nil {
					t.Fatal(err)
				}
				if err := postgresBackend.SyncDevelopmentSchema(ctx, previous); err != nil {
					t.Fatal(err)
				}
				statuses = func() ([]bool, error) {
					recorded, err := postgresBackend.ArtifactStatus(ctx, directory)
					applied := make([]bool, len(recorded))
					for index, status := range recorded {
						applied[index] = status.Applied
					}
					return applied, err
				}
			case projectfile.DatabaseMongoDB:
				databaseURL = devRenameMongoDB(t)
				if _, err := mongodb.CreateArtifact(ctx, directory, "initial", previous, time.Now(), mongodb.ArtifactOptions{}); err != nil {
					t.Fatal(err)
				}
				if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseMongoDB, databaseURL, "", true, true, developmentPreparation{manifest: previous}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
					t.Fatal(err)
				}
				mongoBackend := devRenameMongoDBStore(t, databaseURL)
				if err := mongoBackend.VerifyIndexes(ctx, previous); err != nil {
					t.Fatal(err)
				}
				backend = mongoBackend
				statuses = func() ([]bool, error) {
					recorded, err := devRenameMongoDBStore(t, databaseURL).ArtifactStatus(ctx, directory)
					applied := make([]bool, len(recorded))
					for index, status := range recorded {
						applied[index] = status.Applied
					}
					return applied, err
				}
			}
			original, err := core.New(devVersionsConfig(false, false), backend)
			if err != nil {
				t.Fatal(err)
			}
			post, err := original.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, core.MutationOptions{System: true})
			if err != nil {
				t.Fatal(err)
			}

			renames, prompt := devRenamePrompt("published\n\n", databaseURL)
			stops := 0
			renames.stopServer = func() bool {
				stops++
				return true
			}
			if err := renames.resolveVersions(ctx, definition, current, nil); err != nil {
				t.Fatalf("enabling versions over a stored post = %v\n%s", err, prompt.String())
			}
			if stops != 1 {
				t.Fatalf("the running server was stopped %d times", stops)
			}
			if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,enable-versions-posts" {
				t.Fatalf("migrations = %v", names)
			}
			applied, err := statuses()
			if err != nil || len(applied) != 2 || !applied[0] || !applied[1] {
				t.Fatalf("statuses after enabling versions = %v, %v", applied, err)
			}
			// ridu dev keeps synchronizing the migrated database.
			if _, err := synchronizeDevelopmentSchema(ctx, database, databaseURL, "", true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
				t.Fatal(err)
			}
			if database == projectfile.DatabaseMongoDB {
				// A MongoDB store serves only the indexes it verified.
				migrated := devRenameMongoDBStore(t, databaseURL)
				if err := migrated.VerifyIndexes(ctx, current); err != nil {
					t.Fatal(err)
				}
				backend = migrated
			}
			application, err := core.New(versioned, backend)
			if err != nil {
				t.Fatal(err)
			}
			published := false
			found, err := application.Local().Find(ctx, "posts", post.ID, core.FindOptions{Draft: &published})
			if title, _ := found.Values["title"].StringValue(); err != nil || title != "Hello" {
				t.Fatalf("published post = %#v, %v", found, err)
			}
			versions, err := application.Local().Versions(ctx, "posts", post.ID, core.FindOptions{System: true})
			if err != nil || len(versions) != 1 || versions[0].Status != store.StatusPublished {
				t.Fatalf("versions = %#v, %v", versions, err)
			}
		})
	}
}
