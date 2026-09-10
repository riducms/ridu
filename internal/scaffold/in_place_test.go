package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/scaffold"
)

func TestCreateInPlacePreservesUnrelatedFiles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "My CMS")
	if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"notes.txt", ".git/config"} {
		if err := os.WriteFile(filepath.Join(target, path), []byte("keep me\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	created, err := scaffold.Create(scaffold.Options{
		Target: target, InPlace: true, ModulePath: "example.com/my-cms",
		NPMScope: "@my-cms", FrameworkVersion: "v1.2.3",
	})
	if err != nil || created != target {
		t.Fatalf("Create = %q, %v", created, err)
	}
	for _, path := range []string{"notes.txt", ".git/config"} {
		data, err := os.ReadFile(filepath.Join(target, path))
		if err != nil || string(data) != "keep me\n" {
			t.Fatalf("existing %s changed: %q, %v", path, data, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil || !strings.Contains(string(data), `"name": "my-cms"`) {
		t.Fatalf("package name = %s, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(target, ".agents", "skills", "ridu-project", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestCreateInPlaceRejectsConflictsBeforeWriting(t *testing.T) {
	for _, conflict := range []string{"README.md", ".gitignore", "AGENTS.md", ".agents", "content", "go.sum", "generated", "migrations", ".ridu", "admin-symlink"} {
		t.Run(conflict, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "existing-app")
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			path := conflict
			if conflict == "admin-symlink" {
				path = "admin"
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(target, path)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(target, path), []byte("existing"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := scaffold.Create(scaffold.Options{
				Target: target, InPlace: true, ModulePath: "example.com/existing-app",
				NPMScope: "@existing-app", FrameworkVersion: "v1.2.3",
			})
			if err == nil || !strings.Contains(err.Error(), "conflicting paths") || !strings.Contains(err.Error(), path) {
				t.Fatalf("conflict error = %v", err)
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 1 || entries[0].Name() != path {
				t.Fatalf("conflict left partial output: %v, %v", entries, err)
			}
			if conflict != "admin-symlink" {
				data, err := os.ReadFile(filepath.Join(target, path))
				if err != nil || string(data) != "existing" {
					t.Fatalf("conflict was modified: %q, %v", data, err)
				}
			}
		})
	}
}
