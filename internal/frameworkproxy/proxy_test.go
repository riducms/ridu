package frameworkproxy

import (
	"archive/zip"
	"bytes"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSnapshotVersionsTrackAllPublishedInputsAndPreservePreviousModules(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/riducms/ridu\n\ngo 1.25.13\n")
	write("ridu.go", "package ridu\nconst Value = 1\n")
	write("go.sum", "original dependency checksums\n")
	write("plugins/widget/contract.json", `{"version":1}`)
	write("internal/adminassets/dist/index.html", "initial admin")
	publish := func() (string, string, []byte) {
		t.Helper()
		location, version, err := PublishSnapshot(root, cache)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(location)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(parsed.Path, "github.com", "riducms", "ridu", "@v", version+".zip")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return path, version, data
	}
	firstPath, firstVersion, firstBytes := publish()
	// Non-published test and disposable files must not invalidate Go dependencies.
	write("ridu_test.go", "package ridu\n")
	write(".ridu/generated.go", "disposable")
	_, sameVersion, sameBytes := publish()
	if sameVersion != firstVersion || !bytes.Equal(sameBytes, firstBytes) {
		t.Fatal("excluded files changed module snapshot")
	}
	previous := firstVersion
	for _, change := range []struct{ path, content string }{
		{"ridu.go", "package ridu\nconst Value = 2\n"},
		{"go.mod", "module github.com/riducms/ridu\n\ngo 1.25.14\n"},
		{"go.sum", "changed dependency checksums\n"},
		{"plugins/widget/contract.json", `{"version":2}`},
		{"internal/adminassets/dist/index.html", "changed admin"},
		{"renamed.go", "package ridu\n"},
	} {
		t.Run(change.path, func(t *testing.T) {
			write(change.path, change.content)
			_, version, archive := publish()
			if version == previous {
				t.Fatal("published input did not change immutable version")
			}
			previous = version
			reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
			if err != nil {
				t.Fatal(err)
			}
			file, err := reader.Open(modulePath + "@" + version + "/" + change.path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			file.Close()
			if err != nil || string(data) != change.content {
				t.Fatalf("captured module = %q, %v", data, err)
			}
		})
	}
	unchanged, err := os.ReadFile(firstPath)
	if err != nil || !bytes.Equal(unchanged, firstBytes) {
		t.Fatal("later publication overwrote an earlier immutable module")
	}
}

func TestConcurrentSnapshotPublicationIsCompleteAndIdentical(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/riducms/ridu\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	versions := make([]string, 4)
	for index := range versions {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			location, version, err := PublishSnapshot(root, cache)
			if err != nil {
				t.Error(err)
				return
			}
			versions[index] = version
			parsed, err := url.Parse(location)
			if err != nil {
				t.Error(err)
				return
			}
			for _, suffix := range []string{".zip", ".mod", ".info"} {
				if _, err := os.Stat(filepath.Join(parsed.Path, "github.com", "riducms", "ridu", "@v", version+suffix)); err != nil {
					t.Error(err)
				}
			}
		}(index)
	}
	wait.Wait()
	for _, version := range versions {
		if version == "" || version != versions[0] {
			t.Fatalf("concurrent snapshots differ: %v", versions)
		}
	}
}

func TestSnapshotRejectsDamagedPersistentArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/riducms/ridu\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, damaged := range []string{".zip", ".mod", ".info", "list", "receipt", "wrong version"} {
		t.Run(damaged, func(t *testing.T) {
			for _, missing := range []bool{false, true} {
				cache := t.TempDir()
				location, version, err := PublishSnapshot(root, cache)
				if err != nil {
					t.Fatal(err)
				}
				parsed, _ := url.Parse(location)
				path := snapshotFile(parsed.Path, version+damaged)
				if damaged == "list" {
					path = snapshotFile(parsed.Path, "list")
				}
				if damaged == "receipt" || damaged == "wrong version" {
					path = filepath.Join(parsed.Path, "snapshot.json")
				}
				if missing {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else {
					data := []byte("truncated")
					if damaged == "wrong version" {
						data, err = os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						data = []byte(strings.ReplaceAll(string(data), version, "v0.0.0-other"))
					}
					if err := os.WriteFile(path, data, 0644); err != nil {
						t.Fatal(err)
					}
				}
				if _, _, err := PublishSnapshot(root, cache); err == nil || !strings.Contains(err.Error(), "remove cache directory "+parsed.Path) {
					t.Fatalf("damaged snapshot accepted or unactionable failure: %v", err)
				}
			}
		})
	}
}
