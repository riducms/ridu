package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/projectfile"
)

// ridu upgrade runs the upgraded CLI's migrate create and must tell a created
// migration from a schema that needs none, which migrate create reports as
// success.
func TestCreateUpgradeMigrationReadsTheUpgradedCLIOutcome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in CLI is a shell script")
	}
	for name, test := range map[string]struct {
		script  string
		created bool
		failure string
	}{
		"created":             {script: "echo 'Created migration migrations/x_ridu-0-12-0.ridu.json; review it before running ridu migrate up.'", created: true},
		"no migration needed": {script: "echo '" + noMigrationNeeded + ": the schema has not changed since the latest migration.'"},
		"admin settings only": {script: "echo '" + noMigrationNeeded + ": only admin settings changed since the latest migration, and history ignores them.'"},
		"failure":             {script: "echo '[ERROR] plan migration: migration requires explicit safety resolution' >&2; exit 1", failure: "explicit safety resolution"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "node_modules", ".bin", "ridu")
			if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+test.script+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			created, err := createUpgradeMigration(context.Background(), projectfile.File{Root: root}, "ridu-0-12-0", &stdout)
			if test.failure != "" {
				if err == nil || !strings.Contains(err.Error(), test.failure) {
					t.Fatalf("failure = %v", err)
				}
				return
			}
			if err != nil || created != test.created {
				t.Fatalf("created = %t, %v; want %t", created, err, test.created)
			}
			if printedCreation := strings.Contains(stdout.String(), "Created migration"); printedCreation != test.created {
				t.Fatalf("printed %q", stdout.String())
			}
		})
	}
}
