package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/tests/contracts/unifiedfields"
)

func TestUnifiedFixtureArtifactsCurrent(t *testing.T) {
	manifest, err := core.Resolve(unifiedfields.Config())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join("..", "..", "tests", "contracts", "unifiedfields", "generated")
	for name, generate := range map[string]func(schema.Manifest) ([]byte, error){
		"schema.json":       func(m schema.Manifest) ([]byte, error) { return m.Bytes() },
		"ridu.generated.go": goClient, "ridu.generated.ts": typescript.Client, "openapi.json": openAPI,
	} {
		t.Run(name, func(t *testing.T) {
			actual, err := generate(manifest)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := generate(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, repeated) {
				t.Fatal("generation is nondeterministic")
			}
			path := filepath.Join(directory, name)
			if os.Getenv("RIDU_UPDATE_UNIFIED_FIXTURE") == "1" {
				if err := os.MkdirAll(directory, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, actual, 0644); err != nil {
					t.Fatal(err)
				}
			}
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, expected) {
				t.Fatalf("generated fixture drift: RIDU_UPDATE_UNIFIED_FIXTURE=1 go test ./internal/generate -run TestUnifiedFixtureArtifactsCurrent")
			}
		})
	}
}
