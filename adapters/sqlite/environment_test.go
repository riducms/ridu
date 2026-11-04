package sqlite

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigFromEnvironmentRequiresAnAbsoluteDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content.sqlite")
	t.Setenv("RIDU_SQLITE_PATH", path)
	config, err := ConfigFromEnvironment()
	if err != nil || config.Path != path {
		t.Fatalf("config = %#v, %v", config, err)
	}
	t.Setenv("RIDU_SQLITE_PATH", "content.sqlite")
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "RIDU_SQLITE_PATH") {
		t.Fatalf("relative path error = %v", err)
	}
}
