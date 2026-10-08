//go:build !windows

package durable

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryReportsAMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := SyncDirectory(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("SyncDirectory(%q) = %v, want not exist", missing, err)
	}
}
