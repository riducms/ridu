package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/typescript"
)

func TestGeneratedClientIsCurrent(t *testing.T) {
	manifest, err := ridu.Resolve(config())
	if err != nil {
		t.Fatal(err)
	}
	actual, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "editor_app", "src", "lib", "ridu.generated.ts")
	if os.Getenv("RIDU_UPDATE_GENERATED_CLIENT") == "1" {
		if err := os.WriteFile(path, actual, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated client %s: %v", path, err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("generated client drift; run RIDU_UPDATE_GENERATED_CLIENT=1 go test ./tests/contracts/editor_server to regenerate %s", path)
	}
}
