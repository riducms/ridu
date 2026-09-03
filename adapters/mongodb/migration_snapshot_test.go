package mongodb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
)

func TestMongoDBProjectMigrationDriverExecutesValidatedArtifactSnapshot(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	manifest := mongoDBMigrationTestManifest(t, false, false)
	created, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrationartifact.RequireCurrentHistory(directory, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(created.Path); err != nil {
		t.Fatal(err)
	}

	driver := ProjectMigrations().(*mongoDBProjectMigrationDriver)
	registry, err := newMongoDBDataTransformRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []ridumigration.ProjectAction{ridumigration.ProjectApply, ridumigration.ProjectVerify} {
		t.Run(string(action), func(t *testing.T) {
			request := ridumigration.ProjectRequest{
				Action: action, DatabaseURL: "mongodb://", Directory: directory, AllowInsecureDatabase: true,
			}
			err := driver.runProjectMigrationFiles(ctx, request, files, registry)
			if err == nil || !strings.Contains(err.Error(), "invalid MongoDB connection configuration") {
				t.Fatalf("execute retained %s snapshot error = %v", action, err)
			}
			if strings.Contains(err.Error(), "history is empty") {
				t.Fatalf("execute retained %s snapshot reread the artifact directory: %v", action, err)
			}
		})
	}
}
