package goworkspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/internal/goworkspace"
)

func TestIsolateUnlistedModule(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeFile(t, filepath.Join(workspaceRoot, "go.mod"), "module example.com/workspace\n\ngo 1.25.13\n")
	writeFile(t, filepath.Join(workspaceRoot, "go.work"), "go 1.25.13\n\nuse .\n")
	projectRoot := filepath.Join(workspaceRoot, "project")
	writeFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/project\n\ngo 1.25.13\n")

	environment := goworkspace.IsolateUnlistedModule(projectRoot, []string{"PATH=/bin"})
	if value := environmentValue(environment, "GOWORK"); value != "off" {
		t.Fatalf("GOWORK = %q, want off; environment: %v", value, environment)
	}
}

func TestRetainWorkspaceForListedModule(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectRoot := filepath.Join(workspaceRoot, "project")
	writeFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/project\n\ngo 1.25.13\n")
	workFile := filepath.Join(workspaceRoot, "go.work")
	writeFile(t, workFile, "go 1.25.13\n\nuse ./project\n")

	environment := goworkspace.IsolateUnlistedModule(projectRoot, []string{"GOWORK=" + workFile})
	if value := environmentValue(environment, "GOWORK"); value != workFile {
		t.Fatalf("GOWORK = %q, want %q", value, workFile)
	}
}

func TestRespectExplicitWorkspaceDisable(t *testing.T) {
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/project\n\ngo 1.25.13\n")

	environment := goworkspace.IsolateUnlistedModule(projectRoot, []string{"GOWORK=off"})
	if value := environmentValue(environment, "GOWORK"); value != "off" {
		t.Fatalf("GOWORK = %q, want off", value)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func environmentValue(environment []string, key string) string {
	for _, variable := range environment {
		if len(variable) > len(key) && variable[:len(key)+1] == key+"=" {
			return variable[len(key)+1:]
		}
	}
	return ""
}
