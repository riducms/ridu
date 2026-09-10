package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/schema"
)

func TestMongoDBCLICommandsExecuteFinalValidatedHistorySnapshot(t *testing.T) {
	ctx := context.Background()
	for _, mutation := range []string{"replacement", "append"} {
		t.Run(mutation, func(t *testing.T) {
			directory := t.TempDir()
			manifest := resolveMongoDBCLITestManifest(t, false)
			created, err := mongodb.CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := migrationartifact.RequireCurrentHistory(directory, manifest); err != nil {
				t.Fatalf("initial history validation: %v", err)
			}

			changedManifest := resolveMongoDBCLITestManifest(t, true)
			if mutation == "replacement" {
				if err := os.Remove(created.Path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := mongodb.CreateArtifact(ctx, directory, mutation, changedManifest, time.Unix(2, 0)); err != nil {
				t.Fatal(err)
			}

			for _, command := range []string{"up", "verify", "plan", "status"} {
				t.Run(command, func(t *testing.T) {
					request := mongoDBMigrationCLIOptions{
						command: command, databaseURL: "mongodb://", allowInsecureDatabase: true,
						directory: directory, executableManifest: manifest,
					}
					var stdout, stderr bytes.Buffer
					output := newCLIOutput(&stdout, &stderr, cliOutputOptions{})
					if code := runMongoDBMigrate(ctx, request, &stdout, &stderr, output); code != 1 {
						t.Fatalf("%s exit code = %d, stdout %q, stderr %q", command, code, stdout.String(), stderr.String())
					}
					if !strings.Contains(stderr.String(), "does not match latest migration artifact") {
						t.Fatalf("%s changed-history error = %q", command, stderr.String())
					}
					if strings.Contains(stderr.String(), "open MongoDB") || strings.Contains(stderr.String(), "connection") {
						t.Fatalf("%s opened MongoDB before final history validation: %q", command, stderr.String())
					}
				})
			}
		})
	}
}

func resolveMongoDBCLITestManifest(t *testing.T, summary bool) schema.Manifest {
	t.Helper()
	fields := field.Fields{field.Text("title")}
	if summary {
		fields = append(fields, field.Text("summary"))
	}
	manifest, err := ridu.Resolve(ridu.Config{
		Name:        "MongoDB CLI snapshot",
		Collections: []ridu.Collection{{Slug: "posts", Fields: fields}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
