package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/tests/contracts/primitivelists"
)

func TestPrimitiveListFixtureArtifactsCurrent(t *testing.T) {
	manifest, err := core.Resolve(primitivelists.Config())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join("..", "..", "tests", "contracts", "primitivelists", "generated")
	for name, generate := range map[string]func(schema.Manifest) ([]byte, error){
		"schema.json":       func(m schema.Manifest) ([]byte, error) { return m.Bytes() },
		"ridu.generated.go": goClient, "ridu.generated.ts": typescript.Client, "openapi.json": openAPI,
		"schema.graphql": func(m schema.Manifest) ([]byte, error) { sdl, err := graphql.GenerateSDL(m); return []byte(sdl), err },
	} {
		t.Run(name, func(t *testing.T) {
			actual, err := generate(manifest)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, name)
			if os.Getenv("RIDU_UPDATE_PRIMITIVE_LIST_FIXTURE") == "1" {
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
				t.Fatal("generated primitive list fixture drift: RIDU_UPDATE_PRIMITIVE_LIST_FIXTURE=1 go test ./internal/generate -run TestPrimitiveListFixtureArtifactsCurrent")
			}
		})
	}
}
