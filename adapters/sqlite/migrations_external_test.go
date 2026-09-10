package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/migration"
)

func TestSQLiteCreateArtifactIsUsableOutsideTheAdapterPackage(t *testing.T) {
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "External SQLite migrations",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	additive, err := ridu.Resolve(ridu.Config{
		Name: "External SQLite migrations",
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title"), field.Text("summary").Index()},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	created, err := sqlite.CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	if created.Path != filepath.Join(directory, created.Name) ||
		!strings.HasSuffix(created.Name, "_initial.ridu.json") ||
		len(created.Checksum) != 64 || created.Version != migration.ArtifactVersion {
		t.Fatalf("created artifact = %#v", created)
	}
	if _, err := os.Stat(created.Path); err != nil {
		t.Fatal(err)
	}
	second, err := sqlite.CreateArtifact(ctx, directory, "add-summary", additive, time.Unix(2, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(second.Name, "_add-summary.ridu.json") || second.Name <= created.Name {
		t.Fatalf("second artifact = %#v after %#v", second, created)
	}
	if _, err := sqlite.CreateArtifact(ctx, directory, "duplicate", additive, time.Unix(3, 0), false); err == nil {
		t.Fatal("CreateArtifact accepted a migration when history was already current")
	}

	backend, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "application.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.Ready(ctx, additive); err != nil {
		t.Fatal(err)
	}
}
