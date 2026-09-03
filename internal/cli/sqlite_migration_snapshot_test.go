package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/schema"
)

func TestSQLiteCLICommandsExecuteFinalValidatedHistorySnapshot(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := resolveSQLiteCLITestManifest(t, false)
	created, err := sqlite.CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(created.Path); err != nil {
		t.Fatal(err)
	}
	replacementManifest := resolveSQLiteCLITestManifest(t, true)
	if _, err := sqlite.CreateArtifact(ctx, directory, "replacement", replacementManifest, time.Unix(2, 0), false); err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{"up", "down", "reset", "refresh", "fresh", "verify", "plan", "status"} {
		t.Run(command, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "application.sqlite")
			request := sqliteMigrationCLIOptions{
				command:            command,
				databasePath:       databasePath,
				directory:          directory,
				definition:         projectfile.File{Root: t.TempDir()},
				executableManifest: manifest,
			}
			var stdout, stderr bytes.Buffer
			if code := runSQLiteMigrate(ctx, request, &stdout, &stderr, Options{}); code != 1 {
				t.Fatalf("%s exit code = %d, stdout %q, stderr %q", command, code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "does not match latest migration artifact") {
				t.Fatalf("%s stale-history error = %q", command, stderr.String())
			}
			if command != "plan" && command != "status" {
				if _, err := os.Stat(databasePath); !os.IsNotExist(err) {
					t.Fatalf("%s opened database before final history validation: %v", command, err)
				}
			}
		})
	}
}

func resolveSQLiteCLITestManifest(t *testing.T, summary bool) schema.Manifest {
	t.Helper()
	fields := []field.Definition{field.Text("title")}
	if summary {
		fields = append(fields, field.Text("summary"))
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "SQLite CLI snapshot",
		Collections: []ridu.Collection{{Slug: "posts", Fields: fields}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
