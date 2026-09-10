// Package frameworkproxy publishes a Ridu checkout to a local Go module proxy
// for release-path tests and manual framework dogfooding.
package frameworkproxy

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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
	files, err := captureModule(frameworkRoot)
	if err != nil {
		return "", err
	}
	return publishFiles(files, proxyRoot, version)
}

func publishFiles(files []moduleFile, proxyRoot, version string) (string, error) {
	versionRoot := filepath.Join(proxyRoot, "github.com", "riducms", "ridu", "@v")
	if err := os.MkdirAll(versionRoot, 0o755); err != nil {
		return "", fmt.Errorf("create module proxy: %w", err)
	}
	var module []byte
	for _, file := range files {
		if file.name == "go.mod" {
			module = file.content
			break
		}
	}
	if module == nil {
		return "", fmt.Errorf("framework snapshot omits go.mod")
	}
	for name, content := range map[string][]byte{
		version + ".mod":  module,
		version + ".info": []byte(fmt.Sprintf("{\"Version\":%q,\"Time\":\"2026-08-05T00:00:00Z\"}\n", version)),
		"list":            []byte(version + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(versionRoot, name), content, 0o644); err != nil {
			return "", fmt.Errorf("write module proxy metadata: %w", err)
		}
	}
	if err := writeArchive(files, filepath.Join(versionRoot, version+".zip"), version); err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: proxyRoot}).String(), nil
}

func writeArchive(files []moduleFile, archivePath, version string) (resultError error) {
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
	for _, file := range files {
		writer, err := archive.Create(prefix + file.name)
		if err != nil {
			return err
		}
		if _, err := writer.Write(file.content); err != nil {
			return err
		}
	}
	return nil
}

type moduleFile struct {
	name    string
	content []byte
}

func captureModule(frameworkRoot string) ([]moduleFile, error) {
	var files []moduleFile
	err := filepath.WalkDir(frameworkRoot, func(path string, entry fs.DirEntry, walkError error) error {
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
		// Compiled plugins can embed their declarative JSON contracts. Keep these
		// package assets in release-shaped modules as well as the Go source.
		pluginContract := strings.HasPrefix(filepath.ToSlash(relative), "plugins/") && filepath.Ext(relative) == ".json"
		if relative != "go.mod" && relative != "go.sum" && !embeddedAdminAsset && !pluginContract && (!strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go")) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, moduleFile{name: filepath.ToSlash(relative), content: content})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("capture module snapshot: %w", err)
	}
	return files, nil
}

func skipDirectory(name string) bool {
	switch name {
	case ".git", ".ridu", ".zeno", "node_modules", "playground":
		return true
	default:
		return false
	}
}

// PublishSnapshot captures the exact module inputs before deriving an immutable
// prerelease version. Independent test processes can share Go's dependency and
// build caches without an old synthetic module version masking changed source.
// Publication is atomic; an existing snapshot is never rewritten.
func PublishSnapshot(frameworkRoot, cacheRoot string) (proxyURL, version string, err error) {
	files, err := captureModule(frameworkRoot)
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	// This domain also versions the fixed .info metadata and archive recipe above.
	io.WriteString(hash, "ridu-module-proxy-v1:2026-08-05T00:00:00Z\n")
	for _, file := range files {
		binary.Write(hash, binary.BigEndian, uint64(len(file.name)))
		io.WriteString(hash, file.name)
		binary.Write(hash, binary.BigEndian, uint64(len(file.content)))
		hash.Write(file.content)
	}
	version = fmt.Sprintf("v0.0.0-test.h%x", hash.Sum(nil))
	if err := os.MkdirAll(cacheRoot, 0755); err != nil {
		return "", "", err
	}
	destination := filepath.Join(cacheRoot, version)
	proxyURL = (&url.URL{Scheme: "file", Path: destination}).String()
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		if err := verifySnapshot(destination, version); err != nil {
			return "", "", err
		}
		return proxyURL, version, nil
	}
	staging, err := os.MkdirTemp(cacheRoot, ".snapshot-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(staging)
	if _, err := publishFiles(files, staging, version); err != nil {
		return "", "", err
	}
	if err := sealSnapshot(staging, version); err != nil {
		return "", "", err
	}
	if err := os.Rename(staging, destination); err != nil {
		// Another process may have published the identical captured inputs first.
		if info, statErr := os.Stat(destination); statErr != nil || !info.IsDir() {
			return "", "", err
		}
	}
	if err := verifySnapshot(destination, version); err != nil {
		return "", "", err
	}
	return proxyURL, version, nil
}

// The receipt is written inside staging, after every proxy file is closed. It
// detects interrupted/manual cache damage even if Go already cached this module.
type snapshotReceipt struct {
	Version string                        `json:"version"`
	Files   map[string]snapshotFileDigest `json:"files"`
}
type snapshotFileDigest struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func proxyFiles(version string) []string {
	return []string{version + ".zip", version + ".mod", version + ".info", "list"}
}
func snapshotFile(root, name string) string {
	return filepath.Join(root, "github.com", "riducms", "ridu", "@v", name)
}
func digestSnapshotFile(path string) (snapshotFileDigest, error) {
	file, err := os.Open(path)
	if err != nil {
		return snapshotFileDigest{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return snapshotFileDigest{}, err
	}
	return snapshotFileDigest{SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Size: size}, nil
}
func sealSnapshot(root, version string) error {
	receipt := snapshotReceipt{Version: version, Files: make(map[string]snapshotFileDigest)}
	for _, name := range proxyFiles(version) {
		digest, err := digestSnapshotFile(snapshotFile(root, name))
		if err != nil {
			return err
		}
		receipt.Files[name] = digest
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "snapshot.json"), data, 0644)
}
func verifySnapshot(root, version string) error {
	invalid := func(err error) error {
		return fmt.Errorf("invalid immutable module snapshot: %w; remove cache directory %s and retry", err, root)
	}
	data, err := os.ReadFile(filepath.Join(root, "snapshot.json"))
	if err != nil {
		return invalid(err)
	}
	var receipt snapshotReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return invalid(err)
	}
	if receipt.Version != version || len(receipt.Files) != len(proxyFiles(version)) {
		return invalid(fmt.Errorf("completion receipt does not match %s", version))
	}
	for _, name := range proxyFiles(version) {
		expected, ok := receipt.Files[name]
		if !ok {
			return invalid(fmt.Errorf("completion receipt omits %s", name))
		}
		actual, err := digestSnapshotFile(snapshotFile(root, name))
		if err != nil {
			return invalid(err)
		}
		if actual != expected {
			return invalid(fmt.Errorf("checksum or length changed for %s", name))
		}
	}
	return nil
}
