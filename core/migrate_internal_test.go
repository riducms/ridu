package core

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

type recordingMigrationDriver struct {
	validateErr error
	requests    []migration.ProjectRequest
}

func (driver *recordingMigrationDriver) Validate() error { return driver.validateErr }

func (driver *recordingMigrationDriver) DataTransforms() []migration.DataTransformDescriptor {
	return nil
}

func (driver *recordingMigrationDriver) RunProjectMigration(_ context.Context, request migration.ProjectRequest, _ schema.Manifest) error {
	driver.requests = append(driver.requests, request)
	return nil
}

// A built binary applies only the history it was built with, so it refuses
// before touching a database whenever it can't prove that. The generated
// SQLite project test covers applying a matching history.
func TestMigrateCommandRefusesWhatItCannotProve(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("RIDU_SQLITE_PATH", "")
	config := Config{Name: "Migrations", Collections: []Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}}
	driver := &recordingMigrationDriver{}
	for _, test := range []struct {
		name, digest, want string
		args               []string
		driver             migration.ProjectDriver
	}{
		{name: "no action", digest: "built", want: "usage: migrate up", driver: driver},
		{name: "another action", digest: "built", want: "usage: migrate up", args: []string{"down"}, driver: driver},
		{name: "positional argument", digest: "built", want: "positional", args: []string{"up", "now"}, driver: driver},
		{name: "binary from go build", want: "built with ridu build", args: []string{"up"}, driver: driver},
		{name: "no driver", digest: "built", want: "no migration driver", args: []string{"up"}},
		{name: "invalid driver", digest: "built", want: "duplicate transform", args: []string{"up"}, driver: &recordingMigrationDriver{validateErr: errors.New("duplicate transform")}},
		{name: "no migrations directory", digest: "built", want: "no migrations directory", args: []string{"up", "--directory", filepath.Join(t.TempDir(), "missing")}, driver: driver},
		{name: "no history", digest: "built", want: "migrations in", args: []string{"up", "--directory", t.TempDir()}, driver: driver},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runMigrateCommand(context.Background(), test.args, &stdout, &stderr, config, test.driver, test.digest)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to mention %q", err, test.want)
			}
			if strings.Contains(stdout.String(), "Migrations are current") {
				t.Fatalf("reported success: %q", stdout.String())
			}
		})
	}
	if len(driver.requests) != 0 {
		t.Fatalf("driver ran %d migrations despite refusing", len(driver.requests))
	}
}

func TestSQLitePathAbsoluteMatchesTheServer(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "content.sqlite")
	cases := map[string]bool{
		"":                 false,
		":memory:":         false,
		"content.sqlite":   false,
		"file:data.sqlite": false,
		absolute:           true,
		"file:" + filepath.ToSlash(absolute) + "?_pragma=busy_timeout(5000)": true,
	}
	if runtime.GOOS == "windows" {
		cases["file:///C:/data/content.sqlite"] = true
		cases["file://server/share/content.sqlite"] = true
	} else {
		cases["file:///var/lib/ridu/content.sqlite"] = true
	}
	for path, want := range cases {
		if got := sqlitePathAbsolute(path); got != want {
			t.Errorf("sqlitePathAbsolute(%q) = %v, want %v", path, got, want)
		}
	}
}
