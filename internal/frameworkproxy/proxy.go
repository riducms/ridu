// Package frameworkproxy publishes a Ridu checkout to a local Go module proxy
// for release-path tests and manual framework dogfooding.
package frameworkproxy

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const modulePath = "github.com/riducms/ridu"

// Publish writes versioned module-proxy artifacts for the framework checkout
// and returns the file:// proxy URL.
func Publish(frameworkRoot, proxyRoot, version string) (string, error) {
	versionRoot := filepath.Join(proxyRoot, "github.com", "riducms", "ridu", "@v")
	if err := os.MkdirAll(versionRoot, 0o755); err != nil {
		return "", fmt.Errorf("create module proxy: %w", err)
	}
	moduleFile, err := os.ReadFile(filepath.Join(frameworkRoot, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read framework go.mod: %w", err)
	}
	for name, content := range map[string][]byte{
		version + ".mod":  moduleFile,
		version + ".info": []byte(fmt.Sprintf("{\"Version\":%q,\"Time\":\"2026-08-05T00:00:00Z\"}\n", version)),
		"list":            []byte(version + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(versionRoot, name), content, 0o644); err != nil {
			return "", fmt.Errorf("write module proxy metadata: %w", err)
		}
	}
	if err := writeArchive(frameworkRoot, filepath.Join(versionRoot, version+".zip"), version); err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: proxyRoot}).String(), nil
}

func writeArchive(frameworkRoot, archivePath, version string) (resultError error) {
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create module proxy archive: %w", err)
	}
	defer func() {
		if err := archiveFile.Close(); resultError == nil && err != nil {
			resultError = fmt.Errorf("close module proxy archive: %w", err)
		}
	}()

	archive := zip.NewWriter(archiveFile)
	defer func() {
		if err := archive.Close(); resultError == nil && err != nil {
			resultError = fmt.Errorf("finish module proxy archive: %w", err)
		}
	}()
	prefix := modulePath + "@" + version + "/"
	if err := filepath.WalkDir(frameworkRoot, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, err := filepath.Rel(frameworkRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && skipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		embeddedAdminAsset := strings.HasPrefix(filepath.ToSlash(relative), "internal/adminassets/dist/")
		if relative != "go.mod" && relative != "go.sum" && !embeddedAdminAsset && (!strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go")) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writer, err := archive.Create(prefix + filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		_, err = writer.Write(content)
		return err
	}); err != nil {
		return fmt.Errorf("build module proxy archive: %w", err)
	}
	return nil
}

func skipDirectory(name string) bool {
	switch name {
	case ".git", ".ridu", ".zeno", "node_modules", "playground":
		return true
	default:
		return false
	}
}
