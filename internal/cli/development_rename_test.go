package cli

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func devRenameConfig(fields ...field.Node) core.Config {
	return core.Config{Name: "Rename", Plugins: []core.Plugin{richtext.New()}, Collections: []core.Collection{{Slug: "posts", Fields: fields}}}
}

func devRenameManifest(t *testing.T, config core.Config) schema.Manifest {
	t.Helper()
	app, err := core.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return app.Manifest()
}

// devRenameProject supplies generated output independently of database state.
func devRenameProject(t *testing.T, generated schema.Manifest) projectfile.File {
	t.Helper()
	return devRenameProjectOn(t, projectfile.DatabasePostgres, generated)
}

func devRenameProjectOn(t *testing.T, database projectfile.DatabaseAdapter, generated schema.Manifest) projectfile.File {
	t.Helper()
	definition := projectfile.File{Root: t.TempDir(), Database: database, Schema: "generated/ridu.schema.json", Migrations: "migrations"}
	encoded, err := generated.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	path := definition.Absolute(definition.Schema)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return definition
}

// devRenamePrompt is a terminal session with answers already typed. Without
// a database URL there is no database to inspect, so the rename is taken to
// be waiting; tests over a real database use the real check.
func devRenamePrompt(answers string, databaseURL string) (*developmentRenames, *bytes.Buffer) {
	var prompt bytes.Buffer
	renames := &developmentRenames{
		input: bufio.NewReader(strings.NewReader(answers)), prompt: &prompt, databaseURL: databaseURL,
		output: newCLIOutput(&prompt, io.Discard, cliOutputOptions{}), now: time.Now,
	}
	if databaseURL == "" {
		renames.stillHas = func(context.Context, schema.Manifest, schema.Manifest) (bool, error) { return true, nil }
		var captured *schema.Manifest
		renames.readBaseline = func(_ context.Context, definition projectfile.File) (schema.Manifest, bool, error) {
			if captured != nil {
				return *captured, true, nil
			}
			manifest, exists, err := schemadiff.ReadManifest(definition.Absolute(definition.Schema))
			if err == nil && exists {
				captured = &manifest
			}
			return manifest, exists, err
		}
	}
	return renames, &prompt
}

// devRenameSQLitePrompt is the same session over a SQLite development file.
func devRenameSQLitePrompt(answers string, databasePath string) (*developmentRenames, *bytes.Buffer) {
	renames, prompt := devRenamePrompt(answers, "")
	renames.databasePath, renames.stillHas = databasePath, nil
	renames.readBaseline = nil
	return renames, prompt
}

// devRenameWithoutTerminal is ridu dev under a task runner or a coding agent:
// it can detect a rename but cannot ask about it.
func devRenameWithoutTerminal(databaseURL, databasePath string) (*developmentRenames, *bytes.Buffer) {
	var printed bytes.Buffer
	return &developmentRenames{
		prompt: &printed, databaseURL: databaseURL, databasePath: databasePath,
		output: newCLIOutput(&printed, &printed, cliOutputOptions{}), now: time.Now,
	}, &printed
}

func migrationNames(t *testing.T, definition projectfile.File) []string {
	t.Helper()
	files, err := migrationartifact.ReadAll(definition.Absolute(definition.Migrations))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.Artifact.Name
	}
	return names
}

// ridu dev only asks when someone can answer: in a terminal. A rename also
// needs a migrations directory to record the answer, so without one it is
// held instead, while a field-kind review still gets its prompt.
func TestDevelopmentRenamePromptNeedsAnInteractiveSession(t *testing.T) {
	reporter := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	interactive := Options{Interactive: true, Stdin: strings.NewReader("")}
	for _, database := range []projectfile.DatabaseAdapter{projectfile.DatabasePostgres, projectfile.DatabaseSQLite, projectfile.DatabaseMongoDB} {
		for name, definition := range map[string]projectfile.File{
			"migrations":    {Database: database, Migrations: "migrations"},
			"no migrations": {Database: database},
		} {
			if newDevelopmentRenames(definition, interactive, "", "", io.Discard, reporter).input == nil {
				t.Errorf("an interactive %s session with %s got no prompt", database, name)
			}
		}
		project := projectfile.File{Database: database, Migrations: "migrations"}
		for name, options := range map[string]Options{
			"no terminal": {Stdin: strings.NewReader("")},
			"no input":    {Interactive: true},
		} {
			if newDevelopmentRenames(project, options, "", "", io.Discard, reporter).input != nil {
				t.Errorf("%s with %s got a prompt", database, name)
			}
		}
	}
	previous, current := devRenameManifest(t, devRenameConfig(field.Text("title"))), devRenameManifest(t, devRenameConfig(field.Text("headline")))
	definition := devRenameProject(t, previous)
	definition.Migrations = ""
	renames, prompt := devRenamePrompt("y\n", "")
	var held developmentRenameHeldError
	if _, err := renames.resolve(t.Context(), definition, current, nil); !errors.As(err, &held) || strings.Contains(prompt.String(), "Preserve its existing data") {
		t.Fatalf("rename without a migrations directory = %v\n%s", err, prompt.String())
	}
}

// Declining every rename writes no migration and lets SQLite schema sync carry
// on: the old values stay in the documents under the old name. An empty line
// is not an answer, because Enter pressed before the question appeared is
// still queued in the terminal and must never move data.
func TestDevelopmentRenameDeclinedContinuesWithoutAMigration(t *testing.T) {
	ctx := context.Background()
	original := devRenameConfig(field.Text("title"))
	renamed := devRenameConfig(field.Text("headline"))
	previous, current := devRenameManifest(t, original), devRenameManifest(t, renamed)
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	databasePath := devRenameSQLite(t, definition, previous)
	backend := devRenameSQLiteStore(t, databasePath)
	before, err := core.New(original, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Input that ends without a y or an n is not consent.
	silent, _ := devRenameSQLitePrompt("\n\n", databasePath)
	if _, err := silent.resolve(ctx, definition, current, nil); err == nil || !strings.Contains(err.Error(), "read rename answer") {
		t.Fatalf("empty answers = %v", err)
	}
	if names := migrationNames(t, definition); len(names) != 0 {
		t.Fatalf("empty answers wrote migrations %v", names)
	}

	renames, prompt := devRenameSQLitePrompt("\nmaybe\nn\n", databasePath)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err != nil || !resolved {
		t.Fatalf("declined rename = %t, %v", resolved, err)
	}
	if !strings.Contains(prompt.String(), `field rename "posts".title -> "posts".headline`) || strings.Count(prompt.String(), "Answer y to move the existing data") != 2 {
		t.Fatalf("prompt = %s", prompt.String())
	}
	if !strings.Contains(prompt.String(), "Not a rename") || stops != 0 {
		t.Fatalf("declined rename output = %s, server stops = %d", prompt.String(), stops)
	}
	if names := migrationNames(t, definition); len(names) != 0 {
		t.Fatalf("a declined rename wrote migrations %v", names)
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatalf("schema sync after declining: %v", err)
	}
	after, err := core.New(renamed, backend)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !kept.Values["headline"].IsZero() && kept.Values["headline"].Kind() != store.ValueNull {
		t.Fatalf("a declined rename moved the value: %#v", kept.Values)
	}
	unmoved, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if title, _ := unmoved.Values["title"].StringValue(); err != nil || title != "Hello" {
		t.Fatalf("the old value after declining = %#v, %v", unmoved.Values, err)
	}

	// Nothing to ask when the config adds a field without removing one.
	unchanged, _ := devRenamePrompt("", "")
	if resolved, err := unchanged.resolve(ctx, definition, devRenameManifest(t, devRenameConfig(field.Text("title"), field.Text("summary"))), nil); err != nil || resolved {
		t.Fatalf("an additive change = %t, %v", resolved, err)
	}

	// The database has the new schema now, so the schema file, which still
	// holds the old one, no longer raises the question.
	again, asked := devRenameSQLitePrompt("y\n\n", databasePath)
	if resolved, err := again.resolve(ctx, definition, current, nil); err != nil || resolved || asked.Len() != 0 {
		t.Fatalf("a rename the database has moved past = %t, %v, asked %q", resolved, err, asked.String())
	}

	// Taking the answer back: once schema sync recorded the new schema,
	// restoring the old name is the reverse rename, and declining that one
	// brings the old values back into view.
	reverse, reversed := devRenameSQLitePrompt("n\n", databasePath)
	if resolved, err := reverse.resolve(ctx, definition, previous, nil); err != nil || !resolved || !strings.Contains(reversed.String(), `field rename "posts".headline -> "posts".title`) {
		t.Fatalf("restoring the old name = %t, %v, asked %q", resolved, err, reversed.String())
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: previous}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatal(err)
	}
	if restored, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{}); err != nil {
		t.Fatal(err)
	} else if title, _ := restored.Values["title"].StringValue(); title != "Hello" {
		t.Fatalf("the old value after restoring the name = %#v", restored.Values)
	}
}

// Accepting one rename and declining another would drop the declined field's
// data in the same migration, which needs a reviewed destructive change. The
// advice names what that is on the project's adapter.
func TestDevelopmentRenameMixedAnswersNeedAReviewedMigration(t *testing.T) {
	previous := devRenameManifest(t, devRenameConfig(field.Text("title"), field.Number("views")))
	current := devRenameManifest(t, devRenameConfig(field.Text("headline"), field.Number("reads")))
	for database, advice := range map[projectfile.DatabaseAdapter]string{
		projectfile.DatabasePostgres: "ridu migrate create --allow-destructive",
		projectfile.DatabaseMongoDB:  "ridu migrate create --allow-destructive",
		projectfile.DatabaseSQLite:   "registered data transform",
	} {
		definition := devRenameProjectOn(t, database, previous)
		renames, _ := devRenamePrompt("y\nn\n", "")
		if _, err := renames.resolve(context.Background(), definition, current, nil); err == nil || !strings.Contains(err.Error(), advice) {
			t.Errorf("%s mixed answers = %v", database, err)
		}
		if names := migrationNames(t, definition); len(names) != 0 {
			t.Errorf("%s mixed answers wrote migrations %v", database, names)
		}
	}
}

// A retry only applies a migration that records the renames the developer
// accepted. A migration that reaches the same config another way, such as a
// reviewed removal, would not move the data, so it is never applied under a
// message that says the data was preserved.
func TestDevelopmentRenameRetryRequiresTheRecordedRenames(t *testing.T) {
	ctx := context.Background()
	previous := devRenameManifest(t, devRenameConfig(field.Text("title"), field.Number("views")))
	current := devRenameManifest(t, devRenameConfig(field.Text("headline"), field.Number("views")))
	accepted := schemadiff.RenameCandidates(previous, current)
	if len(accepted) != 1 {
		t.Fatalf("rename candidates = %#v", accepted)
	}
	directory := t.TempDir()
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", previous, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.CreateArtifactWithRenames(ctx, directory, "rename", current, time.Unix(2, 0), contentRenames(accepted)); err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil || len(files) != 2 {
		t.Fatalf("history = %d files, %v", len(files), err)
	}
	if err := developmentRenameRecorded(files[1], accepted); err != nil {
		t.Fatalf("the rename migration = %v", err)
	}
	if err := developmentRenameRecorded(files[0], accepted); err == nil || !strings.Contains(err.Error(), "without recording these renames") {
		t.Fatalf("a migration without the rename = %v", err)
	}
	if err := developmentRenameRecorded(files[1], nil); err == nil {
		t.Fatal("a migration recording other renames was accepted")
	}
}

// A failed rename says what it left behind. Once the database's ledger refers
// to the new files, deleting them would make the history diverge, so that
// advice is only given while nothing was recorded.
func TestDevelopmentRenameFailureAdvice(t *testing.T) {
	cause := errors.New("lock timeout")
	created := []string{"migrations/a.ridu.json", "migrations/b.ridu.json"}
	untouched := developmentRenameFailure(projectfile.DatabasePostgres, "apply rename", cause, nil, "").Error()
	if untouched != "apply rename: lock timeout" {
		t.Fatalf("failure before anything was written = %q", untouched)
	}
	written := developmentRenameFailure(projectfile.DatabasePostgres, "apply rename", cause, created, "").Error()
	if !strings.Contains(written, "delete the new files") || !strings.Contains(written, "ridu migrate up --allow-maintenance") {
		t.Fatalf("failure after writing files = %q", written)
	}
	recorded := developmentRenameFailure(projectfile.DatabasePostgres, "apply rename", cause, created, "the database now records a").Error()
	if strings.Contains(recorded, "delete") || !strings.Contains(recorded, "keep the migration files") {
		t.Fatalf("failure after the ledger recorded a file = %q", recorded)
	}
	if managed := developmentRenameFailure(projectfile.DatabaseSQLite, "apply rename", cause, created, "").Error(); !strings.Contains(managed, "ridu migrate manages this database") || strings.Contains(managed, "--allow-maintenance") {
		t.Fatalf("SQLite failure advice = %q", managed)
	}
}

// recordDevRenameSchema replaces the schema record of a PostgreSQL development
// database and leaves its physical schema as it is.
func recordDevRenameSchema(t *testing.T, databaseURL string, manifest schema.Manifest) {
	t.Helper()
	encoded, err := manifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := migration.DigestManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(t.Context(), `UPDATE ridu_postgres_schema SET manifest_json = $1, manifest_digest = $2`, string(encoded), digest); err != nil {
		t.Fatal(err)
	}
}

func devRenameDatabase(t *testing.T) (string, *postgres.Store) {
	t.Helper()
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run the development rename integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("ridu_dev_rename_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`)
		admin.Close()
	})
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", schemaName)
	parsed.RawQuery = parameters.Encode()
	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: parsed.String(), AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	return parsed.String(), backend
}

// Answering yes writes the rename as a migration and applies it through the
// runner. ridu dev had already added a field no migration covers, so the
// flow first writes a migration for that, which baseline records.
func TestDevelopmentRenameAcceptedMigratesTheDevelopmentDatabase(t *testing.T) {
	ctx := context.Background()
	databaseURL, backend := devRenameDatabase(t)
	committed := devRenameManifest(t, devRenameConfig(field.Text("title")))
	synced := devRenameConfig(field.Text("title"), field.Text("summary"))
	renamed := devRenameConfig(field.Text("headline"), field.Text("summary"))
	previous, current := devRenameManifest(t, synced), devRenameManifest(t, renamed)
	definition := devRenameProject(t, previous)
	directory := definition.Absolute(definition.Migrations)

	initial, err := postgres.BuildArtifact(ctx, "initial", nil, committed, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, previous); err != nil {
		t.Fatal(err)
	}
	before, err := core.New(synced, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "summary": store.String("Kept")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	renames, prompt := devRenamePrompt("y\n\n", databaseURL)
	stops := 0
	renames.stopServer = func() bool {
		stops++
		// The server running the old config stops before the content moves.
		// It would otherwise keep writing group, array and block values under
		// the old names after the rename.
		running, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
		if title, _ := running.Values["title"].StringValue(); err != nil || title != "Hello" {
			t.Errorf("content moved before the server stopped: %#v, %v", running.Values, err)
		}
		return true
	}
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err != nil || !resolved {
		t.Fatalf("accepted rename = %t, %v\n%s", resolved, err, prompt.String())
	}
	if stops != 1 {
		t.Fatalf("the running server was stopped %d times", stops)
	}
	names := migrationNames(t, definition)
	if strings.Join(names, ",") != "initial,changes-before-rename-posts-title-to-headline,rename-posts-title-to-headline" {
		t.Fatalf("migrations = %v", names)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Fatalf("%s is pending after the rename flow", status.Name)
		}
	}
	after, err := core.New(renamed, backend)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	headline, _ := kept.Values["headline"].StringValue()
	summary, _ := kept.Values["summary"].StringValue()
	if headline != "Hello" || summary != "Kept" {
		t.Fatalf("renamed document = %#v", kept.Values)
	}
	// Ordinary schema sync has nothing left to do.
	if err := backend.VerifySchema(ctx, current); err != nil {
		t.Fatalf("development schema after the rename = %v", err)
	}
}

// A database that drifted from the schema ridu dev last knew cannot be
// vouched for, so the prompt does not offer to move its data. The change is
// left to ordinary schema sync, which refuses to drop anything, and nothing
// is written or changed.
func TestDevelopmentRenameLeavesADriftedDatabaseToSchemaSync(t *testing.T) {
	ctx := context.Background()
	databaseURL, backend := devRenameDatabase(t)
	committed := devRenameConfig(field.Text("title"))
	previous := devRenameManifest(t, committed)
	current := devRenameManifest(t, devRenameConfig(field.Text("headline")))
	definition := devRenameProject(t, previous)
	directory := definition.Absolute(definition.Migrations)
	initial, err := postgres.BuildArtifact(ctx, "initial", nil, previous, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Now()); err != nil {
		t.Fatal(err)
	}
	// The database has a column neither its schema record nor any migration
	// describes.
	driftedConfig := devRenameConfig(field.Text("title"), field.Text("stray"))
	drifted := devRenameManifest(t, driftedConfig)
	if err := backend.SyncDevelopmentSchema(ctx, drifted); err != nil {
		t.Fatal(err)
	}
	recordDevRenameSchema(t, databaseURL, previous)
	application, err := core.New(driftedConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "stray": store.String("Kept")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	renames, prompt := devRenamePrompt("y\n\n", databaseURL)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	if resolved, err := renames.resolve(ctx, definition, current, nil); err != nil || resolved || prompt.Len() != 0 {
		t.Fatalf("rename over a drifted database = %t, %v, asked %q", resolved, err, prompt.String())
	}
	if stops != 0 {
		t.Fatalf("the server was stopped %d times", stops)
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial" {
		t.Fatalf("migrations = %v", names)
	}
	_, err = synchronizeDevelopmentSchema(ctx, projectfile.DatabasePostgres, databaseURL, "", true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}))
	if err == nil || !strings.Contains(err.Error(), "explicit safety resolution") {
		t.Fatalf("schema sync over a drifted database = %v", err)
	}
	if err := backend.VerifySchema(ctx, drifted); err != nil {
		t.Fatalf("the rejected reload changed the database: %v", err)
	}
	unchanged, err := application.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	title, _ := unchanged.Values["title"].StringValue()
	stray, _ := unchanged.Values["stray"].StringValue()
	if title != "Hello" || stray != "Kept" {
		t.Fatalf("the rejected reload changed a document: %#v", unchanged.Values)
	}
}

// When the newest migration already reaches the new config as a reviewed
// removal that this database has not run, answering yes must not apply it:
// it would drop the values and report them preserved.
func TestDevelopmentRenameRefusesAPendingRemovalThatReachesTheSameConfig(t *testing.T) {
	ctx := context.Background()
	databaseURL, backend := devRenameDatabase(t)
	original := devRenameConfig(field.Text("title"), field.Number("views"))
	previous := devRenameManifest(t, original)
	current := devRenameManifest(t, devRenameConfig(field.Text("headline"), field.Number("views")))
	definition := devRenameProject(t, previous)
	directory := definition.Absolute(definition.Migrations)
	initial, err := postgres.BuildArtifact(ctx, "initial", nil, previous, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "initial", initial, time.Now()); err != nil {
		t.Fatal(err)
	}
	removal, err := postgres.BuildArtifact(ctx, "replace-title", &previous, current, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationartifact.Create(directory, "replace-title", removal, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := backend.SyncDevelopmentSchema(ctx, previous); err != nil {
		t.Fatal(err)
	}
	application, err := core.New(original, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "views": store.Number(1)}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	renames, prompt := devRenamePrompt("y\n\n", databaseURL)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err == nil || resolved || !strings.Contains(err.Error(), "without recording these renames") {
		t.Fatalf("rename over a pending removal = %t, %v\n%s", resolved, err, prompt.String())
	}
	if stops != 0 || strings.Contains(prompt.String(), "preserved existing data") {
		t.Fatalf("the refused rename stopped the server %d times and printed %s", stops, prompt.String())
	}
	kept, err := application.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if title, _ := kept.Values["title"].StringValue(); err != nil || title != "Hello" {
		t.Fatalf("the refused rename changed the document: %#v, %v", kept.Values, err)
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,replace-title" {
		t.Fatalf("the refused rename wrote migrations %v", names)
	}
}

// Declining on PostgreSQL leaves a column its schema sync will not drop. The
// prompt says so rather than promising the reload continues.
func TestDevelopmentRenameDeclinedOnPostgresSaysSyncStaysPaused(t *testing.T) {
	ctx := context.Background()
	databaseURL, backend := devRenameDatabase(t)
	previous := devRenameManifest(t, devRenameConfig(field.Text("title")))
	current := devRenameManifest(t, devRenameConfig(field.Text("headline")))
	definition := devRenameProject(t, previous)
	if err := backend.SyncDevelopmentSchema(ctx, previous); err != nil {
		t.Fatal(err)
	}
	renames, prompt := devRenamePrompt("n\n", databaseURL)
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err != nil || !resolved {
		t.Fatalf("declined rename = %t, %v", resolved, err)
	}
	if !strings.Contains(prompt.String(), "PostgreSQL schema sync never drops a column") {
		t.Fatalf("declined rename output = %s", prompt.String())
	}
	_, err = synchronizeDevelopmentSchema(ctx, projectfile.DatabasePostgres, databaseURL, "", true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}))
	if err == nil || !strings.Contains(err.Error(), "explicit safety resolution") {
		t.Fatalf("schema sync after declining on PostgreSQL = %v", err)
	}
}

// devRenameSQLite synchronizes a development SQLite file to a manifest, as
// ridu dev does, and returns its path.
func devRenameSQLite(t *testing.T, definition projectfile.File, manifest schema.Manifest) string {
	t.Helper()
	databasePath := filepath.Join(definition.Root, "development.sqlite")
	reporter := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	if _, err := synchronizeDevelopmentSchema(context.Background(), projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: manifest}, reporter); err != nil {
		t.Fatal(err)
	}
	return databasePath
}

func devRenameSQLiteStore(t *testing.T, databasePath string) *sqlite.Store {
	t.Helper()
	backend, err := sqlite.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend
}

// On a SQLite database ridu dev synchronizes, answering yes writes the rename
// as migrations for production and moves the development content directly, so
// the database stays under ridu dev. The running server stops first: it would
// otherwise keep writing documents under the old name.
func TestDevelopmentRenameAcceptedMovesSQLiteDevelopmentContent(t *testing.T) {
	ctx := context.Background()
	// The rich text field carries raw plugin configuration, which the indented
	// schema file must not make look like a changed field.
	committed := devRenameManifest(t, devRenameConfig(field.Text("title"), richtext.Field("content")))
	synced := devRenameConfig(field.Text("title"), richtext.Field("content"), field.Text("summary"))
	renamed := devRenameConfig(field.Text("headline"), richtext.Field("content"), field.Text("summary"))
	previous, current := devRenameManifest(t, synced), devRenameManifest(t, renamed)
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	directory := definition.Absolute(definition.Migrations)
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", committed, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	databasePath := devRenameSQLite(t, definition, previous)
	backend := devRenameSQLiteStore(t, databasePath)
	before, err := core.New(synced, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "summary": store.String("Kept")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	renames, prompt := devRenameSQLitePrompt("y\n\n", databasePath)
	stops := 0
	renames.stopServer = func() bool {
		stops++
		// The content has not moved while the old server could still write.
		running, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
		if title, _ := running.Values["title"].StringValue(); err != nil || title != "Hello" {
			t.Errorf("content moved before the server stopped: %#v, %v", running.Values, err)
		}
		return true
	}
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err != nil || !resolved {
		t.Fatalf("accepted rename = %t, %v\n%s", resolved, err, prompt.String())
	}
	if stops != 1 {
		t.Fatalf("the running server was stopped %d times", stops)
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,changes-before-rename-posts-title-to-headline,rename-posts-title-to-headline" {
		t.Fatalf("migrations = %v", names)
	}
	if managed, err := backend.HasMigrationHistory(ctx); err != nil || managed {
		t.Fatalf("the rename handed the development database to ridu migrate: %t, %v", managed, err)
	}
	// Ordinary schema sync carries on from the renamed content.
	var logs bytes.Buffer
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: current}, newCLIOutput(&logs, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "Synchronized SQLite development schema") {
		t.Fatalf("schema sync after the rename: %s", logs.String())
	}
	after, err := core.New(renamed, backend)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	headline, _ := kept.Values["headline"].StringValue()
	summary, _ := kept.Values["summary"].StringValue()
	if headline != "Hello" || summary != "Kept" {
		t.Fatalf("renamed document = %#v", kept.Values)
	}

	// The migrations it wrote carry the same rename to a deployed database.
	deployed := devRenameSQLiteStore(t, filepath.Join(t.TempDir(), "deployed.sqlite"))
	if err := deployed.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := deployed.Ready(ctx, current); err != nil {
		t.Fatalf("the written migrations do not reach the new config: %v", err)
	}
}

// A SQLite database ridu migrate already manages takes the rename through
// the migration runner, like PostgreSQL.
func TestDevelopmentRenameAcceptedMigratesAManagedSQLiteDatabase(t *testing.T) {
	ctx := context.Background()
	original := devRenameConfig(field.Text("title"))
	renamed := devRenameConfig(field.Text("headline"))
	previous, current := devRenameManifest(t, original), devRenameManifest(t, renamed)
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	directory := definition.Absolute(definition.Migrations)
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", previous, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(definition.Root, "development.sqlite")
	backend := devRenameSQLiteStore(t, databasePath)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	before, err := core.New(original, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	renames, prompt := devRenameSQLitePrompt("y\n\n", databasePath)
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err != nil || !resolved {
		t.Fatalf("accepted rename = %t, %v\n%s", resolved, err, prompt.String())
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,rename-posts-title-to-headline" {
		t.Fatalf("migrations = %v", names)
	}
	statuses, err := backend.ArtifactStatus(ctx, directory, current)
	if err != nil || len(statuses) != 2 || !statuses[1].Applied {
		t.Fatalf("status after the rename flow = %#v, %v", statuses, err)
	}
	after, err := core.New(renamed, backend)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if headline, _ := kept.Values["headline"].StringValue(); headline != "Hello" {
		t.Fatalf("renamed document = %#v", kept.Values)
	}
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseSQLite, "", databasePath, true, true, developmentPreparation{manifest: current}, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})); err != nil {
		t.Fatalf("ridu dev after a managed rename: %v", err)
	}
}

// SQLite has no way to rename a collection in place, so ridu dev says so and
// pauses without asking a question it could not act on.
func TestDevelopmentRenameLeavesSQLiteCollectionRenamesToATransform(t *testing.T) {
	collection := func(slug schema.CollectionSlug) core.Config {
		return core.Config{Name: "Rename", Collections: []core.Collection{{Slug: slug, Fields: field.Fields{field.Text("title")}}}}
	}
	previous, current := devRenameManifest(t, collection("posts")), devRenameManifest(t, collection("articles"))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	renames, prompt := devRenamePrompt("y\n\n", "")
	if _, err := renames.resolve(context.Background(), definition, current, nil); err == nil || !strings.Contains(err.Error(), "cannot rename collection") {
		t.Fatalf("SQLite collection rename = %v", err)
	}
	if prompt.Len() != 0 {
		t.Fatalf("ridu dev asked about a rename it cannot apply: %s", prompt.String())
	}
	if names := migrationNames(t, definition); len(names) != 0 {
		t.Fatalf("a refused rename wrote migrations %v", names)
	}
}

func devRenameMongoDB(t *testing.T) string {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	if baseURL == "" {
		t.Skip("set RIDU_MONGODB_URL to run the MongoDB development rename integration")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	databaseName := fmt.Sprintf("ridu_cli_test_rename_%d", time.Now().UnixNano())
	parsed.Path, parsed.RawPath = "/"+databaseName, ""
	client, err := mongo.Connect(options.Client().ApplyURI(parsed.String()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := client.Database(databaseName).Drop(cleanupContext); err != nil {
			t.Errorf("drop isolated MongoDB development database: %v", err)
		}
		_ = client.Disconnect(cleanupContext)
	})
	return parsed.String()
}

func devRenameMongoDBStore(t *testing.T, databaseURL string) *mongodb.Store {
	t.Helper()
	backend, err := mongodb.OpenWithConfig(context.Background(), mongodb.Config{DatabaseURL: databaseURL, AllowInsecureTransport: true, ApplicationName: "ridu-development-rename-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend
}

// On MongoDB, answering yes stops the running server, writes the rename as a
// migration and applies it through the runner, as ridu migrate up
// --allow-maintenance would after the application was drained.
func TestDevelopmentRenameAcceptedMigratesTheMongoDBDevelopmentDatabase(t *testing.T) {
	ctx := context.Background()
	databaseURL := devRenameMongoDB(t)
	committed := devRenameManifest(t, devRenameConfig(field.Text("title").Index()))
	synced := devRenameConfig(field.Text("title").Index(), field.Text("summary"))
	renamed := devRenameConfig(field.Text("headline").Index(), field.Text("summary"))
	previous, current := devRenameManifest(t, synced), devRenameManifest(t, renamed)
	definition := devRenameProjectOn(t, projectfile.DatabaseMongoDB, previous)
	directory := definition.Absolute(definition.Migrations)
	if _, err := mongodb.CreateArtifact(ctx, directory, "initial", committed, time.Now(), mongodb.ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	reporter := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseMongoDB, databaseURL, "", true, true, developmentPreparation{manifest: previous}, reporter); err != nil {
		t.Fatal(err)
	}
	serving := devRenameMongoDBStore(t, databaseURL)
	if err := serving.VerifyIndexes(ctx, previous); err != nil {
		t.Fatal(err)
	}
	before, err := core.New(synced, serving)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello"), "summary": store.String("Kept")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	renames, prompt := devRenamePrompt("y\n\n", databaseURL)
	stops := 0
	renames.stopServer = func() bool {
		stops++
		running, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
		if title, _ := running.Values["title"].StringValue(); err != nil || title != "Hello" {
			t.Errorf("content moved before the server stopped: %#v, %v", running.Values, err)
		}
		return true
	}
	resolved, err := renames.resolve(ctx, definition, current, nil)
	if err != nil || !resolved {
		t.Fatalf("accepted rename = %t, %v\n%s", resolved, err, prompt.String())
	}
	if stops != 1 {
		t.Fatalf("the running server was stopped %d times", stops)
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,changes-before-rename-posts-title-to-headline,rename-posts-title-to-headline" {
		t.Fatalf("migrations = %v", names)
	}
	migrated := devRenameMongoDBStore(t, databaseURL)
	statuses, err := migrated.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) != 3 {
		t.Fatalf("status after the rename flow = %#v, %v", statuses, err)
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Fatalf("%s is pending after the rename flow", status.Name)
		}
	}
	// ridu dev keeps synchronizing indexes over the migrated database.
	if _, err := synchronizeDevelopmentSchema(ctx, projectfile.DatabaseMongoDB, databaseURL, "", true, true, developmentPreparation{manifest: current}, reporter); err != nil {
		t.Fatal(err)
	}
	if err := migrated.VerifyIndexes(ctx, current); err != nil {
		t.Fatal(err)
	}
	after, err := core.New(renamed, migrated)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	headline, _ := kept.Values["headline"].StringValue()
	summary, _ := kept.Values["summary"].StringValue()
	if headline != "Hello" || summary != "Kept" {
		t.Fatalf("renamed document = %#v", kept.Values)
	}
}

// A document that already stores a value under the new name stops the SQLite
// development rename. Nothing moves, and saving again retries with the
// migration that was already written.
func TestDevelopmentRenameSQLiteConflictChangesNothingAndRetries(t *testing.T) {
	ctx := context.Background()
	original := devRenameConfig(field.Text("title"))
	renamed := devRenameConfig(field.Text("headline"))
	previous, current := devRenameManifest(t, original), devRenameManifest(t, renamed)
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	directory := definition.Absolute(definition.Migrations)
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", previous, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	databasePath := devRenameSQLite(t, definition, previous)
	backend := devRenameSQLiteStore(t, databasePath)
	before, err := core.New(original, backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, core.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// A value left under the new name by an earlier config.
	raw, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `UPDATE ridu_documents SET values_json = json_set(values_json, '$.headline', 'Already here')`); err != nil {
		t.Fatal(err)
	}

	renames, _ := devRenameSQLitePrompt("y\n\n", databasePath)
	if _, err := renames.resolve(ctx, definition, current, nil); err == nil || !strings.Contains(err.Error(), "already has a value") || !strings.Contains(err.Error(), "save again to retry") {
		t.Fatalf("rename onto a stored value = %v", err)
	}
	unchanged, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if title, _ := unchanged.Values["title"].StringValue(); err != nil || title != "Hello" {
		t.Fatalf("a failed rename changed the document: %#v, %v", unchanged.Values, err)
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,rename-posts-title-to-headline" {
		t.Fatalf("migrations after the failed rename = %v", names)
	}

	// With the conflict cleared, the same answer finishes the rename and
	// writes nothing new.
	if _, err := raw.ExecContext(ctx, `UPDATE ridu_documents SET values_json = json_remove(values_json, '$.headline')`); err != nil {
		t.Fatal(err)
	}
	retry, prompt := devRenameSQLitePrompt("y\n\n", databasePath)
	if resolved, err := retry.resolve(ctx, definition, current, nil); err != nil || !resolved {
		t.Fatalf("retried rename = %t, %v\n%s", resolved, err, prompt.String())
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,rename-posts-title-to-headline" {
		t.Fatalf("migrations after the retry = %v", names)
	}
	after, err := core.New(renamed, backend)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
	if headline, _ := moved.Values["headline"].StringValue(); err != nil || headline != "Hello" {
		t.Fatalf("retried rename document = %#v, %v", moved.Values, err)
	}
}

// A managed SQLite database whose history runs compiled data transforms can
// only be migrated by the project binary. ridu dev says so before it writes a
// migration or stops the server.
func TestDevelopmentRenameLeavesTransformHistoriesToTheProjectBinary(t *testing.T) {
	ctx := context.Background()
	original := devRenameConfig(field.Text("title"))
	previous := devRenameManifest(t, original)
	current := devRenameManifest(t, devRenameConfig(field.Text("headline")))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	directory := definition.Absolute(definition.Migrations)
	if _, err := sqlite.CreateArtifact(ctx, directory, "initial", previous, time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	descriptor := migration.DataTransformDescriptor{Name: "backfill", Checksum: migration.DataTransformChecksum([]byte("backfill-v1"))}
	if _, err := sqlite.CreateArtifact(ctx, directory, "backfill", previous, time.Unix(2, 0), false, descriptor); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(definition.Root, "development.sqlite")
	backend := devRenameSQLiteStore(t, databasePath)
	transform := migration.DataTransform{
		DataTransformDescriptor: descriptor,
		Up:                      func(context.Context, migration.DataTransaction) error { return nil },
		Down:                    func(context.Context, migration.DataTransaction) error { return nil },
	}
	if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
		t.Fatal(err)
	}

	renames, _ := devRenameSQLitePrompt("y\n\n", databasePath)
	stops := 0
	renames.stopServer = func() bool { stops++; return true }
	if _, err := renames.resolve(ctx, definition, current, nil); err == nil || !strings.Contains(err.Error(), "compiled data transforms") || !strings.Contains(err.Error(), "ridu migrate up") {
		t.Fatalf("rename over a transform history = %v", err)
	}
	if stops != 0 {
		t.Fatalf("the server was stopped %d times for a rename that could not start", stops)
	}
	if names := migrationNames(t, definition); strings.Join(names, ",") != "initial,backfill" {
		t.Fatalf("the refused rename wrote migrations %v", names)
	}
}

// devRenameHeldDatabase is one adapter's development database for the
// held-rename test, with the three manual steps a developer would run:
// ridu migrate create, baseline and up.
type devRenameHeldDatabase struct {
	adapter      projectfile.DatabaseAdapter
	databaseURL  string
	databasePath string
	// migrate applies the rename by hand, as the pause message instructs.
	migrate func(t *testing.T, directory string, previous, current schema.Manifest)
}

// Without a terminal ridu dev cannot ask about a rename, so it rejects the
// reload, and keeps rejecting it on every later save: nothing is generated or
// synchronized while the database still has the old schema. Rewriting the
// schema file with ridu generate does not release it. It is released by
// restoring the old name, or by migrating the rename by hand, after which the
// next save goes through.
func TestDevelopmentRenameWithoutATerminalHoldsUntilResolved(t *testing.T) {
	ctx := context.Background()
	original := devRenameConfig(field.Text("title"), field.Number("views"))
	renamed := devRenameConfig(field.Text("headline"), field.Number("views"))
	previous, current := devRenameManifest(t, original), devRenameManifest(t, renamed)
	accepted := schemadiff.RenameCandidates(previous, current)
	if len(accepted) != 1 {
		t.Fatalf("rename candidates = %#v", accepted)
	}

	databases := map[string]func(t *testing.T, root string) devRenameHeldDatabase{
		"sqlite": func(t *testing.T, root string) devRenameHeldDatabase {
			databasePath := filepath.Join(root, "development.sqlite")
			return devRenameHeldDatabase{adapter: projectfile.DatabaseSQLite, databasePath: databasePath, migrate: func(t *testing.T, directory string, previous, current schema.Manifest) {
				if _, err := sqlite.CreateArtifactWithRenames(ctx, directory, "rename-title", current, time.Now(), contentRenames(accepted)); err != nil {
					t.Fatal(err)
				}
				backend := devRenameSQLiteStore(t, databasePath)
				if _, err := backend.AdoptArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
			}}
		},
		"postgres": func(t *testing.T, root string) devRenameHeldDatabase {
			databaseURL, backend := devRenameDatabase(t)
			return devRenameHeldDatabase{adapter: projectfile.DatabasePostgres, databaseURL: databaseURL, migrate: func(t *testing.T, directory string, previous, current schema.Manifest) {
				artifact, err := postgres.BuildArtifact(ctx, "rename-title", &previous, current, postgresRenames(accepted), false)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := migrationartifact.Create(directory, "rename-title", artifact, time.Now()); err != nil {
					t.Fatal(err)
				}
				if _, err := backend.AdoptArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifactsWithOptions(ctx, directory, postgres.RunnerOptions{AllowMaintenance: true, AllowInsecureDatabase: true}); err != nil {
					t.Fatal(err)
				}
			}}
		},
		"mongodb": func(t *testing.T, root string) devRenameHeldDatabase {
			databaseURL := devRenameMongoDB(t)
			return devRenameHeldDatabase{adapter: projectfile.DatabaseMongoDB, databaseURL: databaseURL, migrate: func(t *testing.T, directory string, previous, current schema.Manifest) {
				if _, err := mongodb.CreateArtifact(ctx, directory, "rename-title", current, time.Now(), mongodb.ArtifactOptions{Renames: contentRenames(accepted)}); err != nil {
					t.Fatal(err)
				}
				backend := devRenameMongoDBStore(t, databaseURL)
				if _, err := backend.AdoptArtifacts(ctx, directory); err != nil {
					t.Fatal(err)
				}
				if err := backend.ApplyArtifactsWithOptions(ctx, directory, mongodb.RunnerOptions{AllowMaintenance: true}); err != nil {
					t.Fatal(err)
				}
			}}
		},
	}
	for name, open := range databases {
		t.Run(name, func(t *testing.T) {
			definition := devRenameProjectOn(t, projectfile.DatabasePostgres, previous)
			database := open(t, definition.Root)
			definition.Database = database.adapter
			directory := definition.Absolute(definition.Migrations)
			quiet := newCLIOutput(io.Discard, io.Discard, cliOutputOptions{})
			synchronize := func(manifest schema.Manifest) error {
				_, err := synchronizeDevelopmentSchema(ctx, database.adapter, database.databaseURL, database.databasePath, true, true, developmentPreparation{manifest: manifest}, quiet)
				return err
			}
			// The committed history and the development database both have the
			// old schema, and ridu dev recorded that after its last sync.
			initial, _ := devRenameWithoutTerminal(database.databaseURL, database.databasePath)
			if _, err := initial.createMigration(ctx, database.adapter, directory, "initial", nil, previous, nil); err != nil {
				t.Fatal(err)
			}
			if err := synchronize(previous); err != nil {
				t.Fatal(err)
			}
			before, err := core.New(original, devRenameStore(t, database, previous))
			if err != nil {
				t.Fatal(err)
			}
			post, err := before.Local().Create(ctx, "posts", coreValues("title", "Hello"), core.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			readTitle := func() string {
				t.Helper()
				found, err := before.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
				if err != nil {
					t.Fatal(err)
				}
				title, _ := found.Values["title"].StringValue()
				return title
			}

			save := func() (*bytes.Buffer, error) {
				renames, printed := devRenameWithoutTerminal(database.databaseURL, database.databasePath)
				_, err := renames.resolve(ctx, definition, current, nil)
				return printed, err
			}
			requireHeld := func(step string) {
				t.Helper()
				printed, err := save()
				var held developmentRenameHeldError
				if !errors.As(err, &held) || !strings.Contains(err.Error(), "paused for a possible rename") {
					t.Fatalf("%s = %v", step, err)
				}
				if !strings.Contains(printed.String(), `possible field rename "posts".title -> "posts".headline`) {
					t.Fatalf("%s printed %q", step, printed.String())
				}
				if title := readTitle(); title != "Hello" {
					t.Fatalf("%s left the title as %q", step, title)
				}
				if names := migrationNames(t, definition); len(names) != 1 {
					t.Fatalf("%s wrote migrations %v", step, names)
				}
			}
			requireHeld("the first save")
			requireHeld("the second save")

			// ridu generate rewrites the schema file; the rename still waits.
			encoded, err := current.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(definition.Absolute(definition.Schema), encoded, 0o644); err != nil {
				t.Fatal(err)
			}
			requireHeld("a save after ridu generate")

			// Restoring the old name needs no decision.
			restored, _ := devRenameWithoutTerminal(database.databaseURL, database.databasePath)
			if resolved, err := restored.resolve(ctx, definition, previous, nil); err != nil || resolved {
				t.Fatalf("the restored name = %t, %v", resolved, err)
			}

			// Migrating the rename by hand releases the next save.
			database.migrate(t, directory, previous, current)
			printed, err := save()
			if err != nil || printed.Len() != 0 {
				t.Fatalf("a save after the manual migration = %v, printed %q", err, printed.String())
			}
			if err := synchronize(current); err != nil {
				t.Fatalf("schema sync after the manual migration: %v", err)
			}
			after, err := core.New(renamed, devRenameStore(t, database, current))
			if err != nil {
				t.Fatal(err)
			}
			moved, err := after.Local().Find(ctx, "posts", post.ID, core.FindOptions{})
			if headline, _ := moved.Values["headline"].StringValue(); err != nil || headline != "Hello" {
				t.Fatalf("the migrated document = %#v, %v", moved.Values, err)
			}
		})
	}
}

// A baseline recorded for a database that has since been replaced, such as a
// deleted SQLite file, holds nothing: the new database has no content under
// the old name.
func TestDevelopmentRenameDoesNotHoldADatabaseThatNeverHadTheOldSchema(t *testing.T) {
	ctx := context.Background()
	previous := devRenameManifest(t, devRenameConfig(field.Text("title")))
	current := devRenameManifest(t, devRenameConfig(field.Text("headline")))
	definition := devRenameProjectOn(t, projectfile.DatabaseSQLite, previous)
	databasePath := filepath.Join(definition.Root, "development.sqlite")
	renames, printed := devRenameWithoutTerminal("", databasePath)
	if resolved, err := renames.resolve(ctx, definition, current, nil); err != nil || resolved || printed.Len() != 0 {
		t.Fatalf("a rename against an empty database = %t, %v, printed %q", resolved, err, printed.String())
	}
}

func coreValues(name, value string) store.Values {
	return store.Values{name: store.String(value)}
}

// devRenameStore opens the held-rename test's database for document access
// under manifest.
func devRenameStore(t *testing.T, database devRenameHeldDatabase, manifest schema.Manifest) store.Store {
	t.Helper()
	switch database.adapter {
	case projectfile.DatabaseSQLite:
		return devRenameSQLiteStore(t, database.databasePath)
	case projectfile.DatabaseMongoDB:
		backend := devRenameMongoDBStore(t, database.databaseURL)
		if err := backend.VerifyIndexes(context.Background(), manifest); err != nil {
			t.Fatal(err)
		}
		return backend
	}
	backend, err := postgres.OpenWithConfig(context.Background(), postgres.PoolConfig{DatabaseURL: database.databaseURL, AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	return backend
}
