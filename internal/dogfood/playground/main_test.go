package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequiredFrameworkVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "go.mod")
	contents := "module example.com/playground\n\ngo 1.25.13\n\nrequire github.com/riducms/ridu v0.0.0-playground.1\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	version, err := requiredFrameworkVersion(path)
	if err != nil {
		t.Fatalf("requiredFrameworkVersion: %v", err)
	}
	if version != "v0.0.0-playground.1" {
		t.Fatalf("version = %q, want %q", version, "v0.0.0-playground.1")
	}
}
