package main

import (
	"os"
	"path/filepath"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
)

func historyDigest(directory string) (string, error) {
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return "", err
	}
	return migrationartifact.HistoryDigest(files)
}

// The probe measures framework work without a database, using the in-memory test store.
func newProbeApplication(config ridu.Config) (*ridu.App, error) {
	return ridu.New(config, teststore.New())
}

// generatedContracts runs `ridu generate`'s resolved-manifest path into a scratch project and
// reports each artifact's size: the canonical manifest, Go client, OpenAPI and TypeScript client.
func generatedContracts(manifest schema.Manifest, directory string) (map[string]int64, error) {
	root := filepath.Join(directory, "generated")
	if err := os.RemoveAll(root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "content"), 0o755); err != nil {
		return nil, err
	}
	definition := projectfile.File{
		Path:    filepath.Join(root, "ridu.toml"),
		Root:    root,
		Version: 1,
		Schema:  "content/ridu.schema.json",
		OpenAPI: "openapi.json",
		Client:  "client.ts",
	}
	if _, err := generate.RunResolved(definition, manifest, false); err != nil {
		return nil, err
	}
	sizes := map[string]int64{}
	for name, path := range map[string]string{
		"manifest": "content/ridu.schema.json", "go": "content/ridu.generated.go",
		"openAPI": "openapi.json", "typescript": "client.ts",
	} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		sizes[name] = info.Size()
	}
	return sizes, nil
}
