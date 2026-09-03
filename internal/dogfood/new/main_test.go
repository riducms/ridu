package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCreatedProjectTreatsMissingResultAsCancellation(t *testing.T) {
	created, ok, err := readCreatedProject(filepath.Join(t.TempDir(), "missing"), "")
	if err != nil {
		t.Fatal(err)
	}
	if ok || created != "" {
		t.Fatalf("readCreatedProject() = %q, %t, want empty cancellation", created, ok)
	}
}

func TestReadCreatedProjectReturnsPromptedTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "prompted-project")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "ridu.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(t.TempDir(), "new-project-result")
	if err := os.WriteFile(resultFile, []byte(target+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	created, ok, err := readCreatedProject(resultFile, "")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || created != target {
		t.Fatalf("readCreatedProject() = %q, %t, want %q, true", created, ok, target)
	}
}

func TestReadCreatedProjectRejectsUnexpectedTarget(t *testing.T) {
	temporaryRoot := t.TempDir()
	target := filepath.Join(temporaryRoot, "created")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "ridu.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(temporaryRoot, "new-project-result")
	if err := os.WriteFile(resultFile, []byte(target+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readCreatedProject(resultFile, filepath.Join(temporaryRoot, "requested"))
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("readCreatedProject() error = %v, want unexpected target error", err)
	}
}
