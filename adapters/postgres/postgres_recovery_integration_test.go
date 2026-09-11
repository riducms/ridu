package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

func TestPostgresBackupRestoreDrill(t *testing.T) {
	if os.Getenv("RIDU_POSTGRES_RECOVERY_DRILL") != "1" {
		t.Skip("set RIDU_POSTGRES_RECOVERY_DRILL=1 to run the backup/restore drill")
	}
	baseURL := os.Getenv("RIDU_POSTGRES_URL")
	if baseURL == "" {
		t.Fatal("RIDU_POSTGRES_URL is required for the backup/restore drill")
	}
	commands := newPostgresRecoveryCommands(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sourceName := "ridu_recovery_source_" + suffix
	restoredName := "ridu_recovery_restored_" + suffix
	adminURL := postgresDatabaseURL(t, baseURL, "postgres")
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	for _, name := range []string{sourceName, restoredName} {
		identifier := pgx.Identifier{name}.Sanitize()
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
			t.Fatal(err)
		}
		name := name
		t.Cleanup(func() {
			cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			_, _ = admin.Exec(cleanupContext, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
			_, _ = admin.Exec(cleanupContext, "DROP DATABASE IF EXISTS "+identifier)
		})
	}

	config := ridu.Config{
		Name: "PostgreSQL recovery drill", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: field.Fields{field.Text("email").Required().Unique()}},
			{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required(), field.Relationship("author", "users").Required()}},
		},
	}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	sourceURL := postgresDatabaseURL(t, baseURL, sourceName)
	source, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: sourceURL, AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	directory := applyInitialArtifact(t, ctx, source, manifest)
	application, err := ridu.New(config, source)
	if err != nil {
		t.Fatal(err)
	}
	user, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("restore@example.test")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", user.ID, "recovery-drill-password"); err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Survives restore"), "author": store.String(user.ID),
	}, ridu.MutationOptions{Actor: &user})
	if err != nil {
		t.Fatal(err)
	}

	dumpPath := filepath.Join(t.TempDir(), "ridu.dump")
	commands.dump(t, dumpPath, sourceURL)
	restoredURL := postgresDatabaseURL(t, baseURL, restoredName)
	commands.restore(t, dumpPath, restoredURL)

	restored, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: restoredURL, AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	restoredApplication, err := ridu.New(config, restored)
	if err != nil {
		t.Fatal(err)
	}
	restoredPost, err := restoredApplication.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{Actor: &user})
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := restoredPost.Values["title"].StringValue(); title != "Survives restore" || restoredPost.Revision != post.Revision {
		t.Fatalf("restored document = %#v", restoredPost)
	}
	if _, err := restoredApplication.Login(ctx, "users", "restore@example.test", "recovery-drill-password"); err != nil {
		t.Fatalf("restored credentials failed: %v", err)
	}
	status, err := restored.ArtifactStatus(ctx, directory)
	if err != nil || len(status) != 1 || !status[0].Applied {
		t.Fatalf("restored migration ledger = %#v, %v", status, err)
	}
	if plan, err := restored.Plan(ctx, manifest); err != nil || len(plan) != 0 {
		t.Fatalf("restored schema plan = %#v, %v", plan, err)
	}
}

type postgresRecoveryCommands struct {
	docker, image, pgDump, pgRestore string
}

func newPostgresRecoveryCommands(t *testing.T) postgresRecoveryCommands {
	t.Helper()
	image := os.Getenv("RIDU_POSTGRES_CLIENT_IMAGE")
	if image != "" {
		return postgresRecoveryCommands{docker: requirePostgresCommand(t, "docker"), image: image}
	}
	return postgresRecoveryCommands{
		pgDump: requirePostgresCommand(t, "pg_dump"), pgRestore: requirePostgresCommand(t, "pg_restore"),
	}
}

func (commands postgresRecoveryCommands) dump(t *testing.T, dumpPath, databaseURL string) {
	t.Helper()
	arguments := []string{"--format=custom", "--no-owner", "--no-privileges", "--file", dumpPath, databaseURL}
	if commands.image == "" {
		runPostgresCommand(t, commands.pgDump, arguments...)
		return
	}
	commands.runContainer(t, "pg_dump", dumpPath, databaseURL, "--format=custom", "--no-owner", "--no-privileges", "--file", "/backup/ridu.dump")
}

func (commands postgresRecoveryCommands) restore(t *testing.T, dumpPath, databaseURL string) {
	t.Helper()
	arguments := []string{"--exit-on-error", "--no-owner", "--no-privileges", "--dbname", databaseURL, dumpPath}
	if commands.image == "" {
		runPostgresCommand(t, commands.pgRestore, arguments...)
		return
	}
	commands.runContainer(t, "pg_restore", dumpPath, databaseURL, "--exit-on-error", "--no-owner", "--no-privileges", "--dbname", containerPostgresURL(t, databaseURL), "/backup/ridu.dump")
}

func (commands postgresRecoveryCommands) runContainer(t *testing.T, tool, dumpPath, databaseURL string, arguments ...string) {
	t.Helper()
	if tool == "pg_dump" {
		arguments = append(arguments, containerPostgresURL(t, databaseURL))
	}
	dockerArguments := []string{
		"run", "--rm", "--add-host", "host.docker.internal:host-gateway",
		"--volume", filepath.Dir(dumpPath) + ":/backup", commands.image, tool,
	}
	runPostgresCommand(t, commands.docker, append(dockerArguments, arguments...)...)
}

func requirePostgresCommand(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is required when RIDU_POSTGRES_RECOVERY_DRILL=1: %v", name, err)
	}
	return path
}

func runPostgresCommand(t *testing.T, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", filepath.Base(name), err, strings.TrimSpace(string(output)))
	}
}

func postgresDatabaseURL(t *testing.T, baseURL, database string) string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + database
	parameters := parsed.Query()
	parameters.Del("search_path")
	parsed.RawQuery = parameters.Encode()
	return parsed.String()
}

func containerPostgresURL(t *testing.T, databaseURL string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	port := parsed.Port()
	parsed.Host = "host.docker.internal"
	if port != "" {
		parsed.Host += ":" + port
	}
	return parsed.String()
}
