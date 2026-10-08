package cli

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/fileuri"
)

func TestResolveSQLiteMigrationDatabasePathForSourceMatchesRuntimeContract(t *testing.T) {
	projectRoot := t.TempDir()
	if resolved, err := resolveSQLiteMigrationDatabasePathForSource(projectRoot, "relative.sqlite", false); err == nil || resolved != "" || !strings.Contains(err.Error(), "RIDU_SQLITE_PATH must be an absolute") {
		t.Fatalf("relative environment path = %q, %v", resolved, err)
	}
	resolved, err := resolveSQLiteMigrationDatabasePathForSource(projectRoot, "relative.sqlite", true)
	if err != nil || resolved != filepath.Join(projectRoot, "relative.sqlite") {
		t.Fatalf("relative flag path = %q, %v", resolved, err)
	}
	absolute := filepath.Join(projectRoot, "absolute.sqlite")
	resolved, err = resolveSQLiteMigrationDatabasePathForSource(projectRoot, absolute, false)
	if err != nil || resolved != absolute {
		t.Fatalf("absolute environment path = %q, %v", resolved, err)
	}
}

func TestResolveSQLiteMigrationDatabasePathRejectsSharedMemoryURIsBeforeRebasing(t *testing.T) {
	projectRoot := t.TempDir()
	for _, input := range []string{
		"file::memory:?cache=shared",
		"file:%3Amemory%3A?cache=shared",
		"file:%3amemory%3a?cache=shared",
		"file:ridu-shared?mode=memory&cache=shared",
		"file:ridu-shared?vfs=memdb",
	} {
		if resolved, err := resolveSQLiteMigrationDatabasePath(projectRoot, input); err == nil || !strings.Contains(err.Error(), "use :memory:") {
			t.Fatalf("resolve %q = %q, %v", input, resolved, err)
		}
	}
}

func TestResolveSQLiteMigrationDatabasePathRejectsDuplicateModeParameters(t *testing.T) {
	projectRoot := t.TempDir()
	for _, input := range []string{
		"file:database.sqlite?mode=rwc&mode=memory",
		"file:database.sqlite?mode=rwc&mode=ro",
	} {
		if resolved, err := resolveSQLiteMigrationDatabasePath(projectRoot, input); err == nil || !strings.Contains(err.Error(), `parameter "mode" must not be specified more than once`) {
			t.Fatalf("resolve %q = %q, %v", input, resolved, err)
		}
	}
}

func TestResolveSQLiteMigrationDatabasePathRebasesProjectRelativeFileURIs(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "my app")
	resolved, err := resolveSQLiteMigrationDatabasePath(projectRoot, "file:.ridu/development.sqlite?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		t.Fatalf("parse rebased URI %q: %v", resolved, err)
	}
	target, err := fileuri.ToPath(parsed)
	if err != nil || target != filepath.Join(projectRoot, ".ridu", "development.sqlite") || parsed.RawQuery != "mode=rwc" {
		t.Fatalf("rebased URI %q names %q, %v", resolved, target, err)
	}
	again, err := resolveSQLiteMigrationDatabasePath(projectRoot, resolved)
	if err != nil || again != resolved {
		t.Fatalf("absolute rebased URI resolved again to %q, %v", again, err)
	}
}
