package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/cli"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// developmentSession is one ridu dev process running over a scaffolded
// project, with a terminal the test reads and answers.
type developmentSession struct {
	target  string
	address string
	answer  io.Writer
	exited  <-chan struct{}

	mutex  sync.Mutex
	output bytes.Buffer
}

func (session *developmentSession) Write(written []byte) (int, error) {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.output.Write(written)
}

func (session *developmentSession) printed() string {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	return session.output.String()
}

func (session *developmentSession) waitFor(t *testing.T, text string) {
	t.Helper()
	session.waitForCount(t, text, 1)
}

// waitForCount waits until ridu dev has printed text at least count times.
func (session *developmentSession) waitForCount(t *testing.T, text string, count int) {
	t.Helper()
	deadline := time.After(4 * time.Minute)
	for strings.Count(session.printed(), text) < count {
		select {
		case <-session.exited:
			t.Fatalf("ridu dev exited before printing %q:\n%s", text, session.printed())
		case <-deadline:
			t.Fatalf("ridu dev never printed %q:\n%s", text, session.printed())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// startDevelopmentSession scaffolds a project without an admin and starts
// ridu dev over it with a terminal attached.
func startDevelopmentSession(t *testing.T, database string, databaseArguments ...string) *developmentSession {
	t.Helper()
	return startDevelopmentSessionWith(t, database, nil, databaseArguments...)
}

// startDevelopmentSessionWith also lets a test add application code to the
// scaffolded project before ridu dev starts. prepare receives the project root.
func startDevelopmentSessionWith(t *testing.T, database string, prepare func(target string), databaseArguments ...string) *developmentSession {
	t.Helper()
	return startDevelopmentSessionIn(t, database, true, prepare, databaseArguments...)
}

// startDevelopmentSessionIn chooses whether ridu dev has a terminal. Without
// one it runs as it does under a task runner or a coding agent.
func startDevelopmentSessionIn(t *testing.T, database string, terminal bool, prepare func(target string), databaseArguments ...string) *developmentSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the development process shutdown proof currently requires POSIX signals")
	}
	frameworkRoot := moduleRoot(t)
	target := newProjectTarget(t, database+"-dev-rename")
	setFrameworkProxy(t, frameworkRoot)
	var stdout, stderr bytes.Buffer
	options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	if exitCode := cli.Run(context.Background(), []string{"new", "--database", database, "--module", "example.com/" + database + "/devrename", target}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu new: %s", stderr.String())
	}
	// The API alone is enough here; without an admin there is nothing to install.
	projectPath := filepath.Join(target, "ridu.toml")
	project, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	withoutAdmin := strings.Replace(string(project), "admin = \"./admin\"\n", "", 1)
	if withoutAdmin == string(project) {
		t.Fatal("generated project admin setting was not found")
	}
	if err := os.WriteFile(projectPath, []byte(withoutAdmin), 0o644); err != nil {
		t.Fatal(err)
	}
	if exitCode := cli.Run(context.Background(), []string{"generate"}, &stdout, &stderr, options); exitCode != 0 {
		t.Fatalf("ridu generate: %s", stderr.String())
	}
	if prepare != nil {
		prepare(target)
	}

	ctx, stop := context.WithCancel(context.Background())
	answers, answer := io.Pipe()
	exited := make(chan struct{})
	session := &developmentSession{target: target, address: fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t)), answer: answer, exited: exited}
	interactive := options
	interactive.Interactive, interactive.Stdin = terminal, answers
	go func() {
		defer close(exited)
		arguments := append([]string{"dev", "--no-install", "--no-docker", "--address", session.address}, databaseArguments...)
		cli.Run(ctx, arguments, session, session, interactive)
	}()
	t.Cleanup(func() {
		stop()
		_ = answer.Close()
		select {
		case <-exited:
		case <-time.After(time.Minute):
			t.Errorf("ridu dev did not stop:\n%s", session.printed())
		}
	})
	session.waitFor(t, "Watching Go configuration")
	return session
}

// acceptTitleRename renames posts.title in the running project's config and
// accepts the rename ridu dev asks about. It returns once the content moved.
func (session *developmentSession) acceptTitleRename(t *testing.T) {
	t.Helper()
	session.replace(t, "content/posts.go", `field.Text("title")`, `field.Text("headline")`)
	session.waitFor(t, `Detected field rename "posts".title -> "posts".headline. Preserve its existing data as a rename? [y/n] `)
	if _, err := io.WriteString(session.answer, "y\n"); err != nil {
		t.Fatal(err)
	}
	session.waitFor(t, "Migration name [rename-posts-title-to-headline]: ")
	if _, err := io.WriteString(session.answer, "\n"); err != nil {
		t.Fatal(err)
	}
	session.waitFor(t, "Applied rename and preserved existing data")
	printed := session.printed()
	stopped := strings.Index(printed, "Stopping the development server before changing its stored schema")
	if stopped < 0 || strings.Index(printed, "Applied rename and preserved existing data") < stopped {
		t.Fatalf("the server was not stopped before the content moved:\n%s", printed)
	}
}

func (session *developmentSession) replace(t *testing.T, relative, old, replacement string) {
	t.Helper()
	path := filepath.Join(session.target, filepath.FromSlash(relative))
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(source), old, replacement, 1)
	if changed == string(source) {
		t.Fatalf("%s does not contain %s", relative, old)
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
}

// renameTitle accepts the posts.title rename and waits for the replacement
// server to take over.
func (session *developmentSession) renameTitle(t *testing.T) {
	t.Helper()
	session.acceptTitleRename(t)
	session.waitFor(t, "Server reloaded")
	session.requireServing(t)
	files, err := migrationartifact.ReadAll(filepath.Join(session.target, "migrations"))
	if err != nil || len(files) != 2 || files[1].Artifact.Name != "rename-posts-title-to-headline" {
		t.Fatalf("migrations after the rename = %d files, %v", len(files), err)
	}
}

func (session *developmentSession) requireServing(t *testing.T) {
	t.Helper()
	response, err := http.Get("http://" + session.address + "/readyz")
	if err != nil {
		t.Fatalf("the replacement server is not serving: %v\n%s", err, session.printed())
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("replacement readiness = %d\n%s", response.StatusCode, session.printed())
	}
}

// Renaming a field while ridu dev runs over SQLite asks in the terminal, stops
// the server that still writes the old name, moves the stored content, and
// brings up a server on the new config without handing the database to ridu
// migrate.
func TestDevelopmentRenamePromptReloadsARunningSQLiteProject(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(canonicalPath(t, t.TempDir()), "development.sqlite")
	session := startDevelopmentSession(t, "sqlite", "--database-path", databasePath)

	posts := func(name string) ridu.Config {
		return ridu.Config{Name: "Development rename", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text(name)}}}}
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	before, err := ridu.New(posts("title"), backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	session.renameTitle(t)

	after, err := ridu.New(posts("headline"), backend)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if headline, _ := kept.Values["headline"].StringValue(); headline != "Hello" {
		t.Fatalf("renamed document = %#v", kept.Values)
	}
	if managed, err := backend.HasMigrationHistory(ctx); err != nil || managed {
		t.Fatalf("the rename handed the development database to ridu migrate: %t, %v", managed, err)
	}
}

// Over MongoDB the same prompt applies the rename through the migration
// runner, and the replacement server starts against the migrated database.
func TestDevelopmentRenamePromptReloadsARunningMongoDBProject(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("RIDU_MONGODB_URL"))
	if baseURL == "" {
		t.Skip("set RIDU_MONGODB_URL to run the MongoDB development rename qualification")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal("RIDU_MONGODB_URL is not a valid MongoDB URL")
	}
	databaseName := fmt.Sprintf("ridu_cli_test_devrename_%d", time.Now().UnixNano())
	parsed.Path, parsed.RawPath = "/"+databaseName, ""
	databaseURL := parsed.String()
	client, err := mongo.Connect(options.Client().ApplyURI(databaseURL))
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

	session := startDevelopmentSession(t, "mongodb", "--database-url", databaseURL)
	session.renameTitle(t)

	ctx := context.Background()
	backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{DatabaseURL: databaseURL, AllowInsecureTransport: true, ApplicationName: "ridu-development-rename-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	statuses, err := backend.ArtifactStatus(ctx, filepath.Join(session.target, "migrations"))
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("status after the rename = %#v, %v", statuses, err)
	}
}

// Over PostgreSQL the prompt takes the same path: the server stops, the
// runner applies the rename, and the replacement starts against the migrated
// database.
func TestDevelopmentRenamePromptReloadsARunningPostgresProject(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("RIDU_POSTGRES_URL"))
	if baseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run the PostgreSQL development rename qualification")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("ridu_cli_test_devrename_%d", time.Now().UnixNano())
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
	databaseURL := parsed.String()

	session := startDevelopmentSession(t, "postgres", "--database-url", databaseURL)
	session.renameTitle(t)

	backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: databaseURL, AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	statuses, err := backend.ArtifactStatus(ctx, filepath.Join(session.target, "migrations"))
	if err != nil || len(statuses) != 2 || !statuses[0].Applied || !statuses[1].Applied {
		t.Fatalf("status after the rename = %#v, %v", statuses, err)
	}
}

// Application code that uses a generated field stops compiling when the field
// is renamed, so the reload after an accepted rename is rejected. ridu dev
// keeps watching with no server, and the save that fixes the code brings one
// up against the renamed content.
func TestDevelopmentRenameWaitsForAFixWhenTheRenamedCodeDoesNotBuild(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(canonicalPath(t, t.TempDir()), "development.sqlite")
	const usage = "package content\n\nimport \"example.com/sqlite/devrename/generated\"\n\n// PostTitle is application code over the generated contract.\nfunc PostTitle(post generated.Post) *string { return post.Title }\n"
	session := startDevelopmentSessionWith(t, "sqlite", func(target string) {
		if err := os.WriteFile(filepath.Join(target, "content", "uses.go"), []byte(usage), 0o644); err != nil {
			t.Fatal(err)
		}
	}, "--database-path", databasePath)

	posts := func(name string) ridu.Config {
		return ridu.Config{Name: "Development rename", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text(name)}}}}
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	before, err := ridu.New(posts("title"), backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	session.acceptTitleRename(t)
	session.waitFor(t, "Reload rejected after the server was stopped for a schema change")
	session.waitFor(t, "The development server is stopped for the schema change")
	select {
	case <-session.exited:
		t.Fatalf("ridu dev exited instead of waiting for a fix:\n%s", session.printed())
	case <-time.After(time.Second):
	}
	if strings.Contains(session.printed(), "the previous server is still running") {
		t.Fatalf("ridu dev claimed a stopped server was running:\n%s", session.printed())
	}

	session.replace(t, "content/uses.go", "post.Title", "post.Headline")
	session.waitFor(t, "Server reloaded")
	session.requireServing(t)
	after, err := ridu.New(posts("headline"), backend)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := after.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if headline, _ := kept.Values["headline"].StringValue(); headline != "Hello" {
		t.Fatalf("renamed document = %#v", kept.Values)
	}
	if strings.Count(session.printed(), "Preserve its existing data as a rename?") != 1 {
		t.Fatalf("the rename was asked about again:\n%s", session.printed())
	}
}

// Without a terminal, a rename saved while ridu dev runs is rejected on every
// save, including one made after ridu generate rewrote the schema file, and
// the stored values stay where they are. Migrating the rename by hand
// releases the next save.
func TestDevelopmentRenameWithoutATerminalHoldsARunningSQLiteProject(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(canonicalPath(t, t.TempDir()), "development.sqlite")
	session := startDevelopmentSessionIn(t, "sqlite", false, nil, "--database-path", databasePath)

	posts := func(name string) ridu.Config {
		return ridu.Config{Name: "Development rename", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text(name)}}}}
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	before, err := ridu.New(posts("title"), backend)
	if err != nil {
		t.Fatal(err)
	}
	post, err := before.Local().Create(ctx, "posts", store.Values{"title": store.String("Hello")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	save := func(note string) {
		t.Helper()
		path := filepath.Join(session.target, "content", "posts.go")
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(source, []byte("\n// "+note+"\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const paused = "paused for a possible rename"
	requireHeld := func(step string, rejections int) {
		t.Helper()
		session.waitForCount(t, paused, rejections)
		found, err := before.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
		if title, _ := found.Values["title"].StringValue(); err != nil || title != "Hello" {
			t.Fatalf("%s moved or lost the title: %#v, %v", step, found.Values, err)
		}
		if strings.Contains(session.printed(), "Server reloaded") {
			t.Fatalf("%s reloaded the server:\n%s", step, session.printed())
		}
	}

	session.replace(t, "content/posts.go", `field.Text("title")`, `field.Text("headline")`)
	requireHeld("the first save", 1)
	save("a second save")
	requireHeld("the second save", 2)

	options := cli.Options{WorkingDirectory: session.target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
	run := func(arguments ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exitCode := cli.Run(ctx, arguments, &stdout, &stderr, options); exitCode != 0 {
			t.Fatalf("ridu %v: %s%s", arguments, stdout.String(), stderr.String())
		}
	}
	run("generate")
	save("a save after ridu generate")
	requireHeld("the save after ridu generate", 3)

	run("migrate", "create", "--name", "rename-title", "--accept-renames")
	run("migrate", "baseline", "--database-path", databasePath)
	run("migrate", "up", "--database-path", databasePath)
	save("a save after the manual migration")
	session.waitFor(t, "Server reloaded")
	session.requireServing(t)
	after, err := ridu.New(posts("headline"), backend)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := after.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
	if headline, _ := moved.Values["headline"].StringValue(); err != nil || headline != "Hello" {
		t.Fatalf("the migrated document = %#v, %v", moved.Values, err)
	}
	if rejections := strings.Count(session.printed(), paused); rejections != 3 {
		t.Fatalf("the rename was rejected %d times:\n%s", rejections, session.printed())
	}
}
