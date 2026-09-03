package local

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPingRequiresWriteAccessAndCleansItsProbe(t *testing.T) {
	root := t.TempDir()
	operations := defaultFilesystemOperations()
	backend := &Backend{root: root, filesystem: operations}
	if err := backend.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDirectoryEmpty(t, root)

	operations.createTemp = func(string, string) (*os.File, error) {
		return nil, fs.ErrPermission
	}
	backend = &Backend{root: root, filesystem: operations}
	if err := backend.Ping(context.Background()); err == nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("read-only Ping error = %v", err)
	}
	assertDirectoryEmpty(t, root)
}

func TestPingPropagatesDirectorySyncFailureWithoutLeavingAProbe(t *testing.T) {
	root := t.TempDir()
	operations := defaultFilesystemOperations()
	syncFailure := errors.New("injected directory sync failure")
	operations.syncDirectory = func(string) error { return syncFailure }
	backend := &Backend{root: root, filesystem: operations}

	err := backend.Ping(context.Background())
	if err == nil || !errors.Is(err, syncFailure) {
		t.Fatalf("Ping error = %v", err)
	}
	assertDirectoryEmpty(t, root)
}

func TestPutAndDeleteRequireDurableDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	syncFailure := errors.New("injected directory sync failure")
	operations := defaultFilesystemOperations()
	operations.syncDirectory = func(string) error { return syncFailure }
	backend := &Backend{root: root, filesystem: operations}

	err := backend.Put(context.Background(), "object.txt", strings.NewReader("x"), 1, "text/plain")
	if err == nil || !errors.Is(err, syncFailure) || !strings.Contains(err.Error(), "after replace") {
		t.Fatalf("Put error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "object.txt")); err != nil {
		t.Fatalf("renamed object missing after sync failure: %v", err)
	}

	err = backend.Delete(context.Background(), "object.txt")
	if err == nil || !errors.Is(err, syncFailure) || !strings.Contains(err.Error(), "after delete") {
		t.Fatalf("Delete error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "object.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleted object stat error = %v", err)
	}
}

func TestMkdirAllDurableSyncsEveryCreatedParentEntry(t *testing.T) {
	root := t.TempDir()
	operations := defaultFilesystemOperations()
	var synced []string
	actualSync := operations.syncDirectory
	operations.syncDirectory = func(path string) error {
		synced = append(synced, path)
		return actualSync(path)
	}
	directory := filepath.Join(root, "one", "two")
	if err := mkdirAllDurable(operations, directory, 0o750); err != nil {
		t.Fatal(err)
	}
	want := []string{root, filepath.Join(root, "one")}
	if len(synced) != len(want) {
		t.Fatalf("synced directories = %#v", synced)
	}
	for index := range want {
		if synced[index] != want[index] {
			t.Fatalf("sync[%d] = %q, want %q", index, synced[index], want[index])
		}
	}
}

func assertDirectoryEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory contains readiness artifacts: %#v", entries)
	}
}
