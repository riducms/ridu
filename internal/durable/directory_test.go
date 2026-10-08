package durable

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryAcceptsAWrittenDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "artifact.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncDirectory(directory); err != nil {
		t.Fatalf("SyncDirectory(%q) = %v", directory, err)
	}
}
