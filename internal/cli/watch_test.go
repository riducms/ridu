package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoSourceWatcherReportsGoChangesAndIgnoresGeneratedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	watcher, err := newGoSourceWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watcher.Close() })

	generated := filepath.Join(root, "generated")
	if err := os.Mkdir(generated, 0o755); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(generated, "ridu.generated.go")
	if err := os.WriteFile(ignored, []byte("package generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watcher.Changes():
		t.Fatal("generated Go file triggered a reload")
	case <-time.After(2 * goSourceDebounce):
	}

	changed := filepath.Join(root, "content", "config.go")
	revision := watcher.Revision()
	if err := os.WriteFile(changed, []byte("package content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for watcher.Revision() == revision && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if watcher.Revision() == revision {
		t.Fatal("Go change did not advance the immediate watcher revision")
	}
	select {
	case <-watcher.Changes():
	case err := <-watcher.Errors():
		t.Fatalf("watcher error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Go change")
	}
}

func TestAdminSchemaReloadSignalChanges(t *testing.T) {
	root := t.TempDir()
	if err := writeAdminSchemaReloadSignal(root, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(adminSchemaReloadSignal))
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var firstSignal adminSchemaUpdateSignal
	if err := json.Unmarshal(first, &firstSignal); err != nil {
		t.Fatalf("decode first signal: %v", err)
	}
	if firstSignal.Version != 1 || firstSignal.FullReload {
		t.Fatalf("first signal = %+v, want hot version 1", firstSignal)
	}
	time.Sleep(time.Millisecond)
	if err := writeAdminSchemaReloadSignal(root, true); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(first)) == strings.TrimSpace(string(second)) {
		t.Fatalf("admin schema reload signal did not change: %q", first)
	}
	var secondSignal adminSchemaUpdateSignal
	if err := json.Unmarshal(second, &secondSignal); err != nil {
		t.Fatalf("decode second signal: %v", err)
	}
	if !secondSignal.FullReload || secondSignal.Revision == firstSignal.Revision {
		t.Fatalf("second signal = %+v, want newer full reload", secondSignal)
	}
}
