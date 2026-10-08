package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/migration"
)

func TestSQLiteBaselineReplacementRefusesStaleExecutableHeadWithoutChangingHistory(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	oldManifest, err := ridu.Resolve(ridu.Config{Name: "Baseline replacement", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}})
	if err != nil {
		t.Fatal(err)
	}
	currentManifest, err := ridu.Resolve(ridu.Config{Name: "Baseline replacement", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary")}}}})
	if err != nil {
		t.Fatal(err)
	}
	oldDirectory, replacementDirectory := filepath.Join(root, "previous"), filepath.Join(root, "migrations")
	for index, directory := range []string{oldDirectory, replacementDirectory} {
		if _, err := sqlite.CreateArtifact(ctx, directory, "initial", oldManifest, time.Unix(int64(index+1), 0), sqlite.ArtifactOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	databasePath := filepath.Join(root, "database.sqlite")
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.ApplyArtifacts(ctx, oldDirectory); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runSQLiteMigrate(ctx, sqliteMigrationCLIOptions{
		command: "baseline", replaceBaseline: true,
		replacementOptions: migration.BaselineReplacementOptions{PreviousDirectory: oldDirectory, AllowProduction: true},
		databasePath:       databasePath, databasePathFlag: true,
		directory:          replacementDirectory,
		definition:         projectfile.File{Root: root, Database: projectfile.DatabaseSQLite},
		executableManifest: currentManifest,
	}, &stdout, &stderr, Options{WorkingDirectory: root})
	if code != 1 || !strings.Contains(stderr.String(), "validate replacement migration artifact history") {
		t.Fatalf("stale head: code=%d, stdout=%s, stderr=%s", code, stdout.String(), stderr.String())
	}
	status, err := backend.ArtifactStatus(ctx, oldDirectory, oldManifest)
	if err != nil || len(status) != 1 || !status[0].Applied {
		t.Fatalf("refusal altered original artifact history: %#v, %v", status, err)
	}
	if err := backend.Ready(ctx, oldManifest); err != nil {
		t.Fatalf("refusal altered original readiness: %v", err)
	}
}

func TestSQLiteBaselineReplacementAsksBeforeRewritingHistoryAtTheRecordedHead(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	manifest, err := ridu.Resolve(ridu.Config{Name: "Baseline replacement", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}})
	if err != nil {
		t.Fatal(err)
	}
	oldDirectory, replacementDirectory := filepath.Join(root, "previous"), filepath.Join(root, "migrations")
	for index, directory := range []string{oldDirectory, replacementDirectory} {
		if _, err := sqlite.CreateArtifact(ctx, directory, "initial", manifest, time.Unix(int64(index+1), 0), sqlite.ArtifactOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	databasePath := filepath.Join(root, "database.sqlite")
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := backend.ApplyArtifacts(ctx, oldDirectory); err != nil {
		t.Fatal(err)
	}
	run := func(options Options) (int, string, string) {
		var stdout, stderr bytes.Buffer
		options.WorkingDirectory = root
		code := runSQLiteMigrate(ctx, sqliteMigrationCLIOptions{
			command: "baseline", replaceBaseline: true,
			replacementOptions: migration.BaselineReplacementOptions{PreviousDirectory: oldDirectory},
			databasePath:       databasePath, databasePathFlag: true,
			directory:          replacementDirectory,
			definition:         projectfile.File{Root: root, Database: projectfile.DatabaseSQLite},
			executableManifest: manifest,
		}, &stdout, &stderr, options)
		return code, stdout.String(), stderr.String()
	}
	records := func(directory string) bool {
		status, err := backend.ArtifactStatus(ctx, directory, manifest)
		return err == nil && len(status) == 1 && status[0].Applied
	}
	code, stdout, stderr := run(Options{})
	if code != 1 || !strings.Contains(stderr, "exactly at its recorded migration head") || !strings.Contains(stderr, "--allow-production") || strings.Contains(stdout, "[y/N]") || !records(oldDirectory) {
		t.Fatalf("without a terminal: code=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
	code, stdout, stderr = run(Options{Interactive: true, Stdin: strings.NewReader("maybe\nn\n")})
	if code != 1 || !strings.Contains(stdout, "Replace the recorded history anyway? [y/N]") || !strings.Contains(stdout, "is neither") || !strings.Contains(stderr, "cancelled") || !records(oldDirectory) {
		t.Fatalf("declined: code=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
	code, stdout, stderr = run(Options{Interactive: true, Stdin: strings.NewReader("y\n")})
	if code != 0 || !strings.Contains(stdout, "Caution: this database is exactly at its recorded migration head") || !strings.Contains(stdout, "Recorded 1 migration") || !records(replacementDirectory) {
		t.Fatalf("confirmed: code=%d, stdout=%s, stderr=%s", code, stdout, stderr)
	}
}
